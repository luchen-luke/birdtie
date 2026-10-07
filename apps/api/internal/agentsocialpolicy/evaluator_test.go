package agentsocialpolicy

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
)

const ownerID = "74000000-0000-4000-8000-000000000001"
const peerID = "74000000-0000-4000-8000-000000000002"
const agentID = "74000000-0000-4000-8000-000000000003"
const operationID = "74000000-0000-4000-8000-000000000004"
const resourceID = "74000000-0000-4000-8000-000000000005"
const otherID = "74000000-0000-4000-8000-000000000006"

func fixedNow() time.Time { return time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC) }
func agentFixture() agentcognitive.AgentReference {
	return agentcognitive.AgentReference{AgentID: agentID, Principal: actorref.PrincipalRef{Type: actorref.Person, ID: ownerID}, Role: agentruntime.PersonalAgent}
}
func specFixture(now time.Time) Specification {
	return Specification{ValidFrom: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)}
}
func storeFixture(t *testing.T, spec Specification) (*Store, Policy) {
	t.Helper()
	s, err := NewStore(agentFixture(), spec)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	return s, p
}
func requestFixture(now time.Time, category Category) Request {
	r := Request{Purpose: EvaluateSocialPolicy, Actor: actorref.ActorRef{Type: actorref.Person, ID: ownerID}, Agent: agentFixture(), Counterparty: actorref.PrincipalRef{Type: actorref.Person, ID: peerID}, OperationID: operationID, RequestedAt: now.Add(-time.Minute), ExpiresAt: now.Add(-time.Minute).Add(MaxRequestTTL)}
	if category == Business {
		r.Counterparty.Type = actorref.Business
	} else if category == Organization {
		r.Counterparty.Type = actorref.Organization
	}
	relation := Relation{Category: category}
	if kind := sourceKind(category); kind != "" {
		version, _ := agentevent.QueryVersion(now.Add(-time.Minute), []byte(`{"syntheticRows":"pair"}`))
		relation.Source = SourceReference{kind, resourceID, r.Agent.Principal, r.Counterparty, version}
	}
	r.Relations = []Relation{relation}
	return r
}
func boundaryFixture(p Policy, r Request, now time.Time) OfflineBoundary {
	b := OfflineBoundary{OwnerConsentPurpose: EvaluateSocialPolicy, CounterpartyConsentPurpose: EvaluateSocialPolicy, State: BoundaryAllowed, OwnerActive: true, CounterpartyActive: true, OwnerBlock: BlockClear, CounterpartyBlock: BlockClear, CurrentPolicyRevision: p.revision, CurrentRequestDigest: RequestDigest(r), OwnerConsentRevision: 3, CurrentOwnerConsentRevision: 3, CounterpartyConsentRevision: 5, CurrentCounterpartyConsentRevision: 5, CheckedAt: now, ExpiresAt: now.Add(time.Minute)}
	for _, relation := range r.Relations {
		if sourceKind(relation.Category) != "" {
			b.CurrentSources = append(b.CurrentSources, relation.Source)
		}
	}
	return b
}
func assertDisabled(t *testing.T, p Policy, r Request, b OfflineBoundary, now time.Time, want error) {
	t.Helper()
	d, err := EvaluateOffline(p, r, b, now)
	if !errors.Is(err, want) || d.Preference != Disabled || d.Mode != "OFFLINE_CONTRACT" {
		t.Fatal(d, err, want)
	}
}

