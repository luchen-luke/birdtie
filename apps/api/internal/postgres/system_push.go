package postgres

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	sp "github.com/birdtie/birdtie/apps/api/internal/systempush"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type SystemPush struct {
	store  *Store
	ring   *sp.Keyring
	config sp.Config
}

var _ sp.HumanGateway = (*SystemPush)(nil)
var _ sp.RunnerStore = (*SystemPush)(nil)

func NewSystemPush(store *Store, ring *sp.Keyring, config sp.Config) (*SystemPush, error) {
	if store == nil || store.pool == nil || config.Validate() != nil || config.Enabled && (ring == nil || ring.ActiveKeyID() == "") {
		return nil, sp.ErrConfig
	}
	return &SystemPush{store, ring, config}, nil
}
func (p *SystemPush) Enabled() bool { return p != nil && p.config.Enabled }
func pushError(e error) error {
	if e == nil {
		return nil
	}
	for _, v := range []error{sp.ErrInvalid, sp.ErrConflict, sp.ErrNotFound, sp.ErrDisabled, identity.ErrUnauthorized} {
		if errors.Is(e, v) {
			return v
		}
	}
	var sql *pgconn.PgError
	if errors.As(e, &sql) && (sql.Code == "23505" || sql.Code == "23514") {
		return sp.ErrConflict
	}
	return sp.ErrUnavailable
}

type pushInstallationRow struct {
	version                      int64
	proof                        []byte
	owner, session, binding      *string
	generation                   *int64
	platform                     *string
	kind                         *sp.Kind
	app, environment, permission *string
	state                        string
	keyID                        *string
	nonce, cipher, fingerprint   []byte
	registered, until            *time.Time
}

const pushInstallationColumns = `version,proof_sha256,owner_id::text,session_id::text,session_generation,binding_id::text,platform,registration_kind,application_id,environment,permission,state,address_key_id,address_nonce,address_ciphertext,address_fingerprint,registered_at,valid_until`

func scanPushInstallation(row scanner) (pushInstallationRow, error) {
	var r pushInstallationRow
	e := row.Scan(&r.version, &r.proof, &r.owner, &r.session, &r.generation, &r.binding, &r.platform, &r.kind, &r.app, &r.environment, &r.permission, &r.state, &r.keyID, &r.nonce, &r.cipher, &r.fingerprint, &r.registered, &r.until)
	return r, e
}
func readPushInstallation(ctx context.Context, tx pgx.Tx, a sp.Access, lock string) (pushInstallationRow, error) {
	r, e := scanPushInstallation(tx.QueryRow(ctx, `SELECT `+pushInstallationColumns+` FROM system_push_installations WHERE installation_id=$1 `+lock, a.InstallationID))
	if errors.Is(e, pgx.ErrNoRows) {
		return pushInstallationRow{}, nil
	}
	if e != nil {
		return r, e
	}
	if subtle.ConstantTimeCompare(r.proof, a.ProofDigest[:]) != 1 {
		return pushInstallationRow{}, sp.ErrNotFound
	}
	return r, nil
}

type pushSessionProof struct {
	gateway    *SystemPush
	access     sp.Access
	generation int64
}

