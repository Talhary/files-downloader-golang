package engine

import (
	"crypto/tls"
	"net"
	"net/http"
	"time"
)

// Version is the current release version of the Download Engine.
const Version = "1.1.0"

// Default settings
const (
	DefaultConcurrency      = 8
	DefaultChunkSize        = 8 * 1024 * 1024 // 8 MB
	DefaultMaxRetries       = 5
	DefaultRetryDelay       = 1 * time.Second
	DefaultProgressInterval = 200 * time.Millisecond
	DefaultCopyBufferSize   = 64 * 1024 // 64 KB
	DefaultStreamPrefetch   = 4         // Number of chunks prefetched in memory
	DefaultUserAgent        = "DownloadEngine/1.1 (+https://github.com/download-engine)"
	DefaultConnectTimeout   = 15 * time.Second
	DefaultLinkTimeout      = 30 * time.Second // HTTP response header timeout
	DefaultIdleTimeout      = 30 * time.Second // Read stall timeout per chunk
	DefaultProbeTimeout     = 15 * time.Second
)

// ProgressSnapshot contains a snapshot of download progress at a given moment.
type ProgressSnapshot struct {
	TotalBytes      int64         // Total size of the file in bytes (0 if unknown)
	DownloadedBytes int64         // Total bytes downloaded so far
	Percent         float64       // Progress percentage (0 - 100)
	SpeedBytesPerSec float64      // Current smoothed download speed in bytes/sec
	ETA             time.Duration // Estimated time remaining
	Elapsed         time.Duration // Total elapsed time
	ActiveWorkers   int           // Number of active workers currently downloading
	TotalChunks     int           // Total number of chunks
	CompletedChunks int           // Number of completed chunks
	Done            bool          // True if download completed
	Err             error         // Error if download failed
}

// ProgressCallback is invoked periodically with progress snapshots.
type ProgressCallback func(snapshot ProgressSnapshot)

// Options holds configuration for the download engine.
type Options struct {
	Concurrency      int
	ChunkSize        int64
	MaxRetries       int
	RetryDelay       time.Duration
	Timeout          time.Duration // Overall operation timeout (0 = unlimited)
	ConnectTimeout   time.Duration // TCP connect and TLS handshake timeout
	LinkTimeout      time.Duration // HTTP response header timeout
	IdleTimeout      time.Duration // Per-read stall timeout during chunk downloading
	ProbeTimeout     time.Duration // Probe metadata timeout
	Insecure         bool          // Allow insecure TLS certificates (InsecureSkipVerify)
	HTTPClient       *http.Client
	Headers          map[string]string
	UserAgent        string
	CopyBufferSize   int
	StreamPrefetch   int
	ProgressFunc     ProgressCallback
	ProgressInterval time.Duration
	AutoRename       bool // Auto-generate filename if destination is a directory
}

// Option is a functional option for configuring the engine.
type Option func(*Options)

// DefaultOptions returns a new Options struct initialized with robust defaults.
func DefaultOptions() *Options {
	opts := &Options{
		Concurrency:      DefaultConcurrency,
		ChunkSize:        DefaultChunkSize,
		MaxRetries:       DefaultMaxRetries,
		RetryDelay:       DefaultRetryDelay,
		Timeout:          0,
		ConnectTimeout:   DefaultConnectTimeout,
		LinkTimeout:      DefaultLinkTimeout,
		IdleTimeout:      DefaultIdleTimeout,
		ProbeTimeout:     DefaultProbeTimeout,
		Insecure:         false,
		Headers:          make(map[string]string),
		UserAgent:        DefaultUserAgent,
		CopyBufferSize:   DefaultCopyBufferSize,
		StreamPrefetch:   DefaultStreamPrefetch,
		ProgressInterval: DefaultProgressInterval,
		AutoRename:       true,
	}
	opts.ReconfigureTransport()
	return opts
}

