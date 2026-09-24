package tunnel

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type TunnelState string

const (
	StateDisconnected TunnelState = "DISCONNECTED"
	StateConnecting   TunnelState = "CONNECTING"
	StateInjecting    TunnelState = "INJECTING"
	StateSSHAuth      TunnelState = "SSH_AUTH"
	StateConnected    TunnelState = "CONNECTED"
	StateReconnecting TunnelState = "RECONNECTING"
	StateError        TunnelState = "ERROR"
)

type Telemetry struct {
	State          TunnelState `json:"state"`
	StateMessage   string      `json:"state_message"`
	DownloadSpeed  uint64      `json:"download_speed_bps"` // bytes/sec
	UploadSpeed    uint64      `json:"upload_speed_bps"`   // bytes/sec
	TotalBytesIn   uint64      `json:"total_bytes_in"`
	TotalBytesOut  uint64      `json:"total_bytes_out"`
	PingMs         int64       `json:"ping_ms"`
	UptimeSeconds  int64       `json:"uptime_sec"`
	ActiveConns    int64       `json:"active_conns"`
	SocksPort      int         `json:"socks_port"`
	SysProxyActive bool        `json:"sys_proxy_active"`
	VPNActive      bool        `json:"vpn_active"`
	Logs           []string    `json:"logs"`
}

type TunnelManager struct {
	store        *ConfigStore
	mu           sync.RWMutex
	state        TunnelState
	stateMsg     string
	startedAt    time.Time
	sshPool      *SSHPool
	dnsForwarder *DNSForwarder
	socksServer  *SocksServer
	vpn          *VPNController
	sysProxyOn   bool

	logs     []string
	maxLogs  int
	cancelFn context.CancelFunc

	// Throughput calculation
	lastBytesIn   uint64
	lastBytesOut  uint64
	downloadSpeed uint64
	uploadSpeed   uint64
}

func NewTunnelManager(store *ConfigStore) *TunnelManager {
	tm := &TunnelManager{
		store:    store,
		state:    StateDisconnected,
		stateMsg: "Ready to connect",
		sshPool:  NewSSHPool(),
		vpn:      NewVPNController(),
		maxLogs:  150,
	}

	go tm.speedCalculatorLoop()
	return tm
}

func (tm *TunnelManager) logLocked(msg string) {
	timestamp := time.Now().Format("15:04:05")
	formatted := fmt.Sprintf("[%s] %s", timestamp, msg)
	tm.logs = append(tm.logs, formatted)
	if len(tm.logs) > tm.maxLogs {
		tm.logs = tm.logs[len(tm.logs)-tm.maxLogs:]
	}
	DebugLog("%s", formatted)
}

func (tm *TunnelManager) Log(msg string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.logLocked(msg)
}

func (tm *TunnelManager) setState(st TunnelState, msg string) {
	tm.mu.Lock()
	tm.state = st
	tm.stateMsg = msg
	tm.logLocked(fmt.Sprintf("%s: %s", st, msg))
	tm.mu.Unlock()
}

func (tm *TunnelManager) Start() error {
	tm.mu.Lock()
	if tm.state == StateConnected || tm.state == StateConnecting || tm.state == StateSSHAuth {
		tm.mu.Unlock()
		return fmt.Errorf("tunnel is already running or connecting")
	}
	tm.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	tm.cancelFn = cancel

	go tm.runTunnel(ctx)
	return nil
}

func (tm *TunnelManager) Stop() error {
	tm.mu.Lock()
	if tm.cancelFn != nil {
		tm.cancelFn()
		tm.cancelFn = nil
	}
	tm.mu.Unlock()

	tm.cleanup()
	tm.setState(StateDisconnected, "Tunnel stopped by user")
	return nil
}

