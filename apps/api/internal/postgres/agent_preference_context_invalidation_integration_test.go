package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	acb "github.com/birdtie/birdtie/apps/api/internal/agentcontextbuilder"
	"github.com/birdtie/birdtie/apps/api/internal/agentoutbox"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// These tests invoke the original human private-profile writer, 076 preview /
// approval and PreferenceHandler Store. Synthetic local accounts are never
// production identities, and no grant, receipt or source row is forged.
type preferenceNativeFixture struct{ purpose *contextPurposeFixture }

func preferenceNative(t *testing.T) *preferenceNativeFixture {
	t.Helper()
	f := contextBuilderNative(t)
	savePrivateCanaries(t, f.place.private)
	r := f.request(t, acb.ActivitySearch)
	s := acb.PurposeSelection{AgentID: r.Agent.AgentID, TaskID: r.TaskID, CityID: r.CityID, TaskUpdatedAt: r.TaskUpdatedAt, CurrentQuery: r.CurrentQuery, ProfileFields: []string{"availability"}, DeadlineAt: r.DeadlineAt}
	p := &contextPurposeFixture{f: f, selection: s}
	b := f.place.private.base
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for _, q := range []string{
			`DELETE FROM agent_context_purpose_bindings WHERE grant_id IN(SELECT id FROM consent_grants WHERE owner_account_id=ANY($1::uuid[]))`,
			`DELETE FROM agent_context_purpose_previews WHERE owner_id=ANY($1::uuid[])`,
		} {
			if _, e := b.pool.Exec(ctx, q, b.accounts); e != nil {
				t.Error("owned preference fixture cleanup", e)
			}
		}
	})
	return &preferenceNativeFixture{purpose: p}
}

func (f *preferenceNativeFixture) write(t *testing.T, value string) agentprofile.PrivateRecord {
	t.Helper()
	b := f.purpose.f.place.private.base
	a := f.purpose.f.place.private.owner
	p, e := b.store.ReadOwnAgentPrivateProfile(b.ctx, a)
	if e != nil {
		t.Fatal("current private-profile read", e)
	}
	fields := agentprofile.PrivateFields{}
	if value != "" {
		fields.Availability = value
	}
	r, e := b.store.ReplaceOwnAgentPrivateProfile(b.ctx, a, agentprofile.ReplacePrivateInput{ExpectedVersion: p.Profile.ProfileVersion, Fields: fields})
	if e != nil {
		t.Fatal("actual human preference mutation", e)
	}
	return r
}

func (f *preferenceNativeFixture) id(t *testing.T) string {
	t.Helper()
	b := f.purpose.f.place.private.base
	var id string
	if e := b.pool.QueryRow(b.ctx, `SELECT gen_random_uuid()::text`).Scan(&id); e != nil {
		t.Fatal(e)
	}
	return id
}

func (f *preferenceNativeFixture) row(t *testing.T, revision int64, kind agentoutbox.EventType) agentoutbox.Record {
	t.Helper()
	b := f.purpose.f.place.private.base
	r, e := scanAgentOutbox(b.pool.QueryRow(b.ctx, `SELECT `+outboxColumns+` FROM agent_domain_outbox d WHERE subject_id=$1 AND schema_version=$2 AND source_revision=$3 AND event_type=$4`, b.person.ID, agentoutbox.PreferenceSchema, revision, kind))
	if e != nil {
		t.Fatal("actual captured preference row", e)
	}
	return r
}

func (f *preferenceNativeFixture) claim(t *testing.T) (agentoutbox.Record, agentoutbox.Claim) {
	t.Helper()
	b := f.purpose.f.place.private.base
	for i := 0; i < 8; i++ {
		var candidate string
		if e := b.pool.QueryRow(b.ctx, `SELECT event_id::text FROM agent_domain_outbox WHERE subject_id=$1 AND schema_version=$2 AND delivery_state='PENDING' ORDER BY occurred_at,event_id LIMIT 1`, b.person.ID, agentoutbox.PreferenceSchema).Scan(&candidate); e != nil {
			t.Fatal("actual pending candidate", e)
		}
		r, c, e := b.store.ClaimAgentOutboxControlForSubject(b.ctx, b.person, f.id(t), agentoutbox.PreferenceHandler)
		if e == nil {
			return r, c
		}
		// Original claim commits initial stale-source terminal progress without an
		// inbox and returns empty Unavailable. Only a real observed transition may
		// advance this bounded fixture; infrastructure/unknown results never retry.
		var state string
		var attempt int64
		if e == agentoutbox.ErrUnavailable && r == (agentoutbox.Record{}) && c == (agentoutbox.Claim{}) && b.pool.QueryRow(b.ctx, `SELECT delivery_state,attempt FROM agent_domain_outbox WHERE event_id=$1`, candidate).Scan(&state, &attempt) == nil && state == string(agentoutbox.Invalidated) && attempt == 0 {
			continue
		}
		f.diagnose(t, nil)
		t.Fatal("actual preference claim", e)
	}
	t.Fatal("bounded terminal progress did not reach current native source")
	return agentoutbox.Record{}, agentoutbox.Claim{}
}

