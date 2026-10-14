package postgres

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
	"strconv"
	"strings"
	"sync"
	"time"
)

var _ identity.HumanControlSessionStore = (*Store)(nil)
var humanWriteSigning struct {
	sync.Once
	key [32]byte
	err error
}

type humanWriteFrame struct {
	tx         pgx.Tx
	d          [32]byte
	a          identity.Actor
	session    identity.HumanControlFrame
	family, op string
}

func humanWriteFamily(v string) bool {
	switch v {
	case "FAQ", "CONTEXT", "REPORT", "GRANT_REVOKE", "BLOCK":
		return true
	}
	return false
}
func (s *Store) humanWriteSession(ctx context.Context, tx pgx.Tx, d [32]byte, a identity.Actor, lock bool) (identity.HumanControlFrame, time.Time, error) {
	var f identity.HumanControlFrame
	var deadline time.Time
	if a.AccountType != "person" || !identity.HumanControlUUID(a.ID) || d == ([32]byte{}) {
		return f, deadline, identity.ErrUnauthorized
	}
	if lock {
		var id string
		e := tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 AND account_type='person' AND status='active' FOR SHARE`, a.ID).Scan(&id)
		if errors.Is(e, pgx.ErrNoRows) {
			return f, deadline, identity.ErrUnauthorized
		}
		if e != nil {
			return f, deadline, identity.ErrControlUnavailable
		}
		if e = s.lockHumanMomentSession(ctx, tx, d, a.ID); e != nil {
			return f, deadline, e
		}
	}
	e := tx.QueryRow(ctx, `SELECT se.id,se.authorization_generation,least(se.expires_at,se.idle_expires_at) FROM sessions se JOIN accounts a ON a.id=se.account_id WHERE a.id=$1 AND a.account_type='person' AND a.status='active' AND se.token_sha256=$2 AND se.revoked_at IS NULL AND se.expires_at>clock_timestamp() AND se.idle_expires_at>clock_timestamp() AND($3::boolean OR se.authentication_method<>'dev_phone')`, a.ID, d[:], s.devPhoneEnabled).Scan(&f.ID, &f.Generation, &deadline)
	if errors.Is(e, pgx.ErrNoRows) {
		return f, deadline, identity.ErrUnauthorized
	}
	if e != nil {
		return f, deadline, identity.ErrControlUnavailable
	}
	return f, deadline.UTC(), nil
}
func (s *Store) CaptureHumanControlSession(ctx context.Context, d [32]byte, a identity.Actor) (identity.HumanControlFrame, error) {
	if ctx == nil || ctx.Err() != nil || s == nil || s.pool == nil {
		return identity.HumanControlFrame{}, identity.ErrControlUnavailable
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return identity.HumanControlFrame{}, identity.ErrControlUnavailable
	}
	defer tx.Rollback(context.Background())
	first, _, e := s.humanWriteSession(ctx, tx, d, a, false)
	if e != nil {
		return first, e
	}
	current, _, e := s.humanWriteSession(ctx, tx, d, a, true)
	if e != nil {
		return first, e
	}
	if current != first {
		return first, identity.ErrControlChanged
	}
	if e = tx.Commit(ctx); e != nil {
		return first, identity.ErrControlUnavailable
	}
	return first, nil
}
func (s *Store) ValidateHumanControlSession(ctx context.Context, d [32]byte, a identity.Actor, old identity.HumanControlFrame) error {
	next, e := s.CaptureHumanControlSession(ctx, d, a)
	if e != nil {
		return e
	}
	if next != old {
		return identity.ErrControlChanged
	}
	return nil
}
func (s *Store) humanWriteBound(ctx context.Context, d [32]byte, a identity.Actor) (context.Context, identity.HumanControlFrame, error) {
	if ctx == nil || ctx.Err() != nil {
		return ctx, identity.HumanControlFrame{}, identity.ErrControlUnavailable
	}
	first, valid, present := identity.OriginalHumanControlFrame(ctx, d, a)
	if present {
		if !valid || !identity.HumanControlUUID(first.ID) || first.Generation < 1 {
			return ctx, first, identity.ErrControlChanged
		}
		return ctx, first, nil
	}
	first, e := s.CaptureHumanControlSession(ctx, d, a)
	if e != nil {
		return ctx, first, e
	}
	return identity.WithHumanControlFrame(ctx, d, a, first), first, nil
}
func (s *Store) humanWriteBegin(ctx context.Context, d [32]byte, a identity.Actor, family, op string) (context.Context, humanWriteFrame, error) {
	ctx, first, e := s.humanWriteBound(ctx, d, a)
	f := humanWriteFrame{d: d, a: a, session: first, family: family, op: op}
	if e != nil {
		return ctx, f, e
	}
	if !humanWriteFamily(family) || (op != "" && !identity.HumanControlUUID(op)) {
		return ctx, f, identity.ErrControlInvalid
	}
	f.tx, e = s.pool.Begin(ctx)
	if e != nil {
		return ctx, f, identity.ErrControlUnavailable
	}
	fail := func(e error) (context.Context, humanWriteFrame, error) {
		_ = f.tx.Rollback(context.Background())
		return ctx, f, e
	}
	if _, e = f.tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); e != nil {
		return fail(identity.ErrControlUnavailable)
	}
	if op != "" {
		if _, e = f.tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,142))`, a.ID+":"+family+":"+op); e != nil {
			return fail(identity.ErrControlUnavailable)
		}
	}
	return ctx, f, nil
}
func (s *Store) humanWriteFinish(ctx context.Context, f humanWriteFrame, gates ...func() error) error {
	current, _, e := s.humanWriteSession(ctx, f.tx, f.d, f.a, true)
	if errors.Is(e, identity.ErrUnauthorized) {
		return identity.ErrControlChanged
	}
	if e != nil {
		return e
	}
	if current != f.session {
		return identity.ErrControlChanged
	}
	for _, gate := range gates {
		if e = gate(); e != nil {
			return e
		}
	}
	if e = checkHumanMomentSession(ctx, f.tx, f.d, f.a.ID, s.devPhoneEnabled); e != nil {
		return e
	}
	if e = f.tx.Commit(ctx); e != nil {
		return identity.ErrControlUnavailable
	}
	return nil
}
func (s *Store) humanWriteNow(ctx context.Context, f humanWriteFrame, sourceDeadline *time.Time) (time.Time, time.Time, error) {
	var now time.Time
	if e := f.tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return now, now, identity.ErrControlUnavailable
	}
	current, sessionDeadline, e := s.humanWriteSession(ctx, f.tx, f.d, f.a, false)
	if errors.Is(e, identity.ErrUnauthorized) || e == nil && current != f.session {
		return now, now, identity.ErrControlChanged
	}
	if e != nil {
		return now, now, e
	}
	until := now.Add(30 * time.Second)
	if sessionDeadline.Before(until) {
		until = sessionDeadline
	}
	if sourceDeadline != nil && sourceDeadline.Before(until) {
		until = *sourceDeadline
	}
	if !until.After(now) {
		return now, until, identity.ErrControlChanged
	}
	return now.UTC(), until.UTC(), nil
}
func humanWriteMaterial(f humanWriteFrame, scope, action, digest, source string) string {
	b, _ := json.Marshal([]any{"human-write-v1", f.family, f.a.ID, f.session.ID, f.session.Generation, scope, action, digest, source})
	return string(b)
}
func humanWriteApproval(material string, issued, end int64) (string, error) {
	humanWriteSigning.Do(func() { _, humanWriteSigning.err = rand.Read(humanWriteSigning.key[:]) })
	if humanWriteSigning.err != nil {
		return "", identity.ErrControlUnavailable
	}
	h := hmac.New(sha256.New, humanWriteSigning.key[:])
	fmt.Fprintf(h, "%s:%d:%d", material, issued, end)
	return "hw1." + strconv.FormatInt(issued, 10) + "." + strconv.FormatInt(end, 10) + "." + hex.EncodeToString(h.Sum(nil)), nil
}
func humanWriteApprovalDeadline(material, token string, now time.Time) (time.Time, bool) {
	p := strings.Split(token, ".")
	if len(p) != 4 || p[0] != "hw1" {
		return time.Time{}, false
	}
	issued, e := strconv.ParseInt(p[1], 10, 64)
	if e != nil {
		return time.Time{}, false
	}
	end, e := strconv.ParseInt(p[2], 10, 64)
	if e != nil || issued > now.UnixNano() || end <= now.UnixNano() || end-issued <= 0 || end-issued > int64(30*time.Second) {
		return time.Time{}, false
	}
	want, e := humanWriteApproval(material, issued, end)
	return time.Unix(0, end).UTC(), e == nil && hmac.Equal([]byte(want), []byte(token))
}
func (s *Store) humanWritePreview(ctx context.Context, f humanWriteFrame, scope, action, digest, source string, deadline *time.Time) (identity.HumanControlPreview, error) {
	now, until, e := s.humanWriteNow(ctx, f, deadline)
	if e != nil {
		return identity.HumanControlPreview{}, e
	}
	token, e := humanWriteApproval(humanWriteMaterial(f, scope, action, digest, source), now.UnixNano(), until.UnixNano())
	return identity.HumanControlPreview{OperationID: f.op, Action: action, PayloadDigest: digest, ObservedAt: now, ExpiresAt: until, Approval: token}, e
}
func (s *Store) humanWriteApprove(ctx context.Context, f humanWriteFrame, scope, action, digest, source, approval string, sourceDeadline *time.Time) (time.Time, error) {
	now, until, e := s.humanWriteNow(ctx, f, sourceDeadline)
	if e != nil {
		return time.Time{}, e
	}
	end, valid := humanWriteApprovalDeadline(humanWriteMaterial(f, scope, action, digest, source), approval, now)
	if !valid || end.After(until) {
		return end, identity.ErrControlChanged
	}
	return end, nil
}
func humanWriteReceiptTx(ctx context.Context, f humanWriteFrame) (identity.HumanControlReceipt, error) {
	r := identity.HumanControlReceipt{OperationID: f.op, Family: f.family}
	var action, target, result, scope *string
	var digest []byte
	var committed time.Time
	e := f.tx.QueryRow(ctx, `SELECT status,applied_version,scope_id,action,target_id,result_id,payload_sha256,committed_at FROM human_write_control_operations WHERE actor_account_id=$1 AND family=$2 AND operation_id=$3`, f.a.ID, f.family, f.op).Scan(&r.Status, &r.AppliedVersion, &scope, &action, &target, &result, &digest, &committed)
	if e != nil {
		return r, e
	}
	if r.Status == "STOPPED" {
		return r, nil
	}
	if r.Status != "SUCCEEDED" || action == nil || len(digest) != 32 {
		return r, identity.ErrControlUnavailable
	}
	r.Action = *action
	if target != nil {
		r.TargetID = *target
	}
	if result != nil {
		r.ResultID = *result
	}
	if scope != nil {
		r.ScopeID = *scope
	}
	r.PayloadDigest = hex.EncodeToString(digest)
	at := committed.UTC()
	r.CommittedAt = &at
	return r, nil
}
func (s *Store) humanWriteReplay(ctx context.Context, f humanWriteFrame, scope, digest string) (identity.HumanControlReceipt, bool, error) {
	r, e := humanWriteReceiptTx(ctx, f)
	if errors.Is(e, pgx.ErrNoRows) {
		return r, false, nil
	}
	if e != nil {
		return r, true, identity.ErrControlUnavailable
	}
	if r.Status == "SUCCEEDED" && (r.ScopeID != scope || r.PayloadDigest != digest) {
		return r, true, identity.ErrControlChanged
	}
	e = s.humanWriteFinish(ctx, f)
	return r, true, e
}
func humanWriteSaveReceipt(ctx context.Context, f humanWriteFrame, scope, action, target, result, digest string, version int64) (identity.HumanControlReceipt, error) {
	raw, e := hex.DecodeString(digest)
	if e != nil || len(raw) != 32 {
		return identity.HumanControlReceipt{}, identity.ErrControlInvalid
	}
	var scopeArg, targetArg, resultArg any
	if scope != "" {
		scopeArg = scope
	}
	if target != "" {
		targetArg = target
	}
	if result != "" {
		resultArg = result
	}
	_, e = f.tx.Exec(ctx, `INSERT INTO human_write_control_operations(actor_account_id,family,operation_id,status,scope_id,action,target_id,result_id,applied_version,payload_sha256)VALUES($1,$2,$3,'SUCCEEDED',$4,$5,$6,$7,$8,$9)`, f.a.ID, f.family, f.op, scopeArg, action, targetArg, resultArg, version, raw)
	if e != nil {
		return identity.HumanControlReceipt{}, identity.ErrControlUnavailable
	}
	return humanWriteReceiptTx(ctx, f)
}
func (s *Store) humanWriteHistory(ctx context.Context, d [32]byte, a identity.Actor, family, op, scope string, stop bool) (identity.HumanControlReceipt, error) {
	ctx, f, e := s.humanWriteBegin(ctx, d, a, family, op)
	if e != nil {
		return identity.HumanControlReceipt{}, e
	}
	defer f.tx.Rollback(context.Background())
	r, e := humanWriteReceiptTx(ctx, f)
	if errors.Is(e, pgx.ErrNoRows) {
		if !stop {
			if e = s.humanWriteFinish(ctx, f); e != nil {
				return identity.HumanControlReceipt{}, e
			}
			return r, identity.ErrNotFound
		}
		if _, e = f.tx.Exec(ctx, `INSERT INTO human_write_control_operations(actor_account_id,family,operation_id,status)VALUES($1,$2,$3,'STOPPED')`, a.ID, family, op); e != nil {
			return r, identity.ErrControlUnavailable
		}
		r = identity.HumanControlReceipt{OperationID: op, Family: family, Status: "STOPPED"}
	} else if e != nil {
		return r, identity.ErrControlUnavailable
	}
	if r.Status == "SUCCEEDED" && r.ScopeID != scope {
		return identity.HumanControlReceipt{}, identity.ErrControlChanged
	}
	if e = s.humanWriteFinish(ctx, f); e != nil {
		return identity.HumanControlReceipt{}, e
	}
	return r, nil
}