// ReconfigureTransport synchronizes HTTPClient and its Transport with current Options timeouts and TLS settings.
func (o *Options) ReconfigureTransport() {
	if o.HTTPClient == nil {
		o.HTTPClient = &http.Client{Timeout: 0}
	}
	tr, ok := o.HTTPClient.Transport.(*http.Transport)
	if !ok || tr == nil {
		tr = &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   32,
			IdleConnTimeout:       90 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			DisableCompression:    true,
			ForceAttemptHTTP2:     true,
		}
		o.HTTPClient.Transport = tr
	}

	connTimeout := o.ConnectTimeout
	if connTimeout <= 0 {
		connTimeout = DefaultConnectTimeout
	}
	tr.DialContext = (&net.Dialer{
		Timeout:   connTimeout,
		KeepAlive: 30 * time.Second,
	}).DialContext
	tr.TLSHandshakeTimeout = connTimeout

	linkTimeout := o.LinkTimeout
	if linkTimeout <= 0 {
		linkTimeout = DefaultLinkTimeout
	}
	tr.ResponseHeaderTimeout = linkTimeout

	if tr.TLSClientConfig == nil {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: o.Insecure}
	} else {
		tr.TLSClientConfig.InsecureSkipVerify = o.Insecure
	}
}


// WithConcurrency sets the number of parallel download workers.
func WithConcurrency(workers int) Option {
	return func(o *Options) {
		if workers > 0 {
			o.Concurrency = workers
		}
	}
}

// WithChunkSize sets the chunk/part size in bytes.
func WithChunkSize(bytes int64) Option {
	return func(o *Options) {
		if bytes > 0 {
			o.ChunkSize = bytes
		}
	}
}

// WithMaxRetries sets the maximum number of retries per chunk.
func WithMaxRetries(retries int) Option {
	return func(o *Options) {
		if retries >= 0 {
			o.MaxRetries = retries
		}
	}
}

// WithRetryDelay sets the initial delay before retrying a failed chunk.
func WithRetryDelay(d time.Duration) Option {
	return func(o *Options) {
		if d > 0 {
			o.RetryDelay = d
		}
	}
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(client *http.Client) Option {
	return func(o *Options) {
		if client != nil {
			o.HTTPClient = client
		}
	}
}

// WithHeader sets an HTTP request header.
func WithHeader(key, value string) Option {
	return func(o *Options) {
		o.Headers[key] = value
	}
}

// WithHeaders sets multiple HTTP request headers.
func WithHeaders(headers map[string]string) Option {
	return func(o *Options) {
		for k, v := range headers {
			o.Headers[k] = v
		}
	}
}

// WithUserAgent sets a custom User-Agent string.
func WithUserAgent(ua string) Option {
	return func(o *Options) {
		if ua != "" {
			o.UserAgent = ua
		}
	}
}

// WithProgressCallback sets the callback for tracking progress.
func WithProgressCallback(cb ProgressCallback, interval time.Duration) Option {
	return func(o *Options) {
		o.ProgressFunc = cb
		if interval > 0 {
			o.ProgressInterval = interval
		}
	}
}

// WithStreamPrefetch sets the maximum number of chunks kept in memory ahead of the reader.
func WithStreamPrefetch(prefetch int) Option {
	return func(o *Options) {
		if prefetch > 0 {
			o.StreamPrefetch = prefetch
		}
	}
}

// WithCopyBufferSize sets the buffer size used for streaming I/O.
func WithCopyBufferSize(bytes int) Option {
	return func(o *Options) {
		if bytes > 0 {
			o.CopyBufferSize = bytes
		}
	}
}

// WithTimeout sets the overall operation timeout (0 = unlimited).
func WithTimeout(d time.Duration) Option {
	return func(o *Options) {
		o.Timeout = d
	}
}

// WithConnectTimeout sets the TCP dial and TLS handshake timeout.
func WithConnectTimeout(d time.Duration) Option {
	return func(o *Options) {
		if d > 0 {
			o.ConnectTimeout = d
			o.ReconfigureTransport()
		}
	}
}

// WithLinkTimeout sets the HTTP response header timeout (wait time for link/server response).
func WithLinkTimeout(d time.Duration) Option {
	return func(o *Options) {
		if d > 0 {
			o.LinkTimeout = d
			o.ReconfigureTransport()
		}
	}
}

// WithIdleTimeout sets the per-read idle stall timeout during chunk downloads.
func WithIdleTimeout(d time.Duration) Option {
	return func(o *Options) {
		if d > 0 {
			o.IdleTimeout = d
		}
	}
}

// WithProbeTimeout sets the timeout for probing remote file metadata.
func WithProbeTimeout(d time.Duration) Option {
	return func(o *Options) {
		if d > 0 {
			o.ProbeTimeout = d
		}
	}
}

// WithInsecure controls whether TLS certificate verification is skipped.
func WithInsecure(insecure bool) Option {
	return func(o *Options) {
		o.Insecure = insecure
		o.ReconfigureTransport()
	}
}

