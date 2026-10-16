package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/httpapi"
	"github.com/jackc/pgx/v5"
)

func nativeLookup(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) { value, ok := values[key]; return value, ok }
}

func TestNativeDatabaseTargetMustBeExplicitBeforeConnection(t *testing.T) {
	for _, name := range []string{"birdtie_hangzhou", "birdtie_phone_owned_a892882bedb7"} {
		got, err := loadNativeDatabaseName(nativeLookup(map[string]string{"BIRDTIE_NATIVE_DATABASE_NAME": name}), name)
		if err != nil || got != name {
			t.Fatal("explicit native target rejected")
		}
	}
	for _, name := range []string{"", "civu", "civu_restore", "civu_source_copy", "postgres", "template0", "template1", "../birdtie", "birdtie\n", " birdtie", "birdtie-db", strings.Repeat("a", 64)} {
		t.Run(name, func(t *testing.T) {
			if _, err := loadNativeDatabaseName(nativeLookup(map[string]string{"BIRDTIE_NATIVE_DATABASE_NAME": name}), name); !errors.Is(err, errNativeDatabaseBoundary) {
				t.Fatal("unsafe target accepted")
			}
		})
	}
	if _, err := loadNativeDatabaseName(nativeLookup(nil), "birdtie_phone_owned_a892882bedb7"); err == nil {
		t.Fatal("owned-looking name is not explicit authorization")
	}
	if _, err := loadNativeDatabaseName(nativeLookup(map[string]string{"BIRDTIE_NATIVE_DATABASE_NAME": "birdtie_hangzhou"}), "civu"); err == nil {
		t.Fatal("mismatched connection target accepted")
	}
}

func TestBackgroundMaintenanceRequiresExplicitBoolean(t *testing.T) {
	for _, tc := range []struct {
		value   string
		present bool
		want    bool
		valid   bool
	}{{"", false, false, true}, {"false", true, false, true}, {"true", true, true, true}, {" true ", true, true, true}, {"", true, false, false}, {"1", true, false, false}, {"0", true, false, false}, {"TRUE", true, false, false}, {"maybe", true, false, false}} {
		t.Run(tc.value+"-present="+strconv.FormatBool(tc.present), func(t *testing.T) {
			values := map[string]string{}
			if tc.present {
				values["BIRDTIE_BACKGROUND_MAINTENANCE"] = tc.value
			}
			got, err := loadBackgroundMaintenance(nativeLookup(values))
			if (err == nil) != tc.valid || got != tc.want {
				t.Fatal("maintenance did not fail closed")
			}
		})
	}
}

type nativeProbeRow struct {
	name    string
	oid     int64
	legacy  bool
	missing []string
	err     error
}

func (r nativeProbeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != 4 {
		return errors.New("unexpected scan")
	}
	*dest[0].(*string) = r.name
	*dest[1].(*int64) = r.oid
	*dest[2].(*bool) = r.legacy
	*dest[3].(*[]string) = r.missing
	return nil
}

type nativeProbeReader struct {
	t     *testing.T
	row   nativeProbeRow
	calls int
}

func (r *nativeProbeReader) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	r.t.Helper()
	r.calls++
	if ctx == nil || query != nativeDatabaseProbeSQL || len(args) != 0 {
		r.t.Fatal("guard did not use the fixed metadata-only read")
	}
	return r.row
}

func validNativeReader(t *testing.T) *nativeProbeReader {
	return &nativeProbeReader{t: t, row: nativeProbeRow{name: "birdtie_hangzhou", oid: 42}}
}

func TestNativeStartupRefusesUnsafeDependencyWithoutReading(t *testing.T) {
	if pinger, err := newNativeDatabasePinger(context.Background(), nil, "birdtie_hangzhou"); pinger != nil || err == nil {
		t.Fatal("missing native database accepted")
	}
	for _, name := range []string{"civu", "civu_restore", "postgres", "template0", ""} {
		reader := validNativeReader(t)
		if pinger, err := newNativeDatabasePinger(context.Background(), reader, name); pinger != nil || err == nil || reader.calls != 0 {
			t.Fatal("unsafe dependency was probed")
		}
	}
}

