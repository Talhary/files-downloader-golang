package engine

import (
	"crypto/tls"
	"net"
	"net/http"
	"time"
)

// Default settings
const (
	DefaultConcurrency      = 8
	DefaultChunkSize        = 8 * 1024 * 1024 // 8 MB
	DefaultMaxRetries       = 5
	DefaultRetryDelay       = 1 * time.Second
	DefaultProgressInterval = 200 * time.Millisecond
	DefaultCopyBufferSize   = 64 * 1024 // 64 KB
	DefaultStreamPrefetch   = 4         // Number of chunks prefetched in memory
	DefaultUserAgent        = "DownloadEngine/1.0 (+https://github.com/download-engine)"
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
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   32,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: false},
		DisableCompression:    true, // Prevent automatic gzip on large binary archives
		ForceAttemptHTTP2:     true,
	}

	return &Options{
		Concurrency:      DefaultConcurrency,
		ChunkSize:        DefaultChunkSize,
		MaxRetries:       DefaultMaxRetries,
		RetryDelay:       DefaultRetryDelay,
		HTTPClient:       &http.Client{Transport: transport, Timeout: 0}, // 0 timeout for stream/chunk longevity
		Headers:          make(map[string]string),
		UserAgent:        DefaultUserAgent,
		CopyBufferSize:   DefaultCopyBufferSize,
		StreamPrefetch:   DefaultStreamPrefetch,
		ProgressInterval: DefaultProgressInterval,
		AutoRename:       true,
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
