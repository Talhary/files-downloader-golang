package engine

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCalculateChunks(t *testing.T) {
	tests := []struct {
		name         string
		totalSize    int64
		chunkSize    int64
		concurrency  int
		expectedLen  int
		expectedLast int64
	}{
		{
			name:         "Exact single chunk",
			totalSize:    1024,
			chunkSize:    1024,
			concurrency:  4,
			expectedLen:  1,
			expectedLast: 1023,
		},
		{
			name:         "Multiple even chunks",
			totalSize:    4000,
			chunkSize:    1000,
			concurrency:  4,
			expectedLen:  4,
			expectedLast: 3999,
		},
		{
			name:         "Uneven chunk with remainder",
			totalSize:    4500,
			chunkSize:    1000,
			concurrency:  4,
			expectedLen:  5,
			expectedLast: 4499,
		},
		{
			name:         "Zero total size",
			totalSize:    0,
			chunkSize:    1000,
			concurrency:  4,
			expectedLen:  0,
			expectedLast: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			chunks := CalculateChunks(tc.totalSize, tc.chunkSize, tc.concurrency)
			if len(chunks) != tc.expectedLen {
				t.Fatalf("expected %d chunks, got %d", tc.expectedLen, len(chunks))
			}
			if len(chunks) > 0 {
				lastChunk := chunks[len(chunks)-1]
				if lastChunk.End != tc.expectedLast {
					t.Fatalf("expected last chunk end to be %d, got %d", tc.expectedLast, lastChunk.End)
				}
				// Verify chunk continuity
				var expectedStart int64 = 0
				for i, ch := range chunks {
					if ch.Start != expectedStart {
						t.Fatalf("chunk %d start mismatch: expected %d, got %d", i, expectedStart, ch.Start)
					}
					expectedStart = ch.End + 1
				}
			}
		})
	}
}

func TestMultipartDownloadToFile(t *testing.T) {
	// Generate 5MB random test data
	testData := make([]byte, 5*1024*1024)
	_, err := rand.Read(testData)
	if err != nil {
		t.Fatalf("generating random test data: %v", err)
	}

	server := createMockRangeServer(testData)
	defer server.Close()

	tempDir := t.TempDir()
	destFile := filepath.Join(tempDir, "downloaded.bin")

	var progressUpdates int32
	opts := []Option{
		WithAllowPrivateHosts(true),
		WithConcurrency(4),
		WithChunkSize(512 * 1024), // 512 KB chunks = 10 chunks
		WithProgressCallback(func(s ProgressSnapshot) {
			atomic.AddInt32(&progressUpdates, 1)
		}, 20*time.Millisecond),
	}

	eng := New(opts...)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	info, err := eng.DownloadToFile(ctx, server.URL+"/test-file.bin", destFile)
	if err != nil {
		t.Fatalf("DownloadToFile failed: %v", err)
	}

	if info.ContentLength != int64(len(testData)) {
		t.Errorf("expected ContentLength %d, got %d", len(testData), info.ContentLength)
	}

	// Verify file on disk
	downloadedData, err := os.ReadFile(destFile)
	if err != nil {
		t.Fatalf("reading downloaded file: %v", err)
	}

	if !bytes.Equal(testData, downloadedData) {
		t.Fatalf("downloaded file data does not match original source data byte-for-byte!")
	}
}

func TestStreamReader(t *testing.T) {
	testData := make([]byte, 2*1024*1024) // 2MB
	_, _ = rand.Read(testData)

	server := createMockRangeServer(testData)
	defer server.Close()

	eng := New(
		WithAllowPrivateHosts(true),
		WithConcurrency(4),
		WithChunkSize(256*1024), // 8 chunks
		WithStreamPrefetch(3),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rc, info, err := eng.DownloadStream(ctx, server.URL+"/stream-test.bin")
	if err != nil {
		t.Fatalf("DownloadStream failed: %v", err)
	}
	defer rc.Close()

	if info.ContentLength != int64(len(testData)) {
		t.Errorf("expected ContentLength %d, got %d", len(testData), info.ContentLength)
	}

	streamedData, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("reading stream failed: %v", err)
	}

	if !bytes.Equal(testData, streamedData) {
		t.Fatalf("streamed data does not match original data byte-for-byte!")
	}
}

