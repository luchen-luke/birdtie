package postgres

import (
	"context"
	"encoding/json"
	"errors"
	aep "github.com/birdtie/birdtie/apps/api/internal/agentenrichmentpurpose"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

type enrichmentPurposeFixture struct {
	f         *contextBuilderNativeFixture
	moment    content.Moment
	selection aep.Selection
}

func enrichmentPurposeNative(t *testing.T) *enrichmentPurposeFixture {
	t.Helper()
	f := contextBuilderNative(t)
	b := f.place.private.base
	m, e := b.store.CreateMomentDraft(b.ctx, b.person.ID, content.MomentInput{CityID: f.place.city, Title: "UNSELECTED_PRIVATE_TITLE", Body: "本人所选的羽毛球原始正文_ENRICHMENT", TimePrecision: "unknown", LocationPrecision: "city"})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for _, q := range []string{`DELETE FROM agent_enrichment_purpose_bindings WHERE grant_id IN(SELECT id FROM consent_grants WHERE owner_account_id=ANY($1::uuid[]))`, `DELETE FROM agent_enrichment_purpose_previews WHERE owner_id=ANY($1::uuid[])`, `DELETE FROM consent_grants WHERE owner_account_id=ANY($1::uuid[])`, `DELETE FROM moments WHERE author_account_id=ANY($1::uuid[])`, `DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`} {
			if _, e := b.pool.Exec(ctx, q, b.accounts); e != nil {
				t.Error(e)
			}
		}
	})
	return &enrichmentPurposeFixture{f: f, moment: m, selection: aep.Selection{TaskID: f.task.ID, MomentID: m.ID, MomentRevision: m.Revision, Fields: []string{"body"}, DeadlineAt: time.Now().UTC().Truncate(time.Microsecond).Add(10 * time.Minute)}}
}
func (f *enrichmentPurposeFixture) approve(t *testing.T) (aep.Preview, aep.Grant) {
	t.Helper()
	b := f.f.place.private.base
	p, e := b.store.PreviewOwnEnrichmentPurpose(b.ctx, f.f.place.private.owner, f.selection)
	if e != nil {
		t.Fatal("native concrete preview", e)
	}
	g, e := b.store.ApproveOwnEnrichmentPurpose(b.ctx, f.f.place.private.owner, p.ID)
	if e != nil {
		t.Fatal("native exact preview approve", e)
	}
	return p, g
}
func requireEnrichmentEmpty(t *testing.T, r aep.Resolution, e error) {
	t.Helper()
	if e == nil || !reflect.DeepEqual(r, aep.Resolution{}) {
		t.Fatal("analysis must fail closed with no source body", r, e)
	}
}
func requireEnrichmentDenied(t *testing.T, s *Store, ctx context.Context, a agentprofile.PrivateAccess, id string) {
	t.Helper()
	r, e := s.ResolveOwnEnrichmentPurpose(ctx, a, id)
	requireEnrichmentEmpty(t, r, e)
}
func TestEnrichmentPurposeNativeLifecycleAndNoOtherEffects(t *testing.T) {
	f := enrichmentPurposeNative(t)
	b := f.f.place.private.base
	a := f.f.place.private.owner
	before := purposeOwnedControlRows(t, f.f.place.private)
	requireEnrichmentDenied(t, b.store, b.ctx, a, f.moment.ID)
	p, g := f.approve(t)
	if p.Review.Content["body"] != f.moment.Body || len(p.Review.Content) != 1 || aep.ValidatePreview(p) != nil || g.ExpiresAt.After(p.ExpiresAt) {
		t.Fatal(p, g)
	}
	r, e := b.store.ResolveOwnEnrichmentPurpose(b.ctx, a, g.ID)
	if e != nil || len(r.Content) != 1 || r.Content["body"] != f.moment.Body {
		t.Fatal(r, e)
	}
	if _, e = json.Marshal(r); !errors.Is(e, aep.ErrServerOnly) {
		t.Fatal("wire resolution", e)
	}
	retry, e := b.store.ApproveOwnEnrichmentPurpose(b.ctx, a, p.ID)
	if e != nil || retry.ID != g.ID || !retry.ExpiresAt.Equal(g.ExpiresAt) {
		t.Fatal("retry duplicated/renewed grant", retry, e)
	}
	receipt, e := b.store.ReadOwnEnrichmentPurposePreview(b.ctx, a, p.ID)
	if e != nil || receipt.State != "RECEIPT_ONLY" || receipt.ConsumedGrantID != g.ID || len(receipt.Review.Content) != 0 {
		t.Fatal("unknown approve reconciliation", receipt, e)
	}
	revoked, e := b.store.RevokeOwnEnrichmentPurpose(b.ctx, a, g.ID, 1)
	if e != nil || revoked.Revision != 2 || revoked.RevokedAt == nil {
		t.Fatal(revoked, e)
	}
	again, e := b.store.RevokeOwnEnrichmentPurpose(b.ctx, a, g.ID, 1)
	if e != nil || again.Revision != 2 || !again.RevokedAt.Equal(*revoked.RevokedAt) {
		t.Fatal("unknown revoke retry", again, e)
	}
	requireEnrichmentDenied(t, b.store, b.ctx, a, g.ID)
	if _, e = b.store.ApproveOwnEnrichmentPurpose(b.ctx, a, p.ID); e == nil {
		t.Fatal("revoked preview manufactured a new grant")
	}
	if purposeOwnedControlRows(t, f.f.place.private) != before {
		t.Fatal("analysis approval touched original candidate/memory/effect/outbox/model/session rows")
	}
	var raw string
	if e = b.pool.QueryRow(b.ctx, `SELECT to_jsonb(p)::text FROM agent_enrichment_purpose_previews p WHERE id=$1`, p.ID).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	if containsPrivateString(raw, []string{f.moment.Body, f.moment.Title, f.f.task.Query}) {
		t.Fatal("source/Task body persisted in purpose metadata")
	}
}
func containsPrivateString(s string, values []string) bool {
	for _, v := range values {
		if len(v) > 0 {
			for i := 0; i+len(v) <= len(s); i++ {
				if s[i:i+len(v)] == v {
					return true
				}
			}
		}
	}
	return false
}
func TestEnrichmentPurposeNativeCurrentSourceIdentityAndABA(t *testing.T) {
	for _, mode := range []string{"source_revision", "source_ABA", "task_query", "task_ABA", "account_ABA", "agent_ABA", "metadata_ABA", "session_revoke", "wrong_session", "other_person", "organization", "expired", "city_hidden"} {
		t.Run(mode, func(t *testing.T) {
			f := enrichmentPurposeNative(t)
			b := f.f.place.private.base
			a := f.f.place.private.owner
			if mode == "expired" {
				f.selection.DeadlineAt = time.Now().UTC().Truncate(time.Microsecond).Add(300 * time.Millisecond)
			}
			_, g := f.approve(t)
			switch mode {
			case "source_revision":
				b.exec(`UPDATE moments SET revision=revision+1,body='changed',updated_at=clock_timestamp() WHERE id=$1`, f.moment.ID)
			case "source_ABA":
				for _, body := range []string{"changed", f.moment.Body} {
					b.exec(`UPDATE moments SET revision=revision+1,body=$2,updated_at=clock_timestamp() WHERE id=$1`, f.moment.ID, body)
				}
			case "task_query":
				b.exec(`UPDATE agent_tasks SET query='different',filters=jsonb_set(filters,'{currentQuery}','"different"'),updated_at=clock_timestamp() WHERE id=$1`, f.f.task.ID)
			case "task_ABA":
				for _, query := range []string{"different", f.f.task.Query} {
					b.exec(`UPDATE agent_tasks SET query=$2,filters=jsonb_set(filters,'{currentQuery}',to_jsonb($2::text)),updated_at=clock_timestamp() WHERE id=$1`, f.f.task.ID, query)
				}
			case "account_ABA":
				b.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, b.person.ID)
				b.exec(`UPDATE accounts SET status='active' WHERE id=$1`, b.person.ID)
			case "agent_ABA":
				b.exec(`UPDATE agents SET status='suspended' WHERE id=$1`, b.personID)
				b.exec(`UPDATE agents SET status='active' WHERE id=$1`, b.personID)
			case "metadata_ABA":
				b.exec(`UPDATE agent_profiles SET profile_version=profile_version+1,updated_at=clock_timestamp() WHERE agent_id=$1`, b.personID)
			case "session_revoke":
				b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, a.SessionDigest[:])
			case "wrong_session":
				a.SessionDigest = f.f.place.private.peer.SessionDigest
			case "other_person":
				a = f.f.place.private.peer
			case "organization":
				a = f.f.place.private.org
			case "expired":
				if _, e := b.pool.Exec(b.ctx, `SELECT pg_sleep(0.35)`); e != nil {
					t.Fatal(e)
				}
			case "city_hidden":
				b.exec(`UPDATE cities SET publication_status='hidden' WHERE id=$1`, f.f.place.city)
			}
			requireEnrichmentDenied(t, b.store, b.ctx, a, g.ID)
		})
	}
}
func TestEnrichmentPurposeNativeHumanRecoveryDoesNotTransferSession(t *testing.T) {
	f := enrichmentPurposeNative(t)
	b := f.f.place.private.base
	a := f.f.place.private.owner
	p, g := f.approve(t)
	var token [32]byte
	for i := range token {
		token[i] = byte(i + 11)
	}
	b.exec(`INSERT INTO sessions(id,account_id,token_sha256,expires_at,idle_expires_at,authentication_method) VALUES(gen_random_uuid(),$1,$2,clock_timestamp()+interval '1 hour',clock_timestamp()+interval '30 minutes','test')`, b.person.ID, token[:])
	a.SessionDigest = token
	requireEnrichmentDenied(t, b.store, b.ctx, a, g.ID)
	rec, e := b.store.ReadOwnEnrichmentPurposePreview(b.ctx, a, p.ID)
	if e != nil || rec.State != "RECEIPT_ONLY" || rec.ConsumedGrantID != g.ID {
		t.Fatal(rec, e)
	}
	if _, e = b.store.ReadOwnEnrichmentPurpose(b.ctx, a, g.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = b.store.RevokeOwnEnrichmentPurpose(b.ctx, a, g.ID, 1); e != nil {
		t.Fatal(e)
	}
}
func TestEnrichmentPurposeNativeGrantImmutableAndSchemaDown(t *testing.T) {
	ownedMigrationDatabase(t)
	f := enrichmentPurposeNative(t)
	b := f.f.place.private.base
	_, g := f.approve(t)
	for _, q := range []string{`UPDATE consent_grants SET expires_at=expires_at+interval '1 minute' WHERE id=$1`, `UPDATE consent_grants SET actions=ARRAY['analyze_local','write'] WHERE id=$1`, `UPDATE consent_grants SET purpose='TASK_CONTEXT_READ' WHERE id=$1`, `UPDATE consent_grants SET revision=revision+1 WHERE id=$1`} {
		if _, e := b.pool.Exec(b.ctx, q, g.ID); e == nil {
			t.Fatal("native grant mutation admitted", q)
		}
	}
	if _, e := b.store.RevokeOwnEnrichmentPurpose(b.ctx, f.f.place.private.owner, g.ID, 1); e != nil {
		t.Fatal(e)
	}
	if _, e := b.pool.Exec(b.ctx, `UPDATE consent_grants SET revoked_at=NULL,revision=revision+1 WHERE id=$1`, g.ID); e == nil {
		t.Fatal("revoke ABA admitted")
	}
	down, e := os.ReadFile("../../migrations/079_agent_enrichment_purpose_binding.down.sql")
	if e != nil {
		t.Fatal(e)
	}
	before := enrichmentAllPublic(t, b.pool, b.ctx)
	conn, e := b.pool.Acquire(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	_, e = conn.Exec(b.ctx, string(down))
	_, rollback := conn.Exec(b.ctx, "ROLLBACK")
	conn.Release()
	if rollback != nil {
		t.Fatal(rollback)
	}
	if e == nil {
		t.Fatal("down erased approved history")
	}
	if enrichmentAllPublic(t, b.pool, b.ctx) != before {
		t.Fatal("failed down mutated old data")
	}
}
func enrichmentAllPublic(t *testing.T, p *pgxpool.Pool, ctx context.Context) string {
	t.Helper()
	c, e := p.Acquire(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Release()
	_, e = c.Exec(ctx, `CREATE OR REPLACE FUNCTION pg_temp.enrichment_public() RETURNS jsonb LANGUAGE plpgsql AS $$ DECLARE item record;rows jsonb;result jsonb:='{}';BEGIN FOR item IN SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename LOOP EXECUTE format('SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),''[]''::jsonb) FROM public.%I t',item.tablename) INTO rows;result:=result||jsonb_build_object(item.tablename,rows);END LOOP;RETURN result;END $$`)
	if e != nil {
		t.Fatal(e)
	}
	var out string
	if e = c.QueryRow(ctx, `SELECT pg_temp.enrichment_public()::text`).Scan(&out); e != nil {
		t.Fatal(e)
	}
	return out
}
func TestEnrichmentPurposeNativeSchemaGateAndFiniteApproval(t *testing.T) {
	ownedMigrationDatabase(t)
	f := enrichmentPurposeNative(t)
	b := f.f.place.private.base
	a := f.f.place.private.owner
	down, e := os.ReadFile("../../migrations/079_agent_enrichment_purpose_binding.down.sql")
	if e != nil {
		t.Fatal(e)
	}
	up, e := os.ReadFile("../../migrations/079_agent_enrichment_purpose_binding.sql")
	if e != nil {
		t.Fatal(e)
	}
	dependent092 := candidateVocabularyFixture(t, b.pool, b.ctx, false)
	before := enrichmentAllPublic(t, b.pool, b.ctx)
	// Latest owned fixture must downgrade its real dependent080 first. All old
	// missing079, expiry, identity and complete-public assertions remain intact.
	var dependent080 bool
	if e = b.pool.QueryRow(b.ctx, `SELECT to_regclass('public.agent_candidate_retention_previews') IS NOT NULL`).Scan(&dependent080); e != nil {
		t.Fatal(e)
	}
	var up080 []byte
	if dependent080 {
		down080, err := os.ReadFile("../../migrations/080_agent_candidate_retention.down.sql")
		if err != nil {
			t.Fatal(err)
		}
		up080, err = os.ReadFile("../../migrations/080_agent_candidate_retention.sql")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = b.pool.Exec(b.ctx, string(down080)); err != nil {
			t.Fatal("unused dependent080 down", err)
		}
	}
	if _, e = b.pool.Exec(b.ctx, string(down)); e != nil {
		t.Fatal(e)
	}
	if _, e = b.store.PreviewOwnEnrichmentPurpose(b.ctx, a, f.selection); !errors.Is(e, aep.ErrUnavailable) {
		t.Fatal("missing native079 guard", e)
	}
	if _, e = b.pool.Exec(b.ctx, string(up)); e != nil {
		t.Fatal(e)
	}
	if dependent080 {
		if _, e = b.pool.Exec(b.ctx, string(up080)); e != nil {
			t.Fatal("dependent080 reapply", e)
		}
	}
	if dependent092 {
		candidateVocabularyFixture(t, b.pool, b.ctx, true)
	}
	if enrichmentAllPublic(t, b.pool, b.ctx) != before {
		t.Fatal("unused079 roundtrip changed complete old rows")
	}
	b.exec(`ALTER TABLE consent_grants DISABLE TRIGGER agent_enrichment_grant_guard`)
	if _, e = b.store.PreviewOwnEnrichmentPurpose(b.ctx, a, f.selection); !errors.Is(e, aep.ErrUnavailable) {
		t.Fatal("disabled native guard", e)
	}
	b.exec(`ALTER TABLE consent_grants ENABLE TRIGGER agent_enrichment_grant_guard`)
	b.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '2 minutes' WHERE token_sha256=$1`, a.SessionDigest[:])
	p, e := b.store.PreviewOwnEnrichmentPurpose(b.ctx, a, f.selection)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = b.store.Authenticate(b.ctx, a.SessionDigest); e != nil {
		t.Fatal(e)
	}
	g, e := b.store.ApproveOwnEnrichmentPurpose(b.ctx, a, p.ID)
	if e != nil || g.ExpiresAt.After(p.ExpiresAt) {
		t.Fatal("idle refresh extended specific original preview", g, p, e)
	}
	b.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '30 seconds' WHERE token_sha256=$1`, a.SessionDigest[:])
	r, e := b.store.ResolveOwnEnrichmentPurpose(b.ctx, a, g.ID)
	if e != nil || r.ExpiresAt.After(time.Now().Add(31*time.Second)) {
		t.Fatal("read lease outlived current idle", r, e)
	}
}
func TestEnrichmentPurposeNativeRealWaitExpiryAndRevoke(t *testing.T) {
	ownedMigrationDatabase(t)
	for _, mode := range []string{"natural_expiry", "revoke", "source_ABA", "task_ABA", "city_ABA", "session_expiry"} {
		t.Run(mode, func(t *testing.T) {
			f := enrichmentPurposeNative(t)
			b := f.f.place.private.base
			a := f.f.place.private.owner
			if mode == "natural_expiry" {
				f.selection.DeadlineAt = time.Now().UTC().Truncate(time.Microsecond).Add(400 * time.Millisecond)
			}
			_, g := f.approve(t)
			if mode == "session_expiry" {
				b.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '400 milliseconds' WHERE token_sha256=$1`, a.SessionDigest[:])
			}
			held, e := b.pool.Begin(b.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer held.Rollback(context.Background())
			if _, e = held.Exec(b.ctx, `LOCK TABLE agent_enrichment_purpose_previews IN ACCESS EXCLUSIVE MODE`); e != nil {
				t.Fatal(e)
			}
			done := make(chan error, 1)
			go func() {
				r, e := b.store.ResolveOwnEnrichmentPurpose(b.ctx, a, g.ID)
				if e == nil || !reflect.DeepEqual(r, aep.Resolution{}) {
					done <- errors.New("wait released analysis body")
				} else {
					done <- nil
				}
			}()
			waitUntil := time.Now().Add(3 * time.Second)
			for {
				var waiting bool
				if e = b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND wait_event_type='Lock' AND query LIKE '%LOCK TABLE%agent_enrichment_purpose_previews%')`).Scan(&waiting); e != nil {
					t.Fatal(e)
				}
				if waiting {
					break
				}
				if time.Now().After(waitUntil) {
					t.Fatal("actual PostgreSQL relation wait not observed")
				}
				time.Sleep(10 * time.Millisecond)
			}
			switch mode {
			case "revoke":
				if _, e = held.Exec(b.ctx, `UPDATE consent_grants SET revision=revision+1,revoked_at=clock_timestamp() WHERE id=$1`, g.ID); e != nil {
					t.Fatal(e)
				}
			case "source_ABA":
				for _, body := range []string{"changed-while-wait", f.moment.Body} {
					if _, e = held.Exec(b.ctx, `UPDATE moments SET body=$2,revision=revision+1,updated_at=clock_timestamp() WHERE id=$1`, f.moment.ID, body); e != nil {
						t.Fatal(e)
					}
				}
			case "task_ABA":
				for _, query := range []string{"changed-while-wait", f.f.task.Query} {
					if _, e = held.Exec(b.ctx, `UPDATE agent_tasks SET query=$2,filters=jsonb_set(filters,'{currentQuery}',to_jsonb($2::text)),updated_at=clock_timestamp() WHERE id=$1`, f.f.task.ID, query); e != nil {
						t.Fatal(e)
					}
				}
			case "city_ABA":
				for _, status := range []string{"hidden", "published"} {
					if _, e = held.Exec(b.ctx, `UPDATE cities SET publication_status=$2 WHERE id=$1`, f.f.place.city, status); e != nil {
						t.Fatal(e)
					}
				}
			default:
				time.Sleep(450 * time.Millisecond)
			}
			if e = held.Commit(b.ctx); e != nil {
				t.Fatal(e)
			}
			select {
			case e := <-done:
				if e != nil {
					t.Fatal(e)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("native wait did not finish")
			}
		})
	}
}

func receiptPublicXmin(t *testing.T, p *pgxpool.Pool, ctx context.Context) string {
	t.Helper()
	c, e := p.Acquire(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Release()
	_, e = c.Exec(ctx, `CREATE OR REPLACE FUNCTION pg_temp.receipt_public_xmin() RETURNS jsonb LANGUAGE plpgsql AS $$ DECLARE item record;rows jsonb;result jsonb:='{}';BEGIN FOR item IN SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename LOOP EXECUTE format('SELECT coalesce(jsonb_agg(jsonb_build_object(''row'',to_jsonb(t),''xmin'',t.xmin::text) ORDER BY to_jsonb(t)::text),''[]''::jsonb) FROM public.%I t',item.tablename) INTO rows;result:=result||jsonb_build_object(item.tablename,rows);END LOOP;RETURN result;END $$`)
	if e != nil {
		t.Fatal(e)
	}
	var out string
	if e = c.QueryRow(ctx, `SELECT pg_temp.receipt_public_xmin()::text`).Scan(&out); e != nil {
		t.Fatal(e)
	}
	return out
}
func TestEnrichmentPreviewReceiptNativeOriginalHistoryNoSourceReadOrWrite(t *testing.T) {
	ownedMigrationDatabase(t)
	f := enrichmentPurposeNative(t)
	b := f.f.place.private.base
	a := f.f.place.private.owner
	p, e := b.store.PreviewOwnEnrichmentPurpose(b.ctx, a, f.selection)
	if e != nil {
		t.Fatal(e)
	}
	before := receiptPublicXmin(t, b.pool, b.ctx)
	r, e := b.store.ReadOwnEnrichmentPurposePreviewReceipt(b.ctx, a, p.ID)
	if e != nil || r.State != "OPEN_UNCONSUMED" || aep.ValidatePreviewReceipt(r, aep.Purpose) != nil {
		t.Fatal(r, e)
	}
	if receiptPublicXmin(t, b.pool, b.ctx) != before {
		t.Fatal("receipt GET wrote public rows/xmin")
	}
	g, e := b.store.ApproveOwnEnrichmentPurpose(b.ctx, a, p.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = b.store.RevokeOwnEnrichmentPurpose(b.ctx, a, g.ID, 1); e != nil {
		t.Fatal(e)
	}
	b.exec(`UPDATE moments SET body='CHANGED_PRIVATE_RECEIPT_CANARY',revision=revision+1,updated_at=clock_timestamp() WHERE id=$1`, f.moment.ID)
	b.exec(`UPDATE agent_tasks SET status='COMPLETED',updated_at=clock_timestamp() WHERE id=$1`, f.selection.TaskID)
	var token [32]byte
	for i := range token {
		token[i] = byte(i + 41)
	}
	b.exec(`WITH n AS MATERIALIZED(SELECT clock_timestamp() at) INSERT INTO sessions(id,account_id,token_sha256,expires_at,idle_expires_at,authentication_method) SELECT gen_random_uuid(),$1,$2,n.at+interval '1 hour',n.at+interval '30 minutes','test' FROM n`, b.person.ID, token[:])
	a.SessionDigest = token
	before = receiptPublicXmin(t, b.pool, b.ctx)
	r, e = b.store.ReadOwnEnrichmentPurposePreviewReceipt(b.ctx, a, p.ID)
	if e != nil || r.State != "APPROVAL_RECORDED" || r.ConsumedGrantID != g.ID {
		t.Fatal("old source/session/Task is not historical permission", r, e)
	}
	raw, _ := json.Marshal(r)
	for _, v := range []string{f.moment.Body, "CHANGED_PRIVATE_RECEIPT_CANARY", `"selection"`, `"taskQuery"`, `"review"`, `"content"`, `"authority"`} {
		if strings.Contains(string(raw), v) {
			t.Fatal("historical metadata read leaked private content")
		}
	}
	if receiptPublicXmin(t, b.pool, b.ctx) != before {
		t.Fatal("history GET wrote public rows/xmin")
	}
	if _, e = b.store.ReadOwnEnrichmentPurposePreviewReceipt(b.ctx, f.f.place.private.peer, p.ID); e == nil {
		t.Fatal("cross owner history allowed")
	}
}
func TestEnrichmentPreviewReceiptNativeClosedDeadlineAndRealTableWait(t *testing.T) {
	ownedMigrationDatabase(t)
	t.Run("unconsumed_after_fixed_deadline", func(t *testing.T) {
		f := enrichmentPurposeNative(t)
		b := f.f.place.private.base
		a := f.f.place.private.owner
		if e := b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()+interval '2 seconds'`).Scan(&f.selection.DeadlineAt); e != nil {
			t.Fatal(e)
		}
		p, e := b.store.PreviewOwnEnrichmentPurpose(b.ctx, a, f.selection)
		if e != nil {
			t.Fatal(e)
		}
		for {
			var expired bool
			if e = b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()>=$1`, p.ExpiresAt).Scan(&expired); e != nil {
				t.Fatal(e)
			}
			if expired {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		before := receiptPublicXmin(t, b.pool, b.ctx)
		r, e := b.store.ReadOwnEnrichmentPurposePreviewReceipt(b.ctx, a, p.ID)
		if e != nil || r.State != "CLOSED_UNCONSUMED" || !r.PreviewExpiresAt.Equal(p.ExpiresAt) {
			t.Fatal(r, e)
		}
		if receiptPublicXmin(t, b.pool, b.ctx) != before {
			t.Fatal("closed metadata wrote public rows/xmin")
		}
	})
	t.Run("current_session_expired_during_actual_wait", func(t *testing.T) {
		f := enrichmentPurposeNative(t)
		b := f.f.place.private.base
		a := f.f.place.private.owner
		p, _ := f.approve(t)
		b.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '2 seconds' WHERE token_sha256=$1`, a.SessionDigest[:])
		held, e := b.pool.Begin(b.ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer held.Rollback(context.Background())
		var pid int
		if e = held.QueryRow(b.ctx, `SELECT pg_backend_pid()`).Scan(&pid); e != nil {
			t.Fatal(e)
		}
		if _, e = held.Exec(b.ctx, `LOCK TABLE agent_enrichment_purpose_previews IN ACCESS EXCLUSIVE MODE`); e != nil {
			t.Fatal(e)
		}
		ctx, cancel := context.WithTimeout(b.ctx, 8*time.Second)
		defer cancel()
		done := make(chan error, 1)
		go func() {
			r, e := b.store.ReadOwnEnrichmentPurposePreviewReceipt(ctx, a, p.ID)
			if !errors.Is(e, aep.ErrDenied) || !reflect.DeepEqual(r, aep.PreviewReceipt{}) {
				done <- errors.New("wait must refuse expired current session with zero metadata")
			} else {
				done <- nil
			}
		}()
		end := time.Now().Add(3 * time.Second)
		for {
			var waiting bool
			if e = b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1=ANY(pg_blocking_pids(pid)) AND query LIKE '%LOCK TABLE%agent_enrichment_purpose_previews%')`, pid).Scan(&waiting); e != nil {
				t.Fatal(e)
			}
			if waiting {
				break
			}
			if time.Now().After(end) {
				t.Fatal("precise receipt table wait not reached")
			}
			time.Sleep(10 * time.Millisecond)
		}
		for {
			var expired bool
			if e = b.pool.QueryRow(b.ctx, `SELECT idle_expires_at<=clock_timestamp() FROM sessions WHERE token_sha256=$1`, a.SessionDigest[:]).Scan(&expired); e != nil {
				t.Fatal(e)
			}
			if expired {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if e = held.Rollback(b.ctx); e != nil {
			t.Fatal(e)
		}
		select {
		case e = <-done:
			if e != nil {
				t.Fatal(e)
			}
		case <-time.After(4 * time.Second):
			t.Fatal("receipt wait did not release")
		}
	})
}

func TestEnrichmentPreviewReceiptNativeIndependentOfLockedPrivateSourceTables(t *testing.T) {
	ownedMigrationDatabase(t)
	f := enrichmentPurposeNative(t)
	b := f.f.place.private.base
	a := f.f.place.private.owner
	p, g := f.approve(t)
	before := receiptPublicXmin(t, b.pool, b.ctx)
	held, e := b.pool.Begin(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer held.Rollback(context.Background())
	if _, e = held.Exec(b.ctx, `LOCK TABLE moments,agent_tasks,contexts,city_contexts,cities IN ACCESS EXCLUSIVE MODE`); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(b.ctx, 3*time.Second)
	defer cancel()
	r, e := b.store.ReadOwnEnrichmentPurposePreviewReceipt(ctx, a, p.ID)
	if e != nil || r.State != "APPROVAL_RECORDED" || r.ConsumedGrantID != g.ID {
		t.Fatal("bodyless history must not wait for private source/Task tables", r, e)
	}
	if e = held.Rollback(b.ctx); e != nil {
		t.Fatal(e)
	}
	if receiptPublicXmin(t, b.pool, b.ctx) != before {
		t.Fatal("independent receipt read wrote domain rows/xmin")
	}
}

func TestEnrichmentPreviewReceiptNativeWaitsForActualApproveOriginalAdvisory(t *testing.T) {
	ownedMigrationDatabase(t)
	f := enrichmentPurposeNative(t)
	b := f.f.place.private.base
	a := f.f.place.private.owner
	p, e := b.store.PreviewOwnEnrichmentPurpose(b.ctx, a, f.selection)
	if e != nil {
		t.Fatal(e)
	}
	before := receiptPublicXmin(t, b.pool, b.ctx)
	ctx, cancel := context.WithTimeout(b.ctx, 12*time.Second)
	defer cancel()
	held, e := b.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	approveDone := make(chan error, 1)
	readDone := make(chan error, 1)
	startedApprove, startedRead, consumedApprove, consumedRead := false, false, false, false
	defer func() {
		cancel()
		held.Rollback(context.Background())
		for _, x := range []struct {
			started, consumed bool
			done              chan error
		}{{startedApprove, consumedApprove, approveDone}, {startedRead, consumedRead, readDone}} {
			if x.started && !x.consumed {
				select {
				case <-x.done:
				case <-time.After(3 * time.Second):
					t.Error("receipt test goroutine cleanup timeout")
				}
			}
		}
	}()
	var holder int
	if e = held.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&holder); e != nil {
		t.Fatal(e)
	}
	var ignored string
	if e = held.QueryRow(ctx, `SELECT id::text FROM agent_enrichment_purpose_previews WHERE id=$1 FOR UPDATE`, p.ID).Scan(&ignored); e != nil {
		t.Fatal(e)
	}
	startedApprove = true
	go func() { _, e := b.store.ApproveOwnEnrichmentPurpose(ctx, a, p.ID); approveDone <- e }()
	var approver int
	for end := time.Now().Add(4 * time.Second); ; {
		e = b.pool.QueryRow(ctx, `SELECT pid FROM pg_stat_activity WHERE datname=current_database() AND $1=ANY(pg_blocking_pids(pid)) AND query LIKE '%selection,authority,source_binding,task_binding%FOR SHARE%'`, holder).Scan(&approver)
		if e == nil {
			break
		}
		select {
		case e = <-approveDone:
			consumedApprove = true
			t.Fatal("Approve exited before precise wait", e)
		default:
		}
		if time.Now().After(end) {
			t.Fatal("actual original Approve row barrier not reached", e)
		}
		time.Sleep(10 * time.Millisecond)
	}
	startedRead = true
	var r aep.PreviewReceipt
	go func() {
		var e error
		r, e = b.store.ReadOwnEnrichmentPurposePreviewReceipt(ctx, a, p.ID)
		readDone <- e
	}()
	for end := time.Now().Add(4 * time.Second); ; {
		var waiting bool
		if e = b.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1=ANY(pg_blocking_pids(pid)) AND query LIKE '%pg_advisory_xact_lock(hashtextextended($1,$2))%')`, approver).Scan(&waiting); e != nil {
			t.Fatal(e)
		}
		if waiting {
			break
		}
		select {
		case e = <-readDone:
			consumedRead = true
			t.Fatal("receipt returned before in-flight Approve", r, e)
		default:
		}
		if time.Now().After(end) {
			t.Fatal("receipt original approval advisory barrier not reached")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if e = held.Rollback(ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-approveDone:
		consumedApprove = true
		if e != nil {
			t.Fatal(e)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case e = <-readDone:
		consumedRead = true
		if e != nil || r.State != "APPROVAL_RECORDED" || r.ConsumedGrantID == "" {
			t.Fatal(r, e)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	after := receiptPublicXmin(t, b.pool, b.ctx)
	var left, right map[string]json.RawMessage
	if json.Unmarshal([]byte(before), &left) != nil || json.Unmarshal([]byte(after), &right) != nil {
		t.Fatal("snapshot shape")
	}
	for k, v := range left {
		if k != "consent_grants" && k != "agent_enrichment_purpose_bindings" && k != "audit_events" && !reflect.DeepEqual(v, right[k]) {
			t.Fatal("Approve+read unexpectedly wrote table", k)
		}
	}
	var bindings int
	if e = b.pool.QueryRow(ctx, `SELECT count(*) FROM agent_enrichment_purpose_bindings WHERE preview_id=$1 AND grant_id=$2`, p.ID, r.ConsumedGrantID).Scan(&bindings); e != nil || bindings != 1 {
		t.Fatal("exact original binding", bindings, e)
	}
	if _, e = b.store.ReadOwnEnrichmentPurposePreviewReceipt(ctx, a, p.ID); e != nil {
		t.Fatal(e)
	}
	if receiptPublicXmin(t, b.pool, ctx) != after {
		t.Fatal("receipt repeat wrote rows/xmin")
	}
}