func (f *preferenceNativeFixture) consume(t *testing.T, c agentoutbox.Claim) agentoutbox.ConsumerRecord {
	t.Helper()
	b := f.purpose.f.place.private.base
	p, e := b.store.ConsumeAgentOutboxControl(b.ctx, c)
	if e != nil {
		f.diagnose(t, &c)
		t.Fatal("actual preference consume", e)
	}
	return p
}

// A failing real Store path is replayed only inside a new rollback-only native
// transaction to reveal PG error codes; it cannot create a committed receipt.
type preferenceTraceTx struct {
	pgx.Tx
	t *testing.T
}

func (x preferenceTraceTx) Exec(ctx context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	tag, e := x.Tx.Exec(ctx, q, args...)
	x.trace(q, e)
	return tag, e
}
func (x preferenceTraceTx) QueryRow(ctx context.Context, q string, args ...any) pgx.Row {
	return preferenceTraceRow{x.Tx.QueryRow(ctx, q, args...), x, q}
}
func (x preferenceTraceTx) trace(q string, e error) {
	if e == nil {
		return
	}
	var pe *pgconn.PgError
	if errors.As(e, &pe) {
		x.t.Logf("rollback-only PG diagnostic: code=%s constraint=%s table=%s query-prefix=%.100s", pe.Code, pe.ConstraintName, pe.TableName, q)
	} else {
		x.t.Logf("rollback-only native diagnostic: error-type=%T query-prefix=%.100s", e, q)
		if strings.HasPrefix(e.Error(), "failed to encode args[3]") {
			x.t.Logf("safe revision binding diagnostic: %s", e.Error())
		}
	}
}

type preferenceTraceRow struct {
	pgx.Row
	tx preferenceTraceTx
	q  string
}

func (r preferenceTraceRow) Scan(v ...any) error { e := r.Row.Scan(v...); r.tx.trace(r.q, e); return e }
func (f *preferenceNativeFixture) diagnose(t *testing.T, c *agentoutbox.Claim) {
	b := f.purpose.f.place.private.base
	tx, e := b.pool.Begin(b.ctx)
	if e != nil {
		return
	}
	defer tx.Rollback(b.ctx)
	wrapped := preferenceTraceTx{tx, t}
	if c == nil {
		_, _, e = claimPreferenceOutboxTx(b.ctx, wrapped, f.id(t), b.person.ID)
	} else {
		_, e = consumePreferenceOutboxTx(b.ctx, wrapped, *c)
	}
	t.Logf("rollback-only original transaction result type=%T value=%v", e, e)
}

func (f *preferenceNativeFixture) history(t *testing.T) string {
	t.Helper()
	b := f.purpose.f.place.private.base
	var value string
	e := b.pool.QueryRow(b.ctx, `SELECT jsonb_build_object('events',(SELECT jsonb_agg(to_jsonb(d)||jsonb_build_object('xmin',d.xmin::text) ORDER BY event_id) FROM agent_domain_outbox d WHERE subject_id=$1 AND schema_version=$2),'receipts',(SELECT jsonb_agg(to_jsonb(i)||jsonb_build_object('xmin',i.xmin::text) ORDER BY event_id) FROM agent_consumer_inbox i WHERE subject_id=$1 AND handler_version=$3))::text`, b.person.ID, agentoutbox.PreferenceSchema, agentoutbox.PreferenceHandler).Scan(&value)
	if e != nil {
		t.Fatal(e)
	}
	return value
}

