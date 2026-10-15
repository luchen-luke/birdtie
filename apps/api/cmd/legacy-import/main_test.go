package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
)

const testRun = "14600000-0000-4000-8000-000000000001"

func testTarget() postgres.LegacyMigrationTargetIdentity {
	return postgres.LegacyMigrationTargetIdentity{DatabaseName: "birdtie_owned_legacy_test", DatabaseOID: 100,
		Owner: "birdtie", Marker: "birdtie-owned-legacy-import-2026-10-15:" + strings.Repeat("a", 32), ForbiddenSourceDatabaseOID: 99}
}

func TestLegacyCLIArgumentsDoNotEchoPrivateValues(t *testing.T) {
	for _, args := range [][]string{{"--whole-db", "private-credential"}, {"--source", "private-path"}, {"--database-oid", "private-secret"}, {"--apply", "--inspect-run", testRun}, {"--inspect-run", testRun, "--run-id", testRun}, {"--run-id", testRun, "--deadline", "900s"}} {
		var out bytes.Buffer
		err := run(context.Background(), args, func(string) string { return "" }, &out)
		if err == nil || strings.Contains(err.Error(), "private-") || out.Len() != 0 {
			t.Fatal("argument error leaked values or implied success")
		}
	}
}

func TestLegacyCLITargetConfigRejectsSourceAndAmbiguousEnvironment(t *testing.T) {
	getenv := func(string) string { return "" }
	valid := "postgres://birdtie@127.0.0.1:1/birdtie_owned_legacy_test?sslmode=disable"
	config, err := legacyImportConfig(valid, testTarget(), getenv)
	if err != nil || config.ConnConfig.Database != "birdtie_owned_legacy_test" || config.MaxConns != 1 || config.MinConns != 0 || config.ConnConfig.RuntimeParams["search_path"] != "public" {
		t.Fatal("exact loopback configuration rejected")
	}
	for _, raw := range []string{"postgres://birdtie:private-secret@127.0.0.1/civu?sslmode=disable", "postgres://birdtie@127.0.0.1/civu_restore?sslmode=disable", "postgres://other@127.0.0.1/birdtie_owned_legacy_test?sslmode=disable", "postgres://birdtie@remote.invalid/birdtie_owned_legacy_test?sslmode=disable", "postgres://birdtie@127.0.0.1/birdtie_owned_legacy_test?sslmode=disable&role=other", "postgres://birdtie@127.0.0.1/birdtie_owned_legacy_test?sslmode=disable&options=-c%20search_path%3Dprivate", "host=source user=private"} {
		if _, err := legacyImportConfig(raw, testTarget(), getenv); err == nil || strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "postgres:") {
			t.Fatal("unsafe configuration accepted or exposed")
		}
	}
	if _, err := legacyImportConfig(valid, testTarget(), func(key string) string {
		if key == "PGSERVICE" {
			return "private-service"
		}
		return ""
	}); err == nil {
		t.Fatal("ambient alternate connection accepted")
	}
	wrong := testTarget()
	wrong.DatabaseOID = wrong.ForbiddenSourceDatabaseOID
	if _, err := legacyImportConfig(valid, wrong, getenv); err == nil {
		t.Fatal("source database OID accepted")
	}
}

