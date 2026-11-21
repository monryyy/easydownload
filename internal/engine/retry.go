package engine

import (
	"errors"
	"fmt"
	"math"
	"math/rand"
	"net"
	"net/http"
	"time"
)

// fatalError marks a failure that must not be retried.
type fatalError struct{ msg string }

func (e *fatalError) Error() string { return e.msg }

func fatalf(format string, args ...interface{}) error {
	return &fatalError{msg: fmt.Sprintf(format, args...)}
}

// statusError carries an HTTP status code.
type statusError struct {
	code int
	msg  string
}

func (e *statusError) Error() string { return e.msg }

// isRetryable reports whether an error is worth retrying.
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	var fatal *fatalError
	if errors.As(err, &fatal) {
		return false
	}
	var status *statusError
	if errors.As(err, &status) {
		switch status.code {
		case http.StatusRequestTimeout, // 408
			http.StatusTooManyRequests,     // 429
			http.StatusInternalServerError, // 500
			http.StatusBadGateway,          // 502
			http.StatusServiceUnavailable,  // 503
			http.StatusGatewayTimeout:      // 504
			return true
		default:
			return false
		}
	}
	// Connection resets, DNS failures, timeouts, TLS handshake issues, etc.
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	var urlErr interface{ Unwrap() error }
	if errors.As(err, &urlErr) && urlErr.Unwrap() != nil {
		return isRetryable(urlErr.Unwrap())
	}
	// Unknown errors are treated as transient — the retry budget is small.
	return true
}

// backoff computes an exponential delay with full jitter.
func backoff(attempt int, initial, max time.Duration) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	delay := float64(initial) * math.Pow(2, float64(attempt))
	if delay > float64(max) || math.IsInf(delay, 0) {
		delay = float64(max)
	}
	// Full jitter: uniform in [0, delay].
	if delay <= 0 {
		return 0
	}
	return time.Duration(rand.Int63n(int64(delay) + 1))
}
