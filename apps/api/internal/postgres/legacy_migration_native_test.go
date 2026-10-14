package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const legacyTestRunID = "14600000-0000-4000-8000-000000000001"

func legacyTestTarget() LegacyMigrationTargetIdentity {
	return LegacyMigrationTargetIdentity{DatabaseName: "birdtie_owned_legacy_test", DatabaseOID: 100,
		Owner: "birdtie", Marker: "birdtie-owned-legacy-import-2026-10-15:" + strings.Repeat("a", 32), ForbiddenSourceDatabaseOID: 99}
}

func TestLegacyImportTargetAndDependencyFailClosed(t *testing.T) {
	good := legacyTestTarget()
	if ValidateLegacyMigrationTarget(good) != nil {
		t.Fatal("exact independent native target rejected")
	}
	mutations := []func(*LegacyMigrationTargetIdentity){
		func(v *LegacyMigrationTargetIdentity) { v.DatabaseName = "civu" },
		func(v *LegacyMigrationTargetIdentity) { v.DatabaseName = "civu_restore" },
		func(v *LegacyMigrationTargetIdentity) { v.DatabaseName = "postgres" },
		func(v *LegacyMigrationTargetIdentity) { v.DatabaseName = "../native" },
		func(v *LegacyMigrationTargetIdentity) { v.DatabaseOID = v.ForbiddenSourceDatabaseOID },
		func(v *LegacyMigrationTargetIdentity) { v.ForbiddenSourceDatabaseOID = 0 },
		func(v *LegacyMigrationTargetIdentity) { v.DatabaseOID = 4294967296 },
		func(v *LegacyMigrationTargetIdentity) { v.Owner = "" },
		func(v *LegacyMigrationTargetIdentity) { v.Marker += "\n" },
	}
	for i, change := range mutations {
		v := good
		change(&v)
		if ValidateLegacyMigrationTarget(v) == nil {
			t.Fatalf("unsafe target mutation %d accepted", i)
		}
	}
	if importer, err := NewLegacyMigrationImporter(nil, good); importer != nil || !errors.Is(err, ErrLegacyMigrationGuard) {
		t.Fatal("missing target dependency accepted")
	}
	var importer *LegacyMigrationImporter
	if result, err := importer.Apply(context.Background(), legacyTestRunID, []byte("private"), []byte("private")); err == nil || result.Status != "UNKNOWN" {
		t.Fatal("nil target implied a successful/no-effect run")
	}
	if result, err := importer.InspectRun(context.Background(), legacyTestRunID); err == nil || result.Status != "UNKNOWN" {
		t.Fatal("nil inspector invented original facts")
	}
}

func TestLegacyImportErrorsAndSummaryExcludePrivateDetails(t *testing.T) {
	for _, err := range []error{&pgconn.PgError{Code: "23505", Message: "private body", Detail: "private phone"},
		errors.New("postgres://private-password@source/private-sql")} {
		fixed := legacyMigrationFailure(err)
		if strings.Contains(fixed.Error(), "private") || strings.Contains(fixed.Error(), "postgres://") {
			t.Fatal("SQL/credential detail escaped")
		}
	}
	raw, err := json.Marshal(LegacyMigrationResult{RunID: legacyTestRunID, Status: "COMMITTED",
		SourceFileSHA256: "private-source-digest", ManifestFileSHA256: "private-manifest-digest"})
	if err != nil || strings.Contains(string(raw), "private") {
		t.Fatal("summary leaked internal source facts")
	}
}

