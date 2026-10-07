package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentoutbox"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type outboxFixture struct{ private *agentPrivateFixture }

func newOutboxFixture(t *testing.T) *outboxFixture {
	t.Helper()
	f := &outboxFixture{private: agentPrivateTestFixture(t)}
	b := f.private.base
	var installed bool
	if e := b.pool.QueryRow(b.ctx, `SELECT to_regclass('public.agent_domain_outbox') IS NOT NULL`).Scan(&installed); e != nil || !installed {
		t.Fatal("requires current migration064", e)
	}
	if _, e := b.store.EnsureAgentProfile(b.ctx, b.personID, b.person); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for _, q := range []string{`DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`, `DELETE FROM moments WHERE author_account_id=ANY($1::uuid[])`} {
			if _, e := b.pool.Exec(ctx, q, b.accounts); e != nil {
				t.Errorf("owned outbox source cleanup: %v", e)
			}
		}
	})
	return f
}
func (f *outboxFixture) id(t *testing.T) string {
	t.Helper()
	var id string
	if e := f.private.base.pool.QueryRow(f.private.base.ctx, `SELECT gen_random_uuid()::text`).Scan(&id); e != nil {
		t.Fatal(e)
	}
	return id
}
func outboxMomentInput() content.MomentInput {
	historical := time.Date(2017, 5, 2, 0, 0, 0, 0, time.UTC)
	return content.MomentInput{CityID: "aberdeen-gb", Title: "私密原始标题不能进入控制表015", Body: "私密原始正文不能进入控制表015", OccurredAt: &historical, TimePrecision: "day", LocationPrecision: "city"}
}
func (f *outboxFixture) create(t *testing.T) content.Moment {
	t.Helper()
	b := f.private.base
	m, e := b.store.CreateMomentDraft(b.ctx, b.person.ID, outboxMomentInput())
	if e != nil {
		t.Fatal("native Moment writer", e)
	}
	return m
}
func (f *outboxFixture) row(t *testing.T, source string, revision int64) agentoutbox.Record {
	t.Helper()
	b := f.private.base
	r, e := scanAgentOutbox(b.pool.QueryRow(b.ctx, `SELECT `+outboxColumns+` FROM agent_domain_outbox d WHERE source_id=$1 AND source_revision=$2`, source, revision))
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func (f *outboxFixture) count(t *testing.T, table string) int {
	t.Helper()
	b := f.private.base
	var n int
	if table != "agent_domain_outbox" && table != "agent_consumer_inbox" && table != "agent_effect_ledger" {
		t.Fatal("unregistered test table")
	}
	if e := b.pool.QueryRow(b.ctx, `SELECT count(*) FROM `+table+` WHERE subject_id=$1`, b.person.ID).Scan(&n); e != nil {
		t.Fatal(e)
	}
	return n
}
func (f *outboxFixture) claim(t *testing.T, handler agentoutbox.HandlerVersion) (agentoutbox.Record, agentoutbox.Claim) {
	t.Helper()
	b := f.private.base
	r, c, e := b.store.ClaimAgentOutboxControlForSubject(b.ctx, b.person, f.id(t), handler)
	if e != nil {
		t.Fatal("real fenced claim", e)
	}
	return r, c
}
func consumeOutboxUnavailable(t *testing.T, f *outboxFixture, c agentoutbox.Claim) agentoutbox.ConsumerRecord {
	t.Helper()
	r, e := f.private.base.store.ConsumeAgentOutboxControl(f.private.base.ctx, c)
	if !errors.Is(e, agentoutbox.ErrUnavailable) || r.State != agentoutbox.Unavailable || agentoutbox.ValidateConsumerRecord(r) != nil {
		t.Fatalf("actual purpose unavailable control: %+v %v", r, e)
	}
	return r
}

func TestAgentOutboxNativeThreeWritersMetadata(t *testing.T) {
	f := newOutboxFixture(t)
	b := f.private.base
	m := f.create(t)
	created := f.row(t, m.ID, 1)
	if created.Event.EventType != agentoutbox.MomentCreated || !created.Event.OccurredAt.Equal(m.CreatedAt) || !created.Event.ExpiresAt.Equal(m.CreatedAt.Add(15*time.Minute)) || created.Event.OccurredAt.Year() == 2017 {
		t.Fatalf("native clock metadata %+v", created)
	}
	in := outboxMomentInput()
	in.Title = "私密编辑标题015"
	u, e := b.store.UpdateMomentDraft(b.ctx, b.person.ID, m.ID, 1, in)
	if e != nil {
		t.Fatal(e)
	}
	updated := f.row(t, m.ID, 2)
	if updated.Event.EventType != agentoutbox.MomentUpdated || !updated.Event.OccurredAt.Equal(u.UpdatedAt) {
		t.Fatal("update source clock")
	}
	if e = b.store.WithdrawMoment(b.ctx, b.person.ID, m.ID, 2); e != nil {
		t.Fatal(e)
	}
	withdrawn := f.row(t, m.ID, 3)
	if withdrawn.Event.EventType != agentoutbox.MomentWithdrawn || withdrawn.Event.Source.Status != agentoutbox.Withdrawn {
		t.Fatal("native withdrawal")
	}
	if _, e = b.store.UpdateMomentDraft(b.ctx, b.person.ID, m.ID, 2, in); !errors.Is(e, content.ErrConflict) {
		t.Fatal("stale CAS", e)
	}
	if f.count(t, "agent_domain_outbox") != 3 || f.count(t, "agent_effect_ledger") != 0 {
		t.Fatal("duplicate capture or effect")
	}
	var raw string
	if e = b.pool.QueryRow(b.ctx, `SELECT jsonb_agg(to_jsonb(d))::text FROM agent_domain_outbox d WHERE subject_id=$1`, b.person.ID).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	for _, secret := range []string{m.Title, m.Body, in.Title, "session", "token", "latitude", "longitude"} {
		if strings.Contains(raw, secret) {
			t.Fatalf("metadata leaked %q", secret)
		}
	}
}

func TestAgentOutboxMissingMetadataDoesNotCreateConsent(t *testing.T) {
	f := newOutboxFixture(t)
	b := f.private.base
	b.exec(`DELETE FROM agent_profiles WHERE agent_id=$1`, b.personID)
	f.create(t)
	if f.count(t, "agent_domain_outbox") != 0 {
		t.Fatal("captured unbound metadata")
	}
	var n int
	if e := b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_profiles WHERE agent_id=$1`, b.personID).Scan(&n); e != nil || n != 0 {
		t.Fatal("invented metadata", e)
	}
}

// The fault is a real PostgreSQL trigger in this exclusively owned database.
// It changes no production source or original global trigger; all names are
// unique and every statement/drop is checked. It is simulated crash evidence.
func installOutboxFault(t *testing.T, f *outboxFixture, table, timing, operation string) func() {
	t.Helper()
	b := f.private.base
	if table != "agent_domain_outbox" && table != "agent_consumer_inbox" {
		t.Fatal("unregistered fault table")
	}
	name := "test_outbox_fault_" + strings.ReplaceAll(f.id(t), "-", "")
	q := fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.subject_id='%s'::uuid THEN RAISE EXCEPTION 'owned simulated transaction crash'; END IF; RETURN NEW; END $$; CREATE TRIGGER %s %s %s ON %s FOR EACH ROW EXECUTE FUNCTION %s()`, name, b.person.ID, name, timing, operation, table, name)
	if _, e := b.pool.Exec(b.ctx, q); e != nil {
		t.Fatal(e)
	}
	return func() {
		if _, e := b.pool.Exec(b.ctx, fmt.Sprintf(`DROP TRIGGER %s ON %s; DROP FUNCTION %s()`, name, table, name)); e != nil {
			t.Errorf("owned fault cleanup: %v", e)
		}
	}
}

