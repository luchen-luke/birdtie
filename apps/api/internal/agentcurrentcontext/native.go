package agentcurrentcontext

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type nativeResolver struct {
	pool        *pgxpool.Pool
	store       *postgres.Store
	development bool
}

// These SQL fragments only read existing native rows. They introduce no schema,
// source backfill, synthetic owner, Memory record, or second fact database.
// The final load statement is the authorization/source linearization point.
const nativeAuthoritySQL = `WITH clock AS MATERIALIZED (SELECT clock_timestamp() AS checked_at),
authority AS (
 SELECT s.id AS session_id,LEAST(s.expires_at,s.idle_expires_at) AS session_limit,
 a.id AS owner_id,ag.id AS agent_id,ag.created_at AS agent_created_at,
 ap.profile_version,ap.created_at AS metadata_created_at,ap.xmin::text AS metadata_row_token,
 ag.xmin::text AS agent_row_token,a.xmin::text AS account_row_token,clock.checked_at
 FROM clock JOIN sessions s ON s.token_sha256=$1
 JOIN accounts a ON a.id=s.account_id AND a.id=$2 AND a.account_type='person' AND a.status='active'
 JOIN agents ag ON ag.principal_account_id=a.id AND ag.id=$3 AND ag.agent_type='personal' AND ag.status='active'
 JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=a.id AND ap.owner_type='PERSON' AND ap.profile_version>0
 WHERE s.revoked_at IS NULL AND s.expires_at>clock.checked_at AND s.idle_expires_at>clock.checked_at
 AND ($4::boolean OR s.authentication_method<>'dev_phone')
 AND isfinite(ap.created_at) AND isfinite(ap.updated_at) AND isfinite(ag.created_at)
 AND ap.created_at<=clock.checked_at AND ap.updated_at<=clock.checked_at AND ag.created_at<=clock.checked_at) `
const nativeColumns = `SELECT auth.checked_at,auth.session_limit,
 jsonb_build_object('session',auth.session_id,'agent',auth.agent_id,'agentCreated',auth.agent_created_at,
 'metadataCreated',auth.metadata_created_at,'metadataVersion',auth.profile_version,
 'metadataRowToken',auth.metadata_row_token,'agentRowToken',auth.agent_row_token,'accountRowToken',auth.account_row_token)::text,
 jsonb_build_object('id',city.id,'label',city.name,'timeZone',city.time_zone,'contextId',c.id,
 'contextCreatedAt',c.created_at,'contextStateUpdatedAt',cc.updated_at,'sourceUpdatedAt',city.updated_at,
 'sourceExpiresAt',city.expires_at,'sourceVerifiedAt',city.verified_at,
 'cityRowToken',city.xmin::text,'contextRowToken',c.xmin::text,'contextStateRowToken',cc.xmin::text)::text, `
const nativeCityGuard = ` JOIN contexts c ON c.context_type='CITY' AND c.city_id=city.id
 JOIN city_contexts cc ON cc.city_id=city.id AND cc.status='active'
 WHERE city.publication_status='published' AND (city.expires_at IS NULL OR city.expires_at>auth.checked_at)
 AND isfinite(city.updated_at) AND isfinite(c.created_at) AND isfinite(cc.updated_at) `

