package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	mp "github.com/birdtie/birdtie/apps/api/internal/agentmessagepolicy"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Trace pauses after a real PostgreSQL clock result was consumed; a separate
// real connection observes natural wall-clock expiry. It never fakes SQL rows,
// changes the native clock or supplies an authority/permission object.
type policyClockTrace struct {
	clockResult chan struct{}
	release     chan struct{}
	armed       bool
	skip        int
}
type policyQueryKey struct{}

func (p *policyClockTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, policyQueryKey{}, d.SQL)
}
func (p *policyClockTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	if !p.armed || ctx.Value(policyQueryKey{}) != "SELECT clock_timestamp()" {
		return
	}
	if p.skip > 0 {
		p.skip--
		return
	}
	p.armed = false
	close(p.clockResult)
	select {
	case <-p.release:
	case <-ctx.Done():
	}
}

type policyPGFixture struct {
	t              *testing.T
	ctx            context.Context
	pool, observer *pgxpool.Pool
	store          *Store
	access         agentprofile.PrivateAccess
	trace          *policyClockTrace
	agent          string
}

func policyPG(t *testing.T) *policyPGFixture {
	t.Helper()
	if os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" || os.Getenv("BIRDTIE_MESSAGE_POLICY_DATABASE") == "" {
		t.Skip("requires explicitly owned message-policy database runner")
	}
	cfg, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil || cfg.ConnConfig.Database != os.Getenv("BIRDTIE_MESSAGE_POLICY_DATABASE") || !regexp.MustCompile(`^birdtie_age042_policy_[0-9a-f]{12}$`).MatchString(cfg.ConnConfig.Database) || cfg.ConnConfig.Host != "127.0.0.1" || cfg.ConnConfig.Port != 55432 {
		t.Fatal("refuse non-owned/remote policy database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	observer, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(observer.Close)
	p := &policyClockTrace{clockResult: make(chan struct{}), release: make(chan struct{})}
	cfg.ConnConfig.Tracer = p
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(pool.Close)
	f := &policyPGFixture{t: t, ctx: ctx, pool: pool, observer: observer, store: New(pool, false), trace: p}
	var owner string
	if e = pool.QueryRow(ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),'person') RETURNING id`).Scan(&owner); e != nil {
		t.Fatal(e)
	}
	if e = pool.QueryRow(ctx, `INSERT INTO agents(agent_type,principal_account_id) VALUES('personal',$1) RETURNING id`, owner).Scan(&f.agent); e != nil {
		t.Fatal(e)
	}
	_, digest, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, `INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at) VALUES($1,$2,'test',statement_timestamp()+interval '1 hour',statement_timestamp()+interval '1 hour')`, owner, digest[:]); e != nil {
		t.Fatal(e)
	}
	f.access = agentprofile.PrivateAccess{SessionDigest: digest, WorkspacePrincipal: actorref.PrincipalRef{Type: actorref.Person, ID: owner}}
	return f
}
func (f *policyPGFixture) clock() time.Time {
	f.t.Helper()
	var n time.Time
	if e := f.observer.QueryRow(f.ctx, `SELECT clock_timestamp()`).Scan(&n); e != nil {
		f.t.Fatal(e)
	}
	return n.UTC()
}
func TestMessagePolicyNativeFinalClockAfterRecordedRead(t *testing.T) {
	for _, write := range []bool{false, true} {
		name := "GET_natural_expiry"
		if write {
			name = "PUT_natural_expiry_rolls_back"
		}
		t.Run(name, func(t *testing.T) {
			f := policyPG(t)
			if _, e := f.store.PutOwnMessagePolicy(f.ctx, f.access, mp.PutInput{ExpectedVersion: 0, IncomingRequests: mp.Screen, ExpiresAt: f.clock().Add(time.Hour)}); e != nil {
				t.Fatal("initial original policy", e)
			}
			until := f.clock().Add(time.Second)
			if !write {
				if _, e := f.pool.Exec(f.ctx, `UPDATE agent_message_request_policies SET native_revision=native_revision+1,expires_at=$2 WHERE owner_id=$1`, f.access.WorkspacePrincipal.ID, until); e != nil {
					t.Fatal(e)
				}
			}
			f.trace.armed = true
			if write {
				f.trace.skip = 1
			}
			type result struct {
				record mp.Record
				err    error
			}
			done := make(chan result, 1)
			go func() {
				var r mp.Record
				var e error
				if write {
					r, e = f.store.PutOwnMessagePolicy(f.ctx, f.access, mp.PutInput{ExpectedVersion: 1, IncomingRequests: mp.Block, ExpiresAt: until})
				} else {
					r, e = f.store.GetOwnMessagePolicy(f.ctx, f.access)
				}
				done <- result{r, e}
			}()
			select {
			case <-f.trace.clockResult:
			case <-f.ctx.Done():
				t.Fatal("native recorded clock not reached")
			}
			if _, e := f.observer.Exec(f.ctx, `SELECT pg_sleep(GREATEST(0,extract(epoch FROM ($1::timestamptz-clock_timestamp()))+0.03))`, until); e != nil {
				close(f.trace.release)
				t.Fatal(e)
			}
			close(f.trace.release)
			var out result
			select {
			case out = <-done:
			case <-f.ctx.Done():
				t.Fatal("native final clock writer did not return")
			}
			if write {
				var revision, audits int64
				if e := f.observer.QueryRow(f.ctx, `SELECT native_revision,(SELECT count(*) FROM audit_events WHERE actor_account_id=$1 AND purpose='human_message_policy_edit') FROM agent_message_request_policies WHERE owner_id=$1`, f.access.WorkspacePrincipal.ID).Scan(&revision, &audits); e != nil {
					t.Fatal(e)
				}
				if !errors.Is(out.err, mp.ErrChanged) || revision != 1 || audits != 1 {
					t.Fatalf("actual expired PUT err=%v revision=%d audits=%d wantchanged/original1/1", out.err, revision, audits)
				}
			} else if out.err == nil && (out.record.Status != "EXPIRED" || out.record.ObservedAt.Before(until)) {
				t.Fatal("actual GET returned ACTIVE from pre-expiry clock after native deadline")
			}
		})
	}
}

func (f *policyPGFixture) exec(sql string, args ...any) {
	f.t.Helper()
	if _, e := f.pool.Exec(f.ctx, sql, args...); e != nil {
		f.t.Fatal("owned original policy fixture SQL", e)
	}
}

func (f *policyPGFixture) put(version int64, mode mp.Disposition) mp.Record {
	f.t.Helper()
	r, e := f.store.PutOwnMessagePolicy(f.ctx, f.access, mp.PutInput{ExpectedVersion: version, IncomingRequests: mode, ExpiresAt: f.clock().Add(time.Hour)})
	if e != nil || !mp.ValidRecord(r, f.access.WorkspacePrincipal.ID) || r.NativeRevision != version+1 || r.IncomingRequests != mode || r.Status != "ACTIVE" {
		f.t.Fatalf("actual own CAS version=%d mode=%s record=%+v err=%v", version, mode, r, e)
	}
	return r
}

// Compare committed native rows and xmin, including the audit transaction.
// No cleanup deletes these rows; their owning runner removes its whole DB.
func (f *policyPGFixture) snapshot() string {
	f.t.Helper()
	var out string
	e := f.observer.QueryRow(f.ctx, `SELECT jsonb_build_object(
 'policy',(SELECT coalesce(jsonb_agg(jsonb_build_array(to_jsonb(p),p.xmin::text) ORDER BY p.agent_id),'[]'::jsonb) FROM agent_message_request_policies p WHERE p.owner_id=$1),
 'audit',(SELECT coalesce(jsonb_agg(jsonb_build_array(to_jsonb(a),a.xmin::text) ORDER BY a.id),'[]'::jsonb) FROM audit_events a WHERE a.actor_account_id=$1 AND a.purpose='human_message_policy_edit'),
 'requests',(SELECT coalesce(jsonb_agg(jsonb_build_array(to_jsonb(r),r.xmin::text) ORDER BY r.id),'[]'::jsonb) FROM connection_requests r WHERE r.sender_account_id=$1 OR r.recipient_account_id=$1),
 'bindings',(SELECT coalesce(jsonb_agg(jsonb_build_array(to_jsonb(b),b.xmin::text) ORDER BY b.request_id),'[]'::jsonb) FROM connection_request_policy_bindings b JOIN connection_requests r ON r.id=b.request_id WHERE r.sender_account_id=$1 OR r.recipient_account_id=$1))::text`, f.access.WorkspacePrincipal.ID).Scan(&out)
	if e != nil {
		f.t.Fatal(e)
	}
	return out
}

func TestMessagePolicyNativeOwnCASAndPersistence(t *testing.T) {
	f := policyPG(t)
	r, e := f.store.GetOwnMessagePolicy(f.ctx, f.access)
	if e != nil || !mp.ValidRecord(r, f.access.WorkspacePrincipal.ID) || r.Configured || r.NativeRevision != 0 || r.Status != "UNCONFIGURED" {
		t.Fatal("actual unconfigured Person policy", r, e)
	}
	for i, mode := range []mp.Disposition{mp.Request, mp.Screen, mp.Block} {
		r = f.put(int64(i), mode)
	}
	// Identical content is a new exact CAS revision, never a silent no-op.
	until := *r.ExpiresAt
	in := mp.PutInput{ExpectedVersion: 3, IncomingRequests: mp.Block, ExpiresAt: until}
	r, e = f.store.PutOwnMessagePolicy(f.ctx, f.access, in)
	if e != nil || r.NativeRevision != 4 || r.IncomingRequests != mp.Block || !r.ExpiresAt.Equal(until) {
		t.Fatal("same content must advance original CAS", r, e)
	}
	before := f.snapshot()
	if _, e = f.store.PutOwnMessagePolicy(f.ctx, f.access, in); !errors.Is(e, mp.ErrChanged) || f.snapshot() != before {
		t.Fatal("stale original CAS changed policy/audit rows or xmin", e)
	}
	type result struct {
		record mp.Record
		err    error
	}
	start, done := make(chan struct{}), make(chan result, 2)
	for _, mode := range []mp.Disposition{mp.Request, mp.Screen} {
		input := mp.PutInput{ExpectedVersion: 4, IncomingRequests: mode, ExpiresAt: until}
		go func() {
			<-start
			r, e := f.store.PutOwnMessagePolicy(f.ctx, f.access, input)
			done <- result{r, e}
		}()
	}
	close(start)
	wins, changed := 0, 0
	var winner mp.Record
	for i := 0; i < 2; i++ {
		select {
		case out := <-done:
			if out.err == nil {
				wins++
				winner = out.record
			} else if errors.Is(out.err, mp.ErrChanged) {
				changed++
			} else {
				t.Fatal("real concurrent CAS unexpected error", out.err)
			}
		case <-f.ctx.Done():
			t.Fatal("real concurrent CAS did not finish")
		}
	}
	var revision, audits int64
	if e = f.observer.QueryRow(f.ctx, `SELECT native_revision,(SELECT count(*) FROM audit_events WHERE actor_account_id=$1 AND purpose='human_message_policy_edit') FROM agent_message_request_policies WHERE owner_id=$1`, f.access.WorkspacePrincipal.ID).Scan(&revision, &audits); e != nil || wins != 1 || changed != 1 || revision != 5 || audits != 5 || winner.NativeRevision != 5 {
		t.Fatalf("exactly one native CAS wins: wins=%d conflicts=%d revision=%d audits=%d err=%v", wins, changed, revision, audits, e)
	}
	// New pool and Store prove persisted original data, not an in-memory cache.
	p, e := pgxpool.New(f.ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	fresh := New(p, false)
	before = f.snapshot()
	r, e = fresh.GetOwnMessagePolicy(f.ctx, f.access)
	if e != nil || r.NativeRevision != winner.NativeRevision || r.IncomingRequests != winner.IncomingRequests || !r.ExpiresAt.Equal(*winner.ExpiresAt) || fresh.ValidateOwnMessagePolicyResponse(f.ctx, f.access, r) != nil || f.snapshot() != before {
		t.Fatal("new actual pool/Store lost or rewrote original policy", r, e)
	}
}

func TestMessagePolicyNativeAuditRollback(t *testing.T) {
	f := policyPG(t)
	f.put(0, mp.Request)
	var marker string
	if e := f.pool.QueryRow(f.ctx, `SELECT gen_random_uuid()::text`).Scan(&marker); e != nil {
		t.Fatal(e)
	}
	name := "owned_policy_fault_" + strings.ReplaceAll(marker, "-", "")
	function := pgx.Identifier{"public", name}.Sanitize()
	trigger := pgx.Identifier{name}.Sanitize()
	// This new test-only trigger matches one synthetic owner and one purpose.
	// Existing audit functions, triggers and guards remain installed unchanged.
	f.exec(fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.actor_account_id='%s'::uuid AND NEW.purpose='human_message_policy_edit' THEN RAISE EXCEPTION 'owned policy audit fault' USING ERRCODE='P0001';END IF;RETURN NEW;END $$`, function, f.access.WorkspacePrincipal.ID))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, e := f.observer.Exec(ctx, `DROP TRIGGER IF EXISTS `+trigger+` ON public.audit_events`); e != nil {
			t.Error("drop this owned policy fault trigger", e)
		}
		if _, e := f.observer.Exec(ctx, `DROP FUNCTION `+function+`()`); e != nil {
			t.Error("drop this owned policy fault function", e)
		}
	})
	f.exec(`CREATE TRIGGER ` + trigger + ` BEFORE INSERT ON public.audit_events FOR EACH ROW EXECUTE FUNCTION ` + function + `()`)
	before := f.snapshot()
	_, e := f.store.PutOwnMessagePolicy(f.ctx, f.access, mp.PutInput{ExpectedVersion: 1, IncomingRequests: mp.Screen, ExpiresAt: f.clock().Add(time.Hour)})
	if !errors.Is(e, mp.ErrUnavailable) || f.snapshot() != before {
		t.Fatal("native audit failure committed policy revision/audit or changed xmin", e)
	}
}

