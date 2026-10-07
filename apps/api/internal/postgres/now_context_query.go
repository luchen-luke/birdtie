package postgres

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/agentnotification"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	nq "github.com/birdtie/birdtie/apps/api/internal/nowcontextquery"
	"github.com/jackc/pgx/v5"
	"reflect"
	"sync"
	"time"
)

var _ nq.Store = (*Store)(nil)
var onlineKey struct {
	once sync.Once
	key  [32]byte
	err  error
}

func onlineSeal(a nq.Access, r nq.Receipt) (string, error) {
	onlineKey.once.Do(func() { _, onlineKey.err = rand.Read(onlineKey.key[:]) })
	if onlineKey.err != nil {
		return "", nq.ErrUnavailable
	}
	raw, e := json.Marshal(struct {
		Owner, Digest, Proof string
		Response             nq.Response
	}{a.Actor.ID, hex.EncodeToString(a.Digest[:]), r.Proof, r.Response})
	if e != nil {
		return "", nq.ErrUnavailable
	}
	m := hmac.New(sha256.New, onlineKey.key[:])
	m.Write(raw)
	return hex.EncodeToString(m.Sum(nil)), nil
}

type onlineBinding struct{ AgentID, AgentToken, OwnerToken, SessionID, SessionCreated string }
type onlineFrame struct {
	Binding                                                        onlineBinding
	ContextToken, DeclarationToken, DeclarationRelation, TaskToken string
	Sources                                                        map[string]string
}

func (s *Store) onlineBegin(ctx context.Context, a nq.Access) (pgx.Tx, onlineBinding, error) {
	var b onlineBinding
	if ctx == nil || ctx.Err() != nil || s == nil || s.pool == nil || !a.Valid() {
		return nil, b, nq.ErrDenied
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return nil, b, nq.ErrUnavailable
	}
	fail := func(e error) (pgx.Tx, onlineBinding, error) { tx.Rollback(context.Background()); return nil, b, e }
	if _, e = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); e != nil {
		return fail(nq.ErrUnavailable)
	}
	if _, e = tx.Exec(ctx, `LOCK TABLE accounts,agents,sessions,contexts,person_contexts,social_intents,social_intent_audience_targets,account_blocks,agent_tasks,native_notification_decisions,native_notification_policies,inbox_items IN ACCESS SHARE MODE`); e != nil {
		return fail(nq.ErrUnavailable)
	}
	e = tx.QueryRow(ctx, `SELECT xmin::text FROM accounts WHERE id=$1 AND account_type='person' AND status='active' FOR SHARE`, a.Actor.ID).Scan(&b.OwnerToken)
	if errors.Is(e, pgx.ErrNoRows) {
		return fail(nq.ErrDenied)
	}
	if e != nil {
		return fail(nq.ErrUnavailable)
	}
	e = tx.QueryRow(ctx, `SELECT id::text,xmin::text FROM agents WHERE principal_account_id=$1 AND agent_type='personal' AND status='active' FOR SHARE`, a.Actor.ID).Scan(&b.AgentID, &b.AgentToken)
	if errors.Is(e, pgx.ErrNoRows) {
		return fail(nq.ErrDenied)
	}
	if e != nil {
		return fail(nq.ErrUnavailable)
	}
	return tx, b, nil
}
func (s *Store) onlineSession(ctx context.Context, tx pgx.Tx, a nq.Access, b *onlineBinding) error {
	e := tx.QueryRow(ctx, `SELECT id::text,created_at::text FROM sessions WHERE token_sha256=$1 AND account_id=$2 FOR SHARE`, a.Digest[:], a.Actor.ID).Scan(&b.SessionID, &b.SessionCreated)
	if errors.Is(e, pgx.ErrNoRows) {
		return identity.ErrUnauthorized
	}
	if e != nil {
		return nq.ErrUnavailable
	}
	return checkHumanMomentSession(ctx, tx, a.Digest, a.Actor.ID, s.devPhoneEnabled)
}
func onlineContext(ctx context.Context, tx pgx.Tx, a nq.Access, id string, frame *onlineFrame) (nq.Context, error) {
	var c nq.Context
	c.Type = "ONLINE"
	e := tx.QueryRow(ctx, `SELECT c.id::text,c.online_key,c.xmin::text,pc.xmin::text,pc.relation FROM contexts c JOIN person_contexts pc ON pc.context_id=c.id AND pc.person_account_id=$2 WHERE c.id=$1 AND c.context_type='ONLINE' AND pc.visibility='private' AND pc.relation IN ('interest','current','affiliation') ORDER BY pc.relation LIMIT 1 FOR SHARE OF c,pc`, id, a.Actor.ID).Scan(&c.ID, &c.Label, &frame.ContextToken, &frame.DeclarationToken, &frame.DeclarationRelation)
	if errors.Is(e, pgx.ErrNoRows) {
		return c, nq.ErrNotFound
	}
	if e != nil {
		return c, nq.ErrUnavailable
	}
	return c, nil
}