func TestNativeStartupRejectsLegacyIncompleteAndUnavailableSchemas(t *testing.T) {
	for _, tc := range []struct {
		name string
		row  nativeProbeRow
	}{
		{"legacy-source", nativeProbeRow{name: "birdtie_hangzhou", oid: 42, legacy: true}},
		{"wrong-database", nativeProbeRow{name: "civu_restore", oid: 42}},
		{"no-native-account", nativeProbeRow{name: "birdtie_hangzhou", oid: 42, missing: []string{"accounts.account_type"}}},
		{"no-native-agent", nativeProbeRow{name: "birdtie_hangzhou", oid: 42, missing: []string{"agents.principal_account_id"}}},
		{"wrong-session-type", nativeProbeRow{name: "birdtie_hangzhou", oid: 42, missing: []string{"sessions.token_sha256"}}},
		{"no-145", nativeProbeRow{name: "birdtie_hangzhou", oid: 42, missing: []string{"public_moment_photos.rights"}}},
		{"no-148-original-fact", nativeProbeRow{name: "birdtie_hangzhou", oid: 42, missing: []string{"human_moment_control_operations.payload_sha256"}}},
		{"no-148-public-guard", nativeProbeRow{name: "birdtie_hangzhou", oid: 42, missing: []string{"human_moment_control_operations.human_moment_control_immutable"}}},
		{"no-172-author-contract", nativeProbeRow{name: "birdtie_hangzhou", oid: 42, missing: []string{"legacy_moment_author.contract"}}},
		{"no-172-author-binding", nativeProbeRow{name: "birdtie_hangzhou", oid: 42, missing: []string{"legacy_moment_author_facts.legacy_moment_author_fact_binding"}}},
		{"no-171-content-map-contract", nativeProbeRow{name: "birdtie_hangzhou", oid: 42, missing: []string{"content_map.contract"}}},
		{"no-171-anchor-binding", nativeProbeRow{name: "birdtie_hangzhou", oid: 42, missing: []string{"legacy_map_anchor_facts.legacy_map_anchor_fact_binding"}}},
		{"no-150-city-public-contract", nativeProbeRow{name: "birdtie_hangzhou", oid: 42, missing: []string{"city_public_moment.contract"}}},
		{"no-151-moment-interaction-contract", nativeProbeRow{name: "birdtie_hangzhou", oid: 42, missing: []string{"moment_interaction.contract"}}},
		{"no-152-public-media-contract", nativeProbeRow{name: "birdtie_hangzhou", oid: 42, missing: []string{"public_moment_media.contract"}}},
		{"no-153-moment-thread-contract", nativeProbeRow{name: "birdtie_hangzhou", oid: 42, missing: []string{"moment_thread.contract"}}},
		{"no-168-moment-thread-editing-contract", nativeProbeRow{name: "birdtie_hangzhou", oid: 42, missing: []string{"moment_thread_editing.contract"}}},
		{"no-154-legacy-dev-phone-contract", nativeProbeRow{name: "birdtie_hangzhou", oid: 42, missing: []string{"legacy_dev_phone.contract"}}},
		{"no-155-public-video-contract", nativeProbeRow{name: "birdtie_hangzhou", oid: 42, missing: []string{"public_moment_video.contract"}}},
		{"no-169-public-video-carry-contract", nativeProbeRow{name: "birdtie_hangzhou", oid: 42, missing: []string{"public_moment_video_carry.contract"}}},
		{"no-145-operation", nativeProbeRow{name: "birdtie_hangzhou", oid: 42, missing: []string{"public_moment_photo_operations.payload_sha256"}}},
		{"no-146-owner-mapping", nativeProbeRow{name: "birdtie_hangzhou", oid: 42, missing: []string{"legacy_entity_mappings.owner_account_id"}}},
		{"no-146-private-archive", nativeProbeRow{name: "birdtie_hangzhou", oid: 42, missing: []string{"legacy_note_archives.source_relationships"}}},
		{"disabled-146-immutability", nativeProbeRow{name: "birdtie_hangzhou", oid: 42, missing: []string{"legacy_note_archives.legacy_note_immutable"}}},
		{"missing-146-atomic-binding", nativeProbeRow{name: "birdtie_hangzhou", oid: 42, missing: []string{"legacy_import_runs.legacy_run_complete"}}},
		{"no-147-operation", nativeProbeRow{name: "birdtie_hangzhou", oid: 42, missing: []string{"personal_collection_control_operations.payload_digest"}}},
		{"disabled-147-immutability", nativeProbeRow{name: "birdtie_hangzhou", oid: 42, missing: []string{"personal_collection_control_operations.personal_collection_control_operation_guard"}}},
		{"no-database-identity", nativeProbeRow{name: "birdtie_hangzhou"}},
		{"private-error", nativeProbeRow{err: errors.New("postgres://private-secret@source/private-sql")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := &nativeProbeReader{t: t, row: tc.row}
			pinger, err := newNativeDatabasePinger(context.Background(), reader, "birdtie_hangzhou")
			if pinger != nil || !errors.Is(err, errNativeDatabaseBoundary) || err.Error() != "native_database_boundary_unavailable" || reader.calls != 1 {
				t.Fatal("unsafe startup or error detail escaped the boundary")
			}
		})
	}
}

