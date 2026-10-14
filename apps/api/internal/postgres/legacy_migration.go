package postgres

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrLegacyMigrationGuard       = errors.New("legacy_import_target_rejected")
	ErrLegacyMigrationInvalid     = errors.New("legacy_import_source_invalid")
	ErrLegacyMigrationConflict    = errors.New("legacy_import_conflict")
	ErrLegacyMigrationUnavailable = errors.New("legacy_import_unavailable")
	ErrLegacyMigrationUnknown     = errors.New("legacy_import_commit_unknown_inspect_original_run")
	ErrLegacyMigrationNotFound    = errors.New("legacy_import_run_not_found")
)

var legacyNativeName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
var legacyNativeOwner = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)
var legacyNativeMarker = regexp.MustCompile(`^birdtie-owned-legacy-import-2026-10-15:[0-9a-f]{32}$`)

const LegacyMigrationMaxInputBytes = 64 * 1024 * 1024

// The source OID is the forbidden database in this PostgreSQL cluster. Sharing
// a cluster is allowed; connecting to that source database is never allowed.
type LegacyMigrationTargetIdentity struct {
	DatabaseName               string
	DatabaseOID                int64
	Owner                      string
	Marker                     string
	ForbiddenSourceDatabaseOID int64
}

func ValidateLegacyMigrationTarget(v LegacyMigrationTargetIdentity) error {
	if !legacyNativeName.MatchString(v.DatabaseName) || strings.HasPrefix(v.DatabaseName, "civu") ||
		v.DatabaseName == "postgres" || v.DatabaseName == "template0" || v.DatabaseName == "template1" ||
		v.DatabaseOID < 1 || v.DatabaseOID > 4294967295 || v.ForbiddenSourceDatabaseOID < 1 ||
		v.ForbiddenSourceDatabaseOID > 4294967295 || v.DatabaseOID == v.ForbiddenSourceDatabaseOID ||
		!legacyNativeOwner.MatchString(v.Owner) || !legacyNativeMarker.MatchString(v.Marker) {
		return ErrLegacyMigrationGuard
	}
	return nil
}

type LegacyMigrationCounts struct {
	Owners     int64 `json:"owners"`
	Notes      int64 `json:"notes"`
	LiveNotes  int64 `json:"liveNotes"`
	Tombstones int64 `json:"tombstones"`
}

// Sensitive source selectors, projected text and digest values are deliberately
// absent from the public summary. Digest facts remain internal for exact-run
// inspection, never JSON/stdout output.
type LegacyMigrationResult struct {
	RunID              string                `json:"runId"`
	Status             string                `json:"status"`
	Counts             LegacyMigrationCounts `json:"counts"`
	CreatedAt          time.Time             `json:"createdAt"`
	SourceFileSHA256   string                `json:"-"`
	ManifestFileSHA256 string                `json:"-"`
}

type LegacyMigrationImporter struct {
	database legacyMigrationDatabase
	expected LegacyMigrationTargetIdentity
}

type legacyMigrationDatabase interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

func NewLegacyMigrationImporter(pool *pgxpool.Pool, expected LegacyMigrationTargetIdentity) (*LegacyMigrationImporter, error) {
	if pool == nil || ValidateLegacyMigrationTarget(expected) != nil ||
		pool.Config().ConnConfig.Database != expected.DatabaseName || pool.Config().ConnConfig.User != expected.Owner {
		return nil, ErrLegacyMigrationGuard
	}
	return &LegacyMigrationImporter{database: pool, expected: expected}, nil
}

const legacyMigrationTargetSQL = `SELECT current_database(),d.oid::bigint,pg_catalog.pg_get_userbyid(d.datdba),current_user,
 coalesce(pg_catalog.shobj_description(d.oid,'pg_database'),''),
 EXISTS(SELECT 1 FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace
        WHERE n.nspname='public' AND c.relname IN('users','civu_schema_migrations')),
 NOT EXISTS(SELECT 1 FROM (VALUES('accounts','account_type'),('agents','principal_account_id'),
 ('user_profiles','visibility'),('sessions','token_sha256'),('public_moment_photos','rights'),
 ('public_moment_photo_operations','payload_sha256'),('legacy_import_runs','manifest_file_sha256'),
 ('legacy_entity_mappings','claim_status'),('legacy_note_archives','source_relationships')) required(relation,column_name)
 LEFT JOIN pg_catalog.pg_namespace n ON n.nspname='public'
 LEFT JOIN pg_catalog.pg_class c ON c.relnamespace=n.oid AND c.relname=required.relation AND c.relkind IN('r','p')
 LEFT JOIN pg_catalog.pg_attribute a ON a.attrelid=c.oid AND a.attname=required.column_name AND a.attnum>0 AND NOT a.attisdropped
 WHERE a.attname IS NULL)
 FROM pg_catalog.pg_database d WHERE d.datname=current_database()`

