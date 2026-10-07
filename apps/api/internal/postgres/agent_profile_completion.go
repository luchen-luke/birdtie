package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	apc "github.com/birdtie/birdtie/apps/api/internal/agentprofilecompletion"
	"github.com/jackc/pgx/v5"
)

var _ apc.HumanStore = (*Store)(nil)

const profileCompletionGuardSQL = `SELECT to_regclass('public.agent_profile_completion_previews') IS NOT NULL
 AND to_regprocedure('public.birdtie_profile_completion_profile_binding(uuid,uuid)') IS NOT NULL
 AND to_regprocedure('public.birdtie_profile_completion_authority(uuid,uuid,uuid)') IS NOT NULL
 AND EXISTS(SELECT 1 FROM pg_trigger t WHERE t.tgrelid=to_regclass('public.agent_profile_completion_previews')
  AND t.tgname='agent_profile_completion_guard' AND t.tgenabled='O' AND NOT t.tgisinternal
  AND t.tgfoid=to_regprocedure('public.birdtie_profile_completion_guard()'))`

func completionError(e error) error {
	if e == nil {
		return nil
	}
	switch {
	case errors.Is(e, pgx.ErrNoRows):
		return agentprofile.ErrNotFound
	case errors.Is(e, agentprofile.ErrInvalid), errors.Is(e, agentprofile.ErrConflict), errors.Is(e, agentprofile.ErrForbidden), errors.Is(e, agentprofile.ErrNotFound), errors.Is(e, apc.ErrExpired):
		return e
	default:
		return agentprofile.ErrUnavailable
	}
}
func (s *Store) beginProfileCompletion(ctx context.Context, a agentprofile.PrivateAccess, write bool) (pgx.Tx, agentPrivateBinding, agentprofile.Record, error) {
	if agentprofile.ValidatePrivateAccess(a) != nil {
		return nil, agentPrivateBinding{}, agentprofile.Record{}, agentprofile.ErrForbidden
	}
	if s == nil || s.pool == nil || ctx == nil || ctx.Err() != nil {
		return nil, agentPrivateBinding{}, agentprofile.Record{}, agentprofile.ErrUnavailable
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return nil, agentPrivateBinding{}, agentprofile.Record{}, agentprofile.ErrUnavailable
	}
	fail := func(e error) (pgx.Tx, agentPrivateBinding, agentprofile.Record, error) {
		tx.Rollback(context.Background())
		return nil, agentPrivateBinding{}, agentprofile.Record{}, completionError(e)
	}
	var installed bool
	if tx.QueryRow(ctx, profileCompletionGuardSQL).Scan(&installed) != nil || !installed {
		return fail(agentprofile.ErrUnavailable)
	}
	mode := "ACCESS SHARE"
	if write {
		mode = "ROW EXCLUSIVE"
	}
	if _, e = tx.Exec(ctx, `LOCK TABLE agent_profile_completion_previews,agent_profiles,agent_private_profiles,audit_events IN `+mode+` MODE`); e != nil {
		return fail(e)
	}
	if _, e = tx.Exec(ctx, `LOCK TABLE accounts,agents,sessions,agent_memories IN ACCESS SHARE MODE`); e != nil {
		return fail(e)
	}
	if tx.QueryRow(ctx, profileCompletionGuardSQL).Scan(&installed) != nil || !installed {
		return fail(agentprofile.ErrUnavailable)
	}
	b, e := lockOwnAgentPrivateBinding(ctx, tx, a, s.devPhoneEnabled)
	if e != nil {
		return fail(e)
	}
	meta, e := lockAgentPrivateMetadata(ctx, tx, b, write)
	if e != nil {
		return fail(e)
	}
	return tx, b, meta, nil
}

type completionCapture struct {
	profile                                                agentprofile.PrivateRecord
	source                                                 apc.Suggestion
	authority, sourceBinding, profileBinding, fieldsDigest string
	at, end                                                time.Time
}