func legacySyntheticInput(t *testing.T, change func(map[string]any)) ([]byte, []byte) {
	t.Helper()
	tables := map[string]any{}
	for _, name := range legacymigration.Tables() {
		tables[name] = []any{}
	}
	row := func(table string) map[string]any {
		r := map[string]any{}
		for _, column := range legacymigration.ColumnNames(table) {
			r[column] = nil
		}
		return r
	}
	ownerID := "14600000-0000-4000-8000-000000000010"
	u := row("users")
	u["id"], u["version"], u["status"], u["data_origin"] = ownerID, 1, "active", "synthetic_test"
	u["display_name"], u["bio"], u["handle"] = "仅测试档案用户", "测试简介", "legacy-test-handle"
	tables["users"] = []any{u}
	n := row("notes")
	n["id"], n["author_id"], n["version"] = "14600000-0000-4000-8000-000000000020", ownerID, 1
	n["title"], n["caption"], n["visibility"], n["moderation_status"] = "", "仅测试旧正文", "public", "approved"
	tombstone := row("notes")
	for k, v := range n {
		tombstone[k] = v
	}
	tombstone["id"], tombstone["deleted_at"], tombstone["caption"] = "14600000-0000-4000-8000-000000000021", "2026-10-14T00:00:00Z", "删除正文不得迁入"
	tables["notes"] = []any{n, tombstone}
	if change != nil {
		change(tables)
	}
	header := map[string]any{"instanceTag": legacymigration.SourceSystem, "container": "birdtie-civu-restore-20261014",
		"database": "civu_restore", "backupCipherSha256": legacymigration.BackupCipherSHA256, "schemaSha256": legacymigration.SourceSchemaSHA256}
	snapshot, err := legacymigration.Digest(map[string]any{"source": header, "tables": tables})
	if err != nil {
		t.Fatal("synthetic snapshot could not be made")
	}
	header["snapshotSha256"] = snapshot
	source, err := json.Marshal(map[string]any{"schemaVersion": legacymigration.SourceProtocol, "source": header,
		"capture": map[string]any{"observedAt": "2026-10-15T00:00:00Z", "transactionSnapshot": "100:100:"}, "tables": tables})
	if err != nil {
		t.Fatal("synthetic source could not be made")
	}
	counts, hashes := map[string]any{}, map[string]any{}
	for name, list := range tables {
		counts[name] = len(list.([]any))
		hashes[name], err = legacymigration.Digest(list)
		if err != nil {
			t.Fatal("synthetic table hash failed")
		}
	}
	sum := sha256.Sum256(source)
	manifest, err := json.Marshal(map[string]any{"schemaVersion": legacymigration.ManifestProtocol, "source": header,
		"sourceFileSha256": hex.EncodeToString(sum[:]), "auditedSchemaOutputSha256": legacymigration.AuditedOutputSHA256,
		"counts": counts, "tableSha256": hashes, "families": map[string]any{
			"accounts": []string{"users", "auth_identities"}, "places": []string{"cities", "regions", "geo_areas", "places", "place_area_relations", "place_provider_refs"},
			"saved": []string{"user_saved_places", "place_save_sources"}, "collections": []string{"journeys", "journey_stops", "journey_arrangements", "journey_notes", "journey_members", "journey_media", "journey_publications", "journey_publication_places", "theme_maps", "theme_map_entries", "theme_map_entry_sources"},
			"notes": []string{"notes", "note_places", "note_media", "note_media_locations", "moment_threads", "moment_thread_legs"}, "media": []string{"media_assets", "place_images"}},
		"execution": map[string]any{"preservation": "EXTRACTED_ONLY", "targetImport": "NOT_RUN", "identityVerification": "NOT_RUN", "sourceRights": "NOT_RUN", "mediaBytesMatched": "NOT_RUN", "mediaAuthorization": "NOT_RUN", "sessionMigration": "NOT_RUN", "relationshipEffects": 0, "sourceWrites": 0, "runtimeStarted": false}})
	if err != nil {
		t.Fatal("synthetic manifest could not be made")
	}
	if _, err := legacymigration.BuildPlan(legacyTestRunID, source, manifest); err != nil {
		t.Fatal("synthetic closed source rejected")
	}
	return source, manifest
}

type legacyFakeRow struct {
	values []any
	err    error
}

func (r legacyFakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.values) {
		return errors.New("fake scan mismatch")
	}
	for i, v := range r.values {
		reflect.ValueOf(dest[i]).Elem().Set(reflect.ValueOf(v))
	}
	return nil
}

type legacyUnknownDB struct {
	tx    *legacyUnknownTx
	calls int
}

func (d *legacyUnknownDB) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	d.calls++
	return d.tx, nil
}

