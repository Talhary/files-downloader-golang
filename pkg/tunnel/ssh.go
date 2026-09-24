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

// keepAliveLoop sends non-intrusive keepalive pings to maintain carrier NAT state without injecting latency spikes.
func (sc *SSHClient) keepAliveLoop(interval time.Duration, logFn func(string)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		<-ticker.C
		if sc.closed.Load() {
			return
		}

		// Only send keepalive when the tunnel is completely idle
		lastActive := sc.lastActive.Load()
		if lastActive > 0 && time.Since(time.Unix(lastActive, 0)) < 20*time.Second {
			continue
		}

		go func() {
			if sc.closed.Load() {
				return
			}
			// Send asynchronous keepalive to maintain carrier NAT mapping without stalling user data channels
			_, _, _ = sc.client.SendRequest("keepalive@openssh.com", false, nil)
		}()
	}
}