func TestSevenCategoriesDefaultDisabledAndReviewOnly(t *testing.T) {
	now := fixedNow()
	_, defaultPolicy := storeFixture(t, specFixture(now))
	for _, category := range Categories() {
		t.Run(string(category)+"/default", func(t *testing.T) {
			r := requestFixture(now, category)
			want := error(nil)
			if category == Business {
				want = ErrUnavailable
			}
			assertDisabled(t, defaultPolicy, r, boundaryFixture(defaultPolicy, r, now), now, want)
		})
		t.Run(string(category)+"/review_preference", func(t *testing.T) {
			spec := specFixture(now)
			spec.Rules = []Rule{{category, ReviewRequired}}
			_, p := storeFixture(t, spec)
			r := requestFixture(now, category)
			d, err := EvaluateOffline(p, r, boundaryFixture(p, r, now), now)
			if category == Business {
				if err != ErrUnavailable || d.Preference != Disabled {
					t.Fatal(d, err)
				}
			} else if err != nil || d.Preference != ReviewRequired || d.Reason != "review_preference_only" || d.Mode != "OFFLINE_CONTRACT" {
				t.Fatal(d, err)
			}
		})
	}
	t.Run("catalog_copy", func(t *testing.T) {
		c := Categories()
		c[0] = "FORGED"
		if Categories()[0] != SameUniversity {
			t.Fatal("catalog aliased")
		}
	})
}

func TestMultipleRelationsUseConservativeOrderIndependentMerge(t *testing.T) {
	now := fixedNow()
	spec := specFixture(now)
	spec.Rules = []Rule{{SharedActivity, ReviewRequired}, {SharedCommunity, Disabled}, {ExistingConnection, ReviewRequired}}
	_, p := storeFixture(t, spec)
	r := requestFixture(now, SharedActivity)
	r.Relations = append(r.Relations, requestFixture(now, SharedCommunity).Relations[0])
	b := boundaryFixture(p, r, now)
	assertDisabled(t, p, r, b, now, nil)
	t.Run("relation_order", func(t *testing.T) {
		before := RequestDigest(r)
		r.Relations[0], r.Relations[1] = r.Relations[1], r.Relations[0]
		if before != RequestDigest(r) {
			t.Fatal("order changed digest")
		}
		assertDisabled(t, p, r, b, now, nil)
	})
	t.Run("rule_order", func(t *testing.T) {
		spec.Rules[0], spec.Rules[1] = spec.Rules[1], spec.Rules[0]
		_, p := storeFixture(t, spec)
		assertDisabled(t, p, r, boundaryFixture(p, r, now), now, nil)
	})
	t.Run("missing_category_veto", func(t *testing.T) {
		spec.Rules = []Rule{{SharedActivity, ReviewRequired}}
		_, p := storeFixture(t, spec)
		assertDisabled(t, p, r, boundaryFixture(p, r, now), now, nil)
	})
	t.Run("all_review_needs_review", func(t *testing.T) {
		spec.Rules = []Rule{{SharedActivity, ReviewRequired}, {SharedCommunity, ReviewRequired}}
		_, p := storeFixture(t, spec)
		d, err := EvaluateOffline(p, r, boundaryFixture(p, r, now), now)
		if err != nil || d.Preference != ReviewRequired {
			t.Fatal(d, err)
		}
	})
	t.Run("block_beats_preference", func(t *testing.T) {
		b := boundaryFixture(p, r, now)
		b.CounterpartyBlock = BlockPresent
		assertDisabled(t, p, r, b, now, ErrDenied)
	})
}

