package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"time"

	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/organization"
	"github.com/jackc/pgx/v5"
)

type organizationContextSnapshot struct {
	bound                           *organizationContextBound
	selector                        string
	store                           *Store
	digest                          [32]byte
	actor                           identity.Actor
	org, principal, role, authority string
	deadline                        time.Time
	tasks                           []agentworkspace.Task
	taskVersions                    string
	sources                         map[string]string
}

// One receipt and its derived reads share only a tightening clock checkpoint.
// It is neither a persisted permission nor a new authorization generation.
// Native time is converted to a conservative monotonic bound using an anchor
// taken BEFORE issuing the native clock statement, counting SQL/commit waits.
type organizationContextBound struct {
	mu              sync.Mutex
	deadline, until time.Time
}

func (b *organizationContextBound) tighten(deadline, observed, anchor time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	until := anchor.Add(deadline.Sub(observed))
	if b.deadline.IsZero() || deadline.Before(b.deadline) {
		b.deadline = deadline
	}
	if b.until.IsZero() || until.Before(b.until) {
		b.until = until
	}
	return b.deadline.After(observed) && b.until.After(time.Now())
}
func (b *organizationContextBound) valid() bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.until.After(time.Now())
}

// A stable Session identity excludes rolling idle-expiry/xmin. Its current
// validity and the original shortest deadline are still checked every time.
const organizationContextAuthoritySQL = `WITH context_clock AS MATERIALIZED(SELECT clock_timestamp() n)
SELECT jsonb_build_array(o.id,o.xmin::text,owner.id,owner.xmin::text,
 person.id,person.xmin::text,m.id,m.xmin::text,m.role,ag.id,ag.xmin::text,
 (SELECT jsonb_agg(jsonb_build_array(p.agent_id,p.xmin::text) ORDER BY p.agent_id) FROM agent_profiles p WHERE p.agent_id=ag.id),
 (SELECT jsonb_agg(jsonb_build_array(p.family,p.native_revision,p.xmin::text) ORDER BY p.family) FROM agent_policy_settings p WHERE p.agent_id=ag.id),
 se.id,se.created_at,se.expires_at,se.authentication_method)::text token,
 least(se.expires_at,se.idle_expires_at,n+interval '30 seconds') deadline,n observed
FROM organizations o JOIN accounts owner ON owner.id=o.account_id AND owner.account_type='organization' AND owner.status='active'
JOIN accounts person ON person.id=$2 AND person.account_type='person' AND person.status='active'
JOIN organization_memberships m ON m.organization_id=o.id AND m.user_account_id=person.id AND m.status='active' AND m.role=$4
JOIN agents ag ON ag.principal_account_id=owner.id AND ag.agent_type='organization' AND ag.status='active'
JOIN sessions se ON se.account_id=person.id AND se.token_sha256=$3 CROSS JOIN context_clock
WHERE o.id=$1 AND o.account_id=$5 AND o.status='active' AND se.revoked_at IS NULL
AND se.expires_at>n AND se.idle_expires_at>n AND ($6::boolean OR se.authentication_method<>'dev_phone')`

func organizationAuthoritySQL() string {
	payload := strings.TrimPrefix(organizationContextAuthoritySQL, "WITH context_clock AS MATERIALIZED(SELECT clock_timestamp() n)\n")
	return `WITH context_clock AS MATERIALIZED(SELECT clock_timestamp() n), authority AS MATERIALIZED(` + payload + `)
 SELECT coalesce(authority.token,''),coalesce(authority.deadline,context_clock.n),context_clock.n,
 EXISTS(SELECT 1 FROM sessions se JOIN accounts person ON person.id=se.account_id CROSS JOIN context_clock
 WHERE person.id=$2 AND person.account_type='person' AND person.status='active' AND se.token_sha256=$3 AND se.revoked_at IS NULL
 AND se.expires_at>n AND se.idle_expires_at>n AND ($6::boolean OR se.authentication_method<>'dev_phone')) current_session
 FROM context_clock LEFT JOIN authority ON true`
}

