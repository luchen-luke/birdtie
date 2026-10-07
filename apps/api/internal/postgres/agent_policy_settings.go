package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentpolicysettings"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var _ agentpolicysettings.Store = (*Store)(nil)

func policySettingsError(err error) error {
	switch {
	case errors.Is(err, agentprofile.ErrForbidden):
		return agentpolicysettings.ErrForbidden
	case errors.Is(err, agentprofile.ErrNotFound):
		return agentpolicysettings.ErrNotFound
	case errors.Is(err, agentprofile.ErrInvalid):
		return agentpolicysettings.ErrInvalid
	}
	var pgerr *pgconn.PgError
	if errors.As(err, &pgerr) && (pgerr.Code == "23505" || pgerr.Code == "40001" || pgerr.Code == "40P01") {
		return agentpolicysettings.ErrConflict
	}
	return agentpolicysettings.ErrUnavailable
}
func (s *Store) beginPolicySettings(ctx context.Context, access agentprofile.PrivateAccess) (pgx.Tx, agentPrivateBinding, error) {
	if ctx == nil || s == nil || s.pool == nil || ctx.Err() != nil {
		return nil, agentPrivateBinding{}, agentpolicysettings.ErrUnavailable
	}
	if agentprofile.ValidatePrivateAccess(access) != nil {
		return nil, agentPrivateBinding{}, agentpolicysettings.ErrForbidden
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, agentPrivateBinding{}, agentpolicysettings.ErrUnavailable
	}
	failed := func(err error) (pgx.Tx, agentPrivateBinding, error) {
		tx.Rollback(context.Background())
		return nil, agentPrivateBinding{}, policySettingsError(err)
	}
	if _, err = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); err != nil {
		return failed(err)
	}
	// Lock the derived Account before Session/Agent. A real revocation may
	// commit while this wait is blocked; the subsequent Session query uses a
	// fresh READ COMMITTED snapshot. No cached Authenticate result is authority.
	var active string
	if err = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 AND account_type='person' AND status='active' FOR SHARE`, access.WorkspacePrincipal.ID).Scan(&active); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return failed(agentprofile.ErrForbidden)
		}
		return failed(err)
	}
	binding, err := lockOwnAgentPrivateBinding(ctx, tx, access, s.devPhoneEnabled)
	if err != nil {
		return failed(err)
	}
	if _, err = lockAgentPrivateMetadata(ctx, tx, binding, false); err != nil {
		return failed(err)
	}
	return tx, binding, nil
}
func policySettingsBundle(ctx context.Context, tx pgx.Tx, binding agentPrivateBinding) (agentpolicysettings.Bundle, error) {
	b := agentpolicysettings.Bundle{SchemaVersion: agentpolicysettings.SchemaVersion, OwnerType: actorref.Person, OwnerID: binding.accountID, AgentID: binding.agentID, Attention: agentpolicysettings.DefaultRecord(agentpolicysettings.Attention), Social: agentpolicysettings.DefaultRecord(agentpolicysettings.Social), Autonomy: agentpolicysettings.DefaultRecord(agentpolicysettings.Autonomy)}
	rows, err := tx.Query(ctx, `SELECT family,native_revision,settings,valid_from,expires_at,updated_at FROM agent_policy_settings
	 WHERE agent_id=$1 AND owner_id=$2 AND owner_type='PERSON' ORDER BY family FOR SHARE`, binding.agentID, binding.accountID)
	if err != nil {
		return agentpolicysettings.Bundle{}, agentpolicysettings.ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		r := agentpolicysettings.Record{Configured: true}
		var from, expires, updated time.Time
		var raw []byte
		if rows.Scan(&r.Family, &r.NativeRevision, &raw, &from, &expires, &updated) != nil {
			return agentpolicysettings.Bundle{}, agentpolicysettings.ErrUnavailable
		}
		from, expires, updated = from.UTC(), expires.UTC(), updated.UTC()
		normalized, err := agentpolicysettings.NormalizeSettings(r.Family, json.RawMessage(raw), from, expires)
		if err != nil {
			return agentpolicysettings.Bundle{}, agentpolicysettings.ErrUnavailable
		}
		r.Settings = normalized
		r.ValidFrom = &from
		r.ExpiresAt = &expires
		r.UpdatedAt = &updated
		switch r.Family {
		case agentpolicysettings.Attention:
			b.Attention = r
		case agentpolicysettings.Social:
			b.Social = r
		case agentpolicysettings.Autonomy:
			b.Autonomy = r
		default:
			return agentpolicysettings.Bundle{}, agentpolicysettings.ErrUnavailable
		}
	}
	if rows.Err() != nil {
		return agentpolicysettings.Bundle{}, agentpolicysettings.ErrUnavailable
	}
	rows.Close()
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&b.ObservedAt); err != nil {
		return agentpolicysettings.Bundle{}, agentpolicysettings.ErrUnavailable
	}
	b.ObservedAt = b.ObservedAt.UTC()
	for _, r := range []*agentpolicysettings.Record{&b.Attention, &b.Social, &b.Autonomy} {
		if r.Configured {
			r.Status = "ACTIVE"
			if !b.ObservedAt.Before(*r.ExpiresAt) {
				r.Status = "EXPIRED"
			}
		}
	}
	if agentpolicysettings.ValidateBundle(b) != nil {
		return agentpolicysettings.Bundle{}, agentpolicysettings.ErrUnavailable
	}
	return b, nil
}
func (s *Store) finalPolicySettings(ctx context.Context, tx pgx.Tx, binding agentPrivateBinding, family string, revision int64) (time.Time, error) {
	if ctx.Err() != nil {
		return time.Time{}, agentpolicysettings.ErrUnavailable
	}
	var current, future bool
	var observed time.Time
	err := tx.QueryRow(ctx, `WITH current_clock AS MATERIALIZED(SELECT clock_timestamp() AS at)
	 SELECT current_clock.at,EXISTS(SELECT 1 FROM sessions s JOIN accounts a ON a.id=s.account_id
	 JOIN agents ag ON ag.principal_account_id=a.id JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=a.id AND ap.owner_type='PERSON'
	 WHERE s.id=$1 AND a.id=$2 AND ag.id=$3 AND a.account_type='person' AND a.status='active'
	 AND ag.agent_type='personal' AND ag.status='active' AND s.revoked_at IS NULL
	 AND s.expires_at>current_clock.at AND s.idle_expires_at>current_clock.at
	 AND ($4::boolean OR s.authentication_method<>'dev_phone')),
	 ($5::text='' OR EXISTS(SELECT 1 FROM agent_policy_settings p WHERE p.agent_id=$3 AND p.owner_id=$2
	 AND p.family=$5 AND p.native_revision=$6 AND p.expires_at>current_clock.at)) FROM current_clock`, binding.sessionID, binding.accountID, binding.agentID, s.devPhoneEnabled, family, revision).Scan(&observed, &current, &future)
	if err != nil || ctx.Err() != nil {
		return time.Time{}, agentpolicysettings.ErrUnavailable
	}
	if !current {
		return time.Time{}, agentpolicysettings.ErrForbidden
	}
	if !future {
		return time.Time{}, agentpolicysettings.ErrInvalid
	}
	return observed.UTC(), nil
}

func observePolicyBundle(b *agentpolicysettings.Bundle, now time.Time) error {
	b.ObservedAt = now
	for _, r := range []*agentpolicysettings.Record{&b.Attention, &b.Social, &b.Autonomy} {
		if r.Configured {
			r.Status = "ACTIVE"
			if !now.Before(*r.ExpiresAt) {
				r.Status = "EXPIRED"
			}
		}
	}
	if agentpolicysettings.ValidateBundle(*b) != nil {
		return agentpolicysettings.ErrUnavailable
	}
	return nil
}
func (s *Store) GetOwnPolicies(ctx context.Context, access agentprofile.PrivateAccess) (agentpolicysettings.Bundle, error) {
	tx, binding, err := s.beginPolicySettings(ctx, access)
	if err != nil {
		return agentpolicysettings.Bundle{}, err
	}
	defer tx.Rollback(context.Background())
	b, err := policySettingsBundle(ctx, tx, binding)
	if err != nil {
		return agentpolicysettings.Bundle{}, err
	}
	observed, err := s.finalPolicySettings(ctx, tx, binding, "", 0)
	if err != nil {
		return agentpolicysettings.Bundle{}, err
	}
	if err = observePolicyBundle(&b, observed); err != nil {
		return agentpolicysettings.Bundle{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return agentpolicysettings.Bundle{}, agentpolicysettings.ErrUnavailable
	}
	return b, nil
}
func (s *Store) PutOwnPolicy(ctx context.Context, access agentprofile.PrivateAccess, family agentpolicysettings.Family, input agentpolicysettings.PutInput) (agentpolicysettings.Bundle, error) {
	if !agentpolicysettings.ValidFamily(family) {
		return agentpolicysettings.Bundle{}, agentpolicysettings.ErrInvalid
	}
	tx, binding, err := s.beginPolicySettings(ctx, access)
	if err != nil {
		return agentpolicysettings.Bundle{}, err
	}
	defer tx.Rollback(context.Background())
	// All family writes for this Agent serialize before any settings row lock.
	// Their revisions remain independent; this prevents two different-family
	// writers deadlocking when each returns the complete current bundle.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('human-agent-policy:'||$1::text,0))`, binding.agentID); err != nil {
		return agentpolicysettings.Bundle{}, agentpolicysettings.ErrUnavailable
	}
	var version int64
	err = tx.QueryRow(ctx, `SELECT native_revision FROM agent_policy_settings WHERE agent_id=$1 AND owner_id=$2 AND family=$3 FOR UPDATE`, binding.agentID, binding.accountID, string(family)).Scan(&version)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return agentpolicysettings.Bundle{}, agentpolicysettings.ErrUnavailable
	}
	if version != input.ExpectedVersion || version == agentpolicysettings.MaxRevision {
		return agentpolicysettings.Bundle{}, agentpolicysettings.ErrConflict
	}
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return agentpolicysettings.Bundle{}, agentpolicysettings.ErrUnavailable
	}
	normalized, err := agentpolicysettings.NormalizeInput(family, input, now.UTC())
	if err != nil {
		return agentpolicysettings.Bundle{}, err
	}
	tag, err := tx.Exec(ctx, `INSERT INTO agent_policy_settings(agent_id,owner_id,owner_type,family,schema_version,native_revision,settings,valid_from,expires_at,updated_at)
	 VALUES($1,$2,'PERSON',$3,$4,1,$5::jsonb,$6,$7,$6) ON CONFLICT(agent_id,family) DO UPDATE SET native_revision=agent_policy_settings.native_revision+1,
	 settings=EXCLUDED.settings,valid_from=EXCLUDED.valid_from,expires_at=EXCLUDED.expires_at,updated_at=EXCLUDED.updated_at
	 WHERE agent_policy_settings.owner_id=EXCLUDED.owner_id AND agent_policy_settings.native_revision=$8`, binding.agentID, binding.accountID, string(family), agentpolicysettings.SchemaVersion, normalized.Settings, now.UTC(), normalized.ExpiresAt, version)
	if err != nil {
		return agentpolicysettings.Bundle{}, policySettingsError(err)
	}
	if tag.RowsAffected() != 1 {
		return agentpolicysettings.Bundle{}, agentpolicysettings.ErrConflict
	}
	b, err := policySettingsBundle(ctx, tx, binding)
	if err != nil {
		return agentpolicysettings.Bundle{}, err
	}
	var saved agentpolicysettings.Record
	switch family {
	case agentpolicysettings.Attention:
		saved = b.Attention
	case agentpolicysettings.Social:
		saved = b.Social
	case agentpolicysettings.Autonomy:
		saved = b.Autonomy
	}
	if saved.NativeRevision != version+1 || saved.Status != "ACTIVE" {
		return agentpolicysettings.Bundle{}, agentpolicysettings.ErrInvalid
	}
	if err = insertDomainAudit(ctx, tx, binding.accountID, "replace", "agent_policy", binding.agentID, "human_policy_edit", nil); err != nil {
		return agentpolicysettings.Bundle{}, agentpolicysettings.ErrUnavailable
	}
	observed, err := s.finalPolicySettings(ctx, tx, binding, string(family), saved.NativeRevision)
	if err != nil {
		return agentpolicysettings.Bundle{}, err
	}
	if err = observePolicyBundle(&b, observed); err != nil {
		return agentpolicysettings.Bundle{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return agentpolicysettings.Bundle{}, agentpolicysettings.ErrUnavailable
	}
	return b, nil
}
