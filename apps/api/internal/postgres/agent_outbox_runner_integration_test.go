package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentoutbox"
	maint "github.com/birdtie/birdtie/apps/api/internal/agentoutboxmaintenance"
	"github.com/jackc/pgx/v5/pgxpool"
)

func maintenanceBinary(t *testing.T) string {
	t.Helper()
	p := os.Getenv("BIRDTIE_OUTBOX_CONTROL_BINARY")
	if p == "" {
		t.Fatal("native maintenance requires a built BIRDTIE_OUTBOX_CONTROL_BINARY; binary absence is not a skip")
	}
	if stat, e := os.Stat(p); e != nil || stat.IsDir() {
		t.Fatal("native maintenance binary is unavailable")
	}
	return p
}

func maintenanceCommand(binary, subject string, handler agentoutbox.HandlerVersion) (maint.Report, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, binary, "--local-development-only", "--subject", subject, "--handler", string(handler))
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	e := c.Run()
	code := 0
	if e != nil {
		var exit *exec.ExitError
		if errors.As(e, &exit) {
			code = exit.ExitCode()
		} else {
			return maint.Report{}, -1, errors.New("command launch failed")
		}
	}
	if stderr.Len() != 0 {
		return maint.Report{}, code, errors.New("unexpected command stderr")
	}
	var r maint.Report
	decoder := json.NewDecoder(&stdout)
	decoder.DisallowUnknownFields()
	if e = decoder.Decode(&r); e != nil {
		return r, code, errors.New("command did not emit the closed aggregate report")
	}
	if r.Schema != "birdtie.outbox-maintenance.v1" || r.BusinessExecution != "UNAVAILABLE" {
		return r, code, errors.New("invalid aggregate schema")
	}
	return r, code, nil
}

func nativeMaintenanceOptions(t *testing.T, f *outboxFixture) maint.Options {
	return maint.Options{Subject: f.private.base.person, WorkerID: f.id(t), Handler: agentoutbox.HandlerV1, Batch: 25, Timeout: time.Second}
}

func TestAgentOutboxMaintenanceNativeActualCommand100AndVersion(t *testing.T) {
	f := newOutboxFixture(t)
	binary := maintenanceBinary(t)
	m := f.create(t)
	var receiptBytes string
	for i := 0; i < 100; i++ {
		r, code, e := maintenanceCommand(binary, f.private.base.person.ID, agentoutbox.HandlerV1)
		want := 0
		if i == 0 {
			want = 1
		}
		if e != nil || code != 0 || r.Status != "FINISHED" || r.ConfirmedReceipts != want || r.Counts.Unavailable != want {
			t.Fatalf("run %d: %+v code%d %v", i, r, code, e)
		}
		var raw string
		if e := f.private.base.pool.QueryRow(f.private.base.ctx, `SELECT jsonb_agg(to_jsonb(i) ORDER BY handler_version)::text FROM agent_consumer_inbox i WHERE subject_id=$1`, f.private.base.person.ID).Scan(&raw); e != nil {
			t.Fatal(e)
		}
		if i == 0 {
			receiptBytes = raw
		} else if raw != receiptBytes {
			t.Fatal("same-version replay changed committed receipt")
		}
	}
	r, code, e := maintenanceCommand(binary, f.private.base.person.ID, agentoutbox.HandlerV2)
	if e != nil || code != 0 || r.ConfirmedReceipts != 1 || f.count(t, "agent_consumer_inbox") != 2 || f.count(t, "agent_effect_ledger") != 0 {
		t.Fatal(r, code, e)
	}
	for _, handler := range []agentoutbox.HandlerVersion{agentoutbox.HandlerV2, agentoutbox.HandlerV1} {
		r, code, e = maintenanceCommand(binary, f.private.base.person.ID, handler)
		if e != nil || code != 0 || r.ConfirmedReceipts != 0 || f.count(t, "agent_consumer_inbox") != 2 {
			t.Fatal("handler replay changed control", r, code, e)
		}
	}
	var title, body string
	if e = f.private.base.pool.QueryRow(f.private.base.ctx, `SELECT title,body FROM moments WHERE id=$1`, m.ID).Scan(&title, &body); e != nil || title != m.Title || body != m.Body {
		t.Fatal("control changed private source", e)
	}
}

