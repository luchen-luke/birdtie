package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/legacymedia"
	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
	"github.com/birdtie/birdtie/apps/api/internal/media"
)

func legacyMediaNativeInput(t *testing.T) (LegacyPrivateMediaInput, []byte) {
	t.Helper()
	var b bytes.Buffer
	if e := jpeg.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 3, 2)), nil); e != nil {
		t.Fatal(e)
	}
	raw := append(b.Bytes(), []byte("SYNTHETIC_RAW_METADATA_MUST_NOT_SURVIVE")...)
	owner := "14600000-0000-4000-8000-000000000010"
	note := "14600000-0000-4000-8000-000000000020"
	source, manifest := legacySyntheticInput(t, func(tables map[string]any) {
		row := func(table string) map[string]any {
			v := map[string]any{}
			for _, k := range legacymigration.ColumnNames(table) {
				v[k] = nil
			}
			return v
		}
		assets := []any{}
		links := []any{}
		for i, id := range []string{"15700000-0000-4000-8000-000000000011", "15700000-0000-4000-8000-000000000012"} {
			a := row("media_assets")
			a["id"], a["owner_id"], a["status"], a["kind"], a["content_type"], a["storage_key"], a["size_bytes"] = id, owner, "ready", "image", "image/jpeg", "synthetic-"+id, len(raw)
			assets = append(assets, a)
			links = append(links, map[string]any{"note_id": note, "media_id": id, "position": i})
		}
		tables["media_assets"], tables["note_media"] = assets, links
	})
	var doc map[string]json.RawMessage
	if e := json.Unmarshal(source, &doc); e != nil {
		t.Fatal(e)
	}
	selectors := []any{}
	for _, id := range []string{"15700000-0000-4000-8000-000000000011", "15700000-0000-4000-8000-000000000012"} {
		selectors = append(selectors, map[string]any{"sourceMediaId": id, "sourceOwnerId": owner, "role": "original", "sourceStatus": "ready", "sourceStorageKey": "synthetic-" + id, "archiveMember": "private/synthetic-" + id, "sourceMIME": "image/jpeg", "observedMIME": "image/jpeg", "sourceBytes": len(raw), "bytes": len(raw), "sha256": legacymedia.Hash(raw), "matched": true, "reasons": []string{"NOT_SCANNED", "RIGHTS_NOT_REVIEWED"}})
	}
	plain := strings.Repeat("a", 64)
	matched, _ := json.Marshal(map[string]any{"schemaVersion": "birdtie-civu-media-match-v1", "source": doc["source"], "sourceFileSha256": legacymedia.Hash(source), "archivePlaintextSHA256": plain, "archiveBytes": len(raw) * 2, "selectors": selectors})
	seal, _ := json.Marshal(map[string]any{"format": "birdtie-civu-media-authenticated-match-v1", "aesGcmAuthenticated": true, "ciphertextSHA256": legacymedia.MediaCipherSHA256, "plaintextSHA256": plain, "matcherFileSHA256": legacymedia.Hash(matched), "readerExit": 0, "extractedFiles": 0, "databaseWrites": 0, "sourceWrites": 0, "publicationRights": "NOT_AUTHORIZED", "scanner": "NOT_RUN", "exifCleaning": "NOT_RUN"})
	return LegacyPrivateMediaInput{Source: source, Manifest: manifest, Matched: matched, AuthenticatedSeal: seal, ExpectedSealSHA256: legacymedia.Hash(seal)}, raw
}