func completionProfile(ctx context.Context, tx pgx.Tx, b agentPrivateBinding, m agentprofile.Record) (agentprofile.PrivateRecord, string, string, error) {
	var raw []byte
	e := tx.QueryRow(ctx, `SELECT fields FROM agent_private_profiles WHERE agent_id=$1 AND owner_id=$2 AND owner_type='PERSON' FOR SHARE`, b.agentID, b.accountID).Scan(&raw)
	configured := e == nil
	f := agentprofile.PrivateFields{}
	if configured {
		f, e = agentprofile.DecodePrivateFields(raw)
		if e != nil {
			return agentprofile.PrivateRecord{}, "", "", agentprofile.ErrUnavailable
		}
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return agentprofile.PrivateRecord{}, "", "", agentprofile.ErrUnavailable
	}
	r, e := agentprofile.NewPrivateRecord(m, f, configured)
	if e != nil {
		return agentprofile.PrivateRecord{}, "", "", agentprofile.ErrUnavailable
	}
	normalized, e := json.Marshal(r.Fields)
	if e != nil {
		return agentprofile.PrivateRecord{}, "", "", agentprofile.ErrUnavailable
	}
	var pb, fd string
	var canonical bool
	e = tx.QueryRow(ctx, `SELECT birdtie_profile_completion_profile_binding($1,$2),
  encode(sha256(convert_to($3::jsonb::text,'UTF8')),'hex'),
		NOT EXISTS(SELECT 1 FROM agent_private_profiles WHERE agent_id=$1 AND owner_id=$2 AND (fields<>$3::jsonb OR
        NOT isfinite(created_at) OR NOT isfinite(updated_at) OR created_at>clock_timestamp() OR updated_at>clock_timestamp()))`, b.agentID, b.accountID, normalized).Scan(&pb, &fd, &canonical)
	if e != nil || !canonical || !apc.ValidDigest(pb) || !apc.ValidDigest(fd) {
		return agentprofile.PrivateRecord{}, "", "", agentprofile.ErrUnavailable
	}
	return r, pb, fd, nil
}
func (s *Store) completionCapture(ctx context.Context, tx pgx.Tx, b agentPrivateBinding, m agentprofile.Record, id string) (completionCapture, error) {
	c := completionCapture{}
	var e error
	c.profile, c.profileBinding, c.fieldsDigest, e = completionProfile(ctx, tx, b, m)
	if e != nil {
		return c, e
	}
	r, e := scanAgentMemory(tx.QueryRow(ctx, `SELECT `+agentMemoryColumns+` FROM agent_memories WHERE id=$1 AND agent_id=$2 AND owner_id=$3 AND owner_type='PERSON' FOR SHARE`, id, b.agentID, b.accountID))
	if e != nil {
		return c, completionError(e)
	}
	// All row waits occurred before this statement takes its fresh PG clock.
	e = tx.QueryRow(ctx, `WITH clk AS MATERIALIZED(SELECT clock_timestamp() at)
 SELECT birdtie_profile_completion_authority($1,$2,$3),
 encode(sha256(convert_to(jsonb_build_object('memory',to_jsonb(m),'row',m.xmin::text)::text,'UTF8')),'hex'),
 clk.at,least(clk.at+interval '5 minutes',se.expires_at,se.idle_expires_at,m.valid_until)
 FROM sessions se JOIN agent_memories m ON m.owner_id=se.account_id CROSS JOIN clk
 WHERE se.id=$3 AND se.account_id=$1 AND m.id=$4 AND m.agent_id=$2 AND m.owner_type='PERSON'
 AND se.revoked_at IS NULL AND isfinite(se.created_at) AND isfinite(se.expires_at) AND isfinite(se.idle_expires_at)
 AND se.created_at<=clk.at AND se.expires_at>clk.at AND se.idle_expires_at>clk.at
 AND ($5::boolean OR se.authentication_method<>'dev_phone')`, b.accountID, b.agentID, b.sessionID, id, s.devPhoneEnabled).Scan(&c.authority, &c.sourceBinding, &c.at, &c.end)
	if e != nil {
		return c, completionError(e)
	}
	c.at = c.at.UTC()
	c.end = c.end.UTC()
	c.source, e = apc.EligibleSource(r, b.accountID, b.agentID, c.at)
	if e != nil {
		return completionCapture{}, e
	}
	if !c.end.After(c.at) {
		return completionCapture{}, apc.ErrExpired
	}
	if !apc.ValidDigest(c.authority) || !apc.ValidDigest(c.sourceBinding) {
		return completionCapture{}, agentprofile.ErrUnavailable
	}
	return c, nil
}

type completionStored struct {
	input                                                                     apc.PreviewInput
	owner, agent, session, category, authority, source, profile, fields, plan string
	at, end                                                                   time.Time
	resultVersion                                                             *int64
	committed                                                                 *time.Time
	resultBinding                                                             *string
}

