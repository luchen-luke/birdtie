package agentcontextbuilder

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
)

type Service struct {
	store Store
	key   [32]byte
}

func NewService(store Store) (*Service, error) {
	if store == nil {
		return nil, ErrUnavailable
	}
	s := &Service{store: store}
	if _, e := rand.Read(s.key[:]); e != nil {
		return nil, ErrUnavailable
	}
	return s, nil
}
func (s *Service) Resolve(ctx context.Context, a agentprofile.PrivateAccess) (agentcognitive.AgentReference, error) {
	if s == nil || s.store == nil || ctx == nil || ctx.Err() != nil {
		return agentcognitive.AgentReference{}, ErrUnavailable
	}
	return s.store.ResolveOwnContextAgent(ctx, a)
}
func (s *Service) signature(b BuiltContext) []byte {
	request := cloneRequest(b.Request)
	request.Access = agentprofile.PrivateAccess{}
	// Explicit selectors avoid Request.MarshalJSON; access never goes to JSON.
	raw, _ := json.Marshal(struct {
		Bundle                           Bundle
		Activities, Places               any
		Authority                        string
		Agent                            any
		Task, RequestID, City, Query     string
		Selection                        Selection
		Mode                             Mode
		IDs, PlaceIDs, Fields, MemoryIDs []string
		Families                         any
		Deadline, TaskUpdated            any
		PurposeGrant                     string
		PurposeDeadline                  any
		Ties                             []string
	}{b.Bundle, b.Activities, b.Places, b.Authority, request.Agent, request.TaskID, request.RequestID, request.CityID, request.CurrentQuery, request.Selection, request.Mode, request.ActivityIDs, request.PlaceIDs, request.ProfileFields, request.MemoryIDs, request.PolicyFamilies, request.DeadlineAt, request.TaskUpdatedAt, request.PurposeGrantID, request.PurposeDeadlineAt, request.RelationshipTieIDs})
	h := hmac.New(sha256.New, s.key[:])
	h.Write([]byte("birdtie.context.builder.ephemeral.v1\x00"))
	h.Write(raw)
	return h.Sum(nil)
}
func (s *Service) Build(ctx context.Context, r Request) (BuiltContext, error) {
	if s == nil || s.store == nil || ctx == nil || ctx.Err() != nil {
		return BuiltContext{}, ErrUnavailable
	}
	if e := ValidateShape(r); e != nil {
		return BuiltContext{}, e
	}
	out, e := s.store.BuildOwnAgentContext(ctx, cloneRequest(r))
	if e != nil {
		return BuiltContext{}, e
	}
	if ValidateBuilt(r, out) != nil {
		return BuiltContext{}, ErrUnavailable
	}
	if ctx.Err() != nil {
		return BuiltContext{}, ErrUnavailable
	}
	out.Request = cloneRequest(r)
	out.seal = s.signature(out)
	return cloneBuilt(out), nil
}
func (s *Service) RevalidateOwn(ctx context.Context, a agentprofile.PrivateAccess, old BuiltContext) (BuiltContext, error) {
	if s == nil || s.store == nil || ctx == nil || ctx.Err() != nil {
		return BuiltContext{}, ErrUnavailable
	}
	if len(old.seal) != sha256.Size || !hmac.Equal(old.seal, s.signature(old)) {
		return BuiltContext{}, ErrDenied
	}
	if ValidateExactFieldEvidence(old.Bundle) != nil {
		return BuiltContext{}, ErrDenied
	}
	r := cloneRequest(old.Request)
	r.Access = a
	r.DeadlineAt = old.Bundle.ExpiresAt
	current, e := s.store.BuildOwnAgentContext(ctx, r)
	if e != nil {
		return BuiltContext{}, e
	}
	if ValidateBuilt(r, current) != nil {
		return BuiltContext{}, ErrUnavailable
	}
	// Database-issued ObservedAt is compared only to the current database clock.
	if old.Bundle.ObservedAt.After(current.Bundle.ObservedAt) || !old.Bundle.ExpiresAt.After(current.Bundle.ObservedAt) {
		return BuiltContext{}, ErrExpired
	}
	before, _ := json.Marshal(old.Bundle.Sources)
	after, _ := json.Marshal(current.Bundle.Sources)
	if current.Authority != old.Authority || !hmac.Equal(before, after) {
		return BuiltContext{}, ErrDenied
	}
	if ctx.Err() != nil {
		return BuiltContext{}, ErrUnavailable
	}
	return cloneBuilt(old), nil
}
