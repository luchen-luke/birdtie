package modelgateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestRetryAfterHeaderParsingIsBounded(t *testing.T) {
	now := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, input string
		delay       time.Duration
		valid       bool
	}{
		{"zero", "0", 0, true}, {"seconds", "12", 12 * time.Second, true}, {"whitespace", " 1 ", time.Second, true},
		{"limit", "86400", MaxRetryAfter, true}, {"future_date", now.Add(9 * time.Second).Format(http.TimeFormat), 9 * time.Second, true},
		{"past_date", now.Add(-time.Second).Format(http.TimeFormat), 0, true},
		{"empty", "", 0, false}, {"spaces", "  ", 0, false}, {"negative", "-1", 0, false}, {"plus", "+1", 0, false},
		{"fraction", "1.5", 0, false}, {"overflow", strings.Repeat("9", 80), 0, false}, {"over_limit", "86401", 0, false},
		{"multiple", "1, 2", 0, false}, {"private", "private API-key input", 0, false}, {"crlf", "1\r\nsecret", 0, false},
		{"long", strings.Repeat("0", 81), 0, false}, {"far_date", now.Add(25 * time.Hour).Format(http.TimeFormat), 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseRetryAfter(tc.input, now)
			if (err == nil) != tc.valid || got != tc.delay {
				t.Fatalf("got %v/%v", got, err)
			}
		})
	}
	for _, now := range []time.Time{{}, time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)} {
		if _, err := ParseRetryAfter("1", now); !errors.Is(err, ErrRetryAfter) {
			t.Fatal("invalid receipt clock accepted")
		}
	}
}

func TestRetryAfterWhitelistPreservesLegacyProviderError(t *testing.T) {
	for _, code := range []string{"RATE_LIMIT", "TEMPORARY", "REFUSED", "AUTHENTICATION", "INVALID_REQUEST", "UNKNOWN", "private-secret"} {
		t.Run(code, func(t *testing.T) {
			original := NewRetryAfterError(ProviderError{code, true}, 10*time.Millisecond)
			normal := normalizedProviderFailure(fmt.Errorf("private original: %w", original), normalizedProviderError(original))
			var provider ProviderError
			if !errors.As(normal, &provider) {
				t.Fatal("ProviderError compatibility lost")
			}
			delay, present, valid := RetryAfter(normal)
			want := code == "RATE_LIMIT" || code == "TEMPORARY"
			if present != want || (want && (!valid || delay != 10*time.Millisecond || !provider.Retryable)) || strings.Contains(normal.Error(), "private") {
				t.Fatal("unsafe hint/code", normal)
			}
		})
	}
	for _, d := range []time.Duration{-1, MaxRetryAfter + 1} {
		err := normalizedProviderFailure(NewRetryAfterError(ProviderError{"RATE_LIMIT", false}, d), ProviderError{"RATE_LIMIT", true})
		if delay, present, valid := RetryAfter(err); delay != 0 || !present || valid {
			t.Fatal("invalid hint was silently dropped")
		}
	}
	if _, present, _ := RetryAfter(ProviderError{"RATE_LIMIT", true}); present {
		t.Fatal("missing hint fabricated")
	}
	for _, e := range []error{context.Canceled, context.DeadlineExceeded} {
		err := normalizedProviderFailure(errors.Join(e, NewRetryAfterError(ProviderError{"RATE_LIMIT", true}, time.Second)), normalizedProviderError(e))
		if _, present, _ := RetryAfter(err); present {
			t.Fatal("terminal cancellation gained retry hint")
		}
	}
	conflict := errors.Join(ProviderError{"RATE_LIMIT", true}, NewRetryAfterError(ProviderError{"TEMPORARY", true}, time.Second))
	err := normalizedProviderFailure(conflict, normalizedProviderError(conflict))
	if _, present, valid := RetryAfter(err); !present || valid {
		t.Fatal("conflicting error hint accepted or silently ignored")
	}
}

func TestOfflineHarnessRetainsOnlySafeRetryMetadata(t *testing.T) {
	for _, input := range []string{"1", "0", "private-header", "999999999999999999999"} {
		t.Run(input, func(t *testing.T) {
			h, a := harness(t, "")
			a.err = fmt.Errorf("raw-key-secret: %w", NewRetryAfterHeaderError(ProviderError{"RATE_LIMIT", false}, input, testNow()))
			result, err := h.Complete(context.Background(), testRequest(Text))
			var p ProviderError
			if !errors.As(err, &p) || p.Code != "RATE_LIMIT" || !p.Retryable || result.Text != "" || a.calls != 1 {
				t.Fatal("core normalization bypassed")
			}
			_, present, valid := RetryAfter(err)
			if !present || valid != (input == "1" || input == "0") || strings.Contains(err.Error(), "raw-key") || strings.Contains(err.Error(), input) && input != "0" && input != "1" {
				t.Fatal("raw header retained", err)
			}
		})
	}
}
