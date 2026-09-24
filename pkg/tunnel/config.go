package tunnel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Config represents the SSH over WebSocket / HTTP Injection tunnel settings.
type Config struct {
	// Bug Host (Carrier zero-rated / fronting host or IP)
	BugHost string `json:"bug_host"`
	BugPort int    `json:"bug_port"`
	UseTLS  bool   `json:"use_tls"` // True for port 443 with TLS SNI spoofing
	TLSSNI  string `json:"tls_sni"`  // Optional custom SNI, defaults to BugHost

	// Remote SSH Server destination
	SSHHost string `json:"ssh_host"`
	SSHPort int    `json:"ssh_port"`

	// Credentials (never hardcoded in source)
	Username string `json:"username"`
	Password string `json:"password"`

	// HTTP Injection Payload
	// Supports standard injector tags: [crlf], [host], [port], [host_port], [ua], [protocol]
	Payload string `json:"payload"`

	// Expected response status code (e.g. 101 or 200)
	ExpectedStatus int `json:"expected_status"`

	// Local Proxy & Routing Settings
	SocksPort          int  `json:"socks_port"`
	UseVPN             bool `json:"use_vpn"`             // Use TAP-Windows + badvpn-tun2socks L3 VPN (NetMod style)
	AutoSetSystemProxy bool `json:"auto_set_system_proxy"`
	AutoReconnect      bool `json:"auto_reconnect"`
	DisableTCPDelay    bool `json:"disable_tcp_delay"`    // TCP_NODELAY (NetMod style low-latency mode)
	KeepAliveInterval  int  `json:"keepalive_interval_sec"` // in seconds, default 15
}

// DefaultConfig returns clean, safe default parameters without any credentials.
func DefaultConfig() Config {
	return Config{
		BugHost:            "",
		BugPort:            80,
		UseTLS:             false,
		TLSSNI:             "",
		SSHHost:            "",
		SSHPort:            80,
		Username:           "",
		Password:           "",
		Payload:            "GET / HTTP/1.1[crlf]Host: [host][crlf]Upgrade: websocket[crlf]Connection: Upgrade[crlf][crlf]",
		ExpectedStatus:     101,
		SocksPort:          10808,
		UseVPN:             true,
		AutoSetSystemProxy: false,
		AutoReconnect:      true,
		DisableTCPDelay:    true,
		KeepAliveInterval:  15,
	}
}

// ConfigStore handles thread-safe loading and persisting of tunnel configuration.
type ConfigStore struct {
	mu       sync.RWMutex
	filePath string
	current  Config
}

// NewConfigStore creates a store with a given file path.
func NewConfigStore(path string) (*ConfigStore, error) {
	if path == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			homeDir = "."
		}
		path = filepath.Join(homeDir, ".dlengine", "tunnel_config.json")
	}

	cs := &ConfigStore{
		filePath: path,
		current:  DefaultConfig(),
	}

	// Try loading existing file if present
	_ = cs.Load()
	return cs, nil
}

// Get returns a copy of current configuration.
func (cs *ConfigStore) Get() Config {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.current
}

// Update updates the in-memory config and saves to disk.
func (cs *ConfigStore) Update(cfg Config) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	// Enforce sane defaults
	if cfg.BugPort <= 0 {
		cfg.BugPort = 80
	}
	if cfg.SSHPort <= 0 {
		cfg.SSHPort = 80
	}
	if cfg.SocksPort <= 0 {
		cfg.SocksPort = 10808
	}
	if cfg.KeepAliveInterval <= 0 {
		cfg.KeepAliveInterval = 15
	}
	if cfg.Payload == "" {
		cfg.Payload = DefaultConfig().Payload
	}

	cs.current = cfg
	return cs.saveLocked()
}

// Load loads config from disk.
func (cs *ConfigStore) Load() error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	data, err := os.ReadFile(cs.filePath)
	if err != nil {
		return err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return err
	}

	cs.current = cfg
	return nil
}

// saveLocked writes the config to disk with restricted permissions.
func (cs *ConfigStore) saveLocked() error {
	dir := filepath.Dir(cs.filePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cs.current, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(cs.filePath, data, 0600)
}