func TestPreferenceInvalidationNativeCaptureAndTwoStages(t *testing.T) {
	for _, cleared := range []bool{false, true} {
		t.Run(fmt.Sprintf("cleared_%t", cleared), func(t *testing.T) {
			ownedMigrationDatabase(t)
			f := preferenceNative(t)
			b := f.purpose.f.place.private.base
			initial := f.row(t, 2, agentoutbox.PreferenceUpdated)
			old, g := f.purpose.approve(t)
			unbound, e := b.store.PreviewOwnContextPurpose(b.ctx, f.purpose.f.place.private.owner, f.purpose.selection)
			if e != nil {
				t.Fatal(e)
			}
			value := "明确新偏好_PRIVATE_NATIVE"
			if cleared {
				value = ""
			}
			saved := f.write(t, value)
			older := f.row(t, 2, agentoutbox.PreferenceUpdated)
			wantOld := agentoutbox.Invalidated
			if cleared {
				wantOld = agentoutbox.Pending
			}
			if older.State != wantOld || older.Attempt != 0 || older.Fence != 0 || !reflect.DeepEqual(older.Event, initial.Event) || older.LeaseUntil != nil {
				t.Fatal("five-second configured refresh / clear separation altered immutable old metadata")
			}
			r, c := f.claim(t)
			if r.Event.EventType != agentoutbox.PreferenceUpdated || r.Event.Source.Revision != saved.Profile.ProfileVersion || r.Event.CausationID != nil || r.Event.RootTraceID != r.Event.LogicalOperationID || r.Event.Source.Fingerprint == "" {
				t.Fatal("non-native root source")
			}
			if (r.Event.Source.Status == agentoutbox.PreferenceCleared) != cleared {
				t.Fatal("clear did not capture metadata advancement and absence")
			}
			var current bool
			if e = b.pool.QueryRow(b.ctx, `SELECT birdtie_preference_outbox_current($1,$2,$3,$4,$5,$6)`, r.Event.AgentID, r.Event.Subject.ID, r.Event.Source.Revision, r.Event.Source.Status, r.Event.OccurredAt, r.Event.Source.Fingerprint).Scan(&current); e != nil || !current {
				t.Fatal("captured revision/xmin/occurrence does not match actual native source", e)
			}
			p := f.consume(t, c)
			if p.State != agentoutbox.MemoryComplete || p.Reason != agentoutbox.ReasonCleanupComplete {
				t.Fatal("parent not complete", p)
			}
			var exists bool
			if e = b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM agent_context_purpose_previews WHERE id=$1)`, unbound.ID).Scan(&exists); e != nil || exists {
				t.Fatal("old unbound preview survived", e)
			}
			if e = b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM agent_context_purpose_previews WHERE id=$1)`, old.ID).Scan(&exists); e != nil || !exists {
				t.Fatal("bound historical preview deleted", e)
			}
			child := f.row(t, saved.Profile.ProfileVersion, agentoutbox.PreferenceContextInvalidation)
			if child.Event.CausationID == nil || *child.Event.CausationID != r.Event.EventID || child.Event.RootTraceID != r.Event.RootTraceID || !child.Event.OccurredAt.Equal(r.Event.OccurredAt) || !child.Event.ExpiresAt.Equal(r.Event.ExpiresAt) || !child.Event.ReceivedAt.Equal(p.UpdatedAt) {
				t.Fatal("child not bound to original same-transaction parent receipt")
			}
			var sameTransaction bool
			if e = b.pool.QueryRow(b.ctx, `SELECT p.xmin=c.xmin AND p.xmin=i.xmin FROM agent_domain_outbox p JOIN agent_domain_outbox c ON c.causation_id=p.event_id JOIN agent_consumer_inbox i ON i.event_id=p.event_id AND i.handler_version=$2 WHERE p.event_id=$1`, r.Event.EventID, agentoutbox.PreferenceHandler).Scan(&sameTransaction); e != nil || !sameTransaction {
				t.Fatal("parent receipt and depth1 child did not commit in one actual transaction", e)
			}
			_, cc := f.claim(t)
			cp := f.consume(t, cc)
			if cc.EventID != child.Event.EventID || cp.State != agentoutbox.MemoryComplete {
				t.Fatal("child not complete", cp)
			}
			var revision int64
			var revoked *time.Time
			if e = b.pool.QueryRow(b.ctx, `SELECT revision,revoked_at FROM consent_grants WHERE id=$1`, g.ID).Scan(&revision, &revoked); e != nil || revision != g.Revision+1 || revoked == nil {
				t.Fatal("sole old grant not revoked by original CAS", e)
			}
			before := f.history(t)
			again, e := b.store.ConsumeAgentOutboxControl(b.ctx, cc)
			if e != nil || !reflect.DeepEqual(again, cp) || f.history(t) != before {
				t.Fatal("historical replay changed committed receipt", e)
			}
			var effects, memories int
			if e = b.pool.QueryRow(b.ctx, `SELECT (SELECT count(*) FROM agent_effect_ledger WHERE subject_id=$1),(SELECT count(*) FROM agent_memories WHERE owner_id=$1)`, b.person.ID).Scan(&effects, &memories); e != nil || effects != 0 || memories != 0 {
				t.Fatal("metadata cleanup caused unapproved content effect", e)
			}
		})
	}
}

