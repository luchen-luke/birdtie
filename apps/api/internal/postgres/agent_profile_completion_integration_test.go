package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemorycandidate"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	apc "github.com/birdtie/birdtie/apps/api/internal/agentprofilecompletion"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5/pgconn"
)

type completionNativeFixture struct {
	f       *agentPrivateFixture
	memory  agentmemory.Record
	profile agentprofile.PrivateRecord
}

func TestProfileMemoryCompletionErrorMappingPreservesSuccess(t *testing.T) {
	if completionError(nil) != nil || !errors.Is(completionError(errors.New("synthetic private SQL error")), agentprofile.ErrUnavailable) {
		t.Fatal("closed error mapping changed successful native read or disclosed private error")
	}
}

func TestProfileMemoryCompletionNativeDirectGuardNonfiniteRollback(t *testing.T) {
	ownedMigrationDatabase(t)
	for _, mode := range []string{"source_infinity", "session_infinity", "metadata_infinity"} {
		t.Run(mode, func(t *testing.T) {
			f := completionNative(t, "hiking", time.Hour)
			b := f.f.base
			p := f.preview(t)
			before := completionSnapshot(t, b.pool, b.ctx, mode+"-before")
			tx, e := b.pool.Begin(b.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer tx.Rollback(context.Background())
			switch mode {
			case "source_infinity":
				_, e = tx.Exec(b.ctx, `ALTER TABLE agent_memories DROP CONSTRAINT agent_memory_times`)
				if e == nil {
					_, e = tx.Exec(b.ctx, `UPDATE agent_memories SET version=version+1,valid_until='infinity'::timestamptz WHERE id=$1`, f.memory.ID)
				}
			case "session_infinity":
				_, e = tx.Exec(b.ctx, `UPDATE sessions SET expires_at='infinity'::timestamptz,idle_expires_at='infinity'::timestamptz WHERE id=$1`, f.f.ownerSession)
			case "metadata_infinity":
				_, e = tx.Exec(b.ctx, `ALTER TABLE agent_profiles DROP CONSTRAINT agent_profile_finite_times; ALTER TABLE agent_profiles DISABLE TRIGGER agent_profile_revision_guard`)
				if e == nil {
					_, e = tx.Exec(b.ctx, `UPDATE agent_profiles SET updated_at='infinity'::timestamptz WHERE agent_id=$1`, b.personID)
				}
			}
			if e != nil {
				t.Fatal("controlled malformed native fixture", e)
			}
			inTxBefore := initialCaptureSnapshot(t, tx, b.ctx, "completion-"+mode+"-controlled-before")
			if _, e = tx.Exec(b.ctx, `SAVEPOINT completion_malformed_insert`); e != nil {
				t.Fatal(e)
			}
			_, e = tx.Exec(b.ctx, `WITH clk AS MATERIALIZED(SELECT clock_timestamp() at)
   INSERT INTO agent_profile_completion_previews(id,owner_id,agent_id,session_id,memory_id,memory_version,expected_profile_version,category,authority,source_binding,profile_binding,previous_fields_digest,plan_digest,observed_at,expires_at)
   SELECT gen_random_uuid(),p.owner_id,p.agent_id,p.session_id,p.memory_id,m.version,ap.profile_version,p.category,
    birdtie_profile_completion_authority(p.owner_id,p.agent_id,p.session_id),
    encode(sha256(convert_to(jsonb_build_object('memory',to_jsonb(m),'row',m.xmin::text)::text,'UTF8')),'hex'),
    birdtie_profile_completion_profile_binding(p.agent_id,p.owner_id),p.previous_fields_digest,p.plan_digest,clk.at,
    least(clk.at+interval '1 minute',se.expires_at,se.idle_expires_at,m.valid_until)
   FROM agent_profile_completion_previews p JOIN agent_memories m ON m.id=p.memory_id JOIN agent_profiles ap ON ap.agent_id=p.agent_id
   JOIN sessions se ON se.id=p.session_id CROSS JOIN clk WHERE p.id=$1`, p.ID)
			var pgerr *pgconn.PgError
			if !errors.As(e, &pgerr) || pgerr.Code != "23514" || pgerr.Message != "profile completion requires current exact human source" {
				t.Fatal("direct malformed source did not reach closed guard rejection")
			}
			if _, e = tx.Exec(b.ctx, `ROLLBACK TO SAVEPOINT completion_malformed_insert`); e != nil {
				t.Fatal(e)
			}
			if !reflect.DeepEqual(inTxBefore, initialCaptureSnapshot(t, tx, b.ctx, "completion-"+mode+"-controlled-after")) {
				t.Fatal("rejected direct insert changed in-tx ledgers")
			}
			if e = tx.Rollback(b.ctx); e != nil {
				t.Fatal(e)
			}
			requireCompletionPairsEqual(t, before, completionSnapshot(t, b.pool, b.ctx, mode+"-after-rollback"))
			t.Log("CONTROLLED_NONFINITE_FIXTURE=true DIRECT_NEW_TRIGGER_REJECTED=true ORIGINAL_GUARDS_AND_FULL_ROWS_XMIN_RESTORED=true")
		})
	}
}

func completionNative(t *testing.T, category string, lease time.Duration) *completionNativeFixture {
	t.Helper()
	f := agentMemoryTestFixture(t)
	b := f.base
	r, e := b.store.ReadOwnAgentPrivateProfile(b.ctx, f.owner)
	if e != nil {
		t.Fatal(e)
	}
	canary := privateProfileCanaries()
	canary.PreferredActivityTypes = []string{}
	r, e = b.store.ReplaceOwnAgentPrivateProfile(b.ctx, f.owner, agentprofile.ReplacePrivateInput{ExpectedVersion: r.Profile.ProfileVersion, Fields: canary})
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(map[string]string{"activityCategory": category, "nature": "human-declaration"})
	input := agentmemory.PutInput{MemoryType: agentmemory.TypePreference, MemoryKey: "activity_category:" + category, Summary: agentmemorycandidate.Statement(category), StructuredValue: raw, Visibility: agentmemory.VisibilityPrivate, ValidUntil: time.Now().UTC().Add(lease).Truncate(time.Microsecond)}
	memory := mustPutAgentMemory(t, f, agentMemoryID(t, f), input)
	return &completionNativeFixture{f: f, memory: memory, profile: r}
}
func (f *completionNativeFixture) preview(t *testing.T) apc.Preview {
	t.Helper()
	id := agentMemoryID(t, f.f)
	p, e := f.f.base.store.PreviewOwnProfileCompletion(f.f.base.ctx, f.f.owner, apc.PreviewInput{PreviewID: id, MemoryID: f.memory.ID, MemoryVersion: f.memory.Version, ExpectedProfileVersion: f.profile.Profile.ProfileVersion})
	if e != nil || apc.ValidatePreview(p) != nil {
		t.Fatal("native concrete completion preview", e)
	}
	return p
}
func completionSnapshot(t *testing.T, q initialCaptureQueryer, ctx context.Context, label string) map[string]string {
	t.Helper()
	v := initialCaptureSnapshot(t, q, ctx, "completion-"+label)
	if len(v) < 137 {
		t.Fatal("complete actual093 public ledger required")
	}
	if dir := os.Getenv("BIRDTIE_PROFILE_COMPLETION_ARTIFACT_DIR"); dir != "" {
		if !filepath.IsAbs(dir) {
			t.Fatal("completion artifact path must be absolute")
		}
		var db string
		if q.QueryRow(ctx, `SELECT current_database()`).Scan(&db) != nil || !strings.HasPrefix(db, "birdtie_owned_migration_") {
			t.Fatal("completion artifact requires owned child")
		}
		target := filepath.Join(dir, db)
		if e := os.MkdirAll(target, 0700); e != nil {
			t.Fatal(e)
		}
		raw, e := json.MarshalIndent(v, "", "  ")
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(target, label+".json"), append(raw, '\n'), 0600); e != nil {
			t.Fatal(e)
		}
	}
	return v
}
func requireCompletionRejected(t *testing.T, r apc.Receipt, e error) {
	t.Helper()
	if e == nil || !reflect.DeepEqual(r, apc.Receipt{}) {
		t.Fatal("rejection released receipt/body or succeeded")
	}
}
func requireCompletionPairsEqual(t *testing.T, b, a map[string]string) {
	t.Helper()
	if !reflect.DeepEqual(b, a) {
		t.Fatal("rejected operation changed full public rows or xmin")
	}
	t.Logf("FULL_PUBLIC_TABLE_PAIRS=%d ALL_ROWS_AND_XMIN_EQUAL=true", len(b))
}