func TestAgentOutboxOriginalCommitAtomicFailures(t *testing.T) {
	for _, timing := range []string{"BEFORE", "AFTER"} {
		t.Run(timing, func(t *testing.T) {
			f := newOutboxFixture(t)
			b := f.private.base
			remove := installOutboxFault(t, f, "agent_domain_outbox", timing, "INSERT")
			defer remove()
			if _, e := b.store.CreateMomentDraft(b.ctx, b.person.ID, outboxMomentInput()); e == nil {
				t.Fatal("writer swallowed real outbox failure")
			}
			var n int
			if e := b.pool.QueryRow(b.ctx, `SELECT (SELECT count(*) FROM moments WHERE author_account_id=$1)+(SELECT count(*) FROM audit_events WHERE actor_account_id=$1)`, b.person.ID).Scan(&n); e != nil || n != 0 {
				t.Fatal("partial business commit", n, e)
			}
			if f.count(t, "agent_domain_outbox") != 0 {
				t.Fatal("partial event commit")
			}
		})
	}
}

func TestAgentOutbox100ReplayAndHandlerUpgradeZeroEffects(t *testing.T) {
	f := newOutboxFixture(t)
	b := f.private.base
	f.create(t)
	r, c := f.claim(t, agentoutbox.HandlerV1)
	first := consumeOutboxUnavailable(t, f, c)
	for i := 0; i < 100; i++ {
		if _, e := b.store.ConsumeAgentOutboxControl(b.ctx, c); !errors.Is(e, agentoutbox.ErrConflict) {
			t.Fatalf("late replay%d %v", i, e)
		}
		if _, _, e := b.store.ClaimAgentOutboxControlForSubject(b.ctx, b.person, f.id(t), agentoutbox.HandlerV1); !errors.Is(e, agentoutbox.ErrNotFound) {
			t.Fatalf("same handler replay%d %v", i, e)
		}
	}
	r2, c2 := f.claim(t, agentoutbox.HandlerV2)
	if r.Event.EventID != r2.Event.EventID || c2.Fence != c.Fence+1 {
		t.Fatal("handler invented event or fence")
	}
	consumeOutboxUnavailable(t, f, c2)
	if f.count(t, "agent_consumer_inbox") != 2 || f.count(t, "agent_effect_ledger") != 0 {
		t.Fatal("effect or receipt duplicated")
	}
	var unchanged bool
	if e := b.pool.QueryRow(b.ctx, `SELECT fence=$3 AND created_at=$4 AND updated_at=$5 FROM agent_consumer_inbox WHERE event_id=$1 AND handler_version=$2`, first.EventID, first.HandlerVersion, first.Fence, first.CreatedAt, first.UpdatedAt).Scan(&unchanged); e != nil || !unchanged {
		t.Fatal("old terminal receipt changed", e)
	}
	address := agentoutbox.EffectAddress{Tenant: r.Event.Tenant, Subject: r.Event.Subject, AgentID: r.Event.AgentID, LogicalOperationID: r.Event.LogicalOperationID, ActionID: f.id(t), Kind: agentoutbox.MemoryCandidateEffect}
	key, e := agentoutbox.EffectKey(address)
	if e != nil {
		t.Fatal(e)
	}
	_, e = b.pool.Exec(b.ctx, `INSERT INTO agent_effect_ledger(subject_id,effect_key,agent_id,logical_operation_id,action_id,effect_kind,action_digest,source_id,source_revision) VALUES($1,$2,$3,$4,$5,'MEMORY_CANDIDATE',$2,$6,$7)`, b.person.ID, key, b.personID, address.LogicalOperationID, address.ActionID, r.Event.Source.ID, r.Event.Source.Revision)
	var pgerr *pgconn.PgError
	if !errors.As(e, &pgerr) || pgerr.Code != "55000" {
		t.Fatalf("actual effect writer did not fail closed: %v", e)
	}
}