func TestDownloadToWriter(t *testing.T) {
	testData := make([]byte, 1*1024*1024) // 1MB
	_, _ = rand.Read(testData)

	server := createMockRangeServer(testData)
	defer server.Close()

	eng := New(
		WithAllowPrivateHosts(true),
		WithConcurrency(4),
		WithChunkSize(128*1024),
	)

	var buf bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := eng.DownloadToWriter(ctx, server.URL+"/writer-test.bin", &buf)
	if err != nil {
		t.Fatalf("DownloadToWriter failed: %v", err)
	}

	if !bytes.Equal(testData, buf.Bytes()) {
		t.Fatalf("buffered writer data does not match original!")
	}
}

func TestSingleStreamFallback(t *testing.T) {
	testData := []byte("hello single stream fallback download content!")

	// Server without Accept-Ranges
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Length", strconv.Itoa(len(testData)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(testData)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	destFile := filepath.Join(tempDir, "fallback.txt")

	eng := New(WithAllowPrivateHosts(true), WithConcurrency(4))
	ctx := context.Background()

	info, err := eng.DownloadToFile(ctx, server.URL+"/fallback.txt", destFile)
	if err != nil {
		t.Fatalf("fallback download failed: %v", err)
	}

	if info.AcceptRanges {
		t.Errorf("expected AcceptRanges to be false")
	}

	data, err := os.ReadFile(destFile)
	if err != nil {
		t.Fatalf("reading fallback file: %v", err)
	}

	if string(data) != string(testData) {
		t.Errorf("expected %q, got %q", string(testData), string(data))
	}
}

func TestRetryOnTransientFailure(t *testing.T) {
	testData := make([]byte, 512*1024)
	_, _ = rand.Read(testData)

	var failCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Fail the first 2 requests with 500 error, then succeed
		if failCount.Add(1) <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", strconv.Itoa(len(testData)))
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}

		rangeHeader := r.Header.Get("Range")
		if rangeHeader != "" {
			var start, end int64
			_, _ = fmt.Sscanf(rangeHeader, "bytes=%d-%d", &start, &end)
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(testData)))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(testData[start : end+1])
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(testData)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	destFile := filepath.Join(tempDir, "retry-test.bin")

	eng := New(
		WithAllowPrivateHosts(true),
		WithMaxRetries(3),
		WithRetryDelay(10*time.Millisecond),
	)

	_, err := eng.DownloadToFile(context.Background(), server.URL+"/file.bin", destFile)
	if err != nil {
		t.Fatalf("expected download to succeed after retries, but got: %v", err)
	}

	data, err := os.ReadFile(destFile)
	if err != nil {
		t.Fatalf("reading file: %v", err)
	}

	if !bytes.Equal(data, testData) {
		t.Fatalf("data mismatch after retry")
	}
}

func createMockRangeServer(data []byte) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("ETag", `"mock-etag-123"`)

		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}

		rangeHeader := r.Header.Get("Range")
		if rangeHeader != "" && strings.HasPrefix(rangeHeader, "bytes=") {
			rangeSpec := strings.TrimPrefix(rangeHeader, "bytes=")
			parts := strings.Split(rangeSpec, "-")
			if len(parts) == 2 {
				start, _ := strconv.ParseInt(parts[0], 10, 64)
				end, err := strconv.ParseInt(parts[1], 10, 64)
				if err != nil || end >= int64(len(data)) {
					end = int64(len(data)) - 1
				}

				if start <= end && start < int64(len(data)) {
					w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(data)))
					w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
					w.WriteHeader(http.StatusPartialContent)
					_, _ = w.Write(data[start : end+1])
					return
				}
			}
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	}))
}

