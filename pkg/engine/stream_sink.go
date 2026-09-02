package engine

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
)

// DownloadStream returns an io.ReadCloser that produces the file's bytes sequentially.
// If the server supports Range requests, chunks are prefetched concurrently in the background
// with bounded memory backpressure.
func (e *Engine) DownloadStream(ctx context.Context, rawURL string) (io.ReadCloser, *FileInfo, error) {
	info, err := Probe(ctx, rawURL, e.opts)
	if err != nil {
		return nil, nil, fmt.Errorf("probe failed: %w", err)
	}

	if !info.AcceptRanges || info.ContentLength <= 0 || e.opts.Concurrency <= 1 {
		// Single-stream reader
		rc, err := e.singleStreamReader(ctx, info.FinalURL, info)
		return rc, info, err
	}

	// Concurrent prefetching stream reader
	rc := e.newPrefetchStreamReader(ctx, info.FinalURL, info)
	return rc, info, nil
}

// DownloadToWriter streams the downloaded content directly into any io.Writer.
func (e *Engine) DownloadToWriter(ctx context.Context, rawURL string, dest io.Writer) (*FileInfo, error) {
	rc, info, err := e.DownloadStream(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	bufSize := e.opts.CopyBufferSize
	if bufSize <= 0 {
		bufSize = DefaultCopyBufferSize
	}
	buf := make([]byte, bufSize)

	_, err = io.CopyBuffer(dest, rc, buf)
	if err != nil {
		return info, fmt.Errorf("streaming to writer: %w", err)
	}

	return info, nil
}

func (e *Engine) singleStreamReader(ctx context.Context, targetURL string, info *FileInfo) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating stream request: %w", err)
	}
	applyHeaders(req, e.opts)

	resp, err := e.opts.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing stream request: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		return nil, fmt.Errorf("server returned status: %s", resp.Status)
	}

	tracker := NewProgressTracker(info.ContentLength, 1, e.opts.ProgressFunc, e.opts.ProgressInterval)
	tracker.WorkerStarted()

	return &trackingReadCloser{
		rc:      resp.Body,
		tracker: tracker,
	}, nil
}

type trackingReadCloser struct {
	rc      io.ReadCloser
	tracker *ProgressTracker
	closed  bool
}

func (t *trackingReadCloser) Read(p []byte) (int, error) {
	n, err := t.rc.Read(p)
	if n > 0 && t.tracker != nil {
		t.tracker.AddBytes(int64(n))
	}
	if err == io.EOF && t.tracker != nil {
		t.tracker.ChunkCompleted()
	}
	return n, err
}

func (t *trackingReadCloser) Close() error {
	if t.closed {
		return nil
	}
	t.closed = true
	if t.tracker != nil {
		t.tracker.WorkerFinished()
		t.tracker.Stop(nil)
	}
	return t.rc.Close()
}

// prefetchStreamReader coordinates concurrent background downloading and sequential reading.
type prefetchStreamReader struct {
	ctx        context.Context
	cancel     context.CancelFunc
	targetURL  string
	opts       *Options
	info       *FileInfo
	chunks     []*Chunk
	tracker    *ProgressTracker

	// Ordered chunk result slots
	chunkSlots map[int]*chunkResult
	slotsMu    sync.Mutex
	readyCond  *sync.Cond

	currentIndex int
	currentBuf   *bytes.Reader
	closed       bool
	closeErr     error
}

type chunkResult struct {
	data []byte
	err  error
}

func (e *Engine) newPrefetchStreamReader(ctx context.Context, targetURL string, info *FileInfo) *prefetchStreamReader {
	streamCtx, cancel := context.WithCancel(ctx)
	chunks := CalculateChunks(info.ContentLength, e.opts.ChunkSize, e.opts.Concurrency)
	tracker := NewProgressTracker(info.ContentLength, len(chunks), e.opts.ProgressFunc, e.opts.ProgressInterval)

	sr := &prefetchStreamReader{
		ctx:        streamCtx,
		cancel:     cancel,
		targetURL:  targetURL,
		opts:       e.opts,
		info:       info,
		chunks:     chunks,
		tracker:    tracker,
		chunkSlots: make(map[int]*chunkResult),
	}
	sr.readyCond = sync.NewCond(&sr.slotsMu)

	go func() {
		<-streamCtx.Done()
		sr.slotsMu.Lock()
		sr.readyCond.Broadcast()
		sr.slotsMu.Unlock()
	}()

	// Start background prefetch workers
	go sr.startPrefetchWorkers()

	return sr
}