type legacyUnknownTx struct {
	pgx.Tx
	t              *testing.T
	plan           legacymigration.Plan
	target         LegacyMigrationTargetIdentity
	written        bool
	execs, commits int
}

func (x *legacyUnknownTx) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	if query == legacyMigrationTargetSQL {
		return legacyFakeRow{values: []any{x.target.DatabaseName, x.target.DatabaseOID, x.target.Owner, x.target.Owner, x.target.Marker, false, true}}
	}
	if strings.Contains(query, "FROM public.legacy_import_runs WHERE") {
		if !x.written {
			return legacyFakeRow{err: pgx.ErrNoRows}
		}
		r := x.plan.Run
		counts, _ := json.Marshal(r.Counts)
		deferred, _ := json.Marshal(x.plan.Deferred)
		return legacyFakeRow{values: []any{r.RunID, r.SourceSystem, r.SourceProtocol, r.BackupCipherSHA256, r.SchemaSHA256, r.SnapshotSHA256, r.SourceFileSHA256, r.ManifestFileSHA256, []byte(r.Header), counts, deferred, time.Now().UTC()}}
	}
	if strings.Contains(query, "SELECT EXISTS(SELECT 1 FROM public.legacy_entity_mappings") {
		return legacyFakeRow{values: []any{false}}
	}
	if strings.Contains(query, "SELECT count(*) FROM public.legacy_entity_mappings") {
		return legacyFakeRow{values: []any{int64(1), int64(2), int64(1), int64(1)}}
	}
	x.t.Fatal("unrecognized synthetic query")
	return legacyFakeRow{err: ErrLegacyMigrationUnavailable}
}
func (x *legacyUnknownTx) Exec(_ context.Context, query string, _ ...any) (pgconn.CommandTag, error) {
	x.execs++
	if strings.Contains(query, "INSERT INTO public.legacy_import_runs") {
		x.written = true
	}
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}
func (x *legacyUnknownTx) Commit(context.Context) error {
	x.commits++
	return errors.New("private lost COMMIT transport detail")
}
func (x *legacyUnknownTx) Rollback(context.Context) error { return nil }

func TestLegacyImportUnknownCommitDoesNotRetryOrAllocateAnotherRun(t *testing.T) {
	source, manifest := legacySyntheticInput(t, nil)
	plan, err := legacymigration.BuildPlan(legacyTestRunID, source, manifest)
	if err != nil {
		t.Fatal("plan failed")
	}
	tx := &legacyUnknownTx{t: t, plan: plan, target: legacyTestTarget()}
	db := &legacyUnknownDB{tx: tx}
	importer := &LegacyMigrationImporter{database: db, expected: tx.target}
	result, err := importer.Apply(context.Background(), legacyTestRunID, source, manifest)
	if !errors.Is(err, ErrLegacyMigrationUnknown) || result.RunID != legacyTestRunID || result.Status != "UNKNOWN" || db.calls != 1 || tx.commits != 1 {
		t.Fatal("unknown COMMIT was retried or treated as no effect")
	}
	before := tx.execs
	inspected, err := importer.InspectRun(context.Background(), legacyTestRunID)
	if err != nil || inspected.Status != "COMMITTED" || inspected.RunID != legacyTestRunID || tx.execs != before || tx.commits != 1 {
		t.Fatal("inspection wrote or invented another run")
	}
}

type legacyNativeFixture struct {
	Context  context.Context
	Pool     *pgxpool.Pool
	Importer *LegacyMigrationImporter
	Target   LegacyMigrationTargetIdentity
}

