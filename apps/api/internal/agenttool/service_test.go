package agenttool

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentplanner"
	"testing"
)

func TestAgentToolServiceNoNativeHandleNoAuthority(t *testing.T) {
	for _, s := range []*Service{nil, NewService(nil)} {
		if _, _, e := s.Read(context.Background(), agentevent.Access{}, agentplanner.ActionProposal{}, nil); !errors.Is(e, ErrUnavailable) {
			t.Fatal("absent native port didn't fail closed", e)
		}
		if _, _, e := s.Check(nil, agentevent.Access{}, agentplanner.ActionProposal{}, nil); !errors.Is(e, ErrInvalid) {
			t.Fatal("nil context accepted", e)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, e := NewService(nil).Read(ctx, agentevent.Access{}, agentplanner.ActionProposal{}, nil); !errors.Is(e, context.Canceled) {
		t.Fatal("cancel lost", e)
	}
	d := Decision{Disposition: Confirm}
	copy := Clone(d)
	if copy.Disposition != Confirm {
		t.Fatal("view clone changed")
	}
}