func TestProfileMemoryCompletionUnavailableInvocationBoundary(t *testing.T) {
	access := agentprofile.PrivateAccess{WorkspacePrincipal: actorref.PrincipalRef{Type: actorref.Person, ID: "94000000-0000-4000-8000-000000000001"}, SessionDigest: [32]byte{1}}
	for _, s := range []*Store{nil, New(nil, false)} {
		r, e := s.ReadOwnProfileCompletionSuggestions(context.Background(), access)
		if !errors.Is(e, agentprofile.ErrUnavailable) || !reflect.DeepEqual(r, apc.Suggestions{}) {
			t.Fatal("nil Store/pool invocation")
		}
		r, e = s.ReadOwnProfileCompletionSuggestions(nil, access)
		if !errors.Is(e, agentprofile.ErrUnavailable) || !reflect.DeepEqual(r, apc.Suggestions{}) {
			t.Fatal("nil context invocation")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r, e := New(nil, false).ReadOwnProfileCompletionSuggestions(ctx, access)
	if !errors.Is(e, agentprofile.ErrUnavailable) || !reflect.DeepEqual(r, apc.Suggestions{}) {
		t.Fatal("cancelled invocation")
	}
}

// The original profile writer now captures one PreferenceUpdated event.
// Compare every old row/xmin and allow only exact native five-second coalescing;
// never omit the outbox table from the full ledger preservation proof.
func completionExactPreferenceCapture(t *testing.T, q initialCaptureQueryer, ctx context.Context, before, after string, owner, agent string, version int64) {
	t.Helper()
	type pair struct { Row map[string]json.RawMessage `json:"row"`; Xmin string `json:"xmin"` }
	read := func(raw string) map[string]pair {
		var pairs []pair
		if e := json.Unmarshal([]byte(raw), &pairs); e != nil { t.Fatal(e) }
		out := map[string]pair{}
		for _, p := range pairs { var id string; if e := json.Unmarshal(p.Row["event_id"], &id); e != nil || id == "" { t.Fatal("invalid original outbox snapshot", e) }; if _, exists := out[id]; exists { t.Fatal("duplicate event snapshot") }; out[id] = p }
		return out
	}
	text := func(p pair, key string) string { var v string; if e := json.Unmarshal(p.Row[key], &v); e != nil { t.Fatal("outbox string", key, e) }; return v }
	number := func(p pair, key string) int64 { var v int64; if e := json.Unmarshal(p.Row[key], &v); e != nil { t.Fatal("outbox number", key, e) }; return v }
	stamp := func(p pair, key string) time.Time { var v time.Time; if e := json.Unmarshal(p.Row[key], &v); e != nil { t.Fatal("outbox timestamp", key, e) }; return v }
	old, current := read(before), read(after)
	var inserted []string
	for id := range current { if _, exists := old[id]; !exists { inserted = append(inserted, id) } }
	if len(inserted) != 1 { t.Fatalf("exact one PreferenceUpdated capture, got %d", len(inserted)) }
	added := current[inserted[0]]
	if text(added,"schema_version") != "agent-preference-outbox-v1" || text(added,"event_type") != "PREFERENCE_UPDATED" || text(added,"source_type") != "PRIVATE_PREFERENCE" || text(added,"source_id") != agent || text(added,"source_status") != "configured" || text(added,"tenant_id") != owner || text(added,"subject_id") != owner || text(added,"actor_id") != owner || text(added,"agent_id") != agent || number(added,"source_revision") != version || text(added,"delivery_state") != "PENDING" || number(added,"attempt") != 0 || number(added,"fence") != 0 || string(added.Row["causation_id"]) != "null" || string(added.Row["lease_owner"]) != "null" || string(added.Row["lease_until"]) != "null" { t.Fatal("unexpected current preference side effect") }
	if !stamp(added,"expires_at").Equal(stamp(added,"occurred_at").Add(15*time.Minute)) { t.Fatal("preference TTL changed") }
	var exact bool
	if e := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agent_domain_outbox d WHERE d.event_id=$1 AND d.subject_id=$2 AND d.agent_id=$3 AND d.source_revision=$4 AND birdtie_preference_outbox_current(d.agent_id,d.subject_id,d.source_revision,d.source_status,d.occurred_at,d.source_fingerprint) AND d.event_id=birdtie_preference_outbox_event(d.event_type,d.subject_id,d.agent_id,d.source_revision,d.source_status,d.source_fingerprint) AND d.logical_operation_id=birdtie_preference_outbox_operation(d.source_id,d.source_revision,d.event_type) AND d.root_trace_id=d.logical_operation_id)`,inserted[0],owner,agent,version).Scan(&exact); e != nil || !exact { t.Fatal("not the exact current native preference mutation",e) }
	coalesced := 0
	for id, prior := range old {
		next, exists := current[id]; if !exists { t.Fatal("old event deleted", id) }
		if reflect.DeepEqual(prior,next) { continue }
		if text(prior,"schema_version") != "agent-preference-outbox-v1" || text(prior,"event_type") != "PREFERENCE_UPDATED" || text(prior,"subject_id") != owner || text(prior,"agent_id") != agent || text(prior,"source_id") != agent || text(prior,"source_status") != "configured" || number(prior,"source_revision") >= version || text(prior,"delivery_state") != "PENDING" || number(prior,"attempt") != 0 || number(prior,"fence") != 0 || string(prior.Row["causation_id"]) != "null" || string(prior.Row["lease_owner"]) != "null" || string(prior.Row["lease_until"]) != "null" || text(next,"delivery_state") != "INVALIDATED" { t.Fatal("unrelated old outbox row/xmin changed",id) }
		elapsed := stamp(added,"occurred_at").Sub(stamp(prior,"occurred_at")); if elapsed < 0 || elapsed > 5*time.Second { t.Fatal("coalescing exceeded exact window") }
		var history bool
		if e := q.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM agent_consumer_inbox WHERE event_id=$1 AND subject_id=$2)`,id,owner).Scan(&history); e != nil || history { t.Fatal("coalesced claimed history",e) }
		a, b := map[string]json.RawMessage{}, map[string]json.RawMessage{}
		for key,v := range prior.Row { if key != "delivery_state" && key != "updated_at" { a[key]=v } }
		for key,v := range next.Row { if key != "delivery_state" && key != "updated_at" { b[key]=v } }
		if !reflect.DeepEqual(a,b) || stamp(next,"updated_at").Before(stamp(prior,"updated_at")) || prior.Xmin == next.Xmin { t.Fatal("coalescing renewed immutable metadata/TTL or lacks real mutation") }
		coalesced++
	}
	if len(current) != len(old)+1 || coalesced > len(old) { t.Fatal("unbounded preference mutation") }
	t.Logf("EXACT_PREFERENCE_INSERT=1 EXACT_OLD_CONTROL_COALESCED=%d ALL_OTHER_ROWS_XMIN_UNCHANGED=true",coalesced)
}

