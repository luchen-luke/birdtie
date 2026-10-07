package postgres

import (
	"context"
	"encoding/json"
	"errors"
	aem "github.com/birdtie/birdtie/apps/api/internal/agentenrichmentmetrics"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentnotification"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/agentrun"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"github.com/jackc/pgx/v5"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func enrichmentMetricsRead(t *testing.T, f *agentPrivateFixture) aem.Snapshot {
	t.Helper()
	v, e := f.base.store.ReadOwnEnrichmentMetrics(f.base.ctx, f.owner, aem.Window{})
	if e != nil || v.SchemaVersion != aem.Schema || len(v.Metrics) != 6 || v.HistoricalClassification != "UNKNOWN_UNCLASSIFIED_NOT_BACKFILLED" {
		t.Fatal(v, e)
	}
	return v
}

func TestEnrichmentMetricsNativeTraceWindowCurrentClockAndSensitiveCanaries(t *testing.T) {
	ownedMigrationDatabase(t)
	f := newEgressFixture(t, 4)
	b := f.f.native.private.base
	// These values really live in private native rows and are NOT provided as a
	// fabricated trace response. They are never selected by the metadata reader.
	canaries := []string{"PRIVATE_QUERY_METRICS_CANARY", "bts1_SECRET_BEARER_CANARY", "API_KEY_METRICS_CANARY", "PHOTO_METRICS_CANARY", "PRIVATE_CHAT_METRICS_CANARY", "HIDDEN_COT_METRICS_CANARY", f.f.native.task.Query, f.f.native.moment.Body, f.f.native.task.Conversation[0].Text, f.f.native.private.ownerSession}

	// A real current query change requires its original native revision; binding
	// and approval remain the pre-existing concrete source and are not recaptured.
	p := f.preview(t, f.f.native.task.ID, true)
	id := modelRunID(t, f)
	_, c := modelRunPlan(t, f, id, modelRunBindings(t, f, p, 1))
	current := savePrivateCanaries(t, f.f.native.private)
	fields := current.Fields
	fields.AgentNotes = strings.Join(canaries, "|")
	if _, e := b.store.ReplaceOwnAgentPrivateProfile(b.ctx, f.f.native.private.owner, agentprofile.ReplacePrivateInput{ExpectedVersion: current.Profile.ProfileVersion, Fields: fields}); e != nil {
		t.Fatal(e)
	}
	v, e := b.store.ReadOwnEnrichmentTraceWithinWindow(b.ctx, f.f.native.private.owner, "MODEL_REQUEST", id, 3*time.Second)
	if e != nil || v.ValidUntil.After(c.CreatedAt.Add(3*time.Second)) {
		t.Fatal(v, e)
	}
	raw, _ := json.Marshal(v)
	for _, s := range canaries {
		if strings.Contains(string(raw), s) {
			t.Fatal("real private source canary in trace")
		}
	}
	if _, e = b.store.ReadOwnEnrichmentTraceWithinWindow(b.ctx, f.f.native.private.owner, "MODEL_REQUEST", id, aem.DefaultRetention+time.Second); !errors.Is(e, aem.ErrInvalid) {
		t.Fatal("window can extend", e)
	}
	ctx, cancel := context.WithTimeout(b.ctx, 8*time.Second)
	defer cancel()
	hold, e := b.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer hold.Rollback(context.Background())
	if _, e = hold.Exec(ctx, `LOCK TABLE model_local_price_versions IN ACCESS EXCLUSIVE MODE`); e != nil {
		t.Fatal(e)
	}
	var pid int
	if e = hold.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() {
		v, e := b.store.ReadOwnEnrichmentTraceWithinWindow(ctx, f.f.native.private.owner, "MODEL_REQUEST", id, 3*time.Second)
		if e == nil && v.RunID != "" {
			done <- errors.New("expired trace released")
			return
		}
		done <- e
	}()
	// Match the actual metadata reader, not an unrelated package's waiter.
	for {
		var waiting bool
		if e = b.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)) AND query LIKE '%FROM model_local_price_versions WHERE version%')`, pid).Scan(&waiting); e != nil {
			t.Fatal(e)
		}
		if waiting {
			break
		}
		select {
		case e := <-done:
			t.Fatal("reader finished before barrier", e)
		default:
		}
		if ctx.Err() != nil {
			t.Fatal(ctx.Err())
		}
		time.Sleep(5 * time.Millisecond)
	}
	for {
		var now time.Time
		if e = b.pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
			t.Fatal(e)
		}
		if !now.Before(c.CreatedAt.Add(3 * time.Second)) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	hold.Rollback(ctx)
	select {
	case e := <-done:
		if !errors.Is(e, aem.ErrNotFound) {
			t.Fatal("finite trace lease not enforced", e)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if _, e = b.store.ReadOwnEnrichmentTraceWithinWindow(b.ctx, f.f.native.private.owner, "MODEL_REQUEST", id, 3*time.Second); !errors.Is(e, aem.ErrNotFound) {
		t.Fatal("expired trace read", e)
	}
	// Default finite historical metadata still reads; it grants no model call.
	if _, e = b.store.ReadOwnEnrichmentTrace(b.ctx, f.f.native.private.owner, "MODEL_REQUEST", id); e != nil {
		t.Fatal(e)
	}
}

func TestEnrichmentMetricsNativeCurrencyIsolationAndKnownUnknownOriginalFees(t *testing.T) {
	ownedMigrationDatabase(t)
	f := newEgressFixture(t, 4)
	b := f.f.native.private.base
	p := f.preview(t, f.f.native.task.ID, true)
	id := modelRunID(t, f)
	bind := modelRunBindings(t, f, p, 1)
	h, _ := modelRunPlan(t, f, id, bind)
	if _, e := h.ReserveOwnModelAttempt(b.ctx, f.f.native.access, bind[0].Input, f.gate); e != nil {
		t.Fatal(e)
	}
	if _, e := h.BeginOwnLocalModelAttempt(b.ctx, f.f.native.access, bind[0].Input.OperationID, f.gate); e != nil {
		t.Fatal(e)
	}
	if _, e := b.store.SettleOwnLocalModelAttempt(b.ctx, f.f.native.access, bind[0].Input.OperationID, modelegressbudget.LocalUsage{}); e != nil {
		t.Fatal(e)
	}
	v, e := b.store.ReadOwnEnrichmentTrace(b.ctx, f.f.native.private.owner, "MODEL_REQUEST", id)
	if e != nil || v.Steps[0].ActualCostMicros != nil || v.Steps[0].UsageStatus != "UNKNOWN_NOT_SETTLED" || v.Steps[0].HeldUpperCostMicros <= 0 {
		t.Fatal(v, e)
	}
	if _, e = b.store.SettleOwnLocalModelAttempt(b.ctx, f.f.native.access, bind[0].Input.OperationID, egressUsage(t, 10, 5)); e != nil {
		t.Fatal(e)
	}
	v, e = b.store.ReadOwnEnrichmentTrace(b.ctx, f.f.native.private.owner, "MODEL_REQUEST", id)
	if e != nil || v.Steps[0].ActualCostMicros == nil || *v.Steps[0].ActualCostMicros != 35 || v.Steps[0].Evidence != "LOCAL_SYNTHETIC" {
		t.Fatal(v, e)
	}
	gbp, e := b.store.ReadOwnEnrichmentBudgetAlerts(b.ctx, f.f.native.private.owner, f.root, f.f.native.task.ID)
	if e != nil || gbp.Monthly.Currency != "GBP" || gbp.Monthly.KnownActualMicros != 35 {
		t.Fatal(gbp, e)
	}
	price := f.price
	price.Version += "_usd"
	price.Currency = "USD"
	if e = b.store.RegisterLocalModelPrice(b.ctx, price); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if _, e := b.pool.Exec(context.Background(), `DELETE FROM model_local_price_versions WHERE version=$1`, price.Version); e != nil {
			t.Error(e)
		}
	})
	// Original062 single-owner currency is immutable. Do not bypass this guard
	// to fabricate a same-owner mixed-currency production capability.
	before := modelRunAccountingSnapshot(t, f, id)
	if e = b.store.ConfigureOwnModelBudget(b.ctx, f.f.native.access, f.f.native.task.ID, f.binding, "USD", f.limits, f.limits); !errors.Is(e, modelegressbudget.ErrConflict) {
		t.Fatal("original currency immutability lost", e)
	}
	again, e := b.store.ReadOwnEnrichmentBudgetAlerts(b.ctx, f.f.native.private.owner, f.root, f.f.native.task.ID)
	if e != nil || again.Monthly != gbp.Monthly || before != modelRunAccountingSnapshot(t, f, id) {
		t.Fatal("currency rejection/observation rewrote original accounting", again, e)
	}

}

func TestEnrichmentMetricsNativeOldNonemptyMemoryAuditUpDownReapplyNoBackfill(t *testing.T) {
	ownedMigrationDatabase(t)
	f := agentMemoryTestFixture(t)
	b := f.base
	r := mustPutAgentMemory(t, f, agentMemoryID(t, f), agentMemoryInput("old-unknown"))
	b.exec(`DELETE FROM agent_enrichment_observations WHERE owner_id=$1`, b.person.ID)
	var memory, audit string
	if e := b.pool.QueryRow(b.ctx, `SELECT to_jsonb(m)::text||m.xmin::text FROM agent_memories m WHERE id=$1`, r.ID).Scan(&memory); e != nil {
		t.Fatal(e)
	}
	if e := b.pool.QueryRow(b.ctx, `SELECT to_jsonb(a)::text||a.xmin::text FROM audit_events a WHERE actor_account_id=$1 AND resource_id=$2 ORDER BY id LIMIT 1`, b.person.ID, r.ID).Scan(&audit); e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile(filepath.Join("..", "..", "migrations", "089_agent_enrichment_observability.down.sql"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = b.pool.Exec(b.ctx, string(raw)); e != nil {
		t.Fatal(e)
	}
	// Real old nonempty Memory/audit remains on schema88. 089 only adds an empty
	// projection; no UPDATE/retag/backfill may alter these original row versions.
	raw, e = os.ReadFile(filepath.Join("..", "..", "migrations", "089_agent_enrichment_observability.sql"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = b.pool.Exec(b.ctx, string(raw)); e != nil {
		t.Fatal(e)
	}
	var m2, a2 string
	b.pool.QueryRow(b.ctx, `SELECT to_jsonb(m)::text||m.xmin::text FROM agent_memories m WHERE id=$1`, r.ID).Scan(&m2)
	b.pool.QueryRow(b.ctx, `SELECT to_jsonb(a)::text||a.xmin::text FROM audit_events a WHERE actor_account_id=$1 AND resource_id=$2 ORDER BY id LIMIT 1`, b.person.ID, r.ID).Scan(&a2)
	if memory != m2 || audit != a2 {
		t.Fatal("old Memory/audit rows or xmin changed")
	}
	v := enrichmentMetricsRead(t, f)
	if v.LegacyUnclassified != 1 || enrichmentMetricCount(v, aem.Created) != 0 {
		t.Fatal("historical state counted as creation", v)
	}
}
func enrichmentMetricCount(v aem.Snapshot, m string) int64 {
	for _, n := range v.Metrics {
		if n.Kind == m {
			return n.Count
		}
	}
	return -1
}
func TestEnrichmentMetricsNativeMemoryFactsRetryRollbackAndLegacyUnknown(t *testing.T) {
	ownedMigrationDatabase(t)
	f := agentMemoryTestFixture(t)
	b := f.base
	savePrivateCanaries(t, f)
	id := agentMemoryID(t, f)
	in := agentMemoryInput("observation")
	in.Summary = "PRIVATE_METRIC_MEMORY_BODY_CANARY"
	r := mustPutAgentMemory(t, f, id, in)
	for range 3 {
		mustPutAgentMemory(t, f, id, in)
	}
	v := enrichmentMetricsRead(t, f)
	if enrichmentMetricCount(v, aem.Created) != 1 || enrichmentMetricCount(v, aem.Corrected) != 0 {
		t.Fatal(v)
	}
	in.ExpectedVersion = r.Version
	in.Summary = "PRIVATE_CORRECTED_CANARY"
	r = mustPutAgentMemory(t, f, id, in)
	if _, e := b.store.DeleteOwnMemory(b.ctx, f.owner, id, r.Version); e != nil {
		t.Fatal(e)
	}
	if _, e := b.store.DeleteOwnMemory(b.ctx, f.owner, id, r.Version); e != nil {
		t.Fatal(e)
	}
	v = enrichmentMetricsRead(t, f)
	if enrichmentMetricCount(v, aem.Created) != 1 || enrichmentMetricCount(v, aem.Corrected) != 1 || enrichmentMetricCount(v, aem.Deleted) != 1 {
		t.Fatal(v)
	}
	// Old audit has no trustworthy transition classification. It is NOT inferred
	// by reading today's tombstone or by backfilling an observation.
	b.exec(`INSERT INTO audit_events(actor_account_id,action,resource_type,resource_id,decision,purpose) VALUES($1,'put','agent_memory',$2,'allowed','human_explicit_memory_edit')`, b.person.ID, id)
	v = enrichmentMetricsRead(t, f)
	if v.LegacyUnclassified != 1 {
		t.Fatal(v)
	}
	raw, _ := json.Marshal(v)
	for _, s := range []string{"PRIVATE_METRIC_MEMORY_BODY_CANARY", "PRIVATE_CORRECTED_CANARY", "合成私密", f.ownerSession, id} {
		if strings.Contains(string(raw), s) {
			t.Fatal("private canary leaked")
		}
	}
	var before int
	if e := b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_enrichment_observations WHERE owner_id=$1`, b.person.ID).Scan(&before); e != nil {
		t.Fatal(e)
	}
	tx, e := b.pool.Begin(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	bind, e := lockOwnAgentPrivateBinding(b.ctx, tx, f.owner, true)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = lockAgentPrivateMetadata(b.ctx, tx, bind, true); e != nil {
		t.Fatal(e)
	}
	_, _, e = putOwnMemoryInTx(b.ctx, tx, bind, f.owner.WorkspacePrincipal, agentMemoryID(t, f), agentMemoryInput("rollback"))
	if e != nil {
		t.Fatal(e)
	}
	tx.Rollback(b.ctx)
	var after int
	if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_enrichment_observations WHERE owner_id=$1`, b.person.ID).Scan(&after); e != nil || before != after {
		t.Fatal("rollback counted", e, before, after)
	}
	for _, a := range []agentprofile.PrivateAccess{f.peer, f.org, f.biz, {}} {
		got, e := b.store.ReadOwnEnrichmentMetrics(b.ctx, a, aem.Window{})
		if a == f.peer {
			if e != nil || enrichmentMetricCount(got, aem.Created) != 0 {
				t.Fatal(got, e)
			}
		} else if e == nil {
			t.Fatal("cross workspace accepted")
		}
	}
}
func TestEnrichmentMetricsNativeCandidateRejectAndActualPromotion(t *testing.T) {
	ownedMigrationDatabase(t)
	f, s := memoryCandidateFixture(t)
	b := f.base.private.base
	r := memoryCandidateSave(t, f, s)
	if _, e := s.RejectOwnCandidate(b.ctx, f.base.private.owner, r.ID, r.Version); e != nil {
		t.Fatal(e)
	}
	if _, e := s.RejectOwnCandidate(b.ctx, f.base.private.owner, r.ID, r.Version); e != nil {
		t.Fatal(e)
	}
	v := enrichmentMetricsRead(t, f.base.private)
	if enrichmentMetricCount(v, aem.Rejected) != 1 || enrichmentMetricCount(v, aem.Promoted) != 0 {
		t.Fatal(v)
	}
	// Distinct actual source revision avoids the old rejected-source suppression.
	b.exec(`UPDATE moments SET title=title||'更新',revision=revision+1,updated_at=clock_timestamp() WHERE id=$1`, f.sources[agentevent.MomentCreated])
	r = memoryCandidateSave(t, f, s)
	p := memoryCandidatePreview(t, f, s, r)
	if _, e := s.AcceptOwnCandidate(b.ctx, f.base.private.owner, p); e != nil {
		t.Fatal(e)
	}
	if _, e := s.AcceptOwnCandidate(b.ctx, f.base.private.owner, p); e != nil {
		t.Fatal(e)
	}
	v = enrichmentMetricsRead(t, f.base.private)
	if enrichmentMetricCount(v, aem.Promoted) != 1 || enrichmentMetricCount(v, aem.Created) != 1 {
		t.Fatal(v)
	}
}
func TestEnrichmentMetricsNativeOrganizationIsolationAndLifecycle(t *testing.T) {
	ownedMigrationDatabase(t)
	f := newOrgMemoryFixture(t)
	b := f.p.base
	id := agentMemoryID(t, f.p)
	in := orgMemoryTestInput(agentmemory.OrganizationCategories()[0], "observed")
	r, e := b.store.PutOrganizationMemory(b.ctx, f.access, id, in)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = b.store.PutOrganizationMemory(b.ctx, f.access, id, in); e != nil {
		t.Fatal(e)
	}
	in.ExpectedVersion = r.Memory.Version
	in.Summary = "ORG_PRIVATE_CANARY"
	r, e = b.store.PutOrganizationMemory(b.ctx, f.access, id, in)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = b.store.DeleteOrganizationMemory(b.ctx, f.access, id, r.Memory.Version); e != nil {
		t.Fatal(e)
	}
	v, e := b.store.ReadOrganizationEnrichmentMetrics(b.ctx, f.access, aem.Window{})
	if e != nil || enrichmentMetricCount(v, aem.Created) != 1 || enrichmentMetricCount(v, aem.Corrected) != 1 || enrichmentMetricCount(v, aem.Deleted) != 1 || enrichmentMetricCount(v, aem.PolicyTriggered) != 0 {
		t.Fatal(v, e)
	}
	if enrichmentMetricCount(enrichmentMetricsRead(t, f.p), aem.Created) != 0 {
		t.Fatal("org contaminated personal metrics")
	}
	var until time.Time
	if e = b.pool.QueryRow(b.ctx, `WITH c AS MATERIALIZED(SELECT clock_timestamp() n) UPDATE sessions SET idle_expires_at=n+interval '5 seconds' FROM c WHERE id=$1 RETURNING idle_expires_at`, f.p.ownerSession).Scan(&until); e != nil {
		t.Fatal(e)
	}
	v, e = b.store.ReadOrganizationEnrichmentMetrics(b.ctx, f.access, aem.Window{})
	if e != nil || v.ValidUntil.After(until) {
		t.Fatal("org observation outlived Session", v, e)
	}
	a := f.access
	a.ActingPersonID = b.other.ID
	a.SessionDigest = f.p.peer.SessionDigest
	if _, e = b.store.ReadOrganizationEnrichmentMetrics(b.ctx, a, aem.Window{}); !errors.Is(e, aem.ErrDenied) {
		t.Fatal("non-admin accepted")
	}
}
func TestEnrichmentMetricsNativeOriginalNotificationFiveRoutesAndNoRetryDoubleCount(t *testing.T) {
	for _, route := range []agentnotification.Route{agentnotification.Normal, agentnotification.Immediate, agentnotification.Digest, agentnotification.Silent, agentnotification.Block} {
		t.Run(string(route), func(t *testing.T) {
			ownedMigrationDatabase(t)
			f := newNotificationDeliveryFixture(t)
			b := f.private.base
			cv, _ := f.friend(t)
			f.policy(t, f.private.peer, route)
			m := f.message(t, cv)
			v, e := b.store.ReadOwnEnrichmentMetrics(b.ctx, f.private.peer, aem.Window{})
			if e != nil || enrichmentMetricCount(v, aem.PolicyTriggered) != 1 || len(v.Policies) != 1 || v.Policies[0].Disposition != string(route) {
				t.Fatal(v, e)
			}
			tx, e := b.pool.BeginTx(b.ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
			if e != nil {
				t.Fatal(e)
			}
			inserted, e := routeNativeNotification(b.ctx, tx, agentnotification.KindDirectMessage, m.ID, b.other.ID)
			if e != nil || inserted {
				t.Fatal(inserted, e)
			}
			if e = tx.Commit(b.ctx); e != nil {
				t.Fatal(e)
			}
			again, e := b.store.ReadOwnEnrichmentMetrics(b.ctx, f.private.peer, aem.Window{})
			if e != nil || enrichmentMetricCount(again, aem.PolicyTriggered) != 1 {
				t.Fatal(again, e)
			}
		})
	}
}
func TestEnrichmentMetricsNativeModelPinBudgetAlertsAndUnknownUsage(t *testing.T) {
	ownedMigrationDatabase(t)
	f := newEgressFixture(t, 5)
	b := f.f.native.private.base
	p := f.preview(t, f.f.native.task.ID, true)
	id := modelRunID(t, f)
	bindings := modelRunBindings(t, f, p, 1)
	_, c := modelRunPlan(t, f, id, bindings)
	v, e := b.store.ReadOwnEnrichmentTrace(b.ctx, f.f.native.private.owner, "MODEL_REQUEST", id)
	if e != nil || v.RunID != c.ModelRunID || v.Configuration == nil || v.Configuration.Fingerprint != f.f.configs[0].Fingerprint || len(v.Steps) != 1 || v.SourceID != nil || v.Steps[0].ActualCostMicros != nil {
		t.Fatal(v, e)
	}
	raw, _ := json.Marshal(v)
	for _, s := range []string{f.f.configs[0].Prompt.Text, "private", "query", "request_digest", "source_token", "authority_token", "token_sha256"} {
		if s != "private" && strings.Contains(string(raw), s) {
			t.Fatal("private trace field leaked")
		}
	}
	if _, e = b.store.ReadOwnEnrichmentTrace(b.ctx, f.f.native.private.peer, "MODEL_REQUEST", id); e == nil {
		t.Fatal("crossowner trace")
	}
	// Reserve original standalone operation (not a linked Run op) and keep actual
	// fee NULL until real original Settle. UNKNOWN is never reported as zero cost.
	input := nativeAttemptInput(t, f, p)
	if _, e = b.store.ReserveOwnModelAttempt(b.ctx, f.f.native.access, input, f.gate); e != nil {
		t.Fatal(e)
	}
	a, e := b.store.ReadOwnEnrichmentBudgetAlerts(b.ctx, f.f.native.private.owner, input.RootTraceID, input.TaskID)
	if e != nil || len(a.Warnings) != 16 || a.Monthly.Limit != nil || a.Monthly.LimitStatus != "UNAVAILABLE_NO_MONTHLY_QUOTA" || a.Monthly.HeldUnknownUpperMicros <= 0 {
		t.Fatal(a, e)
	}
	// Actual native allocation reached through original Reserve, not edited counters.
	for range 3 {
		in := nativeAttemptInput(t, f, p)
		if _, e = b.store.ReserveOwnModelAttempt(b.ctx, f.f.native.access, in, f.gate); e != nil {
			t.Fatal(e)
		}
	}
	a, e = b.store.ReadOwnEnrichmentBudgetAlerts(b.ctx, f.f.native.private.owner, input.RootTraceID, input.TaskID)
	if e != nil {
		t.Fatal(e)
	}
	near := 0
	for _, w := range a.Warnings {
		if w.Dimension == "REQUESTS" && w.Status == "NEAR_LIMIT" {
			near++
		}
	}
	if near != 4 {
		t.Fatal("four-layer actual80% near alert absent", a)
	}
	if _, e = b.store.ReserveOwnModelAttempt(b.ctx, f.f.native.access, nativeAttemptInput(t, f, p), f.gate); e != nil {
		t.Fatal(e)
	}
	a, e = b.store.ReadOwnEnrichmentBudgetAlerts(b.ctx, f.f.native.private.owner, input.RootTraceID, input.TaskID)
	if e != nil {
		t.Fatal(e)
	}
	found := 0
	for _, w := range a.Warnings {
		if w.Dimension == "REQUESTS" && w.Status == "EXHAUSTED" {
			found++
		}
	}
	if found != 4 {
		t.Fatal("four-layer exhausted alert absent", a)
	}
}

func TestEnrichmentMetricsNativeMetadataGuardNoRetagOrHistoricalForgery(t *testing.T) {
	ownedMigrationDatabase(t)
	f := agentMemoryTestFixture(t)
	b := f.base
	id := agentMemoryID(t, f)
	mustPutAgentMemory(t, f, id, agentMemoryInput("guard"))
	before := agentMemoryOwnedSourceSnapshot(t, f)
	var meta string
	if e := b.pool.QueryRow(b.ctx, `SELECT to_jsonb(m)::text||m.xmin::text FROM agent_enrichment_observations m WHERE owner_id=$1`, b.person.ID).Scan(&meta); e != nil {
		t.Fatal(e)
	}
	for _, sql := range []string{
		`UPDATE agent_enrichment_observations SET metric='memory_deleted' WHERE owner_id=$1`,
		`UPDATE agent_enrichment_observations SET expires_at=expires_at+interval '1 day' WHERE owner_id=$1`,
		`INSERT INTO agent_enrichment_observations(audit_event_id,owner_type,owner_id,agent_id,metric,occurred_at,expires_at) SELECT audit_event_id,owner_type,owner_id,agent_id,'candidate_promoted',occurred_at,expires_at FROM agent_enrichment_observations WHERE owner_id=$1`,
	} {
		if _, e := b.pool.Exec(b.ctx, sql, b.person.ID); e == nil {
			t.Fatal("observation retag/duplicate allowed")
		}
	}
	var audit int64
	var occurred time.Time
	if e := b.pool.QueryRow(b.ctx, `INSERT INTO audit_events(actor_account_id,action,resource_type,resource_id,decision,purpose) VALUES($1,'put','agent_memory',$2,'allowed','human_explicit_memory_edit') RETURNING id,occurred_at`, b.person.ID, id).Scan(&audit, &occurred); e != nil {
		t.Fatal(e)
	}
	// An old/committed audit cannot be classified later using present row state.
	if _, e := b.pool.Exec(b.ctx, `INSERT INTO agent_enrichment_observations(audit_event_id,owner_type,owner_id,agent_id,metric,occurred_at,expires_at) VALUES($1,'PERSON',$2,$3,'memory_created',$4,$5)`, audit, b.person.ID, b.personID, occurred, occurred.Add(time.Hour)); e == nil {
		t.Fatal("retroactive state-derived classification")
	}
	var after string
	if e := b.pool.QueryRow(b.ctx, `SELECT to_jsonb(m)::text||m.xmin::text FROM agent_enrichment_observations m WHERE owner_id=$1`, b.person.ID).Scan(&after); e != nil || meta != after {
		t.Fatal("failed metadata proof changed projection", e)
	}
	// The deliberate extra audit changes the old audit snapshot but not Memory.
	// Remove only that explicitly owned, non-projected fixture row for equality.
	b.exec(`DELETE FROM audit_events WHERE id=$1 AND actor_account_id=$2`, audit, b.person.ID)
	if before != agentMemoryOwnedSourceSnapshot(t, f) {
		t.Fatal("negative metadata mutated original business")
	}
}
func TestEnrichmentMetricsNativeDeterministicRunDistinctNoProvider(t *testing.T) {
	ownedMigrationDatabase(t)
	f, s, a := runFixture(t, false)
	b := f.f.f.place.private.base
	r, e := s.ScheduleOwn(b.ctx, a, agentrun.Input{MomentID: f.f.moment.ID})
	if e != nil {
		t.Fatal(e)
	}
	v, e := b.store.ReadOwnEnrichmentTrace(b.ctx, a, "MOMENT_ENRICHMENT", r.ID)
	if e != nil || v.Configuration != nil || len(v.Steps) != 0 || v.ModelAccess != "UNAVAILABLE" || v.SourceID != nil {
		t.Fatal(v, e)
	}
}
func TestEnrichmentMetricsNativePhysicalPruneOnlyDerivedAndNonemptyDown(t *testing.T) {
	ownedMigrationDatabase(t)
	f := agentMemoryTestFixture(t)
	b := f.base
	tx, e := b.pool.Begin(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(b.ctx)
	bind, e := lockOwnAgentPrivateBinding(b.ctx, tx, f.owner, true)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = lockAgentPrivateMetadata(b.ctx, tx, bind, true); e != nil {
		t.Fatal(e)
	}
	id := agentMemoryID(t, f)
	if _, _, e = putOwnMemoryInTx(b.ctx, tx, bind, f.owner.WorkspacePrincipal, id, agentMemoryInput("prune")); e != nil {
		t.Fatal(e)
	}
	// Trusted fixture chooses a SHORTER projection retention in the same writer
	// transaction; original audit, business row and their timestamps stay intact.
	var audit int64
	var occurred, deadline time.Time
	if e = tx.QueryRow(b.ctx, `DELETE FROM agent_enrichment_observations WHERE owner_id=$1 RETURNING audit_event_id,occurred_at`, b.person.ID).Scan(&audit, &occurred); e != nil {
		t.Fatal(e)
	}
	if e = tx.QueryRow(b.ctx, `SELECT clock_timestamp()+interval '400 milliseconds'`).Scan(&deadline); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(b.ctx, `INSERT INTO agent_enrichment_observations(audit_event_id,owner_type,owner_id,agent_id,metric,occurred_at,expires_at) VALUES($1,'PERSON',$2,$3,'memory_created',$4,$5)`, audit, bind.accountID, bind.agentID, occurred, deadline); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(b.ctx); e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile(filepath.Join("..", "..", "migrations", "089_agent_enrichment_observability.down.sql"))
	if e != nil {
		t.Fatal(e)
	}
	down, e := b.pool.Acquire(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	_, e = down.Exec(b.ctx, string(raw))
	_, cleanupErr := down.Exec(b.ctx, "ROLLBACK")
	down.Release()
	if cleanupErr != nil {
		t.Fatal(cleanupErr)
	}
	if e == nil {
		t.Fatal("nonempty down allowed")
	}
	// Failed explicit down is atomic and never clears the projection/history.
	before := agentMemoryOwnedSourceSnapshot(t, f)
	var memBefore, auditBefore string
	if e = b.pool.QueryRow(b.ctx, `SELECT to_jsonb(m)::text||m.xmin::text FROM agent_memories m WHERE id=$1`, id).Scan(&memBefore); e != nil {
		t.Fatal(e)
	}
	if e = b.pool.QueryRow(b.ctx, `SELECT to_jsonb(a)::text||a.xmin::text FROM audit_events a WHERE id=$1`, audit).Scan(&auditBefore); e != nil {
		t.Fatal(e)
	}
	for {
		var now time.Time
		if e = b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
			t.Fatal(e)
		}
		if !now.Before(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if n, e := b.store.PruneExpiredEnrichmentObservations(b.ctx, 1); e != nil || n != 1 {
		t.Fatal(n, e)
	}
	if n, e := b.store.PruneExpiredEnrichmentObservations(b.ctx, 1); e != nil || n != 0 {
		t.Fatal(n, e)
	}
	var memAfter, auditAfter string
	b.pool.QueryRow(b.ctx, `SELECT to_jsonb(m)::text||m.xmin::text FROM agent_memories m WHERE id=$1`, id).Scan(&memAfter)
	b.pool.QueryRow(b.ctx, `SELECT to_jsonb(a)::text||a.xmin::text FROM audit_events a WHERE id=$1`, audit).Scan(&auditAfter)
	if before != agentMemoryOwnedSourceSnapshot(t, f) || memBefore != memAfter || auditBefore != auditAfter {
		t.Fatal("prune rewrote original ledger")
	}
}
func TestEnrichmentMetricsNativeFinalClockAfterTableWait(t *testing.T) {
	ownedMigrationDatabase(t)
	f := agentMemoryTestFixture(t)
	b := f.base
	ctx, cancel := context.WithTimeout(b.ctx, 8*time.Second)
	defer cancel()
	var sessionDeadline time.Time
	if e := b.pool.QueryRow(b.ctx, `WITH c AS MATERIALIZED(SELECT clock_timestamp() n) UPDATE sessions SET idle_expires_at=n+interval '2 seconds' FROM c WHERE id=$1 RETURNING idle_expires_at`, f.ownerSession).Scan(&sessionDeadline); e != nil {
		t.Fatal(e)
	}
	hold, e := b.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer hold.Rollback(context.Background())
	if _, e = hold.Exec(ctx, `LOCK TABLE agent_enrichment_observations IN ACCESS EXCLUSIVE MODE`); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() {
		v, e := b.store.ReadOwnEnrichmentMetrics(ctx, f.owner, aem.Window{})
		if e == nil && !reflect.DeepEqual(v, aem.Snapshot{}) {
			done <- errors.New("expired payload released")
			return
		}
		done <- e
	}()
	var pid int
	if e = hold.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); e != nil {
		t.Fatal(e)
	}
	for {
		var waiting bool
		if e = b.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1=ANY(pg_blocking_pids(pid)) AND query LIKE '%SELECT metric FROM agent_enrichment_observations WHERE owner_type%')`, pid).Scan(&waiting); e != nil {
			t.Fatal(e)
		}
		if waiting {
			break
		}
		select {
		case e := <-done:
			t.Fatal("reader finished before actual barrier", e)
		default:
		}
		if ctx.Err() != nil {
			t.Fatal(ctx.Err())
		}
		time.Sleep(5 * time.Millisecond)
	}
	for {
		var now time.Time
		if e = b.pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
			t.Fatal(e)
		}
		if !now.Before(sessionDeadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	hold.Rollback(ctx)
	select {
	case e := <-done:
		if !errors.Is(e, aem.ErrDenied) {
			t.Fatal(e)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
