package postgres

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"os"
	"reflect"
	"testing"
)

func TestMemoryCorrectionNativeMigrationUnusedOldRowsAndUsedHistory(t *testing.T) {
	ownedMigrationDatabase(t)
	f := agentMemoryTestFixture(t)
	b := f.base
	id := agentMemoryID(t, f)
	m := mustPutAgentMemory(t, f, id, agentMemoryInput("migration.nonempty"))
	before := initialCaptureSnapshot(t, b.pool, b.ctx, "correction-unused-before")
	up, e := os.ReadFile("../../migrations/094_agent_memory_correction.sql")
	if e != nil {
		t.Fatal(e)
	}
	down, e := os.ReadFile("../../migrations/094_agent_memory_correction.down.sql")
	if e != nil {
		t.Fatal(e)
	}
	catalog := correctionCatalog(t, f)
	if _, e = b.pool.Exec(b.ctx, string(down)); e != nil {
		t.Fatal("actual unused down", e)
	}
	removed := initialCaptureSnapshot(t, b.pool, b.ctx, "correction-unused-down")
	if len(removed)+3 != len(before) {
		t.Fatal("wrong migration table delta")
	}
	for k, v := range removed {
		if v != before[k] {
			t.Fatal("unused down changed nonempty old rows/xmin", k)
		}
	}
	if _, e = b.pool.Exec(b.ctx, string(up)); e != nil {
		t.Fatal("actual reapply", e)
	}
	after := initialCaptureSnapshot(t, b.pool, b.ctx, "correction-unused-reapplied")
	if !reflect.DeepEqual(before, after) || catalog != correctionCatalog(t, f) {
		t.Fatal("reapply did not preserve original full rows/xmin/catalog")
	}
	p := correctionPreview(t, f, id, m.Version, "DELETE", "", nil)
	reject := func() {
		t.Helper()
		before := correctionPairs(t, b.pool, b.ctx)
		conn, e := b.pool.Acquire(b.ctx)
		if e != nil {
			t.Fatal(e)
		}
		_, e = conn.Exec(b.ctx, string(down))
		var pe *pgconn.PgError
		if !errors.As(e, &pe) || pe.Code != "55000" {
			t.Fatal("used down did not explicitly reject", e)
		}
		if _, e = conn.Exec(b.ctx, "ROLLBACK"); e != nil {
			t.Fatal(e)
		}
		conn.Release()
		if before != correctionPairs(t, b.pool, b.ctx) {
			t.Fatal("used down changed actual history")
		}
	}
	reject()
	r := correctionConfirm(t, f, p)
	reject()
	again, e := b.store.ReadOwnMemoryCorrection(b.ctx, f.owner, p.ID)
	if e != nil || again.State != "COMMITTED" || *again.ResultMemoryVersion != *r.ResultMemoryVersion {
		t.Fatal("used history lost", e)
	}
	for _, q := range []string{`UPDATE agent_memory_corrections SET expires_at=expires_at+interval '1 second' WHERE id=$1`, `DELETE FROM agent_memory_corrections WHERE id=$1`, `UPDATE agent_memory_corrections SET committed_at=committed_at WHERE id=$1`} {
		tx, e := b.pool.Begin(b.ctx)
		if e != nil {
			t.Fatal(e)
		}
		_, e = tx.Exec(b.ctx, q, p.ID)
		tx.Rollback(context.Background())
		if e == nil {
			t.Fatal("original deadline/history mutable")
		}
	}

}

func correctionCatalog(t *testing.T, f *agentPrivateFixture) string {
	t.Helper()
	b := f.base
	var raw string
	if e := b.pool.QueryRow(b.ctx, `SELECT jsonb_build_object(
 'functions',(SELECT coalesce(jsonb_agg(jsonb_build_object('name',p.proname,'args',pg_get_function_identity_arguments(p.oid),'definition',pg_get_functiondef(p.oid)) ORDER BY p.proname,pg_get_function_identity_arguments(p.oid)),'[]') FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='public' AND p.prokind='f'),
 'triggers',(SELECT coalesce(jsonb_agg(jsonb_build_object('table',c.relname,'name',t.tgname,'definition',pg_get_triggerdef(t.oid),'enabled',t.tgenabled) ORDER BY c.relname,t.tgname),'[]') FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND NOT t.tgisinternal),
 'constraints',(SELECT coalesce(jsonb_agg(jsonb_build_object('table',r.relname,'name',c.conname,'definition',pg_get_constraintdef(c.oid)) ORDER BY r.relname,c.conname),'[]') FROM pg_constraint c JOIN pg_class r ON r.oid=c.conrelid JOIN pg_namespace n ON n.oid=r.relnamespace WHERE n.nspname='public'),
 'columns',(SELECT coalesce(jsonb_agg(jsonb_build_object('table',c.relname,'name',a.attname,'ordinal',CASE WHEN c.relname='agent_effect_ledger' AND a.attname IN('candidate_id','retention_grant_id','event_id','handler_version','fence','attempt','proof_review') THEN NULL ELSE a.attnum END,'type',format_type(a.atttypid,a.atttypmod),'notNull',a.attnotnull,'default',pg_get_expr(d.adbin,d.adrelid),'identity',a.attidentity,'generated',a.attgenerated) ORDER BY c.relname,a.attname),'[]') FROM pg_attribute a JOIN pg_class c ON c.oid=a.attrelid JOIN pg_namespace n ON n.oid=c.relnamespace LEFT JOIN pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum WHERE n.nspname='public' AND c.relkind IN('r','p','v','m') AND a.attnum>0 AND NOT a.attisdropped),
 'indexes',(SELECT coalesce(jsonb_agg(jsonb_build_object('table',t.relname,'name',i.relname,'definition',pg_get_indexdef(x.indexrelid),'valid',x.indisvalid,'ready',x.indisready) ORDER BY t.relname,i.relname),'[]') FROM pg_index x JOIN pg_class t ON t.oid=x.indrelid JOIN pg_class i ON i.oid=x.indexrelid JOIN pg_namespace n ON n.oid=t.relnamespace WHERE n.nspname='public'),
 'tables',(SELECT coalesce(jsonb_agg(jsonb_build_object('name',c.relname,'kind',c.relkind,'owner',pg_get_userbyid(c.relowner),'rls',c.relrowsecurity,'forceRLS',c.relforcerowsecurity,'privileges',c.relacl) ORDER BY c.relname),'[]') FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind IN('r','p','v','m','S')),
 'policies',(SELECT coalesce(jsonb_agg(to_jsonb(p) ORDER BY tablename,policyname),'[]') FROM pg_policies p WHERE schemaname='public'))::text`).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	return raw
}
