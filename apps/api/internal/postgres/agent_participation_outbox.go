package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemorycandidate"
	"github.com/birdtie/birdtie/apps/api/internal/agentoutbox"
	"github.com/birdtie/birdtie/apps/api/internal/agentreinforcement"
	"github.com/jackc/pgx/v5"
	"reflect"
	"time"
)

const participationOutboxCurrentSQL = `SELECT p.updated_at,p.status,p.xmin::text,birdtie_participation_outbox_fingerprint(p,p.xmin::text),to_jsonb(p),clock_timestamp(),coalesce((SELECT max(id) FROM agent_memory_source_invalidations i WHERE i.owner_id=p.participant_account_id AND i.source_type='ACTIVITY_PARTICIPATION' AND i.source_id=p.id),0),EXISTS(SELECT 1 FROM (`

type participationOutboxSource struct {
	at, observed                          time.Time
	status                                agentoutbox.SourceStatus
	row, fingerprint, snapshot, candidate string
	watermark                             int64
	usable                                bool
}

// Three separate namespaces: outbox metadata, original 057 snapshot token, and
// original 063 source fingerprint (which additionally includes native xmin).
func participationSourceTokens(source string, at time.Time, raw []byte, row string) (string, string, error) {
	v, e := agentevent.SnapshotVersion(agentevent.ParticipationSource, agentevent.UpdatedAtDigestVersion, at, raw)
	if e != nil {
		return "", "", agentoutbox.ErrUnavailable
	}
	epoch, _ := json.Marshal(struct {
		Epoch string `json:"epoch"`
	}{row})
	fp, e := agentreinforcement.Digest(struct {
		Selector agentmemorycandidate.Selector
		Version  agentevent.SourceVersion
		Time     time.Time
		Epoch    json.RawMessage
	}{agentmemorycandidate.Selector{Type: agentevent.ParticipationSource, ID: source}, v, at.UTC(), epoch})
	if e != nil {
		return "", "", agentoutbox.ErrUnavailable
	}
	return v.Token, fp, nil
}
func resolveParticipationOutboxTx(ctx context.Context, tx pgx.Tx, e agentoutbox.Envelope) (participationOutboxSource, bool, error) {
	var s participationOutboxSource
	if _, err := tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC';SET LOCAL jit=off`); err != nil {
		return s, false, agentoutbox.ErrUnavailable
	}
	source, _ := memoryEvidenceSourceSQL(agentevent.ParticipationSource)
	var raw []byte
	q := participationOutboxCurrentSQL + source + `) lawful) FROM activity_participations p WHERE p.id=$2 AND p.participant_account_id=$1 AND p.status IN('going','cancelled') FOR SHARE OF p`
	err := tx.QueryRow(ctx, q, e.Subject.ID, e.Source.ID).Scan(&s.at, &s.status, &s.row, &s.fingerprint, &raw, &s.observed, &s.watermark, &s.usable)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, false, nil
	}
	if err != nil {
		return s, false, agentoutbox.ErrUnavailable
	}
	s.at = s.at.UTC()
	s.observed = s.observed.UTC()
	s.snapshot, s.candidate, err = participationSourceTokens(e.Source.ID, s.at, raw, s.row)
	if err != nil {
		return s, false, err
	}
	current := s.status == e.Source.Status && s.fingerprint == e.Source.Fingerprint && s.at.Equal(e.OccurredAt) && !s.at.After(s.observed) && s.row != ""
	return s, current, nil
}
func participationRootAttemptsTx(ctx context.Context, tx pgx.Tx, e agentoutbox.Envelope) (int64, error) {
	var n int64
	if tx.QueryRow(ctx, `SELECT coalesce(sum(attempt),0)::bigint FROM agent_domain_outbox WHERE subject_id=$1 AND agent_id=$2 AND root_trace_id=$3 AND schema_version='agent-participation-outbox-v1'`, e.Subject.ID, e.AgentID, e.RootTraceID).Scan(&n) != nil || n < 0 || n > agentoutbox.MaxParticipationRootAttempts {
		return 0, agentoutbox.ErrUnavailable
	}
	return n, nil
}
func participationFinalTx(ctx context.Context, tx pgx.Tx, r agentoutbox.Record, c agentoutbox.Claim, admission time.Time, pinned ...*participationOutboxSource) error {
	var expected *participationOutboxSource
	if len(pinned) > 1 {
		return agentoutbox.ErrInvalid
	}
	if len(pinned) == 1 {
		expected = pinned[0]
	}
	source, current, err := resolveParticipationOutboxTx(ctx, tx, r.Event)
	if err != nil {
		return err
	}
	if !current || (expected != nil && (source.watermark != expected.watermark || source.row != expected.row || source.fingerprint != expected.fingerprint || source.snapshot != expected.snapshot || source.candidate != expected.candidate || source.usable != expected.usable)) {
		return agentoutbox.ErrConflict
	}
	var at time.Time
	if tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&at) != nil {
		return agentoutbox.ErrUnavailable
	}
	if err = agentoutbox.CheckFence(at, c, c); err != nil {
		return err
	}
	if !r.Event.ExpiresAt.After(at) {
		return agentoutbox.ErrExpired
	}
	if err = outboxControlAdmissionClock(admission, at); err != nil {
		return err
	}
	if _, err = participationRootAttemptsTx(ctx, tx, r.Event); err != nil {
		return err
	}
	// Revalidate the actual lease, current binding and original source in the
	// final snapshot after all cleanup/trigger waits; no Human Session is minted.
	var good bool
	if tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agent_domain_outbox d JOIN accounts a ON a.id=d.subject_id JOIN agents ag ON ag.id=d.agent_id JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=a.id AND ap.owner_type='PERSON' WHERE d.event_id=$1 AND d.subject_id=$2 AND d.agent_id=$3 AND d.fence=$4 AND((d.delivery_state='LEASED' AND d.lease_owner=$5 AND d.lease_until=$6 AND d.lease_until>clock_timestamp()) OR(d.delivery_state IN('PENDING','INVALIDATION_COMPLETE','DEAD_LETTER') AND d.lease_owner IS NULL AND d.lease_until IS NULL AND clock_timestamp()<$6::timestamptz AND EXISTS(SELECT 1 FROM agent_consumer_inbox i WHERE i.event_id=d.event_id AND i.subject_id=d.subject_id AND i.handler_version='participation-invalidation-v1' AND i.fence=d.fence AND i.attempt=d.attempt AND i.control_state=d.delivery_state AND i.updated_at<$6::timestamptz AND i.xmin::text=mod(pg_current_xact_id()::text::numeric,4294967296)::text AND(d.delivery_state<>'DEAD_LETTER' OR(i.reason_code='ROOT_BUDGET_EXHAUSTED' AND i.attempt=6 AND(SELECT coalesce(sum(attempt),0) FROM agent_domain_outbox root WHERE root.subject_id=d.subject_id AND root.agent_id=d.agent_id AND root.schema_version=d.schema_version AND root.root_trace_id=d.root_trace_id)=6))))) AND d.expires_at>clock_timestamp() AND a.account_type='person' AND a.status='active' AND ag.principal_account_id=a.id AND ag.agent_type='personal' AND ag.status='active' AND ap.profile_version>0)`, c.EventID, c.Subject.ID, c.AgentID, c.Fence, c.WorkerID, c.LeaseUntil).Scan(&good) != nil || !good {
		return agentoutbox.ErrConflict
	}
	return ctx.Err()
}
func sameParticipationEvent(a, b agentoutbox.Envelope) bool { return reflect.DeepEqual(a, b) }