func TestPreferenceInvalidationNativeSourceOrderABA(t *testing.T) {
	ownedMigrationDatabase(t)
	f := preferenceNative(t)
	b := f.purpose.f.place.private.base
	first := f.write(t, "A明确偏好")
	_, c := f.claim(t)
	f.write(t, "B明确偏好")
	last := f.write(t, "A明确偏好")
	if last.Profile.ProfileVersion <= first.Profile.ProfileVersion+1 {
		t.Fatal("ABA did not advance real source revision")
	}
	current, g := f.purpose.approve(t)
	p := f.consume(t, c)
	if p.State != agentoutbox.Invalidated || p.Reason != agentoutbox.ReasonSourceInvalidated {
		t.Fatal("old source accepted after ABA", p)
	}
	var revision int64
	var revoked *time.Time
	var exists bool
	if e := b.pool.QueryRow(b.ctx, `SELECT revision,revoked_at FROM consent_grants WHERE id=$1`, g.ID).Scan(&revision, &revoked); e != nil || revoked != nil || revision != g.Revision {
		t.Fatal("old source cleanup changed current grant", e)
	}
	if e := b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM agent_context_purpose_previews WHERE id=$1)`, current.ID).Scan(&exists); e != nil || !exists {
		t.Fatal("current preview removed", e)
	}
	before := f.history(t)
	bad := c
	bad.Subject = b.other
	if value, e := b.store.ConsumeAgentOutboxControl(b.ctx, bad); e == nil || value != (agentoutbox.ConsumerRecord{}) {
		t.Fatal("foreign subject consumed claim")
	}
	bad = c
	bad.Fence++
	if _, e := b.store.ConsumeAgentOutboxControl(b.ctx, bad); !errors.Is(e, agentoutbox.ErrConflict) {
		t.Fatal("invented fence accepted", e)
	}
	if f.history(t) != before {
		t.Fatal("invalid claim changed historical evidence")
	}
}

func TestPreferenceInvalidationNativeTenantFairnessAndCapacity(t *testing.T) {
	t.Run("actual_global_four_owner_one", func(t *testing.T) {
		ownedMigrationDatabase(t)
		fixtures := make([]*preferenceNativeFixture, 5)
		claims := make([]agentoutbox.Claim, 4)
		for i := range fixtures {
			fixtures[i] = preferenceNative(t)
		}
		b := fixtures[0].purpose.f.place.private.base
		_, claims[0] = fixtures[0].claim(t)
		// A second real source mutation leaves eligible work while the original
		// claim is still leased. Without it, NotFound is correct (no tenant head)
		// and does not exercise the one-owner capacity limit.
		fixtures[0].write(t, "已领取后仍有新的真实待处理偏好")
		beforeOwner := fixtures[0].history(t)
		r, c, e := b.store.ClaimAgentOutboxControlForSubject(b.ctx, b.person, fixtures[0].id(t), agentoutbox.PreferenceHandler)
		if e != agentoutbox.ErrDispatchBusy || r != (agentoutbox.Record{}) || c != (agentoutbox.Claim{}) || fixtures[0].history(t) != beforeOwner {
			t.Fatal("one-owner quota did not close while global capacity remained", e)
		}
		for i := 1; i < 4; i++ {
			_, claims[i] = fixtures[i].claim(t)
		}
		for _, i := range []int{0, 4} {
			f := fixtures[i]
			before := f.history(t)
			r, c, e := b.store.ClaimAgentOutboxControlForSubject(b.ctx, f.purpose.f.place.private.base.person, f.id(t), agentoutbox.PreferenceHandler)
			if e != agentoutbox.ErrDispatchBusy || r != (agentoutbox.Record{}) || c != (agentoutbox.Claim{}) || f.history(t) != before {
				t.Fatal("quota did not close with zero control mutation", i, e)
			}
		}
		var count int
		if e := b.pool.QueryRow(b.ctx, `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() n) SELECT count(*) FROM agent_domain_outbox d CROSS JOIN stamp WHERE `+outboxControlActiveSQL).Scan(&count); e != nil || count != 4 {
			t.Fatal("actual committed native active count", count, e)
		}
		fixtures[0].consume(t, claims[0])
		_, c = fixtures[4].claim(t)
		if c.Subject != fixtures[4].purpose.f.place.private.base.person {
			t.Fatal("fifth owner did not regain capacity")
		}
	})
	t.Run("burst_backlog_does_not_monopolize", func(t *testing.T) {
		ownedMigrationDatabase(t)
		a := preferenceNative(t)
		for i := 0; i < 6; i++ {
			_, c := a.claim(t)
			a.consume(t, c)
			a.write(t, fmt.Sprintf("明确突发偏好%d", i))
		}
		b, c := preferenceNative(t), preferenceNative(t)
		base := a.purpose.f.place.private.base
		seen := map[string]bool{}
		for i := 0; i < 2; i++ {
			_, claim, e := base.store.ClaimAgentOutboxControl(base.ctx, a.id(t), agentoutbox.PreferenceHandler)
			if e != nil {
				t.Fatal("actual unscoped admission", e)
			}
			seen[claim.Subject.ID] = true
			if claim.Subject == base.person {
				t.Fatal("served burst owner monopolized unserved tenant heads")
			}
		}
		if !seen[b.purpose.f.place.private.base.person.ID] || !seen[c.purpose.f.place.private.base.person.ID] {
			t.Fatal("unserved native owners were lost")
		}
	})
}

func TestPreferenceInvalidationNative102RetentionAndDown(t *testing.T) {
	ownedMigrationDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool, e := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	migration := func(down bool) error {
		name := "102_agent_preference_context_invalidation.sql"
		if down {
			name = "102_agent_preference_context_invalidation.down.sql"
		}
		raw, e := os.ReadFile(filepath.Join("..", "..", "migrations", name))
		if e != nil {
			t.Fatal(e)
		}
		conn, e := pool.Acquire(ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer conn.Release()
		_, e = conn.Exec(ctx, string(raw))
		if e != nil {
			_, _ = conn.Exec(ctx, "ROLLBACK")
		}
		return e
	}
	// This own104 database is empty of Preference effects. Remove only102 to
	// build the genuine pre102 fixture while leaving independent103/104 intact.
	if e = migration(true); e != nil {
		t.Fatal("empty102 down", e)
	}
	f := preferenceNative(t)
	b := f.purpose.f.place.private.base
	m, e := b.store.CreateMomentDraft(b.ctx, b.person.ID, outboxMomentInput())
	if e != nil {
		t.Fatal(e)
	}
	_, legacy, e := b.store.ClaimAgentOutboxControlForSubject(b.ctx, b.person, f.id(t), agentoutbox.HandlerV1)
	if e != nil {
		t.Fatal(e)
	}
	legacyReceipt, e := b.store.ConsumeAgentOutboxControl(b.ctx, legacy)
	if e != agentoutbox.ErrUnavailable || legacyReceipt.State != agentoutbox.Unavailable {
		t.Fatal("original Moment branch not preserved before102", e)
	}
	rows := func() string {
		var raw string
		q := `SELECT jsonb_build_object('accounts',(SELECT jsonb_agg(to_jsonb(a)||jsonb_build_object('xmin',a.xmin::text) ORDER BY a.id) FROM accounts a WHERE id=ANY($1::uuid[])),
		'profiles',(SELECT jsonb_agg(to_jsonb(ap)||jsonb_build_object('xmin',ap.xmin::text) ORDER BY agent_id) FROM agent_profiles ap WHERE owner_id=ANY($1::uuid[])),
		'private',(SELECT jsonb_agg(to_jsonb(pp)||jsonb_build_object('xmin',pp.xmin::text) ORDER BY agent_id) FROM agent_private_profiles pp WHERE owner_id=ANY($1::uuid[])),
		'moment',(SELECT to_jsonb(m)||jsonb_build_object('xmin',m.xmin::text) FROM moments m WHERE id=$2),
		'originalOutbox',(SELECT jsonb_agg(to_jsonb(d)||jsonb_build_object('xmin',d.xmin::text) ORDER BY event_id) FROM agent_domain_outbox d WHERE subject_id=$3 AND schema_version='agent-outbox-v1'),
		'originalInbox',(SELECT jsonb_agg(to_jsonb(i)||jsonb_build_object('xmin',i.xmin::text) ORDER BY event_id) FROM agent_consumer_inbox i WHERE subject_id=$3 AND handler_version='mom-control-v1'))::text`
		if e := pool.QueryRow(ctx, q, b.accounts, m.ID, b.person.ID).Scan(&raw); e != nil {
			t.Fatal(e)
		}
		return raw
	}
	catalog := func() string {
		var raw string
		if e := pool.QueryRow(ctx, `SELECT jsonb_build_object('checks',(SELECT jsonb_agg(jsonb_build_object('table',r.relname,'name',c.conname,'definition',pg_get_constraintdef(c.oid)) ORDER BY r.relname,c.conname) FROM pg_constraint c JOIN pg_class r ON r.oid=c.conrelid WHERE r.relname IN('agent_domain_outbox','agent_consumer_inbox') AND c.contype='c'),
		'triggers',(SELECT jsonb_agg(pg_get_triggerdef(t.oid) ORDER BY t.tgname) FROM pg_trigger t JOIN pg_class r ON r.oid=t.tgrelid WHERE r.relname IN('agent_domain_outbox','agent_consumer_inbox','agent_private_profiles') AND NOT t.tgisinternal),
		'indexes',(SELECT jsonb_agg(indexdef ORDER BY indexname) FROM pg_indexes WHERE schemaname='public' AND tablename IN('agent_domain_outbox','agent_consumer_inbox','agent_private_profiles')))::text`).Scan(&raw); e != nil {
			t.Fatal(e)
		}
		return raw
	}
	before, floor := rows(), catalog()
	if e = migration(false); e != nil || rows() != before {
		t.Fatal("actual102 up changed old IDs/rows/xmin", e)
	}
	up := catalog()
	if e = migration(true); e != nil || rows() != before || catalog() != floor {
		t.Fatal("actual102 down changed old rows/floor catalog", e)
	}
	if e = migration(false); e != nil || rows() != before || catalog() != up {
		t.Fatal("actual102 reapply changed old rows/up catalog", e)
	}
	f.write(t, "迁移后本人新偏好")
	_, root := f.claim(t)
	f.consume(t, root)
	_, child := f.claim(t)
	f.consume(t, child)
	history := f.history(t)
	used := catalog()
	e = migration(true)
	var pe *pgconn.PgError
	if !errors.As(e, &pe) || pe.Code != "55000" || f.history(t) != history || catalog() != used {
		t.Fatal("used102 down did not fail atomically preserving records/catalog", e)
	}
	var count int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM agent_consumer_inbox WHERE subject_id=$1 AND handler_version='mom-control-v1'`, b.person.ID).Scan(&count); e != nil || count != 1 {
		t.Fatal("102 removed original Moment receipt", e)
	}
}

