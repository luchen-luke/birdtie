package postgres

import (
	"context"
	"encoding/json"
	ap "github.com/birdtie/birdtie/apps/api/internal/activityparticipation"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentoutbox"
	"reflect"
	"testing"
)

func participationNativeCount(t *testing.T, f *rsvpPGFixture, source string) int {
	t.Helper()
	var n int
	if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM agent_domain_outbox WHERE schema_version='agent-participation-outbox-v1' AND subject_id=$1 AND source_id=$2`, f.a.Actor.ID, source).Scan(&n); e != nil {
		t.Fatal(e)
	}
	return n
}
func TestParticipationOutboxNativeHumanReceiptsReplayAndRollback(t *testing.T) {
	f := rsvpPG(t)
	activity := f.activity()
	op := ap.OperationInput{OperationID: f.uuid(), Operation: "JOIN"}
	bound := f.bound(activity, "JOIN")
	receipt, fresh, e := f.store.WriteHumanParticipationOperation(f.ctx, f.a, activity, op, bound)
	if e != nil || !fresh || receipt.State != "going" || participationNativeCount(t, f, receipt.EffectID) != 1 {
		t.Fatal("actual Human JOIN/event", e)
	}
	old := receipt
	again, fresh, e := f.store.WriteHumanParticipationOperation(f.ctx, f.a, activity, op, bound)
	if e != nil || fresh || !reflect.DeepEqual(again, old) || participationNativeCount(t, f, receipt.EffectID) != 1 {
		t.Fatal("Human operation replay recaptured metadata", e)
	}
	if _, fresh, e := f.store.JoinActivity(f.ctx, f.a.Actor.ID, activity); e != nil || fresh || participationNativeCount(t, f, receipt.EffectID) != 1 {
		t.Fatal("existing going captured mutation", e)
	}
	cancelOp := ap.OperationInput{OperationID: f.uuid(), Operation: "CANCEL_RSVP"}
	cancelled, fresh, e := f.store.WriteHumanParticipationOperation(f.ctx, f.a, activity, cancelOp, f.bound(activity, "CANCEL_RSVP"))
	if e != nil || !fresh || cancelled.EffectID != receipt.EffectID || cancelled.State != "cancelled" || participationNativeCount(t, f, receipt.EffectID) != 2 {
		t.Fatal("actual cancellation/event", e)
	}
	oldRead, e := f.store.ReadHumanParticipationOperation(f.ctx, f.a, activity, op.OperationID)
	if e != nil || !reflect.DeepEqual(oldRead, old) {
		t.Fatal("later cancellation changed original JOIN receipt", e)
	}
	rows, e := f.pool.Query(f.ctx, `SELECT `+outboxColumns+` FROM agent_domain_outbox d WHERE schema_version='agent-participation-outbox-v1' AND subject_id=$1 AND source_id=$2`, f.a.Actor.ID, receipt.EffectID)
	if e != nil {
		t.Fatal(e)
	}
	for rows.Next() {
		r, e := scanAgentOutbox(rows)
		if e != nil || agentoutbox.ValidateRecord(r) != nil || r.Event.Source.Revision != 0 || r.Event.CausationID != nil {
			rows.Close()
			t.Fatal("native capture not closed metadata", e)
		}
		raw, _ := json.Marshal(r)
		for _, forbidden := range []string{"attendance", "NO_PRIVATE_ACTIVITY_BODY_IN_RECEIPT", "COMMITTED", "operationId"} {
			if containsParticipationWire(raw, forbidden) {
				rows.Close()
				t.Fatal("metadata invented human effect/attendance or copied body")
			}
		}
	}
	rows.Close()
	// Fail the original causal receipt after native participation/outbox writes.
	f.exec(`CREATE FUNCTION participation133_receipt_fail() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN RAISE EXCEPTION 'owned receipt wait failure';END$$;CREATE TRIGGER participation133_receipt_fail BEFORE INSERT ON human_participation_operation_receipts FOR EACH ROW EXECUTE FUNCTION participation133_receipt_fail()`)
	t.Cleanup(func() {
		f.pool.Exec(context.Background(), `DROP TRIGGER IF EXISTS participation133_receipt_fail ON human_participation_operation_receipts;DROP FUNCTION IF EXISTS participation133_receipt_fail()`)
	})
	other := f.activity()
	bad := ap.OperationInput{OperationID: f.uuid(), Operation: "JOIN"}
	if _, _, e = f.store.WriteHumanParticipationOperation(f.ctx, f.a, other, bad, f.bound(other, "JOIN")); e == nil {
		t.Fatal("receipt failure committed domain")
	}
	var n int
	if e = f.pool.QueryRow(f.ctx, `SELECT(SELECT count(*) FROM activity_participations WHERE participant_account_id=$1 AND activity_id=$2)+(SELECT count(*) FROM agent_domain_outbox d JOIN activity_participations p ON p.id=d.source_id WHERE d.schema_version='agent-participation-outbox-v1' AND d.subject_id=$1 AND p.activity_id=$2)`, f.a.Actor.ID, other).Scan(&n); e != nil || n != 0 {
		t.Fatal("sameTx source/event rollback lost", e)
	}
}
func containsParticipationWire(raw []byte, value string) bool {
	for i := 0; i+len(value) <= len(raw); i++ {
		if string(raw[i:i+len(value)]) == value {
			return true
		}
	}
	return false
}

func TestParticipationOutboxNativeOriginalAgentJoinEffectPreserved(t *testing.T) {
	x := joinNative(t)
	b := x.f.f.native.private.base
	v := x.preview(t)
	x.confirm(t, v)
	receipt, e := x.g.ExecuteOwn(b.ctx, x.a, v.ApprovalID, v.BindingDigest)
	if e != nil || receipt.ParticipationID == nil {
		t.Fatal("actual original131 Agent JOIN", e)
	}
	var controls, human, effects int
	if e = b.pool.QueryRow(b.ctx, `SELECT(SELECT count(*) FROM agent_domain_outbox WHERE schema_version='agent-participation-outbox-v1' AND subject_id=$1 AND source_id=$2),(SELECT count(*) FROM human_participation_operation_receipts WHERE owner_account_id=$1),(SELECT count(*) FROM agent_activity_join_effects WHERE owner_id=$1 AND participation_id=$2)`, b.person.ID, *receipt.ParticipationID).Scan(&controls, &human, &effects); e != nil || controls != 1 || human != 0 || effects != 1 {
		t.Fatal("capture mixed Human/Agent causality", e)
	}
	worker := egressID(t, x.f)
	t.Run("InboxInitialRequiresNativeClaim", func(t *testing.T) {
		_, e := b.pool.Exec(b.ctx, `INSERT INTO agent_consumer_inbox(event_id,subject_id,handler_version,control_state,reason_code,fence,attempt) SELECT event_id,subject_id,'participation-invalidation-v1','PENDING','CLEANUP_MORE',fence,attempt FROM agent_domain_outbox WHERE schema_version='agent-participation-outbox-v1' AND subject_id=$1 AND source_id=$2`, b.person.ID, *receipt.ParticipationID)
		if e == nil {
			t.Fatal("inbox created without a native claim")
		}
	})
	r, c, e := b.store.ClaimAgentOutboxControlForSubject(b.ctx, actorref.PrincipalRef{Type: actorref.Person, ID: b.person.ID}, worker, agentoutbox.ParticipationHandler)
	if e != nil || r.Event.Source.ID != *receipt.ParticipationID {
		t.Fatal("native Agent-produced source claim", e)
	}
	t.Run("InboxCheckpointExactFence", func(t *testing.T) {
		if _, e := b.pool.Exec(b.ctx, `UPDATE agent_consumer_inbox SET control_state='PENDING',reason_code='CLEANUP_MORE',fence=fence+1 WHERE event_id=$1 AND subject_id=$2 AND handler_version='participation-invalidation-v1'`, c.EventID, c.Subject.ID); e == nil {
			t.Fatal("inbox mismatched fence accepted")
		}
	})
	result, e := b.store.ConsumeAgentOutboxControl(b.ctx, c)
	if e != nil || result.State != agentoutbox.MemoryComplete {
		t.Fatal("metadata-only current empty cleanup", e)
	}
	t.Run("InboxTerminalNeverRevives", func(t *testing.T) {
		if _, e := b.pool.Exec(b.ctx, `UPDATE agent_consumer_inbox SET control_state='LEASED',reason_code='',fence=fence+1,attempt=attempt+1 WHERE event_id=$1 AND subject_id=$2 AND handler_version='participation-invalidation-v1'`, c.EventID, c.Subject.ID); e == nil {
			t.Fatal("terminal inbox revived")
		}
		var state string
		if e := b.pool.QueryRow(b.ctx, `SELECT control_state FROM agent_consumer_inbox WHERE event_id=$1 AND subject_id=$2 AND handler_version='participation-invalidation-v1'`, c.EventID, c.Subject.ID).Scan(&state); e != nil || state != string(agentoutbox.MemoryComplete) {
			t.Fatal("rejected terminal write changed history", e)
		}
	})
	after, e := x.g.ReconcileOwn(b.ctx, x.a, v.ApprovalID)
	if e != nil || !reflect.DeepEqual(func() any { q := receipt; q.Proof = nil; return q }(), func() any { q := after; q.Proof = nil; return q }()) {
		t.Fatal("maintenance changed original effect receipt", e)
	}
}