func (s *Store) CaptureOrganizationTaskAuthority(ctx context.Context, digest [32]byte, actor identity.Actor, org, principal, role string) (agentruntime.ContextSnapshot, error) {
	var empty agentruntime.ContextSnapshot
	if ctx == nil || ctx.Err() != nil || s == nil || s.pool == nil || actor.AccountType != "person" || !egressUUID(actor.ID) || !egressUUID(org) || !egressUUID(principal) {
		return empty, organization.ErrForbidden
	}
	r := &organizationContextSnapshot{store: s, digest: digest, actor: actor, org: org, principal: principal, role: role, bound: &organizationContextBound{}, sources: map[string]string{}}
	var now time.Time
	var currentSession bool
	anchor := time.Now()
	err := s.pool.QueryRow(ctx, organizationAuthoritySQL(), org, actor.ID, digest[:], role, principal, s.devPhoneEnabled).Scan(&r.authority, &r.deadline, &now, &currentSession)
	if errors.Is(err, pgx.ErrNoRows) {
		return empty, organization.ErrForbidden
	}
	if err != nil {
		return empty, arp.ErrUnavailable
	}
	if !currentSession {
		return empty, identity.ErrUnauthorized
	}
	if r.authority == "" {
		return empty, organization.ErrForbidden
	}
	if !r.bound.tighten(r.deadline, now, anchor) {
		return empty, organization.ErrForbidden
	}
	return agentruntime.NewContextSnapshot(r), nil
}

func (s *Store) organizationContextReceipt(receipt agentruntime.ContextSnapshot) (*organizationContextSnapshot, error) {
	r, ok := receipt.NativeValue().(*organizationContextSnapshot)
	if !ok || r == nil || r.store != s || r.authority == "" || r.deadline.IsZero() || r.bound == nil {
		return nil, organization.ErrForbidden
	}
	return r, nil
}

// Match the existing Organization-first membership writer lock order. All
// relation/row acquisition occurs before the final current-clock statement.
func (s *Store) organizationContextTx(ctx context.Context, r *organizationContextSnapshot) (pgx.Tx, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, arp.ErrUnavailable
	}
	fail := func(e error) (pgx.Tx, error) { tx.Rollback(context.Background()); return nil, e }
	if _, err = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC';LOCK TABLE organizations,accounts,organization_memberships,sessions,agents,agent_profiles,agent_policy_settings,agent_tasks,cities,places,activities,activity_organizers,activity_invitations,activity_participations,intents,communities,account_blocks,person_ties,venues,venue_candidates,business_venue_relations,user_profiles,agent_profile_field_visibility,consent_grants,community_memberships,businesses,business_memberships,business_claim_controls,business_console_profiles,business_console_venue_facts,business_public_profile_permissions,business_review_grants,sponsored_opportunity_declarations,sponsored_opportunity_review_grants,city_editor_memberships,activity_sources IN ACCESS SHARE MODE`); err != nil {
		return fail(arp.ErrUnavailable)
	}
	var id string
	if err = tx.QueryRow(ctx, `SELECT id FROM organizations WHERE id=$1 FOR SHARE`, r.org).Scan(&id); err != nil {
		return fail(organization.ErrForbidden)
	}
	rows, err := tx.Query(ctx, `SELECT id FROM accounts WHERE id=ANY($1::uuid[]) ORDER BY id FOR SHARE`, []string{r.actor.ID, r.principal})
	if err != nil {
		return fail(arp.ErrUnavailable)
	}
	for rows.Next() {
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return fail(arp.ErrUnavailable)
		}
	}
	rows.Close()
	if rows.Err() != nil {
		return fail(arp.ErrUnavailable)
	}
	for _, stmt := range []string{`SELECT id FROM organization_memberships WHERE organization_id=$1 AND user_account_id=$2 FOR SHARE`, `SELECT id FROM sessions WHERE token_sha256=$3 AND account_id=$2 FOR SHARE`, `SELECT id FROM agents WHERE principal_account_id=$4 AND agent_type='organization' FOR SHARE`} {
		// Each statement has a deliberately fixed parameter prefix.
		var args []any
		switch stmt {
		case `SELECT id FROM organization_memberships WHERE organization_id=$1 AND user_account_id=$2 FOR SHARE`:
			args = []any{r.org, r.actor.ID}
		case `SELECT id FROM sessions WHERE token_sha256=$3 AND account_id=$2 FOR SHARE`:
			stmt = `SELECT id FROM sessions WHERE token_sha256=$1 AND account_id=$2 FOR SHARE`
			args = []any{r.digest[:], r.actor.ID}
		default:
			stmt = `SELECT id FROM agents WHERE principal_account_id=$1 AND agent_type='organization' FOR SHARE`
			args = []any{r.principal}
		}
		if err = tx.QueryRow(ctx, stmt, args...).Scan(&id); err != nil {
			return fail(organization.ErrForbidden)
		}
	}
	return tx, nil
}

