package engine

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

// FileInfo holds metadata discovered during the probe phase.
type FileInfo struct {
	URL           string
	FinalURL      string
	Filename      string
	ContentLength int64
	AcceptRanges  bool
	ETag          string
	LastModified  time.Time
	ContentType   string
	StatusCode    int
}

// Probe inspects a remote URL to discover its size, capabilities, and filename with automatic retries.
func Probe(ctx context.Context, rawURL string, opts *Options) (*FileInfo, error) {
	if strings.TrimSpace(rawURL) == "" {
		return nil, ErrEmptyURL
	}
	if opts == nil {
		opts = DefaultOptions()
	}

	var lastErr error
	for attempt := 0; attempt <= opts.MaxRetries; attempt++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		if attempt > 0 {
			backoff := opts.RetryDelay * time.Duration(1<<(attempt-1))
			if backoff > 5*time.Second {
				backoff = 5 * time.Second
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		probeCtx := ctx
		var probeCancel context.CancelFunc
		if opts.ProbeTimeout > 0 {
			probeCtx, probeCancel = context.WithTimeout(ctx, opts.ProbeTimeout)
		}

		info, err := attemptProbe(probeCtx, rawURL, opts)
		if probeCancel != nil {
			probeCancel()
		}

		if err == nil {
			return info, nil
		}

		if errors.Is(err, ErrNonRetryable) {
			return nil, err
		}

		lastErr = err
	}

	return nil, fmt.Errorf("probe failed: %w", lastErr)
}

func attemptProbe(ctx context.Context, rawURL string, opts *Options) (*FileInfo, error) {
	// First attempt: HEAD request
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating HEAD request: %w", err)
	}
	applyHeaders(req, opts)

	resp, err := opts.HTTPClient.Do(req)
	if err != nil || resp.StatusCode >= 400 || (resp.ContentLength <= 0 && !hasAcceptRanges(resp)) {
		if resp != nil {
			if resp.StatusCode == http.StatusUnauthorized ||
				resp.StatusCode == http.StatusForbidden ||
				resp.StatusCode == http.StatusNotFound ||
				resp.StatusCode == http.StatusGone {
				if resp.Body != nil {
					_ = resp.Body.Close()
				}
				return nil, fmt.Errorf("%w: HEAD probe returned HTTP status %d: %s", ErrNonRetryable, resp.StatusCode, resp.Status)
			}
			if resp.Body != nil {
				_ = resp.Body.Close()
			}
		}
		// Fallback attempt: GET request with Range: bytes=0-0
		return probeWithGetRange(ctx, rawURL, opts)
	}
	defer resp.Body.Close()

	info := parseResponseInfo(rawURL, resp)

	// If Accept-Ranges is not explicitly specified in HEAD, test with range probe
	if !info.AcceptRanges && info.ContentLength > 0 {
		testInfo, err := probeWithGetRange(ctx, rawURL, opts)
		if err == nil && testInfo.AcceptRanges {
			info.AcceptRanges = true
			if info.ContentLength <= 0 && testInfo.ContentLength > 0 {
				info.ContentLength = testInfo.ContentLength
			}
		}
	}

	return info, nil
}

func probeWithGetRange(ctx context.Context, rawURL string, opts *Options) (*FileInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating GET range probe: %w", err)
	}
	applyHeaders(req, opts)
	req.Header.Set("Range", "bytes=0-0")

	resp, err := opts.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing GET range probe: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized ||
		resp.StatusCode == http.StatusForbidden ||
		resp.StatusCode == http.StatusNotFound ||
		resp.StatusCode == http.StatusGone {
		return nil, fmt.Errorf("%w: range probe returned HTTP status %d: %s", ErrNonRetryable, resp.StatusCode, resp.Status)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("probe failed with HTTP status %d: %s", resp.StatusCode, resp.Status)
	}

	info := parseResponseInfo(rawURL, resp)
	if resp.StatusCode == http.StatusPartialContent { // 206
		info.AcceptRanges = true
		// Parse Content-Range header: "bytes 0-0/1073741824"
		if cr := resp.Header.Get("Content-Range"); cr != "" {
			if slashIdx := strings.LastIndex(cr, "/"); slashIdx != -1 {
				totalStr := strings.TrimSpace(cr[slashIdx+1:])
				if total, err := strconv.ParseInt(totalStr, 10, 64); err == nil && total > 0 {
					info.ContentLength = total
				}
			}
		}
	} else if resp.StatusCode == http.StatusOK { // 200 (Server ignored Range header)
		info.AcceptRanges = hasAcceptRanges(resp)
	}

	return info, nil
}