func TestProfileMemoryCompletionNativeSixCategoriesAndOnceReceipt(t *testing.T) {
	ownedMigrationDatabase(t)
	for _, category := range []string{"badminton", "basketball", "football", "sports", "culture", "hiking"} {
		t.Run(category, func(t *testing.T) {
			f := completionNative(t, category, time.Hour)
			b := f.f.base
			p := f.preview(t)
			again, e := b.store.PreviewOwnProfileCompletion(b.ctx, f.f.owner, apc.PreviewInput{PreviewID: p.ID, MemoryID: f.memory.ID, MemoryVersion: f.memory.Version, ExpectedProfileVersion: f.profile.Profile.ProfileVersion})
			if e != nil || again.PlanDigest != p.PlanDigest || !again.ExpiresAt.Equal(p.ExpiresAt) {
				t.Fatal("same key duplicated or extended preview", e)
			}
			before := completionSnapshot(t, b.pool, b.ctx, category+"-before")
			receipt, e := b.store.AcceptOwnProfileCompletion(b.ctx, f.f.owner, p.ID, apc.AcceptInput{PlanDigest: p.PlanDigest})
			if e != nil || receipt.State != "COMMITTED" || !receipt.CurrentProfileMatches || apc.ValidateReceipt(receipt) != nil {
				t.Fatal("native exact CAS", e)
			}
			current, e := b.store.ReadOwnAgentPrivateProfile(b.ctx, f.f.owner)
			want := f.profile.Fields
			want.PreferredActivityTypes = []string{agentmemorycandidate.Statement(category)}
			if e != nil || !reflect.DeepEqual(current.Fields, want) || current.Profile.ProfileVersion != f.profile.Profile.ProfileVersion+1 {
				t.Fatal("other eight fields or original CAS changed")
			}
			after := completionSnapshot(t, b.pool, b.ctx, category+"-after")
			for table, v := range before {
				if table != "agent_profiles" && table != "agent_private_profiles" && table != "audit_events" && table != "agent_profile_completion_previews" && table != "agent_domain_outbox" && after[table] != v {
					t.Fatal("unrelated original ledger changed", table)
				}
			}
			completionExactPreferenceCapture(t, b.pool, b.ctx, before["agent_domain_outbox"], after["agent_domain_outbox"], b.person.ID, b.personID, current.Profile.ProfileVersion)
			for i := 0; i < 100; i++ {
				r, e := b.store.AcceptOwnProfileCompletion(b.ctx, f.f.owner, p.ID, apc.AcceptInput{PlanDigest: p.PlanDigest})
				if e != nil || r.ResultProfileVersion == nil || *r.ResultProfileVersion != *receipt.ResultProfileVersion || !r.CommittedAt.Equal(*receipt.CommittedAt) {
					t.Fatal("same operation repeated effect", e)
				}
			}
			requireCompletionPairsEqual(t, after, completionSnapshot(t, b.pool, b.ctx, category+"-after100"))
			fresh := New(b.pool, false)
			r, e := fresh.ReadOwnProfileCompletion(b.ctx, f.f.owner, p.ID)
			if e != nil || r.State != "COMMITTED" || !r.CurrentProfileMatches {
				t.Fatal("restart read receipt", e)
			}
			s, e := fresh.ReadOwnProfileCompletionSuggestions(b.ctx, f.f.owner)
			if e != nil || s.State != "FIELD_ALREADY_SET" || len(s.Sources) != 0 {
				t.Fatal("existing target overwritten/offered", e)
			}
			current.Fields.AgentNotes = "本人后来独立编辑"
			if _, e = b.store.ReplaceOwnAgentPrivateProfile(b.ctx, f.f.owner, agentprofile.ReplacePrivateInput{ExpectedVersion: current.Profile.ProfileVersion, Fields: current.Fields}); e != nil {
				t.Fatal(e)
			}
			r, e = fresh.ReadOwnProfileCompletion(b.ctx, f.f.owner, p.ID)
			if e != nil || r.State != "COMMITTED" || r.CurrentProfileMatches {
				t.Fatal("old receipt claimed current fields", e)
			}
			encoded, _ := json.Marshal(r)
			if strings.Contains(string(encoded), agentmemorycandidate.Statement(category)) || strings.Contains(string(encoded), "CANARY") {
				t.Fatal("recovery restored review/private body")
			}
		})
	}
}