func (tm *TunnelManager) runTunnel(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		cfg := tm.store.Get()
		if cfg.BugHost == "" || cfg.SSHHost == "" {
			tm.setState(StateError, "Bug Host and SSH Server must be configured")
			return
		}

		tm.setState(StateConnecting, fmt.Sprintf("Dialing bug host %s:%d...", cfg.BugHost, cfg.BugPort))

		// 1. Dial Bug Host & Inject Payload
		tm.setState(StateInjecting, "Injecting HTTP WebSocket payload...")
		stream, statusLine, err := DialPayload(ctx, cfg, tm.Log)
		if err != nil {
			tm.setState(StateError, err.Error())
			if !cfg.AutoReconnect {
				return
			}
			tm.waitRetry(ctx, 3*time.Second)
			continue
		}

		// 2. Perform SSH Handshake
		tm.setState(StateSSHAuth, fmt.Sprintf("Authenticating SSH user '%s'...", cfg.Username))
		sshClient, err := ConnectSSH(ctx, stream, cfg, tm.Log, func(connErr error) {
			tm.Log(fmt.Sprintf("Primary SSH tunnel dropped: %v", connErr))
			if cfg.AutoReconnect {
				tm.setState(StateReconnecting, "Connection dropped. Reconnecting...")
				go tm.reconnect(ctx)
			}
		})
		if err != nil {
			_ = stream.Close()
			tm.setState(StateError, err.Error())
			if !cfg.AutoReconnect {
				return
			}
			tm.waitRetry(ctx, 4*time.Second)
			continue
		}

		tm.mu.Lock()
		tm.sshPool.Close()
		tm.sshPool = NewSSHPool()
		tm.sshPool.Add(sshClient)
		tm.startedAt = time.Now()
		tm.mu.Unlock()

		// Background: establish parallel SSH connections if configured
		if cfg.SSHConcurrency > 1 {
			targetPool := cfg.SSHConcurrency
			go func() {
				for i := 2; i <= targetPool; i++ {
					select {
					case <-ctx.Done():
						return
					default:
					}
					tm.Log(fmt.Sprintf("Establishing parallel SSH stream %d/%d for high-speed throughput...", i, targetPool))
					pStream, _, pErr := DialPayload(ctx, cfg, func(string) {})
					if pErr != nil {
						tm.Log(fmt.Sprintf("Parallel stream %d dial failed: %v", i, pErr))
						continue
					}
					pClient, pErr := ConnectSSH(ctx, pStream, cfg, func(string) {}, func(err error) {
						tm.Log(fmt.Sprintf("Parallel stream %d dropped: %v", i, err))
					})
					if pErr != nil {
						_ = pStream.Close()
						tm.Log(fmt.Sprintf("Parallel stream %d auth failed: %v", i, pErr))
						continue
					}
					tm.sshPool.Add(pClient)
					tm.Log(fmt.Sprintf("✓ Parallel SSH stream %d/%d active! (Aggregated bandwidth)", i, targetPool))
				}
			}()
		}

		// 3. Start SOCKS5 Server if not already listening
		tm.mu.Lock()
		if tm.socksServer == nil {
			socks, err := NewSocksServer(cfg.SocksPort, func() (*SSHClient, error) {
				return tm.sshPool.Get()
			}, tm.Log)

			if err != nil {
				tm.mu.Unlock()
				tm.sshPool.Close()
				tm.setState(StateError, fmt.Sprintf("SOCKS5 bind error: %v", err))
				return
			}
			tm.socksServer = socks
			tm.logLocked(fmt.Sprintf("Local SOCKS5 proxy running on 127.0.0.1:%d", cfg.SocksPort))
		}
		tm.mu.Unlock()

		// 4. Windows System Proxy or VPN Mode
		if cfg.UseVPN {
			tm.Log("Starting L3 TUN/TAP VPN mode (NetMod tun2socks)...")
			if err := tm.vpn.Start(cfg.SocksPort, cfg.BugHost, tm.Log); err != nil {
				tm.Log(fmt.Sprintf("VPN start error: %v", err))
			} else {
				// Start high-speed local DNS forwarder on TAP IP (10.4.2.2:53) to eliminate UDP drop latency
				df, err := NewDNSForwarder("10.4.2.2:53", func() (*SSHClient, error) {
					return tm.sshPool.Get()
				}, tm.Log)
				if err == nil {
					tm.mu.Lock()
					tm.dnsForwarder = df
					tm.mu.Unlock()
					tm.Log("✓ Embedded DNS-over-TCP proxy active on 10.4.2.2:53 (0ms cached queries, zero UDP drops)")
				}
			}
		} else if cfg.AutoSetSystemProxy {
			if err := SetWindowsSystemProxy(cfg.SocksPort); err != nil {
				tm.Log(fmt.Sprintf("Warning: could not set Windows system proxy: %v", err))
			} else {
				tm.mu.Lock()
				tm.sysProxyOn = true
				tm.mu.Unlock()
				tm.Log("Windows System Proxy enabled automatically")
			}
		}

		tm.setState(StateConnected, fmt.Sprintf("Connected via %s (HTTP Status: %s)", cfg.BugHost, statusLine))
		return
	}
}

