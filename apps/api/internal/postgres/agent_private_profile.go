package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"math"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/jackc/pgx/v5"
)

type agentPrivateBinding struct {
	sessionID string
	accountID string
	agentID   string
}

// ReadOwnAgentPrivateProfile is an ordinary authenticated owner editing read.
// It is not a model-context/Memory/analysis port. Public Profile visibility,
// profile_view grants, social relationships and organization roles are ignored.
// No missing private row or hidden preference is created by this read.
func (s *Store) ReadOwnAgentPrivateProfile(ctx context.Context, access agentprofile.PrivateAccess) (agentprofile.PrivateRecord, error) {
	if err := agentprofile.ValidatePrivateAccess(access); err != nil {
		return agentprofile.PrivateRecord{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return agentprofile.PrivateRecord{}, agentprofile.ErrUnavailable
	}
	defer tx.Rollback(ctx)
	binding, err := lockOwnAgentPrivateBinding(ctx, tx, access, s.devPhoneEnabled)
	if err != nil {
		return agentprofile.PrivateRecord{}, err
	}
	metadata, err := lockAgentPrivateMetadata(ctx, tx, binding, false)
	if err != nil {
		return agentprofile.PrivateRecord{}, err
	}
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT fields FROM agent_private_profiles
		WHERE agent_id=$1 AND owner_id=$2 AND owner_type='PERSON'`, binding.agentID, binding.accountID).Scan(&raw)
	configured := err == nil
	fields := agentprofile.PrivateFields{}
	if configured {
		fields, err = agentprofile.DecodePrivateFields(raw)
		if err != nil {
			return agentprofile.PrivateRecord{}, agentprofile.ErrUnavailable
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return agentprofile.PrivateRecord{}, agentprofile.ErrUnavailable
	}
	record, err := agentprofile.NewPrivateRecord(metadata, fields, configured)
	if err != nil {
		return agentprofile.PrivateRecord{}, agentprofile.ErrUnavailable
	}
	if err := recheckAgentPrivateSession(ctx, tx, binding); err != nil {
		return agentprofile.PrivateRecord{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return agentprofile.PrivateRecord{}, agentprofile.ErrUnavailable
	}
	return record, nil
}

// ReplaceOwnAgentPrivateProfile atomically advances the current native
// AgentProfile revision and replaces the owner's explicit private fields.
// No user/public profile, grant, inference, analysis permission or model port
// is written. Empty canonical fields are an explicit CAS-protected clear.
func (s *Store) ReplaceOwnAgentPrivateProfile(ctx context.Context, access agentprofile.PrivateAccess, input agentprofile.ReplacePrivateInput) (agentprofile.PrivateRecord, error) {
	if err := agentprofile.ValidatePrivateAccess(access); err != nil {
		return agentprofile.PrivateRecord{}, err
	}
	normalized, err := agentprofile.NormalizeReplacePrivateInput(input)
	if err != nil {
		return agentprofile.PrivateRecord{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return agentprofile.PrivateRecord{}, agentprofile.ErrUnavailable
	}
	defer tx.Rollback(ctx)
	binding, err := lockOwnAgentPrivateBinding(ctx, tx, access, s.devPhoneEnabled)
	if err != nil {
		return agentprofile.PrivateRecord{}, err
	}
	metadata, err := lockAgentPrivateMetadata(ctx, tx, binding, true)
	if err != nil {
		return agentprofile.PrivateRecord{}, err
	}
	record, err := replaceAgentPrivateInTx(ctx, tx, binding, metadata, normalized)
	if err != nil {
		return agentprofile.PrivateRecord{}, err
	}
	if err := recheckAgentPrivateSession(ctx, tx, binding); err != nil {
		return agentprofile.PrivateRecord{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return agentprofile.PrivateRecord{}, agentprofile.ErrUnavailable
	}
	return record, nil
}

// Caller owns exact current binding, metadata lock, normalized input, final
// authorization and commit. The existing public method preserves its gates.
func replaceAgentPrivateInTx(ctx context.Context, tx pgx.Tx, binding agentPrivateBinding, metadata agentprofile.Record, normalized agentprofile.ReplacePrivateInput) (agentprofile.PrivateRecord, error) {
	if metadata.ProfileVersion != normalized.ExpectedVersion || metadata.ProfileVersion == math.MaxInt64 {
		return agentprofile.PrivateRecord{}, agentprofile.ErrConflict
	}
	err := tx.QueryRow(ctx, `UPDATE agent_profiles SET profile_version=profile_version+1
		WHERE agent_id=$1 AND owner_id=$2 AND owner_type='PERSON' AND profile_version=$3
		RETURNING `+agentProfileColumns, binding.agentID, binding.accountID, normalized.ExpectedVersion).Scan(
		&metadata.AgentID, &metadata.OwnerType, &metadata.OwnerID,
		&metadata.ProfileVersion, &metadata.CreatedAt, &metadata.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return agentprofile.PrivateRecord{}, agentprofile.ErrConflict
	}
	if err != nil {
		return agentprofile.PrivateRecord{}, agentprofile.ErrUnavailable
	}
	configured := !agentprofile.PrivateFieldsEmpty(normalized.Fields)
	if configured {
		encoded, encodeErr := json.Marshal(normalized.Fields)
		if encodeErr != nil {
			return agentprofile.PrivateRecord{}, agentprofile.ErrInvalid
		}
		_, err = tx.Exec(ctx, `INSERT INTO agent_private_profiles
			(agent_id,owner_id,owner_type,fields,written_profile_version)
			VALUES($1,$2,'PERSON',$3::jsonb,$4)
			ON CONFLICT(agent_id) DO UPDATE SET fields=EXCLUDED.fields,
				written_profile_version=EXCLUDED.written_profile_version,updated_at=clock_timestamp()`,
			binding.agentID, binding.accountID, encoded, metadata.ProfileVersion)
	} else {
		_, err = tx.Exec(ctx, `DELETE FROM agent_private_profiles
			WHERE agent_id=$1 AND owner_id=$2 AND owner_type='PERSON'`, binding.agentID, binding.accountID)
	}
	if err != nil {
		// PostgreSQL errors may include a failed-row private JSON detail. Never
		// return or log that underlying payload through this owner-facing store.
		return agentprofile.PrivateRecord{}, agentprofile.ErrUnavailable
	}
	if err = insertDomainAudit(ctx, tx, binding.accountID, "replace", "agent_private_profile", binding.agentID, "human_profile_edit", nil); err != nil {
		return agentprofile.PrivateRecord{}, agentprofile.ErrUnavailable
	}
	record, err := agentprofile.NewPrivateRecord(metadata, normalized.Fields, configured)
	if err != nil {
		return agentprofile.PrivateRecord{}, agentprofile.ErrUnavailable
	}
	return record, nil
}

func lockOwnAgentPrivateBinding(ctx context.Context, tx pgx.Tx, access agentprofile.PrivateAccess, devPhoneEnabled bool) (agentPrivateBinding, error) {
	principal, err := actorref.ParsePrincipal(string(access.WorkspacePrincipal.Type), access.WorkspacePrincipal.ID)
	if err != nil || principal.Type != actorref.Person {
		return agentPrivateBinding{}, agentprofile.ErrForbidden
	}
	var binding agentPrivateBinding
	err = tx.QueryRow(ctx, `SELECT s.id,a.id,ag.id FROM sessions s
		JOIN accounts a ON a.id=s.account_id AND a.account_type='person' AND a.status='active'
		JOIN agents ag ON ag.principal_account_id=a.id AND ag.agent_type='personal' AND ag.status='active'
		WHERE s.token_sha256=$1 AND a.id=$2 AND s.revoked_at IS NULL
			AND s.expires_at>clock_timestamp() AND s.idle_expires_at>clock_timestamp()
			AND ($3::boolean OR s.authentication_method<>'dev_phone')
		FOR SHARE OF s,a,ag`, access.SessionDigest[:], principal.ID, devPhoneEnabled).Scan(
		&binding.sessionID, &binding.accountID, &binding.agentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return agentPrivateBinding{}, agentprofile.ErrForbidden
	}
	if err != nil {
		return agentPrivateBinding{}, agentprofile.ErrUnavailable
	}
	return binding, nil
}

func lockAgentPrivateMetadata(ctx context.Context, tx pgx.Tx, binding agentPrivateBinding, forUpdate bool) (agentprofile.Record, error) {
	lock := " FOR SHARE"
	if forUpdate {
		lock = " FOR UPDATE"
	}
	var metadata agentprofile.Record
	err := tx.QueryRow(ctx, `SELECT `+agentProfileColumns+` FROM agent_profiles
		WHERE agent_id=$1 AND owner_id=$2 AND owner_type='PERSON'`+lock, binding.agentID, binding.accountID).Scan(
		&metadata.AgentID, &metadata.OwnerType, &metadata.OwnerID,
		&metadata.ProfileVersion, &metadata.CreatedAt, &metadata.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return agentprofile.Record{}, agentprofile.ErrNotFound
	}
	if err != nil || agentprofile.Validate(metadata) != nil {
		return agentprofile.Record{}, agentprofile.ErrUnavailable
	}
	return metadata, nil
}

func recheckAgentPrivateSession(ctx context.Context, tx pgx.Tx, binding agentPrivateBinding) error {
	// Row locks serialize revocation/identity changes. Time can still advance
	// while waiting for metadata: recheck current wall-clock expiry before the
	// transaction releases a loaded record or commits a replacement.
	var current bool
	err := tx.QueryRow(ctx, `SELECT revoked_at IS NULL AND expires_at>clock_timestamp()
		AND idle_expires_at>clock_timestamp() FROM sessions WHERE id=$1`, binding.sessionID).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !current) {
		return agentprofile.ErrForbidden
	}
	if err != nil {
		return agentprofile.ErrUnavailable
	}
	return nil
}