func TestLegacyCLIPrivateInputBoundariesPreserveBytes(t *testing.T) {
	base := t.TempDir()
	repository := filepath.Join(base, "repo")
	private := filepath.Join(base, "private")
	if os.MkdirAll(repository, 0700) != nil || os.MkdirAll(private, 0700) != nil {
		t.Fatal("fixture setup failed")
	}
	path := filepath.Join(private, "source.json")
	want := []byte(`{"only":"synthetic-private-fixture"}`)
	if os.WriteFile(path, want, 0600) != nil {
		t.Fatal("fixture setup failed")
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal("fixture setup failed")
	}
	got, err := readPrivateInput(path, repository)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatal("protected outside-repository input rejected")
	}
	after, err := os.Stat(path)
	if err != nil || after.Mode() != before.Mode() || !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("read changed original permissions/content time")
	}
	inside := filepath.Join(repository, "source.json")
	if os.WriteFile(inside, want, 0600) != nil {
		t.Fatal("fixture setup failed")
	}
	for _, p := range []string{inside, "source.json", private, filepath.Join(private, "missing")} {
		if _, err := readPrivateInput(p, repository); !errors.Is(err, errInputFile) {
			t.Fatal("unprotected path accepted")
		}
	}
	large := filepath.Join(private, "large.json")
	f, err := os.Create(large)
	if err != nil {
		t.Fatal("large fixture setup failed")
	}
	err = f.Truncate(postgres.LegacyMigrationMaxInputBytes + 1)
	_ = f.Close()
	if err != nil {
		t.Fatal("large fixture setup failed")
	}
	if _, err := readPrivateInput(large, repository); !errors.Is(err, errInputFile) {
		t.Fatal("oversize input read or truncated silently")
	}
	link := filepath.Join(private, "linked.json")
	if err = os.Symlink(path, link); err == nil {
		if _, err = readPrivateInput(link, repository); !errors.Is(err, errInputFile) {
			t.Fatal("symlink input accepted")
		}
	} else {
		t.Log("NOT_RUN: actual symlink fixture unavailable; path, size and reparse-attribute gates independently checked")
	}
}

type reparseFixtureInfo struct{}

func (reparseFixtureInfo) Name() string       { return "fixture" }
func (reparseFixtureInfo) Size() int64        { return 1 }
func (reparseFixtureInfo) Mode() os.FileMode  { return 0 }
func (reparseFixtureInfo) ModTime() time.Time { return time.Time{} }
func (reparseFixtureInfo) IsDir() bool        { return false }
func (reparseFixtureInfo) Sys() any           { return &struct{ FileAttributes uint32 }{0x400} }

func TestLegacyCLIWindowsReparseAttributeIsRejected(t *testing.T) {
	if runtime.GOOS == "windows" && !reparseOrLink(reparseFixtureInfo{}) {
		t.Fatal("junction/reparse flag was ignored")
	}
}

func cliSyntheticInputs(t *testing.T, private string) (string, string) {
	t.Helper()
	tables := map[string]any{}
	counts := map[string]any{}
	hashes := map[string]any{}
	for _, name := range legacymigration.Tables() {
		tables[name] = []any{}
		counts[name] = 0
		hashes[name], _ = legacymigration.Digest([]any{})
	}
	header := map[string]any{"instanceTag": legacymigration.SourceSystem, "container": "birdtie-civu-restore-20261014", "database": "civu_restore", "backupCipherSha256": legacymigration.BackupCipherSHA256, "schemaSha256": legacymigration.SourceSchemaSHA256}
	header["snapshotSha256"], _ = legacymigration.Digest(map[string]any{"source": header, "tables": tables})
	source, err := json.Marshal(map[string]any{"schemaVersion": legacymigration.SourceProtocol, "source": header, "capture": map[string]any{"observedAt": "2026-10-15T00:00:00Z", "transactionSnapshot": "100:100:"}, "tables": tables})
	if err != nil {
		t.Fatal("synthetic source setup failed")
	}
	sum := sha256.Sum256(source)
	manifest, err := json.Marshal(map[string]any{"schemaVersion": legacymigration.ManifestProtocol, "source": header, "sourceFileSha256": hex.EncodeToString(sum[:]), "auditedSchemaOutputSha256": legacymigration.AuditedOutputSHA256, "counts": counts, "tableSha256": hashes,
		"families":  map[string]any{"accounts": []string{"users", "auth_identities"}, "places": []string{"cities", "regions", "geo_areas", "places", "place_area_relations", "place_provider_refs"}, "saved": []string{"user_saved_places", "place_save_sources"}, "collections": []string{"journeys", "journey_stops", "journey_arrangements", "journey_notes", "journey_members", "journey_media", "journey_publications", "journey_publication_places", "theme_maps", "theme_map_entries", "theme_map_entry_sources"}, "notes": []string{"notes", "note_places", "note_media", "note_media_locations", "moment_threads", "moment_thread_legs"}, "media": []string{"media_assets", "place_images"}},
		"execution": map[string]any{"preservation": "EXTRACTED_ONLY", "targetImport": "NOT_RUN", "identityVerification": "NOT_RUN", "sourceRights": "NOT_RUN", "mediaBytesMatched": "NOT_RUN", "mediaAuthorization": "NOT_RUN", "sessionMigration": "NOT_RUN", "relationshipEffects": 0, "sourceWrites": 0, "runtimeStarted": false}})
	if err != nil {
		t.Fatal("synthetic manifest setup failed")
	}
	sourcePath, manifestPath := filepath.Join(private, "source.json"), filepath.Join(private, "manifest.json")
	if os.WriteFile(sourcePath, source, 0600) != nil || os.WriteFile(manifestPath, manifest, 0600) != nil {
		t.Fatal("synthetic file setup failed")
	}
	return sourcePath, manifestPath
}

