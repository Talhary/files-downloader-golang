package tunnel

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// SocksStats tracks traffic throughput and connection counters.
type SocksStats struct {
	TotalBytesIn  uint64
	TotalBytesOut uint64
	ActiveConns   int64
}

var pipeBufferPool = sync.Pool{
	New: func() interface{} {
		b := make([]byte, 32*1024)
		return &b
	},
}

// SocksServer is a high-speed SOCKS5 proxy server.
type SocksServer struct {
	listener    net.Listener
	sshProvider func() (*SSHClient, error)
	logFn       func(string)
	closed      atomic.Bool
	wg          sync.WaitGroup

	bytesIn     atomic.Uint64
	bytesOut    atomic.Uint64
	activeConns atomic.Int64

	connsMu sync.Mutex
	conns   map[net.Conn]struct{}
}

// NewSocksServer initializes the SOCKS5 server on the given local port.
func NewSocksServer(port int, sshProvider func() (*SSHClient, error), logFn func(string)) (*SocksServer, error) {
	bindAddr := fmt.Sprintf("127.0.0.1:%d", port)
	l, err := net.Listen("tcp", bindAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to bind SOCKS5 listener on %s: %w", bindAddr, err)
	}

	s := &SocksServer{
		listener:    l,
		sshProvider: sshProvider,
		logFn:       logFn,
		conns:       make(map[net.Conn]struct{}),
	}

	go s.acceptLoop()
	return s, nil
}

// Stats returns current snapshot of traffic metrics.
func (s *SocksServer) Stats() SocksStats {
	return SocksStats{
		TotalBytesIn:  s.bytesIn.Load(),
		TotalBytesOut: s.bytesOut.Load(),
		ActiveConns:   s.activeConns.Load(),
	}
}

// Close stops the SOCKS5 server.
func (s *SocksServer) Close() error {
	if s.closed.Swap(true) {
		return nil
	}
	err := s.listener.Close()

	// Close all active client connections so goroutines unblock immediately
	s.connsMu.Lock()
	for c := range s.conns {
		_ = c.Close()
	}
	s.conns = make(map[net.Conn]struct{})
	s.connsMu.Unlock()

	return err
}

func (s *SocksServer) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if s.closed.Load() {
				return
			}
			continue
		}

		s.connsMu.Lock()
		if s.closed.Load() {
			s.connsMu.Unlock()
			_ = conn.Close()
			return
		}
		s.conns[conn] = struct{}{}
		s.connsMu.Unlock()

		s.wg.Add(1)
		s.activeConns.Add(1)
		go func(c net.Conn) {
			defer func() {
				s.connsMu.Lock()
				delete(s.conns, c)
				s.connsMu.Unlock()
				s.wg.Done()
				s.activeConns.Add(-1)
				_ = c.Close()
			}()
			s.handleConnection(c)
		}(conn)
	}
}

