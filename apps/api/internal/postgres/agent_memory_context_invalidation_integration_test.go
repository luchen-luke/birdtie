package postgres

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentoutbox"
)

func TestMemoryInvalidationNativeOriginalStoreTwoStages(t *testing.T) {
	ownedMigrationDatabase(t)
	f := contextPurposeNative(t)
	b := f.f.place.private.base
	f.selection.ProfileFields = nil
	f.selection.PlaceIDs = nil
	f.selection.ActivityIDs = nil
	f.selection.RelationshipTieIDs = nil
	f.selection.PolicyFamilies = nil
	old, grant := f.approve(t)
	unbound, e := b.store.PreviewOwnContextPurpose(b.ctx, f.f.place.private.owner, f.selection)
	if e != nil {
		t.Fatal(e)
	}
	saved, e := b.store.PutOwnMemory(b.ctx, f.f.place.private.owner, f.memory, agentmemory.PutInput{ExpectedVersion: 1, MemoryType: agentmemory.TypePreference, MemoryKey: "context.explicit.native", Summary: "本人修改的明确声明_NATIVE_MEMORY", StructuredValue: json.RawMessage(`{"declared":true}`), Visibility: agentmemory.VisibilityPrivate, ValidUntil: f.f.now.Add(time.Hour)})
	if e != nil || saved.Version != 2 {
		t.Fatal("original human Memory writer", e)
	}
	var worker string
	if e = b.pool.QueryRow(b.ctx, `SELECT gen_random_uuid()::text`).Scan(&worker); e != nil {
		t.Fatal(e)
	}
	r, c, e := b.store.ClaimAgentOutboxControlForSubject(b.ctx, b.person, worker, agentoutbox.MemoryHandler)
	if e == agentoutbox.ErrUnavailable && r == (agentoutbox.Record{}) && c == (agentoutbox.Claim{}) {
		var state string
		var attempt int64
		if check := b.pool.QueryRow(b.ctx, `SELECT delivery_state,attempt FROM agent_domain_outbox WHERE subject_id=$1 AND source_id=$2 AND source_revision=1 AND event_type='MEMORY_UPDATED'`, b.person.ID, f.memory).Scan(&state, &attempt); check != nil || state != "INVALIDATED" || attempt != 0 {
			t.Fatal("stale original Memory root did not commit terminal progress", check)
		}
		r, c, e = b.store.ClaimAgentOutboxControlForSubject(b.ctx, b.person, worker, agentoutbox.MemoryHandler)
	}
	if e != nil || r.Event.Source.ID != f.memory || r.Event.Source.Revision != saved.Version {
		t.Fatal("original Memory Store claim", e)
	}
	p, e := b.store.ConsumeAgentOutboxControl(b.ctx, c)
	if e != nil {
		// Real Store failure remains the RED oracle. Diagnostic invokes the same
		// transaction function in a new rollback-only native transaction.
		tx, begin := b.pool.Begin(b.ctx)
		if begin == nil {
			defer tx.Rollback(context.Background())
			_, _ = consumeMemoryOutboxTx(b.ctx, preferenceTraceTx{tx, t}, c)
		}
		t.Fatal("original Memory Store parent consume", e)
	}
	if p.State != agentoutbox.MemoryComplete {
		t.Fatal("parent did not complete", p)
	}
	_, child, e := b.store.ClaimAgentOutboxControlForSubject(b.ctx, b.person, worker, agentoutbox.MemoryHandler)
	if e != nil {
		t.Fatal(e)
	}
	if p, e = b.store.ConsumeAgentOutboxControl(b.ctx, child); e != nil || p.State != agentoutbox.MemoryComplete {
		t.Fatal("original Memory Store child consume", e)
	}
	var absent, bound bool
	var revision int64
	var revoked *time.Time
	if e = b.pool.QueryRow(b.ctx, `SELECT NOT EXISTS(SELECT 1 FROM agent_context_purpose_previews WHERE id=$1),EXISTS(SELECT 1 FROM agent_context_purpose_previews WHERE id=$2)`, unbound.ID, old.ID).Scan(&absent, &bound); e != nil || !absent || !bound {
		t.Fatal("original Memory preview cleanup/history", e)
	}
	if e = b.pool.QueryRow(b.ctx, `SELECT revision,revoked_at FROM consent_grants WHERE id=$1`, grant.ID).Scan(&revision, &revoked); e != nil || revision != grant.Revision+1 || revoked == nil {
		t.Fatal("original Memory sole grant CAS", e)
	}
}
