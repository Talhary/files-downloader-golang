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

// DownloadToFD downloads the resource directly into an open OS file descriptor.
// This is critical for Android Scoped Storage / Storage Access Framework (SAF)
// where write access is provided via ParcelFileDescriptor.getFd().
func (e *Engine) DownloadToFD(ctx context.Context, rawURL string, fd int, statePath string) (*FileInfo, error) {
	if e.opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.opts.Timeout)
		defer cancel()
	}

	info, err := Probe(ctx, rawURL, e.opts)
	if err != nil {
		return nil, fmt.Errorf("probe failed: %w", err)
	}

	// Wrap OS file descriptor
	file := os.NewFile(uintptr(fd), info.Filename)
	if file == nil {
		return nil, fmt.Errorf("invalid file descriptor: %d", fd)
	}
	// Note: We do not close `file` here because ownership of fd belongs to caller (or caller can close it)

	if !info.AcceptRanges || info.ContentLength <= 0 || e.opts.Concurrency <= 1 {
		err = e.downloadSingleStreamToFileHandle(ctx, info.FinalURL, file, info)
	} else {
		err = e.downloadMultipartToFileHandle(ctx, info.FinalURL, file, info, statePath)
	}

	if err != nil {
		return info, err
	}
	return info, nil
}

// DownloadToFileWithResume downloads the resource to a destination file,
// checking for an existing checkpoint to resume partial progress.
func (e *Engine) DownloadToFileWithResume(ctx context.Context, rawURL string, destPath string, statePath string) (*FileInfo, error) {
	if e.opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.opts.Timeout)
		defer cancel()
	}

	info, err := Probe(ctx, rawURL, e.opts)
	if err != nil {
		return nil, fmt.Errorf("probe failed: %w", err)
	}

	baseDest, err := e.resolveDestPath(destPath, info, true)
	if err != nil {
		return nil, err
	}
	if statePath == "" {
		statePath = GetDefaultStatePath(baseDest)
	}

	var cp *Checkpoint
	existingCp, err := LoadCheckpoint(statePath)
	if err == nil && existingCp != nil && existingCp.IsResumeValid(info) {
		cp = existingCp
	}
	resolvedDest, err := e.resolveDestPath(destPath, info, cp != nil)
	if err != nil {
		return nil, err
	}

	var openFlags int
	if cp != nil {
		// Resuming: Keep existing bytes
		openFlags = os.O_CREATE | os.O_RDWR
	} else {
		// New download: Truncate / clean start
		openFlags = os.O_CREATE | os.O_RDWR | os.O_TRUNC
	}

	// Ensure destination directory exists
	if dir := filepath.Dir(resolvedDest); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("creating destination directory: %w", err)
		}
	}
	if dir := filepath.Dir(statePath); dir != "" && dir != "." {
		_ = os.MkdirAll(dir, 0o755)
	}

	file, err := os.OpenFile(resolvedDest, openFlags, 0o644)
	if err != nil {
		return nil, fmt.Errorf("opening target file: %w", err)
	}
	defer file.Close()

	if cp != nil || (info.AcceptRanges && info.ContentLength > 0 && e.opts.Concurrency > 1) {
		err = e.downloadMultipartToFileHandleWithCheckpoint(ctx, info.FinalURL, file, info, cp, statePath)
	} else {
		err = e.downloadSingleStreamToFileHandle(ctx, info.FinalURL, file, info)
	}

	if err != nil {
		return info, err
	}

	// Clean up state file upon complete success
	RemoveCheckpoint(statePath)
	return info, nil
}

func (e *Engine) downloadMultipartToFileHandle(ctx context.Context, targetURL string, file *os.File, info *FileInfo, statePath string) error {
	var cp *Checkpoint
	if statePath != "" {
		if loaded, err := LoadCheckpoint(statePath); err == nil && loaded.IsResumeValid(info) {
			cp = loaded
		}
	}
	err := e.downloadMultipartToFileHandleWithCheckpoint(ctx, targetURL, file, info, cp, statePath)
	if err == nil && statePath != "" {
		RemoveCheckpoint(statePath)
	}
	return err
}