func TestProfileMemoryCompletionNativeConcurrentSingleEffect(t *testing.T) {
	ownedMigrationDatabase(t)
	f := completionNative(t, "hiking", time.Hour)
	b := f.f.base
	p := f.preview(t)
	var start sync.WaitGroup
	start.Add(1)
	results := make(chan error, 12)
	for i := 0; i < 12; i++ {
		go func() {
			start.Wait()
			r, e := b.store.AcceptOwnProfileCompletion(b.ctx, f.f.owner, p.ID, apc.AcceptInput{PlanDigest: p.PlanDigest})
			if e == nil && r.State != "COMMITTED" {
				e = errors.New("nonterminal concurrent receipt")
			}
			results <- e
		}()
	}
	start.Done()
	for i := 0; i < 12; i++ {
		if e := <-results; e != nil {
			t.Fatal(e)
		}
	}
	var version, receipts, audits int64
	e := b.pool.QueryRow(b.ctx, `SELECT (SELECT profile_version FROM agent_profiles WHERE agent_id=$1),(SELECT count(*) FROM agent_profile_completion_previews WHERE id=$2 AND result_profile_version IS NOT NULL),(SELECT count(*) FROM audit_events WHERE actor_account_id=$3 AND resource_type='agent_private_profile' AND purpose='human_profile_edit')`, b.personID, p.ID, b.person.ID).Scan(&version, &receipts, &audits)
	if e != nil || version != f.profile.Profile.ProfileVersion+1 || receipts != 1 || audits != 2 {
		t.Fatal("concurrent CAS/audit/receipt count", e)
	} // one fixture edit + one completion
}