func policyNativeRejectSQL(t *testing.T, f *policyPGFixture, code, sql string, args ...any) {
	t.Helper()
	before := f.snapshot()
	tx, e := f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	_, rejected := tx.Exec(f.ctx, sql, args...)
	if e = tx.Rollback(f.ctx); e != nil {
		t.Fatal("same native rejection transaction rollback", e)
	}
	var pe *pgconn.PgError
	if !errors.As(rejected, &pe) || pe.Code != code || f.snapshot() != before {
		t.Fatalf("original 098 rejection code=%s err=%v committed rows preserved=%t", code, rejected, f.snapshot() == before)
	}
}

func TestMessagePolicyNative098Constraints(t *testing.T) {
	f, peer := policyPG(t), policyPG(t)
	// Positive finite exact binding precedes every malformed original SQL case.
	f.put(0, mp.Screen)
	for _, c := range []struct{ name, set string }{
		{"CAS_no_advance", "incoming_requests='REQUEST'"},
		{"CAS_skip_revision", "native_revision=native_revision+2"},
		{"immutable_agent", "native_revision=native_revision+1,agent_id=$2"},
		{"immutable_owner", "native_revision=native_revision+1,owner_id=$3"},
		{"Person_owner_type", "native_revision=native_revision+1,owner_type='ORGANIZATION'"},
		{"no_incoming_ALLOW", "native_revision=native_revision+1,incoming_requests='ALLOW'"},
		{"finite_from", "native_revision=native_revision+1,valid_from='-infinity'::timestamptz"},
		{"finite_expiry", "native_revision=native_revision+1,expires_at='infinity'::timestamptz"},
		{"positive_lifetime", "native_revision=native_revision+1,expires_at=valid_from"},
		{"maximum_30_days", "native_revision=native_revision+1,expires_at=valid_from+interval '30 days 1 microsecond'"},
	} {
		t.Run(c.name, func(t *testing.T) {
			// Reference all typed parameters even where the mutation does not use them.
			policyNativeRejectSQL(t, f, "23514", `UPDATE agent_message_request_policies SET `+c.set+` WHERE agent_id=$1 AND $2::uuid IS NOT NULL AND $3::uuid IS NOT NULL`, f.agent, peer.agent, peer.access.WorkspacePrincipal.ID)
		})
	}
	insert := `INSERT INTO agent_message_request_policies(agent_id,owner_id,owner_type,native_revision,incoming_requests,valid_from,expires_at) VALUES($1,$2,'PERSON',$3,'SCREEN',clock_timestamp(),clock_timestamp()+interval '1 hour')`
	policyNativeRejectSQL(t, peer, "23514", insert, peer.agent, peer.access.WorkspacePrincipal.ID, int64(2))
	policyNativeRejectSQL(t, peer, "23514", insert, peer.agent, f.access.WorkspacePrincipal.ID, int64(1))
	// Native input rejects must likewise leave original committed rows untouched.
	for _, c := range []struct {
		name string
		in   mp.PutInput
	}{
		{"ALLOW", mp.PutInput{ExpectedVersion: 1, IncomingRequests: mp.Allow, ExpiresAt: f.clock().Add(time.Hour)}},
		{"expired", mp.PutInput{ExpectedVersion: 1, IncomingRequests: mp.Screen, ExpiresAt: f.clock().Add(-time.Second)}},
		{"beyond_30_days", mp.PutInput{ExpectedVersion: 1, IncomingRequests: mp.Screen, ExpiresAt: f.clock().Add(31 * 24 * time.Hour)}},
	} {
		t.Run("input_"+c.name, func(t *testing.T) {
			before := f.snapshot()
			if _, e := f.store.PutOwnMessagePolicy(f.ctx, f.access, c.in); !errors.Is(e, mp.ErrInvalid) || before != f.snapshot() {
				t.Fatal("invalid native own input wrote original rows", e)
			}
		})
	}
	for _, p := range []*policyPGFixture{f, peer} {
		p.exec(`INSERT INTO user_profiles(account_id,display_name,visibility) VALUES($1,'合成098守卫','public')`, p.access.WorkspacePrincipal.ID)
	}
	// The request/binding is produced by the original Current writer, not SQL.
	a := ea.Access{Actor: identity.Actor{ID: peer.access.WorkspacePrincipal.ID, AccountType: "person"}, SessionDigest: peer.access.SessionDigest}
	r, e := peer.store.CreateFriendRequestCurrent(peer.ctx, a, f.access.WorkspacePrincipal.ID, "明确合成098待审申请", "", nil)
	if e != nil || r.ID == "" || r.PolicyDisposition != "SCREEN" || r.ScreeningStatus != "PENDING_REVIEW" {
		t.Fatal("original native same SCREEN Request", r, e)
	}
	var binding string
	if e = f.pool.QueryRow(f.ctx, `SELECT disposition FROM connection_request_policy_bindings WHERE request_id=$1 AND recipient_id=$2`, r.ID, f.access.WorkspacePrincipal.ID).Scan(&binding); e != nil || binding != "SCREEN" {
		t.Fatal("same original request binding missing", binding, e)
	}
	policyNativeRejectSQL(t, f, "23514", `UPDATE connection_request_policy_bindings SET disposition='REQUEST' WHERE request_id=$1`, r.ID)
	policyNativeRejectSQL(t, f, "23514", `INSERT INTO connection_request_policy_bindings(request_id,recipient_id,disposition,policy_version,observed_at) VALUES($1,$2,'SCREEN',repeat('a',64),clock_timestamp())`, r.ID, peer.access.WorkspacePrincipal.ID)
	// Use another ORIGINAL pending Request with no existing annotation so its
	// field checks cannot be mistaken for a duplicate-PK rejection.
	other := policyPG(t)
	other.exec(`INSERT INTO user_profiles(account_id,display_name,visibility) VALUES($1,'合成098字段校验','public')`, other.access.WorkspacePrincipal.ID)
	from := ea.Access{Actor: identity.Actor{ID: f.access.WorkspacePrincipal.ID, AccountType: "person"}, SessionDigest: f.access.SessionDigest}
	unannotated, e := f.store.CreateFriendRequestCurrent(f.ctx, from, other.access.WorkspacePrincipal.ID, "明确合成未配置策略申请", "", nil)
	if e != nil || unannotated.ID == "" || unannotated.PolicyDisposition != "" {
		t.Fatal("original unconfigured pending Request", unannotated, e)
	}
	current, e := f.store.ReadMessagePolicyDecision(f.ctx, f.access, other.access.WorkspacePrincipal.ID)
	if e != nil || !mp.ValidSourceVersion(current.SourceVersion) {
		t.Fatal("actual original routing source version", e)
	}
	for _, c := range []struct{ name, disposition, version, observed string }{
		{"finite_observed_at", "SCREEN", current.SourceVersion, "'infinity'::timestamptz"},
		{"no_binding_BLOCK", "BLOCK", current.SourceVersion, "clock_timestamp()"},
		{"binding_lowercase_version", "SCREEN", strings.Repeat("A", 64), "clock_timestamp()"},
	} {
		t.Run(c.name, func(t *testing.T) {
			policyNativeRejectSQL(t, f, "23514", `INSERT INTO connection_request_policy_bindings(request_id,recipient_id,disposition,policy_version,observed_at) VALUES($1,$2,$3,$4,`+c.observed+`)`, unannotated.ID, other.access.WorkspacePrincipal.ID, c.disposition, c.version)
		})
	}
}