func nativeSQL(selection Selection) (string, error) {
	switch selection {
	case CurrentDeclaration:
		return nativeAuthoritySQL + nativeColumns + `jsonb_build_object('contextId',pc.context_id,'owner',pc.person_account_id,'relation',pc.relation,'visibility',pc.visibility,'createdAt',pc.created_at,'rowToken',pc.xmin::text)::text,NULL::text
 FROM authority auth JOIN person_contexts pc ON pc.person_account_id=auth.owner_id AND pc.relation='current' AND pc.visibility='private'
 JOIN contexts chosen ON chosen.id=pc.context_id AND chosen.context_type='CITY'
 JOIN cities city ON city.id=chosen.city_id ` + nativeCityGuard + `AND isfinite(pc.created_at)
 AND (SELECT count(*) FROM person_contexts duplicate JOIN contexts dc ON dc.id=duplicate.context_id
 WHERE duplicate.person_account_id=auth.owner_id AND duplicate.relation='current' AND dc.context_type='CITY')=1`, nil
	case CurrentTask:
		return nativeAuthoritySQL + nativeColumns + `NULL::text,(to_jsonb(t)||jsonb_build_object('_rowToken',t.xmin::text))::text
 FROM authority auth JOIN agent_tasks t ON t.id=$5::uuid AND t.owner_account_id=auth.owner_id AND t.principal_type='person'
 AND (t.acting_user_account_id IS NULL OR t.acting_user_account_id=auth.owner_id) AND t.status='ACTIVE'
 JOIN cities city ON city.id=t.city_context_id ` + nativeCityGuard + `AND t.context_type='CITY' AND t.context_id=c.id AND isfinite(t.created_at) AND isfinite(t.updated_at)`, nil
	case SelectedCity:
		return nativeAuthoritySQL + nativeColumns + `NULL::text,NULL::text
 FROM authority auth JOIN cities city ON city.id=$5 ` + nativeCityGuard, nil
	default:
		return "", ErrInvalid
	}
}

type nativeCity struct {
	ID                    string     `json:"id"`
	Label                 string     `json:"label"`
	TimeZone              string     `json:"timeZone"`
	ContextID             string     `json:"contextId"`
	ContextCreatedAt      time.Time  `json:"contextCreatedAt"`
	ContextStateUpdatedAt time.Time  `json:"contextStateUpdatedAt"`
	SourceUpdatedAt       time.Time  `json:"sourceUpdatedAt"`
	SourceExpiresAt       *time.Time `json:"sourceExpiresAt"`
	SourceVerifiedAt      *time.Time `json:"sourceVerifiedAt"`
}
type nativeDeclaration struct {
	ContextID  string    `json:"contextId"`
	Owner      string    `json:"owner"`
	Relation   string    `json:"relation"`
	Visibility string    `json:"visibility"`
	CreatedAt  time.Time `json:"createdAt"`
}