func TestAgentOutboxConcurrentClaimOneFence(t *testing.T) {
	f := newOutboxFixture(t)
	b := f.private.base
	f.create(t)
	workers := make([]string, 8)
	for i := range workers {
		workers[i] = f.id(t)
	}
	var wg sync.WaitGroup
	claims := make(chan agentoutbox.Claim, 8)
	errs := make(chan error, 8)
	denied := make(chan error, 8)
	for _, w := range workers {
		wg.Add(1)
		go func(w string) {
			defer wg.Done()
			_, c, e := b.store.ClaimAgentOutboxControlForSubject(b.ctx, b.person, w, agentoutbox.HandlerV1)
			if e == nil {
				claims <- c
			} else if e == agentoutbox.ErrNotFound || e == agentoutbox.ErrDispatchBusy {
				denied <- e
			} else {
				errs <- e
			}
		}(w)
	}
	wg.Wait()
	close(claims)
	close(errs)
	close(denied)
	for e := range errs {
		t.Error(e)
	}
	if len(claims) != 1 || len(denied) != 7 {
		t.Fatalf("claims=%d denied=%d", len(claims), len(denied))
	}
	for c := range claims {
		if c.Fence != 1 {
			t.Fatal("first fence")
		}
		consumeOutboxUnavailable(t, f, c)
	}
}