func TestCurrentPurposeSourcesBothConsentBlockAndExpiryFailClosed(t *testing.T) {
	now := fixedNow()
	spec := specFixture(now)
	spec.Rules = []Rule{{ExistingConnection, ReviewRequired}}
	_, p := storeFixture(t, spec)
	r := requestFixture(now, ExistingConnection)
	cases := []struct {
		name   string
		change func(*OfflineBoundary)
		want   error
	}{
		{"unknown_state", func(b *OfflineBoundary) { b.State = "VERIFIED" }, ErrDenied},
		{"zero_state", func(b *OfflineBoundary) { b.State = "" }, ErrDenied},
		{"denied", func(b *OfflineBoundary) { b.State = BoundaryDenied }, ErrDenied},
		{"missing_resolver", func(b *OfflineBoundary) { b.State = BoundaryUnavailable }, ErrUnavailable},
		{"old_matching_purpose_not_inherited", func(b *OfflineBoundary) { b.OwnerConsentPurpose = "NEW_PEOPLE_052" }, ErrDenied},
		{"profile_view_not_private_agent_consent", func(b *OfflineBoundary) { b.CounterpartyConsentPurpose = "profile_view" }, ErrDenied},
		{"friend_not_processing_consent", func(b *OfflineBoundary) { b.OwnerConsentPurpose = "human_friend" }, ErrDenied},
		{"missing_consent_purpose", func(b *OfflineBoundary) { b.CounterpartyConsentPurpose = "" }, ErrDenied},
		{"owner_inactive", func(b *OfflineBoundary) { b.OwnerActive = false }, ErrDenied},
		{"peer_inactive", func(b *OfflineBoundary) { b.CounterpartyActive = false }, ErrDenied},
		{"owner_block", func(b *OfflineBoundary) { b.OwnerBlock = BlockPresent }, ErrDenied},
		{"peer_block", func(b *OfflineBoundary) { b.CounterpartyBlock = BlockPresent }, ErrDenied},
		{"unknown_owner_block", func(b *OfflineBoundary) { b.OwnerBlock = "" }, ErrDenied},
		{"unknown_peer_block", func(b *OfflineBoundary) { b.CounterpartyBlock = "NO_BLOCK" }, ErrDenied},
		{"permission_revoked", func(b *OfflineBoundary) { b.Revoked = true }, ErrDenied},
		{"source_removed", func(b *OfflineBoundary) { b.SourceWithdrawn = true }, ErrDenied},
		{"old_policy", func(b *OfflineBoundary) { b.CurrentPolicyRevision++ }, ErrDenied},
		{"owner_consent_missing", func(b *OfflineBoundary) { b.OwnerConsentRevision = 0 }, ErrDenied},
		{"peer_consent_missing", func(b *OfflineBoundary) { b.CounterpartyConsentRevision = 0 }, ErrDenied},
		{"owner_consent_changed", func(b *OfflineBoundary) { b.CurrentOwnerConsentRevision++ }, ErrDenied},
		{"peer_consent_changed", func(b *OfflineBoundary) { b.CurrentCounterpartyConsentRevision++ }, ErrDenied},
		{"zero_current_owner_consent", func(b *OfflineBoundary) { b.CurrentOwnerConsentRevision = 0 }, ErrDenied},
		{"zero_current_peer_consent", func(b *OfflineBoundary) { b.CurrentCounterpartyConsentRevision = 0 }, ErrDenied},
		{"old_target_digest", func(b *OfflineBoundary) { b.CurrentRequestDigest = "old" }, ErrDenied},
		{"source_missing", func(b *OfflineBoundary) { b.CurrentSources = nil }, ErrDenied},
		{"source_extra", func(b *OfflineBoundary) { b.CurrentSources = append(b.CurrentSources, b.CurrentSources[0]) }, ErrDenied},
		{"source_owner_changed", func(b *OfflineBoundary) { b.CurrentSources[0].Owner.ID = otherID }, ErrDenied},
		{"source_peer_changed", func(b *OfflineBoundary) { b.CurrentSources[0].Counterparty.ID = otherID }, ErrDenied},
		{"source_id_changed", func(b *OfflineBoundary) { b.CurrentSources[0].ResourceID = otherID }, ErrDenied},
		{"source_kind_changed", func(b *OfflineBoundary) { b.CurrentSources[0].Kind = CommunityMembership }, ErrDenied},
		{"source_native_digest_changed", func(b *OfflineBoundary) {
			b.CurrentSources[0].Version, _ = agentevent.QueryVersion(now, []byte(`{"changed":true}`))
		}, ErrDenied},
		{"check_stale", func(b *OfflineBoundary) { b.CheckedAt = now.Add(-time.Nanosecond) }, ErrExpired},
		{"check_future", func(b *OfflineBoundary) { b.CheckedAt = now.Add(time.Nanosecond) }, ErrExpired},
		{"expiry_exact", func(b *OfflineBoundary) { b.ExpiresAt = now }, ErrExpired},
		{"expiry_missing", func(b *OfflineBoundary) { b.ExpiresAt = time.Time{} }, ErrExpired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := boundaryFixture(p, r, now)
			tc.change(&b)
			assertDisabled(t, p, r, b, now, tc.want)
		})
	}
	t.Run("request_expiry_exact", func(t *testing.T) {
		at := r.ExpiresAt
		assertDisabled(t, p, r, boundaryFixture(p, r, at), at, ErrExpired)
	})
	t.Run("policy_expiry_exact", func(t *testing.T) {
		spec.ExpiresAt = now
		_, p := storeFixture(t, spec)
		assertDisabled(t, p, r, boundaryFixture(p, r, now), now, ErrExpired)
	})
	t.Run("policy_not_started", func(t *testing.T) {
		spec = specFixture(now)
		spec.ValidFrom = now.Add(time.Nanosecond)
		_, p := storeFixture(t, spec)
		assertDisabled(t, p, r, boundaryFixture(p, r, now), now, ErrExpired)
	})
	t.Run("revoked_policy", func(t *testing.T) {
		s, p := storeFixture(t, specFixture(now))
		if s.Revoke(p.revision) != nil {
			t.Fatal("revoke failed")
		}
		current, _ := s.Snapshot()
		assertDisabled(t, current, r, boundaryFixture(current, r, now), now, ErrDenied)
	})
}

