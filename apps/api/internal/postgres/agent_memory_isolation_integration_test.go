package postgres

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

// Uses committed native Memory/Evidence and active native Organization Agent.
// Business remains suspended. These random local accounts are not pilot users.
func TestMemoryIsolationNativeRolesAndExistingGrants(t *testing.T) {
	f := memoryEvidenceNativeFixture(t)
	p := f.native.base.private
	b := p.base
	private := savePrivateCanaries(t, p)
	f.attach(t, agentevent.MomentSource)
	f.attach(t, agentevent.SavedPlaceSource)
	before := f.provenance(t)
	if len(before.Evidence) != 2 {
		t.Fatal("actual current native evidence missing")
	}
	b.exec(`INSERT INTO organization_memberships(organization_id,user_account_id,role) VALUES($1,$2,'owner'),($1,$3,'admin')`, b.orgID, b.person.ID, b.other.ID)
	b.exec(`UPDATE user_profiles SET visibility='private' WHERE account_id=$1`, b.person.ID)
	if _, err := b.store.GrantProfileRead(b.ctx, b.person.ID, b.other.ID, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	ordinary, err := b.store.ReadProfile(b.ctx, b.other.ID, b.person.ID)
	if err != nil || ordinary.AccountID != b.person.ID {
		t.Fatal("real profile_view positive control failed")
	}
	assertNoPrivateCanaries(t, ordinary)
	var orgActive, bizDormant bool
	if err = b.pool.QueryRow(b.ctx, `SELECT (SELECT status='active' FROM agents WHERE id=$1),(SELECT status<>'active' FROM agents WHERE id=$2)`, b.orgAgentID, b.bizAgentID).Scan(&orgActive, &bizDormant); err != nil || !orgActive || !bizDormant || b.orgID == b.org.ID {
		t.Fatal("actual role/entity namespace control failed")
	}
	for _, c := range []struct {
		name   string
		access agentprofile.PrivateAccess
	}{
		{"other_person_claims_owner", agentprofile.PrivateAccess{SessionDigest: p.peer.SessionDigest, WorkspacePrincipal: b.person}},
		{"active_organization_session", p.org},
		{"personal_owner_switches_to_organization", agentprofile.PrivateAccess{SessionDigest: p.owner.SessionDigest, WorkspacePrincipal: b.org}},
		{"organization_admin_switches_workspace", agentprofile.PrivateAccess{SessionDigest: p.peer.SessionDigest, WorkspacePrincipal: b.org}},
		{"organization_entity_is_not_principal", agentprofile.PrivateAccess{SessionDigest: p.org.SessionDigest, WorkspacePrincipal: actorref.PrincipalRef{Type: actorref.Organization, ID: b.orgID}}},
		{"organization_claims_member", agentprofile.PrivateAccess{SessionDigest: p.org.SessionDigest, WorkspacePrincipal: b.person}},
		{"reserved_business_session", p.biz},
		{"business_claims_customer", agentprofile.PrivateAccess{SessionDigest: p.biz.SessionDigest, WorkspacePrincipal: b.person}},
		{"unknown_session", agentprofile.PrivateAccess{SessionDigest: [32]byte{60}, WorkspacePrincipal: b.person}},
	} {
		t.Run(c.name, func(t *testing.T) {
			mem, e := b.store.ReadOwnMemories(b.ctx, c.access)
			if !errors.Is(e, agentmemory.ErrForbidden) || mem != nil {
				t.Fatal("cross-subject native Memory payload")
			}
			profile, e := b.store.ReadOwnAgentPrivateProfile(b.ctx, c.access)
			requirePrivateProfileError(t, profile, e, agentprofile.ErrForbidden)
			provenance, e := b.store.ReadOwnMemoryProvenance(b.ctx, c.access, f.memory.ID)
			if !errors.Is(e, agentmemory.ErrForbidden) || !reflect.DeepEqual(provenance, agentmemory.Provenance{}) {
				t.Fatal("cross-subject native provenance payload")
			}
			raw, _ := json.Marshal([]any{mem, profile, provenance})
			if strings.Contains(string(raw), f.memory.ID) || strings.Contains(string(raw), "合成私密") {
				t.Fatal("denied native payload leaked a canary")
			}
		})
	}
	t.Run("administrator_personal_self_does_not_select_member_memory", func(t *testing.T) {
		mem, e := b.store.ReadOwnMemories(b.ctx, p.peer)
		if e != nil || len(mem) != 0 {
			t.Fatal("admin personal self got member Memory")
		}
		profile, e := b.store.ReadOwnAgentPrivateProfile(b.ctx, p.peer)
		if e != nil || profile.Configured || !agentprofile.PrivateFieldsEmpty(profile.Fields) {
			t.Fatal("admin personal self got member Profile")
		}
		prov, e := b.store.ReadOwnMemoryProvenance(b.ctx, p.peer, f.memory.ID)
		if !errors.Is(e, agentmemory.ErrNotFound) || !reflect.DeepEqual(prov, agentmemory.Provenance{}) {
			t.Fatal("admin personal self got member Evidence")
		}
	})
	t.Run("block_retains_private_self_but_removes_public_grant", func(t *testing.T) {
		if e := b.store.BlockAccount(b.ctx, b.person.ID, b.other.ID); e != nil {
			t.Fatal(e)
		}
		if _, e := b.store.ReadProfile(b.ctx, b.other.ID, b.person.ID); !errors.Is(e, identity.ErrNotFound) {
			t.Fatal("Block failed to override profile_view")
		}
		records, e := b.store.ReadOwnMemories(b.ctx, p.owner)
		if e != nil || len(records) != 1 || !reflect.DeepEqual(records[0], f.memory) {
			t.Fatal("role/grant/block changed owner's actual Memory")
		}
		profile, e := b.store.ReadOwnAgentPrivateProfile(b.ctx, p.owner)
		if e != nil || !reflect.DeepEqual(profile.Fields, private.Fields) {
			t.Fatal("owner private fields changed")
		}
		if got := f.provenance(t); !reflect.DeepEqual(got, before) {
			t.Fatal("role/grant/block changed actual Evidence")
		}
	})
	t.Log("LOCAL_DISPOSABLE_SYNTHETIC_ONLY: native Person/active Org/dormant Business, role/profile_view and actual SavedPlace do not grant private reads; SavedPlace is not proof of arrival")
}