func TestMessagePolicyNativeResponseProofs(t *testing.T) {
	f, peer := policyPG(t), policyPG(t)
	for _, p := range []*policyPGFixture{f, peer} {
		p.exec(`INSERT INTO user_profiles(account_id,display_name,visibility) VALUES($1,'合成原生策略凭据','public')`, p.access.WorkspacePrincipal.ID)
	}
	f.put(0, mp.Request)
	peer.put(0, mp.Screen)
	own, e := f.store.GetOwnMessagePolicy(f.ctx, f.access)
	if e != nil || f.store.ValidateOwnMessagePolicyResponse(f.ctx, f.access, own) != nil {
		t.Fatal("positive original Record proof", e)
	}
	decision, e := f.store.ReadMessagePolicyDecision(f.ctx, f.access, peer.access.WorkspacePrincipal.ID)
	if e != nil || !mp.ValidDecision(decision) || decision.Disposition != mp.Screen || f.store.ValidateMessagePolicyDecisionResponse(f.ctx, f.access, peer.access.WorkspacePrincipal.ID, decision) != nil {
		t.Fatal("positive original Decision proof", decision, e)
	}
	_, digest, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	f.exec(`INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at) VALUES($1,$2,'test',statement_timestamp()+interval '1 hour',statement_timestamp()+interval '1 hour')`, f.access.WorkspacePrincipal.ID, digest[:])
	otherSession := f.access
	otherSession.SessionDigest = digest
	// A second live own Session is real, but cannot reuse the first proof.
	if _, e = f.store.GetOwnMessagePolicy(f.ctx, otherSession); e != nil {
		t.Fatal("second real own Session invalid", e)
	}
	for _, kind := range []string{"Record", "Decision"} {
		t.Run(kind, func(t *testing.T) {
			for _, mode := range []string{"json_roundtrip", "mutated_wire", "foreign_store", "foreign_session", "foreign_owner", "nil_context", "cancelled_context"} {
				t.Run(mode, func(t *testing.T) {
					ctx, access, store := f.ctx, f.access, f.store
					r, d := own, decision
					switch mode {
					case "json_roundtrip":
						var raw []byte
						var e error
						if kind == "Record" {
							raw, e = json.Marshal(r)
							r = mp.Record{}
							if e == nil {
								e = json.Unmarshal(raw, &r)
							}
						} else {
							raw, e = json.Marshal(d)
							d = mp.Decision{}
							if e == nil {
								e = json.Unmarshal(raw, &d)
							}
						}
						if e != nil || strings.Contains(string(raw), "ResponseProof") || strings.Contains(string(raw), "SessionDigest") || (kind == "Record" && r.ResponseProof != nil) || (kind == "Decision" && d.ResponseProof != nil) {
							t.Fatal("private native proof appeared on wire or survived JSON", e)
						}
					case "mutated_wire":
						r.IncomingRequests = mp.Screen
						d.Disposition = mp.Request
					case "foreign_store":
						store = New(f.pool, false)
					case "foreign_session":
						access = otherSession
					case "foreign_owner":
						access = peer.access
					case "nil_context":
						ctx = nil
					case "cancelled_context":
						var cancel context.CancelFunc
						ctx, cancel = context.WithCancel(f.ctx)
						cancel()
					}
					before := f.snapshot()
					var rejected error
					if kind == "Record" {
						rejected = store.ValidateOwnMessagePolicyResponse(ctx, access, r)
					} else {
						rejected = store.ValidateMessagePolicyDecisionResponse(ctx, access, peer.access.WorkspacePrincipal.ID, d)
					}
					if !errors.Is(rejected, mp.ErrUnavailable) || before != f.snapshot() {
						t.Fatal("unusable native proof accepted or mutated original rows", rejected)
					}
				})
			}
		})
	}
	t.Run("decision_foreign_peer", func(t *testing.T) {
		if e := f.store.ValidateMessagePolicyDecisionResponse(f.ctx, f.access, f.access.WorkspacePrincipal.ID, decision); !errors.Is(e, mp.ErrUnavailable) {
			t.Fatal("native Decision reused for a different original peer", e)
		}
	})
	t.Run("nil_reader_contexts", func(t *testing.T) {
		if _, e := f.store.GetOwnMessagePolicy(nil, f.access); !errors.Is(e, mp.ErrUnavailable) {
			t.Fatal("nil own context", e)
		}
		if _, e := f.store.PutOwnMessagePolicy(nil, f.access, mp.PutInput{ExpectedVersion: 1, IncomingRequests: mp.Screen, ExpiresAt: f.clock().Add(time.Hour)}); !errors.Is(e, mp.ErrUnavailable) {
			t.Fatal("nil put context", e)
		}
		if _, e := f.store.ReadMessagePolicyDecision(nil, f.access, peer.access.WorkspacePrincipal.ID); !errors.Is(e, mp.ErrUnavailable) {
			t.Fatal("nil decision context", e)
		}
	})
	t.Run("actual_single_connection_pool", func(t *testing.T) {
		cfg, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
		if e != nil {
			t.Fatal(e)
		}
		cfg.MaxConns = 1
		cfg.ConnConfig.Tracer = nil
		p, e := pgxpool.NewWithConfig(f.ctx, cfg)
		if e != nil {
			t.Fatal(e)
		}
		defer p.Close()
		ctx, cancel := context.WithTimeout(f.ctx, 3*time.Second)
		defer cancel()
		one := New(p, false)
		r, e := one.GetOwnMessagePolicy(ctx, f.access)
		if e != nil || one.ValidateOwnMessagePolicyResponse(ctx, f.access, r) != nil {
			t.Fatal("own validator retained a pool connection across another reader", e)
		}
		d, e := one.ReadMessagePolicyDecision(ctx, f.access, peer.access.WorkspacePrincipal.ID)
		if e != nil || one.ValidateMessagePolicyDecisionResponse(ctx, f.access, peer.access.WorkspacePrincipal.ID, d) != nil {
			t.Fatal("decision validator retained a pool connection across another reader", e)
		}
	})
}