func TestLinkTimeout(t *testing.T) {
	// Server that delays sending headers for 500ms
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("too late"))
	}))
	defer server.Close()

	eng := New(
		WithAllowPrivateHosts(true),
		WithLinkTimeout(100*time.Millisecond),
		WithMaxRetries(0),
	)

	ctx := context.Background()
	_, err := eng.Probe(ctx, server.URL)
	if err == nil {
		t.Fatalf("expected probe to fail due to link timeout, but got nil")
	}
}

func TestIdleTimeoutRetry(t *testing.T) {
	testData := []byte("hello world with idle timeout test data that is 64 bytes long!!")
	var attemptCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", strconv.Itoa(len(testData)))
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}

		call := attemptCount.Add(1)
		var start, end int64
		_, _ = fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(testData)))
		w.WriteHeader(http.StatusPartialContent)

		if call == 1 {
			// First call: write partial bytes, then stall
			_, _ = w.Write(testData[start : start+10])
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			time.Sleep(400 * time.Millisecond) // Exceeds 100ms idle timeout
			_, _ = w.Write(testData[start+10:])
			return
		}

		// Subsequent call: write immediately
		_, _ = w.Write(testData[start:])
	}))
	defer server.Close()

	tempDir := t.TempDir()
	destFile := filepath.Join(tempDir, "idle_test.bin")

	eng := New(
		WithAllowPrivateHosts(true),
		WithIdleTimeout(100*time.Millisecond),
		WithMaxRetries(2),
		WithRetryDelay(10*time.Millisecond),
	)

	_, err := eng.DownloadToFile(context.Background(), server.URL, destFile)
	if err != nil {
		t.Fatalf("expected download to succeed after idle timeout retry, got: %v", err)
	}

	downloaded, err := os.ReadFile(destFile)
	if err != nil {
		t.Fatalf("reading downloaded file: %v", err)
	}
	if !bytes.Equal(downloaded, testData) {
		t.Fatalf("file content mismatch: expected %q, got %q", testData, downloaded)
	}
}

func TestRetryIntegrityAndOffset(t *testing.T) {
	// 512KB test data
	testData := make([]byte, 512*1024)
	_, _ = rand.Read(testData)

	var chunkFailCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", strconv.Itoa(len(testData)))
			w.WriteHeader(http.StatusOK)
			return
		}

		rangeHeader := r.Header.Get("Range")
		var start, end int64
		_, _ = fmt.Sscanf(rangeHeader, "bytes=%d-%d", &start, &end)

		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(testData)))
		w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
		w.WriteHeader(http.StatusPartialContent)

		// Intentionally fail the first attempt of the second half after writing 100 bytes
		if start > 0 && chunkFailCount.Add(1) == 1 {
			_, _ = w.Write(testData[start : start+100])
			// Close connection abruptly
			hj, ok := w.(http.Hijacker)
			if ok {
				conn, _, _ := hj.Hijack()
				_ = conn.Close()
				return
			}
		}

		_, _ = w.Write(testData[start : end+1])
	}))
	defer server.Close()

	tempDir := t.TempDir()
	destFile := filepath.Join(tempDir, "offset_integrity.bin")

	eng := New(
		WithAllowPrivateHosts(true),
		WithConcurrency(2),
		WithChunkSize(256*1024), // 2 chunks: 0-262143 and 262144-524287
		WithMaxRetries(3),
		WithRetryDelay(10*time.Millisecond),
	)

	_, err := eng.DownloadToFile(context.Background(), server.URL, destFile)
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}

	downloaded, err := os.ReadFile(destFile)
	if err != nil {
		t.Fatalf("reading file: %v", err)
	}

	if !bytes.Equal(downloaded, testData) {
		t.Fatalf("downloaded file corrupted after retry! Byte mismatch detected.")
	}
}

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"normal.zip", "normal.zip"},
		{"file:name?.rar", "file_name_.rar"},
		{"../../evil.exe", "evil.exe"},
		{"trailing_dot.", "trailing_dot"},
		{"<illegal>|chars*.txt", "_illegal__chars_.txt"},
		{"", "downloaded_file"},
		{"...", "downloaded_file"},
		{"NUL.rar", "_NUL.rar"},
		{"con.txt", "_con.txt"},
		{"COM1", "_COM1"},
		{"lpt9.log", "_lpt9.log"},
	}

	for _, tc := range tests {
		got := sanitizeFilename(tc.input)
		if got != tc.expected {
			t.Errorf("sanitizeFilename(%q) = %q; want %q", tc.input, got, tc.expected)
		}
	}

	// Test extractFilename with URL containing query string and fragment
	u := "https://example.com/downloads/archive.tar.gz?nocache=123#frag"
	extracted := extractFilename(u, u, "", "")
	if extracted != "archive.tar.gz" {
		t.Errorf("extractFilename() = %q; want archive.tar.gz", extracted)
	}

	// Test query param filename extraction
	qUrl := "https://example.com/download?file=document.pdf"
	if ext := extractFilename(qUrl, qUrl, "", ""); ext != "document.pdf" {
		t.Errorf("extractFilename() = %q; want document.pdf", ext)
	}

	// Test Content-Type extension inference when URL has no extension
	streamUrl := "https://example.com/video/stream"
	if ext := extractFilename(streamUrl, streamUrl, "", "video/mp4"); ext != "stream.mp4" {
		t.Errorf("extractFilename() = %q; want stream.mp4", ext)
	}
}

