package tunnel

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

// PrefixedConn wraps a net.Conn with leftover initial bytes, then falls back to direct socket calls.
type PrefixedConn struct {
	net.Conn
	prefix []byte
}

func (p *PrefixedConn) Read(b []byte) (int, error) {
	if len(p.prefix) > 0 {
		n := copy(b, p.prefix)
		p.prefix = p.prefix[n:]
		return n, nil
	}
	return p.Conn.Read(b)
}

// DialPayload connects to the bug host, injects the HTTP payload, and validates response.
func DialPayload(ctx context.Context, cfg Config, logFn func(string)) (net.Conn, string, error) {
	if cfg.BugHost == "" {
		return nil, "", fmt.Errorf("bug host address is required")
	}
	if cfg.SSHHost == "" {
		return nil, "", fmt.Errorf("remote SSH server host is required")
	}

	targetBugAddr := net.JoinHostPort(cfg.BugHost, strconv.Itoa(cfg.BugPort))
	logFn(fmt.Sprintf("Connecting to Bug Host %s...", targetBugAddr))

	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	rawConn, err := dialer.DialContext(ctx, "tcp", targetBugAddr)
	if err != nil {
		return nil, "", fmt.Errorf("failed to connect to bug host %s: %w", targetBugAddr, err)
	}

	if tcp, ok := rawConn.(*net.TCPConn); ok {
		_ = tcp.SetNoDelay(true)
		_ = tcp.SetKeepAlive(true)
		_ = tcp.SetKeepAlivePeriod(15 * time.Second)
		_ = tcp.SetReadBuffer(32768)
		_ = tcp.SetWriteBuffer(16384)
	}

	var conn net.Conn = rawConn

	// If TLS is enabled (e.g. port 443 with SNI)
	if cfg.UseTLS && cfg.BugPort != 80 {
		sni := cfg.TLSSNI
		if sni == "" {
			sni = cfg.BugHost
		}
		logFn(fmt.Sprintf("Establishing TLS handshake (SNI: %s)...", sni))
		tlsConfig := &tls.Config{
			ServerName:         sni,
			InsecureSkipVerify: true, // Many CDNs/ISP fronts use self-signed or domain mismatches
		}
		tlsConn := tls.Client(rawConn, tlsConfig)
		if err := tlsConn.Handshake(); err != nil {
			_ = rawConn.Close()
			return nil, "", fmt.Errorf("TLS handshake failed with SNI %s: %w", sni, err)
		}
		conn = tlsConn
	} else if cfg.UseTLS && cfg.BugPort == 80 {
		logFn("Note: Bug Port is 80 (plaintext HTTP). Skipping TLS handshake.")
	}

	// Prepare and inject HTTP payload
	formattedPayload := FormatPayload(cfg.Payload, cfg.SSHHost, cfg.SSHPort)
	logFn("Injecting HTTP WebSocket payload...")

	// Handle packet splitting if configured
	chunks := SplitPayload(formattedPayload)
	for i, chunk := range chunks {
		if _, err := conn.Write([]byte(chunk)); err != nil {
			_ = conn.Close()
			return nil, "", fmt.Errorf("failed writing payload chunk %d: %w", i, err)
		}
		if len(chunks) > 1 && i < len(chunks)-1 {
			time.Sleep(50 * time.Millisecond) // micro-delay for anti-DPI packet boundary
		}
	}

	// Read and parse HTTP response
	reader := bufio.NewReader(conn)
	_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))

	statusLine, err := reader.ReadString('\n')
	if err != nil {
		_ = conn.Close()
		return nil, "", fmt.Errorf("failed reading HTTP status line: %w", err)
	}
	statusLine = strings.TrimSpace(statusLine)
	logFn(fmt.Sprintf("HTTP Response: %s", statusLine))

	// Validate status code
	parts := strings.Split(statusLine, " ")
	if len(parts) < 2 {
		_ = conn.Close()
		return nil, "", fmt.Errorf("invalid HTTP status line from proxy: %s", statusLine)
	}

	statusCode, err := strconv.Atoi(parts[1])
	if err != nil {
		_ = conn.Close()
		return nil, "", fmt.Errorf("unrecognized HTTP status code %s: %w", parts[1], err)
	}

	// Read until end of HTTP headers (\r\n\r\n or \n\n)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			_ = conn.Close()
			return nil, "", fmt.Errorf("failed reading response headers: %w", err)
		}
		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == "" {
			// Headers ended
			break
		}
	}

	// Clear read deadline for continuous SSH stream
	_ = conn.SetReadDeadline(time.Time{})

	// Check if status is acceptable (101 Switching Protocols, 200 OK / Connection established, or configured ExpectedStatus)
	isValid := false
	if statusCode == 101 || statusCode == 200 {
		isValid = true
	} else if cfg.ExpectedStatus > 0 && statusCode == cfg.ExpectedStatus {
		isValid = true
	}

	if !isValid {
		_ = conn.Close()
		return nil, statusLine, fmt.Errorf("unexpected HTTP status %d (expected 101 or 200): %s", statusCode, statusLine)
	}

	// Wrap connection with PrefixedConn only if reader buffered any excess bytes
	var stream net.Conn = conn
	if reader.Buffered() > 0 {
		buf := make([]byte, reader.Buffered())
		n, _ := reader.Read(buf)
		if n > 0 {
			stream = &PrefixedConn{
				Conn:   conn,
				prefix: buf[:n],
			}
		}
	}

	return stream, statusLine, nil
}
