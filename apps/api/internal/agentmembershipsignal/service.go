package agentmembershipsignal

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"

	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	native      *nativeResolver
	controller  *agentfeature.Controller
	key         [32]byte
	beforeFinal func()
}

// NewService binds a real native database and actual server rollout controller.
// developmentSessions is server startup configuration, never request JSON.
// No resolver/fake injection or model/provider port exists on this service.
func NewService(pool *pgxpool.Pool, controller *agentfeature.Controller, developmentSessions bool) (*Service, error) {
	if pool == nil || controller == nil {
		return nil, ErrUnavailable
	}
	s := &Service{native: &nativeResolver{pool, postgres.New(pool, developmentSessions), developmentSessions}, controller: controller}
	if _, e := rand.Read(s.key[:]); e != nil {
		return nil, ErrUnavailable
	}
	return s, nil
}
func (s *Service) ticket(ctx context.Context) (agentfeature.Ticket, error) {
	if ctx == nil {
		return agentfeature.Ticket{}, ErrInvalid
	}
	if e := ctx.Err(); e != nil {
		return agentfeature.Ticket{}, e
	}
	if s == nil || s.native == nil || s.controller == nil {
		return agentfeature.Ticket{}, ErrUnavailable
	}
	t, e := s.controller.Capture(agentfeature.Enrichment)
	if e != nil {
		return agentfeature.Ticket{}, ErrUnavailable
	}
	return t, nil
}
func (s *Service) signature(snapshot Snapshot) []byte {
	snapshot.seal = nil
	body, _ := json.Marshal(struct {
		Snapshot     Snapshot
		Selector     requestSelectors
		Authority    string
		NativeDigest string
		Generation   uint64
	}{snapshot, snapshot.selector, snapshot.authority, snapshot.nativeDigest, snapshot.generation})
	h := hmac.New(sha256.New, s.key[:])
	h.Write([]byte("birdtie.membership-context.ephemeral.v1\x00"))
	h.Write(body)
	return h.Sum(nil)
}
func (s *Service) ReadOwnMembershipContext(ctx context.Context, r Request) (Snapshot, error) {
	ticket, e := s.ticket(ctx)
	if e != nil {
		return Snapshot{}, e
	}
	generation := s.controller.Revision()
	now, e := s.native.clock(ctx)
	if e != nil {
		return Snapshot{}, e
	}
	if e = validateRequest(r, now); e != nil {
		return Snapshot{}, e
	}
	if e = s.native.authenticate(ctx, r); e != nil {
		return Snapshot{}, e
	}
	initial, e := s.native.load(ctx, r)
	if e != nil {
		return Snapshot{}, e
	}
	out, e := assembleContext(r, initial)
	if e != nil {
		return Snapshot{}, e
	}
	if s.beforeFinal != nil {
		s.beforeFinal()
	}
	// This second payload SELECT uses the current Read Committed statement
	// snapshot; changed/removed/re-created sources or session/Agent/metadata fail.
	final, e := s.native.load(ctx, r)
	if e != nil {
		return Snapshot{}, e
	}
	if final.authority != initial.authority || evidenceDigest([]Evidence{final.fact}) != out.nativeDigest {
		return Snapshot{}, ErrDenied
	}
	out, e = assembleContext(r, final)
	if e != nil {
		return Snapshot{}, e
	}
	if e = ctx.Err(); e != nil {
		return Snapshot{}, e
	}
	now, e = s.native.clock(ctx)
	if e != nil {
		return Snapshot{}, e
	}
	if e = snapshotTimeAt(out, now); e != nil {
		return Snapshot{}, e
	}
	if e = ctx.Err(); e != nil {
		return Snapshot{}, e
	}
	if !s.controller.Current(ticket) {
		return Snapshot{}, ErrUnavailable
	}
	out.generation = generation
	out.seal = s.signature(out)
	return cloneSnapshot(out), nil
}
func (s *Service) RevalidateOwn(ctx context.Context, access agentprofile.PrivateAccess, snapshot Snapshot) (Snapshot, error) {
	ticket, e := s.ticket(ctx)
	if e != nil {
		return Snapshot{}, e
	}
	if len(snapshot.seal) != sha256.Size || !hmac.Equal(snapshot.seal, s.signature(snapshot)) || snapshot.MemoryPromotionAllowed || snapshot.InterestInferred || snapshot.IdentityVerified || snapshot.FriendEstablished || snapshot.ModelAccess != "UNAVAILABLE" || snapshot.generation != s.controller.Revision() {
		return Snapshot{}, ErrDenied
	}
	now, e := s.native.clock(ctx)
	if e != nil {
		return Snapshot{}, e
	}
	if e = snapshotTimeAt(snapshot, now); e != nil {
		return Snapshot{}, e
	}
	r := fromSelectors(snapshot.selector, access)
	if e = validateRequest(r, now); e != nil {
		return Snapshot{}, e
	}
	if e = s.native.authenticate(ctx, r); e != nil {
		return Snapshot{}, e
	}
	current, e := s.native.load(ctx, r)
	if e != nil {
		return Snapshot{}, e
	}
	if current.authority != snapshot.authority || evidenceDigest([]Evidence{current.fact}) != snapshot.nativeDigest {
		return Snapshot{}, ErrDenied
	}
	// Revalidation never renews an old relative time, lease or source snapshot.
	if e = snapshotTimeAt(snapshot, current.now); e != nil {
		return Snapshot{}, e
	}
	now, e = s.native.clock(ctx)
	if e != nil {
		return Snapshot{}, e
	}
	if e = snapshotTimeAt(snapshot, now); e != nil {
		return Snapshot{}, e
	}
	if e = ctx.Err(); e != nil {
		return Snapshot{}, e
	}
	if !s.controller.Current(ticket) {
		return Snapshot{}, ErrUnavailable
	}
	return cloneSnapshot(snapshot), nil
}

// Ordinary self-review is not runtime/model/source-purpose permission. Even
// an actual native Snapshot or ON enrichment flag cannot unlock this port.
func (s *Service) ReadForCognition(context.Context, agentcognitive.ReadRequest) (agentcognitive.KnowledgeView, error) {
	return agentcognitive.KnowledgeView{}, agentcognitive.ErrUnavailable
}