func (tm *TunnelManager) waitRetry(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

func (tm *TunnelManager) reconnect(ctx context.Context) {
	tm.mu.Lock()
	if tm.dnsForwarder != nil {
		_ = tm.dnsForwarder.Close()
		tm.dnsForwarder = nil
	}
	if tm.sshPool != nil {
		tm.sshPool.Close()
	}
	tm.mu.Unlock()

	tm.runTunnel(ctx)
}

func (tm *TunnelManager) cleanup() {
	// First stop VPN routes and tun2socks outside manager lock to prevent any deadlock
	if tm.vpn != nil {
		tm.vpn.Stop(tm.Log)
	}

	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tm.dnsForwarder != nil {
		_ = tm.dnsForwarder.Close()
		tm.dnsForwarder = nil
	}

	if tm.sshPool != nil {
		tm.sshPool.Close()
	}

	if tm.socksServer != nil {
		_ = tm.socksServer.Close()
		tm.socksServer = nil
	}

	if tm.sysProxyOn {
		_ = ClearWindowsSystemProxy()
		tm.sysProxyOn = false
		tm.logLocked("Windows System Proxy disabled")
	}
}

func (tm *TunnelManager) ToggleSystemProxy() (bool, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	cfg := tm.store.Get()
	if tm.sysProxyOn {
		if err := ClearWindowsSystemProxy(); err != nil {
			return true, err
		}
		tm.sysProxyOn = false
		tm.logLocked("Windows System Proxy turned OFF")
		return false, nil
	} else {
		if err := SetWindowsSystemProxy(cfg.SocksPort); err != nil {
			return false, err
		}
		tm.sysProxyOn = true
		tm.logLocked(fmt.Sprintf("Windows System Proxy turned ON (127.0.0.1:%d)", cfg.SocksPort))
		return true, nil
	}
}

func (tm *TunnelManager) GetTelemetry() Telemetry {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	cfg := tm.store.Get()
	tel := Telemetry{
		State:          tm.state,
		StateMessage:   tm.stateMsg,
		DownloadSpeed:  tm.downloadSpeed,
		UploadSpeed:    tm.uploadSpeed,
		SocksPort:      cfg.SocksPort,
		SysProxyActive: tm.sysProxyOn,
		VPNActive:      tm.vpn.IsActive(),
		Logs:           append([]string{}, tm.logs...),
	}

	if tm.state == StateConnected && !tm.startedAt.IsZero() {
		tel.UptimeSeconds = int64(time.Since(tm.startedAt).Seconds())
	}

	if tm.sshPool != nil {
		tel.PingMs = tm.sshPool.GetPingMs()
	}

	if tm.socksServer != nil {
		stats := tm.socksServer.Stats()
		tel.TotalBytesIn = stats.TotalBytesIn
		tel.TotalBytesOut = stats.TotalBytesOut
		tel.ActiveConns = stats.ActiveConns
	}

	return tel
}

func (tm *TunnelManager) speedCalculatorLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		tm.mu.Lock()
		if tm.socksServer != nil {
			stats := tm.socksServer.Stats()
			inDiff := stats.TotalBytesIn - tm.lastBytesIn
			outDiff := stats.TotalBytesOut - tm.lastBytesOut

			tm.downloadSpeed = inDiff
			tm.uploadSpeed = outDiff

			tm.lastBytesIn = stats.TotalBytesIn
			tm.lastBytesOut = stats.TotalBytesOut
		} else {
			tm.downloadSpeed = 0
			tm.uploadSpeed = 0
		}
		tm.mu.Unlock()
	}
}
