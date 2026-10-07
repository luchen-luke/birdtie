package agentcognitive

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/contextgraph"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

type domainReadSpy struct {
	actor                                             identity.Actor
	active                                            bool
	profile                                           identity.Profile
	declarations                                      []contextgraph.Declaration
	authErr, agentErr, profileErr, contextErr         error
	authCalls, agentCalls, profileCalls, contextCalls int
	readActor, readTarget, contextActor               string
	afterProfile, afterContext                        func()
}

func (s *domainReadSpy) Authenticate(context.Context, [32]byte) (identity.Actor, error) {
	s.authCalls++
	return s.actor, s.authErr
}
func (s *domainReadSpy) HasActiveAgent(_ context.Context, kind, id string) (bool, error) {
	s.agentCalls++
	if kind != "person" || id != s.actor.ID {
		return false, errors.New("unexpected principal")
	}
	return s.active, s.agentErr
}
func (s *domainReadSpy) ReadProfile(_ context.Context, actor, target string) (identity.Profile, error) {
	s.profileCalls++
	s.readActor, s.readTarget = actor, target
	if s.afterProfile != nil {
		s.afterProfile()
	}
	return s.profile, s.profileErr
}
func (s *domainReadSpy) ListOwnContextDeclarations(_ context.Context, actor string) ([]contextgraph.Declaration, error) {
	s.contextCalls++
	s.contextActor = actor
	if s.afterContext != nil {
		s.afterContext()
	}
	return s.declarations, s.contextErr
}

func currentDomainFixture(t *testing.T) (*CurrentDomainAdapter, *domainReadSpy, SessionAccess) {
	t.Helper()
	owner := "11111111-1111-4111-8111-111111111111"
	spy := &domainReadSpy{actor: identity.Actor{ID: owner, AccountType: "person"}, active: true,
		profile: identity.Profile{AccountID: owner, DisplayName: "合成本人测试", Bio: "本人领域资料", Visibility: "private"},
		declarations: []contextgraph.Declaration{{ContextID: "33333333-3333-4333-8333-333333333333", Type: contextgraph.Online,
			SourceKey: "ordinary-online-interest", Label: "普通线上兴趣声明", Relation: "interest", Visibility: "private"}}}
	adapter, err := NewCurrentDomainAdapter(spy)
	if err != nil {
		t.Fatal(err)
	}
	return adapter, spy, SessionAccess{Digest: [32]byte{1}, Workspace: actorref.PrincipalRef{Type: actorref.Person, ID: owner}}
}

func TestCurrentDomainAdapterUsesNativeSelfReads(t *testing.T) {
	adapter, spy, request := currentDomainFixture(t)
	profile, err := adapter.ReadOwnProfile(context.Background(), request)
	if err != nil || profile != spy.profile || spy.readActor != request.Workspace.ID || spy.readTarget != request.Workspace.ID {
		t.Fatalf("native self profile not reused: %v", err)
	}
	declarations, err := adapter.ReadOwnContextDeclarations(context.Background(), request)
	if err != nil || !reflect.DeepEqual(declarations, spy.declarations) || spy.contextActor != request.Workspace.ID {
		t.Fatalf("native declarations not reused: %v", err)
	}
	if spy.authCalls != 4 || spy.agentCalls != 4 || spy.profileCalls != 1 || spy.contextCalls != 1 {
		t.Fatal("reads did not refresh current native authority before/after")
	}
	declarations[0].Label = "caller edit"
	if spy.declarations[0].Label == "caller edit" {
		t.Fatal("domain output alias leaked mutable cache")
	}
}

func TestCurrentDomainAdapterRejectsWrongIdentityAndAuthority(t *testing.T) {
	cases := []struct {
		name string
		edit func(*domainReadSpy, *SessionAccess)
	}{
		{"anonymous", func(_ *domainReadSpy, r *SessionAccess) { r.Digest = [32]byte{} }},
		{"missing workspace", func(_ *domainReadSpy, r *SessionAccess) { r.Workspace = actorref.PrincipalRef{} }},
		{"another Person", func(_ *domainReadSpy, r *SessionAccess) { r.Workspace.ID = "22222222-2222-4222-8222-222222222222" }},
		{"Organization workspace", func(_ *domainReadSpy, r *SessionAccess) { r.Workspace.Type = actorref.Organization }},
		{"Business workspace", func(_ *domainReadSpy, r *SessionAccess) { r.Workspace.Type = actorref.Business }},
		{"Community workspace", func(_ *domainReadSpy, r *SessionAccess) { r.Workspace.Type = actorref.Community }},
		{"org session", func(s *domainReadSpy, _ *SessionAccess) { s.actor.AccountType = "organization" }},
		{"business session", func(s *domainReadSpy, _ *SessionAccess) { s.actor.AccountType = "business" }},
		{"expired session", func(s *domainReadSpy, _ *SessionAccess) { s.authErr = identity.ErrUnauthorized }},
		{"session resolver failure", func(s *domainReadSpy, _ *SessionAccess) { s.authErr = errors.New("secret SQL error") }},
		{"inactive Agent", func(s *domainReadSpy, _ *SessionAccess) { s.active = false }},
		{"Agent resolver failure", func(s *domainReadSpy, _ *SessionAccess) { s.agentErr = errors.New("secret SQL error") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			adapter, spy, request := currentDomainFixture(t)
			tc.edit(spy, &request)
			profile, err := adapter.ReadOwnProfile(context.Background(), request)
			if err == nil || !reflect.ValueOf(profile).IsZero() {
				t.Fatal("private profile released")
			}
			declarations, err := adapter.ReadOwnContextDeclarations(context.Background(), request)
			if err == nil || declarations != nil {
				t.Fatal("private declarations released")
			}
			if spy.profileCalls != 0 || spy.contextCalls != 0 {
				t.Fatal("unauthorized read reached native data port")
			}
		})
	}
}

