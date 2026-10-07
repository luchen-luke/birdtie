package agentcognitive

import (
	"context"
	"errors"
	"reflect"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/contextgraph"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

// CurrentDomainStore is a narrow read-only projection of existing domain
// methods, implemented by postgres.Store. It has no profile/context write,
// raw SQL, Memory, provider, inference, invitation or action method.
type CurrentDomainStore interface {
	Authenticate(context.Context, [32]byte) (identity.Actor, error)
	HasActiveAgent(context.Context, string, string) (bool, error)
	ReadProfile(context.Context, string, string) (identity.Profile, error)
	ListOwnContextDeclarations(context.Context, string) ([]contextgraph.Declaration, error)
}

// SessionAccess is an in-process ordinary self-review request. A digest is
// resolved again on every read; Workspace is a claim checked against that
// session, never authority supplied by a model. It cannot identify another
// person's profile, switch into an organization, or approve cognition/egress.
type SessionAccess struct {
	Digest    [32]byte
	Workspace actorref.PrincipalRef
}

func (SessionAccess) MarshalJSON() ([]byte, error)  { return nil, ErrAuthorityJSON }
func (v *SessionAccess) UnmarshalJSON([]byte) error { *v = SessionAccess{}; return ErrAuthorityJSON }

type OwnProfileReader interface {
	ReadOwnProfile(context.Context, SessionAccess) (identity.Profile, error)
}

type OwnContextReader interface {
	ReadOwnContextDeclarations(context.Context, SessionAccess) ([]contextgraph.Declaration, error)
}

// CurrentDomainAdapter bridges only already-implemented UserProfile and Person
// declarations. Output retains the native type without invented cognition
// versions, confidence, Memory, entitlement or model-context representations.
// HasActiveAgent verifies a current principal's agent exists, not a specific ID.
type CurrentDomainAdapter struct{ store CurrentDomainStore }

var _ OwnProfileReader = (*CurrentDomainAdapter)(nil)
var _ OwnContextReader = (*CurrentDomainAdapter)(nil)

func NewCurrentDomainAdapter(store CurrentDomainStore) (*CurrentDomainAdapter, error) {
	if store == nil {
		return nil, ErrUnavailable
	}
	value := reflect.ValueOf(store)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		if value.IsNil() {
			return nil, ErrUnavailable
		}
	}
	return &CurrentDomainAdapter{store: store}, nil
}

func (a *CurrentDomainAdapter) resolveSelf(ctx context.Context, request SessionAccess) (actorref.PrincipalRef, error) {
	if a == nil || a.store == nil {
		return actorref.PrincipalRef{}, ErrUnavailable
	}
	if request.Workspace.Type != actorref.Person || !validID(request.Workspace.ID) || request.Digest == ([32]byte{}) {
		return actorref.PrincipalRef{}, ErrDenied
	}
	if err := ctx.Err(); err != nil {
		return actorref.PrincipalRef{}, err
	}
	actor, err := a.store.Authenticate(ctx, request.Digest)
	if err != nil {
		if errors.Is(err, identity.ErrUnauthorized) {
			return actorref.PrincipalRef{}, ErrDenied
		}
		return actorref.PrincipalRef{}, ErrUnavailable
	}
	principal, err := actorref.ParsePrincipal(actor.AccountType, actor.ID)
	if err != nil || principal.Type != actorref.Person || !validID(actor.ID) || !principal.Equal(request.Workspace) {
		return actorref.PrincipalRef{}, ErrDenied
	}
	active, err := a.store.HasActiveAgent(ctx, "person", principal.ID)
	if err != nil {
		return actorref.PrincipalRef{}, ErrUnavailable
	}
	if !active {
		return actorref.PrincipalRef{}, ErrDenied
	}
	return principal, nil
}

func (a *CurrentDomainAdapter) ReadOwnProfile(ctx context.Context, request SessionAccess) (identity.Profile, error) {
	principal, err := a.resolveSelf(ctx, request)
	if err != nil {
		return identity.Profile{}, err
	}
	profile, err := a.store.ReadProfile(ctx, principal.ID, principal.ID)
	if err != nil {
		if errors.Is(err, identity.ErrNotFound) {
			return identity.Profile{}, identity.ErrNotFound
		}
		return identity.Profile{}, ErrUnavailable
	}
	if !validID(profile.AccountID) || !principal.Equal(actorref.PrincipalRef{Type: actorref.Person, ID: profile.AccountID}) {
		return identity.Profile{}, ErrDenied
	}
	// No cache: a revoked session/agent during the domain read cannot release the
	// loaded payload. A future cognition read requires its own atomic resolver.
	if _, err = a.resolveSelf(ctx, request); err != nil {
		return identity.Profile{}, err
	}
	return profile, nil
}

func (a *CurrentDomainAdapter) ReadOwnContextDeclarations(ctx context.Context, request SessionAccess) ([]contextgraph.Declaration, error) {
	principal, err := a.resolveSelf(ctx, request)
	if err != nil {
		return nil, err
	}
	declarations, err := a.store.ListOwnContextDeclarations(ctx, principal.ID)
	if err != nil {
		return nil, ErrUnavailable
	}
	for _, item := range declarations {
		ref, parseErr := contextgraph.Parse(string(item.Type), item.ContextID)
		_, inputErr := contextgraph.NormalizeDeclaration(contextgraph.DeclarationInput{Type: item.Type, SourceKey: item.SourceKey, Relation: item.Relation})
		if parseErr != nil || !validID(ref.ID) || inputErr != nil || item.Visibility != "private" {
			return nil, ErrDenied
		}
	}
	if _, err = a.resolveSelf(ctx, request); err != nil {
		return nil, err
	}
	return append([]contextgraph.Declaration{}, declarations...), nil
}
