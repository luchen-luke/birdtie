package modelgateway

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// MaxRetryAfter is a parsing limit, not permission to wait this long. The
// caller must reject a hint beyond its remaining deadline instead of shortening
// it and retrying before the provider's earliest time.
const MaxRetryAfter = 24 * time.Hour

var ErrRetryAfter = errors.New("invalid model retry-after hint")

// retryAfterError keeps only a normalized whitelist error and a duration. It
// never retains a header, response body, URL, original error or private input.
// Keeping ProviderError unchanged preserves its two-field literal contract.
type retryAfterError struct {
	provider ProviderError
	delay    time.Duration
	valid    bool
}

func (e retryAfterError) Error() string { return e.provider.Error() }
func (e retryAfterError) Unwrap() error { return e.provider }

// ParseRetryAfter accepts one bounded delta-seconds or HTTP-date value. The
// receipt time is an adapter's server clock, never the provider's Date header.
func ParseRetryAfter(value string, receivedAt time.Time) (time.Duration, error) {
	if len(value) == 0 || len(value) > 80 || strings.ContainsAny(value, "\r\n\x00") || receivedAt.IsZero() || receivedAt.UTC().Year() < 1 || receivedAt.UTC().Year() > 9999 {
		return 0, ErrRetryAfter
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, ErrRetryAfter
	}
	digits := true
	for _, c := range value {
		if c < '0' || c > '9' {
			digits = false
			break
		}
	}
	if digits {
		seconds, err := strconv.ParseUint(value, 10, 64)
		if err != nil || seconds > uint64(MaxRetryAfter/time.Second) {
			return 0, ErrRetryAfter
		}
		return time.Duration(seconds) * time.Second, nil
	}
	at, err := http.ParseTime(value)
	if err != nil || at.UTC().Year() < 1 || at.UTC().Year() > 9999 {
		return 0, ErrRetryAfter
	}
	if !at.After(receivedAt) {
		return 0, nil
	}
	delay := at.Sub(receivedAt)
	if delay > MaxRetryAfter {
		return 0, ErrRetryAfter
	}
	return delay, nil
}

// NewRetryAfterError is an adapter metadata constructor, not a retry grant.
// Malformed hints remain present-but-invalid so callers stop rather than
// silently ignoring a lower bound. Unknown/terminal codes cannot carry a hint.
func NewRetryAfterError(provider ProviderError, delay time.Duration) error {
	provider = normalizedProviderError(provider)
	if !provider.Retryable {
		return provider
	}
	valid := delay >= 0 && delay <= MaxRetryAfter
	if !valid {
		delay = 0
	}
	return retryAfterError{provider: provider, delay: delay, valid: valid}
}

func NewRetryAfterHeaderError(provider ProviderError, value string, receivedAt time.Time) error {
	delay, err := ParseRetryAfter(value, receivedAt)
	if err != nil {
		return NewRetryAfterError(provider, -1)
	}
	return NewRetryAfterError(provider, delay)
}

// RetryAfter reads only this package's safe wrapper. Arbitrary adapter methods
// and headers cannot add trusted retry metadata. valid=false requires stopping.
func RetryAfter(err error) (delay time.Duration, present bool, valid bool) {
	var hint retryAfterError
	if !errors.As(err, &hint) || !hint.provider.Retryable || (hint.provider.Code != "RATE_LIMIT" && hint.provider.Code != "TEMPORARY") {
		return 0, false, false
	}
	return hint.delay, true, hint.valid
}

func normalizedProviderFailure(original error, provider ProviderError) error {
	if provider.Retryable {
		var wrapped retryAfterError
		if errors.As(original, &wrapped) && wrapped.provider.Code != provider.Code {
			return NewRetryAfterError(provider, -1)
		}
		delay, present, valid := RetryAfter(original)
		if present {
			if !valid {
				delay = -1
			}
			return NewRetryAfterError(provider, delay)
		}
	}
	return provider
}