func TestMessagePolicyNativeFutureValidFromResponseLease(t *testing.T) {
	f, peer := policyPG(t), policyPG(t)
	for _, p := range []*policyPGFixture{f, peer} {
		p.exec(`INSERT INTO user_profiles(account_id,display_name,visibility) VALUES($1,'合成未来策略起点','public')`, p.access.WorkspacePrincipal.ID)
	}
	f.put(0, mp.Screen)
	from := f.clock().Add(1500 * time.Millisecond)
	// Legal original 098 finite future interval, exact revision one -> two.
	f.exec(`UPDATE agent_message_request_policies SET native_revision=native_revision+1,valid_from=$2,expires_at=$2::timestamptz+interval '1 hour' WHERE owner_id=$1`, f.access.WorkspacePrincipal.ID, from)
	r, e := f.store.GetOwnMessagePolicy(f.ctx, f.access)
	if e != nil || !mp.ValidRecord(r, f.access.WorkspacePrincipal.ID) || r.Status != "EXPIRED" || r.NativeRevision != 2 || !r.ValidFrom.Equal(from) || f.store.ValidateOwnMessagePolicyResponse(f.ctx, f.access, r) != nil {
		t.Fatal("legal before-validFrom native Record", r, e)
	}
	d, e := peer.store.ReadMessagePolicyDecision(peer.ctx, peer.access, f.access.WorkspacePrincipal.ID)
	if e != nil || !mp.ValidDecision(d) || d.Disposition != mp.Block || peer.store.ValidateMessagePolicyDecisionResponse(peer.ctx, peer.access, f.access.WorkspacePrincipal.ID, d) != nil {
		t.Fatal("legal before-validFrom native BLOCK", d, e)
	}
	before := f.snapshot()
	if _, e := f.observer.Exec(f.ctx, `SELECT pg_sleep(GREATEST(0,extract(epoch FROM ($1::timestamptz-clock_timestamp()))+0.03))`, from); e != nil {
		t.Fatal(e)
	}
	if e := f.store.ValidateOwnMessagePolicyResponse(f.ctx, f.access, r); !errors.Is(e, mp.ErrChanged) {
		t.Fatal("old EXPIRED response survived its real valid_from boundary", e)
	}
	if e := peer.store.ValidateMessagePolicyDecisionResponse(peer.ctx, peer.access, f.access.WorkspacePrincipal.ID, d); !errors.Is(e, mp.ErrChanged) {
		t.Fatal("old BLOCK response survived its real valid_from boundary", e)
	}
	fresh, e := f.store.GetOwnMessagePolicy(f.ctx, f.access)
	if e != nil || fresh.Status != "ACTIVE" || fresh.NativeRevision != 2 {
		t.Fatal("same persisted policy did not become ACTIVE", fresh, e)
	}
	current, e := peer.store.ReadMessagePolicyDecision(peer.ctx, peer.access, f.access.WorkspacePrincipal.ID)
	if e != nil || current.Disposition != mp.Screen || !mp.ValidDecision(current) || before != f.snapshot() {
		t.Fatal("fresh SCREEN/current read changed stored source", current, e)
	}
}

