package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"image"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/legacyimagerecovery"
	"github.com/birdtie/birdtie/apps/api/internal/legacymedia"
	"github.com/birdtie/birdtie/apps/api/internal/media"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Test-only diagnostics: static stage/code/constraint, never query arguments,
// private source text or raw PostgreSQL messages. Not used by any real CLI.
type recoveryDiagnosticDatabase struct {
	legacyMigrationDatabase
	t *testing.T
}

func (d recoveryDiagnosticDatabase) BeginTx(ctx context.Context, o pgx.TxOptions) (pgx.Tx, error) {
	tx, e := d.legacyMigrationDatabase.BeginTx(ctx, o)
	if e != nil {
		return tx, e
	}
	return recoveryDiagnosticTx{Tx: tx, t: d.t}, nil
}

type recoveryDiagnosticTx struct {
	pgx.Tx
	t *testing.T
}

func (tx recoveryDiagnosticTx) Exec(ctx context.Context, q string, a ...any) (pgconn.CommandTag, error) {
	tag, e := tx.Tx.Exec(ctx, q, a...)
	var p *pgconn.PgError
	if errors.As(e, &p) {
		stage := "OTHER"
		for _, v := range []string{"legacy_private_image_recovery_runs", "private_moment_images", "media_assets", "legacy_private_image_recovery_items"} {
			if strings.Contains(q, "INSERT INTO "+v) {
				stage = v
				break
			}
		}
		if strings.Contains(q, "SET CONSTRAINTS") {
			stage = "FINAL_CONSTRAINTS"
		}
		tx.t.Logf("controlled recovery SQL diagnostic: stage=%s code=%s constraint=%s", stage, p.Code, p.ConstraintName)
	}
	return tag, e
}

func legacyRecoveredNativeInput(t *testing.T) (LegacyPrivateMediaInput, []byte) {
	input, raw := legacyMediaNativeInput(t)
	var doc struct {
		Tables map[string][]map[string]any `json:"tables"`
	}
	d := json.NewDecoder(bytes.NewReader(input.Source))
	d.UseNumber()
	if d.Decode(&doc) != nil {
		t.Fatal("synthetic source")
	}
	doc.Tables["media_assets"][1]["content_type"] = ""
	doc.Tables["media_assets"][1]["size_bytes"] = 0
	doc.Tables["note_media"][0]["position"], doc.Tables["note_media"][1]["position"] = 1, 0
	input.Source, input.Manifest = legacySyntheticInput(t, func(tables map[string]any) {
		for _, name := range []string{"media_assets", "note_media"} {
			rows := []any{}
			for _, row := range doc.Tables[name] {
				rows = append(rows, row)
			}
			tables[name] = rows
		}
	})
	var m map[string]any
	if json.Unmarshal(input.Matched, &m) != nil {
		t.Fatal("synthetic matcher")
	}
	m["sourceFileSha256"] = legacymedia.Hash(input.Source)
	var fresh map[string]json.RawMessage
	if json.Unmarshal(input.Source, &fresh) != nil {
		t.Fatal("synthetic fresh header")
	}
	m["source"] = fresh["source"]
	rows := m["selectors"].([]any)
	s := rows[1].(map[string]any)
	s["sourceMIME"], s["sourceBytes"], s["matched"] = "", 0, false
	s["reasons"] = []string{"NOT_SCANNED", "RIGHTS_NOT_REVIEWED", "SOURCE_MIME_UNKNOWN", "SOURCE_SIZE_UNKNOWN"}
	input.Matched, _ = json.Marshal(m)
	var auth map[string]any
	if json.Unmarshal(input.AuthenticatedSeal, &auth) != nil {
		t.Fatal("synthetic seal")
	}
	auth["matcherFileSHA256"] = legacymedia.Hash(input.Matched)
	input.AuthenticatedSeal, _ = json.Marshal(auth)
	input.ExpectedSealSHA256 = legacymedia.Hash(input.AuthenticatedSeal)
	return input, raw
}

