package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentautonomy"
	"github.com/birdtie/birdtie/apps/api/internal/agentplanner"
	"github.com/birdtie/birdtie/apps/api/internal/agentpolicysettings"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
	"time"
)

// Same original 065 settings, with xmin in addition to its domain revision.
// No new preference store, consent resolver, authority generation or ledger.
type nativeToolPolicy struct {
	owner, agent, token string
	level               agentautonomy.Level
	observed, until     time.Time
}

const nativeToolPolicyTokenSQL = `SELECT encode(sha256(convert_to(COALESCE(jsonb_agg(to_jsonb(p)||jsonb_build_object('_xmin',p.xmin::text) ORDER BY family),'[]'::jsonb)::text,'UTF8')),'hex') FROM agent_policy_settings p WHERE p.owner_id=$1 AND p.agent_id=$2 AND p.owner_type='PERSON'`

func captureNativeToolPolicy(ctx context.Context, tx pgx.Tx, b agentPrivateBinding, until time.Time) (*nativeToolPolicy, error) {
	// The short read-only Tx also freezes absence of an unconfigured row. The
	// final statement still checks the fingerprint/time after all native waits.
	if _, e := tx.Exec(ctx, `LOCK TABLE agent_policy_settings IN SHARE MODE`); e != nil {
		return nil, agenttool.ErrUnavailable
	}
	bundle, e := policySettingsBundle(ctx, tx, b)
	if e != nil {
		return nil, agenttool.ErrUnavailable
	}
	p := &nativeToolPolicy{owner: b.accountID, agent: b.agentID, observed: bundle.ObservedAt, until: until}
	var settings agentpolicysettings.AutonomySettings
	if json.Unmarshal(bundle.Autonomy.Settings, &settings) != nil {
		return nil, agenttool.ErrUnavailable
	}
	if bundle.Autonomy.Configured {
		if bundle.Autonomy.Status != "ACTIVE" {
			return nil, agenttool.ErrDenied
		}
		p.until = retryMinimum(p.until, *bundle.Autonomy.ExpiresAt)
	}
	p.level = settings.Level
	if !p.until.After(p.observed) {
		return nil, agenttool.ErrChanged
	}
	if tx.QueryRow(ctx, nativeToolPolicyTokenSQL, b.accountID, b.agentID).Scan(&p.token) != nil {
		return nil, agenttool.ErrUnavailable
	}
	return p, nil
}

// This predicate is inserted in the original final payload statement. Policy
// fields come only from the same native Tx, never the model or a JSON Decision.
func nativeToolProjectionStatement(sql string, args []any, p *nativeToolPolicy) (string, []any) {
	n := len(args)
	tokenSQL := fmt.Sprintf(`SELECT encode(sha256(convert_to(COALESCE(jsonb_agg(to_jsonb(tp)||jsonb_build_object('_xmin',tp.xmin::text) ORDER BY family),'[]'::jsonb)::text,'UTF8')),'hex') FROM agent_policy_settings tp WHERE tp.owner_id=$%d::uuid AND tp.agent_id=$%d::uuid AND tp.owner_type='PERSON'`, n+3, n+4)
	return sql + fmt.Sprintf(` AND p.observed_at<$%d::timestamptz AND (%s)=$%d::text`, n+1, tokenSQL, n+2), append(args, p.until, p.token, p.owner, p.agent)
}
func nativeToolDecision(tool, action, operation, resource, digest, owner, agent string, p *nativeToolPolicy, disposition, reason string) agenttool.Decision {
	d, _ := agenttool.Lookup(tool)
	return agenttool.Decision{SchemaVersion: agenttool.Schema, DecisionID: decisionToolID(operation, tool, action, p.token, digest), Disposition: disposition, ReasonCodes: []string{reason}, Tool: tool, ToolVersion: d.Version, ActionID: action, LogicalOperationID: operation, ActorID: owner, AgentID: agent, SubjectType: string(actorref.Person), SubjectID: owner, PolicyVersion: p.token, ResourceVersion: resource, ArgumentsDigest: digest, Purpose: d.Purpose, DataDestinations: []string{"LOCAL_OWNER"}, ObservedAt: p.observed, ExpiresAt: p.until}
}
func decisionToolID(operation, tool, action, policy, digest string) string {
	h := agenttool.Digest([]string{operation, tool, action, policy, digest})
	return h[:8] + "-" + h[8:12] + "-8" + h[13:16] + "-a" + h[17:20] + "-" + h[20:32]
}