func TestLegacyCLIDefaultPrepareNeverLooksUpDatabase(t *testing.T) {
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	private := filepath.Join(base, "private")
	if os.MkdirAll(repo, 0700) != nil || os.MkdirAll(private, 0700) != nil {
		t.Fatal("fixture setup failed")
	}
	source, manifest := cliSyntheticInputs(t, private)
	getenv := func(key string) string {
		if key == "BIRDTIE_REPOSITORY_ROOT" {
			return repo
		}
		t.Fatal("prepare accessed database environment")
		return ""
	}
	var out bytes.Buffer
	err := run(context.Background(), []string{"--run-id", testRun, "--source", source, "--manifest", manifest}, getenv, &out)
	if err != nil {
		t.Fatal("synthetic prepare failed")
	}
	var summary publicSummary
	if json.Unmarshal(out.Bytes(), &summary) != nil || summary.Status != "PREPARED_NOT_IMPORTED" || !summary.CountsKnown || summary.Activated || summary.MediaImported || summary.SourceWrites != 0 || strings.Contains(out.String(), private) || strings.Contains(out.String(), "Sha256") {
		t.Fatal("prepare leaked values or claimed apply")
	}
}

func TestLegacyCLIUnknownAndMissingRunDoNotReportZeroEffects(t *testing.T) {
	for _, status := range []string{"UNKNOWN", "NOT_FOUND"} {
		var out bytes.Buffer
		if err := writeSummary(&out, testRun, status, postgres.LegacyMigrationCounts{}); err != nil {
			t.Fatal(err)
		}
		var summary map[string]any
		if json.Unmarshal(out.Bytes(), &summary) != nil || summary["countsKnown"] != false {
			t.Fatal("unknown counts marked authoritative")
		}
		if _, present := summary["counts"]; present {
			t.Fatal("missing/unknown run invented zero committed effects")
		}
	}
}