func (s *SocksServer) handleConnection(localConn net.Conn) {
	if tcp, ok := localConn.(*net.TCPConn); ok {
		_ = tcp.SetNoDelay(true)
		_ = tcp.SetKeepAlive(true)
		_ = tcp.SetKeepAlivePeriod(15 * time.Second)
		_ = tcp.SetReadBuffer(32768)
		_ = tcp.SetWriteBuffer(16384)
	}

	// Set initial handshake deadline
	_ = localConn.SetDeadline(time.Now().Add(10 * time.Second))

	// 1. SOCKS5 Method Negotiation
	header := make([]byte, 2)
	if _, err := io.ReadFull(localConn, header); err != nil {
		return
	}

	if header[0] != 0x05 { // SOCKS version 5
		return
	}

	numMethods := int(header[1])
	methods := make([]byte, numMethods)
	if _, err := io.ReadFull(localConn, methods); err != nil {
		return
	}

	// Respond with NO AUTHENTICATION REQUIRED (0x00)
	if _, err := localConn.Write([]byte{0x05, 0x00}); err != nil {
		return
	}

	// 2. SOCKS5 Request
	reqHeader := make([]byte, 4)
	if _, err := io.ReadFull(localConn, reqHeader); err != nil {
		return
	}

	if reqHeader[0] != 0x05 || reqHeader[1] != 0x01 { // CMD 0x01 = CONNECT
		// Command not supported
		_, _ = localConn.Write([]byte{0x05, 0x07, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}

	var targetHost string
	addrType := reqHeader[3]

	switch addrType {
	case 0x01: // IPv4
		ipBuf := make([]byte, 4)
		if _, err := io.ReadFull(localConn, ipBuf); err != nil {
			return
		}
		targetHost = net.IP(ipBuf).String()

	case 0x03: // Domain name (Remote DNS resolution)
		lenBuf := make([]byte, 1)
		if _, err := io.ReadFull(localConn, lenBuf); err != nil {
			return
		}
		domainLen := int(lenBuf[0])
		domainBuf := make([]byte, domainLen)
		if _, err := io.ReadFull(localConn, domainBuf); err != nil {
			return
		}
		targetHost = string(domainBuf)

	case 0x04: // IPv6
		ipBuf := make([]byte, 16)
		if _, err := io.ReadFull(localConn, ipBuf); err != nil {
			return
		}
		targetHost = net.IP(ipBuf).String()

	default:
		_, _ = localConn.Write([]byte{0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}

	portBuf := make([]byte, 2)
	if _, err := io.ReadFull(localConn, portBuf); err != nil {
		return
	}
	targetPort := binary.BigEndian.Uint16(portBuf)
	targetAddr := net.JoinHostPort(targetHost, strconv.Itoa(int(targetPort)))

	// Obtain current SSH client
	sshClient, err := s.sshProvider()
	if err != nil || sshClient == nil {
		// Host unreachable / proxy server failure
		_, _ = localConn.Write([]byte{0x05, 0x04, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}

	// Clear deadlines for data transfer
	_ = localConn.SetDeadline(time.Time{})

	// Dial destination over SSH
	remoteConn, err := sshClient.Dial("tcp", targetAddr)
	if err != nil {
		_, _ = localConn.Write([]byte{0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0}) // Connection refused
		return
	}
	defer remoteConn.Close()

	// Respond with SOCKS5 SUCCESS (0x00)
	// BND.ADDR 0.0.0.0:0
	reply := []byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}
	if _, err := localConn.Write(reply); err != nil {
		return
	}

	type closeWriter interface {
		CloseWrite() error
	}

	// 3. Bi-directional stream forwarding with metric counters & graceful half-close
	var pipeWg sync.WaitGroup
	pipeWg.Add(2)

	go func() {
		defer pipeWg.Done()
		bufPtr := pipeBufferPool.Get().(*[]byte)
		buf := *bufPtr
		defer pipeBufferPool.Put(bufPtr)

		for {
			n, err := localConn.Read(buf)
			if n > 0 {
				s.bytesOut.Add(uint64(n))
				sshClient.Touch()
				if _, werr := remoteConn.Write(buf[:n]); werr != nil {
					break
				}
			}
			if err != nil {
				break
			}
		}
		// Signal to remote that local is done writing, but allow remote to finish sending
		if cw, ok := remoteConn.(closeWriter); ok {
			_ = cw.CloseWrite()
		} else {
			_ = remoteConn.Close()
		}
	}()

	go func() {
		defer pipeWg.Done()
		bufPtr := pipeBufferPool.Get().(*[]byte)
		buf := *bufPtr
		defer pipeBufferPool.Put(bufPtr)

		for {
			n, err := remoteConn.Read(buf)
			if n > 0 {
				s.bytesIn.Add(uint64(n))
				sshClient.Touch()
				if _, werr := localConn.Write(buf[:n]); werr != nil {
					break
				}
			}
			if err != nil {
				break
			}
		}
		// Signal to local that remote is done writing
		if cw, ok := localConn.(closeWriter); ok {
			_ = cw.CloseWrite()
		} else {
			_ = localConn.Close()
		}
	}()

	pipeWg.Wait()
	_ = localConn.Close()
	_ = remoteConn.Close()
}