type humanWriteProof struct {
	s          *Store
	d          [32]byte
	a          identity.Actor
	frame      identity.HumanControlFrame
	family, op string
	until      time.Time
	check      func(context.Context, humanWriteFrame) error
}

func (p *humanWriteProof) RevalidateHumanControl(ctx context.Context) error {
	ctx = identity.WithHumanControlFrame(ctx, p.d, p.a, p.frame)
	ctx, f, e := p.s.humanWriteBegin(ctx, p.d, p.a, p.family, p.op)
	if e != nil {
		return e
	}
	defer f.tx.Rollback(context.Background())
	return p.s.humanWriteFinish(ctx, f, humanWriteGateDeadline(ctx, f, p.until, func() error { return p.check(ctx, f) }))
}
func humanWriteAttachProof(s *Store, f humanWriteFrame, until time.Time, check func(context.Context, humanWriteFrame) error) identity.HumanControlProof {
	return &humanWriteProof{s, f.d, f.a, f.session, f.family, f.op, until, check}
}
func humanWriteHashJSON(v any) string {
	b, _ := json.Marshal(v)
	return identity.HumanControlTextHash(string(b))
}
func humanWriteGateDeadline(ctx context.Context, f humanWriteFrame, deadline time.Time, check func() error) func() error {
	return func() error {
		current := func() error {
			var now time.Time
			if e := f.tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
				return identity.ErrControlUnavailable
			}
			if !deadline.After(now) {
				return identity.ErrControlChanged
			}
			return nil
		}
		if e := current(); e != nil {
			return e
		}
		if e := check(); e != nil {
			return e
		}
		return current()
	}
}