// Native task names follow actual PostgreSQL columns, not an alternate DTO.
type nativeTask struct {
	ID            string            `json:"id"`
	Owner         string            `json:"owner_account_id"`
	PrincipalType string            `json:"principal_type"`
	ActingUser    *string           `json:"acting_user_account_id"`
	CityID        string            `json:"city_context_id"`
	ContextType   string            `json:"context_type"`
	ContextID     string            `json:"context_id"`
	Query         string            `json:"query"`
	Intent        string            `json:"intent"`
	Status        string            `json:"status"`
	Filters       map[string]string `json:"filters"`
	Conversation  []struct {
		Role string `json:"role"`
		Text string `json:"text"`
	} `json:"conversation"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
type nativeState struct {
	now          time.Time
	sessionLimit time.Time
	authority    string
	city         nativeCity
	sources      []Source
	task         *nativeTask
}

func (n *nativeResolver) authenticate(ctx context.Context, r Request) error {
	if n == nil || n.pool == nil || n.store == nil {
		return ErrUnavailable
	}
	actor, e := n.store.Authenticate(ctx, r.Access.SessionDigest)
	if errors.Is(e, identity.ErrUnauthorized) {
		return ErrDenied
	}
	if e != nil {
		return ErrUnavailable
	}
	if actor.AccountType != "person" || actor.ID != r.Access.WorkspacePrincipal.ID {
		return ErrDenied
	}
	return nil
}
func (n *nativeResolver) load(ctx context.Context, r Request) (nativeState, error) {
	var state nativeState
	if n == nil || n.pool == nil {
		return state, ErrUnavailable
	}
	sql, e := nativeSQL(r.Selection)
	if e != nil {
		return state, e
	}
	var authorityRaw, cityRaw string
	var declarationRaw, taskRaw *string
	args := []any{r.Access.SessionDigest[:], r.Access.WorkspacePrincipal.ID, r.Agent.AgentID, n.development}
	if r.Selection == CurrentTask {
		args = append(args, r.TaskID)
	} else if r.Selection == SelectedCity {
		args = append(args, r.CityID)
	}
	tx, e := n.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nativeState{}, ErrUnavailable
	}
	defer tx.Rollback(ctx)
	// Canonical timestamps use transaction-local UTC across pool connections.
	if _, e = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); e != nil {
		return nativeState{}, ErrUnavailable
	}
	e = tx.QueryRow(ctx, sql, args...).Scan(&state.now, &state.sessionLimit, &authorityRaw, &cityRaw, &declarationRaw, &taskRaw)
	if errors.Is(e, pgx.ErrNoRows) {
		return nativeState{}, ErrDenied
	}
	if e != nil {
		return nativeState{}, ErrUnavailable
	}
	if !validTime(state.now) || !validTime(state.sessionLimit) || !state.sessionLimit.After(state.now) || len(cityRaw) > 64*1024 || json.Unmarshal([]byte(cityRaw), &state.city) != nil {
		return nativeState{}, ErrUnavailable
	}
	if e = tx.Commit(ctx); e != nil {
		return nativeState{}, ErrUnavailable
	}
	state.authority = authorityRaw
	if state.city.SourceVerifiedAt != nil && (!validTime(*state.city.SourceVerifiedAt) || state.city.SourceVerifiedAt.After(state.now)) {
		return nativeState{}, ErrDenied
	}
	if state.city.SourceExpiresAt != nil && (!validTime(*state.city.SourceExpiresAt) || !state.city.SourceExpiresAt.After(state.now)) {
		return nativeState{}, ErrDenied
	}
	if !validCityID(state.city.ID) || !validID(state.city.ContextID) || state.city.Label == "" || !validTime(state.city.SourceUpdatedAt) || !validTime(state.city.ContextCreatedAt) || !validTime(state.city.ContextStateUpdatedAt) || state.city.SourceUpdatedAt.After(state.now) || state.city.ContextCreatedAt.After(state.now) || state.city.ContextStateUpdatedAt.After(state.now) {
		return nativeState{}, ErrDenied
	}
	version, e := contextVersion("CITY_CATALOG", agentevent.UpdatedAtDigestVersion, state.city.SourceUpdatedAt, []byte(cityRaw))
	if e != nil {
		return nativeState{}, e
	}
	state.sources = []Source{{"CITY_CATALOG", state.city.ID, version, state.city.SourceUpdatedAt.UTC()}}
	if r.Selection == CurrentDeclaration {
		var d nativeDeclaration
		if declarationRaw == nil || len(*declarationRaw) > 64*1024 || json.Unmarshal([]byte(*declarationRaw), &d) != nil || d.Owner != r.Access.WorkspacePrincipal.ID || d.ContextID != state.city.ContextID || d.Relation != "current" || d.Visibility != "private" || !validTime(d.CreatedAt) || d.CreatedAt.After(state.now) {
			return nativeState{}, ErrDenied
		}
		version, e = contextVersion("CITY_DECLARATION", agentevent.CreatedAtDigestVersion, d.CreatedAt, []byte(*declarationRaw))
		if e != nil {
			return nativeState{}, e
		}
		state.sources = append(state.sources, Source{"CITY_DECLARATION", d.ContextID, version, d.CreatedAt.UTC()})
	} else if r.Selection == CurrentTask {
		var task nativeTask
		if taskRaw == nil || len(*taskRaw) > 64*1024 || json.Unmarshal([]byte(*taskRaw), &task) != nil {
			return nativeState{}, ErrUnavailable
		}
		if task.ID != r.TaskID || task.Owner != r.Agent.Principal.ID || task.PrincipalType != "person" || task.CityID != state.city.ID || task.ContextType != "CITY" || task.ContextID != state.city.ContextID || task.Status != "ACTIVE" || !validTime(task.CreatedAt) || !validTime(task.UpdatedAt) || task.UpdatedAt.Before(task.CreatedAt) || task.UpdatedAt.After(state.now) || (task.ActingUser != nil && *task.ActingUser != task.Owner) {
			return nativeState{}, ErrDenied
		}
		version, e = agentevent.QueryVersion(task.UpdatedAt, []byte(*taskRaw))
		if e != nil {
			return nativeState{}, ErrDenied
		}
		state.sources = append(state.sources, Source{"AGENT_TASK", task.ID, version, task.UpdatedAt.UTC()})
		state.task = &task
	}
	return state, nil
}
