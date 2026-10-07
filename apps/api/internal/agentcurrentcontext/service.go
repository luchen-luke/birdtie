package agentcurrentcontext

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/contextgraph"
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
	h.Write([]byte("birdtie.current-context.ephemeral.v1\x00"))
	h.Write(body)
	return h.Sum(nil)
}
func buildSnapshot(r Request, state nativeState) (Snapshot, error) {
	if e := validateRequest(r, state.now); e != nil {
		return Snapshot{}, e
	}
	query, origin, pref := r.Query, "CURRENT_REQUEST", requestTimePreference(r.Query)
	var taskUpdated *time.Time
	if r.Selection == CurrentTask {
		if state.task == nil {
			return Snapshot{}, ErrDenied
		}
		query = state.task.Query
		origin = "AGENT_TASK"
		pref = state.task.Filters["timePreference"]
		taskUpdated = &state.task.UpdatedAt
		for i := len(state.task.Conversation) - 1; i >= 0; i-- {
			if state.task.Conversation[i].Role == "user" {
				query = state.task.Conversation[i].Text
				break
			}
		}
		if pref == "" {
			pref = requestTimePreference(query)
		}
	}
	if len(query) > MaxQueryBytes || query != strings.TrimSpace(query) || strings.ContainsAny(query, "\x00\r\n") {
		return Snapshot{}, ErrDenied
	}
	start, end, e := temporaryWindow(state.now, state.city.TimeZone, pref, taskUpdated)
	if e != nil {
		return Snapshot{}, e
	}
	expires := state.now.Add(MaxLease)
	for _, limit := range []time.Time{r.DeadlineAt, state.sessionLimit} {
		if limit.Before(expires) {
			expires = limit
		}
	}
	if state.city.SourceExpiresAt != nil && state.city.SourceExpiresAt.Before(expires) {
		expires = *state.city.SourceExpiresAt
	}
	if end != nil && end.Before(expires) {
		expires = *end
	}
	if !expires.After(state.now) {
		return Snapshot{}, ErrExpired
	}
	freshness := "unverified"
	if state.city.SourceVerifiedAt != nil {
		freshness = "current"
		if state.city.SourceVerifiedAt.Before(state.city.SourceUpdatedAt) {
			freshness = "review_needed"
		}
	}
	out := Snapshot{SchemaVersion: SchemaVersion, Agent: r.Agent, Purpose: "HUMAN_SELF_REVIEW", Scope: "CURRENT_CONTEXT", City: CityView{state.city.ID, state.city.Label, state.city.TimeZone, contextgraph.Ref{Type: contextgraph.City, ID: state.city.ContextID}, r.Selection, freshness}, Query: query, QueryOrigin: origin, TimePreference: pref, WindowStart: start, WindowEnd: end, ObservedAt: state.now.UTC(), ExpiresAt: expires.UTC(), Sources: append([]Source(nil), state.sources...), MemoryPromotionAllowed: false, ModelAccess: "UNAVAILABLE", selector: selectors(r), authority: state.authority, nativeDigest: sourceDigest(state.sources)}
	return out, nil
}
func (s *Service) ReadOwn(ctx context.Context, r Request) (Snapshot, error) {
	ticket, e := s.ticket(ctx)
	if e != nil {
		return Snapshot{}, e
	}
	generation := s.controller.Revision()
	if e = validateRequest(r, time.Now().UTC()); e != nil {
		return Snapshot{}, e
	}
	if e = s.native.authenticate(ctx, r); e != nil {
		return Snapshot{}, e
	}
	initial, e := s.native.load(ctx, r)
	if e != nil {
		return Snapshot{}, e
	}
	out, e := buildSnapshot(r, initial)
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
	if final.authority != initial.authority || sourceDigest(final.sources) != out.nativeDigest {
		return Snapshot{}, ErrDenied
	}
	out, e = buildSnapshot(r, final)
	if e != nil {
		return Snapshot{}, e
	}
	if e = ctx.Err(); e != nil {
		return Snapshot{}, e
	}
	if !s.controller.Current(ticket) {
		return Snapshot{}, ErrUnavailable
	}
	if !out.ExpiresAt.After(time.Now().UTC()) {
		return Snapshot{}, ErrExpired
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
	if len(snapshot.seal) != sha256.Size || !hmac.Equal(snapshot.seal, s.signature(snapshot)) || snapshot.MemoryPromotionAllowed || snapshot.ModelAccess != "UNAVAILABLE" || snapshot.generation != s.controller.Revision() {
		return Snapshot{}, ErrDenied
	}
	now := time.Now().UTC()
	if !snapshot.ExpiresAt.After(now) {
		return Snapshot{}, ErrExpired
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
	if current.authority != snapshot.authority || sourceDigest(current.sources) != snapshot.nativeDigest {
		return Snapshot{}, ErrDenied
	}
	// Revalidation never renews an old relative time, lease or source snapshot.
	if e = validateSnapshotClocks(snapshot, current.now, time.Now().UTC()); e != nil {
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

// ObservedAt is issued by PostgreSQL, so only the current PostgreSQL clock can
// establish whether it is in the future. Host time still bounds expiration;
// neither clock can extend the sealed original lease.
func validateSnapshotClocks(snapshot Snapshot, databaseNow, hostNow time.Time) error {
	if !validTime(databaseNow) || !validTime(hostNow) ||
		!validTime(snapshot.ObservedAt) || !validTime(snapshot.ExpiresAt) ||
		snapshot.ObservedAt.After(databaseNow) ||
		!snapshot.ExpiresAt.After(databaseNow) || !snapshot.ExpiresAt.After(hostNow) {
		return ErrExpired
	}
	return nil
}

// Ordinary self-review is not runtime/model/source-purpose permission. Even
// an actual native Snapshot or ON enrichment flag cannot unlock this port.
func (s *Service) ReadForCognition(context.Context, agentcognitive.ReadRequest) (agentcognitive.KnowledgeView, error) {
	return agentcognitive.KnowledgeView{}, agentcognitive.ErrUnavailable
}
