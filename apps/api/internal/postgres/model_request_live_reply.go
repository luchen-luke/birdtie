package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
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

// A reply write adds only human-readable native membership. Acquire the
// original human policy/relation locks before the budget owner and Run locks;
// taking them after a Task lock would invert the existing human writer order.
func (h *nativeLiveSourceAnswerRun) beginReplyCurrent(ctx context.Context) (pgx.Tx, storedModelRun, *nativeToolPolicy, error) {
	var empty storedModelRun
	if !h.brake(ctx) || !h.checkpoint.ValidAt(time.Now()) {
		return nil, empty, nil, modelegressbudget.ErrDenied
	}
	actor := identity.Actor{ID: h.owner, AccountType: "person"}
	tx, binding, policy, e := h.store.beginCurrentToolRead(ctx, actor, h.access.SessionDigest, resultProjectionRelations)
	if e != nil {
		return nil, empty, nil, e
	}
	fail := func(e error) (pgx.Tx, storedModelRun, *nativeToolPolicy, error) {
		tx.Rollback(context.Background())
		return nil, empty, nil, e
	}
	if binding.accountID != h.owner || binding.sessionID != h.session || binding.agentID != h.original.request.Agent.AgentID {
		return fail(modelegressbudget.ErrDenied)
	}
	if _, e = tx.Exec(ctx, `SET LOCAL jit=off`); e != nil {
		return fail(modelegressbudget.ErrUnavailable)
	}
	if e = egressOwnerLock(ctx, tx, h.owner); e != nil {
		return fail(e)
	}
	r, e := readModelRun(ctx, tx, h.id, h.owner)
	if e != nil {
		return fail(e)
	}
	if e = h.currentRunTx(ctx, tx, r, true); e != nil {
		return fail(e)
	}
	return tx, r, policy, nil
}

// FinalizeSourceReply appends the actual validated model text to the original
// conversation exactly once and, for supported human queries, captures native
// entity refs in the SAME Task transaction. No entity is exported to the model.
func (h *nativeLiveSourceAnswerRun) FinalizeSourceReply(ctx context.Context) (agentworkspace.LiveReply, error) {
	if h == nil {
		return nil, modelegressbudget.ErrDenied
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.brake(ctx) || !h.checkpoint.ValidAt(time.Now()) {
		return nil, modelegressbudget.ErrDenied
	}
	// This preliminary read chooses a lock-compatible branch only. Both paths
	// recheck the original Task generation and snapshot under their native locks
	// before any write; it supplies no entity or dispatch authority.
	observed, e := h.store.GetTask(ctx, h.owner, h.search.TaskID)
	if e != nil {
		return nil, e
	}
	supported := replyMembershipSupported(observed)
	var tx pgx.Tx
	var r storedModelRun
	var policy *nativeToolPolicy
	if supported {
		tx, r, policy, e = h.beginReplyCurrent(ctx)
	} else {
		tx, r, e = h.beginCurrent(ctx, true)
	}
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
	if task.Status != agentworkspace.TaskActive || replyMembershipSupported(task) != supported {
		return nil, modelegressbudget.ErrDenied
	}
	sources := make([]agentworkspace.AnswerSource, 0, len(h.batch.data.Sources))
	for i, s := range h.batch.data.Sources {
		sources = append(sources, agentworkspace.AnswerSource{ID: fmt.Sprintf("source-%d", i+1), Title: s.Title, URL: s.URL})
	}
	message := agentworkspace.Message{Role: "assistant", Text: h.completedResult.Text, Sources: sources, SourceRunID: h.id, SourceEvidenceDigest: h.batch.evidenceDigest}
	var history *nativeMessageResultsRead
	if supported {
		raw, marshalErr := json.Marshal(agentworkspace.SanitizeTaskForResponse(task))
		if marshalErr != nil {
			return nil, marshalErr
		}
		access := arp.Access{Actor: identity.Actor{ID: h.owner, AccountType: "person"}, SessionDigest: h.access.SessionDigest, TaskID: task.ID, ExpectedTask: raw}
		query, source, captureErr := h.store.captureReplyCurrentSourceTx(ctx, tx, access, task, policy)
		if captureErr != nil {
			return nil, captureErr
		}
		task, history, e = h.store.persistReplyMembershipTx(ctx, tx, access, task, policy, query.Kind, source, message)
	} else {
		task.Conversation = append(task.Conversation, message)
		task.Status = agentworkspace.TaskCompleted
		task, e = h.store.updateTaskInTx(ctx, tx, task)
	}
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
	// This final native batch runs after all original Run/preview/authority
	// waits. Earlier source reads cannot release a withdrawn historical card.
	if history != nil {
		if e = h.store.finalReplySourceProofsTx(ctx, tx, history.state().access, task, policy, history.state()); e != nil {
			return nil, e
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, egressError(e)
	}
	if history != nil && !nativeReplyReadUnexpired(history.state(), history.state().until, time.Now()) {
		return nil, arp.ErrChanged
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