func legacyMigrationCheckTarget(ctx context.Context, tx pgx.Tx, expected LegacyMigrationTargetIdentity) error {
	if ValidateLegacyMigrationTarget(expected) != nil || tx == nil || ctx == nil || ctx.Err() != nil {
		return ErrLegacyMigrationGuard
	}
	var name, owner, role, marker string
	var oid int64
	var source, native bool
	if err := tx.QueryRow(ctx, legacyMigrationTargetSQL).Scan(&name, &oid, &owner, &role, &marker, &source, &native); err != nil ||
		name != expected.DatabaseName || oid != expected.DatabaseOID || owner != expected.Owner || role != expected.Owner || marker != expected.Marker ||
		oid == expected.ForbiddenSourceDatabaseOID || source || !native || ctx.Err() != nil {
		return ErrLegacyMigrationGuard
	}
	return nil
}

func legacyMigrationFailure(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && (strings.HasPrefix(pgErr.Code, "23") || pgErr.Code == "P0001") {
		return ErrLegacyMigrationConflict
	}
	return ErrLegacyMigrationUnavailable
}

func legacyMigrationUUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", ErrLegacyMigrationUnavailable
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	hexID := hex.EncodeToString(value[:])
	return hexID[:8] + "-" + hexID[8:12] + "-" + hexID[12:16] + "-" + hexID[16:20] + "-" + hexID[20:], nil
}

func legacyMigrationRollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}

func legacyMigrationJSON(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, ErrLegacyMigrationInvalid
	}
	return raw, nil
}

func legacyMigrationSameJSON(a, b []byte) bool {
	decode := func(raw []byte) (any, error) {
		d := json.NewDecoder(bytes.NewReader(raw))
		d.UseNumber()
		var v any
		err := d.Decode(&v)
		return v, err
	}
	x, ex := decode(a)
	y, ey := decode(b)
	return ex == nil && ey == nil && reflect.DeepEqual(x, y)
}

type legacyRunFact struct {
	run      legacymigration.Run
	deferred map[string]int
	result   LegacyMigrationResult
}

