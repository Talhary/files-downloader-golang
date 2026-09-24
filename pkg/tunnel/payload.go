package tunnel

import (
	"fmt"
	"strings"
)

const defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36"

// FormatPayload replaces NetMod / HTTP Custom style tags with real HTTP values.
func FormatPayload(rawPayload string, sshHost string, sshPort int) string {
	if rawPayload == "" {
		return ""
	}

	p := rawPayload

	// Format host and port tokens
	hostPort := fmt.Sprintf("%s:%d", sshHost, sshPort)
	if sshPort == 80 || sshPort == 443 {
		// When standard ports, many proxies prefer just the hostname in the Host header
		p = strings.ReplaceAll(p, "[host_port]", hostPort)
		p = strings.ReplaceAll(p, "[host]", sshHost)
	} else {
		p = strings.ReplaceAll(p, "[host_port]", hostPort)
		p = strings.ReplaceAll(p, "[host]", sshHost)
	}
	p = strings.ReplaceAll(p, "[port]", fmt.Sprintf("%d", sshPort))
	p = strings.ReplaceAll(p, "[ua]", defaultUserAgent)
	p = strings.ReplaceAll(p, "[protocol]", "HTTP/1.1")

	// Standard line terminator tags
	p = strings.ReplaceAll(p, "[crlf]", "\r\n")
	p = strings.ReplaceAll(p, "[cr]", "\r")
	p = strings.ReplaceAll(p, "[lf]", "\n")

	// Ensure the HTTP upgrade request headers end cleanly at \r\n\r\n.
	// Any accidental characters typed after \r\n\r\n would bleed directly
	// into the raw SSH stream and cause SSH handshake failure!
	if idx := strings.Index(p, "\r\n\r\n"); idx != -1 {
		p = p[:idx+4]
	}

	return p
}

// SplitPayload checks if the payload has [split] delimiter for packet chunking / anti-DPI.
func SplitPayload(formattedPayload string) []string {
	if strings.Contains(formattedPayload, "[split]") {
		parts := strings.Split(formattedPayload, "[split]")
		var result []string
		for _, part := range parts {
			if len(part) > 0 {
				result = append(result, part)
			}
		}
		return result
	}
	return []string{formattedPayload}
}
