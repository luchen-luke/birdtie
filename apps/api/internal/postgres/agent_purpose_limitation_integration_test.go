package postgres

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/agentpurpose"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
)

func TestPurposeLimitationNativeRSVPDoesNotBecomeRetentionPermission(t *testing.T) {
	f := memoryEvidenceNativeFixture(t)
	p := f.native.base.private
	b := p.base
	// The actual committed participation is only the owner's manual reference.
	// It is NOT an Activity temporary delegation grant or attendance evidence.
	evidence := f.attach(t, agentevent.ParticipationSource)
	beforeProvenance := f.provenance(t)
	if evidence.Source == nil || len(beforeProvenance.Evidence) != 1 {
		t.Fatal("actual RSVP reference positive control missing")
	}
	b.exec(`INSERT INTO organization_memberships(organization_id,user_account_id,role) VALUES($1,$2,'owner'),($1,$3,'admin')`, b.orgID, b.person.ID, b.other.ID)
	b.exec(`UPDATE user_profiles SET visibility='private' WHERE account_id=$1`, b.person.ID)
	if _, err := b.store.GrantProfileRead(b.ctx, b.person.ID, b.other.ID, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if profile, err := b.store.ReadProfile(b.ctx, b.other.ID, b.person.ID); err != nil || profile.AccountID != b.person.ID {
		t.Fatal("actual limited profile_view positive control missing")
	}
	config, err := agentfeature.ParseConfig([]byte(`{"schemaVersion":"agent-feature-flags-v1","flags":{"agent_enrichment":true,"agent_memory":true,"agent_attention_policy":false,"agent_social_policy":false,"life_map":false},"pilot":{"memory":"basic","inference":"conservative","autonomousAction":false,"sensitiveInference":false}}`))
	if err != nil {
		t.Fatal(err)
	}
	controller, err := agentfeature.NewController(config)
	if err != nil {
		t.Fatal(err)
	}
	domains, err := agentcognitive.NewFeatureGatedDomains(controller, b.store)
	if err != nil {
		t.Fatal(err)
	}
	before := agentMemoryOwnedRowsSnapshot(t, p)
	effectsBefore := purposeOwnedControlRows(t, p)
	for _, recipient := range []struct {
		name      string
		principal actorref.PrincipalRef
		agentID   string
	}{
		{"owner_person", b.person, b.personID}, {"profile_view_admin_person", b.other, b.otherID}, {"active_organization", b.org, b.orgAgentID}, {"reserved_suspended_business", b.business, b.bizAgentID},
	} {
		t.Run(recipient.name, func(t *testing.T) {
			for _, ttl := range []time.Duration{time.Minute, 24 * time.Hour} {
				request := agentcognitive.ReadRequest{Version: agentcognitive.ContractVersion, Agent: agentcognitive.AgentReference{AgentID: recipient.agentID, Principal: recipient.principal, Role: agentruntime.ForType(recipient.principal.Type).Role}, Purpose: agentcognitive.SubmitMemoryCandidate, Scope: agentruntime.Private, Source: agentcognitive.SourceReference{Type: agentcognitive.MemoryEvidence, ID: evidence.ID, Owner: b.person, Version: evidence.Version}, ExpiresAt: time.Now().Add(ttl)}
				receipt, submitErr := domains.SubmitMemoryCandidate(b.ctx, agentcognitive.CandidateSubmission{Request: request, Sources: []agentcognitive.SourceReference{request.Source}, PayloadDigest: "hypothetical-reference-only"})
				want := agentpurpose.ErrUnavailable
				if recipient.principal.Type != actorref.Person {
					want = agentpurpose.ErrProhibited
				}
				if !errors.Is(submitErr, agentcognitive.ErrUnavailable) || !errors.Is(submitErr, want) || receipt.Status != agentcognitive.Unavailable {
					t.Fatal("RSVP, role, profile_view or longer TTL manufactured memory permission")
				}
				view, readErr := domains.ReadMemory(b.ctx, request)
				if !errors.Is(readErr, agentpurpose.ErrUnavailable) || !reflect.DeepEqual(view, agentcognitive.KnowledgeView{}) {
					t.Fatal("machine path released existing personal memory")
				}
			}
		})
	}
	for _, access := range []struct {
		name        string
		accessIndex int
	}{{"peer", 1}, {"org", 2}, {"business", 3}} {
		t.Run("human_writer_"+access.name, func(t *testing.T) {
			a := []agentprofile.PrivateAccess{p.owner, p.peer, p.org, p.biz}[access.accessIndex]
			if access.accessIndex == 1 {
				a.WorkspacePrincipal = b.person
			}
			input := agentMemoryInput("evidence-005")
			input.ExpectedVersion = f.memory.Version
			record, e := b.store.PutOwnMemory(b.ctx, a, f.memory.ID, input)
			requireAgentMemoryError(t, record, e, agentmemory.ErrForbidden)
		})
	}
	if agentMemoryOwnedRowsSnapshot(t, p) != before || purposeOwnedControlRows(t, p) != effectsBefore || !reflect.DeepEqual(f.provenance(t), beforeProvenance) {
		t.Fatal("purpose rejection changed full Memory/source or actual Evidence rows")
	}
	loaded, e := b.store.ReadOwnMemories(b.ctx, p.owner)
	if e != nil || len(loaded) != 1 || !reflect.DeepEqual(loaded[0], f.memory) {
		t.Fatal("ordinary explicit owner memory no longer works")
	}
	t.Log("LOCAL_SYNTHETIC_NATIVE_ONLY: actual RSVP and human profile_view exist, ON machine boundary rejects without promotion; no native temporary Activity grant or cleanup lifecycle claimed")
}

// Complete rows, not counts. Native fixtures owned by concurrent packages are
// excluded; the isolated runner separately checks every original public row.
func purposeOwnedControlRows(t *testing.T, p *agentPrivateFixture) string {
	t.Helper()
	var value string
	err := p.base.pool.QueryRow(p.base.ctx, `SELECT jsonb_build_object(
	 'evidence',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM agent_memory_evidence x WHERE owner_id=ANY($1::uuid[])),
	 'candidates',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM agent_memory_candidates x WHERE owner_id=ANY($1::uuid[])),
	 'outbox',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY event_id),'[]') FROM agent_domain_outbox x WHERE subject_id=ANY($1::uuid[])),
	 'inbox',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY event_id,handler_version),'[]') FROM agent_consumer_inbox x WHERE subject_id=ANY($1::uuid[])),
	 'effects',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY subject_id,effect_key),'[]') FROM agent_effect_ledger x WHERE subject_id=ANY($1::uuid[])),
	 'sessions',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM sessions x WHERE account_id=ANY($1::uuid[]))
	 )::text`, p.base.accounts).Scan(&value)
	if err != nil {
		t.Fatal("cannot snapshot actual complete native purpose control rows")
	}
	return value
}
