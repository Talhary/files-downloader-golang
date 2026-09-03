package engine

import "context"

// Engine represents the multi-part concurrent download and streaming engine.
type Engine struct {
	opts *Options
}

// New creates a new download Engine with the specified functional options.
func New(opts ...Option) *Engine {
	options := DefaultOptions()
	for _, opt := range opts {
		if opt != nil {
			opt(options)
		}
	}
	options.ReconfigureTransport()
	return &Engine{opts: options}
}

// Options returns a copy of current engine options.
func (e *Engine) Options() *Options {
	return e.opts
}

// Probe inspects a remote URL to fetch metadata (Content-Length, Accept-Ranges, Filename, etc.).
func (e *Engine) Probe(ctx context.Context, rawURL string) (*FileInfo, error) {
	return Probe(ctx, rawURL, e.opts)
}