// Human reads acquire ACCESS SHARE relation waits before Account ->
// Session/Agent/metadata. The original policy writer's advisory lock must precede
// the stronger settings-table freeze, avoiding settings row/table inversion.
// Observe is a brake; this does not use the unavailable machine Social service
// or invent an independent-purpose consent from human matching/profile access.
func (s *Store) beginCurrentToolRead(ctx context.Context, a identity.Actor, digest [32]byte, relations string) (pgx.Tx, agentPrivateBinding, *nativeToolPolicy, error) {
	if ctx == nil || ctx.Err() != nil || s == nil || s.pool == nil || a.AccountType != "person" || !agentplanner.ValidID(a.ID) || digest == ([32]byte{}) {
		return nil, agentPrivateBinding{}, nil, agenttool.ErrDenied
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return nil, agentPrivateBinding{}, nil, agenttool.ErrUnavailable
	}
	fail := func(e error) (pgx.Tx, agentPrivateBinding, *nativeToolPolicy, error) {
		tx.Rollback(context.Background())
		return nil, agentPrivateBinding{}, nil, e
	}
	if _, e = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC';`+relations); e != nil {
		return fail(agenttool.ErrUnavailable)
	}
	var owner string
	if e = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 AND account_type='person' AND status='active' FOR SHARE`, a.ID).Scan(&owner); e != nil {
		return fail(agenttool.ErrDenied)
	}
	access := agentprofile.PrivateAccess{WorkspacePrincipal: actorref.PrincipalRef{Type: actorref.Person, ID: a.ID}, SessionDigest: digest}
	b, e := lockOwnAgentPrivateBinding(ctx, tx, access, s.devPhoneEnabled)
	if e != nil {
		return fail(agenttool.ErrDenied)
	}
	if _, e = lockAgentPrivateMetadata(ctx, tx, b, false); e != nil {
		return fail(agenttool.ErrDenied)
	}
	var until time.Time
	if e = tx.QueryRow(ctx, `WITH n AS MATERIALIZED(SELECT clock_timestamp() at) SELECT least(s.expires_at,s.idle_expires_at,n.at+interval '30 seconds') FROM sessions s CROSS JOIN n WHERE s.id=$1 AND s.revoked_at IS NULL AND s.expires_at>n.at AND s.idle_expires_at>n.at`, b.sessionID).Scan(&until); e != nil {
		return fail(agenttool.ErrDenied)
	}
	p, e := captureCurrentHumanToolPolicy(ctx, tx, b, until)
	if e != nil {
		return fail(e)
	}
	return tx, b, p, nil
}

func captureCurrentHumanToolPolicy(ctx context.Context, tx pgx.Tx, b agentPrivateBinding, until time.Time) (*nativeToolPolicy, error) {
	// Exactly the existing PutOwnPolicy serializer. No separate authority or
	// consent lock is created, and the original ModelRun paths are unchanged.
	if _, e := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('human-agent-policy:'||$1::text,0))`, b.agentID); e != nil {
		return nil, agenttool.ErrUnavailable
	}
	return captureNativeToolPolicy(ctx, tx, b, until)
}

func currentToolProjectionStatement(sql string, args []any, p *nativeToolPolicy) (string, []any) {
	// p alias is a payload projected by the ORIGINAL same current source SQL.
	wrapped := `SELECT p.* FROM (` + sql + `) p(observed_at,valid_until,items,commercial_refs,own_raw,proof,authority,activity_rows,place_rows,current_session,action_raw) WHERE true`
	wrapped, args = nativeToolProjectionStatement(wrapped, args, p)
	return wrapped + fmt.Sprintf(` AND EXISTS(SELECT 1 FROM agents current_agent WHERE current_agent.id=$%d::uuid AND current_agent.principal_account_id=$%d::uuid AND current_agent.agent_type='personal' AND current_agent.status='active')`, len(args), len(args)-1), args
}