func TestLegacyPrivateMediaNativeOriginalOwnerReadableAndNoResurrection(t *testing.T) {
	f := legacyMigrationNativeFixture(t)
	input, raw := legacyMediaNativeInput(t)
	p, e := legacymedia.Build(legacyTestRunID, input.Source, input.Manifest, input.Matched, input.AuthenticatedSeal, input.ExpectedSealSHA256)
	if e != nil || p.Counts != (legacymedia.Counts{Images: 2, Notes: 1, Owners: 1}) {
		t.Fatal("synthetic authenticated plan", e)
	}
	reader := func(context.Context, legacymedia.Record) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(raw)), nil
	}
	if _, e = f.Importer.ApplyLegacyPrivateMedia(f.Context, legacyTestRunID, input, reader); !errors.Is(e, ErrLegacyMigrationNotFound) {
		t.Fatal("missing156 accepted", e)
	}
	if _, e = f.Importer.Apply(f.Context, legacyTestRunID, input.Source, input.Manifest); e != nil {
		t.Fatal("preservation", e)
	}
	if _, e = f.Importer.ApplyPersonalArchives(f.Context, legacyTestRunID, input.Source, input.Manifest); e != nil {
		t.Fatal("personal preservation", e)
	}
	if _, e = f.Importer.ApplyNativeAdaptation(f.Context, legacyTestRunID, input.Source, input.Manifest); e != nil {
		t.Fatal("native drafts", e)
	}
	calls := 0
	bad := func(context.Context, legacymedia.Record) (io.ReadCloser, error) {
		calls++
		if calls == 2 {
			return io.NopCloser(strings.NewReader("wrong original")), nil
		}
		return io.NopCloser(bytes.NewReader(raw)), nil
	}
	if _, e = f.Importer.ApplyLegacyPrivateMedia(f.Context, legacyTestRunID, input, bad); !errors.Is(e, ErrLegacyPrivateMediaDerivative) {
		t.Fatal("bad second input accepted", e)
	}
	var clean bool
	if e = f.Pool.QueryRow(f.Context, `SELECT NOT EXISTS(SELECT 1 FROM legacy_private_media_import_runs) AND NOT EXISTS(SELECT 1 FROM legacy_private_media_import_items) AND NOT EXISTS(SELECT 1 FROM private_moment_images WHERE session_id IS NULL) AND NOT EXISTS(SELECT 1 FROM media_assets WHERE storage_key LIKE 'legacy-private-moment/%')`).Scan(&clean); e != nil || !clean {
		t.Fatal("partial private bytes survived rollback", e)
	}
	first, e := f.Importer.ApplyLegacyPrivateMedia(f.Context, legacyTestRunID, input, reader)
	if e != nil || first.Status != "COMMITTED" || first.Counts != p.Counts {
		t.Fatal("private import", e)
	}
	var owner, moment string
	if e = f.Pool.QueryRow(f.Context, `SELECT owner_account_id::text,moment_id::text FROM legacy_private_media_import_items ORDER BY source_position LIMIT 1`).Scan(&owner, &moment); e != nil {
		t.Fatal(e)
	}
	if e = f.Pool.QueryRow(f.Context, `SELECT (SELECT count(*) FROM sessions)=0 AND (SELECT bool_and(session_id IS NULL AND status='ready_private' AND derivative IS NOT NULL) FROM private_moment_images) AND (SELECT bool_and(status='suspended') FROM accounts WHERE id=$1)`, owner).Scan(&clean); e != nil || !clean {
		t.Fatal("import fabricated session or activation", e)
	}
	// This explicit synthetic test setup is a new ordinary human Session; it is
	// not part of the importer and is required by the real existing read path.
	digest := sha256.Sum256([]byte("synthetic-new-human-read-session"))
	if _, e = f.Pool.Exec(f.Context, `UPDATE accounts SET status='active' WHERE id=$1`, owner); e != nil {
		t.Fatal(e)
	}
	if _, e = f.Pool.Exec(f.Context, `INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at)VALUES($1,$2,'synthetic_test',clock_timestamp()+interval '1 hour',clock_timestamp()+interval '1 hour')`, owner, digest[:]); e != nil {
		t.Fatal(e)
	}
	store := New(f.Pool, true)
	actor := identity.Actor{ID: owner, AccountType: "person"}
	_, e = store.ReadHumanPrivateImages(f.Context, digest, actor, moment, p.Records[0].ImageID, true, func(receipts []media.PrivateImageReceipt, body []byte) ([]byte, error) {
		if len(receipts) != 1 || receipts[0].Origin != "LEGACY_PRIVATE_IMPORT" || len(body) == 0 || bytes.Contains(body, []byte("SYNTHETIC_RAW_METADATA")) {
			return nil, errors.New("unsafe derivative")
		}
		cfg, _, err := image.DecodeConfig(bytes.NewReader(body))
		if err != nil || cfg.Width != 3 || cfg.Height != 2 {
			return nil, errors.New("unreadable derivative")
		}
		return []byte("READABLE"), nil
	})
	if e != nil {
		t.Fatal("original owner's fresh session could not view imported pixels", e)
	}
	if e = store.DeleteHumanPrivateImage(f.Context, digest, actor, moment, p.Records[0].ImageID, 1); e != nil {
		t.Fatal("ordinary owner delete", e)
	}
	for _, retry := range []bool{false, true} {
		var next LegacyPrivateMediaResult
		if retry {
			next, e = f.Importer.ApplyLegacyPrivateMedia(f.Context, legacyTestRunID, input, func(context.Context, legacymedia.Record) (io.ReadCloser, error) {
				t.Fatal("replay reopened original blob")
				return nil, nil
			})
		} else {
			next, e = f.Importer.InspectLegacyPrivateMedia(f.Context, legacyTestRunID)
		}
		if e != nil || !reflect.DeepEqual(first, next) {
			t.Fatal("original receipt changed", e)
		}
	}
	if e = f.Pool.QueryRow(f.Context, `SELECT status='deleted' AND derivative IS NULL FROM private_moment_images WHERE id=$1`, p.Records[0].ImageID).Scan(&clean); e != nil || !clean {
		t.Fatal("replay resurrected deleted image", e)
	}
}
