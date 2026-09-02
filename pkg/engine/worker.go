package engine

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// downloadChunk executes a download for a single chunk with retry and backoff.
func downloadChunk(
	ctx context.Context,
	rawURL string,
	chunk *Chunk,
	dest io.Writer,
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

		err := attemptDownloadChunk(ctx, rawURL, chunk, dest, opts, tracker)
		if err == nil {
			chunk.MarkCompleted()
			if tracker != nil {
				tracker.ChunkCompleted()
			}
			return nil
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

	if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, resp.Status)
	}

	// Buffer for copying
	bufSize := opts.CopyBufferSize
	if bufSize <= 0 {
		bufSize = DefaultCopyBufferSize
	}
	buf := make([]byte, bufSize)

	// Wrap destination with counting writer that feeds the progress tracker
	expectedBytes := chunk.Size()
	var totalRead int64

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		n, rErr := resp.Body.Read(buf)
		if n > 0 {
			// Write to dest
			wWritten, wErr := dest.Write(buf[:n])
			if wErr != nil {
				return fmt.Errorf("writing to destination: %w", wErr)
			}
			if wWritten != n {
				return io.ErrShortWrite
			}

			totalRead += int64(n)
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