func TestPreferenceInvalidationNativeRootBudgetAndDepth(t *testing.T) {
	ownedMigrationDatabase(t)
	f := preferenceNative(t)
	b := f.purpose.f.place.private.base
	for i := 0; i < 501; i++ {
		f.purpose.approve(t)
	}
	saved := f.write(t, "真实新偏好导致501份旧具体版本许可失效")
	root, claim := f.claim(t)
	f.consume(t, claim)
	var final agentoutbox.ConsumerRecord
	for i := 0; i < 5; i++ {
		_, child := f.claim(t)
		final = f.consume(t, child)
	}
	if final.State != agentoutbox.DeadLetter || final.Reason != agentoutbox.ReasonRootExhausted {
		t.Fatal("root attempt budget silently reset", final)
	}
	var sum, revoked, pending int
	if e := b.pool.QueryRow(b.ctx, `SELECT (SELECT sum(attempt) FROM agent_domain_outbox WHERE subject_id=$1 AND root_trace_id=$2),(SELECT count(*) FROM consent_grants WHERE owner_account_id=$1 AND purpose='TASK_CONTEXT_READ' AND revoked_at IS NOT NULL),(SELECT count(*) FROM consent_grants WHERE owner_account_id=$1 AND purpose='TASK_CONTEXT_READ' AND revoked_at IS NULL)`, b.person.ID, root.Event.RootTraceID).Scan(&sum, &revoked, &pending); e != nil || sum != 6 || revoked != 500 || pending != 1 {
		t.Fatal("bounded cleanup did not retain visible remaining work", sum, revoked, pending, e)
	}
	child := f.row(t, saved.Profile.ProfileVersion, agentoutbox.PreferenceContextInvalidation)
	before := f.history(t)
	for _, pair := range []struct{ id, cause string }{{root.Event.EventID, root.Event.EventID}, {child.Event.EventID, child.Event.EventID}, {child.Event.EventID, root.Event.EventID}} {
		// Real original trigger rejects self/third-level or a recreated depth1
		// outside the original parent's commit transaction; no test receipt inserted.
		_, e := b.pool.Exec(b.ctx, `INSERT INTO agent_domain_outbox(schema_version,event_id,event_type,tenant_id,subject_id,actor_id,agent_id,source_type,source_id,source_revision,source_fingerprint,source_status,logical_operation_id,root_trace_id,causation_id,occurred_at,received_at,expires_at)
		 SELECT schema_version,event_id,event_type,tenant_id,subject_id,actor_id,agent_id,source_type,source_id,source_revision,source_fingerprint,source_status,logical_operation_id,root_trace_id,$2::uuid,occurred_at,received_at,expires_at FROM agent_domain_outbox WHERE event_id=$1`, pair.id, pair.cause)
		var pg *pgconn.PgError
		if !errors.As(e, &pg) || pg.Code != "55000" {
			t.Fatal("native causal guard did not reject impossible chain", e)
		}
	}
	if f.history(t) != before {
		t.Fatal("denied causal insertion changed native history")
	}
	var memories int
	if e := b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_memories WHERE owner_id=$1`, b.person.ID).Scan(&memories); e != nil || memories != 0 {
		t.Fatal("cleanup generated a self MemoryUpdated loop", e)
	}
}

func preferenceWait(t *testing.T, pool *pgxpool.Pool, ctx context.Context, name string) {
	t.Helper()
	until := time.Now().Add(3 * time.Second)
	for time.Now().Before(until) {
		var wait bool
		if e := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND application_name=$1 AND wait_event_type='Lock')`, name).Scan(&wait); e != nil {
			t.Fatal(e)
		}
		if wait {
			t.Log("actual native PG lock wait observed", name)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("actual PostgreSQL lock wait absent", name)
}

