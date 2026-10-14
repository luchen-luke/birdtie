package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/identity"

	sp "github.com/birdtie/birdtie/apps/api/internal/systempush"
	"github.com/jackc/pgx/v5"
)

type pushOpenProof struct {
	gateway    *SystemPush
	access     sp.Access
	input      sp.OpenInput
	encoded    string
	generation int64
}

// All source, original Inbox ACL, installation proof, registration Session and
// current viewer Session conditions are evaluated in the same final statement.
const pushOpenFrom = ` FROM system_push_events e
 JOIN system_push_installations p ON p.installation_id=e.installation_id
 JOIN sessions ss ON ss.id=p.session_id JOIN accounts a ON a.id=p.owner_id
 JOIN sessions viewer ON viewer.token_sha256=$6 AND viewer.account_id=$5
 JOIN inbox_items i ON i.id=e.inbox_id AND i.recipient_account_id=e.recipient_id
 LEFT JOIN native_notification_decisions d ON d.id=i.routing_decision_id
 WHERE e.id=$1 AND e.installation_id=$2 AND e.binding_id=$3 AND e.binding_version=$4 AND e.recipient_id=$5
 AND p.proof_sha256=$7 AND p.owner_id=$5 AND p.binding_id=e.binding_id AND p.version=e.binding_version
 AND e.expires_at>clock_timestamp() AND ` + pushBindingCurrent + `
 AND ($8::boolean OR ss.authentication_method<>'dev_phone')
 AND viewer.revoked_at IS NULL AND viewer.expires_at>clock_timestamp() AND viewer.idle_expires_at>clock_timestamp()
 AND viewer.authorization_generation=$12
 AND ($8::boolean OR viewer.authentication_method<>'dev_phone')
 AND p.registration_kind=$9 AND p.application_id=$10 AND p.environment=$11
 AND ` + inboxVisibility

func (p *SystemPush) openQuery(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, a sp.Access, in sp.OpenInput, row pushInstallationRow, generation int64) (sp.Open, error) {
	if row.kind == nil || row.app == nil || row.environment == nil {
		return sp.Open{}, sp.ErrNotFound
	}
	app, env, allowed := p.config.Namespace(*row.kind)
	if !allowed || app != *row.app || env != *row.environment {
		return sp.Open{}, sp.ErrNotFound
	}
	item, e := scanInboxItem(q.QueryRow(ctx, `SELECT `+inboxColumns+pushOpenFrom, in.EventID, a.InstallationID, in.BindingID, in.BindingVersion, a.Actor.ID, a.SessionDigest[:], a.ProofDigest[:], p.store.devPhoneEnabled, *row.kind, app, env, generation))
	if errors.Is(e, pgx.ErrNoRows) {
		return sp.Open{}, sp.ErrNotFound
	}
	if e != nil || ctx.Err() != nil {
		return sp.Open{}, sp.ErrUnavailable
	}
	return sp.Open{Schema: "system-push-open-v1", EventID: in.EventID, BindingID: in.BindingID, BindingVersion: in.BindingVersion, Item: item}, nil
}
func (p *SystemPush) ResolveEvent(ctx context.Context, a sp.Access, in sp.OpenInput) (sp.Open, error) {
	if !sp.ValidAccess(a) {
		return sp.Open{}, sp.ErrNotFound
	}
	if !sp.ValidOpen(in) {
		return sp.Open{}, sp.ErrInvalid
	}
	if !p.Enabled() {
		return sp.Open{}, sp.ErrNotFound
	}
	generation, e := p.captureSession(ctx, a)
	if e != nil {
		return sp.Open{}, e
	}
	tx, e := p.store.beginHumanInboxTx(ctx, a.SessionDigest, a.Actor)
	if e != nil {
		return sp.Open{}, pushError(e)
	}
	defer tx.Rollback(context.Background())
	row, e := readPushInstallation(ctx, tx, a, "FOR SHARE")
	if e != nil {
		return sp.Open{}, pushError(e)
	}
	var id string
	e = tx.QueryRow(ctx, `SELECT i.id FROM system_push_events e JOIN inbox_items i ON i.id=e.inbox_id WHERE e.id=$1 AND e.installation_id=$2 AND e.recipient_id=$3 FOR SHARE OF i`, in.EventID, a.InstallationID, a.Actor.ID).Scan(&id)
	if errors.Is(e, pgx.ErrNoRows) {
		return sp.Open{}, sp.ErrNotFound
	}
	if e != nil {
		return sp.Open{}, sp.ErrUnavailable
	}
	if e = p.lockSession(ctx, tx, a, generation); e != nil {
		return sp.Open{}, pushError(e)
	}
	v, e := p.openQuery(ctx, tx, a, in, row, generation)
	if e != nil {
		return sp.Open{}, e
	}
	if e = p.finishSession(ctx, tx, a, generation); e != nil {
		return sp.Open{}, pushError(e)
	}
	raw, _ := json.Marshal(v)
	v.Proof = &pushOpenProof{p, a, in, string(raw), generation}
	return v, nil
}
func (p *SystemPush) ValidateEvent(ctx context.Context, a sp.Access, in sp.OpenInput, v sp.Open) error {
	proof, ok := v.Proof.(*pushOpenProof)
	raw, e := json.Marshal(v)
	if e != nil || !ok || proof == nil || proof.gateway != p || proof.access != a || proof.input != in || proof.encoded != string(raw) {
		return sp.ErrUnavailable
	}
	// Resolve again first to honor all real relation/resource/Session waits.
	current, e := p.ResolveEvent(ctx, a, in)
	if e != nil {
		return e
	}
	currentProof, ok := current.Proof.(*pushOpenProof)
	if !ok || currentProof.generation != proof.generation {
		return identity.ErrUnauthorized
	}
	now, _ := json.Marshal(current)
	if string(now) != proof.encoded {
		return sp.ErrNotFound
	}
	// No wait follows this combined source/current-session snapshot. The entire
	// original object is re-read and compared, so changes to read_at/targets/body
	// cannot release a stale cached item after an authorization wait.
	row, e := scanPushInstallation(p.store.pool.QueryRow(ctx, `SELECT `+pushInstallationColumns+` FROM system_push_installations WHERE installation_id=$1 AND proof_sha256=$2`, a.InstallationID, a.ProofDigest[:]))
	if errors.Is(e, pgx.ErrNoRows) {
		return sp.ErrNotFound
	}
	if e != nil {
		return sp.ErrUnavailable
	}
	fresh, e := p.openQuery(ctx, p.store.pool, a, in, row, proof.generation)
	if e != nil {
		return e
	}
	freshRaw, _ := json.Marshal(fresh)
	if string(freshRaw) != proof.encoded {
		return sp.ErrNotFound
	}
	return nil
}