func TestNativeReadyRechecksSchemaAndPinnedDatabaseOID(t *testing.T) {
	reader := validNativeReader(t)
	pinger, err := newNativeDatabasePinger(context.Background(), reader, "birdtie_hangzhou")
	if err != nil || pinger.Ping(context.Background()) != nil || reader.calls != 2 {
		t.Fatal("native schema should be ready")
	}
	reader.row.missing = []string{"public_moment_photo_operations.status"}
	if pinger.Ping(context.Background()) == nil {
		t.Fatal("a live connection hid an incomplete schema")
	}
	reader.row.missing = nil
	reader.row.oid = 43
	if pinger.Ping(context.Background()) == nil {
		t.Fatal("same-name replacement database became ready")
	}
	reader.row.oid = 42
	reader.row.legacy = true
	if pinger.Ping(context.Background()) == nil {
		t.Fatal("introduced Civu source table became ready")
	}
	reader.row.legacy = false
	if pinger.Ping(context.Background()) != nil {
		t.Fatal("original native identity did not recover")
	}
}

func TestNativeReadyHTTPActuallyUsesTheGuard(t *testing.T) {
	reader := validNativeReader(t)
	pinger, err := newNativeDatabasePinger(context.Background(), reader, "birdtie_hangzhou")
	if err != nil {
		t.Fatal(err)
	}
	handler := httpapi.New(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, pinger, nil)
	get := func(path string) *httptest.ResponseRecorder {
		r := httptest.NewRecorder()
		handler.ServeHTTP(r, httptest.NewRequest(http.MethodGet, path, nil))
		return r
	}
	if get("/readyz").Code != http.StatusOK {
		t.Fatal("valid native ready request failed")
	}
	reader.row.err = errors.New("private-db-query-and-password")
	unavailable := get("/readyz")
	if unavailable.Code != http.StatusServiceUnavailable || strings.Contains(unavailable.Body.String(), "private-db") || !strings.Contains(unavailable.Body.String(), "database_unavailable") {
		t.Fatal("ready request did not safely reject the source")
	}
	if get("/healthz").Code != http.StatusOK {
		t.Fatal("health should report process liveness separately")
	}
}

func TestNativeDatabaseGuardHonorsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := newNativeDatabasePinger(ctx, validNativeReader(t), "birdtie_hangzhou"); err == nil {
		t.Fatal("cancelled startup became ready")
	}
}

type maintenanceSpy struct {
	calls       atomic.Int32
	badDeadline atomic.Bool
	done        chan struct{}
}

func (s *maintenanceSpy) EnqueueStartsSoonReminders(ctx context.Context) (int64, error) {
	s.calls.Add(1)
	if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) <= 0 || time.Until(deadline) > 10*time.Second {
		s.badDeadline.Store(true)
		return 0, errors.New("missing maintenance deadline")
	}
	return 0, nil
}

func (s *maintenanceSpy) PurgeExpiredAgentVisualPixels(context.Context) (int64, error) {
	s.calls.Add(1)
	if s.done != nil {
		close(s.done)
	}
	return 0, nil
}

func TestBackgroundMaintenanceDisabledDoesNotStartWrites(t *testing.T) {
	spy := &maintenanceSpy{done: make(chan struct{})}
	startBackgroundMaintenance(context.Background(), false, spy)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	startBackgroundMaintenance(cancelled, true, spy)
	select {
	case <-spy.done:
		t.Fatal("disabled/cancelled scheduler wrote to the database")
	case <-time.After(30 * time.Millisecond):
	}
	if spy.calls.Load() != 0 {
		t.Fatal("disabled/cancelled scheduler enqueued reminders")
	}
}

func TestBackgroundMaintenanceExplicitEnableUsesBoundedContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	spy := &maintenanceSpy{done: make(chan struct{})}
	startBackgroundMaintenance(ctx, true, spy)
	select {
	case <-spy.done:
	case <-time.After(time.Second):
		t.Fatal("explicitly enabled maintenance did not run")
	}
	cancel()
	if spy.calls.Load() != 2 || spy.badDeadline.Load() {
		t.Fatal("maintenance did not run the two bounded actions once")
	}
}