func TestAgentOutboxSourceAndIdentityInvalidation(t *testing.T) {
	for _, change := range []string{"edit", "withdraw", "physical_delete", "metadata_delete", "agent_restore", "person_restore", "context_change"} {
		t.Run(change, func(t *testing.T) {
			f := newOutboxFixture(t)
			b := f.private.base
			m := f.create(t)
			_, c := f.claim(t, agentoutbox.HandlerV1)
			switch change {
			case "edit":
				in := outboxMomentInput()
				in.Title = "另一个真实版本"
				if _, e := b.store.UpdateMomentDraft(b.ctx, b.person.ID, m.ID, 1, in); e != nil {
					t.Fatal(e)
				}
			case "withdraw":
				if e := b.store.WithdrawMoment(b.ctx, b.person.ID, m.ID, 1); e != nil {
					t.Fatal(e)
				}
			case "physical_delete":
				b.exec(`DELETE FROM moments WHERE id=$1`, m.ID)
			case "metadata_delete":
				b.exec(`DELETE FROM agent_profiles WHERE agent_id=$1`, b.personID)
			case "agent_restore":
				b.exec(`UPDATE agents SET status='suspended' WHERE id=$1`, b.personID)
				b.exec(`UPDATE agents SET status='active' WHERE id=$1`, b.personID)
			case "person_restore":
				b.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, b.person.ID)
				b.exec(`UPDATE accounts SET status='active' WHERE id=$1`, b.person.ID)
			case "context_change":
				b.exec(`UPDATE moments SET location_precision='none' WHERE id=$1`, m.ID)
			}
			receipt, e := b.store.ConsumeAgentOutboxControl(b.ctx, c)
			if !errors.Is(e, agentoutbox.ErrUnavailable) || receipt.State != agentoutbox.Invalidated || receipt.Reason != agentoutbox.ReasonSourceInvalidated {
				t.Fatalf("invalid source %s: %+v %v", change, receipt, e)
			}
			if f.count(t, "agent_effect_ledger") != 0 {
				t.Fatal("invalid source effect")
			}
		})
	}
}

func TestAgentOutboxForgedOrLateClaimDoesNotCheckpoint(t *testing.T) {
	f := newOutboxFixture(t)
	b := f.private.base
	f.create(t)
	_, c := f.claim(t, agentoutbox.HandlerV1)
	for _, change := range []string{"worker", "fence", "agent", "subject", "handler", "lease"} {
		t.Run(change, func(t *testing.T) {
			wrong := c
			switch change {
			case "worker":
				wrong.WorkerID = f.id(t)
			case "fence":
				wrong.Fence++
			case "agent":
				wrong.AgentID = b.otherID
			case "subject":
				wrong.Subject = actorref.PrincipalRef{Type: actorref.Person, ID: b.other.ID}
			case "handler":
				wrong.HandlerVersion = agentoutbox.HandlerV2
			case "lease":
				wrong.LeaseUntil = wrong.LeaseUntil.Add(time.Second)
			}
			if _, e := b.store.ConsumeAgentOutboxControl(b.ctx, wrong); !errors.Is(e, agentoutbox.ErrConflict) {
				t.Fatal("forged claim", e)
			}
		})
	}
	consumeOutboxUnavailable(t, f, c)
}

