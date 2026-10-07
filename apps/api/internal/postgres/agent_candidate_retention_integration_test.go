package postgres

import (
	"context"
	"encoding/json"
	"errors"
	acr "github.com/birdtie/birdtie/apps/api/internal/agentcandidateretention"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

type retentionFixture struct {
	f         *enrichmentPurposeFixture
	selection acr.Selection
}

// Schema regressions downgrade actual unused dependencies, never omit latest
// migrations. Used v2 history cannot be removed to make old fixtures pass.
func candidateVocabularyFixture(t *testing.T, pool *pgxpool.Pool, ctx context.Context, up bool) bool {
	t.Helper()
	var present bool
	if e := pool.QueryRow(ctx, `SELECT to_regprocedure('public.birdtie_candidate_algorithm_vocabulary(text,text)') IS NOT NULL`).Scan(&present); e != nil {
		t.Fatal(e)
	}
	if !up && !present {
		return false
	}
	name := "../../migrations/092_agent_candidate_hiking_vocabulary.sql"
	if !up {
		name = "../../migrations/092_agent_candidate_hiking_vocabulary.down.sql"
	}
	raw, e := os.ReadFile(name)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, string(raw)); e != nil {
		t.Fatal("092 dependency fixture", up, e)
	}
	return present
}

func hikingAllRowXmin(t *testing.T, pool *pgxpool.Pool, ctx context.Context) string {
	t.Helper()
	conn, e := pool.Acquire(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Release()
	_, e = conn.Exec(ctx, `CREATE OR REPLACE FUNCTION pg_temp.hiking_all_xmin() RETURNS jsonb LANGUAGE plpgsql AS $$ DECLARE x record;v jsonb;r jsonb:='{}';BEGIN FOR x IN SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename LOOP EXECUTE format('SELECT coalesce(jsonb_agg(jsonb_build_object(''row'',to_jsonb(t),''xmin'',t.xmin::text) ORDER BY to_jsonb(t)::text),''[]''::jsonb) FROM public.%I t',x.tablename) INTO v;r:=r||jsonb_build_object(x.tablename,v);END LOOP;RETURN r;END $$`)
	if e != nil {
		t.Fatal(e)
	}
	var out string
	if e = conn.QueryRow(ctx, `SELECT pg_temp.hiking_all_xmin()::text`).Scan(&out); e != nil {
		t.Fatal(e)
	}
	return out
}

func retentionNative(t *testing.T) *retentionFixture {
	t.Helper()
	f := enrichmentPurposeNative(t)
	b := f.f.place.private.base
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for _, q := range []string{`DELETE FROM agent_candidate_retention_bindings WHERE grant_id IN(SELECT id FROM consent_grants WHERE owner_account_id=ANY($1::uuid[]))`, `DELETE FROM agent_candidate_retention_previews WHERE owner_id=ANY($1::uuid[])`} {
			if _, e := b.pool.Exec(ctx, q, b.accounts); e != nil {
				t.Error(e)
			}
		}
	})
	_, g := f.approve(t)
	return &retentionFixture{f: f, selection: acr.Selection{AnalysisGrantID: g.ID, RetainUntil: time.Now().UTC().Truncate(time.Microsecond).Add(time.Hour)}}
}
func (f *retentionFixture) approve(t *testing.T) (acr.Preview, acr.Grant) {
	t.Helper()
	b := f.f.f.place.private.base
	a := f.f.f.place.private.owner
	p, e := b.store.PreviewOwnCandidateRetention(b.ctx, a, f.selection)
	if e != nil {
		t.Fatal("retention preview", e)
	}
	g, e := b.store.ApproveOwnCandidateRetention(b.ctx, a, p.ID)
	if e != nil {
		t.Fatal("retention approve", e)
	}
	return p, g
}
func requireRetentionDenied(t *testing.T, s *Store, ctx context.Context, a agentprofile.PrivateAccess, id string) {
	t.Helper()
	r, e := s.ResolveOwnCandidateRetention(ctx, a, id)
	if e == nil || !reflect.DeepEqual(r, acr.Resolution{}) {
		t.Fatal("returned unapproved candidate payload", r, e)
	}
}
func TestCandidateRetentionNativeLifecycleIndependentNoCandidateWrite(t *testing.T) {
	f := retentionNative(t)
	b := f.f.f.place.private.base
	a := f.f.f.place.private.owner
	before := purposeOwnedControlRows(t, f.f.f.place.private)
	requireRetentionDenied(t, b.store, b.ctx, a, f.selection.AnalysisGrantID)
	p, g := f.approve(t)
	if acr.ValidatePreview(p) != nil || acr.ValidateGrant(g) != nil || p.Review.Proposal.Category != "badminton" || p.Review.Clusters != 1 || g.ExpiresAt.After(p.ExpiresAt) {
		t.Fatal(p, g)
	}
	r, e := b.store.ResolveOwnCandidateRetention(b.ctx, a, g.ID)
	if e != nil || r.Review.Proposal.Category != "badminton" || r.AnalysisTask != f.f.f.task.ID {
		t.Fatal(r, e)
	}
	if _, e = json.Marshal(r); !errors.Is(e, acr.ErrServerOnly) {
		t.Fatal("wire authority", e)
	}
	retry, e := b.store.ApproveOwnCandidateRetention(b.ctx, a, p.ID)
	if e != nil || retry.ID != g.ID || !retry.ExpiresAt.Equal(g.ExpiresAt) {
		t.Fatal("renewed retry", retry, e)
	}
	rec, e := b.store.ReadOwnCandidateRetentionPreview(b.ctx, a, p.ID)
	if e != nil || rec.State != "RECEIPT_ONLY" || rec.Review != nil || rec.ConsumedGrantID != g.ID {
		t.Fatal("unknown approve", rec, e)
	}
	revoked, e := b.store.RevokeOwnCandidateRetention(b.ctx, a, g.ID, 1)
	if e != nil || revoked.Revision != 2 || revoked.RevokedAt == nil {
		t.Fatal(revoked, e)
	}
	retry, e = b.store.RevokeOwnCandidateRetention(b.ctx, a, g.ID, 1)
	if e != nil || retry.Revision != 2 {
		t.Fatal(e)
	}
	requireRetentionDenied(t, b.store, b.ctx, a, g.ID)
	if _, e = b.store.ApproveOwnCandidateRetention(b.ctx, a, p.ID); !errors.Is(e, acr.ErrExpired) {
		t.Fatal("revoked retry", e)
	}
	if purposeOwnedControlRows(t, f.f.f.place.private) != before {
		t.Fatal("analysis/retention approval mutated candidate/memory/effect/control/session")
	}
	var raw string
	if e = b.pool.QueryRow(b.ctx, `SELECT to_jsonb(p)::text FROM agent_candidate_retention_previews p WHERE id=$1`, p.ID).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	if containsPrivateString(raw, []string{f.f.moment.Body, f.f.moment.Title, f.f.f.task.Query}) {
		t.Fatal("source body persisted", raw)
	}
}
func TestCandidateRetentionNativeCurrentIdentitySourceAndPurpose(t *testing.T) {
	for _, mode := range []string{"analysis_revoke", "source_revision", "source_ABA", "task_ABA", "account_ABA", "agent_ABA", "metadata_ABA", "session_revoke", "other_person", "organization", "wrong_session", "expired", "city_ABA", "title_not_selected", "negated", "ambiguous"} {
		t.Run(mode, func(t *testing.T) {
			f := retentionNative(t)
			b := f.f.f.place.private.base
			a := f.f.f.place.private.owner
			if mode == "title_not_selected" || mode == "negated" || mode == "ambiguous" {
				body := "无明确类别"
				if mode == "negated" {
					body = "我不打羽毛球"
				}
				if mode == "ambiguous" {
					body = "羽毛球和篮球"
				}
				b.exec(`UPDATE moments SET title='羽毛球',body=$2,revision=revision+1,updated_at=clock_timestamp() WHERE id=$1`, f.f.moment.ID, body)
				f.f.selection.MomentRevision++
				_, g := f.f.approve(t)
				f.selection.AnalysisGrantID = g.ID
				if _, e := b.store.PreviewOwnCandidateRetention(b.ctx, a, f.selection); !errors.Is(e, acr.ErrUnavailable) {
					t.Fatal("not an approved exact unambiguous selected hypothesis", e)
				}
				return
			}
			if mode == "expired" {
				f.selection.RetainUntil = time.Now().UTC().Truncate(time.Microsecond).Add(400 * time.Millisecond)
			}
			_, g := f.approve(t)
			switch mode {
			case "analysis_revoke":
				if _, e := b.store.RevokeOwnEnrichmentPurpose(b.ctx, a, f.selection.AnalysisGrantID, 1); e != nil {
					t.Fatal(e)
				}
			case "source_revision":
				b.exec(`UPDATE moments SET revision=revision+1,body='different',updated_at=clock_timestamp() WHERE id=$1`, f.f.moment.ID)
			case "source_ABA":
				for _, body := range []string{"different", f.f.moment.Body} {
					b.exec(`UPDATE moments SET revision=revision+1,body=$2,updated_at=clock_timestamp() WHERE id=$1`, f.f.moment.ID, body)
				}
			case "task_ABA":
				for _, q := range []string{"different", f.f.f.task.Query} {
					b.exec(`UPDATE agent_tasks SET query=$2,filters=jsonb_set(filters,'{currentQuery}',to_jsonb($2::text)),updated_at=clock_timestamp() WHERE id=$1`, f.f.f.task.ID, q)
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
			case "other_person":
				a = f.f.f.place.private.peer
			case "organization":
				a = f.f.f.place.private.org
			case "wrong_session":
				a.SessionDigest = f.f.f.place.private.peer.SessionDigest
			case "expired":
				time.Sleep(450 * time.Millisecond)
			case "city_ABA":
				for _, status := range []string{"hidden", "published"} {
					b.exec(`UPDATE cities SET publication_status=$2 WHERE id=$1`, f.f.f.place.city, status)
				}
			}
			requireRetentionDenied(t, b.store, b.ctx, a, g.ID)
		})
	}
}
func TestCandidateRetentionNativeHumanReceiptNewSessionCannotResolve(t *testing.T) {
	f := retentionNative(t)
	b := f.f.f.place.private.base
	a := f.f.f.place.private.owner
	p, g := f.approve(t)
	var token [32]byte
	for i := range token {
		token[i] = byte(i + 13)
	}
	b.exec(`INSERT INTO sessions(id,account_id,token_sha256,expires_at,idle_expires_at,authentication_method) VALUES(gen_random_uuid(),$1,$2,clock_timestamp()+interval '1 hour',clock_timestamp()+interval '30 minutes','test')`, b.person.ID, token[:])
	a.SessionDigest = token
	requireRetentionDenied(t, b.store, b.ctx, a, g.ID)
	rec, e := b.store.ReadOwnCandidateRetentionPreview(b.ctx, a, p.ID)
	if e != nil || rec.State != "RECEIPT_ONLY" || rec.Review != nil {
		t.Fatal(rec, e)
	}
	if _, e = b.store.ReadOwnCandidateRetention(b.ctx, a, g.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = b.store.ApproveOwnCandidateRetention(b.ctx, a, p.ID); !errors.Is(e, acr.ErrDenied) {
		t.Fatal("new session approve inherited", e)
	}
	if _, e = b.store.RevokeOwnCandidateRetention(b.ctx, a, g.ID, 1); e != nil {
		t.Fatal(e)
	}
}
func TestCandidateRetentionNativeGrantImmutableUsedDownAtomic(t *testing.T) {
	ownedMigrationDatabase(t)
	f := retentionNative(t)
	b := f.f.f.place.private.base
	_, g := f.approve(t)
	for _, q := range []string{`UPDATE consent_grants SET expires_at=expires_at+interval '1 minute' WHERE id=$1`, `UPDATE consent_grants SET actions=ARRAY['stage_candidate','write'] WHERE id=$1`, `UPDATE consent_grants SET purpose='MOMENT_LOCAL_ANALYSIS' WHERE id=$1`, `UPDATE consent_grants SET revision=revision+1 WHERE id=$1`} {
		if _, e := b.pool.Exec(b.ctx, q, g.ID); e == nil {
			t.Fatal("grant metadata mutation admitted", q)
		}
	}
	if _, e := b.store.RevokeOwnCandidateRetention(b.ctx, f.f.f.place.private.owner, g.ID, 1); e != nil {
		t.Fatal(e)
	}
	if _, e := b.pool.Exec(b.ctx, `UPDATE consent_grants SET revoked_at=NULL,revision=revision+1 WHERE id=$1`, g.ID); e == nil {
		t.Fatal("grant revoke clear admitted")
	}
	down, e := os.ReadFile("../../migrations/080_agent_candidate_retention.down.sql")
	if e != nil {
		t.Fatal(e)
	}
	before := enrichmentAllPublic(t, b.pool, b.ctx)
	conn, e := b.pool.Acquire(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	_, e = conn.Exec(b.ctx, string(down))
	_, roll := conn.Exec(b.ctx, "ROLLBACK")
	conn.Release()
	if e == nil || roll != nil {
		t.Fatal("used down", e, roll)
	}
	if enrichmentAllPublic(t, b.pool, b.ctx) != before {
		t.Fatal("failed used-down changed old rows")
	}
}
func TestCandidateRetentionNativeSchemaGateAndNoExpiryRenewal(t *testing.T) {
	ownedMigrationDatabase(t)
	f := retentionNative(t)
	b := f.f.f.place.private.base
	a := f.f.f.place.private.owner
	dependent092 := candidateVocabularyFixture(t, b.pool, b.ctx, false)
	down, e := os.ReadFile("../../migrations/080_agent_candidate_retention.down.sql")
	if e != nil {
		t.Fatal(e)
	}
	up, e := os.ReadFile("../../migrations/080_agent_candidate_retention.sql")
	if e != nil {
		t.Fatal(e)
	}
	before := enrichmentAllPublic(t, b.pool, b.ctx)
	if _, e = b.pool.Exec(b.ctx, string(down)); e != nil {
		t.Fatal(e)
	}
	if _, e = b.store.PreviewOwnCandidateRetention(b.ctx, a, f.selection); !errors.Is(e, acr.ErrUnavailable) {
		t.Fatal("missing080", e)
	}
	if _, e = b.pool.Exec(b.ctx, string(up)); e != nil {
		t.Fatal(e)
	}
	if dependent092 {
		candidateVocabularyFixture(t, b.pool, b.ctx, true)
	}
	if enrichmentAllPublic(t, b.pool, b.ctx) != before {
		t.Fatal("unused roundtrip changed old rows")
	}
	for _, q := range []string{`ALTER TABLE consent_grants DISABLE TRIGGER agent_candidate_retention_grant_guard`, `ALTER TABLE agent_candidate_retention_previews DISABLE TRIGGER agent_candidate_retention_preview_guard`, `ALTER TABLE agent_candidate_retention_bindings DISABLE TRIGGER agent_candidate_retention_binding_guard`} {
		b.exec(q)
		if _, e = b.store.PreviewOwnCandidateRetention(b.ctx, a, f.selection); !errors.Is(e, acr.ErrUnavailable) {
			t.Fatal("disabled guard", e)
		}
		b.exec(strings.Replace(q, "DISABLE", "ENABLE", 1))
	}
	b.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '30 seconds' WHERE token_sha256=$1`, a.SessionDigest[:])
	p, e := b.store.PreviewOwnCandidateRetention(b.ctx, a, f.selection)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = b.store.Authenticate(b.ctx, a.SessionDigest); e != nil {
		t.Fatal(e)
	}
	g, e := b.store.ApproveOwnCandidateRetention(b.ctx, a, p.ID)
	if e != nil || g.ExpiresAt.After(p.ExpiresAt) {
		t.Fatal("idle refresh extended original review", p, g, e)
	}
	b.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '10 seconds' WHERE token_sha256=$1`, a.SessionDigest[:])
	r, e := b.store.ResolveOwnCandidateRetention(b.ctx, a, g.ID)
	if e != nil || r.Grant.ExpiresAt.After(time.Now().Add(11*time.Second)) {
		t.Fatal("lease past current idle", r, e)
	}
}

func TestCandidateRetentionNativeRealWaitExpiryAndRevoke(t *testing.T) {
	ownedMigrationDatabase(t)
	for _, mode := range []string{"natural_expiry", "revoke", "analysis_revoke", "source_ABA", "task_ABA", "city_ABA", "session_expiry"} {
		t.Run(mode, func(t *testing.T) {
			f := retentionNative(t)
			b := f.f.f.place.private.base
			a := f.f.f.place.private.owner
			if mode == "natural_expiry" {
				f.selection.RetainUntil = time.Now().UTC().Truncate(time.Microsecond).Add(400 * time.Millisecond)
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
			if _, e = held.Exec(b.ctx, `LOCK TABLE moments IN ACCESS EXCLUSIVE MODE`); e != nil {
				t.Fatal(e)
			}
			done := make(chan error, 1)
			go func() {
				r, e := b.store.ResolveOwnCandidateRetention(b.ctx, a, g.ID)
				if e == nil || !reflect.DeepEqual(r, acr.Resolution{}) {
					done <- errors.New("wait released analysis body")
				} else {
					done <- nil
				}
			}()
			waitUntil := time.Now().Add(3 * time.Second)
			for {
				var waiting bool
				if e = b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND wait_event_type='Lock' AND query LIKE '%LOCK TABLE%moments%')`).Scan(&waiting); e != nil {
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
			case "analysis_revoke":
				if _, e = held.Exec(b.ctx, `UPDATE consent_grants SET revision=revision+1,revoked_at=clock_timestamp() WHERE id=$1`, f.selection.AnalysisGrantID); e != nil {
					t.Fatal(e)
				}
			case "revoke":
				if _, e = held.Exec(b.ctx, `UPDATE consent_grants SET revision=revision+1,revoked_at=clock_timestamp() WHERE id=$1`, g.ID); e != nil {
					t.Fatal(e)
				}
			case "source_ABA":
				for _, body := range []string{"changed-while-wait", f.f.moment.Body} {
					if _, e = held.Exec(b.ctx, `UPDATE moments SET body=$2,revision=revision+1,updated_at=clock_timestamp() WHERE id=$1`, f.f.moment.ID, body); e != nil {
						t.Fatal(e)
					}
				}
			case "task_ABA":
				for _, query := range []string{"changed-while-wait", f.f.f.task.Query} {
					if _, e = held.Exec(b.ctx, `UPDATE agent_tasks SET query=$2,filters=jsonb_set(filters,'{currentQuery}',to_jsonb($2::text)),updated_at=clock_timestamp() WHERE id=$1`, f.f.f.task.ID, query); e != nil {
						t.Fatal(e)
					}
				}
			case "city_ABA":
				for _, status := range []string{"hidden", "published"} {
					if _, e = held.Exec(b.ctx, `UPDATE cities SET publication_status=$2 WHERE id=$1`, f.f.f.place.city, status); e != nil {
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

func TestCandidateRetentionNativeLinkedMomentHasNoRetentionPermission(t *testing.T) {
	f := retentionNative(t)
	b := f.f.f.place.private.base
	a := f.f.f.place.private.owner
	m, e := b.store.UpdateMomentDraft(b.ctx, b.person.ID, f.f.moment.ID, f.f.moment.Revision, content.MomentInput{CityID: f.f.f.place.city, Title: f.f.moment.Title, Body: f.f.moment.Body, TimePrecision: "unknown", LocationPrecision: "city", ActivityID: &f.f.f.public})
	if e != nil {
		t.Fatal("real original linked Moment mutation", e)
	}
	f.f.selection.MomentRevision = m.Revision
	_, ag := f.f.approve(t)
	f.selection.AnalysisGrantID = ag.ID
	if _, e = b.store.PreviewOwnCandidateRetention(b.ctx, a, f.selection); !errors.Is(e, acr.ErrDenied) {
		t.Fatal("analysis of concrete text became analysis of linked Activity", e)
	}
	var count int
	if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_candidate_retention_previews WHERE owner_id=$1`, b.person.ID).Scan(&count); e != nil || count != 0 {
		t.Fatal("linked rejection made retention preview", count, e)
	}
}
func TestCandidateRetentionNativePreviewRefusesUnboundGenericGrant(t *testing.T) {
	f := retentionNative(t)
	b := f.f.f.place.private.base
	a := f.f.f.place.private.owner
	var generic string
	if e := b.pool.QueryRow(b.ctx, `INSERT INTO consent_grants(id,owner_account_id,recipient_account_id,resource_type,resource_id,purpose,actions,expires_at) VALUES(gen_random_uuid(),$1,$1,'profile',$1::uuid::text,'profile_view',ARRAY['read'],clock_timestamp()+interval '1 hour') RETURNING id`, b.person.ID).Scan(&generic); e != nil {
		t.Fatal(e)
	}
	f.selection.AnalysisGrantID = generic
	if _, e := b.store.PreviewOwnCandidateRetention(b.ctx, a, f.selection); !errors.Is(e, acr.ErrDenied) {
		t.Fatal("generic profile grant passed analysis purpose", e)
	}
	var count int
	if e := b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_candidate_retention_previews WHERE owner_id=$1`, b.person.ID).Scan(&count); e != nil || count != 0 {
		t.Fatal(count, e)
	}
}