func TestRelationClaimsAndNamespacesCannotGrantAuthority(t *testing.T) {
	now := fixedNow()
	_, p := storeFixture(t, specFixture(now))
	cases := []struct {
		name   string
		change func(*Request)
		want   error
	}{
		{"university_string_not_category", func(r *Request) { r.Relations[0].Category = "University of Aberdeen" }, ErrInvalid},
		{"request_purpose_override", func(r *Request) { r.Purpose = "SEND_MESSAGE" }, ErrInvalid},
		{"university_string_not_source_id", func(r *Request) { r.Relations[0].Source.ResourceID = "University of Aberdeen" }, ErrInvalid},
		{"unknown_relation", func(r *Request) { r.Relations[0].Category = "CLOSE_FRIEND" }, ErrInvalid},
		{"duplicate_relation", func(r *Request) { r.Relations = append(r.Relations, r.Relations[0]) }, ErrInvalid},
		{"unknown_plus_shared", func(r *Request) { r.Relations = append(r.Relations, Relation{Category: UnknownPerson}) }, ErrInvalid},
		{"wrong_person_type", func(r *Request) { r.Counterparty.Type = actorref.Organization }, ErrInvalid},
		{"wrong_source_kind", func(r *Request) { r.Relations[0].Source.Kind = PersonTie }, ErrInvalid},
		{"missing_source", func(r *Request) { r.Relations[0].Source = SourceReference{} }, ErrInvalid},
		{"source_owner_wrong", func(r *Request) { r.Relations[0].Source.Owner.ID = otherID }, ErrInvalid},
		{"source_counterparty_wrong", func(r *Request) { r.Relations[0].Source.Counterparty.ID = otherID }, ErrInvalid},
		{"fabricated_revision_one", func(r *Request) {
			r.Relations[0].Source.Version = agentevent.SourceVersion{Kind: agentevent.RevisionVersion, Revision: 1}
		}, ErrInvalid},
		{"digest_extra_revision", func(r *Request) { r.Relations[0].Source.Version.Revision = 1 }, ErrInvalid},
		{"digest_invalid_hex", func(r *Request) {
			r.Relations[0].Source.Version.Token = "gggggggggggggggggggggggggggggggggggggggggggggggggggggggggggggggg"
		}, ErrInvalid},
		{"actor_wrong", func(r *Request) { r.Actor.ID = peerID }, ErrInvalid},
		{"organization_actor_not_principal", func(r *Request) { r.Actor.Type = actorref.Organization }, ErrInvalid},
		{"agent_wrong", func(r *Request) { r.Agent.AgentID = otherID }, ErrInvalid},
		{"self_counterparty", func(r *Request) { r.Counterparty = r.Agent.Principal }, ErrInvalid},
		{"community_not_account", func(r *Request) { r.Counterparty.Type = actorref.Community }, ErrInvalid},
		{"zero_peer_id", func(r *Request) { r.Counterparty.ID = "00000000-0000-0000-0000-000000000000" }, ErrInvalid},
		{"zero_operation", func(r *Request) { r.OperationID = "" }, ErrInvalid},
		{"no_relations", func(r *Request) { r.Relations = nil }, ErrInvalid},
		{"future_request", func(r *Request) {
			r.RequestedAt = now.Add(time.Nanosecond)
			r.ExpiresAt = r.RequestedAt.Add(MaxRequestTTL)
		}, ErrExpired},
		{"unbounded_request", func(r *Request) { r.ExpiresAt = r.RequestedAt.Add(24 * time.Hour) }, ErrExpired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := requestFixture(now, SameUniversity)
			tc.change(&r)
			assertDisabled(t, p, r, boundaryFixture(p, r, now), now, tc.want)
		})
	}
	t.Run("organization_category_is_not_person", func(t *testing.T) {
		r := requestFixture(now, Organization)
		r.Counterparty.Type = actorref.Person
		assertDisabled(t, p, r, boundaryFixture(p, r, now), now, ErrInvalid)
	})
	t.Run("unknown_has_no_hidden_source", func(t *testing.T) {
		r := requestFixture(now, UnknownPerson)
		r.Relations[0].Source = requestFixture(now, SameUniversity).Relations[0].Source
		assertDisabled(t, p, r, boundaryFixture(p, r, now), now, ErrInvalid)
	})
	t.Run("valid_org_account_shape_is_not_resource_authority", func(t *testing.T) {
		r := requestFixture(now, Organization)
		spec := specFixture(now)
		spec.Rules = []Rule{{Organization, ReviewRequired}}
		_, p := storeFixture(t, spec)
		b := boundaryFixture(p, r, now)
		b.State = BoundaryUnavailable
		assertDisabled(t, p, r, b, now, ErrUnavailable)
	})
}

