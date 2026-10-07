package agentmulticandidate

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"testing"
)

func TestMultiCandidatePureNilServiceNoAuthority(t *testing.T) {
	a := agentprofile.PrivateAccess{WorkspacePrincipal: actorref.PrincipalRef{Type: actorref.Person, ID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"}, SessionDigest: [32]byte{1}}
	s := NewService(nil, nil)
	id := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	if _, e := s.StageOwnMultiCandidate(context.Background(), a, id); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
	if _, e := s.ApproveOwnMultiCandidate(context.Background(), a, id); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
}
