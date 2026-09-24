package engine

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net"
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

func validateDownloadURL(rawURL string, allowPrivate bool) error {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("%w: URL must use http or https and include a host", ErrInvalidURL)
	}
	if !allowPrivate {
		if err := validateHostAddresses(context.Background(), parsed.Hostname(), allowPrivate); err != nil {
			return err
		}
	}
	return nil
}

func validateHostAddresses(ctx context.Context, host string, allowPrivate bool) error {
	if allowPrivate {
		return nil
	}
	if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil {
		if isPrivateIP(ip) {
			return fmt.Errorf("%w: %s", ErrPrivateNetworkAccess, host)
		}
		return nil
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("resolving download host %q: %w", host, err)
	}
	if len(addresses) == 0 {
		return fmt.Errorf("resolving download host %q: no addresses returned", host)
	}
	for _, address := range addresses {
		if isPrivateIP(address.IP) {
			return fmt.Errorf("%w: %s resolves to %s", ErrPrivateNetworkAccess, host, address.IP)
		}
	}
	return nil
}

func isPrivateIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast()
}

// Probe inspects a remote URL to discover its size, capabilities, and filename with automatic retries.
func Probe(ctx context.Context, rawURL string, opts *Options) (*FileInfo, error) {
	if strings.TrimSpace(rawURL) == "" {
		return nil, ErrEmptyURL
	}
	if opts == nil {
		opts = DefaultOptions()
	}
	if err := validateDownloadURL(rawURL, opts.AllowPrivateHosts); err != nil {
		return nil, err
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

	contentType := resp.Header.Get("Content-Type")
	filename := extractFilename(originalURL, finalURL, resp.Header.Get("Content-Disposition"), contentType)

	return &FileInfo{
		URL:           originalURL,
		FinalURL:      finalURL,
		Filename:      filename,
		ContentLength: cl,
		AcceptRanges:  acceptRanges,
		ETag:          strings.Trim(resp.Header.Get("ETag"), `"`),
		LastModified:  lastMod,
		ContentType:   contentType,
		StatusCode:    resp.StatusCode,
	}
}

func hasAcceptRanges(resp *http.Response) bool {
	ar := strings.ToLower(resp.Header.Get("Accept-Ranges"))
	return strings.Contains(ar, "bytes")
}

func inferExtension(contentType string) string {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	if idx := strings.Index(ct, ";"); idx != -1 {
		ct = strings.TrimSpace(ct[:idx])
	}
	switch ct {
	case "video/mp4":
		return ".mp4"
	case "video/webm":
		return ".webm"
	case "video/x-matroska":
		return ".mkv"
	case "video/quicktime":
		return ".mov"
	case "video/x-flv":
		return ".flv"
	case "audio/mpeg", "audio/mp3":
		return ".mp3"
	case "audio/wav", "audio/x-wav":
		return ".wav"
	case "audio/flac":
		return ".flac"
	case "audio/aac":
		return ".aac"
	case "audio/ogg":
		return ".ogg"
	case "application/pdf":
		return ".pdf"
	case "application/zip", "application/x-zip-compressed":
		return ".zip"
	case "application/x-rar-compressed", "application/vnd.rar":
		return ".rar"
	case "application/x-7z-compressed":
		return ".7z"
	case "application/x-tar":
		return ".tar"
	case "application/gzip", "application/x-gzip":
		return ".tar.gz"
	case "application/vnd.android.package-archive":
		return ".apk"
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "image/svg+xml":
		return ".svg"
	case "text/plain":
		return ".txt"
	case "text/html":
		return ".html"
	case "application/json":
		return ".json"
	}
	exts, err := mime.ExtensionsByType(ct)
	if err == nil && len(exts) > 0 {
		return exts[0]
	}
	return ""
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
	if isWindowsReservedName(sanitized) {
		return "_" + sanitized
	}
	return sanitized
}

func isWindowsReservedName(name string) bool {
	stem := name
	if dot := strings.IndexByte(stem, '.'); dot >= 0 {
		stem = stem[:dot]
	}
	stem = strings.TrimRight(stem, ". ")
	upper := strings.ToUpper(stem)
	if upper == "CON" || upper == "PRN" || upper == "AUX" || upper == "NUL" {
		return true
	}
	if len(upper) == 4 && (strings.HasPrefix(upper, "COM") || strings.HasPrefix(upper, "LPT")) && upper[3] >= '1' && upper[3] <= '9' {
		return true
	}
	return false
}

func extractFilename(originalURL, finalURL, contentDisposition, contentType string) string {
	// 1. Try Content-Disposition header first
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
					return ensureExtension(s, contentType)
				}
			}
			if fn, ok := params["filename"]; ok && fn != "" {
				if unescaped, err := url.PathUnescape(fn); err == nil {
					fn = unescaped
				}
				if s := sanitizeFilename(fn); s != "downloaded_file" {
					return ensureExtension(s, contentType)
				}
			}
		}
	}

	// 2. Check query parameters on finalURL and originalURL (common on AWS S3, Azure, CDNs)
	for _, targetURL := range []string{finalURL, originalURL} {
		if parsed, err := url.Parse(targetURL); err == nil {
			q := parsed.Query()
			// Check response-content-disposition query parameter
			if rcd := q.Get("response-content-disposition"); rcd != "" {
				if _, params, err := mime.ParseMediaType(rcd); err == nil {
					if fn := params["filename"]; fn != "" {
						if unescaped, err := url.PathUnescape(fn); err == nil {
							fn = unescaped
						}
						if s := sanitizeFilename(fn); s != "downloaded_file" {
							return ensureExtension(s, contentType)
						}
					}
				}
			}
			for _, key := range []string{"filename", "file_name", "file", "name", "title"} {
				if val := q.Get(key); val != "" {
					if unescaped, err := url.PathUnescape(val); err == nil {
						val = unescaped
					}
					if s := sanitizeFilename(val); s != "downloaded_file" && strings.Contains(s, ".") {
						return ensureExtension(s, contentType)
					}
				}
			}
		}
	}

	// 3. Fallback to URL path
	for _, targetURL := range []string{finalURL, originalURL} {
		if parsed, err := url.Parse(targetURL); err == nil {
			cleaned := path.Base(parsed.Path)
			if cleaned != "" && cleaned != "." && cleaned != "/" {
				if unescaped, err := url.PathUnescape(cleaned); err == nil {
					cleaned = unescaped
				}
				if s := sanitizeFilename(cleaned); s != "downloaded_file" {
					return ensureExtension(s, contentType)
				}
			}
		}
	}

	ext := inferExtension(contentType)
	if ext != "" {
		return "downloaded_file" + ext
	}
	return "downloaded_file"
}

func ensureExtension(filename, contentType string) string {
	ext := path.Ext(filename)
	if ext == "" || ext == ".bin" || ext == ".tmp" {
		inferred := inferExtension(contentType)
		if inferred != "" && inferred != ext {
			if ext == "" {
				return filename + inferred
			}
			return strings.TrimSuffix(filename, ext) + inferred
		}
	}
	return filename
}

func applyHeaders(req *http.Request, opts *Options) {
	if opts.UserAgent != "" {
		req.Header.Set("User-Agent", opts.UserAgent)
	}
	for k, v := range opts.Headers {
		req.Header.Set(k, v)
	}
}