func completionStoredTx(ctx context.Context, tx pgx.Tx, id string, write bool) (completionStored, error) {
	p := completionStored{}
	lock := " FOR SHARE"
	if write {
		lock = " FOR UPDATE"
	}
	e := tx.QueryRow(ctx, `SELECT id,owner_id,agent_id,session_id,memory_id,memory_version,expected_profile_version,category,authority,source_binding,profile_binding,previous_fields_digest,plan_digest,observed_at,expires_at,result_profile_version,committed_at,result_profile_binding
 FROM agent_profile_completion_previews WHERE id=$1`+lock, id).Scan(&p.input.PreviewID, &p.owner, &p.agent, &p.session, &p.input.MemoryID, &p.input.MemoryVersion, &p.input.ExpectedProfileVersion, &p.category, &p.authority, &p.source, &p.profile, &p.fields, &p.plan, &p.at, &p.end, &p.resultVersion, &p.committed, &p.resultBinding)
	return p, completionError(e)
}
func completionMatches(p completionStored, c completionCapture) bool {
	return p.authority == c.authority && p.source == c.sourceBinding && p.profile == c.profileBinding && p.fields == c.fieldsDigest && p.input.MemoryVersion == c.source.MemoryVersion && p.input.ExpectedProfileVersion == c.profile.Profile.ProfileVersion && p.category == c.source.Category
}
func completionPlan(input apc.PreviewInput, c completionCapture) string {
	return contextPurposeHash(struct {
		Schema, Purpose, Field             string
		Input                              apc.PreviewInput
		Authority, Source, Profile, Fields string
		Category, Value                    string
		ExpiresAt                          time.Time
	}{apc.Schema, apc.Purpose, apc.Field, input, c.authority, c.sourceBinding, c.profileBinding, c.fieldsDigest, c.source.Category, c.source.Value, c.end})
}
func completionPreview(p completionStored, c completionCapture) apc.Preview {
	return apc.Preview{SchemaVersion: apc.Schema, ID: p.input.PreviewID, Purpose: apc.Purpose, Owner: actorref.PrincipalRef{Type: actorref.Person, ID: p.owner}, AgentID: p.agent, Source: c.source, ExpectedProfileVersion: p.input.ExpectedProfileVersion, TargetField: apc.Field, Before: []string{}, After: []string{c.source.Value}, PlanDigest: p.plan, ObservedAt: c.at, ExpiresAt: p.end.UTC(), Explanation: "只补齐这一项本人私密活动偏好，其它八项保持原值。确认后这是你新确认的独立声明；来源记忆到期或撤回不会自动删除这项资料。不会发布、发送给模型或授予分析许可。"}
}

// finishProfileCompletion is the last potentially authoritative statement.
// Table/row/constraint/audit waits precede it; no earlier clock is reused.
func (s *Store) finishProfileCompletion(ctx context.Context, tx pgx.Tx, b agentPrivateBinding, p *completionStored) (time.Time, error) {
	var now time.Time
	var valid bool
	q := `WITH clk AS MATERIALIZED(SELECT clock_timestamp() at) SELECT clk.at,
 EXISTS(SELECT 1 FROM sessions se JOIN accounts ac ON ac.id=se.account_id JOIN agents ag ON ag.principal_account_id=ac.id
 WHERE se.id=$1 AND ac.id=$2 AND ag.id=$3 AND ac.account_type='person' AND ac.status='active' AND ag.agent_type='personal' AND ag.status='active'
 AND se.revoked_at IS NULL AND isfinite(se.created_at) AND isfinite(se.expires_at) AND isfinite(se.idle_expires_at)
 AND se.created_at<=clk.at AND se.expires_at>clk.at AND se.idle_expires_at>clk.at AND ($4::boolean OR se.authentication_method<>'dev_phone')
 AND EXISTS(SELECT 1 FROM agent_profiles ap WHERE ap.agent_id=ag.id AND ap.owner_id=ac.id AND ap.owner_type='PERSON'
 AND isfinite(ap.created_at) AND isfinite(ap.updated_at) AND ap.created_at<=clk.at AND ap.updated_at<=clk.at)) FROM clk`
	if e := tx.QueryRow(ctx, q, b.sessionID, b.accountID, b.agentID, s.devPhoneEnabled).Scan(&now, &valid); e != nil {
		return time.Time{}, agentprofile.ErrUnavailable
	}
	if !valid {
		return time.Time{}, agentprofile.ErrForbidden
	}
	if p != nil {
		e := tx.QueryRow(ctx, `WITH clk AS MATERIALIZED(SELECT clock_timestamp() at) SELECT clk.at,
   $5::timestamptz>clk.at AND EXISTS(SELECT 1 FROM agent_memories m WHERE m.id=$1 AND m.agent_id=$2 AND m.owner_id=$3 AND m.owner_type='PERSON'
   AND m.version=$4 AND m.status='ACTIVE' AND m.source_type='EXPLICIT' AND m.visibility='PRIVATE' AND m.memory_type='PREFERENCE'
   AND isfinite(m.created_at) AND isfinite(m.updated_at) AND isfinite(m.valid_from) AND isfinite(m.valid_until)
   AND m.created_at<=clk.at AND m.valid_from<=clk.at AND m.valid_until>clk.at AND m.updated_at<=clk.at
   AND encode(sha256(convert_to(jsonb_build_object('memory',to_jsonb(m),'row',m.xmin::text)::text,'UTF8')),'hex')=$6)
   AND EXISTS(SELECT 1 FROM sessions se WHERE se.id=$7 AND se.revoked_at IS NULL AND se.expires_at>clk.at AND se.idle_expires_at>clk.at)
   AND birdtie_profile_completion_authority($3,$2,$7)=$8 FROM clk`, p.input.MemoryID, b.agentID, b.accountID, p.input.MemoryVersion, p.end, p.source, b.sessionID, p.authority).Scan(&now, &valid)
		if e != nil {
			return time.Time{}, agentprofile.ErrUnavailable
		}
		if !valid {
			return time.Time{}, apc.ErrExpired
		}
	}
	if ctx.Err() != nil {
		return time.Time{}, agentprofile.ErrUnavailable
	}
	return now.UTC(), nil
}