const onlineItemSelect = `SELECT i.id::text,i.title,i.modality,i.context_id::text,i.updated_at,i.expires_at,i.xmin::text,creator.xmin::text FROM social_intents i JOIN accounts creator ON creator.id=i.creator_account_id AND creator.account_type='person' AND creator.status='active' WHERE i.modality='ONLINE' AND i.audience='PUBLIC' AND i.status='ACTIVE' AND i.context_id=$1 AND isfinite(i.expires_at) AND i.expires_at>clock_timestamp() AND NOT EXISTS(SELECT 1 FROM account_blocks bl WHERE (bl.blocker_account_id=$2 AND bl.blocked_account_id=creator.id) OR (bl.blocked_account_id=$2 AND bl.blocker_account_id=creator.id))`

func onlineItems(ctx context.Context, tx pgx.Tx, a nq.Access, cid, query, id string, lock bool) ([]nq.Item, map[string]string, bool, error) {
	tail := ` AND ($3::uuid IS NULL OR i.id=$3) AND (cardinality($4::text[])=0 OR EXISTS(SELECT 1 FROM unnest($4::text[]) term WHERE strpos(lower(i.title),term)>0)) ORDER BY i.updated_at DESC,i.id DESC LIMIT 21`
	if lock {
		tail += ` FOR SHARE OF i`
	}
	var selected any
	if id != "" {
		selected = id
	}
	terms := nq.Terms(query)
	if terms == nil {
		terms = []string{}
	}
	rows, e := tx.Query(ctx, onlineItemSelect+tail, cid, a.Actor.ID, selected, terms)
	if e != nil {
		return nil, nil, false, nq.ErrUnavailable
	}
	defer rows.Close()
	items := []nq.Item{}
	sources := map[string]string{}
	for rows.Next() {
		var x nq.Item
		var token, actorToken string
		if e = rows.Scan(&x.ID, &x.Title, &x.Modality, &x.ContextID, &x.SourceUpdatedAt, &x.ExpiresAt, &token, &actorToken); e != nil {
			return nil, nil, false, nq.ErrUnavailable
		}
		items = append(items, x)
		sources[x.ID] = token + ":" + actorToken
	}
	if rows.Err() != nil {
		return nil, nil, false, nq.ErrUnavailable
	}
	truncated := len(items) > nq.MaxResults
	if truncated {
		delete(sources, items[len(items)-1].ID)
		items = items[:nq.MaxResults]
	}
	return items, sources, truncated, nil
}
func onlineTask(ctx context.Context, tx pgx.Tx, a nq.Access, id string, write bool) (agentworkspace.Task, string, error) {
	var t agentworkspace.Task
	var token string
	q := `SELECT ` + agentTaskColumns + ` FROM agent_tasks WHERE id=$1 AND owner_account_id=$2 AND principal_type='person'`
	if write {
		q += ` FOR UPDATE`
	} else {
		q += ` FOR SHARE`
	}
	var e error
	t, e = scanAgentTask(tx.QueryRow(ctx, q, id, a.Actor.ID))
	if errors.Is(e, agentworkspace.ErrNotFound) {
		return t, "", nq.ErrNotFound
	}
	if e != nil {
		return t, "", nq.ErrUnavailable
	}
	if t.ContextType != "ONLINE" || t.CityID != "" || t.Intent != nq.Intent || t.ActingUserID != a.Actor.ID {
		return t, "", nq.ErrDenied
	}
	e = tx.QueryRow(ctx, `SELECT xmin::text FROM agent_tasks WHERE id=$1`, id).Scan(&token)
	return t, token, e
}
func onlineOptions(ctx context.Context, tx pgx.Tx, a nq.Access, lock bool) ([]nq.Context, map[string]string, error) {
	q := `WITH selected AS MATERIALIZED (SELECT c.id FROM contexts c JOIN person_contexts pc ON pc.context_id=c.id WHERE pc.person_account_id=$1 AND c.context_type='ONLINE' AND pc.visibility='private' AND pc.relation IN ('interest','current','affiliation') GROUP BY c.id ORDER BY c.id LIMIT 20)
 SELECT c.id::text,c.online_key,'ONLINE',c.xmin::text,pc.xmin::text,pc.relation FROM selected JOIN contexts c ON c.id=selected.id JOIN person_contexts pc ON pc.context_id=c.id WHERE pc.person_account_id=$1 AND pc.visibility='private' AND pc.relation IN ('interest','current','affiliation') ORDER BY c.id,pc.relation`
	if lock {
		q += ` FOR SHARE OF c,pc`
	}
	rows, e := tx.Query(ctx, q, a.Actor.ID)
	if e != nil {
		return nil, nil, nq.ErrUnavailable
	}
	defer rows.Close()
	contexts := []nq.Context{}
	sources := map[string]string{}
	seen := map[string]bool{}
	for rows.Next() {
		var x nq.Context
		var ct, pt, relation string
		if e = rows.Scan(&x.ID, &x.Label, &x.Type, &ct, &pt, &relation); e != nil {
			return nil, nil, nq.ErrUnavailable
		}
		if !seen[x.ID] {
			contexts = append(contexts, x)
			seen[x.ID] = true
		}
		sources[x.ID+":"+relation] = ct + ":" + pt
	}
	if rows.Err() != nil {
		return nil, nil, nq.ErrUnavailable
	}
	return contexts, sources, nil
}
func onlineOptionsSeal(a nq.Access, r nq.ContextListReceipt) (string, error) {
	raw, e := json.Marshal(r.Contexts)
	if e != nil {
		return "", nq.ErrUnavailable
	}
	return onlineSeal(a, nq.Receipt{Proof: "online-options:" + r.Proof, Response: nq.Response{Schema: "now-online-context-options-seal-v1", Answer: string(raw)}})
}
func (s *Store) ListOwnOnlineContexts(ctx context.Context, a nq.Access) (nq.ContextListReceipt, error) {
	var result nq.ContextListReceipt
	tx, b, e := s.onlineBegin(ctx, a)
	if e != nil {
		return result, e
	}
	defer tx.Rollback(context.Background())
	if _, _, e = onlineOptions(ctx, tx, a, true); e != nil {
		return result, e
	}
	if e = s.onlineSession(ctx, tx, a, &b); e != nil {
		return result, e
	}
	contexts, sources, e := onlineOptions(ctx, tx, a, false)
	if e != nil {
		return result, e
	}
	if e = checkHumanMomentSession(ctx, tx, a.Digest, a.Actor.ID, s.devPhoneEnabled); e != nil {
		return result, e
	}
	raw, e := json.Marshal(onlineFrame{Binding: b, Sources: sources})
	if e != nil {
		return result, nq.ErrUnavailable
	}
	result = nq.ContextListReceipt{Contexts: contexts, Proof: string(raw)}
	result.Seal, e = onlineOptionsSeal(a, result)
	if e != nil {
		return nq.ContextListReceipt{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return nq.ContextListReceipt{}, nq.ErrUnavailable
	}
	return result, nil
}
func (s *Store) RevalidateOwnOnlineContexts(ctx context.Context, a nq.Access, r nq.ContextListReceipt) error {
	seal, e := onlineOptionsSeal(a, r)
	if e != nil || r.Seal == "" || !hmac.Equal([]byte(seal), []byte(r.Seal)) {
		return nq.ErrDenied
	}
	var expected onlineFrame
	if json.Unmarshal([]byte(r.Proof), &expected) != nil {
		return nq.ErrDenied
	}
	tx, b, e := s.onlineBegin(ctx, a)
	if e != nil {
		return e
	}
	defer tx.Rollback(context.Background())
	if _, _, e = onlineOptions(ctx, tx, a, true); e != nil {
		return e
	}
	if e = s.onlineSession(ctx, tx, a, &b); e != nil {
		return e
	}
	contexts, sources, e := onlineOptions(ctx, tx, a, false)
	if e != nil {
		return e
	}
	if !reflect.DeepEqual(contexts, r.Contexts) || !reflect.DeepEqual(onlineFrame{Binding: b, Sources: sources}, expected) {
		return nq.ErrConflict
	}
	if e = checkHumanMomentSession(ctx, tx, a.Digest, a.Actor.ID, s.devPhoneEnabled); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return nq.ErrUnavailable
	}
	return nil
}
func (s *Store) QueryOwnPublicOnline(ctx context.Context, a nq.Access, in nq.Input) (nq.Receipt, error) {
	if in.Valid() != nil {
		return nq.Receipt{}, nq.ErrInvalid
	}
	return s.onlineRead(ctx, a, in.ContextID, in.Query, in.TaskID, "", &in)
}
func (s *Store) RestoreOwnPublicOnline(ctx context.Context, a nq.Access, id string) (nq.Receipt, error) {
	if !validHumanSocialID(id) {
		return nq.Receipt{}, nq.ErrInvalid
	}
	return s.onlineRead(ctx, a, "", "", id, "", nil)
}
func (s *Store) ReadOwnPublicOnlineIntent(ctx context.Context, a nq.Access, id string) (nq.Receipt, error) {
	if !validHumanSocialID(id) {
		return nq.Receipt{}, nq.ErrInvalid
	}
	return s.onlineRead(ctx, a, "", "", "", id, nil)
}
func validHumanSocialID(id string) bool { return nq.Input{ContextID: id, Query: "x"}.Valid() == nil }
func (s *Store) onlineRead(ctx context.Context, a nq.Access, cid, query, taskID, itemID string, in *nq.Input) (nq.Receipt, error) {
	var out nq.Receipt
	tx, b, e := s.onlineBegin(ctx, a)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(context.Background())
	frame := onlineFrame{Binding: b, Sources: map[string]string{}}
	var task agentworkspace.Task
	if taskID != "" {
		task, frame.TaskToken, e = onlineTask(ctx, tx, a, taskID, in != nil)
		if e != nil {
			return out, e
		}
		if in != nil {
			at, er := time.Parse(time.RFC3339Nano, in.ExpectedTaskUpdatedAt)
			if er != nil || !at.Equal(task.UpdatedAt) || in.ContextID != task.ContextID {
				return out, nq.ErrConflict
			}
		} else {
			cid = task.ContextID
			query = task.Filters["currentQuery"]
		}
	}
	if itemID != "" {
		e = tx.QueryRow(ctx, `SELECT context_id::text FROM social_intents WHERE id=$1 AND modality='ONLINE' AND audience='PUBLIC' AND status='ACTIVE'`, itemID).Scan(&cid)
		if errors.Is(e, pgx.ErrNoRows) {
			return out, nq.ErrNotFound
		}
		if e != nil {
			return out, nq.ErrUnavailable
		}
	}
	c, e := onlineContext(ctx, tx, a, cid, &frame)
	if e != nil {
		return out, e
	}
	// Complete all resource waits before the Session lock. The final source
	// SELECT below follows that lock with a new RC statement clock.
	if _, _, _, e = onlineItems(ctx, tx, a, cid, query, itemID, true); e != nil {
		return out, e
	}
	if e = s.onlineSession(ctx, tx, a, &b); e != nil {
		return out, e
	}
	frame.Binding = b
	items, sources, truncated, e := onlineItems(ctx, tx, a, cid, query, itemID, false)
	if e != nil {
		return out, e
	}
	if itemID != "" && len(items) != 1 {
		return out, nq.ErrNotFound
	}
	frame.Sources = sources
	var observed time.Time
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&observed); e != nil {
		return out, nq.ErrUnavailable
	}
	answer := nq.Answer(query, len(items), truncated)
	if in != nil {
		if len(task.Conversation) > 46 {
			return out, nq.ErrConflict
		}
		task.Conversation = append(task.Conversation, agentworkspace.Message{Role: "user", Text: query}, agentworkspace.Message{Role: "assistant", Text: answer})
		filters, _ := json.Marshal(map[string]string{"currentQuery": query})
		conversation, _ := json.Marshal(task.Conversation)
		if taskID == "" {
			task, e = scanAgentTask(tx.QueryRow(ctx, `INSERT INTO agent_tasks(principal_type,owner_account_id,acting_user_account_id,city_id,city_context_id,context_type,context_id,query,intent,status,filters,conversation,created_at,updated_at) VALUES('person',$1,$1,NULL,NULL,'ONLINE',$2,$3,$4,'COMPLETED',$5,$6,clock_timestamp(),clock_timestamp()) RETURNING `+agentTaskColumns, a.Actor.ID, cid, query, nq.Intent, filters, conversation))
		} else {
			task, e = scanAgentTask(tx.QueryRow(ctx, `UPDATE agent_tasks SET filters=$3,conversation=$4,status='COMPLETED',updated_at=clock_timestamp() WHERE id=$1 AND owner_account_id=$2 RETURNING `+agentTaskColumns, taskID, a.Actor.ID, filters, conversation))
		}
		if e != nil {
			return out, nq.ErrUnavailable
		}
		if _, e = routeNativeNotification(ctx, tx, agentnotification.KindAgentTaskCompleted, task.ID, a.Actor.ID); e != nil {
			return out, nq.ErrUnavailable
		}
		if e = tx.QueryRow(ctx, `SELECT xmin::text FROM agent_tasks WHERE id=$1`, task.ID).Scan(&frame.TaskToken); e != nil {
			return out, nq.ErrUnavailable
		}
	}
	out.Response = nq.Response{Schema: nq.Schema, Context: c, Query: query, Answer: answer, Items: items, ObservedAt: observed, ModelAccess: "UNAVAILABLE", Promotion: false, Truncated: truncated}
	if task.ID != "" {
		safe := agentworkspace.SanitizeTaskForResponse(task)
		out.Response.Task = &safe
	}
	if e = checkHumanMomentSession(ctx, tx, a.Digest, a.Actor.ID, s.devPhoneEnabled); e != nil {
		return nq.Receipt{}, e
	}
	// Deadline/block/creator state are read again after notification/FK waits.
	current, nowSources, nowTruncated, e := onlineItems(ctx, tx, a, cid, query, itemID, false)
	if e != nil {
		return nq.Receipt{}, e
	}
	if !reflect.DeepEqual(current, items) || !reflect.DeepEqual(nowSources, sources) || nowTruncated != truncated {
		return nq.Receipt{}, nq.ErrConflict
	}
	if e = checkHumanMomentSession(ctx, tx, a.Digest, a.Actor.ID, s.devPhoneEnabled); e != nil {
		return nq.Receipt{}, e
	}
	raw, e := json.Marshal(frame)
	if e != nil {
		return out, nq.ErrUnavailable
	}
	out.Proof = string(raw)
	out.Seal, e = onlineSeal(a, out)
	if e != nil {
		return out, e
	}
	if e = tx.Commit(ctx); e != nil {
		return nq.Receipt{}, nq.ErrUnavailable
	}
	return out, nil
}
func (s *Store) RevalidateOwnPublicOnline(ctx context.Context, a nq.Access, r nq.Receipt) error {
	seal, e := onlineSeal(a, r)
	if e != nil || r.Seal == "" || !hmac.Equal([]byte(seal), []byte(r.Seal)) {
		return nq.ErrDenied
	}
	var expected onlineFrame
	if json.Unmarshal([]byte(r.Proof), &expected) != nil {
		return nq.ErrDenied
	}
	tx, b, e := s.onlineBegin(ctx, a)
	if e != nil {
		return e
	}
	defer tx.Rollback(context.Background())
	frame := onlineFrame{Binding: b, Sources: map[string]string{}}
	c, e := onlineContext(ctx, tx, a, r.Response.Context.ID, &frame)
	if e != nil {
		return e
	}
	if c != r.Response.Context {
		return nq.ErrConflict
	}
	if r.Response.Task != nil {
		task, token, e := onlineTask(ctx, tx, a, r.Response.Task.ID, false)
		if e != nil {
			return e
		}
		if !reflect.DeepEqual(agentworkspace.SanitizeTaskForResponse(task), *r.Response.Task) {
			return nq.ErrConflict
		}
		frame.TaskToken = token
	}
	id := ""
	if r.Response.Task == nil && len(r.Response.Items) == 1 {
		id = r.Response.Items[0].ID
	}
	if _, _, _, e = onlineItems(ctx, tx, a, c.ID, r.Response.Query, id, true); e != nil {
		return e
	}
	if e = s.onlineSession(ctx, tx, a, &b); e != nil {
		return e
	}
	frame.Binding = b
	items, sources, truncated, e := onlineItems(ctx, tx, a, c.ID, r.Response.Query, id, false)
	if e != nil {
		return e
	}
	frame.Sources = sources
	if !reflect.DeepEqual(items, r.Response.Items) || truncated != r.Response.Truncated || !reflect.DeepEqual(frame, expected) {
		return nq.ErrConflict
	}
	if e = checkHumanMomentSession(ctx, tx, a.Digest, a.Actor.ID, s.devPhoneEnabled); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return nq.ErrUnavailable
	}
	return nil
}