func TestAgentOutboxConsumerCheckpointCrashRollback(t *testing.T) {
	f := newOutboxFixture(t)
	b := f.private.base
	f.create(t)
	_, c := f.claim(t, agentoutbox.HandlerV1)
	remove := installOutboxFault(t, f, "agent_consumer_inbox", "AFTER", "UPDATE")
	if _, e := b.store.ConsumeAgentOutboxControl(b.ctx, c); e == nil || errors.Is(e, agentoutbox.ErrUnavailable) {
		remove()
		t.Fatal("simulated transaction failure swallowed", e)
	}
	remove()
	var state string
	if e := b.pool.QueryRow(b.ctx, `SELECT delivery_state FROM agent_domain_outbox WHERE event_id=$1`, c.EventID).Scan(&state); e != nil || state != "LEASED" {
		t.Fatal("checkpoint escaped rolled back tx", state, e)
	}
	consumeOutboxUnavailable(t, f, c)
	if f.count(t, "agent_consumer_inbox") != 1 {
		t.Fatal("crash duplicated receipt")
	}
}

func TestAgentOutboxActualLeaseTimeoutRecovery(t *testing.T) {
	f := newOutboxFixture(t)
	b := f.private.base
	f.create(t)
	_, old := f.claim(t, agentoutbox.HandlerV1)
	// Actual PG lease expiry; no trigger waiver, fake clock or forced fence.
	time.Sleep(time.Until(old.LeaseUntil) + 100*time.Millisecond)
	reopened, e := pgxpool.NewWithConfig(b.ctx, b.pool.Config().Copy())
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	restarted := New(reopened, false)
	_, fresh, e := restarted.ClaimAgentOutboxControlForSubject(b.ctx, b.person, f.id(t), agentoutbox.HandlerV1)
	if e != nil || fresh.Fence != old.Fence+1 {
		t.Fatal("durable control recovery", e)
	}
	if _, e = restarted.ConsumeAgentOutboxControl(b.ctx, old); !errors.Is(e, agentoutbox.ErrConflict) {
		t.Fatal("old worker accepted", e)
	}
	if _, e = restarted.ConsumeAgentOutboxControl(b.ctx, fresh); !errors.Is(e, agentoutbox.ErrUnavailable) {
		t.Fatal(e)
	}
	if f.count(t, "agent_consumer_inbox") != 1 || f.count(t, "agent_effect_ledger") != 0 {
		t.Fatal("restart effects/receipts")
	}
}

