package tunnel

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"
)

// SSHClient wraps an authenticated SSH connection with health monitoring.
type SSHClient struct {
	client    *ssh.Client
	conn      net.Conn
	mu        sync.RWMutex
	closed     atomic.Bool
	lastPing   int64 // ping round-trip time in milliseconds
	lastActive atomic.Int64
	onFailure  func(err error)
}

// Touch records that data was actively transferred, preventing unnecessary keepalive stalls.
func (sc *SSHClient) Touch() {
	sc.lastActive.Store(time.Now().Unix())
}

// ConnectSSH handshakes SSH over an existing stream (upgraded WebSocket conn).
func ConnectSSH(ctx context.Context, stream net.Conn, cfg Config, logFn func(string), onFailure func(err error)) (*SSHClient, error) {
	logFn(fmt.Sprintf("Starting SSH handshake for user '%s'...", cfg.Username))

	authMethods := []ssh.AuthMethod{}
	if cfg.Password != "" {
		authMethods = append(authMethods, ssh.Password(cfg.Password))
	} else {
		// Try empty password or keyboard-interactive if password omitted
		authMethods = append(authMethods, ssh.Password(""))
	}

	sshConfig := &ssh.ClientConfig{
		User:            cfg.Username,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
		ClientVersion:   "SSH-2.0-OpenSSH_9.6p1",
		Config: ssh.Config{
			// Prioritize hardware-accelerated AEAD ciphers for maximum throughput & low CPU latency
			Ciphers: []string{
				"aes128-gcm@openssh.com",
				"chacha20-poly1305@openssh.com",
				"aes256-gcm@openssh.com",
				"aes128-ctr",
				"aes192-ctr",
				"aes256-ctr",
			},
			// Modern fast elliptic curves for faster handshake
			KeyExchanges: []string{
				"curve25519-sha256",
				"curve25519-sha256@libssh.org",
				"ecdh-sha2-nistp256",
				"ecdh-sha2-nistp384",
				"diffie-hellman-group14-sha256",
			},
		},
	}

	targetAddr := fmt.Sprintf("%s:%d", cfg.SSHHost, cfg.SSHPort)
	DebugLog("ConnectSSH: Calling ssh.NewClientConn to %s...", targetAddr)
	c, chans, reqs, err := ssh.NewClientConn(stream, targetAddr, sshConfig)
	DebugLog("ConnectSSH: ssh.NewClientConn returned, err=%v", err)
	if err != nil {
		_ = stream.Close()
		return nil, fmt.Errorf("SSH handshake failed: %w", err)
	}

	client := ssh.NewClient(c, chans, reqs)
	DebugLog("ConnectSSH: ssh.NewClient created successfully")
	logFn("SSH Authentication successful! Secure tunnel established.")

	sc := &SSHClient{
		client:    client,
		conn:      stream,
		onFailure: onFailure,
	}

	// Start background keepalive ping loop
	interval := time.Duration(cfg.KeepAliveInterval) * time.Second
	if interval < 5*time.Second {
		interval = 10 * time.Second
	}
	go sc.keepAliveLoop(interval, logFn)

	return sc, nil
}

// Dial opens a remote TCP connection through the SSH tunnel.
func (sc *SSHClient) Dial(network, address string) (net.Conn, error) {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	if sc.closed.Load() || sc.client == nil {
		return nil, fmt.Errorf("SSH client is disconnected")
	}

	return sc.client.Dial(network, address)
}

// Close terminates the SSH client and underlying socket.
func (sc *SSHClient) Close() error {
	if sc.closed.Swap(true) {
		return nil
	}

	sc.mu.Lock()
	defer sc.mu.Unlock()

	var err error
	if sc.client != nil {
		err = sc.client.Close()
	}
	if sc.conn != nil {
		_ = sc.conn.Close()
	}
	return err
}

// GetPingMs returns the last measured keepalive ping latency in milliseconds.
func (sc *SSHClient) GetPingMs() int64 {
	return atomic.LoadInt64(&sc.lastPing)
}

// keepAliveLoop sends non-intrusive keepalive pings to maintain carrier NAT state and measures real RTT.
func (sc *SSHClient) keepAliveLoop(interval time.Duration, logFn func(string)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Initial ping measurement after 1 second
	go func() {
		time.Sleep(1 * time.Second)
		if !sc.closed.Load() && sc.client != nil {
			t0 := time.Now()
			_, _, err := sc.client.SendRequest("keepalive@openssh.com", true, nil)
			if err == nil {
				atomic.StoreInt64(&sc.lastPing, time.Since(t0).Milliseconds())
			}
		}
	}()

	for {
		<-ticker.C
		if sc.closed.Load() {
			return
		}

		go func() {
			if sc.closed.Load() || sc.client == nil {
				return
			}
			t0 := time.Now()
			// Send request with reply in background goroutine to measure true latency without blocking channels
			_, _, err := sc.client.SendRequest("keepalive@openssh.com", true, nil)
			if err == nil {
				rtt := time.Since(t0).Milliseconds()
				atomic.StoreInt64(&sc.lastPing, rtt)
			}
		}()
	}
}

// SSHPool manages multiple concurrent SSH connections to eliminate single-stream TCP head-of-line blocking.
type SSHPool struct {
	mu      sync.RWMutex
	clients []*SSHClient
	idx     atomic.Uint64
}

// NewSSHPool creates an empty pool of SSH clients.
func NewSSHPool() *SSHPool {
	return &SSHPool{
		clients: make([]*SSHClient, 0, 4),
	}
}

// Add adds an active SSH client to the pool.
func (p *SSHPool) Add(c *SSHClient) {
	if c == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clients = append(p.clients, c)
}

// Get selects an active SSH client using round-robin distribution.
func (p *SSHPool) Get() (*SSHClient, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	active := make([]*SSHClient, 0, len(p.clients))
	for _, c := range p.clients {
		if !c.closed.Load() {
			active = append(active, c)
		}
	}

	if len(active) == 0 {
		return nil, fmt.Errorf("no active SSH connections in pool")
	}

	n := p.idx.Add(1)
	return active[n%uint64(len(active))], nil
}

// Close terminates all SSH connections in the pool.
func (p *SSHPool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, c := range p.clients {
		_ = c.Close()
	}
	p.clients = nil
}

// ActiveCount returns number of connected SSH tunnels.
func (p *SSHPool) ActiveCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	count := 0
	for _, c := range p.clients {
		if !c.closed.Load() {
			count++
		}
	}
	return count
}

// GetPingMs returns the lowest ping from active clients in the pool.
func (p *SSHPool) GetPingMs() int64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	var lowest int64 = 0
	for _, c := range p.clients {
		if !c.closed.Load() {
			ping := c.GetPingMs()
			if ping > 0 && (lowest == 0 || ping < lowest) {
				lowest = ping
			}
		}
	}
	return lowest
}