func TestAgentOutboxMaintenanceNativeTwoCommandWorkers(t *testing.T) {
	f := newOutboxFixture(t)
	binary := maintenanceBinary(t)
	for i := 0; i < 4; i++ {
		f.create(t)
	}
	var wg sync.WaitGroup
	type result struct {
		r    maint.Report
		code int
		e    error
	}
	out := make(chan result, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, c, e := maintenanceCommand(binary, f.private.base.person.ID, agentoutbox.HandlerV1)
			out <- result{r, c, e}
		}()
	}
	wg.Wait()
	close(out)
	sum := 0
	for p := range out {
		if p.e != nil || p.code != 0 || p.r.Status != "FINISHED" {
			t.Fatal(p.r, p.code, p.e)
		}
		sum += p.r.ConfirmedReceipts
	}
	if sum != 4 || f.count(t, "agent_consumer_inbox") != 4 || f.count(t, "agent_effect_ledger") != 0 {
		t.Fatal("duplicate/lost control", sum)
	}
}

// A real child claims and exits without Consume. No serialized claim crosses
// process boundaries; the parent observes the original native row in its DB.
func TestAgentOutboxMaintenanceHelperProcess(t *testing.T) {
	if os.Getenv("BIRDTIE_OUTBOX_HELPER_MODE") != "claim" {
		return
	}
	if os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" {
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p, e := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		os.Exit(3)
	}
	subject, e := actorref.ParsePrincipal("PERSON", os.Getenv("BIRDTIE_OUTBOX_HELPER_SUBJECT"))
	if e != nil {
		os.Exit(4)
	}
	_, _, e = New(p, false).ClaimAgentOutboxControlForSubject(ctx, subject, os.Getenv("BIRDTIE_OUTBOX_HELPER_WORKER"), agentoutbox.HandlerV1)
	if e != nil {
		os.Exit(5)
	}
	fmt.Print("CLAIMED")
	os.Exit(0)
}