func (s *Store) organizationContextFinish(ctx context.Context, tx pgx.Tx, r *organizationContextSnapshot) error {
	var authority, versions string
	var deadline, now time.Time
	var proofs []byte
	var currentSession bool
	sourceNames, err := json.Marshal(r.sources)
	if err != nil {
		return arp.ErrUnavailable
	}
	// All authority and source versions are evaluated under ONE statement
	// snapshot/clock after relation, Session and Task waits. No query follows it.
	authoritySQL := strings.ReplaceAll(organizationAuthoritySQL(), "clock_timestamp()", "(SELECT n FROM finish_clock)")
	sourcesSQL := strings.NewReplacer("$1", "source_city.id", "$2", "$2", "clock_timestamp()", "(SELECT n FROM context_clock)").Replace(organizationPublicSourceSQL)
	versionsSQL := strings.NewReplacer("$1", "$5", "$2", "$8").Replace(organizationTaskVersionsSQL)
	query := `WITH finish_clock AS MATERIALIZED(SELECT clock_timestamp() n), context_clock AS MATERIALIZED(SELECT n FROM finish_clock), authority(token,deadline,observed,current_session) AS MATERIALIZED(` + authoritySQL + `),
 current_sources AS MATERIALIZED(SELECT source_city.id,source.proof,source.deadline FROM jsonb_object_keys($7::jsonb) source_city(id) CROSS JOIN LATERAL(` + sourcesSQL + `) source(proof,deadline,observed))
 SELECT authority.token,least(authority.deadline,(SELECT min(deadline) FROM current_sources)),authority.observed,authority.current_session,(` + versionsSQL + `),coalesce((SELECT jsonb_object_agg(id,proof) FROM current_sources),'{}'::jsonb) FROM authority`
	anchor := time.Now()
	err = tx.QueryRow(ctx, query, r.org, r.actor.ID, r.digest[:], r.role, r.principal, s.devPhoneEnabled, sourceNames, r.selector).Scan(&authority, &deadline, &now, &currentSession, &versions, &proofs)
	if errors.Is(err, pgx.ErrNoRows) {
		return organization.ErrForbidden
	}
	if err != nil {
		return arp.ErrUnavailable
	}
	if !currentSession {
		return identity.ErrUnauthorized
	}
	if authority != r.authority || !r.bound.tighten(deadline, now, anchor) {
		return organization.ErrForbidden
	}
	var current map[string]string
	if json.Unmarshal(proofs, &current) != nil {
		return arp.ErrUnavailable
	}
	if !reflect.DeepEqual(current, r.sources) || (r.taskVersions != "" && versions != r.taskVersions) {
		return arp.ErrChanged
	}
	return nil
}

func (s *Store) ReadOrganizationTaskContext(ctx context.Context, receipt agentruntime.ContextSnapshot, id string) (agentworkspace.Task, agentruntime.ContextSnapshot, error) {
	tasks, next, err := s.readOrganizationTaskContext(ctx, receipt, id)
	if err != nil {
		return agentworkspace.Task{}, agentruntime.ContextSnapshot{}, err
	}
	if len(tasks) != 1 {
		return agentworkspace.Task{}, agentruntime.ContextSnapshot{}, agentworkspace.ErrNotFound
	}
	return tasks[0], next, nil
}