func TestPolicySettingsCASRevocationNoWireAndNoAlias(t *testing.T) {
	now := fixedNow()
	spec := specFixture(now)
	spec.Rules = []Rule{{SharedCommunity, ReviewRequired}}
	s, p := storeFixture(t, spec)
	spec.Rules[0].Preference = Disabled
	if preference, _ := p.Preference(SharedCommunity); preference != ReviewRequired {
		t.Fatal("input alias")
	}
	copy := p.Specification()
	copy.Rules[0].Preference = Disabled
	if preference, _ := p.Preference(SharedCommunity); preference != ReviewRequired {
		t.Fatal("output alias")
	}
	const workers = 32
	var wg sync.WaitGroup
	results := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- s.Replace(1, specFixture(now)) }()
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if err != ErrConflict {
			t.Fatal(err)
		}
	}
	current, _ := s.Snapshot()
	if success != 1 || current.revision != 2 || s.current(p) {
		t.Fatal("CAS not exclusive", success, current.revision)
	}
	if s.Revoke(2) != nil {
		t.Fatal("revoke failed")
	}
	revoked, _ := s.Snapshot()
	if revoked.revision != 3 || !revoked.revoked || s.current(current) {
		t.Fatal("old settings current")
	}
	if s.Revoke(2) != ErrConflict || s.Replace(2, specFixture(now)) != ErrConflict {
		t.Fatal("old revision accepted")
	}
	if s.Replace(3, specFixture(now)) != nil {
		t.Fatal("replace failed")
	}
	fresh, _ := s.Snapshot()
	if fresh.revision != 4 || fresh.revoked || s.current(p) || s.current(revoked) {
		t.Fatal("revived old revision")
	}
	t.Run("overflow", func(t *testing.T) {
		s, _ := storeFixture(t, specFixture(now))
		s.policy.revision = ^uint64(0)
		if s.Replace(^uint64(0), specFixture(now)) != ErrConflict || s.Revoke(^uint64(0)) != ErrConflict {
			t.Fatal("overflow accepted")
		}
	})
	for name, value := range map[string]any{"policy": p, "settings": spec, "request": requestFixture(now, UnknownPerson), "offline_facts": OfflineBoundary{State: BoundaryAllowed}} {
		t.Run("wire_encode_"+name, func(t *testing.T) {
			if _, err := json.Marshal(value); !errors.Is(err, ErrServerOnly) {
				t.Fatal(err)
			}
		})
	}
	t.Run("verified_facts_wire_decode", func(t *testing.T) {
		b := OfflineBoundary{State: BoundaryAllowed}
		if err := json.Unmarshal([]byte(`{"Verified":true,"State":"ALLOWED","friend":true,"university":"same"}`), &b); !errors.Is(err, ErrServerOnly) || b.State != "" {
			t.Fatal(err, b)
		}
	})
	t.Run("settings_wire_decode", func(t *testing.T) {
		spec := specFixture(now)
		if err := json.Unmarshal([]byte(`{"Rules":[]}`), &spec); !errors.Is(err, ErrServerOnly) || !spec.ValidFrom.IsZero() {
			t.Fatal(err, spec)
		}
	})
	t.Run("request_wire_decode", func(t *testing.T) {
		r := requestFixture(now, SameUniversity)
		if err := json.Unmarshal([]byte(`{"verifiedUniversity":true}`), &r); !errors.Is(err, ErrServerOnly) || len(r.Relations) != 0 {
			t.Fatal(err, r)
		}
	})
}