func TestProfileMemoryCompletionNativeChangedSourceIdentityAndABA(t *testing.T) {
	ownedMigrationDatabase(t)
	for _, mode := range []string{"wrong_digest", "wrong_session", "other_person", "organization", "account_ABA", "agent_ABA", "profile_change", "source_version", "source_delete", "session_revoke", "future_source", "already_set"} {
		t.Run(mode, func(t *testing.T) {
			f := completionNative(t, "hiking", time.Hour)
			b := f.f.base
			p := f.preview(t)
			access := f.f.owner
			digest := p.PlanDigest
			switch mode {
			case "wrong_digest":
				digest = strings.Repeat("f", 64)
			case "wrong_session":
				_, d, e := identity.NewToken()
				if e != nil {
					t.Fatal(e)
				}
				b.exec(`INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at) VALUES($1,$2,'test',statement_timestamp()+interval '1 hour',statement_timestamp()+interval '1 hour')`, b.person.ID, d[:])
				access.SessionDigest = d
			case "other_person":
				access = f.f.peer
			case "organization":
				access = f.f.org
			case "account_ABA":
				b.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, b.person.ID)
				b.exec(`UPDATE accounts SET status='active' WHERE id=$1`, b.person.ID)
			case "agent_ABA":
				b.exec(`UPDATE agents SET status='suspended' WHERE id=$1`, b.personID)
				b.exec(`UPDATE agents SET status='active' WHERE id=$1`, b.personID)
			case "profile_change", "already_set":
				fields := f.profile.Fields
				if mode == "profile_change" {
					fields.AgentNotes = "改变后恢复同一字段"
				} else {
					fields.PreferredActivityTypes = []string{"本人已有偏好"}
				}
				r, e := b.store.ReplaceOwnAgentPrivateProfile(b.ctx, f.f.owner, agentprofile.ReplacePrivateInput{ExpectedVersion: f.profile.Profile.ProfileVersion, Fields: fields})
				if e != nil {
					t.Fatal(e)
				}
				if mode == "profile_change" {
					if _, e = b.store.ReplaceOwnAgentPrivateProfile(b.ctx, f.f.owner, agentprofile.ReplacePrivateInput{ExpectedVersion: r.Profile.ProfileVersion, Fields: f.profile.Fields}); e != nil {
						t.Fatal(e)
					}
				}
			case "source_version":
				b.exec(`UPDATE agent_memories SET version=version+1,summary='已改变的本人声明',updated_at=clock_timestamp() WHERE id=$1`, f.memory.ID)
				b.exec(`UPDATE agent_memories SET version=version+1,summary=$2,updated_at=clock_timestamp() WHERE id=$1`, f.memory.ID, f.memory.Summary)
			case "source_delete":
				if _, e := b.store.DeleteOwnMemory(b.ctx, f.f.owner, f.memory.ID, f.memory.Version); e != nil {
					t.Fatal(e)
				}
			case "session_revoke":
				b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.f.ownerSession)
			case "future_source":
				b.exec(`UPDATE agent_memories SET version=version+1,valid_from=clock_timestamp()+interval '1 minute',updated_at=clock_timestamp()+interval '1 minute' WHERE id=$1`, f.memory.ID)
			}
			before := completionSnapshot(t, b.pool, b.ctx, mode+"-before")
			r, e := b.store.AcceptOwnProfileCompletion(b.ctx, access, p.ID, apc.AcceptInput{PlanDigest: digest})
			requireCompletionRejected(t, r, e)
			requireCompletionPairsEqual(t, before, completionSnapshot(t, b.pool, b.ctx, mode+"-after"))
			if mode == "wrong_session" {
				r, e = b.store.ReadOwnProfileCompletion(b.ctx, access, p.ID)
				if e != nil || r.State != "PENDING" || r.ResultProfileVersion != nil {
					t.Fatal("new session metadata read", e)
				}
			}
		})
	}
}