func TestLegacyPrivateImageRecoverySyntheticFixture(t *testing.T) {
	input, _ := legacyRecoveredNativeInput(t)
	p, e := legacyimagerecovery.Build(legacyTestRunID, input.Source, input.Manifest, input.Matched, input.AuthenticatedSeal, input.ExpectedSealSHA256)
	if e != nil || p.Counts != (legacyimagerecovery.Counts{Images: 1, JPEG: 1, Notes: 1, Owners: 1}) {
		t.Fatal("synthetic recovery fixture", e)
	}
	old, e := legacymedia.Build(legacyTestRunID, input.Source, input.Manifest, input.Matched, input.AuthenticatedSeal, input.ExpectedSealSHA256)
	if e != nil || old.Counts.Images != 1 {
		t.Fatal("synthetic original157 fixture", e)
	}
}

// One native chain covers the new provenance alongside the untouched157 fact.
// Full real HEIC codec verification is a separate protected --verify-blobs run.
func TestLegacyPrivateImageRecoveryNativeIndependentFactsAndOwnerRead(t *testing.T) {
	f := legacyMigrationNativeFixture(t)
	input, raw := legacyRecoveredNativeInput(t)
	plan, e := legacyimagerecovery.Build(legacyTestRunID, input.Source, input.Manifest, input.Matched, input.AuthenticatedSeal, input.ExpectedSealSHA256)
	if e != nil || plan.Counts != (legacyimagerecovery.Counts{Images: 1, JPEG: 1, Notes: 1, Owners: 1}) {
		t.Fatal("recovery fixture", e)
	}
	if _, e = f.Importer.Apply(f.Context, legacyTestRunID, input.Source, input.Manifest); e != nil {
		t.Fatal(e)
	}
	if _, e = f.Importer.ApplyPersonalArchives(f.Context, legacyTestRunID, input.Source, input.Manifest); e != nil {
		t.Fatal(e)
	}
	if _, e = f.Importer.ApplyNativeAdaptation(f.Context, legacyTestRunID, input.Source, input.Manifest); e != nil {
		t.Fatal(e)
	}
	original, e := f.Importer.ApplyLegacyPrivateMedia(f.Context, legacyTestRunID, input, func(context.Context, legacymedia.Record) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(raw)), nil
	})
	if e != nil || original.Counts.Images != 1 {
		t.Fatal("original157 fact", e)
	}
	normalize := &legacyimagerecovery.Normalizer{}
	bad := func(context.Context, legacyimagerecovery.Record) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewBufferString("wrong source")), nil
	}
	if _, e = f.Importer.ApplyLegacyPrivateImageRecovery(f.Context, legacyTestRunID, input, bad, normalize); !errors.Is(e, ErrLegacyPrivateImageRecoveryDerivative) {
		t.Fatal("wrong bytes accepted", e)
	}
	var clean bool
	if e = f.Pool.QueryRow(f.Context, `SELECT NOT EXISTS(SELECT 1 FROM legacy_private_image_recovery_runs)AND(SELECT count(*)FROM private_moment_images)=1`).Scan(&clean); e != nil || !clean {
		t.Fatal("partial recovery", e)
	}
	f.Importer.database = recoveryDiagnosticDatabase{legacyMigrationDatabase: f.Importer.database, t: t}
	first, e := f.Importer.ApplyLegacyPrivateImageRecovery(f.Context, legacyTestRunID, input, func(context.Context, legacyimagerecovery.Record) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(raw)), nil
	}, normalize)
	if e != nil || first.Status != "COMMITTED" || first.Counts != plan.Counts {
		t.Fatal("recovery commit", e)
	}
	var owner, moment string
	if e = f.Pool.QueryRow(f.Context, `SELECT owner_account_id::text,moment_id::text FROM legacy_private_image_recovery_items`).Scan(&owner, &moment); e != nil {
		t.Fatal(e)
	}
	if e = f.Pool.QueryRow(f.Context, `SELECT NOT EXISTS(SELECT 1 FROM sessions)AND(SELECT status='suspended'FROM accounts WHERE id=$1)AND(SELECT status='suspended'FROM agents WHERE principal_account_id=$1 AND agent_type='personal')`, owner).Scan(&clean); e != nil || !clean {
		t.Fatal("recovery activated identity", e)
	}
	digest := sha256.Sum256([]byte("synthetic-recovery-fresh-owner-session"))
	if _, e = f.Pool.Exec(f.Context, `UPDATE accounts SET status='active'WHERE id=$1`, owner); e != nil {
		t.Fatal(e)
	}
	if _, e = f.Pool.Exec(f.Context, `INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at)VALUES($1,$2,'synthetic_test',clock_timestamp()+interval '1 hour',clock_timestamp()+interval '1 hour')`, owner, digest[:]); e != nil {
		t.Fatal(e)
	}
	store := New(f.Pool, true)
	actor := identity.Actor{ID: owner, AccountType: "person"}
	id := plan.Records[0].ImageID
	_, e = store.ReadHumanPrivateImages(f.Context, digest, actor, moment, "", false, func(items []media.PrivateImageReceipt, _ []byte) ([]byte, error) {
		if len(items) != 2 || items[0].ID != id {
			return nil, errors.New("original source order lost across157 and161")
		}
		return []byte("ORDERED"), nil
	})
	if e != nil {
		t.Fatal("combined native list", e)
	}
	_, e = store.ReadHumanPrivateImages(f.Context, digest, actor, moment, id, true, func(items []media.PrivateImageReceipt, body []byte) ([]byte, error) {
		if len(items) != 1 || items[0].Origin != "LEGACY_PRIVATE_IMPORT" || bytes.Contains(body, []byte("SYNTHETIC_RAW_METADATA")) {
			return nil, errors.New("unsafe private recovery")
		}
		cfg, _, e := image.DecodeConfig(bytes.NewReader(body))
		if e != nil || cfg.Width != 3 || cfg.Height != 2 {
			return nil, errors.New("frame changed")
		}
		return []byte("READABLE"), nil
	})
	if e != nil {
		t.Fatal("owner read", e)
	}
	_, e = store.ReadHumanPrivateImages(f.Context, digest, identity.Actor{ID: "16100000-0000-4000-8000-000000000099", AccountType: "person"}, moment, id, true, func([]media.PrivateImageReceipt, []byte) ([]byte, error) {
		t.Fatal("foreign owner saw bytes")
		return nil, nil
	})
	if e == nil {
		t.Fatal("foreign owner accepted")
	}
	if e = store.DeleteHumanPrivateImage(f.Context, digest, actor, moment, id, 1); e != nil {
		t.Fatal("delete", e)
	}
	repeated, e := f.Importer.ApplyLegacyPrivateImageRecovery(f.Context, legacyTestRunID, input, func(context.Context, legacyimagerecovery.Record) (io.ReadCloser, error) {
		t.Fatal("replay reopened source")
		return nil, nil
	}, normalize)
	if e != nil || !reflect.DeepEqual(first, repeated) {
		t.Fatal("replay changed immutable recovery fact", e)
	}
	inspected, e := f.Importer.InspectLegacyPrivateImageRecovery(f.Context, legacyTestRunID)
	if e != nil || !reflect.DeepEqual(first, inspected) {
		t.Fatal("inspect changed recovery fact", e)
	}
	preserved, e := f.Importer.InspectLegacyPrivateMedia(f.Context, legacyTestRunID)
	if e != nil || !reflect.DeepEqual(original, preserved) {
		t.Fatal("original157 receipt changed", e)
	}
	if e = f.Pool.QueryRow(f.Context, `SELECT status='deleted'AND derivative IS NULL FROM private_moment_images WHERE id=$1`, id).Scan(&clean); e != nil || !clean {
		t.Fatal("deleted recovery resurrected", e)
	}
	if _, e = f.Pool.Exec(f.Context, `UPDATE legacy_private_image_recovery_items SET source_position=source_position+1`); e == nil {
		t.Fatal("recovery provenance mutable")
	}
}