// Bind public query dependencies before the original domain reads. Legitimate
// self Task writes may follow; they cannot replace this captured source proof.
func (s *Store) BindOrganizationTaskQuerySources(ctx context.Context, receipt agentruntime.ContextSnapshot, city string) (agentruntime.ContextSnapshot, error) {
	r, err := s.organizationContextReceipt(receipt)
	if err != nil {
		return agentruntime.ContextSnapshot{}, err
	}
	next := *r
	next.sources = make(map[string]string, len(r.sources)+1)
	for id, proof := range r.sources {
		next.sources[id] = proof
	}
	tx, err := s.organizationContextTx(ctx, r)
	if err != nil {
		return agentruntime.ContextSnapshot{}, err
	}
	defer tx.Rollback(context.Background())
	if err = s.organizationContextFinish(ctx, tx, r); err != nil {
		return agentruntime.ContextSnapshot{}, err
	}
	if _, exists := next.sources[city]; !exists {
		proof, _, e := s.organizationPublicSources(ctx, tx, r, city)
		if e != nil {
			return agentruntime.ContextSnapshot{}, e
		}
		next.sources[city] = proof
	}
	if err = s.organizationContextFinish(ctx, tx, &next); err != nil {
		return agentruntime.ContextSnapshot{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return agentruntime.ContextSnapshot{}, arp.ErrUnavailable
	}
	if !next.bound.valid() {
		return agentruntime.ContextSnapshot{}, organization.ErrForbidden
	}
	return agentruntime.NewContextSnapshot(&next), nil
}

func (s *Store) SaveOrganizationTaskContext(ctx context.Context, receipt agentruntime.ContextSnapshot, task agentworkspace.Task) (agentworkspace.Task, agentruntime.ContextSnapshot, error) {
	return s.writeOrganizationTaskContext(ctx, receipt, task, true)
}
func (s *Store) UpdateOrganizationTaskContext(ctx context.Context, receipt agentruntime.ContextSnapshot, task agentworkspace.Task) (agentworkspace.Task, agentruntime.ContextSnapshot, error) {
	return s.writeOrganizationTaskContext(ctx, receipt, task, false)
}
func (s *Store) writeOrganizationTaskContext(ctx context.Context, receipt agentruntime.ContextSnapshot, task agentworkspace.Task, create bool) (agentworkspace.Task, agentruntime.ContextSnapshot, error) {
	r, err := s.organizationContextReceipt(receipt)
	fail := func(err error) (agentworkspace.Task, agentruntime.ContextSnapshot, error) {
		return agentworkspace.Task{}, agentruntime.ContextSnapshot{}, err
	}
	if err != nil {
		return fail(err)
	}
	if task.PrincipalType != "organization" || task.PrincipalID != r.principal || (create && task.ActingUserID != r.actor.ID) || task.CityID == "" {
		return fail(organization.ErrForbidden)
	}
	if _, ok := r.sources[task.CityID]; !ok {
		return fail(organization.ErrForbidden)
	}
	if !create && (r.selector != task.ID || len(r.tasks) != 1 || r.taskVersions == "") {
		return fail(arp.ErrChanged)
	}
	if !create && task.ActingUserID != r.tasks[0].ActingUserID {
		return fail(arp.ErrChanged)
	}
	tx, err := s.organizationContextTx(ctx, r)
	if err != nil {
		return fail(err)
	}
	defer tx.Rollback(context.Background())
	if !create {
		var id string
		if err = tx.QueryRow(ctx, `SELECT id FROM agent_tasks WHERE id=$1 AND owner_account_id=$2 FOR UPDATE`, task.ID, r.principal).Scan(&id); err != nil {
			return fail(agentworkspace.ErrNotFound)
		}
	}
	if err = s.organizationContextFinish(ctx, tx, r); err != nil {
		return fail(err)
	}
	var stored agentworkspace.Task
	if create {
		stored, err = s.saveTaskInTx(ctx, tx, task)
	} else {
		stored, err = s.updateTaskInTxWithAuditActor(ctx, tx, task, r.actor.ID)
	}
	if err != nil {
		return fail(err)
	}
	next := *r
	next.selector = stored.ID
	next.tasks = []agentworkspace.Task{stored}
	// Only this exact self write advances its Task version. Sources, authority
	// and the shared earliest clock checkpoint retain the original query proof.
	if err = tx.QueryRow(ctx, organizationTaskVersionsSQL, r.principal, stored.ID).Scan(&next.taskVersions); err != nil {
		return fail(arp.ErrUnavailable)
	}
	if err = s.organizationContextFinish(ctx, tx, &next); err != nil {
		return fail(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return fail(arp.ErrUnavailable)
	}
	if !next.bound.valid() {
		return fail(organization.ErrForbidden)
	}
	return stored, agentruntime.NewContextSnapshot(&next), nil
}
func (s *Store) ReadOrganizationTasksContext(ctx context.Context, receipt agentruntime.ContextSnapshot) ([]agentworkspace.Task, agentruntime.ContextSnapshot, error) {
	return s.readOrganizationTaskContext(ctx, receipt, "")
}

func (s *Store) readOrganizationTaskContext(ctx context.Context, receipt agentruntime.ContextSnapshot, id string) ([]agentworkspace.Task, agentruntime.ContextSnapshot, error) {
	r, err := s.organizationContextReceipt(receipt)
	if err != nil {
		return nil, agentruntime.ContextSnapshot{}, err
	}
	next := *r
	next.selector = id
	next.tasks = []agentworkspace.Task{}
	next.sources = make(map[string]string, len(r.sources))
	for city, proof := range r.sources {
		next.sources[city] = proof
	}
	tx, err := s.organizationContextTx(ctx, r)
	if err != nil {
		return nil, agentruntime.ContextSnapshot{}, err
	}
	defer tx.Rollback(context.Background())
	if err = s.organizationContextFinish(ctx, tx, r); err != nil {
		return nil, agentruntime.ContextSnapshot{}, err
	}
	rows, err := tx.Query(ctx, `SELECT `+agentTaskColumns+` FROM agent_tasks WHERE owner_account_id=$1 AND principal_type='organization' AND ($2::text='' OR id::text=$2) ORDER BY updated_at DESC,id DESC LIMIT 50 FOR SHARE`, r.principal, id)
	if err != nil {
		return nil, agentruntime.ContextSnapshot{}, arp.ErrUnavailable
	}
	for rows.Next() {
		task, e := scanAgentTask(rows)
		if e != nil {
			rows.Close()
			return nil, agentruntime.ContextSnapshot{}, e
		}
		next.tasks = append(next.tasks, task)
	}
	rows.Close()
	if rows.Err() != nil {
		return nil, agentruntime.ContextSnapshot{}, arp.ErrUnavailable
	}
	if err = tx.QueryRow(ctx, organizationTaskVersionsSQL, r.principal, id).Scan(&next.taskVersions); err != nil {
		return nil, agentruntime.ContextSnapshot{}, arp.ErrUnavailable
	}
	for _, task := range next.tasks {
		if task.ContextType != "CITY" {
			return nil, agentruntime.ContextSnapshot{}, organization.ErrForbidden
		}
		if _, ok := next.sources[task.CityID]; ok {
			continue
		}
		proof, deadline, e := s.organizationPublicSources(ctx, tx, r, task.CityID)
		if e != nil {
			return nil, agentruntime.ContextSnapshot{}, e
		}
		next.sources[task.CityID] = proof
		if deadline.Before(next.deadline) {
			next.deadline = deadline
		}
	}
	if err = s.organizationContextFinish(ctx, tx, &next); err != nil {
		return nil, agentruntime.ContextSnapshot{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, agentruntime.ContextSnapshot{}, arp.ErrUnavailable
	}
	if !next.bound.valid() {
		return nil, agentruntime.ContextSnapshot{}, organization.ErrForbidden
	}
	return next.tasks, agentruntime.NewContextSnapshot(&next), nil
}

const organizationTaskVersionsSQL = `SELECT coalesce(jsonb_agg(jsonb_build_array(id,xmin::text) ORDER BY id),'[]'::jsonb)::text FROM (SELECT id,xmin FROM agent_tasks WHERE owner_account_id=$1 AND principal_type='organization' AND ($2::text='' OR id::text=$2) ORDER BY updated_at DESC,id DESC LIMIT 50) bound_tasks`

func (s *Store) RevalidateOrganizationTaskContext(ctx context.Context, receipt agentruntime.ContextSnapshot, expected []agentworkspace.Task) error {
	r, err := s.organizationContextReceipt(receipt)
	if err != nil {
		return err
	}
	if len(expected) != len(r.tasks) {
		return arp.ErrChanged
	}
	for i := range expected {
		if !reflect.DeepEqual(agentworkspace.SanitizeTaskForResponse(expected[i]), agentworkspace.SanitizeTaskForResponse(r.tasks[i])) {
			return arp.ErrChanged
		}
	}
	tx, err := s.organizationContextTx(ctx, r)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	// Acquire concrete Task rows before the last current statement.
	rows, err := tx.Query(ctx, `SELECT id FROM agent_tasks WHERE owner_account_id=$1 AND principal_type='organization' AND ($2::text='' OR id::text=$2) ORDER BY id FOR SHARE`, r.principal, r.selector)
	if err != nil {
		return arp.ErrUnavailable
	}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return arp.ErrUnavailable
		}
	}
	rows.Close()
	if rows.Err() != nil {
		return arp.ErrUnavailable
	}
	if err = s.organizationContextFinish(ctx, tx, r); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return arp.ErrUnavailable
	}
	if err = tx.Commit(ctx); err != nil {
		return arp.ErrUnavailable
	}
	if !r.bound.valid() {
		return organization.ErrForbidden
	}
	return nil
}

