package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/legacymedia"
	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
	"github.com/birdtie/birdtie/apps/api/internal/legacyvideo"
	"github.com/birdtie/birdtie/apps/api/internal/media"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func legacyVideoNativeInput(t *testing.T, raw []byte) (LegacyPrivateVideoInput, []byte) {
	t.Helper()
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
		for i, id := range []string{"15900000-0000-4000-8000-000000000011", "15900000-0000-4000-8000-000000000012"} {
			a := row("media_assets")
			a["id"], a["owner_id"], a["status"], a["kind"], a["content_type"], a["storage_key"], a["size_bytes"] = id, owner, "ready", "video", "video/mp4", "synthetic-"+id, len(raw)
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
	for _, id := range []string{"15900000-0000-4000-8000-000000000011", "15900000-0000-4000-8000-000000000012"} {
		selectors = append(selectors, map[string]any{"sourceMediaId": id, "sourceOwnerId": owner, "role": "original", "sourceStatus": "ready", "sourceStorageKey": "synthetic-" + id, "archiveMember": "private/synthetic-" + id, "sourceMIME": "video/mp4", "observedMIME": "video/mp4", "sourceBytes": len(raw), "bytes": len(raw), "sha256": legacyvideo.Hash(raw), "matched": true, "reasons": []string{"NOT_SCANNED", "RIGHTS_NOT_REVIEWED"}})
	}
	plain := strings.Repeat("a", 64)
	matched, _ := json.Marshal(map[string]any{"schemaVersion": "birdtie-civu-media-match-v1", "source": doc["source"], "sourceFileSha256": legacyvideo.Hash(source), "archivePlaintextSHA256": plain, "archiveBytes": len(raw) * 2, "selectors": selectors})
	seal, _ := json.Marshal(map[string]any{"format": "birdtie-civu-media-authenticated-match-v1", "aesGcmAuthenticated": true, "ciphertextSHA256": legacymedia.MediaCipherSHA256, "plaintextSHA256": plain, "matcherFileSHA256": legacyvideo.Hash(matched), "readerExit": 0, "extractedFiles": 0, "databaseWrites": 0, "sourceWrites": 0, "publicationRights": "NOT_AUTHORIZED", "scanner": "NOT_RUN", "exifCleaning": "NOT_RUN"})
	return LegacyPrivateVideoInput{Source: source, Manifest: manifest, Matched: matched, AuthenticatedSeal: seal, ExpectedSealSHA256: legacyvideo.Hash(seal)}, raw
}

func TestLegacyPrivateVideoNativeCompleteOwnerReadAtomicRetryAndRetirement(t *testing.T) {
	f := legacyMigrationNativeFixture(t)
	var beforeSessions, beforeActive, beforeMaterials int64
	if e := f.Pool.QueryRow(f.Context, `SELECT (SELECT count(*) FROM sessions),(SELECT count(*) FROM accounts WHERE status='active'),(SELECT count(*) FROM private_video_materials)`).Scan(&beforeSessions, &beforeActive, &beforeMaterials); e != nil {
		t.Fatal("synthetic baseline unavailable", e)
	}
	executable := func(env, name string) string {
		if p := os.Getenv(env); p != "" {
			return p
		}
		p, _ := exec.LookPath(name)
		return p
	}
	codec := &legacyvideo.Normalizer{FFmpegPath: executable("BIRDTIE_PUBLIC_VIDEO_FFMPEG", "ffmpeg"), FFprobePath: executable("BIRDTIE_PUBLIC_VIDEO_FFPROBE", "ffprobe"), PrlimitPath: executable("BIRDTIE_PUBLIC_VIDEO_PRLIMIT", "prlimit"), TempRoot: t.TempDir()}
	if codec.Validate() != nil {
		t.Skip("NOT_RUN: bounded real video codec unavailable")
	}
	file := filepath.Join(t.TempDir(), "synthetic.mp4")
	cmd := exec.Command(codec.FFmpegPath, "-nostdin", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=320x180:rate=30:duration=1.2", "-f", "lavfi", "-i", "sine=frequency=440:duration=1.2", "-c:v", "libx264", "-threads", "1", "-pix_fmt", "yuv420p", "-c:a", "aac", "-metadata", "comment=SYNTHETIC_PRIVATE_METADATA", "-movflags", "+faststart", file)
	if e := cmd.Run(); e != nil {
		t.Fatal("real synthetic video generation", e)
	}
	raw, e := os.ReadFile(file)
	if e != nil {
		t.Fatal(e)
	}
	defer clear(raw)
	input, _ := legacyVideoNativeInput(t, raw)
	p, e := legacyvideo.Build(legacyTestRunID, input.Source, input.Manifest, input.Matched, input.AuthenticatedSeal, input.ExpectedSealSHA256)
	if e != nil || p.Counts != (legacyvideo.Counts{Videos: 2, Notes: 1, Owners: 1}) {
		t.Fatal("authenticated original video plan", e)
	}
	reader := func(context.Context, legacyvideo.Record) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(raw)), nil
	}
	if _, e = f.Importer.ApplyLegacyPrivateVideo(f.Context, legacyTestRunID, input, reader, codec); !errors.Is(e, ErrLegacyMigrationNotFound) {
		t.Fatal("missing native156 accepted", e)
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
	calls := 0
	bad := func(context.Context, legacyvideo.Record) (io.ReadCloser, error) {
		calls++
		if calls == 2 {
			return io.NopCloser(strings.NewReader("wrong second original")), nil
		}
		return io.NopCloser(bytes.NewReader(raw)), nil
	}
	if _, e = f.Importer.ApplyLegacyPrivateVideo(f.Context, legacyTestRunID, input, bad, codec); !errors.Is(e, ErrLegacyPrivateVideoDerivative) {
		t.Fatal("bad second input accepted", e)
	}
	var clean bool
	if e = f.Pool.QueryRow(f.Context, `SELECT NOT EXISTS(SELECT 1 FROM legacy_private_video_import_runs) AND NOT EXISTS(SELECT 1 FROM legacy_private_video_import_items) AND NOT EXISTS(SELECT 1 FROM private_moment_videos) AND NOT EXISTS(SELECT 1 FROM media_assets WHERE storage_key LIKE 'legacy-private-video/%')`).Scan(&clean); e != nil || !clean {
		t.Fatal("partial native video survived rollback", e)
	}
	first, e := f.Importer.ApplyLegacyPrivateVideo(f.Context, legacyTestRunID, input, reader, codec)
	if e != nil || first.Status != "COMMITTED" || first.Counts != p.Counts {
		t.Fatal("complete native import", e)
	}
	var owner, moment string
	if e = f.Pool.QueryRow(f.Context, `SELECT owner_account_id::text,moment_id::text FROM legacy_private_video_import_items LIMIT 1`).Scan(&owner, &moment); e != nil {
		t.Fatal(e)
	}
	if e = f.Pool.QueryRow(f.Context, `SELECT (SELECT count(*) FROM sessions)=$2 AND (SELECT count(*) FROM accounts WHERE status='active')=$3 AND (SELECT count(*) FROM private_video_materials)=$4 AND EXISTS(SELECT 1 FROM accounts WHERE id=$1 AND status='suspended')`, owner, beforeSessions, beforeActive, beforeMaterials).Scan(&clean); e != nil || !clean {
		t.Fatal("import changed Sessions / active account baseline /125 or activated original owner", e)
	}
	digest := sha256.Sum256([]byte("synthetic-native-video-owner-session"))
	if _, e = f.Pool.Exec(f.Context, `UPDATE accounts SET status='active' WHERE id=$1`, owner); e != nil {
		t.Fatal(e)
	}
	if _, e = f.Pool.Exec(f.Context, `WITH cl AS MATERIALIZED(SELECT clock_timestamp() at) INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at)SELECT $1,$2,'synthetic_test',cl.at+interval '1 hour',cl.at+interval '30 minutes' FROM cl`, owner, digest[:]); e != nil {
		t.Fatal(e)
	}
	store := New(f.Pool, true)
	actor := identity.Actor{ID: owner, AccountType: "person"}
	var receipt media.PrivateVideoReceipt
	_, e = store.ReadHumanPrivateVideos(f.Context, digest, actor, moment, p.Records[0].VideoID, "content", func(items []media.PrivateVideoReceipt, b []byte) ([]byte, error) {
		if len(items) != 1 || !items[0].Valid() || int64(len(b)) != items[0].ByteSize || legacyvideo.Hash(b) != items[0].SHA256 || bytes.Contains(b, []byte("SYNTHETIC_PRIVATE_METADATA")) {
			return nil, errors.New("unsafe native playback")
		}
		receipt = items[0]
		return []byte("READABLE"), nil
	})
	if e != nil {
		t.Fatal("owner could not read real playback", e)
	}
	peer, _ := legacyMigrationUUID()
	peerDigest := sha256.Sum256([]byte("synthetic-native-video-foreign-session"))
	if _, e = f.Pool.Exec(f.Context, `INSERT INTO accounts(id,account_type,status)VALUES($1,'person','active')`, peer); e != nil {
		t.Fatal(e)
	}
	if _, e = f.Pool.Exec(f.Context, `WITH cl AS MATERIALIZED(SELECT clock_timestamp() at) INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at)SELECT $1,$2,'synthetic_test',cl.at+interval '1 hour',cl.at+interval '30 minutes' FROM cl`, peer, peerDigest[:]); e != nil {
		t.Fatal(e)
	}
	if _, e = store.ReadHumanPrivateVideos(f.Context, peerDigest, identity.Actor{ID: peer, AccountType: "person"}, moment, p.Records[0].VideoID, "content", func([]media.PrivateVideoReceipt, []byte) ([]byte, error) {
		t.Fatal("foreign callback received private bytes")
		return nil, nil
	}); !errors.Is(e, content.ErrNotFound) {
		t.Fatal("foreign owner allowed", e)
	}
	if _, e = store.DeleteHumanPrivateVideo(f.Context, digest, actor, moment, receipt.ID, receipt.Revision); e != nil {
		t.Fatal("ordinary owner deletion", e)
	}
	for _, retry := range []bool{false, true} {
		var next LegacyPrivateVideoResult
		if retry {
			next, e = f.Importer.ApplyLegacyPrivateVideo(f.Context, legacyTestRunID, input, func(context.Context, legacyvideo.Record) (io.ReadCloser, error) {
				t.Fatal("retry reopened original")
				return nil, nil
			}, nil)
		} else {
			next, e = f.Importer.InspectLegacyPrivateVideo(f.Context, legacyTestRunID)
		}
		if e != nil || !reflect.DeepEqual(first, next) {
			t.Fatal("immutable original receipt changed", e)
		}
	}
	if e = f.Pool.QueryRow(f.Context, `SELECT status='deleted' AND original IS NULL AND content IS NULL AND poster IS NULL FROM private_moment_videos WHERE id=$1`, receipt.ID).Scan(&clean); e != nil || !clean {
		t.Fatal("delete/replay retained bytes", e)
	}
	// A legitimate private text revision keeps the second imported attachment.
	if _, e = f.Pool.Exec(f.Context, `UPDATE moments SET title='own changed title',revision=revision+1 WHERE id=$1`, moment); e != nil {
		t.Fatal(e)
	}
	if _, e = store.ReadHumanPrivateVideos(f.Context, digest, actor, moment, p.Records[1].VideoID, "poster", func(v []media.PrivateVideoReceipt, b []byte) ([]byte, error) {
		if len(v) != 1 || legacyvideo.Hash(b) != v[0].PosterSHA256 {
			return nil, errors.New("bad poster")
		}
		return []byte("POSTER"), nil
	}); e != nil {
		t.Fatal("new private text revision lost video", e)
	}
	if _, e = f.Pool.Exec(f.Context, `UPDATE moments SET status='withdrawn',revision=revision+1 WHERE id=$1`, moment); e != nil {
		t.Fatal("Moment withdraw", e)
	}
	if e = f.Pool.QueryRow(f.Context, `SELECT NOT EXISTS(SELECT 1 FROM private_moment_videos WHERE status<>'deleted' OR original IS NOT NULL OR content IS NOT NULL OR poster IS NOT NULL)`).Scan(&clean); e != nil || !clean {
		t.Fatal("Moment withdrawal retained native video bytes", e)
	}
	if _, e = store.ReadHumanPrivateVideos(f.Context, digest, actor, moment, p.Records[1].VideoID, "content", func([]media.PrivateVideoReceipt, []byte) ([]byte, error) {
		t.Fatal("withdrawn Moment yielded bytes")
		return nil, nil
	}); !errors.Is(e, content.ErrNotFound) {
		t.Fatal("withdrawn Moment readable", e)
	}
}