func TestAgentOutboxMaintenanceNativeProcessExitAndLeaseRecovery(t *testing.T) {
	f := newOutboxFixture(t)
	binary := maintenanceBinary(t)
	m := f.create(t)
	worker := f.id(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAgentOutboxMaintenanceHelperProcess$")
	child.Env = append(os.Environ(), "BIRDTIE_OUTBOX_HELPER_MODE=claim", "BIRDTIE_OUTBOX_HELPER_SUBJECT="+f.private.base.person.ID, "BIRDTIE_OUTBOX_HELPER_WORKER="+worker)
	out, e := child.CombinedOutput()
	if e != nil || string(out) != "CLAIMED" {
		t.Fatal("real claim child failed")
	}
	r := f.row(t, m.ID, 1)
	if r.State != agentoutbox.Leased || r.LeaseUntil == nil || r.LeaseOwner != worker {
		t.Fatal("child did not leave actual lease")
	}
	old := agentoutbox.Claim{EventID: r.Event.EventID, Subject: r.Event.Subject, AgentID: r.Event.AgentID, WorkerID: worker, HandlerVersion: agentoutbox.HandlerV1, Fence: r.Fence, LeaseUntil: *r.LeaseUntil}
	time.Sleep(time.Until(*r.LeaseUntil) + 100*time.Millisecond)
	p, code, e := maintenanceCommand(binary, f.private.base.person.ID, agentoutbox.HandlerV1)
	if e != nil || code != 0 || p.ConfirmedReceipts != 1 {
		t.Fatal(p, code, e)
	}
	if _, e = f.private.base.store.ConsumeAgentOutboxControl(f.private.base.ctx, old); !errors.Is(e, agentoutbox.ErrConflict) {
		t.Fatal("old fence accepted", e)
	}
	next := f.row(t, m.ID, 1)
	if next.Fence != r.Fence+1 || f.count(t, "agent_consumer_inbox") != 1 || f.count(t, "agent_effect_ledger") != 0 {
		t.Fatal("restart duplicated control")
	}
}

func TestAgentOutboxMaintenanceNativeSourceChangeStopsEmptyUnavailable(t *testing.T) {
	f := newOutboxFixture(t)
	m := f.create(t)
	b := f.private.base
	in := outboxMomentInput()
	in.Title = "明确的新版本本地来源"
	if _, e := b.store.UpdateMomentDraft(b.ctx, b.person.ID, m.ID, 1, in); e != nil {
		t.Fatal(e)
	}
	old := f.row(t, m.ID, 1)
	if old.State != agentoutbox.Invalidated || old.Attempt != 0 || old.Fence != 0 || f.row(t, m.ID, 2).State != agentoutbox.Pending {
		t.Fatal("refresh must retire only the unclaimed old control")
	}
	r := maint.Run(b.ctx, b.store, nativeMaintenanceOptions(t, f))
	if r.Status != "FINISHED" || r.Stage != "CLAIM" || r.Reason != "NO_ELIGIBLE_WORK" || r.ConfirmedReceipts != 1 || f.row(t, m.ID, 2).State != agentoutbox.Unavailable || f.count(t, "agent_effect_ledger") != 0 {
		t.Fatal(r)
	}
	if !reflect.DeepEqual(old, f.row(t, m.ID, 1)) {
		t.Fatal("maintenance rewrote already coalesced immutable event or control")
	}
	r = maint.Run(b.ctx, b.store, nativeMaintenanceOptions(t, f))
	if r.Status != "FINISHED" || r.Reason != "NO_ELIGIBLE_WORK" || r.ConfirmedReceipts != 0 || f.count(t, "agent_effect_ledger") != 0 {
		t.Fatal("same handler replay invented another receipt", r)
	}
}

type maintenanceUnknownReply struct {
	*Store
	calls int
}

type maintenanceBeforeConsume struct {
	*Store
	before func()
}

func (s *maintenanceBeforeConsume) ConsumeAgentOutboxControl(ctx context.Context, c agentoutbox.Claim) (agentoutbox.ConsumerRecord, error) {
	s.before()
	return s.Store.ConsumeAgentOutboxControl(ctx, c)
}

func TestAgentOutboxMaintenanceNativeCurrentSourceRecheckedBeforeConsume(t *testing.T) {
	f := newOutboxFixture(t)
	m := f.create(t)
	b := f.private.base
	w := &maintenanceBeforeConsume{Store: b.store, before: func() {
		in := outboxMomentInput()
		in.Title = "领取后明确修改的新版本"
		if _, e := b.store.UpdateMomentDraft(b.ctx, b.person.ID, m.ID, 1, in); e != nil {
			t.Fatal(e)
		}
	}}
	o := nativeMaintenanceOptions(t, f)
	o.Batch = 1
	r := maint.Run(b.ctx, w, o)
	if r.Status != "FINISHED" || r.ConfirmedReceipts != 1 || r.Counts.Invalidated != 1 || r.Counts.Unavailable != 0 || f.row(t, m.ID, 1).State != agentoutbox.Invalidated || f.row(t, m.ID, 2).State != agentoutbox.Pending || f.count(t, "agent_effect_ledger") != 0 {
		t.Fatal(r)
	}
}

func (s *maintenanceUnknownReply) ConsumeAgentOutboxControl(ctx context.Context, c agentoutbox.Claim) (agentoutbox.ConsumerRecord, error) {
	s.calls++
	_, e := s.Store.ConsumeAgentOutboxControl(ctx, c)
	if e != agentoutbox.ErrUnavailable {
		return agentoutbox.ConsumerRecord{}, e
	}
	return agentoutbox.ConsumerRecord{}, errors.New("PRIVATE_UNKNOWN_COMMIT")
}

func TestAgentOutboxMaintenanceNativeUnknownCommittedReplyStops(t *testing.T) {
	f := newOutboxFixture(t)
	f.create(t)
	second := f.create(t)
	b := f.private.base
	w := &maintenanceUnknownReply{Store: b.store}
	r := maint.Run(b.ctx, w, nativeMaintenanceOptions(t, f))
	wire, _ := json.Marshal(r)
	if r.Status != "STOPPED" || r.ConfirmedReceipts != 0 || w.calls != 1 || strings.Contains(string(wire), "PRIVATE") || f.count(t, "agent_consumer_inbox") != 1 || f.row(t, second.ID, 1).State != agentoutbox.Pending {
		t.Fatal(r)
	}
	r = maint.Run(b.ctx, b.store, nativeMaintenanceOptions(t, f))
	if r.ConfirmedReceipts != 1 || f.count(t, "agent_consumer_inbox") != 2 || f.count(t, "agent_effect_ledger") != 0 {
		t.Fatal(r)
	}
}

func TestAgentOutboxMaintenanceNativeExpiryAndPoolDeadline(t *testing.T) {
	t.Run("naturalSourceExpiry", func(t *testing.T) {
		f := newOutboxFixture(t)
		b := f.private.base
		tx, e := b.pool.Begin(b.ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer tx.Rollback(b.ctx)
		var id string
		e = tx.QueryRow(b.ctx, `WITH clock AS MATERIALIZED (SELECT clock_timestamp() AS at) INSERT INTO moments(author_account_id,city_id,title,body,time_precision,location_precision,created_at,updated_at) SELECT $1,'aberdeen-gb','合成本地限期来源','未经运营核验','unknown','city',at-interval '14 minutes 59 seconds',at-interval '14 minutes 59 seconds' FROM clock RETURNING id::text`, b.person.ID).Scan(&id)
		if e != nil {
			t.Fatal(e)
		}
		if e = appendMomentOutboxTx(b.ctx, tx, id, agentoutbox.MomentCreated); e != nil {
			t.Fatal(e)
		}
		if e = tx.Commit(b.ctx); e != nil {
			t.Fatal(e)
		}
		r := f.row(t, id, 1)
		time.Sleep(time.Until(r.Event.ExpiresAt) + 100*time.Millisecond)
		p := maint.Run(b.ctx, b.store, nativeMaintenanceOptions(t, f))
		if p.Status != "STOPPED" || p.Reason != "CLAIM_UNCONFIRMED" || p.ConfirmedReceipts != 0 || f.row(t, id, 1).State != agentoutbox.Expired || f.count(t, "agent_effect_ledger") != 0 {
			t.Fatal(p)
		}
	})
	t.Run("poolWait", func(t *testing.T) {
		f := newOutboxFixture(t)
		m := f.create(t)
		b := f.private.base
		cfg := b.pool.Config().Copy()
		cfg.MaxConns = 1
		cfg.MinConns = 0
		pool, e := pgxpool.NewWithConfig(b.ctx, cfg)
		if e != nil {
			t.Fatal(e)
		}
		defer pool.Close()
		hold, e := pool.Acquire(b.ctx)
		if e != nil {
			t.Fatal(e)
		}
		o := nativeMaintenanceOptions(t, f)
		o.Timeout = 30 * time.Millisecond
		r := maint.Run(b.ctx, New(pool, false), o)
		hold.Release()
		if r.Status != "STOPPED" || r.Reason != "DEADLINE_EXCEEDED" || r.ConfirmedReceipts != 0 || f.row(t, m.ID, 1).State != agentoutbox.Pending || f.count(t, "agent_consumer_inbox") != 0 {
			t.Fatal(r)
		}
	})
}
