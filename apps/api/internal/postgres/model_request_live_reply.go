package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"github.com/birdtie/birdtie/apps/api/internal/modelrequestrun"
	"github.com/jackc/pgx/v5"
)

// A completed reply is held only by the originating native Run. Persisted
// conversation citations are presentation data, not authority for later calls.
type nativeLiveSourceReply struct {
	run                    *nativeLiveSourceAnswerRun
	task                   agentworkspace.Task
	taskDigest, generation string
	revision               int64
	sources                []agentworkspace.AnswerSource
}

var _ agentworkspace.LiveReply = (*nativeLiveSourceReply)(nil)

func (*nativeLiveSourceReply) MarshalJSON() ([]byte, error) {
	return nil, modelegressbudget.ErrServerOnly
}
func (*nativeLiveSourceReply) UnmarshalJSON([]byte) error { return modelegressbudget.ErrServerOnly }
func (*nativeLiveSourceReply) Format(s fmt.State, _ rune) {
	_, _ = io.WriteString(s, "nativeLiveSourceReply{redacted}")
}
func (p *nativeLiveSourceReply) Task() agentworkspace.Task {
	if p == nil {
		return agentworkspace.Task{}
	}
	raw, _ := json.Marshal(p.task)
	var task agentworkspace.Task
	_ = json.Unmarshal(raw, &task)
	return task
}
func (p *nativeLiveSourceReply) Answer(requestID string) (*agentworkspace.SourcedAnswer, error) {
	if p == nil || p.run == nil || p.run.completedResult == nil {
		return nil, modelegressbudget.ErrDenied
	}
	responseTask := agentworkspace.SanitizeTaskForResponse(p.task)
	query, e := agentworkspace.SourcedAnswerQueryDigest(responseTask)
	if e != nil {
		return nil, e
	}
	responseDigest, e := agentworkspace.SourcedAnswerTaskDigest(responseTask)
	if e != nil {
		return nil, e
	}
	return agentworkspace.NewSourcedAnswer(agentworkspace.SourcedAnswerBinding{
		TaskID: p.task.ID, RequestID: requestID, CurrentQueryDigest: query, TaskSnapshotDigest: responseDigest,
		SourceEvidenceDigest: p.run.batch.evidenceDigest, RunID: p.run.id,
		GeneratedAt: p.task.UpdatedAt, ValidUntil: p.run.deadline,
	}, p.run.completedResult.Text, p.sources)
}

// Check immutable source/operation facts without treating the legal final
// Task status transition as fresh dispatch authority. This helper never sends.
func (h *nativeLiveSourceAnswerRun) replyFactsTx(ctx context.Context, tx pgx.Tx, r storedModelRun) error {
	if e := h.store.revalidateResolvedPublicCityTx(ctx, tx, h.original); e != nil {
		return e
	}
	if !h.matchesRun(r, true) || r.control.State != modelrequestrun.RunFinished || h.completedResult == nil || !h.batch.valid || len(r.control.Steps) != 2 {
		return modelegressbudget.ErrDenied
	}
	if e := validateLiveSourceBatchData(h.batch.data, h.batch.evidenceDigest, time.Now()); e != nil {
		return e
	}
	for i, step := range r.control.Steps {
		if step.State != modelrequestrun.StepUnknown || step.BindingState != modelrequestrun.Bound {
			return modelegressbudget.ErrDenied
		}
		reservation, kind, e := readLiveReservation(ctx, tx, step.OperationID, h.owner)
		if e != nil {
			return e
		}
		if reservation.State != "UNKNOWN" || reservation.ExecutionStatus != "LIVE_ATTEMPTED" || reservation.RequestDigest != step.RequestDigest || reservation.PreviewID != step.PreviewID || reservation.RootTraceID != h.search.RootTraceID || reservation.TaskID != h.search.TaskID {
			return modelegressbudget.ErrDenied
		}
		v, e := readLiveEgressPreview(ctx, tx, step.PreviewID)
		if e != nil {
			return e
		}
		if v.status != "APPROVED" || v.owner != h.owner || v.session != h.session || v.binding != h.original.binding || v.source != h.original.source || v.authority != h.original.authority || v.digest != step.RequestDigest || !v.in.DeadlineAt.After(time.Now()) {
			return modelegressbudget.ErrDenied
		}
		if i == 0 {
			if kind != modelegressbudget.LiveCall || v.kind != kind || v.digest != h.batch.data.Evidence.SourceRequestDigest || step.PreviewID != h.batch.data.Evidence.PreviewID {
				return modelegressbudget.ErrDenied
			}
		} else {
			data, e := readLiveSourceBatchData(v.sourceBatch)
			if e != nil || kind != modelegressbudget.LiveToken || v.kind != kind || v.scope != liveModelSourceScope(h.original) || v.sourceEvidenceDigest != h.batch.evidenceDigest || step.SourceEvidenceDigest != h.batch.evidenceDigest || validateLiveSourceBatchData(data, h.batch.evidenceDigest, time.Now()) != nil {
				return modelegressbudget.ErrDenied
			}
			stored, _ := json.Marshal(data)
			expected, _ := json.Marshal(h.batch.data)
			if string(stored) != string(expected) {
				return modelegressbudget.ErrDenied
			}
		}
	}
	return egressFinish(ctx, tx, h.session, nil, h.deadline, r.control.LeaseUntil)
}