func TestNonRetryableError(t *testing.T) {
	var requestCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusNotFound) // 404
	}))
	defer server.Close()

	eng := New(
		WithAllowPrivateHosts(true),
		WithMaxRetries(5),
		WithRetryDelay(10*time.Millisecond),
	)

	_, err := eng.Probe(context.Background(), server.URL)
	if err == nil {
		t.Fatalf("expected 404 to fail probe, but got nil")
	}

	if !errors.Is(err, ErrNonRetryable) {
		t.Errorf("expected ErrNonRetryable, got: %v", err)
	}

	// Should not retry 5 times
	if count := requestCount.Load(); count > 2 {
		t.Errorf("expected at most 2 requests for non-retryable 404, got %d", count)
	}
}

func TestPauseAndResumeCheckpoint(t *testing.T) {
	testData := make([]byte, 1024*1024) // 1 MB
	for i := range testData {
		testData[i] = byte(i % 256)
	}

	var chunkRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("ETag", "\"test-etag-123\"")
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", strconv.Itoa(len(testData)))
			w.WriteHeader(http.StatusOK)
			return
		}

		rangeHeader := r.Header.Get("Range")
		var start, end int64
		_, _ = fmt.Sscanf(rangeHeader, "bytes=%d-%d", &start, &end)

		chunkRequests.Add(1)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(testData)))
		w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(testData[start : end+1])
	}))
	defer server.Close()

	tempDir := t.TempDir()
	destFile := filepath.Join(tempDir, "resumable.bin")
	stateFile := filepath.Join(tempDir, "resumable.bin.dlstate.json")

	// Phase 1: Simulate partial download where first chunk (256KB) completed
	chunkSize := int64(256 * 1024)
	file, err := os.Create(destFile)
	if err != nil {
		t.Fatalf("creating dest file: %v", err)
	}
	// Write first 256KB
	_, _ = file.Write(testData[:chunkSize])
	_ = file.Truncate(int64(len(testData)))
	file.Close()

	// Write checkpoint file showing chunk 0 completed, chunks 1..3 remaining
	totalChunks := 4
	chunks := make([]*ChunkState, totalChunks)
	for i := 0; i < totalChunks; i++ {
		start := int64(i) * chunkSize
		end := start + chunkSize - 1
		completed := (i == 0)
		downloaded := int64(0)
		if completed {
			downloaded = chunkSize
		}
		chunks[i] = &ChunkState{
			Index:      i,
			Start:      start,
			End:        end,
			Downloaded: downloaded,
			Completed:  completed,
		}
	}

	cp := &Checkpoint{
		URL:           server.URL,
		FinalURL:      server.URL,
		Filename:      "resumable.bin",
		ContentLength: int64(len(testData)),
		ETag:          "\"test-etag-123\"",
		AcceptRanges:  true,
		ChunkSize:     chunkSize,
		TotalChunks:   totalChunks,
		CompletedSize: chunkSize,
		Chunks:        chunks,
	}
	if err := cp.Save(stateFile); err != nil {
		t.Fatalf("saving checkpoint: %v", err)
	}

	// Phase 2: Resume download with DownloadToFileWithResume
	eng := New(
		WithAllowPrivateHosts(true),
		WithConcurrency(4),
		WithChunkSize(chunkSize),
	)

	_, err = eng.DownloadToFileWithResume(context.Background(), server.URL, destFile, stateFile)
	if err != nil {
		t.Fatalf("resuming download failed: %v", err)
	}

	// Checkpoint file should have been cleaned up on success
	if _, err := os.Stat(stateFile); !os.IsNotExist(err) {
		t.Errorf("expected state file to be removed after successful resume")
	}

	// Verify complete content matches byte-for-byte
	resultData, err := os.ReadFile(destFile)
	if err != nil {
		t.Fatalf("reading resumed file: %v", err)
	}
	if !bytes.Equal(resultData, testData) {
		t.Fatalf("resumed file content does not match original data")
	}

	// Only remaining 3 chunks should have been requested (chunk 0 was skipped!)
	if requests := chunkRequests.Load(); requests != 3 {
		t.Errorf("expected exactly 3 chunk requests during resume, got %d", requests)
	}
}

