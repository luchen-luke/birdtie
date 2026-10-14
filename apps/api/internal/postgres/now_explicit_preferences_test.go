package postgres

import (
	"context"
	"errors"
	"testing"

	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
)

func TestNowPreferenceNativeEntryRejectsMissingAuthority(t *testing.T) {
	for _, store := range []*Store{nil, {}} {
		if _, e := store.ExecuteOwnNowPreference(context.Background(), arp.Access{}); !errors.Is(e, arp.ErrDenied) {
			t.Fatalf("missing authority returned %v", e)
		}
	}
	c, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := (&Store{}).ExecuteOwnNowPreference(c, arp.Access{}); !errors.Is(e, arp.ErrDenied) {
		t.Fatalf("cancelled entry returned %v", e)
	}
}