func (p *SystemPush) captureSession(ctx context.Context, a sp.Access) (int64, error) {
	if p == nil || ctx == nil || ctx.Err() != nil || !sp.ValidAccess(a) {
		return 0, identity.ErrUnauthorized
	}
	var generation int64
	e := p.store.pool.QueryRow(ctx, `SELECT ss.authorization_generation FROM sessions ss JOIN accounts a ON a.id=ss.account_id WHERE ss.token_sha256=$1 AND ss.account_id=$2 AND a.account_type='person' AND a.status='active' AND ss.revoked_at IS NULL AND ss.expires_at>clock_timestamp() AND ss.idle_expires_at>clock_timestamp() AND ($3::boolean OR ss.authentication_method<>'dev_phone')`, a.SessionDigest[:], a.Actor.ID, p.store.devPhoneEnabled).Scan(&generation)
	if errors.Is(e, pgx.ErrNoRows) {
		return 0, identity.ErrUnauthorized
	}
	if e != nil {
		return 0, sp.ErrUnavailable
	}
	return generation, nil
}
func (p *SystemPush) sameSession(ctx context.Context, tx pgx.Tx, a sp.Access, generation int64) error {
	var current int64
	e := tx.QueryRow(ctx, `SELECT authorization_generation FROM sessions WHERE token_sha256=$1 AND account_id=$2`, a.SessionDigest[:], a.Actor.ID).Scan(&current)
	if e != nil {
		return pushError(e)
	}
	if current != generation {
		return identity.ErrUnauthorized
	}
	return nil
}
func (p *SystemPush) lockSession(ctx context.Context, tx pgx.Tx, a sp.Access, generation int64) error {
	if e := p.store.lockHumanMomentSession(ctx, tx, a.SessionDigest, a.Actor.ID); e != nil {
		return pushError(e)
	}
	return p.sameSession(ctx, tx, a, generation)
}
func (p *SystemPush) finishSession(ctx context.Context, tx pgx.Tx, a sp.Access, generation int64) error {
	if e := p.sameSession(ctx, tx, a, generation); e != nil {
		return e
	}
	return pushError(p.store.finishHumanMomentTx(ctx, tx, a.SessionDigest, a.Actor.ID))
}
func (p *SystemPush) proofSession(ctx context.Context, a sp.Access, proof any) error {
	v, ok := proof.(*pushSessionProof)
	if !ok || v == nil || v.gateway != p || v.access != a {
		return sp.ErrUnavailable
	}
	generation, e := p.captureSession(ctx, a)
	if e != nil {
		return e
	}
	if generation != v.generation {
		return identity.ErrUnauthorized
	}
	return nil
}
func (p *SystemPush) begin(ctx context.Context, a sp.Access) (pgx.Tx, int64, error) {
	if p == nil || !sp.ValidAccess(a) {
		return nil, 0, identity.ErrUnauthorized
	}
	generation, e := p.captureSession(ctx, a)
	if e != nil {
		return nil, 0, e
	}
	tx, e := p.store.beginHumanMomentTx(ctx, a.SessionDigest, a.Actor)
	return tx, generation, pushError(e)
}
func (p *SystemPush) ReadOptions(ctx context.Context, a identity.ProfileAccess) (sp.Options, error) {
	v := sp.Options{Schema: "system-push-options-v1", RegistrationKinds: []sp.Kind{}}
	if p == nil {
		return v, sp.ErrUnavailable
	}
	if e := p.store.ValidateHumanSocialResponse(ctx, a.SessionDigest, a.Actor); e != nil {
		return v, pushError(e)
	}
	v.RegistrationKinds = p.config.Kinds()
	v.Available = len(v.RegistrationKinds) > 0
	return v, nil
}
func (p *SystemPush) ValidateOptions(ctx context.Context, a identity.ProfileAccess, v sp.Options) error {
	if !sp.ValidOptions(v) {
		return sp.ErrUnavailable
	}
	actual, e := p.ReadOptions(ctx, a)
	if e != nil {
		return e
	}
	x, _ := json.Marshal(actual)
	y, _ := json.Marshal(v)
	if string(x) != string(y) {
		return sp.ErrConflict
	}
	return nil
}

// Binding/current Session are checked together after all resource/Session waits.
const pushBindingCurrent = `p.state='ACTIVE' AND p.owner_id=ss.account_id AND a.id=p.owner_id AND a.account_type='person' AND a.status='active'
 AND p.session_id=ss.id AND p.session_generation=ss.authorization_generation AND p.valid_until>clock_timestamp()
 AND ss.revoked_at IS NULL AND ss.expires_at>clock_timestamp() AND ss.idle_expires_at>clock_timestamp()`