func TestAgentOutboxExpiryUsesActualNativeClock(t *testing.T) {
	f := newOutboxFixture(t)
	b := f.private.base
	tx, e := b.pool.BeginTx(b.ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(b.ctx)
	var id string
	e = tx.QueryRow(b.ctx, `INSERT INTO moments(author_account_id,city_id,title,body,time_precision,location_precision,created_at,updated_at)
 VALUES($1,'aberdeen-gb','自有历史原生源','非生产','unknown','city',clock_timestamp()-interval '14 minutes 59 seconds',clock_timestamp()-interval '14 minutes 59 seconds') RETURNING id::text`, b.person.ID).Scan(&id)
	if e != nil {
		t.Fatal(e)
	}
	if e = appendMomentOutboxTx(b.ctx, tx, id, agentoutbox.MomentCreated); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(b.ctx); e != nil {
		t.Fatal(e)
	}
	_, claim := f.claim(t, agentoutbox.HandlerV1)
	time.Sleep(time.Until(claim.LeaseUntil) + 100*time.Millisecond)
	if _, e = b.store.ConsumeAgentOutboxControl(b.ctx, claim); !errors.Is(e, agentoutbox.ErrExpired) {
		t.Fatal("expired lease checkpoint", e)
	}
	if _, _, e = b.store.ClaimAgentOutboxControlForSubject(b.ctx, b.person, f.id(t), agentoutbox.HandlerV1); !errors.Is(e, agentoutbox.ErrUnavailable) {
		t.Fatal("expired source refreshed", e)
	}
	var state string
	if e = b.pool.QueryRow(b.ctx, `SELECT control_state FROM agent_consumer_inbox WHERE event_id=$1`, claim.EventID).Scan(&state); e != nil || state != "EXPIRED" {
		t.Fatal("expired control not closed", state, e)
	}
}

func TestAgentOutboxWritersAndControlOverridePoolRepeatableRead(t *testing.T) {
	f := newOutboxFixture(t)
	b := f.private.base
	cfg := b.pool.Config().Copy()
	cfg.ConnConfig.RuntimeParams["default_transaction_isolation"] = "repeatable read"
	pool, e := pgxpool.NewWithConfig(b.ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	s := New(pool, false)
	m, e := s.CreateMomentDraft(b.ctx, b.person.ID, outboxMomentInput())
	if e != nil {
		t.Fatal(e)
	}
	_, claim, e := s.ClaimAgentOutboxControlForSubject(b.ctx, b.person, f.id(t), agentoutbox.HandlerV1)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ConsumeAgentOutboxControl(b.ctx, claim); !errors.Is(e, agentoutbox.ErrUnavailable) {
		t.Fatal(e)
	}
	if _, e = s.UpdateMomentDraft(b.ctx, b.person.ID, m.ID, 1, outboxMomentInput()); e != nil {
		t.Fatal(e)
	}
	if e = s.WithdrawMoment(b.ctx, b.person.ID, m.ID, 2); e != nil {
		t.Fatal(e)
	}
}

func TestAgentOutboxImmutableAndNativeSQLGuards(t *testing.T) {
	f := newOutboxFixture(t)
	b := f.private.base
	m := f.create(t)
	r := f.row(t, m.ID, 1)
	for _, q := range []string{`UPDATE agent_domain_outbox SET source_revision=2 WHERE event_id=$1`, `UPDATE agent_domain_outbox SET expires_at=expires_at+interval '1 second' WHERE event_id=$1`, `UPDATE agent_domain_outbox SET source_fingerprint=repeat('0',64) WHERE event_id=$1`, `UPDATE agent_domain_outbox SET delivery_state='LEASED',lease_owner=gen_random_uuid(),lease_until=clock_timestamp()+interval '1 second' WHERE event_id=$1`} {
		if _, e := b.pool.Exec(b.ctx, q, r.Event.EventID); e == nil {
			t.Fatal("SQL mutation guard bypass")
		}
	}
	_, claim := f.claim(t, agentoutbox.HandlerV1)
	consumeOutboxUnavailable(t, f, claim)
	if _, e := b.pool.Exec(b.ctx, `UPDATE agent_consumer_inbox SET control_state='LEASED',reason_code='' WHERE event_id=$1`, r.Event.EventID); e == nil {
		t.Fatal("terminal consumer revived")
	}
	raw, e := json.Marshal(claim)
	if e == nil || len(raw) != 0 {
		t.Fatal("wire claim self-authority")
	}
}

func TestAgentOutboxClaimFinalClockRejectsDelayedReceipt(t *testing.T) {
	f := newOutboxFixture(t)
	b := f.private.base
	tx, e := b.pool.BeginTx(b.ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(b.ctx)
	var id string
	e = tx.QueryRow(b.ctx, `INSERT INTO moments(author_account_id,city_id,title,body,time_precision,location_precision,created_at,updated_at)
 VALUES($1,'aberdeen-gb','真实限期来源','非生产','unknown','city',clock_timestamp()-interval '14 minutes 58 seconds',clock_timestamp()-interval '14 minutes 58 seconds') RETURNING id::text`, b.person.ID).Scan(&id)
	if e != nil {
		t.Fatal(e)
	}
	if e = appendMomentOutboxTx(b.ctx, tx, id, agentoutbox.MomentCreated); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(b.ctx); e != nil {
		t.Fatal(e)
	}
	name := "test_outbox_wait_" + strings.ReplaceAll(f.id(t), "-", "")
	q := fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.subject_id='%s'::uuid THEN PERFORM pg_sleep(2.4); END IF; RETURN NEW; END $$; CREATE TRIGGER %s AFTER INSERT ON agent_consumer_inbox FOR EACH ROW EXECUTE FUNCTION %s()`, name, b.person.ID, name, name)
	b.exec(q)
	defer func() { b.exec(fmt.Sprintf(`DROP TRIGGER %s ON agent_consumer_inbox; DROP FUNCTION %s()`, name, name)) }()
	if _, _, e = b.store.ClaimAgentOutboxControlForSubject(b.ctx, b.person, f.id(t), agentoutbox.HandlerV1); !errors.Is(e, agentoutbox.ErrExpired) {
		t.Fatalf("delayed claim committed stale lease: %v", e)
	}
	r := f.row(t, id, 1)
	if r.State != agentoutbox.Pending || r.Fence != 0 || f.count(t, "agent_consumer_inbox") != 0 {
		t.Fatalf("late claim escaped rollback: %+v", r)
	}
}

func TestAgentOutboxSubjectSelectorKeepsOtherJobs(t *testing.T) {
	other := newOutboxFixture(t)
	foreign := other.create(t)
	f := newOutboxFixture(t)
	b := f.private.base
	own := f.create(t)
	r, c, e := b.store.ClaimAgentOutboxControlForSubject(b.ctx, b.person, f.id(t), agentoutbox.HandlerV1)
	if e != nil || r.Event.Source.ID != own.ID || r.Event.Subject != b.person {
		t.Fatalf("wrong subject job: %+v %v", r, e)
	}
	if other.row(t, foreign.ID, 1).State != agentoutbox.Pending {
		t.Fatal("foreign control job changed")
	}
	consumeOutboxUnavailable(t, f, c)
	for _, subject := range []actorref.PrincipalRef{b.org, b.business, {Type: actorref.Person, ID: "00000000-0000-0000-0000-000000000000"}} {
		if _, _, e = b.store.ClaimAgentOutboxControlForSubject(b.ctx, subject, f.id(t), agentoutbox.HandlerV1); !errors.Is(e, agentoutbox.ErrInvalid) {
			t.Fatal("invalid typed maintenance filter", e)
		}
	}
}

func TestAgentOutboxConsumeFinalClockRejectsDelayedCheckpoint(t *testing.T) {
	f := newOutboxFixture(t)
	b := f.private.base
	tx, e := b.pool.BeginTx(b.ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(b.ctx)
	var id string
	e = tx.QueryRow(b.ctx, `INSERT INTO moments(author_account_id,city_id,title,body,time_precision,location_precision,created_at,updated_at)
 VALUES($1,'aberdeen-gb','真实消费限期来源','非生产','unknown','city',clock_timestamp()-interval '14 minutes 58 seconds',clock_timestamp()-interval '14 minutes 58 seconds') RETURNING id::text`, b.person.ID).Scan(&id)
	if e != nil {
		t.Fatal(e)
	}
	if e = appendMomentOutboxTx(b.ctx, tx, id, agentoutbox.MomentCreated); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(b.ctx); e != nil {
		t.Fatal(e)
	}
	_, claim := f.claim(t, agentoutbox.HandlerV1)
	name := "test_outbox_wait_" + strings.ReplaceAll(f.id(t), "-", "")
	b.exec(fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.subject_id='%s'::uuid THEN PERFORM pg_sleep(2.4); END IF; RETURN NEW; END $$; CREATE TRIGGER %s AFTER UPDATE ON agent_consumer_inbox FOR EACH ROW EXECUTE FUNCTION %s()`, name, b.person.ID, name, name))
	if _, e = b.store.ConsumeAgentOutboxControl(b.ctx, claim); !errors.Is(e, agentoutbox.ErrExpired) {
		t.Fatalf("late checkpoint accepted: %v", e)
	}
	b.exec(fmt.Sprintf(`DROP TRIGGER %s ON agent_consumer_inbox; DROP FUNCTION %s()`, name, name))
	if r := f.row(t, id, 1); r.State != agentoutbox.Leased {
		t.Fatal("late control checkpoint escaped rollback")
	}
	if _, _, e = b.store.ClaimAgentOutboxControlForSubject(b.ctx, b.person, f.id(t), agentoutbox.HandlerV1); !errors.Is(e, agentoutbox.ErrUnavailable) {
		t.Fatal("expired control renewed", e)
	}
	if f.row(t, id, 1).State != agentoutbox.Expired || f.count(t, "agent_effect_ledger") != 0 {
		t.Fatal("expiry failed closed")
	}
}