func (e *Engine) downloadMultipartToFileHandleWithCheckpoint(
	ctx context.Context,
	targetURL string,
	file *os.File,
	info *FileInfo,
	cp *Checkpoint,
	statePath string,
) (resultErr error) {
	// Pre-allocate file size if new download
	if cp == nil && info.ContentLength > 0 {
		_ = file.Truncate(info.ContentLength)
	}

	var chunks []*Chunk
	var initialDownloadedBytes int64

	if cp != nil && len(cp.Chunks) > 0 {
		// Restore chunks from checkpoint
		chunks = make([]*Chunk, len(cp.Chunks))
		for i, cs := range cp.Chunks {
			ch := &Chunk{
				Index:      cs.Index,
				Start:      cs.Start,
				End:        cs.End,
				Downloaded: cs.Downloaded,
				Completed:  cs.Completed,
			}
			chunks[i] = ch
			initialDownloadedBytes += ch.GetDownloaded()
		}
	} else {
		// New chunks layout
		chunks = CalculateChunks(info.ContentLength, e.opts.ChunkSize, e.opts.Concurrency)
		if statePath != "" {
			cp = NewCheckpoint(info, chunks, e.opts.ChunkSize)
			_ = cp.Save(statePath)
		}
	}

	tracker := NewProgressTracker(info.ContentLength, len(chunks), e.opts.ProgressFunc, e.opts.ProgressInterval)
	if initialDownloadedBytes > 0 {
		tracker.AddBytes(initialDownloadedBytes)
	}
	defer func() {
		tracker.Stop(resultErr)
	}()

	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan *Chunk, len(chunks))
	for _, ch := range chunks {
		if !ch.IsCompleted() {
			jobs <- ch
		} else {
			tracker.ChunkCompleted()
		}
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
	var cpMu sync.Mutex

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

				destFactory := func() (io.Writer, error) {
					return io.NewOffsetWriter(file, chunk.Start+chunk.GetDownloaded()), nil
				}

				if err := downloadChunk(workerCtx, targetURL, chunk, destFactory, e.opts, tracker); err != nil {
					errOnce.Do(func() {
						downloadErr = err
						cancel()
					})
					return
				}

				// Chunk finished successfully, sync checkpoint
				if cp != nil && statePath != "" {
					cpMu.Lock()
					cp.SyncFromChunks(chunks)
					_ = cp.Save(statePath)
					cpMu.Unlock()
				}
			}
		}()
	}

	wg.Wait()

	// Persist checkpoint on pause or cancel
	if (workerCtx.Err() != nil || ctx.Err() != nil || downloadErr != nil) && cp != nil && statePath != "" {
		cpMu.Lock()
		cp.SyncFromChunks(chunks)
		_ = cp.Save(statePath)
		cpMu.Unlock()
	}

	if downloadErr != nil {
		return downloadErr
	}
	if workerCtx.Err() != nil && ctx.Err() != nil {
		return ctx.Err()
	}

	_ = file.Sync()
	return nil
}

func (e *Engine) downloadSingleStreamToFileHandle(ctx context.Context, targetURL string, file *os.File, info *FileInfo) (resultErr error) {
	if err := file.Truncate(0); err != nil {
		return fmt.Errorf("truncating destination: %w", err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("seeking destination: %w", err)
	}

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

	var bodyReader io.ReadCloser = resp.Body
	if e.opts.IdleTimeout > 0 {
		idleReader := newIdleTimeoutReader(resp.Body, e.opts.IdleTimeout)
		defer idleReader.Close()
		bodyReader = idleReader
	}

	tracker := NewProgressTracker(info.ContentLength, 1, e.opts.ProgressFunc, e.opts.ProgressInterval)
	defer func() {
		tracker.Stop(resultErr)
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

		n, rErr := bodyReader.Read(buf)
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