func TestLegacyDevPhoneCLIPrepareAndProtectedChannel(t *testing.T) {
	base := t.TempDir()
	repo, private := filepath.Join(base, "repo"), filepath.Join(base, "private")
	if os.MkdirAll(repo, 0700) != nil || os.MkdirAll(private, 0700) != nil {
		t.Fatal("owned input fixture")
	}
	source, manifest := cliSyntheticInputs(t, private)
	var out bytes.Buffer
	e := run(context.Background(), []string{"--dev-phone-bindings", "--run-id", testRun, "--source", source, "--manifest", manifest}, func(key string) string {
		if key == "BIRDTIE_REPOSITORY_ROOT" {
			return repo
		}
		t.Fatal("prepare touched database or runtime configuration")
		return ""
	}, &out)
	var summary map[string]any
	if e != nil || json.Unmarshal(out.Bytes(), &summary) != nil || summary["status"] != "DEV_PHONE_BINDINGS_PREPARED" || summary["countsKnown"] != true || summary["devOnly"] != true || summary["activated"] != false || summary["agentActivated"] != false || summary["oldSessionsCopied"] != false || strings.Contains(out.String(), private) {
		t.Fatal("safe prepare summary", e)
	}
	config, e := legacyImportConfig("postgres://birdtie@127.0.0.1:1/birdtie_owned_legacy_test?sslmode=disable", testTarget(), func(string) string { return "" })
	if e != nil {
		t.Fatal("config fixture")
	}
	settings := map[string]string{"BIRDTIE_DEV_PHONE_AUTH": "1", "BIRDTIE_API_ADDR": "127.0.0.1:18114", "BIRDTIE_ALLOWED_ORIGINS": "http://localhost:7357"}
	getenv := func(key string) string { return settings[key] }
	if !devPhoneBindingChannel(config, getenv) {
		t.Fatal("protected current fixed-code channel rejected")
	}
	settings["BIRDTIE_ALLOWED_ORIGINS"] = "http://localhost:7357,https://47.110.154.133"
	if devPhoneBindingChannel(config, getenv) {
		t.Fatal("importer public channel opened without explicit opt-in")
	}
	settings["BIRDTIE_DEV_PHONE_PUBLIC_TEST_ORIGIN"] = "https://47.110.154.133"
	if !devPhoneBindingChannel(config, getenv) {
		t.Fatal("importer rejected exact authorized public test channel")
	}
	settings["BIRDTIE_DEV_PHONE_PUBLIC_TEST_ORIGIN"] = "https://outside.invalid"
	if devPhoneBindingChannel(config, getenv) {
		t.Fatal("importer accepted alternate public test URL")
	}
	delete(settings, "BIRDTIE_DEV_PHONE_PUBLIC_TEST_ORIGIN")
	settings["BIRDTIE_ALLOWED_ORIGINS"] = "http://localhost:7357"
	for key, value := range map[string]string{"BIRDTIE_DEV_PHONE_AUTH": "0", "BIRDTIE_API_ADDR": "0.0.0.0:18114", "BIRDTIE_ALLOWED_ORIGINS": "https://outside.invalid"} {
		old := settings[key]
		settings[key] = value
		if devPhoneBindingChannel(config, getenv) {
			t.Fatal("unprotected fixed-code channel accepted")
		}
		settings[key] = old
	}
	out.Reset()
	if writeDevPhoneSummary(&out, postgres.LegacyDevPhoneBindingResult{RunID: testRun, Status: "UNKNOWN"}) != nil || json.Unmarshal(out.Bytes(), &summary) != nil {
		t.Fatal("unknown output")
	}
	if strings.Contains(out.String(), "\"counts\"") || strings.Contains(out.String(), "\"enrolledOwners\"") {
		t.Fatal("unknown result invented zero committed effects")
	}
	if run(context.Background(), []string{"--dev-phone-bindings", "--inspect-run", testRun}, func(string) string { return "" }, &bytes.Buffer{}) == nil {
		t.Fatal("ambiguous inspect accepted")
	}
}

func TestLegacyDevEmailCLIRealmAndUnknownSummary(t *testing.T) {
	var out bytes.Buffer
	if e := run(context.Background(), []string{"--dev-phone-bindings", "--dev-email-bindings"}, func(string) string { return "" }, &out); e == nil || out.Len() != 0 {
		t.Fatal("mixed credential binding modes accepted")
	}
	if e := writeDevEmailSummary(&out, postgres.LegacyDevEmailBindingResult{RunID: testRun, Status: "UNKNOWN", Counts: legacymigration.DevEmailCounts{Owners: 2, BindableOwners: 2}}); e != nil {
		t.Fatal(e)
	}
	var v map[string]any
	if json.Unmarshal(out.Bytes(), &v) != nil || v["countsKnown"] != false || v["counts"] != nil || v["enrolledOwners"] != nil || v["credentialKind"] != "email" || v["devOnly"] != true || v["activated"] != false {
		t.Fatal("unknown receipt fabricated successful binding or activation")
	}
}