// FinalizeSourceReply appends the actual validated model text to the original
// conversation exactly once. It preserves every filter and entity reference.
func (h *nativeLiveSourceAnswerRun) FinalizeSourceReply(ctx context.Context) (agentworkspace.LiveReply, error) {
	if h == nil {
		return nil, modelegressbudget.ErrDenied
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	tx, r, e := h.beginCurrent(ctx, true)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	if e = h.replyFactsTx(ctx, tx, r); e != nil {
		return nil, e
	}
	task, e := scanAgentTask(tx.QueryRow(ctx, `SELECT `+agentTaskColumns+` FROM agent_tasks WHERE id=$1 AND owner_account_id=$2 FOR UPDATE`, h.search.TaskID, h.owner))
	if e != nil {
		return nil, egressError(e)
	}
	if task.Status != agentworkspace.TaskActive {
		return nil, modelegressbudget.ErrDenied
	}
	sources := make([]agentworkspace.AnswerSource, 0, len(h.batch.data.Sources))
	for i, s := range h.batch.data.Sources {
		sources = append(sources, agentworkspace.AnswerSource{ID: fmt.Sprintf("source-%d", i+1), Title: s.Title, URL: s.URL})
	}
	task.Conversation = append(task.Conversation, agentworkspace.Message{Role: "assistant", Text: h.completedResult.Text, Sources: sources, SourceRunID: h.id, SourceEvidenceDigest: h.batch.evidenceDigest})
	task.Status = agentworkspace.TaskCompleted
	task, e = h.store.updateTaskInTx(ctx, tx, task)
	if e != nil {
		return nil, e
	}
	digest, e := agentworkspace.SourcedAnswerTaskDigest(task)
	if e != nil {
		return nil, e
	}
	var generation string
	if e = tx.QueryRow(ctx, `SELECT xmin::text FROM agent_tasks WHERE id=$1`, task.ID).Scan(&generation); e != nil {
		return nil, egressError(e)
	}
	p := &nativeLiveSourceReply{run: h, task: task, taskDigest: digest, generation: generation, revision: r.control.Revision, sources: sources}
	if e = h.replyFactsTx(ctx, tx, r); e != nil || !h.brake(ctx) {
		return nil, modelegressbudget.ErrDenied
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, egressError(e)
	}
	if e = p.Revalidate(ctx); e != nil {
		return nil, e
	}
	return p, nil
}

func (p *nativeLiveSourceReply) Revalidate(ctx context.Context) error {
	if p == nil || p.run == nil || !p.run.brake(ctx) || !p.run.checkpoint.ValidAt(time.Now()) {
		return modelegressbudget.ErrDenied
	}
	h := p.run
	tx, e := h.store.beginEgress(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	owner, session, e := h.store.egressOwner(ctx, tx, h.access)
	if e != nil || owner != h.owner || session != h.session {
		return modelegressbudget.ErrDenied
	}
	if e = egressOwnerLock(ctx, tx, owner); e != nil {
		return e
	}
	r, e := readModelRun(ctx, tx, h.id, owner)
	if e != nil || r.control.Revision != p.revision {
		return modelegressbudget.ErrDenied
	}
	current, e := lockModelConfigurationTask(ctx, tx, h.access, p.task.ID, h.store.devPhoneEnabled)
	if e != nil || current.status != agentworkspace.TaskCompleted || current.agent != h.original.request.Agent {
		return modelegressbudget.ErrDenied
	}
	task, e := scanAgentTask(tx.QueryRow(ctx, `SELECT `+agentTaskColumns+` FROM agent_tasks WHERE id=$1 AND owner_account_id=$2`, p.task.ID, owner))
	if e != nil {
		return e
	}
	digest, e := agentworkspace.SourcedAnswerTaskDigest(task)
	if e != nil || digest != p.taskDigest {
		return modelegressbudget.ErrDenied
	}
	var generation string
	if e = tx.QueryRow(ctx, `SELECT xmin::text FROM agent_tasks WHERE id=$1`, task.ID).Scan(&generation); e != nil || generation != p.generation {
		return modelegressbudget.ErrDenied
	}
	authority, e := liveReplyAuthorityTx(ctx, tx, current)
	if e != nil || authority != h.original.authority {
		return modelegressbudget.ErrDenied
	}
	if e = h.replyFactsTx(ctx, tx, r); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return egressError(e)
	}
	if !h.brake(ctx) || !h.checkpoint.ValidAt(time.Now()) {
		return modelegressbudget.ErrDenied
	}
	return nil
}

// The original 062 authority fingerprint is retained after the sole permitted
// reply write. A profile/account/Agent disable and restore cannot revive it.
func liveReplyAuthorityTx(ctx context.Context, tx pgx.Tx, current currentConfigurationTask) (string, error) {
	var raw, accountRaw, agentRaw []byte
	var generation, accountGeneration, agentGeneration string
	e := tx.QueryRow(ctx, `SELECT to_jsonb(ap),ap.xmin::text,to_jsonb(actor),actor.xmin::text,to_jsonb(ag),ag.xmin::text
 FROM agent_profiles ap JOIN accounts actor ON actor.id=ap.owner_id JOIN agents ag ON ag.id=ap.agent_id
 WHERE ap.agent_id=$1 AND ap.owner_id=$2 AND ap.owner_type='PERSON' AND actor.status='active' AND ag.status='active'
 FOR SHARE OF ap,actor,ag`, current.agent.AgentID, current.agent.Principal.ID).Scan(&raw, &generation, &accountRaw, &accountGeneration, &agentRaw, &agentGeneration)
	if e != nil {
		return "", egressError(e)
	}
	material := append([]byte("ap:"+generation+"\x00"), raw...)
	material = append(material, []byte("\x00account:"+accountGeneration+"\x00")...)
	material = append(material, accountRaw...)
	material = append(material, []byte("\x00agent:"+agentGeneration+"\x00")...)
	material = append(material, agentRaw...)
	return liveDigestBytes("birdtie.model-egress.authority.v1", material), nil
}