func legacyMigrationReadRun(ctx context.Context, tx pgx.Tx, runID string) (legacyRunFact, error) {
	var fact legacyRunFact
	var header, counts, deferred []byte
	err := tx.QueryRow(ctx, `SELECT run_id::text,source_system,source_protocol,backup_cipher_sha256,schema_sha256,
 snapshot_sha256,source_file_sha256,manifest_file_sha256,source_header,source_counts,deferred_counts,created_at
 FROM public.legacy_import_runs WHERE run_id=$1`, runID).Scan(&fact.run.RunID, &fact.run.SourceSystem,
		&fact.run.SourceProtocol, &fact.run.BackupCipherSHA256, &fact.run.SchemaSHA256, &fact.run.SnapshotSHA256,
		&fact.run.SourceFileSHA256, &fact.run.ManifestFileSHA256, &header, &counts, &deferred, &fact.result.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return fact, ErrLegacyMigrationNotFound
	}
	if err != nil || json.Unmarshal(counts, &fact.run.Counts) != nil || json.Unmarshal(deferred, &fact.deferred) != nil ||
		!json.Valid(header) || fact.run.SourceSystem != legacymigration.SourceSystem || fact.run.SourceProtocol != legacymigration.SourceProtocol {
		return fact, ErrLegacyMigrationUnavailable
	}
	fact.run.Header = append([]byte(nil), header...)
	fact.result.RunID, fact.result.Status = fact.run.RunID, "COMMITTED"
	fact.result.SourceFileSHA256, fact.result.ManifestFileSHA256 = fact.run.SourceFileSHA256, fact.run.ManifestFileSHA256
	err = tx.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM public.legacy_entity_mappings WHERE run_id=$1 AND entity_kind='user'),
 (SELECT count(*) FROM public.legacy_entity_mappings WHERE run_id=$1 AND entity_kind='note'),
 (SELECT count(*) FROM public.legacy_note_archives WHERE run_id=$1 AND NOT deleted),
 (SELECT count(*) FROM public.legacy_note_archives WHERE run_id=$1 AND deleted)`, runID).Scan(&fact.result.Counts.Owners,
		&fact.result.Counts.Notes, &fact.result.Counts.LiveNotes, &fact.result.Counts.Tombstones)
	if err != nil || fact.result.Counts.Owners != int64(fact.run.Counts["users"]) ||
		fact.result.Counts.Notes != int64(fact.run.Counts["notes"]) ||
		fact.result.Counts.Notes != fact.result.Counts.LiveNotes+fact.result.Counts.Tombstones {
		return fact, ErrLegacyMigrationUnavailable
	}
	return fact, nil
}

func legacyMigrationSameRun(fact legacyRunFact, plan legacymigration.Plan) bool {
	a, b := fact.run, plan.Run
	return a.RunID == b.RunID && a.SourceSystem == b.SourceSystem && a.SourceProtocol == b.SourceProtocol &&
		a.BackupCipherSHA256 == b.BackupCipherSHA256 && a.SchemaSHA256 == b.SchemaSHA256 && a.SnapshotSHA256 == b.SnapshotSHA256 &&
		a.SourceFileSHA256 == b.SourceFileSHA256 && a.ManifestFileSHA256 == b.ManifestFileSHA256 &&
		legacyMigrationSameJSON(a.Header, b.Header) && reflect.DeepEqual(a.Counts, b.Counts) && reflect.DeepEqual(fact.deferred, plan.Deferred)
}

// Apply reparses the original bounded source/manifest instead of accepting a
// caller-mutated Plan. Each effect belongs to one immutable run. There is no
// automatic retry, new run ID, identity activation, or source DB connection.
func (s *LegacyMigrationImporter) Apply(ctx context.Context, runID string, sourceRaw, manifestRaw []byte) (LegacyMigrationResult, error) {
	unknown := LegacyMigrationResult{RunID: runID, Status: "UNKNOWN"}
	if s == nil || s.database == nil || ctx == nil || ctx.Err() != nil || ValidateLegacyMigrationTarget(s.expected) != nil {
		return unknown, ErrLegacyMigrationGuard
	}
	if len(sourceRaw) < 1 || len(sourceRaw) > LegacyMigrationMaxInputBytes || len(manifestRaw) < 1 || len(manifestRaw) > LegacyMigrationMaxInputBytes {
		return unknown, ErrLegacyMigrationInvalid
	}
	plan, err := legacymigration.BuildPlan(runID, sourceRaw, manifestRaw)
	if err != nil {
		return unknown, ErrLegacyMigrationInvalid
	}
	tx, err := s.database.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return unknown, ErrLegacyMigrationUnavailable
	}
	defer legacyMigrationRollback(tx)
	if err = legacyMigrationCheckTarget(ctx, tx, s.expected); err != nil {
		return unknown, err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,146))`, plan.Run.SourceSystem); err != nil {
		return unknown, legacyMigrationFailure(err)
	}
	previous, err := legacyMigrationReadRun(ctx, tx, runID)
	if err == nil {
		if !legacyMigrationSameRun(previous, plan) {
			return unknown, ErrLegacyMigrationConflict
		}
		return previous.result, nil
	}
	if !errors.Is(err, ErrLegacyMigrationNotFound) {
		return unknown, err
	}
	var existing bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM public.legacy_entity_mappings WHERE source_system=$1)`, plan.Run.SourceSystem).Scan(&existing); err != nil {
		return unknown, legacyMigrationFailure(err)
	}
	if existing {
		return unknown, ErrLegacyMigrationConflict
	}
	counts, err := legacyMigrationJSON(plan.Run.Counts)
	if err != nil {
		return unknown, err
	}
	deferred, err := legacyMigrationJSON(plan.Deferred)
	if err != nil {
		return unknown, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO public.legacy_import_runs(run_id,source_system,source_protocol,backup_cipher_sha256,
 schema_sha256,snapshot_sha256,source_file_sha256,manifest_file_sha256,source_header,source_counts,deferred_counts)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, runID, plan.Run.SourceSystem, plan.Run.SourceProtocol,
		plan.Run.BackupCipherSHA256, plan.Run.SchemaSHA256, plan.Run.SnapshotSHA256, plan.Run.SourceFileSHA256,
		plan.Run.ManifestFileSHA256, plan.Run.Header, counts, deferred)
	if err != nil {
		return unknown, legacyMigrationFailure(err)
	}
	owners := map[string]string{}
	for _, owner := range plan.Owners {
		accountID, err := legacyMigrationUUID()
		if err != nil {
			return unknown, err
		}
		agentID, err := legacyMigrationUUID()
		if err != nil {
			return unknown, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO public.accounts(id,account_type,status) VALUES($1,'person','suspended')`, accountID); err != nil {
			return unknown, legacyMigrationFailure(err)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO public.user_profiles(account_id,display_name,bio,visibility) VALUES($1,$2,$3,'private')`, accountID, owner.DisplayName, owner.Bio); err != nil {
			return unknown, legacyMigrationFailure(err)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO public.agents(id,agent_type,principal_account_id,status) VALUES($1,'personal',$2,'suspended')`, agentID, accountID); err != nil {
			return unknown, legacyMigrationFailure(err)
		}
		_, err = tx.Exec(ctx, `INSERT INTO public.legacy_entity_mappings(source_system,entity_kind,source_id,target_id,
 owner_account_id,run_id,source_version,source_sha256,source_status,source_data_origin,claim_status)
 VALUES($1,'user',$2,$3,$3,$4,$5,$6,$7,$8,'UNCLAIMED')`, plan.Run.SourceSystem, owner.SourceID, accountID,
			runID, owner.SourceVersion, owner.SourceSHA256, owner.SourceStatus, owner.DataOrigin)
		if err != nil {
			return unknown, legacyMigrationFailure(err)
		}
		owners[owner.SourceID] = accountID
	}
	for _, note := range plan.Notes {
		ownerID, ok := owners[note.OwnerSourceID]
		if !ok {
			return unknown, ErrLegacyMigrationConflict
		}
		archiveID, err := legacyMigrationUUID()
		if err != nil {
			return unknown, err
		}
		status := legacymigration.NoteLiveStatus
		if note.Deleted {
			status = legacymigration.NoteDeletedStatus
		}
		_, err = tx.Exec(ctx, `INSERT INTO public.legacy_entity_mappings(source_system,entity_kind,source_id,target_id,
 owner_account_id,run_id,source_version,source_sha256,source_status,source_data_origin,claim_status)
 VALUES($1,'note',$2,$3,$4,$5,$6,$7,$8,$9,'UNCLAIMED')`, plan.Run.SourceSystem, note.SourceID, archiveID, ownerID,
			runID, note.SourceVersion, note.SourceSHA256, status, legacymigration.NoteDataOrigin)
		if err != nil {
			return unknown, legacyMigrationFailure(err)
		}
		var relationships any
		if !note.Deleted && len(note.Payload) != 0 && string(note.Payload) != "null" {
			relationships = note.Payload
		}
		_, err = tx.Exec(ctx, `INSERT INTO public.legacy_note_archives(id,source_system,source_note_id,owner_account_id,
 run_id,source_version,source_sha256,deleted,title,body,recorded_start_date,recorded_end_date,
 source_visibility,source_moderation_status,source_relationships)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`, archiveID, plan.Run.SourceSystem, note.SourceID,
			ownerID, runID, note.SourceVersion, note.SourceSHA256, note.Deleted, note.Title, note.Body, note.RecordedStartDate,
			note.RecordedEndDate, note.SourceVisibility, note.SourceModerationStatus, relationships)
		if err != nil {
			return unknown, legacyMigrationFailure(err)
		}
	}
	current, err := legacyMigrationReadRun(ctx, tx, runID)
	if err != nil || !legacyMigrationSameRun(current, plan) {
		return unknown, ErrLegacyMigrationConflict
	}
	if err = tx.Commit(ctx); err != nil {
		// The connection may have lost a successful COMMIT response. Never
		// infer rollback, retry this write, or allocate another run here.
		return unknown, ErrLegacyMigrationUnknown
	}
	return current.result, nil
}

func (s *LegacyMigrationImporter) InspectRun(ctx context.Context, runID string) (LegacyMigrationResult, error) {
	result := LegacyMigrationResult{RunID: runID, Status: "UNKNOWN"}
	if s == nil || s.database == nil || ctx == nil || ctx.Err() != nil || ValidateLegacyMigrationTarget(s.expected) != nil || !legacymigration.ValidRunID(runID) {
		return result, ErrLegacyMigrationGuard
	}
	tx, err := s.database.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, ErrLegacyMigrationUnavailable
	}
	defer legacyMigrationRollback(tx)
	if err = legacyMigrationCheckTarget(ctx, tx, s.expected); err != nil {
		return result, err
	}
	fact, err := legacyMigrationReadRun(ctx, tx, runID)
	if err != nil {
		if errors.Is(err, ErrLegacyMigrationNotFound) {
			result.Status = "NOT_FOUND"
		}
		return result, err
	}
	return fact.result, nil
}