// Source hashing reads only IDs/xmin and finite source deadlines. No Personal
// Memory, private profile body, grant payload or query enters organization
// context. The conservative city closure rejects changes even to other public
// candidates; it is not a new authority, epoch, cache or persisted source ledger.
func (s *Store) organizationPublicSources(ctx context.Context, tx pgx.Tx, r *organizationContextSnapshot, city string) (string, time.Time, error) {
	var proof string
	var deadline, observed time.Time
	anchor := time.Now()
	err := tx.QueryRow(ctx, organizationPublicSourceSQL, city, r.actor.ID).Scan(&proof, &deadline, &observed)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", time.Time{}, organization.ErrForbidden
	}
	if err != nil {
		return "", time.Time{}, arp.ErrUnavailable
	}
	if !r.bound.tighten(deadline, observed, anchor) {
		return "", time.Time{}, organization.ErrForbidden
	}
	return proof, deadline, nil
}

const organizationPublicSourceSQL = `WITH source_clock AS MATERIALIZED(SELECT clock_timestamp() n),
 city AS MATERIALIZED(SELECT id,xmin::text token,expires_at FROM cities,source_clock WHERE id=$1 AND publication_status='published' AND (expires_at IS NULL OR expires_at>n)),
 supply_communities AS MATERIALIZED(SELECT ao.community_id id FROM activity_organizers ao JOIN activities a ON a.id=ao.activity_id WHERE a.city_id=$1 AND ao.community_id IS NOT NULL),
 supply_businesses AS MATERIALIZED(SELECT business_id id FROM activity_organizers ao JOIN activities a ON a.id=ao.activity_id WHERE a.city_id=$1 UNION SELECT r.business_id FROM business_venue_relations r JOIN places p ON p.id=r.place_id WHERE p.city_id=$1),
 owners AS MATERIALIZED(SELECT $2::uuid id UNION SELECT host_account_id FROM activities WHERE city_id=$1 UNION SELECT owner_account_id FROM intents WHERE city_id=$1 UNION SELECT owner_account_id FROM communities WHERE city_id=$1 OR id IN(SELECT id FROM supply_communities) UNION SELECT ao.person_account_id FROM activity_organizers ao JOIN activities a ON a.id=ao.activity_id WHERE a.city_id=$1 UNION SELECT o.account_id FROM organizations o WHERE o.id IN(SELECT organization_id FROM activities WHERE city_id=$1 UNION SELECT ao.organization_id FROM activity_organizers ao JOIN activities a ON a.id=ao.activity_id WHERE a.city_id=$1 UNION SELECT v.operator_organization_id FROM venues v JOIN places p ON p.id=v.place_id WHERE p.city_id=$1) UNION SELECT b.account_id FROM businesses b WHERE b.id IN(SELECT id FROM supply_businesses) UNION SELECT cl.submitted_by FROM business_claim_controls cl WHERE cl.business_id IN(SELECT id FROM supply_businesses) UNION SELECT cl.reviewed_by FROM business_claim_controls cl WHERE cl.business_id IN(SELECT id FROM supply_businesses) UNION SELECT d.submitted_by FROM sponsored_opportunity_declarations d WHERE d.city_id=$1 UNION SELECT d.reviewed_by FROM sponsored_opportunity_declarations d WHERE d.city_id=$1),
 source_versions AS MATERIALIZED(
 SELECT 'city' kind,id key,token FROM city
 UNION ALL SELECT 'place',id::text,xmin::text FROM places WHERE city_id=$1
 UNION ALL SELECT 'activity',id::text,xmin::text FROM activities WHERE city_id=$1
 UNION ALL SELECT 'organizer',ao.activity_id::text,ao.xmin::text FROM activity_organizers ao JOIN activities a ON a.id=ao.activity_id WHERE a.city_id=$1
 UNION ALL SELECT 'invite',i.id::text,i.xmin::text FROM activity_invitations i JOIN activities a ON a.id=i.activity_id WHERE a.city_id=$1
 UNION ALL SELECT 'participation',p.id::text,p.xmin::text FROM activity_participations p JOIN activities a ON a.id=p.activity_id WHERE a.city_id=$1
 UNION ALL SELECT 'intent',id::text,xmin::text FROM intents WHERE city_id=$1
 UNION ALL SELECT 'community',id::text,xmin::text FROM communities WHERE city_id=$1 OR id IN(SELECT id FROM supply_communities)
 UNION ALL SELECT 'account',a.id::text,a.xmin::text FROM accounts a WHERE a.id IN(SELECT id FROM owners)
 UNION ALL SELECT 'profile',p.account_id::text,p.xmin::text FROM user_profiles p WHERE p.account_id IN(SELECT id FROM owners)
 UNION ALL SELECT 'agent',a.id::text,a.xmin::text FROM agents a WHERE a.principal_account_id IN(SELECT id FROM owners)
 UNION ALL SELECT 'agent-profile',p.agent_id::text,p.xmin::text FROM agent_profiles p WHERE p.owner_id IN(SELECT id FROM owners)
 UNION ALL SELECT 'field-visibility',p.owner_id::text,p.xmin::text FROM agent_profile_field_visibility p WHERE p.owner_id IN(SELECT id FROM owners)
 UNION ALL SELECT 'field-community',c.id::text,c.xmin::text FROM communities c WHERE c.id IN(SELECT m.community_id FROM community_memberships m WHERE m.user_account_id IN(SELECT id FROM owners))
 UNION ALL SELECT 'field-community-member',m.id::text,m.xmin::text FROM community_memberships m WHERE m.community_id IN(SELECT c.community_id FROM community_memberships c WHERE c.user_account_id IN(SELECT id FROM owners))
 UNION ALL SELECT 'profile-grant',g.id::text,g.xmin::text FROM consent_grants g WHERE g.recipient_account_id=$2 AND g.owner_account_id IN(SELECT id FROM owners) AND g.resource_type='profile' AND g.purpose='profile_view'
 UNION ALL SELECT 'activity-source',concat_ws(':',a.activity_id,a.candidate_id),a.xmin::text FROM activity_sources a WHERE a.activity_id IN(SELECT id FROM activities WHERE city_id=$1)
 UNION ALL SELECT 'business',b.id::text,b.xmin::text FROM businesses b WHERE b.id IN(SELECT id FROM supply_businesses)
 UNION ALL SELECT 'business-member',m.id::text,m.xmin::text FROM business_memberships m WHERE m.business_id IN(SELECT id FROM supply_businesses)
 UNION ALL SELECT 'business-claim',c.business_id::text,c.xmin::text FROM business_claim_controls c WHERE c.business_id IN(SELECT id FROM supply_businesses)
 UNION ALL SELECT 'business-profile',p.business_id::text,p.xmin::text FROM business_console_profiles p WHERE p.business_id IN(SELECT id FROM supply_businesses)
 UNION ALL SELECT 'business-facts',concat_ws(':',v.business_id,v.place_id),v.xmin::text FROM business_console_venue_facts v WHERE v.business_id IN(SELECT id FROM supply_businesses)
 UNION ALL SELECT 'business-permission',p.business_id::text,p.xmin::text FROM business_public_profile_permissions p WHERE p.business_id IN(SELECT id FROM supply_businesses)
 UNION ALL SELECT 'review-grant',concat_ws(':',g.business_id,g.reviewer_account_id),g.xmin::text FROM business_review_grants g WHERE g.business_id IN(SELECT id FROM supply_businesses)
 UNION ALL SELECT 'sponsored',d.id::text,d.xmin::text FROM sponsored_opportunity_declarations d WHERE d.business_id IN(SELECT id FROM supply_businesses) OR d.activity_id IN(SELECT id FROM activities WHERE city_id=$1) OR d.place_id IN(SELECT id FROM places WHERE city_id=$1)
 UNION ALL SELECT 'sponsor-current-source',d.id::text,coalesce(birdtie_sponsor_source_snapshot(d.business_id,d.submitted_by,CASE WHEN d.activity_id IS NOT NULL THEN 'ACTIVITY' ELSE 'PLACE' END,coalesce(d.activity_id,d.place_id),source_clock.n),'') FROM sponsored_opportunity_declarations d CROSS JOIN source_clock WHERE d.city_id=$1
 UNION ALL SELECT 'sponsor-current-review',d.id::text,coalesce(birdtie_sponsor_review_snapshot(d.city_id,d.reviewed_by,source_clock.n),'') FROM sponsored_opportunity_declarations d CROSS JOIN source_clock WHERE d.city_id=$1
 UNION ALL SELECT 'sponsor-review',concat_ws(':',g.city_id,g.reviewer_account_id),g.xmin::text FROM sponsored_opportunity_review_grants g WHERE g.city_id=$1
 UNION ALL SELECT 'city-editor',concat_ws(':',m.city_id,m.account_id),m.xmin::text FROM city_editor_memberships m WHERE m.city_id=$1
 UNION ALL SELECT 'block',concat_ws(':',blocker_account_id,blocked_account_id),xmin::text FROM account_blocks WHERE blocker_account_id=$2 OR blocked_account_id=$2
 UNION ALL SELECT 'tie',id::text,xmin::text FROM person_ties WHERE person_a_account_id=$2 OR person_b_account_id=$2
 UNION ALL SELECT 'org',o.id::text,o.xmin::text FROM organizations o WHERE o.id IN(SELECT organization_id FROM activities WHERE city_id=$1 UNION SELECT ao.organization_id FROM activity_organizers ao JOIN activities a ON a.id=ao.activity_id WHERE a.city_id=$1)
 UNION ALL SELECT 'org-member',m.id::text,m.xmin::text FROM organization_memberships m WHERE m.organization_id IN(SELECT organization_id FROM activities WHERE city_id=$1 UNION SELECT ao.organization_id FROM activity_organizers ao JOIN activities a ON a.id=ao.activity_id WHERE a.city_id=$1)
 UNION ALL SELECT 'venue',v.place_id::text,v.xmin::text FROM venues v JOIN places p ON p.id=v.place_id WHERE p.city_id=$1
 UNION ALL SELECT 'venue-candidate',c.id::text,c.xmin::text FROM venue_candidates c WHERE c.place_id IN(SELECT id FROM places WHERE city_id=$1)
 UNION ALL SELECT 'venue-relation',concat_ws(':',r.business_id,r.place_id),r.xmin::text FROM business_venue_relations r JOIN places p ON p.id=r.place_id WHERE p.city_id=$1
 ), deadlines AS MATERIALIZED(
 SELECT expires_at d FROM city UNION ALL SELECT expires_at FROM activity_sources a,source_clock WHERE a.activity_id IN(SELECT id FROM activities WHERE city_id=$1) AND expires_at>n UNION ALL SELECT expires_at FROM places,source_clock WHERE city_id=$1 AND expires_at>n
 UNION ALL SELECT ends_at FROM activities,source_clock WHERE city_id=$1 AND publication_status='published' AND ends_at>n
 UNION ALL SELECT expires_at FROM activities,source_clock WHERE city_id=$1 AND expires_at>n
 UNION ALL SELECT expires_at FROM communities,source_clock WHERE (city_id=$1 OR id IN(SELECT id FROM supply_communities) OR id IN(SELECT m.community_id FROM community_memberships m WHERE m.user_account_id IN(SELECT id FROM owners))) AND expires_at>n
 UNION ALL SELECT expires_at FROM intents,source_clock WHERE city_id=$1 AND expires_at>n
 UNION ALL SELECT available_from FROM intents,source_clock WHERE city_id=$1 AND available_from>n
 UNION ALL SELECT available_until FROM intents,source_clock WHERE city_id=$1 AND available_until>n
 UNION ALL SELECT expires_at FROM consent_grants g,source_clock WHERE g.recipient_account_id=$2 AND g.owner_account_id IN(SELECT id FROM owners) AND g.resource_type='profile' AND g.purpose='profile_view' AND expires_at>n
 UNION ALL SELECT valid_until FROM business_public_profile_permissions p,source_clock WHERE p.business_id IN(SELECT id FROM supply_businesses) AND valid_until>n
 UNION ALL SELECT valid_until FROM business_console_venue_facts v,source_clock WHERE v.business_id IN(SELECT id FROM supply_businesses) AND valid_until>n
 UNION ALL SELECT valid_until FROM business_review_grants g,source_clock WHERE g.business_id IN(SELECT id FROM supply_businesses) AND valid_until>n
 UNION ALL SELECT valid_until FROM sponsored_opportunity_review_grants g,source_clock WHERE g.city_id=$1 AND valid_until>n
 UNION ALL SELECT expires_at FROM sponsored_opportunity_declarations d,source_clock WHERE d.city_id=$1 AND expires_at>n
 UNION ALL SELECT v.expires_at FROM venues v JOIN places p ON p.id=v.place_id,source_clock WHERE p.city_id=$1 AND v.expires_at>n
 ) SELECT encode(sha256(convert_to((SELECT jsonb_agg(jsonb_build_array(kind,key,token) ORDER BY kind,key) FROM source_versions)::text,'UTF8')),'hex'),
 least((SELECT n FROM source_clock)+interval '30 seconds',(SELECT min(d) FROM deadlines)),(SELECT n FROM source_clock) FROM city`

// Assert no native private evidence is serialized as a response by accident.
var _ json.Marshaler = agentruntime.ContextSnapshot{}