func TestInvalidPolicyShapesAreAtomic(t *testing.T) {
	now := fixedNow()
	s, p := storeFixture(t, specFixture(now))
	cases := []struct {
		name   string
		change func(*Specification)
	}{
		{"unknown_category", func(s *Specification) { s.Rules = []Rule{{"CLOSE", ReviewRequired}} }},
		{"unknown_preference", func(s *Specification) { s.Rules = []Rule{{UnknownPerson, "ALLOW"}} }},
		{"duplicate", func(s *Specification) { s.Rules = []Rule{{SameUniversity, Disabled}, {SameUniversity, ReviewRequired}} }},
		{"missing_valid_from", func(s *Specification) { s.ValidFrom = time.Time{} }},
		{"missing_expiry", func(s *Specification) { s.ExpiresAt = time.Time{} }},
		{"backward_expiry", func(s *Specification) { s.ExpiresAt = s.ValidFrom }},
		{"utc_year_overflow", func(s *Specification) {
			s.ExpiresAt = time.Date(9999, 12, 31, 23, 59, 59, 0, time.FixedZone("west", -14*3600))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := specFixture(now)
			tc.change(&spec)
			if s.Replace(1, spec) != ErrInvalid {
				t.Fatal("invalid policy accepted")
			}
			after, _ := s.Snapshot()
			if after.revision != p.revision {
				t.Fatal("invalid mutation")
			}
		})
	}
	t.Run("business_owner_not_active", func(t *testing.T) {
		a := agentFixture()
		a.Principal.Type = actorref.Business
		a.Role = agentruntime.BusinessAgent
		if _, err := NewStore(a, specFixture(now)); err != ErrInvalid {
			t.Fatal(err)
		}
	})
	t.Run("org_owner_not_person", func(t *testing.T) {
		a := agentFixture()
		a.Principal.Type = actorref.Organization
		a.Role = agentruntime.OrganizationAgent
		if _, err := NewStore(a, specFixture(now)); err != ErrInvalid {
			t.Fatal(err)
		}
	})
	t.Run("wrong_role", func(t *testing.T) {
		a := agentFixture()
		a.Role = agentruntime.OrganizationAgent
		if _, err := NewStore(a, specFixture(now)); err != ErrInvalid {
			t.Fatal(err)
		}
	})
	t.Run("nil_store", func(t *testing.T) {
		var s *Store
		if _, err := s.Snapshot(); err != ErrUnavailable || s.Replace(1, specFixture(now)) != ErrUnavailable || s.Revoke(1) != ErrUnavailable {
			t.Fatal(err)
		}
	})
}
