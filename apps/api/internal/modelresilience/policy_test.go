package modelresilience

import (
	"errors"
	"testing"
	"time"
)

func TestPolicyBoundsAndDefaults(t *testing.T) {
	if ValidatePolicy(DefaultPolicy()) != nil {
		t.Fatal("default invalid")
	}
	for _, tc := range []struct {
		name   string
		change func(*Policy)
	}{
		{"zero_attempts", func(p *Policy) { p.MaxAttempts = 0 }}, {"unbounded_attempts", func(p *Policy) { p.MaxAttempts = 9 }},
		{"zero_elapsed", func(p *Policy) { p.MaxElapsed = 0 }}, {"elapsed_over_gateway", func(p *Policy) { p.MaxElapsed = 3 * time.Minute }},
		{"zero_attempt_deadline", func(p *Policy) { p.AttemptTimeout = 0 }}, {"attempt_over_elapsed", func(p *Policy) { p.AttemptTimeout = 2 * time.Minute }},
		{"zero_backoff", func(p *Policy) { p.BaseBackoff = 0 }}, {"negative_backoff", func(p *Policy) { p.BaseBackoff = -1 }},
		{"cap_less_base", func(p *Policy) { p.MaxBackoff = time.Millisecond }}, {"excess_cap", func(p *Policy) { p.MaxBackoff = 2 * time.Minute }},
		{"negative_jitter", func(p *Policy) { p.JitterPermille = -1 }}, {"excess_jitter", func(p *Policy) { p.JitterPermille = 1001 }},
		{"zero_route_attempts", func(p *Policy) { p.SameRouteAttempts = 0 }}, {"excess_route_attempts", func(p *Policy) { p.SameRouteAttempts = 5 }},
		{"negative_switch", func(p *Policy) { p.MaxProviderSwitches = -1 }}, {"excess_switch", func(p *Policy) { p.MaxProviderSwitches = 4 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := DefaultPolicy()
			tc.change(&p)
			if !errors.Is(ValidatePolicy(p), ErrInvalid) {
				t.Fatal("unbounded policy accepted")
			}
		})
	}
}

func TestRetryDelayJitterExponentialAndHintLowerBound(t *testing.T) {
	p := DefaultPolicy()
	for attempt := 1; attempt <= MaxAttempts; attempt++ {
		for _, unit := range []int{0, 250, 500, 750, 1000} {
			t.Run(time.Duration(attempt).String()+"/"+time.Duration(unit).String(), func(t *testing.T) {
				d, err := RetryDelay(p, attempt, 0, false, unit)
				if err != nil || d < p.BaseBackoff || d > p.MaxBackoff {
					t.Fatal("unsafe delay", d, err)
				}
			})
		}
	}
	if d, e := RetryDelay(p, 1, time.Second, true, 0); e != nil || d != time.Second {
		t.Fatal("hint lower bound ignored")
	}
	if d, e := RetryDelay(p, 1, 0, true, 1000); e != nil || d != 300*time.Millisecond {
		t.Fatal("jitter absent", d, e)
	}
	for _, hint := range []time.Duration{p.MaxBackoff + 1, time.Duration(1<<63 - 1)} {
		if _, e := RetryDelay(p, 1, hint, true, 0); !errors.Is(e, ErrBudget) {
			t.Fatal("oversized hint shortened")
		}
	}
	for _, tc := range []struct {
		failure, unit int
		hint          time.Duration
	}{{0, 0, 0}, {9, 0, 0}, {1, -1, 0}, {1, 1001, 0}, {1, 0, -1}} {
		if _, e := RetryDelay(p, tc.failure, tc.hint, true, tc.unit); !errors.Is(e, ErrInvalid) {
			t.Fatal("invalid scheduling accepted")
		}
	}
}