func (s *Store) ReadOwnProfileCompletionSuggestions(ctx context.Context, a agentprofile.PrivateAccess) (apc.Suggestions, error) {
	tx, b, m, e := s.beginProfileCompletion(ctx, a, false)
	if e != nil {
		return apc.Suggestions{}, e
	}
	defer tx.Rollback(context.Background())
	r, _, _, e := completionProfile(ctx, tx, b, m)
	if e != nil {
		return apc.Suggestions{}, e
	}
	out := apc.Suggestions{SchemaVersion: apc.Schema, Owner: actorref.PrincipalRef{Type: actorref.Person, ID: b.accountID}, AgentID: b.agentID, ProfileVersion: m.ProfileVersion, TargetField: apc.Field, State: "FIELD_EMPTY", Sources: []apc.Suggestion{}}
	captures := []completionCapture{}
	if len(r.Fields.PreferredActivityTypes) > 0 {
		out.State = "FIELD_ALREADY_SET"
	} else {
		rows, e := tx.Query(ctx, `SELECT id FROM agent_memories WHERE agent_id=$1 AND owner_id=$2 AND owner_type='PERSON' AND memory_type='PREFERENCE' AND source_type='EXPLICIT' AND visibility='PRIVATE' AND status='ACTIVE'
   AND memory_key=ANY($3::text[]) ORDER BY memory_key,id LIMIT 7`, b.agentID, b.accountID, []string{"activity_category:badminton", "activity_category:basketball", "activity_category:football", "activity_category:sports", "activity_category:culture", "activity_category:hiking"})
		if e != nil {
			return apc.Suggestions{}, agentprofile.ErrUnavailable
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if rows.Scan(&id) != nil {
				rows.Close()
				return apc.Suggestions{}, agentprofile.ErrUnavailable
			}
			ids = append(ids, id)
		}
		rows.Close()
		if rows.Err() != nil || len(ids) > 6 {
			return apc.Suggestions{}, agentprofile.ErrUnavailable
		}
		for _, id := range ids {
			c, e := s.completionCapture(ctx, tx, b, m, id)
			if errors.Is(e, agentprofile.ErrForbidden) || errors.Is(e, apc.ErrExpired) {
				continue
			}
			if e != nil {
				return apc.Suggestions{}, e
			}
			captures = append(captures, c)
		}
	}
	now, e := s.finishProfileCompletion(ctx, tx, b, nil)
	if e != nil {
		return apc.Suggestions{}, e
	}
	out.ObservedAt = now
	for _, c := range captures {
		if c.source.MemoryValidUntil.After(now) {
			out.Sources = append(out.Sources, c.source)
		}
	}
	if apc.ValidateSuggestions(out) != nil {
		return apc.Suggestions{}, agentprofile.ErrUnavailable
	}
	if tx.Commit(ctx) != nil {
		return apc.Suggestions{}, agentprofile.ErrUnavailable
	}
	return out, nil
}
func (s *Store) PreviewOwnProfileCompletion(ctx context.Context, a agentprofile.PrivateAccess, input apc.PreviewInput) (apc.Preview, error) {
	input, e := apc.NormalizePreviewInput(input)
	if e != nil {
		return apc.Preview{}, e
	}
	tx, b, m, e := s.beginProfileCompletion(ctx, a, true)
	if e != nil {
		return apc.Preview{}, e
	}
	defer tx.Rollback(context.Background())
	c, e := s.completionCapture(ctx, tx, b, m, input.MemoryID)
	if e != nil {
		return apc.Preview{}, e
	}
	if input.MemoryVersion != c.source.MemoryVersion || input.ExpectedProfileVersion != m.ProfileVersion {
		return apc.Preview{}, agentprofile.ErrConflict
	}
	if _, e = apc.Replacement(c.profile, c.source); e != nil {
		return apc.Preview{}, e
	}
	// Serialize the caller-generated exact operation key without renewing it.
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, input.PreviewID); e != nil {
		return apc.Preview{}, agentprofile.ErrUnavailable
	}
	p, e := completionStoredTx(ctx, tx, input.PreviewID, true)
	if errors.Is(e, agentprofile.ErrNotFound) {
		p = completionStored{input: input, owner: b.accountID, agent: b.agentID, session: b.sessionID, category: c.source.Category, authority: c.authority, source: c.sourceBinding, profile: c.profileBinding, fields: c.fieldsDigest, plan: completionPlan(input, c), at: c.at, end: c.end}
		_, e = tx.Exec(ctx, `INSERT INTO agent_profile_completion_previews(id,owner_id,agent_id,session_id,memory_id,memory_version,expected_profile_version,category,authority,source_binding,profile_binding,previous_fields_digest,plan_digest,observed_at,expires_at)
   VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`, input.PreviewID, p.owner, p.agent, p.session, input.MemoryID, input.MemoryVersion, input.ExpectedProfileVersion, p.category, p.authority, p.source, p.profile, p.fields, p.plan, p.at, p.end)
		if e != nil {
			return apc.Preview{}, completionError(e)
		}
	} else if e != nil {
		return apc.Preview{}, e
	} else if p.owner != b.accountID || p.agent != b.agentID || p.session != b.sessionID || !reflect.DeepEqual(p.input, input) || p.resultVersion != nil || !completionMatches(p, c) {
		return apc.Preview{}, agentprofile.ErrConflict
	}
	now, e := s.finishProfileCompletion(ctx, tx, b, &p)
	if e != nil {
		return apc.Preview{}, e
	}
	c.at = now
	out := completionPreview(p, c)
	if apc.ValidatePreview(out) != nil {
		return apc.Preview{}, agentprofile.ErrUnavailable
	}
	if tx.Commit(ctx) != nil {
		return apc.Preview{}, agentprofile.ErrUnavailable
	}
	return out, nil
}
func completionReceipt(p completionStored, b agentPrivateBinding, currentBinding string, now time.Time) apc.Receipt {
	state := "PENDING"
	if !p.end.After(now) {
		state = "EXPIRED"
	}
	if p.resultVersion != nil {
		state = "COMMITTED"
	}
	return apc.Receipt{SchemaVersion: apc.Schema, ID: p.input.PreviewID, Purpose: apc.Purpose, Owner: actorref.PrincipalRef{Type: actorref.Person, ID: b.accountID}, AgentID: b.agentID, MemoryID: p.input.MemoryID, MemoryVersion: p.input.MemoryVersion, ExpectedProfileVersion: p.input.ExpectedProfileVersion, PlanDigest: p.plan, State: state, ResultProfileVersion: p.resultVersion, CommittedAt: p.committed, CurrentProfileMatches: p.resultBinding != nil && *p.resultBinding == currentBinding, ObservedAt: now, ExpiresAt: p.end.UTC()}
}
func (s *Store) ReadOwnProfileCompletion(ctx context.Context, a agentprofile.PrivateAccess, id string) (apc.Receipt, error) {
	if !apc.ValidID(id) {
		return apc.Receipt{}, agentprofile.ErrInvalid
	}
	tx, b, _, e := s.beginProfileCompletion(ctx, a, false)
	if e != nil {
		return apc.Receipt{}, e
	}
	defer tx.Rollback(context.Background())
	p, e := completionStoredTx(ctx, tx, id, false)
	if e != nil {
		return apc.Receipt{}, e
	}
	if p.owner != b.accountID || p.agent != b.agentID {
		return apc.Receipt{}, agentprofile.ErrForbidden
	}
	var pb string
	if tx.QueryRow(ctx, `SELECT birdtie_profile_completion_profile_binding($1,$2)`, b.agentID, b.accountID).Scan(&pb) != nil {
		return apc.Receipt{}, agentprofile.ErrUnavailable
	}
	now, e := s.finishProfileCompletion(ctx, tx, b, nil)
	if e != nil {
		return apc.Receipt{}, e
	}
	out := completionReceipt(p, b, pb, now)
	if apc.ValidateReceipt(out) != nil {
		return apc.Receipt{}, agentprofile.ErrUnavailable
	}
	if tx.Commit(ctx) != nil {
		return apc.Receipt{}, agentprofile.ErrUnavailable
	}
	return out, nil
}
func (s *Store) AcceptOwnProfileCompletion(ctx context.Context, a agentprofile.PrivateAccess, id string, input apc.AcceptInput) (apc.Receipt, error) {
	if !apc.ValidID(id) {
		return apc.Receipt{}, agentprofile.ErrInvalid
	}
	input, e := apc.NormalizeAcceptInput(input)
	if e != nil {
		return apc.Receipt{}, e
	}
	tx, b, m, e := s.beginProfileCompletion(ctx, a, true)
	if e != nil {
		return apc.Receipt{}, e
	}
	defer tx.Rollback(context.Background())
	p, e := completionStoredTx(ctx, tx, id, true)
	if e != nil {
		return apc.Receipt{}, e
	}
	if p.owner != b.accountID || p.agent != b.agentID || p.session != b.sessionID || p.plan != input.PlanDigest {
		return apc.Receipt{}, agentprofile.ErrForbidden
	}
	applied := p.resultVersion == nil
	if p.resultVersion == nil {
		c, e := s.completionCapture(ctx, tx, b, m, p.input.MemoryID)
		if e != nil {
			return apc.Receipt{}, e
		}
		if !completionMatches(p, c) {
			return apc.Receipt{}, agentprofile.ErrConflict
		}
		replacement, e := apc.Replacement(c.profile, c.source)
		if e != nil {
			return apc.Receipt{}, e
		}
		if _, e = s.finishProfileCompletion(ctx, tx, b, &p); e != nil {
			return apc.Receipt{}, e
		}
		saved, e := replaceAgentPrivateInTx(ctx, tx, b, m, replacement)
		if e != nil {
			return apc.Receipt{}, completionError(e)
		}
		if saved.Profile.ProfileVersion != p.input.ExpectedProfileVersion+1 {
			return apc.Receipt{}, agentprofile.ErrUnavailable
		}
		e = tx.QueryRow(ctx, `UPDATE agent_profile_completion_previews SET result_profile_version=$2,committed_at=clock_timestamp(),result_profile_binding=birdtie_profile_completion_profile_binding($3,$4)
   WHERE id=$1 AND result_profile_version IS NULL RETURNING result_profile_version,committed_at,result_profile_binding`, id, saved.Profile.ProfileVersion, b.agentID, b.accountID).Scan(&p.resultVersion, &p.committed, &p.resultBinding)
		if e != nil {
			return apc.Receipt{}, completionError(e)
		}
		// Fire any deferred native constraints before the final clock/ACL statement.
		if _, e = tx.Exec(ctx, `SET CONSTRAINTS ALL IMMEDIATE`); e != nil {
			return apc.Receipt{}, completionError(e)
		}
		if _, e = s.finishProfileCompletion(ctx, tx, b, &p); e != nil {
			return apc.Receipt{}, e
		}
	}
	var current string
	if tx.QueryRow(ctx, `SELECT birdtie_profile_completion_profile_binding($1,$2)`, b.agentID, b.accountID).Scan(&current) != nil {
		return apc.Receipt{}, agentprofile.ErrUnavailable
	}
	var finalSource *completionStored
	if applied {
		finalSource = &p
	}
	now, e := s.finishProfileCompletion(ctx, tx, b, finalSource)
	if e != nil {
		return apc.Receipt{}, e
	}
	out := completionReceipt(p, b, current, now)
	if apc.ValidateReceipt(out) != nil {
		return apc.Receipt{}, agentprofile.ErrUnavailable
	}
	if tx.Commit(ctx) != nil {
		return apc.Receipt{}, agentprofile.ErrUnavailable
	}
	return out, nil
}