// Root can reuse this same explicitly enabled child fixture for owner-only
// archive reads. No apply, activation or real source input occurs in setup.
func legacyMigrationNativeFixture(t *testing.T) *legacyNativeFixture {
	t.Helper()
	if os.Getenv("BIRDTIE_LEGACY_IMPORT_NATIVE_TEST") != "1" {
		t.Skip("NOT_RUN: explicit native integration activation missing")
	}
	ownedMigrationDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	pool, err := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if err != nil {
		cancel()
		t.Fatal("native child unavailable")
	}
	t.Cleanup(func() { pool.Close(); cancel() })
	var target LegacyMigrationTargetIdentity
	if err = pool.QueryRow(ctx, `SELECT current_database(),oid::bigint,pg_get_userbyid(datdba) FROM pg_database WHERE datname=current_database()`).Scan(&target.DatabaseName, &target.DatabaseOID, &target.Owner); err != nil {
		t.Fatal("native identity unavailable")
	}
	if !strings.HasPrefix(target.DatabaseName, "birdtie_owned_migration_") {
		t.Fatal("fixture is not its independent child")
	}
	id, err := legacyMigrationUUID()
	if err != nil {
		t.Fatal("fixture marker failed")
	}
	target.Marker = "birdtie-owned-legacy-import-2026-10-15:" + strings.ReplaceAll(id, "-", "")
	// The fixture source is synthetic, so this distinct forbidden OID is a
	// negative-identity sentinel; root separately audits the real source OID.
	target.ForbiddenSourceDatabaseOID = target.DatabaseOID + 1
	if _, err = pool.Exec(ctx, "COMMENT ON DATABASE "+pgx.Identifier{target.DatabaseName}.Sanitize()+" IS '"+target.Marker+"'"); err != nil {
		t.Fatal("native marker setup failed")
	}
	importer, err := NewLegacyMigrationImporter(pool, target)
	if err != nil {
		t.Fatal("guarded importer rejected owned child")
	}
	return &legacyNativeFixture{Context: ctx, Pool: pool, Importer: importer, Target: target}
}

