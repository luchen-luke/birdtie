package socialintent

import (
	"testing"
	"time"
)

func TestLifecycleTransitionsAndReadTimeExpiry(t *testing.T) {
	for _, tc := range []struct {
		from, to string
		allowed  bool
	}{
		{Draft, Active, true}, {Draft, Cancelled, true},
		{Active, Matched, true}, {Active, Cancelled, true},
		{Matched, Converted, true}, {Matched, Cancelled, true},
		{Draft, Converted, false}, {Active, Converted, false},
		{Cancelled, Active, false}, {Expired, Active, false},
		{Converted, Active, false},
	} {
		if got := CanTransition(tc.from, tc.to); got != tc.allowed {
			t.Fatalf("%s -> %s: got %t", tc.from, tc.to, got)
		}
	}
	now := time.Now()
	for _, status := range []string{Draft, Active, Matched} {
		if got := EffectiveStatus(status, now.Add(-time.Second), now); got != Expired {
			t.Fatalf("stale %s remained %s", status, got)
		}
		if got := EffectiveStatus(status, now.Add(time.Second), now); got != status {
			t.Fatalf("current %s became %s", status, got)
		}
	}
	for _, status := range []string{Cancelled, Converted} {
		if got := EffectiveStatus(status, now.Add(-time.Second), now); got != status {
			t.Fatalf("terminal %s changed to %s", status, got)
		}
	}
}