func TestProfileMemoryCompletionNativeRollbackAllLedgers(t *testing.T) {
	ownedMigrationDatabase(t)
	for _, mode := range []string{"audit_failure", "receipt_failure", "late_receipt_expiry"} {
		t.Run(mode, func(t *testing.T) {
			lease := time.Hour
			if mode == "late_receipt_expiry" {
				lease = 2 * time.Second
			}
			f := completionNative(t, "hiking", lease)
			b := f.f.base
			p := f.preview(t)
			table := "audit_events"
			event := "INSERT"
			body := `RAISE EXCEPTION 'controlled owned completion failure';`
			if mode != "audit_failure" {
				table = "agent_profile_completion_previews"
				event = "UPDATE"
			}
			if mode == "late_receipt_expiry" {
				body = `PERFORM pg_sleep(2.1); RETURN NEW;`
			}
			sql := `CREATE FUNCTION public.birdtie_owned_completion_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN ` + body + ` END $$; CREATE TRIGGER zz_owned_completion_fault BEFORE ` + event + ` ON ` + table + ` FOR EACH ROW EXECUTE FUNCTION public.birdtie_owned_completion_fault()`
			if _, e := b.pool.Exec(b.ctx, sql); e != nil {
				t.Fatal(e)
			}
			before := completionSnapshot(t, b.pool, b.ctx, mode+"-before")
			r, e := b.store.AcceptOwnProfileCompletion(b.ctx, f.f.owner, p.ID, apc.AcceptInput{PlanDigest: p.PlanDigest})
			requireCompletionRejected(t, r, e)
			requireCompletionPairsEqual(t, before, completionSnapshot(t, b.pool, b.ctx, mode+"-after"))
			if _, e = b.pool.Exec(b.ctx, `DROP TRIGGER zz_owned_completion_fault ON `+table+`; DROP FUNCTION public.birdtie_owned_completion_fault()`); e != nil {
				t.Fatal(e)
			}
		})
	}
}

