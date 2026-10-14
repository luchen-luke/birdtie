package postgres

import (
	"context"
	"errors"
	"regexp"
	"time"

	sp "github.com/birdtie/birdtie/apps/api/internal/systempush"
	"github.com/jackc/pgx/v5"
)

func (p *SystemPush) Claim(ctx context.Context, kinds []sp.Kind, limit int) ([]sp.Claim, error) {
	if !p.Enabled() {
		return []sp.Claim{}, nil
	}
	if limit < 1 || limit > 100 || len(kinds) == 0 || len(kinds) > 2 {
		return nil, sp.ErrInvalid
	}
	values := []string{}
	for _, k := range kinds {
		if _, _, allowed := p.config.Namespace(k); !allowed {
			return nil, sp.ErrConfig
		}
		values = append(values, string(k))
	}
	tx, e := p.store.pool.BeginTx(ctx, pgx.TxOptions{})
	if e != nil {
		return nil, sp.ErrUnavailable
	}
	defer tx.Rollback(context.Background())
	// Only expired PREPARED attempts are safe to retry; expired SENDING attempts
	// mean the provider may have accepted a send and are permanently UNKNOWN.
	rows, e := tx.Query(ctx, `SELECT id::text,current_attempt_id::text,state FROM system_push_events WHERE state IN ('PREPARED','SENDING') AND lease_until<=clock_timestamp() ORDER BY lease_until,id LIMIT 100 FOR UPDATE SKIP LOCKED`)
	if e != nil {
		return nil, sp.ErrUnavailable
	}
	type expired struct{ id, attempt, state string }
	old := []expired{}
	for rows.Next() {
		var r expired
		if e = rows.Scan(&r.id, &r.attempt, &r.state); e != nil {
			rows.Close()
			return nil, sp.ErrUnavailable
		}
		old = append(old, r)
	}
	rows.Close()
	if rows.Err() != nil {
		return nil, sp.ErrUnavailable
	}
	for _, r := range old {
		state, outcome, code := "QUEUED", "REJECTED", "PREPARE_LEASE_EXPIRED"
		if r.state == "SENDING" {
			state, outcome, code = "UNKNOWN", "UNKNOWN", "LEASE_UNKNOWN"
		}
		if _, e = tx.Exec(ctx, `UPDATE system_push_attempts SET state=$2,code=$3,finished_at=clock_timestamp() WHERE id=$1 AND state IN ('PREPARED','SENDING')`, r.attempt, outcome, code); e != nil {
			return nil, sp.ErrUnavailable
		}
		if _, e = tx.Exec(ctx, `UPDATE system_push_events SET state=$2,lease_until=NULL,current_attempt_id=NULL WHERE id=$1`, r.id, state); e != nil {
			return nil, sp.ErrUnavailable
		}
	}
	rows, e = tx.Query(ctx, `SELECT id::text,registration_kind FROM system_push_events WHERE state IN ('QUEUED','RETRY_WAIT') AND next_attempt_at<=clock_timestamp() AND registration_kind=ANY($1::text[]) ORDER BY next_attempt_at,id LIMIT $2 FOR UPDATE SKIP LOCKED`, values, limit)
	if e != nil {
		return nil, sp.ErrUnavailable
	}
	claims := []sp.Claim{}
	for rows.Next() {
		var c sp.Claim
		if e = rows.Scan(&c.EventID, &c.Kind); e != nil {
			rows.Close()
			return nil, sp.ErrUnavailable
		}
		claims = append(claims, c)
	}
	rows.Close()
	if rows.Err() != nil {
		return nil, sp.ErrUnavailable
	}
	for index := range claims {
		c := &claims[index]
		if e = tx.QueryRow(ctx, `INSERT INTO system_push_attempts(id,event_id,state) VALUES(gen_random_uuid(),$1,'PREPARED') RETURNING id::text`, c.EventID).Scan(&c.AttemptID); e != nil {
			return nil, sp.ErrUnavailable
		}
		if _, e = tx.Exec(ctx, `UPDATE system_push_events SET state='PREPARED',current_attempt_id=$2,lease_until=clock_timestamp()+interval '2 minutes' WHERE id=$1`, c.EventID, c.AttemptID); e != nil {
			return nil, sp.ErrUnavailable
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, sp.ErrUnavailable
	}
	return claims, nil
}

const pushSendFrom = ` FROM system_push_events e JOIN system_push_installations p ON p.installation_id=e.installation_id
 JOIN sessions ss ON ss.id=p.session_id JOIN accounts a ON a.id=p.owner_id
 JOIN inbox_items i ON i.id=e.inbox_id AND i.recipient_account_id=e.recipient_id
 JOIN native_notification_decisions d ON d.id=i.routing_decision_id
 WHERE e.id=$1 AND e.current_attempt_id=$2 AND e.state='PREPARED' AND e.lease_until>clock_timestamp()
 AND e.binding_id=p.binding_id AND e.binding_version=p.version AND e.recipient_id=p.owner_id AND e.registration_kind=p.registration_kind
 AND e.expires_at>clock_timestamp() AND e.attempt_count<5 AND i.read_at IS NULL AND d.disposition IN ('IMMEDIATE','NORMAL')
 AND ` + pushBindingCurrent + ` AND ($3::boolean OR ss.authentication_method<>'dev_phone') AND p.application_id=$4 AND p.environment=$5 AND ` + inboxVisibility

func (p *SystemPush) PrepareSend(ctx context.Context, c sp.Claim) (sp.SendRequest, error) {
	if !p.Enabled() {
		return sp.SendRequest{}, sp.ErrDisabled
	}
	if !sp.ValidID(c.EventID) || !sp.ValidID(c.AttemptID) {
		return sp.SendRequest{}, sp.ErrInvalid
	}
	app, env, allowed := p.config.Namespace(c.Kind)
	if !allowed {
		return sp.SendRequest{}, sp.ErrDisabled
	}
	tx, e := p.store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return sp.SendRequest{}, sp.ErrUnavailable
	}
	defer tx.Rollback(context.Background())
	var iid, owner string
	e = tx.QueryRow(ctx, `SELECT installation_id::text,recipient_id::text FROM system_push_events WHERE id=$1`, c.EventID).Scan(&iid, &owner)
	if e != nil {
		return sp.SendRequest{}, sp.ErrNotFound
	}
	var locked string
	e = tx.QueryRow(ctx, `SELECT id::text FROM accounts WHERE id=$1 FOR SHARE`, owner).Scan(&locked)
	if e != nil {
		return sp.SendRequest{}, sp.ErrUnavailable
	}
	row, e := scanPushInstallation(tx.QueryRow(ctx, `SELECT `+pushInstallationColumns+` FROM system_push_installations WHERE installation_id=$1 FOR SHARE`, iid))
	if e != nil {
		return sp.SendRequest{}, sp.ErrUnavailable
	}
	e = tx.QueryRow(ctx, `SELECT id::text FROM system_push_events WHERE id=$1 AND current_attempt_id=$2 AND state='PREPARED' FOR UPDATE`, c.EventID, c.AttemptID).Scan(&locked)
	if e != nil {
		return sp.SendRequest{}, sp.ErrNotFound
	}
	// Inbox lock before Session. Native source predicates are still read again in
	// the final statement after every wait, without reusing an earlier ACL result.
	e = tx.QueryRow(ctx, `SELECT i.id::text FROM inbox_items i JOIN system_push_events e ON e.inbox_id=i.id WHERE e.id=$1 FOR SHARE OF i`, c.EventID).Scan(&locked)
	cancel := func() (sp.SendRequest, error) {
		if _, e = tx.Exec(ctx, `UPDATE system_push_events SET state='CANCELLED',lease_until=NULL WHERE id=$1 AND current_attempt_id=$2 AND state='PREPARED'`, c.EventID, c.AttemptID); e != nil {
			return sp.SendRequest{}, sp.ErrUnavailable
		}
		if _, e = tx.Exec(ctx, `UPDATE system_push_attempts SET state='REJECTED',code='SOURCE_UNAVAILABLE',finished_at=clock_timestamp() WHERE id=$1 AND state='PREPARED'`, c.AttemptID); e != nil {
			return sp.SendRequest{}, sp.ErrUnavailable
		}
		if e = tx.Commit(ctx); e != nil {
			return sp.SendRequest{}, sp.ErrUnavailable
		}
		return sp.SendRequest{}, sp.ErrNotFound
	}
	if errors.Is(e, pgx.ErrNoRows) {
		return cancel()
	}
	if e != nil {
		return sp.SendRequest{}, sp.ErrUnavailable
	}
	if row.session == nil {
		return cancel()
	}
	e = tx.QueryRow(ctx, `SELECT id::text FROM sessions WHERE id=$1 FOR SHARE`, *row.session).Scan(&locked)
	if errors.Is(e, pgx.ErrNoRows) {
		return cancel()
	}
	if e != nil {
		return sp.SendRequest{}, sp.ErrUnavailable
	}
	var current bool
	e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 `+pushSendFrom+`)`, c.EventID, c.AttemptID, p.store.devPhoneEnabled, app, env).Scan(&current)
	if e != nil {
		return sp.SendRequest{}, sp.ErrUnavailable
	}
	if !current {
		return cancel()
	}
	if row.keyID == nil || row.binding == nil {
		return cancel()
	}
	address, e := p.ring.Open(sp.Ciphertext{KeyID: *row.keyID, Nonce: row.nonce, Ciphertext: row.cipher}, pushAAD(iid, row))
	if e != nil || !address.Valid(c.Kind) {
		return cancel()
	}
	// This last source statement both rechecks and durably fences external send.
	// After this commit a crash, timeout, or lost receipt must never re-send.
	var binding string
	var version int64
	e = tx.QueryRow(ctx, `WITH permitted AS MATERIALIZED(SELECT e.id,e.binding_id,e.binding_version `+pushSendFrom+`), started AS(UPDATE system_push_events e SET state='SENDING',attempt_count=attempt_count+1,lease_until=clock_timestamp()+interval '2 minutes' FROM permitted q WHERE e.id=q.id RETURNING q.binding_id::text,q.binding_version) SELECT * FROM started`, c.EventID, c.AttemptID, p.store.devPhoneEnabled, app, env).Scan(&binding, &version)
	if errors.Is(e, pgx.ErrNoRows) {
		return cancel()
	}
	if e != nil {
		return sp.SendRequest{}, sp.ErrUnavailable
	}
	if _, e = tx.Exec(ctx, `UPDATE system_push_attempts SET state='SENDING',started_at=clock_timestamp() WHERE id=$1 AND state='PREPARED'`, c.AttemptID); e != nil {
		return sp.SendRequest{}, sp.ErrUnavailable
	}
	if e = tx.Commit(ctx); e != nil {
		return sp.SendRequest{}, sp.ErrUnavailable
	}
	return sp.SendRequest{EventID: c.EventID, BindingID: binding, BindingVersion: version, Kind: c.Kind, Address: address}, nil
}

var pushCode = regexp.MustCompile(`^[A-Z_]{1,64}$`)

func (p *SystemPush) CheckSend(ctx context.Context, c sp.Claim, r sp.SendRequest) error {
	if !p.Enabled() || ctx == nil || ctx.Err() != nil || !r.Valid() || r.EventID != c.EventID || r.Kind != c.Kind {
		return sp.ErrNotFound
	}
	app, env, allowed := p.config.Namespace(c.Kind)
	if !allowed {
		return sp.ErrNotFound
	}
	fingerprint, e := p.ring.Fingerprint(c.Kind, app, env, r.Address)
	if e != nil {
		return sp.ErrUnavailable
	}
	var current bool
	e = p.store.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM system_push_events e JOIN system_push_installations p ON p.installation_id=e.installation_id JOIN sessions ss ON ss.id=p.session_id JOIN accounts a ON a.id=p.owner_id JOIN inbox_items i ON i.id=e.inbox_id AND i.recipient_account_id=e.recipient_id JOIN native_notification_decisions d ON d.id=i.routing_decision_id WHERE e.id=$1 AND e.current_attempt_id=$2 AND e.state='SENDING' AND e.lease_until>clock_timestamp() AND e.binding_id=$3 AND e.binding_version=$4 AND p.binding_id=e.binding_id AND p.version=e.binding_version AND p.owner_id=e.recipient_id AND p.registration_kind=$5 AND p.application_id=$6 AND p.environment=$7 AND p.address_fingerprint=$8 AND e.expires_at>clock_timestamp() AND i.read_at IS NULL AND d.disposition IN ('IMMEDIATE','NORMAL') AND `+pushBindingCurrent+` AND ($9::boolean OR ss.authentication_method<>'dev_phone') AND `+inboxVisibility+`)`, c.EventID, c.AttemptID, r.BindingID, r.BindingVersion, c.Kind, app, env, fingerprint[:], p.store.devPhoneEnabled).Scan(&current)
	if e != nil || ctx.Err() != nil {
		return sp.ErrUnavailable
	}
	if !current {
		return sp.ErrNotFound
	}
	return nil
}