func parseResponseInfo(originalURL string, resp *http.Response) *FileInfo {
	finalURL := originalURL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}

	var cl int64 = resp.ContentLength
	if cl < 0 {
		cl = 0
	}

	acceptRanges := hasAcceptRanges(resp)

	var lastMod time.Time
	if lm := resp.Header.Get("Last-Modified"); lm != "" {
		if t, err := http.ParseTime(lm); err == nil {
			lastMod = t
		}
	}

	filename := extractFilename(originalURL, finalURL, resp.Header.Get("Content-Disposition"))

	return &FileInfo{
		URL:           originalURL,
		FinalURL:      finalURL,
		Filename:      filename,
		ContentLength: cl,
		AcceptRanges:  acceptRanges,
		ETag:          strings.Trim(resp.Header.Get("ETag"), `"`),
		LastModified:  lastMod,
		ContentType:   resp.Header.Get("Content-Type"),
		StatusCode:    resp.StatusCode,
	}
}

func hasAcceptRanges(resp *http.Response) bool {
	ar := strings.ToLower(resp.Header.Get("Accept-Ranges"))
	return strings.Contains(ar, "bytes")
}

func sanitizeFilename(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "\\", "/")
	name = path.Base(name)

	// Replace illegal Windows and Unix path characters
	var sb strings.Builder
	for _, r := range name {
		if r < 32 || r == '<' || r == '>' || r == ':' || r == '"' || r == '/' || r == '\\' || r == '|' || r == '?' || r == '*' {
			sb.WriteRune('_')
		} else {
			sb.WriteRune(r)
		}
	}
	sanitized := strings.TrimSpace(sb.String())
	sanitized = strings.TrimRight(sanitized, ". ")

	if sanitized == "" || sanitized == "." || sanitized == ".." {
		return "downloaded_file"
	}
	return sanitized
}


func extractFilename(originalURL, finalURL, contentDisposition string) string {
	// Try Content-Disposition header first
	if contentDisposition != "" {
		if _, params, err := mime.ParseMediaType(contentDisposition); err == nil {
			if fn, ok := params["filename*"]; ok && fn != "" {
				// Handle RFC 5987 (e.g. UTF-8''filename)
				if idx := strings.Index(fn, "''"); idx != -1 {
					fn = fn[idx+2:]
				}
				if unescaped, err := url.PathUnescape(fn); err == nil {
					fn = unescaped
				}
				if s := sanitizeFilename(fn); s != "downloaded_file" {
					return s
				}
			}
			if fn, ok := params["filename"]; ok && fn != "" {
				if unescaped, err := url.PathUnescape(fn); err == nil {
					fn = unescaped
				}
				if s := sanitizeFilename(fn); s != "downloaded_file" {
					return s
				}
			}
		}
	}

	// Fallback to URL path
	for _, targetURL := range []string{finalURL, originalURL} {
		if parsed, err := url.Parse(targetURL); err == nil {
			cleaned := path.Base(parsed.Path)
			if cleaned != "" && cleaned != "." && cleaned != "/" {
				if unescaped, err := url.PathUnescape(cleaned); err == nil {
					cleaned = unescaped
				}
				if s := sanitizeFilename(cleaned); s != "downloaded_file" {
					return s
				}
			}
		}
	}

	return "downloaded_file"
}

func applyHeaders(req *http.Request, opts *Options) {
	if opts.UserAgent != "" {
		req.Header.Set("User-Agent", opts.UserAgent)
	}
	for k, v := range opts.Headers {
		req.Header.Set(k, v)
	}
}

