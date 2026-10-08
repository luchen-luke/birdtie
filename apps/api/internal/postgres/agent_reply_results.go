package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/jackc/pgx/v5"
)

var _ agentworkspace.HumanReplyResultsPort = (*Store)(nil)

// This selector is constructed only after reading the same owned Task in the
// original native transaction. Persisted refs are correlation, not permission.
type nativeReplySelector struct {
	index      int
	membership agentworkspace.ReplyMembership
}

func (h *nativeReplySelector) valid(task agentworkspace.Task, q arp.Query) bool {
	return h != nil && agentworkspace.ValidateReplyMembership(task, h.index) == nil &&
		reflect.DeepEqual(h.membership, *task.Conversation[h.index].ResultMembership) &&
		q.CityID == task.CityID && q.Kind == h.membership.Kind && q.SearchTerm == "" && q.Category == "" &&
		q.TimePreference == "" && !q.Closer && q.Bounds == nil && !q.Comparison && len(q.CompareIDs) == 0
}

func ownReplyTaskTx(ctx context.Context, tx pgx.Tx, a arp.Access, write bool) (agentworkspace.Task, error) {
	if !a.Valid() {
		return agentworkspace.Task{}, arp.ErrDenied
	}
	lock := " FOR SHARE"
	if write {
		lock = " FOR UPDATE"
	}
	t, e := scanAgentTask(tx.QueryRow(ctx, `SELECT `+agentTaskColumns+` FROM agent_tasks WHERE id=$1 AND owner_account_id=$2 AND principal_type='person' AND acting_user_account_id=$2`+lock, a.TaskID, a.Actor.ID))
	if e != nil {
		return t, e
	}
	raw, e := json.Marshal(agentworkspace.SanitizeTaskForResponse(t))
	var expected, actual any
	if e != nil || json.Unmarshal(a.ExpectedTask, &expected) != nil || json.Unmarshal(raw, &actual) != nil || !reflect.DeepEqual(expected, actual) {
		return agentworkspace.Task{}, arp.ErrChanged
	}
	if t.ContextType != "CITY" || t.PrincipalID != a.Actor.ID {
		return agentworkspace.Task{}, arp.ErrDenied
	}
	return t, nil
}

func replyCurrentQuery(t agentworkspace.Task) (arp.Query, error) {
	kind := map[string]string{agentworkspace.FindActivity: "activity", agentworkspace.AreaDiscovery: "activity", agentworkspace.RefineResults: "activity", agentworkspace.CompareResults: "activity", agentworkspace.FindPlace: "place", agentworkspace.FindOrganization: "organization"}[t.Intent]
	q := arp.Query{CityID: t.CityID, Kind: kind, SearchTerm: t.Filters["searchTerm"], Category: t.Filters["category"], TimePreference: t.Filters["timePreference"], Closer: t.Filters["distancePreference"] == "closer", Comparison: t.Intent == agentworkspace.CompareResults, CompareIDs: []string{}}
	if q.Comparison {
		for _, id := range strings.Split(t.Filters["resultIDs"], ",") {
			if id != "" && len(q.CompareIDs) < 2 {
				q.CompareIDs = append(q.CompareIDs, id)
			}
		}
	}
	b, e := agentworkspace.BoundsFromFilters(t.Filters)
	if e != nil {
		return q, arp.ErrDenied
	}
	if b != nil {
		q.Bounds = &arp.Bounds{West: b.West, South: b.South, East: b.East, North: b.North}
	}
	if !q.Valid() {
		return q, arp.ErrDenied
	}
	return q, nil
}