// Child fixtures deliberately do not bypass policyPG's parent-name guard.
// Only the unchanged ownedMigrationDatabase helper creates/drops the child.
func policyNativeChildFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) *policyPGFixture {
	t.Helper()
	f := &policyPGFixture{t: t, ctx: ctx, pool: pool, observer: pool, store: New(pool, false)}
	var owner string
	if e := pool.QueryRow(ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),'person') RETURNING id`).Scan(&owner); e != nil {
		t.Fatal(e)
	}
	if e := pool.QueryRow(ctx, `INSERT INTO agents(agent_type,principal_account_id) VALUES('personal',$1) RETURNING id`, owner).Scan(&f.agent); e != nil {
		t.Fatal(e)
	}
	_, digest, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, `INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at) VALUES($1,$2,'test',statement_timestamp()+interval '1 hour',statement_timestamp()+interval '1 hour')`, owner, digest[:]); e != nil {
		t.Fatal(e)
	}
	f.access = agentprofile.PrivateAccess{SessionDigest: digest, WorkspacePrincipal: actorref.PrincipalRef{Type: actorref.Person, ID: owner}}
	f.exec(`INSERT INTO user_profiles(account_id,display_name,visibility) VALUES($1,'独占迁移合成人物','public')`, owner)
	return f
}

// Complete original public rows and physical xmin; no evidence output side path.
func policyNativeAllRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool) map[string]string {
	t.Helper()
	rows, e := pool.Query(ctx, `SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename`)
	if e != nil {
		t.Fatal(e)
	}
	var names []string
	for rows.Next() {
		var name string
		if e = rows.Scan(&name); e != nil {
			rows.Close()
			t.Fatal(e)
		}
		names = append(names, name)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		t.Fatal(e)
	}
	out := make(map[string]string, len(names))
	for _, name := range names {
		var raw string
		sql := `SELECT coalesce(jsonb_agg(jsonb_build_object('row',to_jsonb(r),'xmin',r.xmin::text) ORDER BY to_jsonb(r)::text,r.xmin::text),'[]'::jsonb)::text FROM ` + pgx.Identifier{"public", name}.Sanitize() + ` r`
		if e := pool.QueryRow(ctx, sql).Scan(&raw); e != nil {
			t.Fatal("original rows/xmin", name, e)
		}
		out[name] = raw
	}
	return out
}

// Catalog definitions exclude volatile object OIDs, while retaining original
// columns, constraints, indexes, triggers, functions, ownership, RLS and ACLs.
func policyNativeCatalog(t *testing.T, ctx context.Context, pool *pgxpool.Pool, include098 bool) string {
	t.Helper()
	var out string
	e := pool.QueryRow(ctx, `WITH old_tables AS MATERIALIZED(SELECT c.oid FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND ($1::boolean OR c.relname NOT IN('agent_message_request_policies','connection_request_policy_bindings')))
 SELECT jsonb_build_object(
 'functions',(SELECT coalesce(jsonb_agg(jsonb_build_object('name',p.proname,'args',pg_get_function_identity_arguments(p.oid),'definition',pg_get_functiondef(p.oid),'owner',pg_get_userbyid(p.proowner),'acl',p.proacl) ORDER BY p.proname,pg_get_function_identity_arguments(p.oid)),'[]') FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='public' AND p.prokind='f' AND ($1::boolean OR p.proname NOT IN('birdtie_guard_message_request_policy','birdtie_guard_message_request_binding'))),
 'triggers',(SELECT coalesce(jsonb_agg(jsonb_build_object('table',c.relname,'name',t.tgname,'definition',pg_get_triggerdef(t.oid),'enabled',t.tgenabled) ORDER BY c.relname,t.tgname),'[]') FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid WHERE c.oid IN(SELECT oid FROM old_tables) AND NOT t.tgisinternal),
 'constraints',(SELECT coalesce(jsonb_agg(jsonb_build_object('table',r.relname,'name',c.conname,'definition',pg_get_constraintdef(c.oid)) ORDER BY r.relname,c.conname),'[]') FROM pg_constraint c JOIN pg_class r ON r.oid=c.conrelid WHERE r.oid IN(SELECT oid FROM old_tables)),
 'columns',(SELECT coalesce(jsonb_agg(jsonb_build_object('table',c.relname,'name',a.attname,'ordinal',a.attnum,'type',format_type(a.atttypid,a.atttypmod),'notNull',a.attnotnull,'default',pg_get_expr(d.adbin,d.adrelid),'identity',a.attidentity,'generated',a.attgenerated) ORDER BY c.relname,a.attnum),'[]') FROM pg_attribute a JOIN pg_class c ON c.oid=a.attrelid LEFT JOIN pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum WHERE c.oid IN(SELECT oid FROM old_tables) AND c.relkind IN('r','p','v','m') AND a.attnum>0 AND NOT a.attisdropped),
 'indexes',(SELECT coalesce(jsonb_agg(jsonb_build_object('table',t.relname,'name',i.relname,'definition',pg_get_indexdef(x.indexrelid),'valid',x.indisvalid,'ready',x.indisready) ORDER BY t.relname,i.relname),'[]') FROM pg_index x JOIN pg_class t ON t.oid=x.indrelid JOIN pg_class i ON i.oid=x.indexrelid WHERE t.oid IN(SELECT oid FROM old_tables)),
 'tables',(SELECT coalesce(jsonb_agg(jsonb_build_object('name',c.relname,'kind',c.relkind,'owner',pg_get_userbyid(c.relowner),'rls',c.relrowsecurity,'forceRLS',c.relforcerowsecurity,'privileges',c.relacl) ORDER BY c.relname),'[]') FROM pg_class c WHERE c.oid IN(SELECT oid FROM old_tables) AND c.relkind IN('r','p','v','m','S')),
 'policies',(SELECT coalesce(jsonb_agg(to_jsonb(p) ORDER BY tablename,policyname),'[]') FROM pg_policies p WHERE schemaname='public' AND ($1::boolean OR tablename NOT IN('agent_message_request_policies','connection_request_policy_bindings'))))::text`, include098).Scan(&out)
	if e != nil {
		t.Fatal("actual native migration catalog", e)
	}
	return out
}

func policyNativeMigration(t *testing.T, ctx context.Context, pool *pgxpool.Pool, name string, wantSuccess bool) {
	t.Helper()
	raw, e := os.ReadFile(filepath.Join("..", "..", "migrations", name))
	if e != nil {
		t.Fatal(e)
	}
	c, e := pool.Acquire(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Release()
	_, outcome := c.Exec(ctx, string(raw))
	if outcome != nil {
		if _, e := c.Exec(ctx, `ROLLBACK`); e != nil {
			t.Fatal("same migration connection rollback", e)
		}
	}
	if wantSuccess && outcome != nil {
		t.Fatal("actual original migration", name, outcome)
	}
	if !wantSuccess {
		var pe *pgconn.PgError
		if !errors.As(outcome, &pe) || pe.Code != "P0001" || !strings.Contains(pe.Message, "098 down refuses") {
			t.Fatal("original 098 used history was not protected", outcome)
		}
	}
}

func TestMessagePolicyNative098MigrationRetention(t *testing.T) {
	parent := policyPG(t) // Strict parent ownership/loopback guard first.
	parentBefore := parent.snapshot()
	var childName, childOwner, childMarker string
	var childOID uint32
	// Registered before the helper: LIFO runs this after its whole-child drop.
	t.Cleanup(func() {
		if childName == "" {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		var remaining int
		if e := parent.observer.QueryRow(ctx, `SELECT count(*) FROM pg_database WHERE datname=$1 OR oid=$2`, childName, childOID).Scan(&remaining); e != nil || remaining != 0 {
			t.Error("owned child was not completely removed", childName, childOID, remaining, e)
		}
		if got := parent.snapshot(); got != parentBefore {
			t.Error("child migration changed original parent policy/request/audit rows/xmin")
		}
		t.Logf("OWNED_098_CHILD_CLEANUP name=%s oid=%d owner=%s marker=%s remaining=%d parent_rows_preserved=%t", childName, childOID, childOwner, childMarker, remaining, parent.snapshot() == parentBefore)
	})
	ownedMigrationDatabase(t) // Unmodified actual 001..117 helper and dev seeds.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	pool, e := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(pool.Close) // Close before the helper's database drop.
	var currentRole, name string
	if e = pool.QueryRow(ctx, `SELECT current_database(),d.oid,pg_get_userbyid(d.datdba),current_user FROM pg_database d WHERE d.datname=current_database()`).Scan(&name, &childOID, &childOwner, &currentRole); e != nil || !regexp.MustCompile(`^birdtie_owned_migration_[0-9a-f]{24}$`).MatchString(name) || name == os.Getenv("BIRDTIE_MESSAGE_POLICY_DATABASE") || childOwner != currentRole {
		t.Fatal("independent native child ownership", name, childOID, childOwner, e)
	}
	childName = name
	if e = pool.QueryRow(ctx, `SELECT 'AGE042-owned-policy-'||gen_random_uuid()::text`).Scan(&childMarker); e != nil {
		t.Fatal(e)
	}
	// Marker belongs only to the successfully created random child identifier.
	if _, e = pool.Exec(ctx, `COMMENT ON DATABASE `+pgx.Identifier{childName}.Sanitize()+` IS '`+childMarker+`'`); e != nil {
		t.Fatal(e)
	}
	var marker string
	if e = pool.QueryRow(ctx, `SELECT shobj_description(oid,'pg_database') FROM pg_database WHERE oid=$1 AND datname=$2`, childOID, childName).Scan(&marker); e != nil || marker != childMarker {
		t.Fatal("native child marker not recorded", e)
	}
	t.Logf("OWNED_098_CHILD name=%s oid=%d owner=%s marker=%s parent=%s", childName, childOID, childOwner, marker, os.Getenv("BIRDTIE_MESSAGE_POLICY_DATABASE"))
	f := policyNativeChildFixture(t, ctx, pool)
	before, oldCatalog := policyNativeAllRows(t, ctx, pool), policyNativeCatalog(t, ctx, pool, false)
	if before["agent_message_request_policies"] != "[]" || before["connection_request_policy_bindings"] != "[]" {
		t.Fatal("new native child 098 was already used")
	}
	policyNativeMigration(t, ctx, pool, "098_agent_message_request_policy.down.sql", true)
	downRows := policyNativeAllRows(t, ctx, pool)
	oldRows := make(map[string]string, len(before)-2)
	for name, raw := range before {
		if name != "agent_message_request_policies" && name != "connection_request_policy_bindings" {
			oldRows[name] = raw
		}
	}
	if !reflect.DeepEqual(oldRows, downRows) || policyNativeCatalog(t, ctx, pool, false) != oldCatalog {
		t.Fatal("empty original098 down changed older rows/xmin/catalog")
	}
	if r, e := f.store.GetOwnMessagePolicy(ctx, f.access); !errors.Is(e, mp.ErrUnavailable) || !reflect.DeepEqual(r, mp.Record{}) {
		t.Fatal("absent098 source manufactured a policy", r, e)
	}
	policyNativeMigration(t, ctx, pool, "098_agent_message_request_policy.sql", true)
	if !reflect.DeepEqual(before, policyNativeAllRows(t, ctx, pool)) || policyNativeCatalog(t, ctx, pool, false) != oldCatalog {
		t.Fatal("original098 reapply changed older rows/xmin/catalog or seeded settings")
	}
	r, e := f.store.GetOwnMessagePolicy(ctx, f.access)
	if e != nil || r.Configured || r.Status != "UNCONFIGURED" || f.store.ValidateOwnMessagePolicyResponse(ctx, f.access, r) != nil {
		t.Fatal("reapplied actual098 missing current own contract", r, e)
	}
	f.put(0, mp.Screen)
	sender := policyNativeChildFixture(t, ctx, pool)
	a := ea.Access{Actor: identity.Actor{ID: sender.access.WorkspacePrincipal.ID, AccountType: "person"}, SessionDigest: sender.access.SessionDigest}
	var request connection.Request
	request, e = sender.store.CreateFriendRequestCurrent(ctx, a, f.access.WorkspacePrincipal.ID, "独占子库原待审申请", "", nil)
	if e != nil || request.ID == "" || request.PolicyDisposition != "SCREEN" || request.ScreeningStatus != "PENDING_REVIEW" {
		t.Fatal("used098 did not retain an original native Request/binding", request, e)
	}
	usedRows, usedCatalog := policyNativeAllRows(t, ctx, pool), policyNativeCatalog(t, ctx, pool, true)
	policyNativeMigration(t, ctx, pool, "098_agent_message_request_policy.down.sql", false)
	if !reflect.DeepEqual(usedRows, policyNativeAllRows(t, ctx, pool)) || usedCatalog != policyNativeCatalog(t, ctx, pool, true) {
		t.Fatal("refused used098 down changed retained current history or catalog")
	}
	var bindings, audits int
	if e = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM connection_request_policy_bindings WHERE request_id=$1 AND recipient_id=$2),(SELECT count(*) FROM audit_events WHERE actor_account_id=$2 AND purpose='human_message_policy_edit')`, request.ID, f.access.WorkspacePrincipal.ID).Scan(&bindings, &audits); e != nil || bindings != 1 || audits != 1 {
		t.Fatal("original098 protected binding/audit disappeared", bindings, audits, e)
	}
	t.Logf("NATIVE_098 empty_down_reapply_old_rows_xmin_catalog=true used_down_refused_retained_request=%s binding=%d audit=%d", request.ID, bindings, audits)
}