func (sr *prefetchStreamReader) startPrefetchWorkers() {
	concurrency := sr.opts.Concurrency
	if concurrency > len(sr.chunks) {
		concurrency = len(sr.chunks)
	}
	if concurrency <= 0 {
		concurrency = DefaultConcurrency
	}

	maxPrefetch := sr.opts.StreamPrefetch
	if maxPrefetch <= 0 {
		maxPrefetch = DefaultStreamPrefetch
	}

	jobChan := make(chan *Chunk, len(sr.chunks))
	for _, chunk := range sr.chunks {
		jobChan <- chunk
	}
	close(jobChan)

	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sr.tracker.WorkerStarted()
			defer sr.tracker.WorkerFinished()

			for chunk := range jobChan {
				// Wait until this chunk is within the sliding prefetch window
				sr.slotsMu.Lock()
				for !sr.closed && sr.ctx.Err() == nil && chunk.Index >= sr.currentIndex+maxPrefetch {
					sr.readyCond.Wait()
				}
				if sr.closed || sr.ctx.Err() != nil {
					sr.slotsMu.Unlock()
					return
				}
				sr.slotsMu.Unlock()

				// Download chunk into memory buffer
				buf := bytes.NewBuffer(make([]byte, 0, chunk.Size()))
				err := downloadChunk(sr.ctx, sr.targetURL, chunk, buf, sr.opts, sr.tracker)

				sr.slotsMu.Lock()
				sr.chunkSlots[chunk.Index] = &chunkResult{
					data: buf.Bytes(),
					err:  err,
				}
				sr.readyCond.Broadcast()
				sr.slotsMu.Unlock()

				if err != nil {
					sr.cancel()
					return
				}
			}
		}()
	}

	// Wait for workers in separate goroutine and broadcast completion
	go func() {
		wg.Wait()
		sr.slotsMu.Lock()
		sr.readyCond.Broadcast()
		sr.slotsMu.Unlock()
	}()
}

func (sr *prefetchStreamReader) Read(p []byte) (int, error) {
	sr.slotsMu.Lock()
	defer sr.slotsMu.Unlock()

	if sr.closed {
		if sr.closeErr != nil {
			return 0, sr.closeErr
		}
		return 0, io.EOF
	}

	for {
		if sr.ctx.Err() != nil {
			return 0, sr.ctx.Err()
		}

		// Read from current buffer if available
		if sr.currentBuf != nil && sr.currentBuf.Len() > 0 {
			return sr.currentBuf.Read(p)
		}

		// Check if we reached end of all chunks
		if sr.currentIndex >= len(sr.chunks) {
			return 0, io.EOF
		}

		// Check if next chunk is downloaded and ready
		res, ok := sr.chunkSlots[sr.currentIndex]
		if ok {
			if res.err != nil {
				return 0, res.err
			}
			sr.currentBuf = bytes.NewReader(res.data)
			// Remove previous chunk from memory to free RAM
			delete(sr.chunkSlots, sr.currentIndex)
			sr.currentIndex++
			sr.readyCond.Broadcast() // Wake up waiting prefetch workers
			continue
		}

		// Wait for next chunk to be delivered by workers
		sr.readyCond.Wait()
	}
}

func (sr *prefetchStreamReader) Close() error {
	sr.slotsMu.Lock()
	defer sr.slotsMu.Unlock()

	if sr.closed {
		return nil
	}
	sr.closed = true
	sr.cancel()
	sr.readyCond.Broadcast()

	if sr.tracker != nil {
		sr.tracker.Stop(nil)
	}

	// Release any buffered chunk memory
	sr.chunkSlots = nil
	sr.currentBuf = nil

	return nil
}
