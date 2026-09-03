package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"
)

// WriterFactory creates a new destination writer (or resets an existing one) for each chunk attempt.
type WriterFactory func() (io.Writer, error)

// downloadChunk executes a download for a single chunk with retry, backoff, and rollback on failure.
func downloadChunk(
	ctx context.Context,
	rawURL string,
	chunk *Chunk,
	destFactory WriterFactory,
	opts *Options,
	tracker *ProgressTracker,
) error {
	var lastErr error

	for attempt := 0; attempt <= opts.MaxRetries; attempt++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if attempt > 0 {
			chunk.Retries++
			// Exponential backoff
			backoff := opts.RetryDelay * time.Duration(1<<(attempt-1))
			if backoff > 10*time.Second {
				backoff = 10 * time.Second
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}

		dest, err := destFactory()
		if err != nil {
			return fmt.Errorf("creating chunk writer: %w", err)
		}

		var attemptBytes int64
		err = attemptDownloadChunk(ctx, rawURL, chunk, dest, opts, tracker, &attemptBytes)
		if err == nil {
			chunk.MarkCompleted()
			if tracker != nil {
				tracker.ChunkCompleted()
			}
			return nil
		}

		// Roll back partially counted bytes on attempt failure to keep progress statistics accurate
		if tracker != nil && attemptBytes > 0 {
			tracker.SubBytes(attemptBytes)
		}

		// Fast-fail non-retryable errors
		if errors.Is(err, ErrNonRetryable) || errors.Is(err, ErrRangeNotSupported) {
			return err
		}

		lastErr = err
	}

	return fmt.Errorf("chunk %d [%s] failed after %d retries: %w", chunk.Index, chunk.RangeHeader(), opts.MaxRetries, lastErr)
}

func attemptDownloadChunk(
	ctx context.Context,
	rawURL string,
	chunk *Chunk,
	dest io.Writer,
	opts *Options,
	tracker *ProgressTracker,
	attemptBytes *int64,
) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fmt.Errorf("creating chunk request: %w", err)
	}

	applyHeaders(req, opts)
	req.Header.Set("Range", chunk.RangeHeader())

	resp, err := opts.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("executing chunk request: %w", err)
	}
	defer resp.Body.Close()

	// Non-retryable HTTP client errors
	if resp.StatusCode == http.StatusUnauthorized ||
		resp.StatusCode == http.StatusForbidden ||
		resp.StatusCode == http.StatusNotFound ||
		resp.StatusCode == http.StatusGone ||
		resp.StatusCode == http.StatusBadRequest ||
		resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		return fmt.Errorf("%w: server returned status %d: %s", ErrNonRetryable, resp.StatusCode, resp.Status)
	}

	if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, resp.Status)
	}

	// If server ignored Range on a chunk past the first one, fail fast
	if resp.StatusCode == http.StatusOK && chunk.Start > 0 {
		return fmt.Errorf("%w: server ignored Range header and returned full content (status 200 OK)", ErrRangeNotSupported)
	}

	var bodyReader io.ReadCloser = resp.Body
	if opts.IdleTimeout > 0 {
		idleReader := newIdleTimeoutReader(resp.Body, opts.IdleTimeout)
		defer idleReader.Close()
		bodyReader = idleReader
	}

	// Buffer for copying
	bufSize := opts.CopyBufferSize
	if bufSize <= 0 {
		bufSize = DefaultCopyBufferSize
	}
	buf := make([]byte, bufSize)

	expectedBytes := chunk.Size()
	var totalRead int64

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		n, rErr := bodyReader.Read(buf)
		if n > 0 {
			wWritten, wErr := dest.Write(buf[:n])
			if wErr != nil {
				return fmt.Errorf("writing to destination: %w", wErr)
			}
			if wWritten != n {
				return io.ErrShortWrite
			}

			totalRead += int64(n)
			if attemptBytes != nil {
				*attemptBytes += int64(n)
			}
			if tracker != nil {
				tracker.AddBytes(int64(n))
			}
		}

		if rErr != nil {
			if rErr == io.EOF {
				break
			}
			return fmt.Errorf("reading chunk body: %w", rErr)
		}
	}

	// Verify byte length matches chunk size if range was accepted
	if resp.StatusCode == http.StatusPartialContent && totalRead != expectedBytes {
		return fmt.Errorf("chunk %d size mismatch: expected %d bytes, received %d bytes", chunk.Index, expectedBytes, totalRead)
	}

	return nil
}

// idleTimeoutReader interrupts stalled reads if no data is received within the specified timeout.
type idleTimeoutReader struct {
	rc       io.ReadCloser
	timeout  time.Duration
	timer    *time.Timer
	timedOut atomic.Bool
}

func newIdleTimeoutReader(rc io.ReadCloser, timeout time.Duration) *idleTimeoutReader {
	r := &idleTimeoutReader{
		rc:      rc,
		timeout: timeout,
	}
	if timeout > 0 {
		r.timer = time.AfterFunc(timeout, func() {
			r.timedOut.Store(true)
			_ = rc.Close()
		})
	}
	return r
}

func (r *idleTimeoutReader) Read(p []byte) (int, error) {
	if r.timer != nil {
		r.timer.Reset(r.timeout)
	}
	n, err := r.rc.Read(p)
	if r.timer != nil {
		r.timer.Reset(r.timeout)
	}
	if err != nil && r.timedOut.Load() {
		return n, fmt.Errorf("chunk read idle timeout after %v: %w", r.timeout, ErrTimeout)
	}
	return n, err
}

func (r *idleTimeoutReader) Close() error {
	if r.timer != nil {
		r.timer.Stop()
	}
	return r.rc.Close()
}
