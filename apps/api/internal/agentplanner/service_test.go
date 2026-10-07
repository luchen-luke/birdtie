package agentplanner

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"testing"
)

func TestBoundedPlannerDefaultUnavailablePorts(t *testing.T) {
	for _, s := range []*Service{nil, NewService(nil, nil), NewService(UnavailablePorts{}, UnavailablePorts{})} {
		if _, e := s.Prepare(context.Background(), agentevent.Access{}, testID, testID); !errors.Is(e, ErrUnavailable) {
			t.Fatal(e)
		}
		if _, e := s.Review(context.Background(), agentprofile.PrivateAccess{}, CandidateSelection{LogicalOperationID: testID}); !errors.Is(e, ErrUnavailable) {
			t.Fatal(e)
		}
	}
	c, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := NewService(nil, nil).Prepare(c, agentevent.Access{}, testID, testID); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}