func TestPreferenceInvalidationNativeRealWaitFinalFence(t *testing.T) {
	ownedMigrationDatabase(t)
	f := preferenceNative(t)
	b := f.purpose.f.place.private.base
	_, grant := f.purpose.approve(t)
	saved := f.write(t, "需要真实等待后再验租期")
	_, root := f.claim(t)
	f.consume(t, root)
	_, child := f.claim(t)
	tx, e := b.pool.Begin(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(context.Background())
	var id string
	if e = tx.QueryRow(b.ctx, `SELECT id::text FROM consent_grants WHERE id=$1 FOR UPDATE`, grant.ID).Scan(&id); e != nil {
		t.Fatal(e)
	}
	cfg, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	name := "preference-native-consumer-" + b.person.ID
	cfg.ConnConfig.RuntimeParams["application_name"] = name
	pool, e := pgxpool.NewWithConfig(b.ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	type result struct {
		p agentoutbox.ConsumerRecord
		e error
	}
	done := make(chan result, 1)
	go func() { p, e := New(pool, false).ConsumeAgentOutboxControl(b.ctx, child); done <- result{p, e} }()
	preferenceWait(t, b.pool, b.ctx, name)
	// The actual consumer holds metadata before waiting for its old grant row.
	// The original human writer must wait there and cancel without a source write.
	cfg.ConnConfig.RuntimeParams["application_name"] = "preference-native-human-" + b.person.ID
	human, e := pgxpool.NewWithConfig(b.ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer human.Close()
	hctx, cancel := context.WithTimeout(b.ctx, 2*time.Second)
	defer cancel()
	hdone := make(chan error, 1)
	go func() {
		_, e := New(human, false).ReplaceOwnAgentPrivateProfile(hctx, f.purpose.f.place.private.owner, agentprofile.ReplacePrivateInput{ExpectedVersion: saved.Profile.ProfileVersion, Fields: agentprofile.PrivateFields{Availability: "未获执行的等待写入"}})
		hdone <- e
	}()
	preferenceWait(t, b.pool, b.ctx, cfg.ConnConfig.RuntimeParams["application_name"])
	if e = <-hdone; e == nil {
		t.Fatal("waiting human writer bypassed original metadata lock")
	}
	time.Sleep(time.Until(child.LeaseUntil) + 100*time.Millisecond)
	if e = tx.Rollback(b.ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case r := <-done:
		if !errors.Is(r.e, agentoutbox.ErrExpired) || r.p != (agentoutbox.ConsumerRecord{}) {
			t.Fatal("expired blocked consumer returned committed result", r.e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("native consumer wait unresolved")
	}
	var version int64
	var revoked *time.Time
	if e = b.pool.QueryRow(b.ctx, `SELECT revision,revoked_at FROM consent_grants WHERE id=$1`, grant.ID).Scan(&version, &revoked); e != nil || version != grant.Revision || revoked != nil {
		t.Fatal("late cleanup committed partial revocation", e)
	}
	r := f.row(t, saved.Profile.ProfileVersion, agentoutbox.PreferenceContextInvalidation)
	if r.State != agentoutbox.Leased || r.Fence != child.Fence {
		t.Fatal("late consumer wrote checkpoint despite rollback")
	}
}

// A real child test process claims through the original Store and exits. The
// parent reads actual native rows; Claim's intentionally non-wire authority is
// never serialized or passed to a different process as a permission.
func TestPreferenceInvalidationNativeProcessRestart(t *testing.T) {
	if os.Getenv("BIRDTIE_PREFERENCE_NATIVE_HELPER") == "claim" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		pool, e := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
		if e != nil {
			os.Exit(2)
		}
		subject, e := actorref.ParsePrincipal("PERSON", os.Getenv("BIRDTIE_PREFERENCE_NATIVE_SUBJECT"))
		if e != nil {
			os.Exit(3)
		}
		_, _, e = New(pool, false).ClaimAgentOutboxControlForSubject(ctx, subject, os.Getenv("BIRDTIE_PREFERENCE_NATIVE_WORKER"), agentoutbox.PreferenceHandler)
		if e != nil {
			os.Exit(4)
		}
		fmt.Print("CLAIMED")
		os.Exit(0)
	}
	ownedMigrationDatabase(t)
	f := preferenceNative(t)
	b := f.purpose.f.place.private.base
	worker := f.id(t)
	ctx, cancel := context.WithTimeout(b.ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPreferenceInvalidationNativeProcessRestart$")
	cmd.Env = append(os.Environ(), "BIRDTIE_PREFERENCE_NATIVE_HELPER=claim", "BIRDTIE_PREFERENCE_NATIVE_SUBJECT="+b.person.ID, "BIRDTIE_PREFERENCE_NATIVE_WORKER="+worker)
	out, e := cmd.CombinedOutput()
	if e != nil || string(out) != "CLAIMED" {
		t.Fatal("actual native child claim failed", e)
	}
	old := f.row(t, 2, agentoutbox.PreferenceUpdated)
	if old.State != agentoutbox.Leased || old.LeaseOwner != worker || old.LeaseUntil == nil {
		t.Fatal("child did not leave actual native lease")
	}
	claim := agentoutbox.Claim{EventID: old.Event.EventID, Subject: old.Event.Subject, AgentID: old.Event.AgentID, HandlerVersion: agentoutbox.PreferenceHandler, WorkerID: worker, Fence: old.Fence, LeaseUntil: *old.LeaseUntil}
	time.Sleep(time.Until(claim.LeaseUntil) + 100*time.Millisecond)
	binary := maintenanceBinary(t)
	report, code, e := maintenanceCommand(binary, b.person.ID, agentoutbox.PreferenceHandler)
	if e != nil || code != 0 || report.ConfirmedReceipts != 2 || report.Counts.MaintenanceComplete != 2 {
		t.Fatal("actual new-process parent/child recovery failed", report, code, e)
	}
	if _, e = b.store.ConsumeAgentOutboxControl(b.ctx, claim); !errors.Is(e, agentoutbox.ErrConflict) {
		t.Fatal("expired original worker fence accepted", e)
	}
	current := f.row(t, 2, agentoutbox.PreferenceUpdated)
	if current.Fence != old.Fence+1 || current.State != agentoutbox.MemoryComplete {
		t.Fatal("process restart reset or lost lease fence")
	}
	before := f.history(t)
	report, code, e = maintenanceCommand(binary, b.person.ID, agentoutbox.PreferenceHandler)
	if e != nil || code != 0 || report.ConfirmedReceipts != 0 || f.history(t) != before {
		t.Fatal("new process repeated historical cleanup/receipt", report, code, e)
	}
}