func (p *SystemPush) CompleteSend(ctx context.Context, c sp.Claim, r sp.ProviderResult) error {
	if p == nil || !sp.ValidID(c.EventID) || !sp.ValidID(c.AttemptID) {
		return sp.ErrInvalid
	}
	if !pushCode.MatchString(r.Code) || !sp.ValidResultID(r.MessageID) {
		return sp.ErrInvalid
	}
	switch r.Outcome {
	case sp.Accepted, sp.Retry, sp.Rejected, sp.InvalidToken, sp.Unknown:
	default:
		return sp.ErrInvalid
	}
	tx, e := p.store.pool.BeginTx(ctx, pgx.TxOptions{})
	if e != nil {
		return sp.ErrUnavailable
	}
	defer tx.Rollback(context.Background())
	var iid string
	if e = tx.QueryRow(ctx, `SELECT installation_id::text FROM system_push_events WHERE id=$1`, c.EventID).Scan(&iid); e != nil {
		return sp.ErrNotFound
	}
	var lock string
	if e = tx.QueryRow(ctx, `SELECT installation_id::text FROM system_push_installations WHERE installation_id=$1 FOR UPDATE`, iid).Scan(&lock); e != nil {
		return sp.ErrUnavailable
	}
	var binding, state string
	var version int64
	var count int
	var expiry time.Time
	e = tx.QueryRow(ctx, `SELECT binding_id::text,binding_version,state,attempt_count,expires_at FROM system_push_events WHERE id=$1 AND current_attempt_id=$2 FOR UPDATE`, c.EventID, c.AttemptID).Scan(&binding, &version, &state, &count, &expiry)
	if errors.Is(e, pgx.ErrNoRows) {
		return sp.ErrNotFound
	}
	if e != nil {
		return sp.ErrUnavailable
	}
	if state != "SENDING" {
		return sp.ErrConflict
	}
	eventState := string(r.Outcome)
	if r.Outcome == sp.InvalidToken {
		eventState = "REJECTED"
	}
	var retryAt *time.Time
	if r.Outcome == sp.Retry {
		delay := r.RetryAfter
		if delay < time.Minute {
			delay = time.Minute
		}
		if delay > 24*time.Hour {
			delay = 24 * time.Hour
		}
		backoff := time.Minute * time.Duration(1<<uint(count-1))
		if delay < backoff {
			delay = backoff
		}
		delay += time.Duration(c.AttemptID[0]%31) * time.Second
		var at time.Time
		if e = tx.QueryRow(ctx, `SELECT clock_timestamp()+$1::interval`, delay.String()).Scan(&at); e != nil {
			return sp.ErrUnavailable
		}
		if count < 5 && at.Before(expiry) {
			eventState = "RETRY_WAIT"
			retryAt = &at
		} else {
			eventState = "REJECTED"
		}
	}
	tag, e := tx.Exec(ctx, `UPDATE system_push_attempts SET state=$2,code=$3,provider_message_id=NULLIF($4,''),invalidated_at=$5,retry_at=$6,finished_at=clock_timestamp() WHERE id=$1 AND event_id=$7 AND state='SENDING'`, c.AttemptID, r.Outcome, r.Code, r.MessageID, r.InvalidatedAt, retryAt, c.EventID)
	if e != nil || tag.RowsAffected() != 1 {
		return sp.ErrConflict
	}
	if _, e = tx.Exec(ctx, `UPDATE system_push_events SET state=$2,lease_until=NULL,next_attempt_at=COALESCE($3,next_attempt_at) WHERE id=$1 AND current_attempt_id=$4`, c.EventID, eventState, retryAt, c.AttemptID); e != nil {
		return sp.ErrUnavailable
	}
	if r.Outcome == sp.InvalidToken {
		// FCM exact binding/version; APNS timestamp additionally protects a fresh
		// registration from a delayed 410 response for the same address.
		if _, e = tx.Exec(ctx, `UPDATE system_push_installations SET version=version+1,state='REVOKED',binding_id=NULL,address_key_id=NULL,address_nonce=NULL,address_ciphertext=NULL,address_fingerprint=NULL,updated_at=clock_timestamp() WHERE installation_id=$1 AND binding_id=$2 AND version=$3 AND state='ACTIVE' AND version<4503599627370496 AND ($4::timestamptz IS NULL OR registered_at<=$4)`, iid, binding, version, r.InvalidatedAt); e != nil {
			return sp.ErrUnavailable
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return sp.ErrUnavailable
	}
	return nil
}
