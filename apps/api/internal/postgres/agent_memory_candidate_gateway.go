package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"reflect"
	"regexp"
	"sync"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentconfidence"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemorycandidate"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
)

const humanCandidatePreviewCapacity = 128

var humanPreviewID = regexp.MustCompile(`^[0-9a-f]{64}$`)

type humanCandidateHeld struct {
	access      agentprofile.PrivateAccess
	candidateID string
	input       agentmemorycandidate.HumanPreviewInput
	preview     *MemoryCandidatePreview
}
type memoryCandidateHumanGateway struct {
	service *MemoryCandidateService
	mu      sync.Mutex
	held    map[string]humanCandidateHeld
	spent   map[string]bool
}

// The caller supplies the SAME trusted startup Controller. A nil/default-OFF
// controller never becomes ON here. This cache is not a grant or another ledger.
func NewMemoryCandidateHumanGateway(s *Store, f *agentfeature.Controller) agentmemorycandidate.HumanGateway {
	return &memoryCandidateHumanGateway{service: NewMemoryCandidateService(s, f), held: map[string]humanCandidateHeld{}, spent: map[string]bool{}}
}
func (g *memoryCandidateHumanGateway) List(ctx context.Context, a agentprofile.PrivateAccess) ([]agentmemorycandidate.Record, error) {
	return g.service.ListOwnCandidates(ctx, a)
}
func (g *memoryCandidateHumanGateway) Read(ctx context.Context, a agentprofile.PrivateAccess, id string) (agentmemorycandidate.Record, error) {
	return g.service.ReadOwnCandidate(ctx, a, id)
}
func humanCandidateUUID() (string, error) {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		return "", agentmemory.ErrUnavailable
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	v := hex.EncodeToString(b[:])
	return fmt.Sprintf("%s-%s-%s-%s-%s", v[:8], v[8:12], v[12:16], v[16:20], v[20:]), nil
}
func (g *memoryCandidateHumanGateway) Save(ctx context.Context, a agentprofile.PrivateAccess, d agentmemorycandidate.HumanDraft) (agentmemorycandidate.Record, error) {
	if agentmemorycandidate.Statement(d.Category) == "" {
		return agentmemorycandidate.Record{}, agentmemory.ErrInvalid
	}
	id, e := humanCandidateUUID()
	if e != nil {
		return agentmemorycandidate.Record{}, e
	}
	assessment, _ := agentconfidence.NewOrdinal(agentconfidence.Low)
	return g.service.SaveOwnCandidate(ctx, a, id, agentmemorycandidate.Draft{Predicate: "ACTIVITY_CATEGORY", Category: d.Category, Assessment: assessment, Sources: d.Sources, ValidUntil: d.ValidUntil}, "", 0)
}
func (g *memoryCandidateHumanGateway) clean() {
	for id, v := range g.held {
		if !v.preview.review.ExpiresAt.After(time.Now()) {
			delete(g.held, id)
			g.spent[id] = true
		}
	}
}
func (g *memoryCandidateHumanGateway) Preview(ctx context.Context, a agentprofile.PrivateAccess, id string, in agentmemorycandidate.HumanPreviewInput) (agentmemorycandidate.HumanPreview, error) {
	fail := func(e error) (agentmemorycandidate.HumanPreview, error) {
		return agentmemorycandidate.HumanPreview{}, e
	}
	if !humanPreviewID.MatchString(in.PreviewID) || in.ExpectedVersion < 1 || agentprofile.ValidatePrivateAccess(a) != nil {
		return fail(agentmemory.ErrInvalid)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.clean()
	if g.spent[in.PreviewID] {
		return fail(agentmemory.ErrConflict)
	}
	if h, ok := g.held[in.PreviewID]; ok {
		if h.access != a || h.candidateID != id || !reflect.DeepEqual(h.input, in) {
			return fail(agentmemory.ErrConflict)
		}
		r, e := g.service.ReadOwnCandidate(ctx, a, id)
		if e != nil {
			return fail(e)
		}
		if !reflect.DeepEqual(r, h.preview.review.Candidate) {
			return fail(agentmemory.ErrConflict)
		}
		return agentmemorycandidate.HumanPreview{PreviewID: in.PreviewID, Review: h.preview.Review()}, nil
	}
	if len(g.held) >= humanCandidatePreviewCapacity || len(g.spent) >= 8192 {
		return fail(agentmemory.ErrUnavailable)
	}
	own := 0
	for _, h := range g.held {
		if h.access == a {
			own++
		}
	}
	if own >= 8 {
		return fail(agentmemory.ErrConflict)
	}
	memoryID, e := humanCandidateUUID()
	if e != nil {
		return fail(e)
	}
	p, e := g.service.PreviewOwnAcceptance(ctx, a, id, in.ExpectedVersion, memoryID, 0, in.MemoryValidUntil)
	if e != nil {
		return fail(e)
	}
	g.held[in.PreviewID] = humanCandidateHeld{a, id, in, p}
	return agentmemorycandidate.HumanPreview{PreviewID: in.PreviewID, Review: p.Review()}, nil
}
func (g *memoryCandidateHumanGateway) Accept(ctx context.Context, a agentprofile.PrivateAccess, id, previewID string) (agentmemorycandidate.Record, error) {
	if !humanPreviewID.MatchString(previewID) {
		return agentmemorycandidate.Record{}, agentmemory.ErrInvalid
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.clean()
	h, ok := g.held[previewID]
	if !ok || h.access != a || h.candidateID != id {
		return agentmemorycandidate.Record{}, agentmemory.ErrConflict
	}
	// Keep the original sealed plan after success for same-key retry. Native
	// acceptance checks current session, all sources, CAS and original deadline.
	return g.service.AcceptOwnCandidate(ctx, a, h.preview)
}
func (g *memoryCandidateHumanGateway) Reject(ctx context.Context, a agentprofile.PrivateAccess, id string, v int64) (agentmemorycandidate.Record, error) {
	return g.service.RejectOwnCandidate(ctx, a, id, v)
}
