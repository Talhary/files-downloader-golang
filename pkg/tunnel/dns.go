package tunnel

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// dnsCacheEntry holds cached DNS response payload and expiry time.
type dnsCacheEntry struct {
	data      []byte
	expiresAt time.Time
}

// DNSForwarder runs a local UDP DNS listener that forwards DNS queries
// over SSH TCP (RFC 7766) to 1.1.1.1:53 or 8.8.8.8:53.
// This resolves the badvpn-tun2socks UDP drop issue and eliminates
// the 1-2 second Windows DNS timeout on every new domain.
type DNSForwarder struct {
	udpConn     *net.UDPConn
	sshProvider func() (*SSHClient, error)
	logFn       func(string)
	closed      atomic.Bool

	cacheMu sync.RWMutex
	cache   map[string]dnsCacheEntry
}

// NewDNSForwarder starts listening on UDP bindAddr (e.g. "10.4.2.2:53" or "127.0.0.1:53").
func NewDNSForwarder(bindAddr string, sshProvider func() (*SSHClient, error), logFn func(string)) (*DNSForwarder, error) {
	addr, err := net.ResolveUDPAddr("udp4", bindAddr)
	if err != nil {
		return nil, fmt.Errorf("invalid DNS bind address %s: %w", bindAddr, err)
	}

	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to bind UDP DNS listener on %s: %w", bindAddr, err)
	}

	df := &DNSForwarder{
		udpConn:     conn,
		sshProvider: sshProvider,
		logFn:       logFn,
		cache:       make(map[string]dnsCacheEntry),
	}

	go df.serveLoop()
	return df, nil
}

// Close terminates the DNS forwarder.
func (df *DNSForwarder) Close() error {
	if df.closed.Swap(true) {
		return nil
	}
	return df.udpConn.Close()
}

func (df *DNSForwarder) serveLoop() {
	buf := make([]byte, 2048)
	for {
		n, clientAddr, err := df.udpConn.ReadFromUDP(buf)
		if err != nil {
			if df.closed.Load() {
				return
			}
			continue
		}

		if n < 12 { // DNS header minimum length is 12 bytes
			continue
		}

		query := make([]byte, n)
		copy(query, buf[:n])

		go df.handleQuery(clientAddr, query)
	}
}

func (df *DNSForwarder) handleQuery(clientAddr *net.UDPAddr, query []byte) {
	txID := binary.BigEndian.Uint16(query[:2])
	cacheKey := string(query[12:]) // Questions payload as cache key

	// 1. Check in-memory DNS cache
	df.cacheMu.RLock()
	entry, found := df.cache[cacheKey]
	df.cacheMu.RUnlock()

	if found && time.Now().Before(entry.expiresAt) {
		resp := make([]byte, len(entry.data))
		copy(resp, entry.data)
		// Overwrite transaction ID to match client query
		binary.BigEndian.PutUint16(resp[:2], txID)
		_, _ = df.udpConn.WriteToUDP(resp, clientAddr)
		return
	}

	// 2. Obtain active SSH client
	sshClient, err := df.sshProvider()
	if err != nil || sshClient == nil {
		return
	}

	// 3. Query DNS server over SSH TCP (RFC 7766 DNS-over-TCP)
	// Cloudflare (1.1.1.1) and Google (8.8.8.8) support standard DNS over TCP on port 53
	tcpConn, err := sshClient.Dial("tcp", "1.1.1.1:53")
	if err != nil {
		// Fallback to Google DNS
		tcpConn, err = sshClient.Dial("tcp", "8.8.8.8:53")
		if err != nil {
			return
		}
	}
	defer tcpConn.Close()

	_ = tcpConn.SetDeadline(time.Now().Add(4 * time.Second))

	// TCP DNS frame format: 2-byte big-endian length prefix + wire DNS query
	reqFrame := make([]byte, 2+len(query))
	binary.BigEndian.PutUint16(reqFrame[:2], uint16(len(query)))
	copy(reqFrame[2:], query)

	if _, err := tcpConn.Write(reqFrame); err != nil {
		return
	}

	// Read 2-byte response length
	lenBuf := make([]byte, 2)
	if _, err := io.ReadFull(tcpConn, lenBuf); err != nil {
		return
	}
	respLen := binary.BigEndian.Uint16(lenBuf)
	if respLen == 0 || respLen > 4096 {
		return
	}

	respBuf := make([]byte, respLen)
	if _, err := io.ReadFull(tcpConn, respBuf); err != nil {
		return
	}

	// Send UDP response to client
	_, _ = df.udpConn.WriteToUDP(respBuf, clientAddr)

	// Cache successful response for 60 seconds
	df.cacheMu.Lock()
	if len(df.cache) > 2000 {
		// Simple map prune when cache grows large
		df.cache = make(map[string]dnsCacheEntry)
	}
	df.cache[cacheKey] = dnsCacheEntry{
		data:      respBuf,
		expiresAt: time.Now().Add(60 * time.Second),
	}
	df.cacheMu.Unlock()
}