func TestProfileMemoryCompletionNativeRealWaitFinalGates(t *testing.T) {
	ownedMigrationDatabase(t)
	for _, mode := range []string{"source_expiry", "session_expiry", "session_revoke", "source_ABA", "owner_ABA"} {
		t.Run(mode, func(t *testing.T) {
			lease := time.Hour
			if mode == "source_expiry" {
				lease = 2 * time.Second
			}
			f := completionNative(t, "hiking", lease)
			b := f.f.base
			p := f.preview(t)
			if mode == "session_expiry" {
				b.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '2 seconds' WHERE id=$1`, f.f.ownerSession)
			}
			held, e := b.pool.Begin(b.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer held.Rollback(context.Background())
			var locker int
			if held.QueryRow(b.ctx, `SELECT pg_backend_pid()`).Scan(&locker) != nil {
				t.Fatal("owned locker PID")
			}
			if _, e = held.Exec(b.ctx, `LOCK TABLE agent_memories IN ACCESS EXCLUSIVE MODE`); e != nil {
				t.Fatal(e)
			}
			done := make(chan error, 1)
			go func() {
				r, e := b.store.AcceptOwnProfileCompletion(b.ctx, f.f.owner, p.ID, apc.AcceptInput{PlanDigest: p.PlanDigest})
				if e == nil || !reflect.DeepEqual(r, apc.Receipt{}) {
					done <- errors.New("afterwait source released")
				} else {
					done <- nil
				}
			}()
			until := time.Now().Add(4 * time.Second)
			for {
				var waiting bool
				e = b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1=ANY(pg_blocking_pids(pid)) AND wait_event_type='Lock' AND query LIKE '%LOCK TABLE%agent_memories%')`, locker).Scan(&waiting)
				if e != nil {
					t.Fatal(e)
				}
				if waiting {
					break
				}
				if time.Now().After(until) {
					t.Fatal("actual owned relation wait not observed")
				}
				time.Sleep(10 * time.Millisecond)
			}
			switch mode {
			case "session_revoke":
				_, e = held.Exec(b.ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.f.ownerSession)
			case "source_ABA":
				_, e = held.Exec(b.ctx, `UPDATE agent_memories SET version=version+1,summary='changed-while-wait',updated_at=clock_timestamp() WHERE id=$1`, f.memory.ID)
				if e == nil {
					_, e = held.Exec(b.ctx, `UPDATE agent_memories SET version=version+1,summary=$2,updated_at=clock_timestamp() WHERE id=$1`, f.memory.ID, f.memory.Summary)
				}
			case "owner_ABA":
				_, e = held.Exec(b.ctx, `UPDATE accounts SET status='suspended' WHERE id=$1`, b.person.ID)
				if e == nil {
					_, e = held.Exec(b.ctx, `UPDATE accounts SET status='active' WHERE id=$1`, b.person.ID)
				}
			default:
				time.Sleep(2100 * time.Millisecond)
			}
			if e != nil {
				t.Fatal(e)
			}
			beforeRelease := initialCaptureSnapshot(t, held, b.ctx, "completion-wait-"+mode+"-before-release")
			if e = held.Commit(b.ctx); e != nil {
				t.Fatal(e)
			}
			select {
			case e = <-done:
				if e != nil {
					t.Fatal(e)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("afterwait call did not terminate")
			}
			requireCompletionPairsEqual(t, beforeRelease, completionSnapshot(t, b.pool, b.ctx, "wait-"+mode+"-after"))
			var count int
			if b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_profile_completion_previews WHERE id=$1 AND result_profile_version IS NOT NULL`, p.ID).Scan(&count) != nil || count != 0 {
				t.Fatal("late receipt committed")
			}
			current, e := b.store.ReadOwnAgentPrivateProfile(b.ctx, f.f.peer)
			if e != nil || len(current.Fields.PreferredActivityTypes) != 0 {
				t.Fatal("peer changed", e)
			}
			t.Log("OWNED_POSTGRES_BLOCKING_PID_OBSERVED=true FINAL_CURRENT_GATE_REJECTED=true COMMITTED_RECEIPTS=0")
		})
	}
}