// CaptureOwnHumanReply completes only the original ordinary rules branch. The
// caller has already persisted final filters while ACTIVE; exact Task CAS,
// native policy and source read all precede the same original Task writer.
func (s *Store) CaptureOwnHumanReply(ctx context.Context, a arp.Access) (agentworkspace.Task, error) {
	tx, _, p, e := s.beginCurrentToolRead(ctx, a.Actor, a.SessionDigest, resultProjectionRelations)
	if e != nil {
		return agentworkspace.Task{}, e
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(ctx, `SET LOCAL jit=off`); e != nil {
		return agentworkspace.Task{}, arp.ErrUnavailable
	}
	t, e := ownReplyTaskTx(ctx, tx, a, true)
	if e != nil {
		return agentworkspace.Task{}, e
	}
	// An exact completed retry is a read, not another appended message/notice.
	if t.Status == agentworkspace.TaskCompleted && len(t.Conversation) > 0 && agentworkspace.ValidateReplyMembership(t, len(t.Conversation)-1) == nil {
		var existing *nativeMessageResultsRead
		if existing, e = s.readMessageResultsTx(ctx, tx, a, t, p); e != nil {
			return agentworkspace.Task{}, e
		}
		if e = tx.Commit(ctx); e != nil {
			return agentworkspace.Task{}, e
		}
		if !nativeReplyReadUnexpired(existing.state(), existing.state().until, time.Now()) {
			return agentworkspace.Task{}, arp.ErrChanged
		}
		return t, ctx.Err()
	}
	q, source, e := s.captureReplyCurrentSourceTx(ctx, tx, a, t, p)
	if e != nil {
		return agentworkspace.Task{}, e
	}
	t, latest, e := s.persistReplyMembershipTx(ctx, tx, a, t, p, q.Kind, source, agentworkspace.Message{Role: "assistant", Text: agentworkspace.NativeRuleReply(q.Kind, len(source.Items))})
	if e != nil {
		return agentworkspace.Task{}, e
	}
	if e = tx.Commit(ctx); e != nil || ctx.Err() != nil {
		return agentworkspace.Task{}, arp.ErrUnavailable
	}
	if !nativeReplyReadUnexpired(latest.state(), latest.state().until, time.Now()) {
		return agentworkspace.Task{}, arp.ErrChanged
	}
	return t, nil
}

// A completed model reply and an ordinary rules reply select the SAME native
// human-visible entities. This helper remains inside the caller's Task
// transaction; its receipt is never exported to a model or used as a grant.
func replyCaptureQuery(t agentworkspace.Task) (arp.Query, error) {
	if t.Status != agentworkspace.TaskActive || len(t.Conversation) == 0 || t.Conversation[len(t.Conversation)-1].Role != "user" {
		return arp.Query{}, arp.ErrDenied
	}
	query := t.Filters["currentQuery"]
	if query == "" {
		query = t.Query
	}
	if t.Conversation[len(t.Conversation)-1].Text != query {
		return arp.Query{}, arp.ErrDenied
	}
	return replyCurrentQuery(t)
}

// The old literal-query model lifecycle also handles PENDING Tasks without
// native human results. It remains text/citations only; a malformed supported
// query must still reach replyCaptureQuery and fail, rather than losing refs.
func replyMembershipSupported(t agentworkspace.Task) bool {
	switch t.Intent {
	case agentworkspace.FindActivity, agentworkspace.FindPlace, agentworkspace.FindOrganization, agentworkspace.AreaDiscovery, agentworkspace.RefineResults, agentworkspace.CompareResults:
		return true
	}
	return false
}

func (s *Store) captureReplyCurrentSourceTx(ctx context.Context, tx pgx.Tx, a arp.Access, t agentworkspace.Task, p *nativeToolPolicy) (arp.Query, arp.Receipt, error) {
	q, e := replyCaptureQuery(t)
	if e != nil {
		return q, arp.Receipt{}, e
	}
	var source arp.Receipt
	if agenttool.CurrentSearchTool(q.Kind) != "" {
		r, readErr := s.readOwnCurrentSearchTx(ctx, tx, agenttool.CurrentSearch{Access: a, Query: q}, p)
		if readErr != nil {
			return q, source, currentReadError(readErr)
		}
		source = r.Source
	} else {
		// Activity/Organization retain their original explicit human NativeStore
		// read. They do not enlarge the Place/Person CurrentSearch tool schema or
		// acquire its public Activity model-egress purpose.
		source, e = s.captureAgentResultProjectionCurrentTx(ctx, tx, a, q, nil, p)
		if e != nil {
			return q, source, e
		}
	}
	return q, source, nil
}

// Only the caller's just-captured native receipt supplies membership. The
// persisted record contains bounded refs, never coordinates or old actions.
func (s *Store) persistReplyMembershipTx(ctx context.Context, tx pgx.Tx, a arp.Access, t agentworkspace.Task, p *nativeToolPolicy, kind string, source arp.Receipt, message agentworkspace.Message) (agentworkspace.Task, *nativeMessageResultsRead, error) {
	if tx == nil || p == nil || message.Role != "assistant" || message.Text == "" || message.ResultMembership != nil {
		return agentworkspace.Task{}, nil, arp.ErrDenied
	}
	refs := []arp.Ref{}
	for _, item := range source.Items {
		refs = append(refs, item.Entity)
	}
	t.Conversation = append(t.Conversation, message)
	i := len(t.Conversation) - 1
	var e error
	t.Conversation[i].ResultMembership, e = agentworkspace.NewReplyMembership(t, i, kind, refs)
	if e != nil {
		return agentworkspace.Task{}, nil, e
	}
	t.Status = agentworkspace.TaskCompleted
	t, e = s.updateTaskInTx(ctx, tx, t)
	if e != nil {
		return agentworkspace.Task{}, nil, e
	}
	// Reacquire fresh restricted receipts bound to the newly written Task row.
	// The pre-write ExpectedTask/receipt cannot authorize this response.
	raw, e := json.Marshal(agentworkspace.SanitizeTaskForResponse(t))
	if e != nil {
		return agentworkspace.Task{}, nil, arp.ErrUnavailable
	}
	a.ExpectedTask = raw
	latest, e := s.readMessageResultsTx(ctx, tx, a, t, p)
	if e != nil {
		return agentworkspace.Task{}, nil, e
	}
	entries := latest.Results()
	if len(entries) == 0 || entries[len(entries)-1].MessageIndex != i || !reflect.DeepEqual(arp.Refs(entries[len(entries)-1].ResultSet.Items), refs) {
		// A source withdrawal between capture and the original Task write rolls
		// back the completion rather than storing an explanation for another set.
		return agentworkspace.Task{}, nil, arp.ErrChanged
	}
	return t, latest, nil
}

type nativeMessageResultsState struct {
	store                  *Store
	access                 arp.Access
	entries                []agentworkspace.MessageResult
	sources                []arp.Receipt
	taskToken, policyToken string
	observed, until        time.Time
	checkedAt              time.Time
}

// The database clock is captured at statement start. Include the elapsed
// execution time conservatively so a source expiring during the final batch
// cannot be released after that statement finishes.
func nativeReplyReadUnexpired(h *nativeMessageResultsState, until, now time.Time) bool {
	if h == nil || h.checkedAt.IsZero() || h.observed.IsZero() {
		return false
	}
	elapsed := now.Sub(h.checkedAt)
	return elapsed >= 0 && elapsed < until.Sub(h.observed)
}

// Even fmt's invalid-verb reflection fallback sees only a function, never
// authority, Task text, Session bytes or the Store's connection state.
type nativeMessageResultsRead struct {
	read func() *nativeMessageResultsState
}

func (h *nativeMessageResultsRead) state() *nativeMessageResultsState {
	if h == nil || h.read == nil {
		return nil
	}
	return h.read()
}

func (*nativeMessageResultsRead) MarshalJSON() ([]byte, error) { return nil, agenttool.ErrServerOnly }
func (h *nativeMessageResultsRead) UnmarshalJSON([]byte) error {
	if h != nil {
		*h = nativeMessageResultsRead{}
	}
	return agenttool.ErrServerOnly
}
func (*nativeMessageResultsRead) Format(state fmt.State, _ rune) {
	_, _ = state.Write([]byte("native human history read"))
}

func (handle *nativeMessageResultsRead) Results() []agentworkspace.MessageResult {
	h := handle.state()
	if h == nil {
		return nil
	}
	// The response is an immutable copy; callers cannot alter the private proof.
	raw, _ := json.Marshal(h.entries)
	out := []agentworkspace.MessageResult{}
	_ = json.Unmarshal(raw, &out)
	return out
}

func (handle *nativeMessageResultsRead) Revalidate(ctx context.Context) error {
	h := handle.state()
	if h == nil || h.store == nil {
		return arp.ErrDenied
	}
	next, e := h.store.ReadOwnMessageResults(ctx, h.access)
	if e != nil {
		return e
	}
	nextHandle, ok := next.(*nativeMessageResultsRead)
	if !ok {
		return arp.ErrChanged
	}
	n := nextHandle.state()
	if n == nil || h.taskToken != n.taskToken || h.policyToken != n.policyToken || !h.until.After(n.observed) || len(h.sources) != len(n.sources) {
		return arp.ErrChanged
	}
	for i := range h.sources {
		if !unchangedOutputProjection(h.sources[i], n.sources[i]) {
			return arp.ErrChanged
		}
	}
	if ctx.Err() != nil || !nativeReplyReadUnexpired(n, h.until, time.Now()) {
		return arp.ErrChanged
	}
	return nil
}

func (s *Store) ReadOwnMessageResults(ctx context.Context, a arp.Access) (agentworkspace.MessageResultsRead, error) {
	tx, _, p, e := s.beginCurrentToolRead(ctx, a.Actor, a.SessionDigest, resultProjectionRelations)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(ctx, `SET LOCAL jit=off`); e != nil {
		return nil, arp.ErrUnavailable
	}
	t, e := ownReplyTaskTx(ctx, tx, a, false)
	if e != nil {
		return nil, e
	}
	h, e := s.readMessageResultsTx(ctx, tx, a, t, p)
	if e != nil {
		return nil, e
	}
	if e = tx.Commit(ctx); e != nil || ctx.Err() != nil {
		return nil, arp.ErrUnavailable
	}
	if !nativeReplyReadUnexpired(h.state(), h.state().until, time.Now()) {
		return nil, arp.ErrChanged
	}
	return h, nil
}

func (s *Store) readMessageResultsTx(ctx context.Context, tx pgx.Tx, a arp.Access, t agentworkspace.Task, p *nativeToolPolicy) (*nativeMessageResultsRead, error) {
	a.ExpectedTask = append(json.RawMessage(nil), a.ExpectedTask...)
	h := &nativeMessageResultsState{store: s, access: a, entries: []agentworkspace.MessageResult{}, sources: []arp.Receipt{}, policyToken: p.token, until: p.until}
	for _, i := range agentworkspace.LatestReplyMemberships(t) {
		m := *t.Conversation[i].ResultMembership
		if tool := agenttool.CurrentSearchTool(m.Kind); tool != "" && agenttool.Restrict(tool, actorref.Person, p.level).Denied {
			return nil, arp.ErrDenied
		}
		selector := &nativeReplySelector{index: i, membership: m}
		q := arp.Query{CityID: t.CityID, Kind: m.Kind, CompareIDs: []string{}}
		r, e := s.captureAgentResultProjectionSelectedTx(ctx, tx, a, q, nil, p, selector)
		if e != nil {
			return nil, e
		}
		// Keep original membership order, independent of changed source labels.
		items := []arp.Item{}
		activities := []foundation.Activity{}
		places := []foundation.Place{}
		organizations := []agentworkspace.Organization{}
		for _, ref := range m.Refs {
			for _, item := range r.Items {
				if item.Entity == ref {
					items = append(items, item)
				}
			}
			for _, item := range r.Activities {
				if ref.Type == "activity" && item.ID == ref.ID {
					activities = append(activities, item)
				}
			}
			for _, item := range r.Places {
				if ref.Type == "place" && item.ID == ref.ID {
					places = append(places, item)
				}
			}
		}
		r.Items = items
		r.Activities = activities
		r.Places = places
		result := agentworkspace.WithContract(agentworkspace.Results{CityID: t.CityID, NativeProjection: true, ProjectionItems: items, Activities: activities, Places: places, Organizations: organizations, Query: ""}, t, "history")
		result.ResultSet.ID = m.ResultSetID
		result.ResultSet.GeneratedAt = r.ObservedAt
		result.ResultSet.Filters = map[string]string{}
		h.entries = append(h.entries, agentworkspace.MessageResult{MessageIndex: i, TurnDigest: m.TurnDigest, ValidUntil: r.ValidUntil, ResultSet: result.ResultSet, Activities: activities, Places: places, Organizations: organizations, MapEffects: result.MapEffects})
		h.sources = append(h.sources, r)
		h.until = retryMinimum(h.until, r.ValidUntil)
	}
	// One final SQL snapshot rechecks EVERY earlier message source. A later
	// history read/lock wait cannot release a withdrawn earlier card.
	if e := s.finalReplySourceProofsTx(ctx, tx, a, t, p, h); e != nil {
		return nil, e
	}
	if ctx.Err() != nil || !nativeReplyReadUnexpired(h, h.until, time.Now()) {
		return nil, arp.ErrChanged
	}
	return &nativeMessageResultsRead{read: func() *nativeMessageResultsState { return h }}, nil
}

func historicalReplyProjectionSQL(sql string) string {
	return strings.Replace(sql, "s.kind=q->>'Kind' AND", "s.kind=q->>'Kind' AND EXISTS(SELECT 1 FROM jsonb_array_elements(q->'HistoryRefs') original_ref WHERE original_ref->>'type'=s.kind AND original_ref->>'id'=s.id) AND", 1)
}

func finalReplySourceStatement() string {
	// The inner original resolver keeps all seven-source ACL/proof predicates.
	// Only this private three-kind membership selector changes its Query input;
	// every lateral resolver shares the final statement's clock and snapshot.
	inner := historicalReplyProjectionSQL(resultProjectionSQL())
	inner = strings.ReplaceAll(inner, "$5::jsonb", "original_query.value")
	inner = strings.Replace(inner, "SELECT clock_timestamp() n", "SELECT history_clock.n n", 1)
	return `WITH history_clock AS MATERIALIZED(SELECT clock_timestamp() n),
 original_queries AS MATERIALIZED(SELECT value,ordinality FROM jsonb_array_elements($5::jsonb) WITH ORDINALITY),
 final_authority AS MATERIALIZED(SELECT t.xmin::text task_epoch,
 encode(sha256(convert_to(jsonb_build_array(a.id,a.xmin::text,se.id,se.created_at,se.expires_at,se.authentication_method,ag.id,ag.xmin::text,ap.xmin::text,t.xmin::text,c.xmin::text,cx.xmin::text,cc.xmin::text)::text,'UTF8')),'hex') proof,
 least(se.expires_at,se.idle_expires_at,c.expires_at,$7::timestamptz) until,
 se.revoked_at IS NULL AND se.expires_at>n AND se.idle_expires_at>n AND $7::timestamptz>n
 AND ($4::boolean OR se.authentication_method<>'dev_phone')
 AND (SELECT encode(sha256(convert_to(COALESCE(jsonb_agg(to_jsonb(tp)||jsonb_build_object('_xmin',tp.xmin::text) ORDER BY family),'[]'::jsonb)::text,'UTF8')),'hex') FROM agent_policy_settings tp WHERE tp.owner_id=$2 AND tp.agent_id=$9 AND tp.owner_type='PERSON')=$8 current
 FROM accounts a JOIN sessions se ON se.account_id=a.id AND se.token_sha256=$3
 JOIN agents ag ON ag.id=$9 AND ag.principal_account_id=a.id AND ag.agent_type='personal' AND ag.status='active'
 JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_type='PERSON' AND ap.owner_id=a.id
 JOIN agent_tasks t ON t.id=$6 AND t.owner_account_id=a.id AND t.principal_type='person' AND t.acting_user_account_id=a.id AND t.context_type='CITY'
 JOIN cities c ON c.id=$1 AND c.id=t.city_id AND c.publication_status='published'
 JOIN contexts cx ON cx.id=t.context_id AND cx.context_type='CITY' AND cx.city_id=c.id
 JOIN city_contexts cc ON cc.city_id=c.id CROSS JOIN history_clock
 WHERE a.id=$2 AND a.account_type='person' AND a.status='active' AND (c.expires_at IS NULL OR c.expires_at>n)),
 final_sources AS MATERIALIZED(SELECT original_query.ordinality,p.proof,p.observed_at,p.valid_until,p.authority,p.current_session
 FROM history_clock CROSS JOIN original_queries original_query CROSS JOIN LATERAL (` + inner + `) p(observed_at,valid_until,items,commercial_refs,own_raw,proof,authority,activity_rows,place_rows,current_session,action_raw))
 SELECT final_authority.proof,history_clock.n,final_authority.until,final_authority.current,
 coalesce((SELECT jsonb_agg(jsonb_build_object('proof',proof,'validUntil',valid_until,'authority',authority,'session',current_session) ORDER BY ordinality) FROM final_sources),'[]'::jsonb)
 FROM final_authority CROSS JOIN history_clock`
}

func (s *Store) finalReplySourceProofsTx(ctx context.Context, tx pgx.Tx, a arp.Access, t agentworkspace.Task, p *nativeToolPolicy, h *nativeMessageResultsState) error {
	queries := []any{}
	for _, entry := range h.entries {
		m := t.Conversation[entry.MessageIndex].ResultMembership
		queries = append(queries, struct {
			arp.Query
			HistoryRefs []arp.Ref
		}{arp.Query{CityID: t.CityID, Kind: m.Kind, CompareIDs: []string{}}, m.Refs})
	}
	raw, e := json.Marshal(queries)
	if e != nil {
		return arp.ErrUnavailable
	}
	var current bool
	var batch []byte
	h.checkedAt = time.Now()
	e = tx.QueryRow(ctx, finalReplySourceStatement(), t.CityID, a.Actor.ID, a.SessionDigest[:], s.devPhoneEnabled, raw, a.TaskID, h.until, p.token, p.agent).Scan(&h.taskToken, &h.observed, &h.until, &current, &batch)
	if e != nil || !current {
		return arp.ErrChanged
	}
	var proofs []struct {
		Proof      string    `json:"proof"`
		ValidUntil time.Time `json:"validUntil"`
		Authority  bool      `json:"authority"`
		Session    bool      `json:"session"`
	}
	if json.Unmarshal(batch, &proofs) != nil || len(proofs) != len(h.sources) {
		return arp.ErrChanged
	}
	for i, proof := range proofs {
		if !proof.Authority || !proof.Session || proof.Proof != h.sources[i].Proof || !proof.ValidUntil.After(h.observed) || !h.sources[i].ValidUntil.After(h.observed) {
			return arp.ErrChanged
		}
		h.until = retryMinimum(h.until, proof.ValidUntil)
	}
	return nil
}