func TestOptionsReturnsIndependentCopy(t *testing.T) {
	eng := New(WithHeader("X-Test", "original"), WithAllowPrivateHosts(true))
	opts := eng.Options()
	opts.Concurrency = 1
	opts.Headers["X-Test"] = "changed"

	current := eng.Options()
	if current.Concurrency == 1 || current.Headers["X-Test"] != "original" {
		t.Fatalf("mutating Options result changed engine options: %+v", current)
	}
}

func TestRejectsInvalidAndPrivateURLs(t *testing.T) {
	eng := New()
	if _, err := eng.Probe(context.Background(), "file:///tmp/archive.zip"); !errors.Is(err, ErrInvalidURL) {
		t.Fatalf("Probe(file URL) error = %v; want ErrInvalidURL", err)
	}
	if _, err := eng.Probe(context.Background(), "http://127.0.0.1/private"); !errors.Is(err, ErrPrivateNetworkAccess) {
		t.Fatalf("Probe(loopback URL) error = %v; want ErrPrivateNetworkAccess", err)
	}
}

func TestRejectsInvalidContentRange(t *testing.T) {
	data := make([]byte, 256)
	for i := range data {
		data[i] = byte(i)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			w.WriteHeader(http.StatusOK)
			return
		}
		var start, end int64
		_, _ = fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start+50, end+50, len(data)))
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(data)
	}))
	defer server.Close()

	eng := New(WithAllowPrivateHosts(true), WithConcurrency(2), WithChunkSize(128), WithMaxRetries(0))
	_, err := eng.DownloadToFile(context.Background(), server.URL+"/file.bin", filepath.Join(t.TempDir(), "file.bin"))
	if !errors.Is(err, ErrInvalidRange) {
		t.Fatalf("DownloadToFile() error = %v; want ErrInvalidRange", err)
	}
}