func (p *SystemPush) ReadInstallation(ctx context.Context, a sp.Access) (sp.Installation, error) {
	v := sp.Installation{Schema: "system-push-installation-v1", InstallationID: a.InstallationID}
	tx, generation, e := p.begin(ctx, a)
	if e != nil {
		return v, e
	}
	defer tx.Rollback(context.Background())
	row, e := readPushInstallation(ctx, tx, a, "FOR SHARE")
	if e != nil {
		return v, pushError(e)
	}
	v.Version = row.version
	if e = p.lockSession(ctx, tx, a, generation); e != nil {
		return v, pushError(e)
	}
	if p.Enabled() && row.owner != nil && *row.owner == a.Actor.ID && row.kind != nil && row.app != nil && row.environment != nil {
		app, env, allowed := p.config.Namespace(*row.kind)
		if allowed && app == *row.app && env == *row.environment {
			var current bool
			e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM system_push_installations p JOIN sessions ss ON ss.id=p.session_id JOIN accounts a ON a.id=p.owner_id WHERE p.installation_id=$1 AND `+pushBindingCurrent+` AND ($2::boolean OR ss.authentication_method<>'dev_phone'))`, a.InstallationID, p.store.devPhoneEnabled).Scan(&current)
			if e != nil {
				return v, sp.ErrUnavailable
			}
			if current {
				v.Enabled = true
				v.BindingID = row.binding
				v.RegistrationKind = row.kind
				v.RegisteredAt = row.registered
				v.ValidUntil = row.until
			}
		}
	}
	if e = p.finishSession(ctx, tx, a, generation); e != nil {
		return sp.Installation{}, pushError(e)
	}
	v.Proof = &pushSessionProof{p, a, generation}
	return v, nil
}
func (p *SystemPush) ValidateInstallation(ctx context.Context, a sp.Access, v sp.Installation) error {
	if e := p.proofSession(ctx, a, v.Proof); e != nil {
		return e
	}
	if !sp.ValidInstallation(v) || v.InstallationID != a.InstallationID {
		return sp.ErrUnavailable
	}
	current, e := p.ReadInstallation(ctx, a)
	if e != nil {
		return e
	}
	x, _ := json.Marshal(v)
	y, _ := json.Marshal(current)
	if string(x) != string(y) {
		return sp.ErrConflict
	}
	return p.proofSession(ctx, a, v.Proof)
}

type pushAddressProof struct {
	gateway     *SystemPush
	access      sp.Access
	kind        sp.Kind
	fingerprint [32]byte
	expected    int64
	encoded     string
	generation  int64
}

func (p *SystemPush) CheckAddress(ctx context.Context, a sp.Access, in sp.AddressCheckInput) (sp.AddressMatch, error) {
	if e := sp.ValidateAddressCheck(in); e != nil {
		return sp.AddressMatch{}, e
	}
	if !p.Enabled() {
		return sp.AddressMatch{}, sp.ErrNotFound
	}
	app, env, allowed := p.config.Namespace(in.RegistrationKind)
	if !allowed {
		return sp.AddressMatch{}, sp.ErrNotFound
	}
	fingerprint, e := p.ring.Fingerprint(in.RegistrationKind, app, env, in.Address)
	if e != nil {
		return sp.AddressMatch{}, sp.ErrUnavailable
	}
	tx, generation, e := p.begin(ctx, a)
	if e != nil {
		return sp.AddressMatch{}, e
	}
	defer tx.Rollback(context.Background())
	_, e = readPushInstallation(ctx, tx, a, "FOR SHARE")
	if e != nil {
		return sp.AddressMatch{}, pushError(e)
	}
	if e = p.lockSession(ctx, tx, a, generation); e != nil {
		return sp.AddressMatch{}, pushError(e)
	}
	var binding string
	var matches bool
	e = tx.QueryRow(ctx, `SELECT p.binding_id::text,p.address_fingerprint=$9 FROM system_push_installations p JOIN sessions ss ON ss.id=p.session_id JOIN accounts a ON a.id=p.owner_id JOIN sessions viewer ON viewer.token_sha256=$5 AND viewer.account_id=$2 WHERE p.installation_id=$1 AND p.owner_id=$2 AND p.proof_sha256=$3 AND p.version=$4 AND p.registration_kind=$6 AND p.application_id=$7 AND p.environment=$8 AND `+pushBindingCurrent+` AND ($10::boolean OR ss.authentication_method<>'dev_phone') AND viewer.revoked_at IS NULL AND viewer.expires_at>clock_timestamp() AND viewer.idle_expires_at>clock_timestamp() AND ($10::boolean OR viewer.authentication_method<>'dev_phone')`, a.InstallationID, a.Actor.ID, a.ProofDigest[:], in.ExpectedVersion, a.SessionDigest[:], in.RegistrationKind, app, env, fingerprint[:], p.store.devPhoneEnabled).Scan(&binding, &matches)
	if errors.Is(e, pgx.ErrNoRows) {
		return sp.AddressMatch{}, sp.ErrConflict
	}
	if e != nil {
		return sp.AddressMatch{}, sp.ErrUnavailable
	}
	if e = p.finishSession(ctx, tx, a, generation); e != nil {
		return sp.AddressMatch{}, pushError(e)
	}
	v := sp.AddressMatch{Schema: "system-push-address-match-v1", InstallationID: a.InstallationID, BindingID: binding, Version: in.ExpectedVersion, Matches: matches}
	raw, _ := json.Marshal(v)
	v.Proof = &pushAddressProof{p, a, in.RegistrationKind, fingerprint, in.ExpectedVersion, string(raw), generation}
	return v, nil
}
func (p *SystemPush) ValidateAddressMatch(ctx context.Context, a sp.Access, in sp.AddressCheckInput, v sp.AddressMatch) error {
	proof, ok := v.Proof.(*pushAddressProof)
	if !ok || proof == nil || proof.gateway != p || proof.access != a || proof.kind != in.RegistrationKind || proof.expected != in.ExpectedVersion || !sp.ValidAddressMatch(v) {
		return sp.ErrUnavailable
	}
	app, env, allowed := p.config.Namespace(in.RegistrationKind)
	if !allowed {
		return sp.ErrNotFound
	}
	fp, e := p.ring.Fingerprint(in.RegistrationKind, app, env, in.Address)
	raw, _ := json.Marshal(v)
	if e != nil || fp != proof.fingerprint || string(raw) != proof.encoded {
		return sp.ErrUnavailable
	}
	current, e := p.CheckAddress(ctx, a, in)
	if e != nil {
		return e
	}
	currentProof, ok := current.Proof.(*pushAddressProof)
	if !ok || currentProof.generation != proof.generation {
		return identity.ErrUnauthorized
	}
	actual, _ := json.Marshal(current)
	if string(actual) != proof.encoded {
		return sp.ErrConflict
	}
	return nil
}
func pushAAD(iid string, r pushInstallationRow) []byte {
	b, _ := json.Marshal([]any{"birdtie-push-address-v1", iid, r.owner, r.session, r.generation, r.binding, r.version, r.kind, r.app, r.environment})
	return b
}
func pushRequestDigest(operation string, expected int64, in sp.RegistrationInput, fingerprint [32]byte, app, env string) [32]byte {
	b, _ := json.Marshal([]any{"system-push-operation-v1", operation, expected, in.RegistrationKind, in.Platform, in.Permission, fingerprint, app, env})
	return sha256.Sum256(b)
}
func scanPushOperation(row scanner) (sp.Operation, []byte, string, error) {
	v := sp.Operation{Schema: "system-push-operation-v1", State: "COMMITTED"}
	var digest []byte
	var actor string
	e := row.Scan(&v.OperationID, &v.Operation, &v.ExpectedVersion, &v.ResultVersion, &v.BindingID, &v.CommittedAt, &digest, &actor)
	return v, digest, actor, e
}

const pushOperationColumns = `operation_id::text,operation,expected_version,result_version,binding_id::text,committed_at,request_digest,actor_id::text`

func (p *SystemPush) change(ctx context.Context, a sp.Access, in sp.RegistrationInput, revoke sp.RevokeInput, isRevoke bool) (sp.Operation, error) {
	var out sp.Operation
	if !p.Enabled() && !isRevoke {
		return out, sp.ErrDisabled
	}
	operation := "REGISTER"
	op, expected := in.OperationID, in.ExpectedVersion
	var fingerprint [32]byte
	app, env := "", ""
	if isRevoke {
		if e := sp.ValidateRevoke(revoke); e != nil {
			return out, e
		}
		operation = "REVOKE"
		op, expected = revoke.OperationID, revoke.ExpectedVersion
	} else {
		if e := sp.ValidateRegistration(in); e != nil {
			return out, e
		}
		var allowed bool
		app, env, allowed = p.config.Namespace(in.RegistrationKind)
		if !allowed {
			return out, sp.ErrDisabled
		}
		var e error
		fingerprint, e = p.ring.Fingerprint(in.RegistrationKind, app, env, in.Address)
		if e != nil {
			return out, sp.ErrUnavailable
		}
	}
	digest := pushRequestDigest(operation, expected, in, fingerprint, app, env)
	tx, generation, e := p.begin(ctx, a)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(context.Background())
	// This account-scoped transaction lock bounds the number of live devices even
	// when two different installations register concurrently.
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('system-push-owner:'||$1::text,0))`, a.Actor.ID); e != nil {
		return out, sp.ErrUnavailable
	}
	if _, e = tx.Exec(ctx, `INSERT INTO system_push_installations(installation_id,proof_sha256) VALUES($1,$2) ON CONFLICT DO NOTHING`, a.InstallationID, a.ProofDigest[:]); e != nil {
		return out, pushError(e)
	}
	row, e := readPushInstallation(ctx, tx, a, "FOR UPDATE")
	if e != nil {
		return out, pushError(e)
	}
	if e = p.lockSession(ctx, tx, a, generation); e != nil {
		return out, pushError(e)
	}
	old, oldDigest, oldActor, e := scanPushOperation(tx.QueryRow(ctx, `SELECT `+pushOperationColumns+` FROM system_push_operations WHERE installation_id=$1 AND operation_id=$2`, a.InstallationID, op))
	if e == nil {
		if oldActor != a.Actor.ID || subtle.ConstantTimeCompare(oldDigest, digest[:]) != 1 {
			return out, sp.ErrConflict
		}
		if e = p.finishSession(ctx, tx, a, generation); e != nil {
			return out, pushError(e)
		}
		old.Proof = &pushSessionProof{p, a, generation}
		return old, nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return out, sp.ErrUnavailable
	}
	if row.version != expected {
		return out, sp.ErrConflict
	}
	if isRevoke {
		// A holder of the original installation proof can revoke its old account's
		// binding after switching accounts, but cannot see the previous owner.
		_, e = tx.Exec(ctx, `UPDATE system_push_installations SET version=version+1,state='REVOKED',binding_id=NULL,address_key_id=NULL,address_nonce=NULL,address_ciphertext=NULL,address_fingerprint=NULL,updated_at=clock_timestamp() WHERE installation_id=$1`, a.InstallationID)
	} else {
		var count int
		if e = tx.QueryRow(ctx, `SELECT count(*) FROM system_push_installations p JOIN sessions ss ON ss.id=p.session_id JOIN accounts a ON a.id=p.owner_id WHERE p.owner_id=$1 AND p.installation_id<>$2 AND `+pushBindingCurrent+` AND ($3::boolean OR ss.authentication_method<>'dev_phone')`, a.Actor.ID, a.InstallationID, p.store.devPhoneEnabled).Scan(&count); e != nil {
			return out, sp.ErrUnavailable
		}
		if count >= 5 {
			return out, sp.ErrConflict
		}
		var sid, binding string
		var generation int64
		var registered, until time.Time
		if e = tx.QueryRow(ctx, `SELECT ss.id::text,ss.authorization_generation,gen_random_uuid()::text,clock_timestamp(),LEAST(ss.expires_at,clock_timestamp()+interval '30 days') FROM sessions ss WHERE ss.token_sha256=$1 AND ss.account_id=$2 AND ss.revoked_at IS NULL AND ss.expires_at>clock_timestamp() AND ss.idle_expires_at>clock_timestamp() AND ($3::boolean OR ss.authentication_method<>'dev_phone')`, a.SessionDigest[:], a.Actor.ID, p.store.devPhoneEnabled).Scan(&sid, &generation, &binding, &registered, &until); e != nil {
			return out, pushError(e)
		}
		owner, kind := a.Actor.ID, in.RegistrationKind
		row.owner = &owner
		row.session = &sid
		row.generation = &generation
		row.binding = &binding
		row.version++
		row.kind = &kind
		row.app = &app
		row.environment = &env
		sealed, se := p.ring.Seal(in.Address, pushAAD(a.InstallationID, row))
		if se != nil {
			return out, sp.ErrUnavailable
		}
		_, e = tx.Exec(ctx, `UPDATE system_push_installations SET version=$2,owner_id=$3,session_id=$4,session_generation=$5,binding_id=$6,platform=$7,registration_kind=$8,application_id=$9,environment=$10,permission=$11,state='ACTIVE',address_key_id=$12,address_nonce=$13,address_ciphertext=$14,address_fingerprint=$15,registered_at=$16,valid_until=$17,updated_at=clock_timestamp() WHERE installation_id=$1`, a.InstallationID, row.version, owner, sid, generation, binding, in.Platform, in.RegistrationKind, app, env, in.Permission, sealed.KeyID, sealed.Nonce, sealed.Ciphertext, fingerprint[:], registered, until)
	}
	if e != nil {
		return out, pushError(e)
	}
	// Cancelling prepared work is safe. In-flight sends remain SENDING and retain
	// their eventual outcome; provider acceptance cannot be recalled by a revoke.
	if _, e = tx.Exec(ctx, `WITH cancelled AS(UPDATE system_push_events SET state='CANCELLED',lease_until=NULL WHERE installation_id=$1 AND state IN ('QUEUED','PREPARED','RETRY_WAIT') RETURNING current_attempt_id)
 UPDATE system_push_attempts SET state='REJECTED',code='BINDING_CHANGED',finished_at=clock_timestamp() WHERE id IN(SELECT current_attempt_id FROM cancelled) AND state='PREPARED'`, a.InstallationID); e != nil {
		return out, sp.ErrUnavailable
	}
	binding := row.binding
	if isRevoke {
		binding = nil
	}
	out, _, _, e = scanPushOperation(tx.QueryRow(ctx, `INSERT INTO system_push_operations(installation_id,operation_id,actor_id,request_digest,operation,expected_version,result_version,binding_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING `+pushOperationColumns, a.InstallationID, op, a.Actor.ID, digest[:], operation, expected, expected+1, binding))
	if e != nil {
		return sp.Operation{}, pushError(e)
	}
	if e = p.finishSession(ctx, tx, a, generation); e != nil {
		return sp.Operation{}, pushError(e)
	}
	out.Proof = &pushSessionProof{p, a, generation}
	return out, nil
}
func (p *SystemPush) RegisterInstallation(ctx context.Context, a sp.Access, in sp.RegistrationInput) (sp.Operation, error) {
	return p.change(ctx, a, in, sp.RevokeInput{}, false)
}
func (p *SystemPush) RevokeInstallation(ctx context.Context, a sp.Access, in sp.RevokeInput) (sp.Operation, error) {
	return p.change(ctx, a, sp.RegistrationInput{}, in, true)
}
func (p *SystemPush) ReadOperation(ctx context.Context, a sp.Access, op string) (sp.Operation, error) {
	if !sp.ValidID(op) {
		return sp.Operation{}, sp.ErrInvalid
	}
	tx, generation, e := p.begin(ctx, a)
	if e != nil {
		return sp.Operation{}, e
	}
	defer tx.Rollback(context.Background())
	if _, e = readPushInstallation(ctx, tx, a, "FOR SHARE"); e != nil {
		return sp.Operation{}, pushError(e)
	}
	v, _, actor, e := scanPushOperation(tx.QueryRow(ctx, `SELECT `+pushOperationColumns+` FROM system_push_operations WHERE installation_id=$1 AND operation_id=$2`, a.InstallationID, op))
	if errors.Is(e, pgx.ErrNoRows) || e == nil && actor != a.Actor.ID {
		return sp.Operation{}, sp.ErrNotFound
	}
	if e != nil {
		return sp.Operation{}, sp.ErrUnavailable
	}
	if e = p.lockSession(ctx, tx, a, generation); e == nil {
		e = p.finishSession(ctx, tx, a, generation)
	}
	if e != nil {
		return sp.Operation{}, pushError(e)
	}
	v.Proof = &pushSessionProof{p, a, generation}
	return v, nil
}
func (p *SystemPush) ValidateOperation(ctx context.Context, a sp.Access, v sp.Operation) error {
	if e := p.proofSession(ctx, a, v.Proof); e != nil {
		return e
	}
	if !sp.ValidOperation(v) {
		return sp.ErrUnavailable
	}
	current, e := p.ReadOperation(ctx, a, v.OperationID)
	if e != nil {
		return e
	}
	x, _ := json.Marshal(v)
	y, _ := json.Marshal(current)
	if string(x) != string(y) {
		return sp.ErrConflict
	}
	return p.proofSession(ctx, a, v.Proof)
}
