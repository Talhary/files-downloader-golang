package engine

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
)

// DownloadToFile downloads the remote resource directly to a local file.
// If the server supports range requests, it downloads parts concurrently using WriteAt.
func (e *Engine) DownloadToFile(ctx context.Context, rawURL string, destPath string) (*FileInfo, error) {
	info, err := Probe(ctx, rawURL, e.opts)
	if err != nil {
		return nil, fmt.Errorf("probe failed: %w", err)
	}

	// Resolve destination file path
	resolvedDest, err := e.resolveDestPath(destPath, info)
	if err != nil {
		return nil, err
	}

	// Ensure destination directory exists
	if dir := filepath.Dir(resolvedDest); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("creating destination directory: %w", err)
		}
	}

	if !info.AcceptRanges || info.ContentLength <= 0 || e.opts.Concurrency <= 1 {
		// Single-stream fallback
		err = e.downloadSingleStreamToFile(ctx, info.FinalURL, resolvedDest, info)
	} else {
		// Concurrent multi-part download
		err = e.downloadMultipartToFile(ctx, info.FinalURL, resolvedDest, info)
	}

	if err != nil {
		return info, err
	}

	return info, nil
}

func (e *Engine) downloadMultipartToFile(ctx context.Context, targetURL string, destPath string, info *FileInfo) error {
	file, err := os.OpenFile(destPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("creating target file: %w", err)
	}
	defer file.Close()

	// Pre-allocate / truncate file size to reserve disk space
	if info.ContentLength > 0 {
		if err := file.Truncate(info.ContentLength); err != nil {
			// Non-fatal, continue even if pre-allocation fails
			_ = err
		}
	}

	chunks := CalculateChunks(info.ContentLength, e.opts.ChunkSize, e.opts.Concurrency)
	tracker := NewProgressTracker(info.ContentLength, len(chunks), e.opts.ProgressFunc, e.opts.ProgressInterval)
	defer func() {
		tracker.Stop(err)
	}()

	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan *Chunk, len(chunks))
	for _, ch := range chunks {
		jobs <- ch
	}
	close(jobs)

	concurrency := e.opts.Concurrency
	if concurrency > len(chunks) {
		concurrency = len(chunks)
	}
	if concurrency <= 0 {
		concurrency = DefaultConcurrency
	}

	var wg sync.WaitGroup
	var errOnce sync.Once
	var downloadErr error

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tracker.WorkerStarted()
			defer tracker.WorkerFinished()

			for chunk := range jobs {
				select {
				case <-workerCtx.Done():
					return
				default:
				}

				// Direct random-access writer at chunk start offset
				offsetWriter := io.NewOffsetWriter(file, chunk.Start)
				if err := downloadChunk(workerCtx, targetURL, chunk, offsetWriter, e.opts, tracker); err != nil {
					errOnce.Do(func() {
						downloadErr = err
						cancel() // Stop other workers
					})
					return
				}
			}
		}()
	}

	wg.Wait()

	if downloadErr != nil {
		return downloadErr
	}
	if workerCtx.Err() != nil && ctx.Err() != nil {
		return ctx.Err()
	}

	if err := file.Sync(); err != nil {
		return fmt.Errorf("syncing file to disk: %w", err)
	}

	return nil
}

func (e *Engine) downloadSingleStreamToFile(ctx context.Context, targetURL string, destPath string, info *FileInfo) error {
	file, err := os.OpenFile(destPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("creating target file: %w", err)
	}
	defer file.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	applyHeaders(req, e.opts)

	resp, err := e.opts.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("server returned status: %s", resp.Status)
	}

	tracker := NewProgressTracker(info.ContentLength, 1, e.opts.ProgressFunc, e.opts.ProgressInterval)
	defer func() {
		tracker.Stop(err)
	}()

	tracker.WorkerStarted()
	defer tracker.WorkerFinished()

	bufSize := e.opts.CopyBufferSize
	if bufSize <= 0 {
		bufSize = DefaultCopyBufferSize
	}
	buf := make([]byte, bufSize)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		n, rErr := resp.Body.Read(buf)
		if n > 0 {
			if _, wErr := file.Write(buf[:n]); wErr != nil {
				return fmt.Errorf("writing to disk: %w", wErr)
			}
			tracker.AddBytes(int64(n))
		}
		if rErr != nil {
			if rErr == io.EOF {
				tracker.ChunkCompleted()
				break
			}
			return fmt.Errorf("reading response: %w", rErr)
		}
	}

	return file.Sync()
}

func (e *Engine) resolveDestPath(destPath string, info *FileInfo) (string, error) {
	if destPath == "" {
		return info.Filename, nil
	}

	stat, err := os.Stat(destPath)
	if err == nil && stat.IsDir() {
		return filepath.Join(destPath, info.Filename), nil
	}

	// Check if path ends with separator indicating a directory
	if destPath[len(destPath)-1] == os.PathSeparator || destPath[len(destPath)-1] == '/' {
		return filepath.Join(destPath, info.Filename), nil
	}

	return destPath, nil
}