func TestChunkRetryResumesAtExactByteOffset(t *testing.T) {
	data := []byte("0123456789")
	var rangesMu sync.Mutex
	var ranges []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rangeHeader := r.Header.Get("Range")
		rangesMu.Lock()
		ranges = append(ranges, rangeHeader)
		rangesMu.Unlock()
		var start, end int64
		_, _ = fmt.Sscanf(rangeHeader, "bytes=%d-%d", &start, &end)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(data)))
		w.WriteHeader(http.StatusPartialContent)
		if len(ranges) == 1 {
			_, _ = w.Write(data[start : start+4])
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			if hijacker, ok := w.(http.Hijacker); ok {
				connection, _, _ := hijacker.Hijack()
				_ = connection.Close()
			}
			return
		}
		_, _ = w.Write(data[start : end+1])
	}))
	defer server.Close()

	destination := filepath.Join(t.TempDir(), "resume.bin")
	file, err := os.Create(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := file.Truncate(int64(len(data))); err != nil {
		t.Fatal(err)
	}
	chunk := &Chunk{Start: 0, End: int64(len(data) - 1)}
	factory := func() (io.Writer, error) {
		return io.NewOffsetWriter(file, chunk.GetDownloaded()), nil
	}
	opts := DefaultOptions()
	opts.AllowPrivateHosts = true
	opts.MaxRetries = 1
	opts.RetryDelay = time.Millisecond
	if err := downloadChunk(context.Background(), server.URL, chunk, factory, opts, nil); err != nil {
		t.Fatalf("downloadChunk() error = %v", err)
	}
	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("downloaded data = %q; want %q", got, data)
	}
	rangesMu.Lock()
	defer rangesMu.Unlock()
	if len(ranges) != 2 || ranges[0] != "bytes=0-9" || ranges[1] != "bytes=4-9" {
		t.Fatalf("request ranges = %v; want [bytes=0-9 bytes=4-9]", ranges)
	}
}

func TestFinalProgressReportsDownloadError(t *testing.T) {
	payload := []byte("complete payload that will be interrupted")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodGet {
			_, _ = w.Write(payload[:5])
			if hijacker, ok := w.(http.Hijacker); ok {
				connection, _, _ := hijacker.Hijack()
				_ = connection.Close()
			}
		}
	}))
	defer server.Close()

	snapshots := make(chan ProgressSnapshot, 1)
	eng := New(WithAllowPrivateHosts(true), WithConcurrency(1), WithMaxRetries(0), WithProgressCallback(func(snapshot ProgressSnapshot) {
		select {
		case snapshots <- snapshot:
		default:
		}
	}, time.Millisecond))
	_, err := eng.DownloadToFile(context.Background(), server.URL+"/file", filepath.Join(t.TempDir(), "file"))
	if err == nil {
		t.Fatal("DownloadToFile() unexpectedly succeeded")
	}
	select {
	case snapshot := <-snapshots:
		if !snapshot.Done || snapshot.Err == nil {
			t.Fatalf("final snapshot = %+v; want Done with non-nil error", snapshot)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for final progress snapshot")
	}
}

func TestAutoRenamePreservesExistingFile(t *testing.T) {
	payload := []byte("new payload")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	destination := filepath.Join(t.TempDir(), "download.bin")
	if err := os.WriteFile(destination, []byte("old payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	eng := New(WithAllowPrivateHosts(true), WithConcurrency(1))
	if _, err := eng.DownloadToFile(context.Background(), server.URL+"/download.bin", destination); err != nil {
		t.Fatalf("DownloadToFile() error = %v", err)
	}
	original, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	renamed, err := os.ReadFile(filepath.Join(filepath.Dir(destination), "download (1).bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(original, []byte("old payload")) || !bytes.Equal(renamed, payload) {
		t.Fatalf("files = %q, %q; want preserved original and renamed download", original, renamed)
	}
}

func TestDownloadToFDTruncatesAndSeeksStart(t *testing.T) {
	payload := []byte("new")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	destination := filepath.Join(t.TempDir(), "fd.bin")
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	runtime.SetFinalizer(file, nil)
	if _, err := file.WriteString("stale data with trailing bytes"); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(7, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	eng := New(WithAllowPrivateHosts(true), WithConcurrency(1))
	if _, err := eng.DownloadToFD(context.Background(), server.URL+"/file", int(file.Fd()), ""); err != nil {
		t.Fatalf("DownloadToFD() error = %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("FD destination = %q; want %q", got, payload)
	}
}