func TestCurrentDomainAdapterLateRevocationDiscardsPayload(t *testing.T) {
	for _, domain := range []string{"profile", "context"} {
		for _, change := range []string{"session revoked", "agent suspended", "actor replaced"} {
			t.Run(domain+" "+change, func(t *testing.T) {
				adapter, spy, request := currentDomainFixture(t)
				late := func() {
					switch change {
					case "session revoked":
						spy.authErr = identity.ErrUnauthorized
					case "agent suspended":
						spy.active = false
					case "actor replaced":
						spy.actor.ID = "22222222-2222-4222-8222-222222222222"
					}
				}
				if domain == "profile" {
					spy.afterProfile = late
					profile, err := adapter.ReadOwnProfile(context.Background(), request)
					if !errors.Is(err, ErrDenied) || !reflect.ValueOf(profile).IsZero() {
						t.Fatal("late authority allowed profile")
					}
				} else {
					spy.afterContext = late
					declarations, err := adapter.ReadOwnContextDeclarations(context.Background(), request)
					if !errors.Is(err, ErrDenied) || declarations != nil {
						t.Fatal("late authority allowed declarations")
					}
				}
			})
		}
	}
}

func TestCurrentDomainAdapterRejectsMalformedOrFailedDomainData(t *testing.T) {
	cases := []struct {
		name    string
		edit    func(*domainReadSpy)
		profile bool
	}{
		{"cross-owner profile", func(s *domainReadSpy) { s.profile.AccountID = "22222222-2222-4222-8222-222222222222" }, true},
		{"missing profile", func(s *domainReadSpy) { s.profileErr = identity.ErrNotFound }, true},
		{"profile internal error", func(s *domainReadSpy) { s.profileErr = errors.New("secret SQL error") }, true},
		{"context internal error", func(s *domainReadSpy) { s.contextErr = errors.New("secret SQL error") }, false},
		{"invalid context reference", func(s *domainReadSpy) { s.declarations[0].ContextID = "" }, false},
		{"private-context public claim", func(s *domainReadSpy) { s.declarations[0].Visibility = "public" }, false},
		{"unknown context", func(s *domainReadSpy) { s.declarations[0].Type = "PRIVATE_MEMORY" }, false},
		{"unknown relation", func(s *domainReadSpy) { s.declarations[0].Relation = "verified_resident" }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			adapter, spy, request := currentDomainFixture(t)
			tc.edit(spy)
			if tc.profile {
				result, err := adapter.ReadOwnProfile(context.Background(), request)
				if err == nil || !reflect.ValueOf(result).IsZero() {
					t.Fatal("invalid profile released")
				}
			} else {
				result, err := adapter.ReadOwnContextDeclarations(context.Background(), request)
				if err == nil || result != nil {
					t.Fatal("invalid context released")
				}
			}
		})
	}
	adapter, _, request := currentDomainFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := adapter.ReadOwnProfile(ctx, request); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled read still performed")
	}
	if _, err := NewCurrentDomainAdapter(nil); !errors.Is(err, ErrUnavailable) {
		t.Fatal("missing domain store not unavailable")
	}
	var missingStore *domainReadSpy
	if _, err := NewCurrentDomainAdapter(missingStore); !errors.Is(err, ErrUnavailable) {
		t.Fatal("typed nil domain store not unavailable")
	}
	var absent *CurrentDomainAdapter
	if _, err := absent.ReadOwnProfile(context.Background(), request); !errors.Is(err, ErrUnavailable) {
		t.Fatal("nil adapter not unavailable")
	}
}

func TestCurrentDomainPortsContainNoWriteOrModelMethods(t *testing.T) {
	typ := reflect.TypeOf((*CurrentDomainStore)(nil)).Elem()
	if typ.NumMethod() != 4 {
		t.Fatal("native read boundary expanded")
	}
	allowed := map[string]bool{"Authenticate": true, "HasActiveAgent": true, "ReadProfile": true, "ListOwnContextDeclarations": true}
	for i := 0; i < typ.NumMethod(); i++ {
		if !allowed[typ.Method(i).Name] {
			t.Fatal("model/native adapter acquired a write or raw database method")
		}
	}
}
