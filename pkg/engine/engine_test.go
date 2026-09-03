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
	"strconv"
	"strings"
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

	eng := New(WithConcurrency(4))
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
		WithLinkTimeout(100 * time.Millisecond),
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
		w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", len(testData)-1, len(testData)))
		w.WriteHeader(http.StatusPartialContent)

		if call == 1 {
			// First call: write partial bytes, then stall
			_, _ = w.Write(testData[:10])
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			time.Sleep(400 * time.Millisecond) // Exceeds 100ms idle timeout
			_, _ = w.Write(testData[10:])
			return
		}

		// Subsequent call: write immediately
		_, _ = w.Write(testData)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	destFile := filepath.Join(tempDir, "idle_test.bin")

	eng := New(
		WithIdleTimeout(100 * time.Millisecond),
		WithMaxRetries(2),
		WithRetryDelay(10 * time.Millisecond),
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
	}

	for _, tc := range tests {
		got := sanitizeFilename(tc.input)
		if got != tc.expected {
			t.Errorf("sanitizeFilename(%q) = %q; want %q", tc.input, got, tc.expected)
		}
	}

	// Test extractFilename with URL containing query string and fragment
	u := "https://example.com/downloads/archive.tar.gz?nocache=123#frag"
	extracted := extractFilename(u, u, "")
	if extracted != "archive.tar.gz" {
		t.Errorf("extractFilename() = %q; want archive.tar.gz", extracted)
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

