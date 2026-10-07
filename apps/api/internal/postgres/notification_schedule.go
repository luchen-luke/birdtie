package postgres

import (
	"context"
	"encoding/json"
	"errors"
	ns "github.com/birdtie/birdtie/apps/api/internal/agentnotificationschedule"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"math"
	"time"
)

const notificationScheduleColumns = `p.version,p.agent_id,p.settings,p.valid_from,p.expires_at,p.updated_at,
 EXISTS(SELECT 1 FROM sessions origin WHERE origin.id=p.session_id AND origin.account_id=p.owner_id
 AND origin.revoked_at IS NULL AND origin.expires_at>clock_timestamp() AND origin.idle_expires_at>clock_timestamp()),clock_timestamp()`

func notificationScheduleError(e error) error {
	switch {
	case errors.Is(e, agentprofile.ErrForbidden), errors.Is(e, agentprofile.ErrNotFound):
		return ns.ErrDenied
	case errors.Is(e, agentprofile.ErrInvalid), errors.Is(e, ns.ErrInvalid):
		return ns.ErrInvalid
	case errors.Is(e, ns.ErrChanged):
		return ns.ErrChanged
	case errors.Is(e, ns.ErrDenied):
		return ns.ErrDenied
	}
	var pg *pgconn.PgError
	if errors.As(e, &pg) && (pg.Code == "23505" || pg.Code == "40001" || pg.Code == "40P01") {
		return ns.ErrChanged
	}
	return ns.ErrUnavailable
}
func scanNotificationSchedule(row pgx.Row) (ns.Policy, error) {
	p := ns.Policy{SchemaVersion: ns.SchemaVersion, Configured: true, BudgetWindowHours: ns.BudgetWindowHours}
	var raw []byte
	var session bool
	var at time.Time
	e := row.Scan(&p.Version, &p.AgentID, &raw, &p.ValidFrom, &p.ExpiresAt, &p.UpdatedAt, &session, &at)
	if e != nil {
		return ns.Policy{}, e
	}
	p.Settings, e = ns.DecodeSettings(raw)
	if e != nil {
		return ns.Policy{}, ns.ErrUnavailable
	}
	p.Status = "ACTIVE"
	if !p.Enabled {
		p.Status = "DISABLED"
	}
	if p.ExpiresAt == nil || !p.ExpiresAt.After(at) {
		p.Status = "EXPIRED"
	} else if !session {
		p.Status = "PAUSED_SESSION"
	}
	if ns.ValidatePolicy(p) != nil {
		return ns.Policy{}, ns.ErrUnavailable
	}
	return p, nil
}
func (s *Store) beginNotificationSchedule(ctx context.Context, a agentprofile.PrivateAccess) (pgx.Tx, agentPrivateBinding, error) {
	if ctx == nil || ctx.Err() != nil || s == nil || s.pool == nil {
		return nil, agentPrivateBinding{}, ns.ErrUnavailable
	}
	if agentprofile.ValidatePrivateAccess(a) != nil {
		return nil, agentPrivateBinding{}, ns.ErrDenied
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return nil, agentPrivateBinding{}, ns.ErrUnavailable
	}
	b, e := lockOwnAgentPrivateBinding(ctx, tx, a, s.devPhoneEnabled)
	if e == nil {
		_, e = lockAgentPrivateMetadata(ctx, tx, b, false)
	}
	if e != nil {
		_ = tx.Rollback(context.Background())
		return nil, b, notificationScheduleError(e)
	}
	return tx, b, nil
}
func (s *Store) GetOwnNotificationSchedule(ctx context.Context, a agentprofile.PrivateAccess) (ns.Policy, error) {
	tx, b, e := s.beginNotificationSchedule(ctx, a)
	if e != nil {
		return ns.Policy{}, e
	}
	defer tx.Rollback(context.Background())
	p, e := scanNotificationSchedule(tx.QueryRow(ctx, `SELECT `+notificationScheduleColumns+` FROM native_notification_schedules p WHERE p.owner_id=$1 AND p.agent_id=$2 FOR SHARE OF p`, b.accountID, b.agentID))
	if errors.Is(e, pgx.ErrNoRows) {
		p, e = ns.DefaultPolicy(b.agentID)
	}
	if e != nil {
		return ns.Policy{}, notificationScheduleError(e)
	}
	if e = recheckAgentPrivateSession(ctx, tx, b); e != nil {
		return ns.Policy{}, notificationScheduleError(e)
	}
	if e = tx.Commit(ctx); e != nil {
		return ns.Policy{}, notificationScheduleError(e)
	}
	return p, nil
}
func (s *Store) PutOwnNotificationSchedule(ctx context.Context, a agentprofile.PrivateAccess, in ns.PutInput) (ns.Policy, error) {
	tx, b, e := s.beginNotificationSchedule(ctx, a)
	if e != nil {
		return ns.Policy{}, e
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('notification-schedule:'||$1::text,0))`, b.accountID); e != nil {
		return ns.Policy{}, ns.ErrUnavailable
	}
	var current uint64
	e = tx.QueryRow(ctx, `SELECT version FROM native_notification_schedules WHERE owner_id=$1 AND agent_id=$2 FOR UPDATE`, b.accountID, b.agentID).Scan(&current)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return ns.Policy{}, ns.ErrUnavailable
	}
	if current != in.ExpectedVersion || current == math.MaxInt64 {
		return ns.Policy{}, ns.ErrChanged
	}
	var now time.Time
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return ns.Policy{}, ns.ErrUnavailable
	}
	in, e = ns.NormalizePut(in, now.UTC())
	if e != nil {
		return ns.Policy{}, e
	}
	raw, e := json.Marshal(in.Settings)
	if e != nil {
		return ns.Policy{}, ns.ErrInvalid
	}
	p, e := scanNotificationSchedule(tx.QueryRow(ctx, `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() n), saved AS (
 INSERT INTO native_notification_schedules(owner_id,agent_id,session_id,version,settings,valid_from,expires_at,updated_at)
 SELECT $1,$2,$3,1,$4,stamp.n,$5,stamp.n FROM stamp ON CONFLICT(owner_id) DO UPDATE SET
 version=native_notification_schedules.version+1,session_id=EXCLUDED.session_id,settings=EXCLUDED.settings,
 valid_from=EXCLUDED.valid_from,expires_at=EXCLUDED.expires_at,updated_at=EXCLUDED.updated_at
 WHERE native_notification_schedules.agent_id=EXCLUDED.agent_id AND native_notification_schedules.version=$6 RETURNING *)
 SELECT `+notificationScheduleColumns+` FROM saved p`, b.accountID, b.agentID, b.sessionID, raw, in.ExpiresAt, current))
	if e != nil {
		return ns.Policy{}, notificationScheduleError(e)
	}
	if e = insertDomainAudit(ctx, tx, b.accountID, "replace", "notification_schedule", b.agentID, "human_notification_schedule_edit", nil); e != nil {
		return ns.Policy{}, ns.ErrUnavailable
	}
	if e = finishNotificationScheduleEdit(ctx, tx, b, p.Version, s.devPhoneEnabled); e != nil {
		return ns.Policy{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return ns.Policy{}, notificationScheduleError(e)
	}
	return p, nil
}
func finishNotificationScheduleEdit(ctx context.Context, tx pgx.Tx, b agentPrivateBinding, version uint64, dev bool) error {
	var session, current bool
	e := tx.QueryRow(ctx, `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() n)
 SELECT EXISTS(SELECT 1 FROM sessions se CROSS JOIN stamp WHERE se.id=$3 AND se.account_id=$1 AND se.revoked_at IS NULL AND se.expires_at>stamp.n AND se.idle_expires_at>stamp.n AND ($5::boolean OR se.authentication_method<>'dev_phone')),
 EXISTS(SELECT 1 FROM native_notification_schedules p CROSS JOIN stamp WHERE p.owner_id=$1 AND p.agent_id=$2 AND p.version=$4 AND p.session_id=$3 AND p.valid_from<=stamp.n AND p.expires_at>stamp.n)`, b.accountID, b.agentID, b.sessionID, version, dev).Scan(&session, &current)
	if e != nil {
		return ns.ErrUnavailable
	}
	if !session {
		return ns.ErrDenied
	}
	if !current {
		return ns.ErrChanged
	}
	return nil
}
