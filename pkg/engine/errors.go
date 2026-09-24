package engine

import "errors"

var (
	// ErrRangeNotSupported indicates the remote server does not support HTTP Range requests.
	ErrRangeNotSupported = errors.New("remote server does not support byte range requests")

	// ErrInvalidRange indicates that an invalid byte range was requested or returned.
	ErrInvalidRange = errors.New("invalid byte range")

	// ErrDownloadCanceled indicates the download was canceled via context.
	ErrDownloadCanceled = errors.New("download canceled by caller")

	// ErrEmptyURL indicates that the provided URL is empty.
	ErrEmptyURL = errors.New("download URL cannot be empty")

	// ErrInvalidURL indicates that the provided URL is malformed or uses an unsupported scheme.
	ErrInvalidURL = errors.New("invalid download URL")

	// ErrPrivateNetworkAccess indicates that the URL resolves to a private or local network address.
	ErrPrivateNetworkAccess = errors.New("download URL targets a private or local network address")

	// ErrZeroContentLength indicates that the remote resource reported 0 bytes.
	ErrZeroContentLength = errors.New("resource content length is 0")

	// ErrMaxRetriesExceeded indicates that a chunk failed to download after all retries.
	ErrMaxRetriesExceeded = errors.New("maximum retries exceeded for chunk")

	// ErrTimeout indicates an HTTP connection, response header, or read idle timeout occurred.
	ErrTimeout = errors.New("network connection or read idle timeout")

	// ErrNonRetryable indicates an error that should not be retried (e.g. 401, 403, 404).
	ErrNonRetryable = errors.New("non-retryable HTTP error")
)