// Prepared actual PostgreSQL test. Root must enable and execute this in the
// explicitly controlled parent. No real source files or auth credentials are
// used; the fixture owns a fresh random child DB and marks only that child.
func TestLegacyImportNativeAtomicOwnerArchive(t *testing.T) {
	f := legacyMigrationNativeFixture(t)
	ctx, pool, importer, target := f.Context, f.Pool, f.Importer, f.Target
	var err error
	source, manifest := legacySyntheticInput(t, nil)
	measure := func() [6]int64 {
		var v [6]int64
		if e := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM accounts),(SELECT count(*) FROM agents),(SELECT count(*) FROM user_profiles),(SELECT count(*) FROM legacy_import_runs),(SELECT count(*) FROM legacy_entity_mappings),(SELECT count(*) FROM legacy_note_archives)`).Scan(&v[0], &v[1], &v[2], &v[3], &v[4], &v[5]); e != nil {
			t.Fatal("owned atomic count unavailable")
		}
		return v
	}
	before := measure()
	if _, err = pool.Exec(ctx, `CREATE FUNCTION birdtie_owned_legacy_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'owned synthetic archive fault'; END $$; CREATE TRIGGER birdtie_owned_legacy_fault BEFORE INSERT ON legacy_note_archives FOR EACH ROW EXECUTE FUNCTION birdtie_owned_legacy_fault()`); err != nil {
		t.Fatal("owned fault injection failed")
	}
	if _, err = importer.Apply(ctx, legacyTestRunID, source, manifest); !errors.Is(err, ErrLegacyMigrationConflict) || measure() != before {
		t.Fatal("archive failure left a partial run/owner/Agent/mapping")
	}
	if _, err = pool.Exec(ctx, `DROP TRIGGER birdtie_owned_legacy_fault ON legacy_note_archives; DROP FUNCTION birdtie_owned_legacy_fault()`); err != nil {
		t.Fatal("owned fault cleanup failed")
	}
	first, err := importer.Apply(ctx, legacyTestRunID, source, manifest)
	if err != nil || first.Status != "COMMITTED" || first.Counts != (LegacyMigrationCounts{Owners: 1, Notes: 2, LiveNotes: 1, Tombstones: 1}) {
		t.Fatal("actual native import failed")
	}
	var account, agent, archive string
	if err = pool.QueryRow(ctx, `SELECT m.owner_account_id::text,a.id::text,n.id::text FROM legacy_entity_mappings m JOIN agents a ON a.principal_account_id=m.owner_account_id AND a.agent_type='personal' JOIN legacy_note_archives n ON n.owner_account_id=m.owner_account_id AND NOT n.deleted WHERE m.run_id=$1 AND m.entity_kind='user'`, legacyTestRunID).Scan(&account, &agent, &archive); err != nil {
		t.Fatal("permanent owner/Agent/archive mapping absent")
	}
	var closed bool
	if err = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM accounts a JOIN agents g ON g.principal_account_id=a.id JOIN user_profiles p ON p.account_id=a.id WHERE a.id=$1 AND a.status='suspended' AND a.handle IS NULL AND g.status='suspended' AND p.visibility='private') AND NOT EXISTS(SELECT 1 FROM account_auth_identities WHERE account_id=$1) AND NOT EXISTS(SELECT 1 FROM sessions WHERE account_id=$1)`, account).Scan(&closed); err != nil || !closed {
		t.Fatal("import activated identity or copied authorization")
	}
	var tombstoneOnly bool
	if err = pool.QueryRow(ctx, `SELECT title='' AND body='' AND source_relationships IS NULL AND recorded_start_date IS NULL AND recorded_end_date IS NULL AND source_visibility='' AND source_moderation_status='' FROM legacy_note_archives WHERE run_id=$1 AND deleted`, legacyTestRunID).Scan(&tombstoneOnly); err != nil || !tombstoneOnly {
		t.Fatal("deleted content was resurrected")
	}
	repeat, err := importer.Apply(ctx, legacyTestRunID, source, manifest)
	if err != nil || repeat.Counts != first.Counts || repeat.CreatedAt != first.CreatedAt {
		t.Fatal("exact original run was not idempotent")
	}
	var same bool
	if err = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM legacy_entity_mappings WHERE run_id=$1 AND entity_kind='user' AND target_id=$2) AND EXISTS(SELECT 1 FROM agents WHERE id=$3 AND principal_account_id=$2) AND EXISTS(SELECT 1 FROM legacy_note_archives WHERE id=$4)`, legacyTestRunID, account, agent, archive).Scan(&same); err != nil || !same {
		t.Fatal("retry replaced permanent IDs")
	}
	changedSource, changedManifest := legacySyntheticInput(t, func(tables map[string]any) {
		tables["notes"].([]any)[0].(map[string]any)["caption"] = "另一个测试正文"
	})
	if _, err = importer.Apply(ctx, legacyTestRunID, changedSource, changedManifest); !errors.Is(err, ErrLegacyMigrationConflict) {
		t.Fatal("changed payload overwrote fixed run")
	}
	if _, err = importer.Apply(ctx, "14600000-0000-4000-8000-000000000002", source, manifest); !errors.Is(err, ErrLegacyMigrationConflict) {
		t.Fatal("new run rebound original source")
	}
	missing, err := importer.InspectRun(ctx, "14600000-0000-4000-8000-000000000003")
	if !errors.Is(err, ErrLegacyMigrationNotFound) || missing.Status != "NOT_FOUND" {
		t.Fatal("missing original run implied STOP/no-effect")
	}
	wrong := target
	wrong.Marker += "b"
	if _, err = NewLegacyMigrationImporter(pool, wrong); !errors.Is(err, ErrLegacyMigrationGuard) {
		t.Fatal("invalid marker accepted")
	}
	wrong = target
	wrong.Marker = "birdtie-owned-legacy-import-2026-10-15:" + strings.Repeat("b", 32)
	foreign, err := NewLegacyMigrationImporter(pool, wrong)
	if err != nil {
		t.Fatal("syntactically valid foreign marker fixture rejected too early")
	}
	if _, err = foreign.InspectRun(ctx, legacyTestRunID); !errors.Is(err, ErrLegacyMigrationGuard) {
		t.Fatal("foreign target marker read original run")
	}
	if _, err = pool.Exec(ctx, `UPDATE legacy_note_archives SET body='changed' WHERE id=$1`, archive); err == nil {
		t.Fatal("archive mutable outside original effect")
	}
	down, err := os.ReadFile("../../migrations/146_legacy_civu_archive.down.sql")
	if err != nil {
		t.Fatal("146 down unavailable")
	}
	if _, err = pool.Exec(ctx, string(down)); err == nil {
		t.Fatal("nonempty migration history dropped")
	}
}
