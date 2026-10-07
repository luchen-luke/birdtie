package postgres

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/businessconsole"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

const bizIdentityID = "be000000-0000-4000-8000-000000000001"
const bizIdentityOwner = "be000000-0000-4000-8000-000000000002"
const bizIdentityPrincipal = "be000000-0000-4000-8000-000000000003"
const bizIdentityAgent = "be000000-0000-4000-8000-000000000004"

type bizIdentityRow struct {
	values []any
	err    error
	after  func()
}

func (r bizIdentityRow) Scan(d ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(d) != len(r.values) {
		return errors.New("spy row column mismatch")
	}
	for i, v := range r.values {
		reflect.ValueOf(d[i]).Elem().Set(reflect.ValueOf(v).Convert(reflect.ValueOf(d[i]).Elem().Type()))
	}
	if r.after != nil {
		r.after()
	}
	return nil
}

type bizIdentityTx struct {
	pgx.Tx
	at                       time.Time
	exists                   bool
	status, role             string
	version                  int64
	current, session         bool
	commits, audits, inserts int
	log                      []string
	afterFinal               func()
}

func newBizIdentityTx() *bizIdentityTx {
	return &bizIdentityTx{at: time.Now().UTC(), status: "suspended", role: "owner", version: 2, current: true, session: true}
}
func (tx *bizIdentityTx) Exec(_ context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	tx.log = append(tx.log, q)
	if strings.Contains(q, "INSERT INTO business_console_audit_events") {
		tx.audits++
		if args[2] != "agent_provision" {
			return pgconn.CommandTag{}, errors.New("wrong audit")
		}
	}
	return pgconn.NewCommandTag("SELECT 1"), nil
}
func (tx *bizIdentityTx) QueryRow(_ context.Context, q string, args ...any) pgx.Row {
	tx.log = append(tx.log, q)
	row := func(v ...any) pgx.Row { return bizIdentityRow{values: v} }
	switch {
	case strings.Contains(q, "WITH stamp AS MATERIALIZED"):
		return bizIdentityRow{values: []any{tx.at.Add(time.Millisecond), tx.current, tx.session, "s1"}, after: tx.afterFinal}
	case strings.Contains(q, "FROM businesses"):
		return row(bizIdentityPrincipal, "合成商家", "active", "verified", "b1", tx.at)
	case strings.Contains(q, "FROM business_claim_controls"):
		return row(tx.version, "verified", "c1")
	case strings.Contains(q, "FROM accounts"):
		kind := "person"
		if args[0] == bizIdentityPrincipal {
			kind = "business"
		}
		return row(kind, "active", "a1")
	case strings.Contains(q, "FROM business_memberships"):
		return row(tx.role, "active", "m1")
	case strings.Contains(q, "INSERT INTO agents"):
		tx.exists = true
		tx.inserts++
		return row(bizIdentityAgent)
	case strings.Contains(q, "FROM agents"):
		if !tx.exists {
			return bizIdentityRow{err: pgx.ErrNoRows}
		}
		return row(bizIdentityAgent, tx.status, "g1")
	case strings.Contains(q, "FROM agent_profiles"):
		return row(bizIdentityAgent, "BUSINESS", bizIdentityPrincipal, int64(1), tx.at, tx.at, "p1")
	case strings.Contains(q, "FROM sessions"):
		if !tx.session {
			return bizIdentityRow{err: pgx.ErrNoRows}
		}
		return row("session-id")
	}
	return bizIdentityRow{err: errors.New("unexpected SQL")}
}
func (tx *bizIdentityTx) Commit(context.Context) error { tx.commits++; return nil }
func bizIdentityAccess() businessconsole.Access {
	return businessconsole.Access{SessionDigest: [32]byte{1}, ActingPersonID: bizIdentityOwner, BusinessID: bizIdentityID}
}
func TestBusinessAgentIdentityNativeCreateAndStableReuse(t *testing.T) {
	s := &Store{}
	v := int64(2)
	tx := newBizIdentityTx()
	out, e := s.businessIdentityTx(context.Background(), tx, bizIdentityAccess(), &v)
	if e != nil || out.Agent == nil || out.Agent.Status != "suspended" || tx.inserts != 1 || tx.audits != 1 || tx.commits != 1 {
		t.Fatalf("%+v %v %d %d", out, e, tx.inserts, tx.audits)
	}
	session, profile := -1, -1
	for i, q := range tx.log {
		if strings.Contains(q, "FOR SHARE") && strings.Contains(q, "FROM sessions") {
			session = i
		}
		if strings.Contains(q, "FROM agent_profiles") {
			profile = i
		}
	}
	if profile < 0 || session <= profile || tx.log[len(tx.log)-1] != businessIdentityFinalSQL {
		t.Fatal("resource/session/last clock order")
	}
	tx = newBizIdentityTx()
	tx.exists = true
	tx.status = "retired"
	out, e = s.businessIdentityTx(context.Background(), tx, bizIdentityAccess(), &v)
	if e != nil || out.Agent.Status != "retired" || tx.inserts != 0 || tx.audits != 0 {
		t.Fatal("stable retired identity must not resurrect/reset")
	}
}
func TestBusinessAgentIdentityNativeVersionAndRole(t *testing.T) {
	s := &Store{}
	v := int64(3)
	tx := newBizIdentityTx()
	_, e := s.businessIdentityTx(context.Background(), tx, bizIdentityAccess(), &v)
	if !errors.Is(e, businessconsole.ErrConflict) || tx.inserts != 0 || tx.commits != 0 {
		t.Fatal(e)
	}
	v = 2
	tx = newBizIdentityTx()
	tx.role = "member"
	_, e = s.businessIdentityTx(context.Background(), tx, bizIdentityAccess(), &v)
	if !errors.Is(e, businessconsole.ErrForbidden) || tx.inserts != 0 {
		t.Fatal(e)
	}
	v = 3
	tx = newBizIdentityTx()
	tx.role = "member"
	_, e = s.businessIdentityTx(context.Background(), tx, bizIdentityAccess(), &v)
	if !errors.Is(e, businessconsole.ErrForbidden) || tx.inserts != 0 {
		t.Fatal("permission must precede stale version", e)
	}
}
func TestBusinessAgentIdentityNativeFinalSourceAndSession(t *testing.T) {
	v := int64(2)
	for _, what := range []string{"source", "session", "cancel"} {
		tx := newBizIdentityTx()
		ctx, cancel := context.WithCancel(context.Background())
		if what == "source" {
			tx.current = false
		}
		if what == "session" {
			tx.session = false
		}
		if what == "cancel" {
			tx.afterFinal = cancel
		}
		_, e := (&Store{}).businessIdentityTx(ctx, tx, bizIdentityAccess(), &v)
		cancel()
		if e == nil || tx.commits != 0 {
			t.Fatal(what, e)
		}
		if what == "session" && !errors.Is(e, identity.ErrUnauthorized) {
			t.Fatal(e)
		}
	}
}
func TestBusinessAgentIdentityNativeUnconfiguredRead(t *testing.T) {
	tx := newBizIdentityTx()
	out, e := (&Store{}).businessIdentityTx(context.Background(), tx, bizIdentityAccess(), nil)
	if e != nil || out.Agent != nil || tx.inserts != 0 || tx.audits != 0 {
		t.Fatal(e)
	}
}
func TestBusinessAgentIdentityMigrationAdditiveAudit(t *testing.T) {
	old, e := os.ReadFile("../../migrations/070_business_claim_console.sql")
	if e != nil {
		t.Fatal(e)
	}
	up, _ := os.ReadFile("../../migrations/104_business_agent_identity.sql")
	down, _ := os.ReadFile("../../migrations/104_business_agent_identity.down.sql")
	for _, action := range []string{"claim_submit", "claim_approve", "claim_reject", "claim_revoke", "profile_submit", "profile_approve", "profile_reject", "profile_revoke", "venue_submit", "venue_approve", "venue_reject", "venue_revoke", "member_grant", "member_remove", "member_transfer_owner"} {
		for _, raw := range [][]byte{old, up, down} {
			if !strings.Contains(string(raw), "'"+action+"'") {
				t.Fatal(action)
			}
		}
	}
	if !strings.Contains(string(up), "'agent_provision'") || !strings.Contains(string(down), "RAISE EXCEPTION") || strings.Contains(string(up), "UPDATE agents") {
		t.Fatal("audit-only expansion")
	}
}
