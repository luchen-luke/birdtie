package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func externalCacheFixture() (agentworkspace.OnlinePlaceCacheScope, agentworkspace.OnlinePlaceSearch, time.Time) {
	at := time.Now().UTC().Truncate(time.Microsecond)
	lat, lon := 57.15, -2.09
	q := agentworkspace.OnlinePlaceCacheScope{CityID: "aberdeen-gb", Provider: "openstreetmap", CategoryCode: "restaurant", Latitude: lat, Longitude: lon, RadiusMeters: 6000, PolicyVersion: "odbl-cache-v1"}
	v := agentworkspace.OnlinePlaceSearch{Schema: "birdtie-online-places-v2", CityID: q.CityID, Provider: q.Provider, Status: "SUCCESS", Scope: "city", Attribution: externalPlaceAttribution, LicenseURL: externalPlaceLicenseURL,
		ObservedAt: at, ExpiresAt: at.Add(24 * time.Hour), Query: "有没有好的餐厅", RequestID: "PRIVATE_REQUEST_CANARY", SnapshotID: currentReadonlyPGEntity, TaskID: currentReadonlyPGTask, TurnDigest: agentworkspace.OnlinePlaceTurnDigest("有没有好的餐厅"), MessageIndex: 1, Origin: "LIVE", Freshness: "FRESH",
		Places: []agentworkspace.ExternalPlaceCandidate{{ID: "osm:node:101", CityID: q.CityID, CategoryCode: q.CategoryCode, Name: "UNIT public restaurant", AddressLabel: "UNIT public address",
			Location: foundation.Location{Precision: "point", CoordinateSystem: "wgs84", Latitude: &lat, Longitude: &lon}, SourceURL: "https://www.openstreetmap.org/node/101", Attribution: externalPlaceAttribution, ReviewStatus: "UNREVIEWED", ObservedAt: at, ExpiresAt: at.Add(24 * time.Hour)}}}
	return q, v, at
}

func TestExternalPlaceCacheSeparatesPublicLibraryFromPrivateConversation(t *testing.T) {
	q, v, at := externalCacheFixture()
	pub, e := publicExternalCacheSearch(q, v, at)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(pub)
	if strings.Contains(string(raw), v.Query) || strings.Contains(string(raw), "PRIVATE_") || pub.SnapshotID != "" || pub.TaskID != "" || pub.MessageIndex != -1 {
		t.Fatal("public cache retained private query or Task association", string(raw))
	}
	if len(pub.Places) != 1 || pub.Places[0].ID != v.Places[0].ID || pub.Places[0].Name != v.Places[0].Name || pub.Places[0].ReviewStatus != "UNREVIEWED" {
		t.Fatal("public place identity/provenance changed")
	}
	*pub.Places[0].Location.Latitude = 1
	if *v.Places[0].Location.Latitude != q.Latitude {
		t.Fatal("normalizing cache mutated original private snapshot")
	}
}

func TestExternalPlaceCacheExpiredFreshnessStillRestoresSamePlace(t *testing.T) {
	q, v, at := externalCacheFixture()
	pub, e := publicExternalCacheSearch(q, v, at)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(pub)
	for _, now := range []time.Time{at.Add(time.Hour), v.ExpiresAt, v.ExpiresAt.Add(30 * 24 * time.Hour)} {
		got, e := decodePublicExternalCache(q, raw, 4, now)
		if e != nil || len(got.Places) != 1 || got.Places[0].ID != v.Places[0].ID || got.ObservedAt != v.ObservedAt || got.ExpiresAt != v.ExpiresAt || got.CacheVersion != 4 || got.Origin != "CACHE" {
			t.Fatal("freshness became deletion/renewal", got, e)
		}
		want := "FRESH"
		if !v.ExpiresAt.After(now) {
			want = "STALE"
		}
		if got.Freshness != want {
			t.Fatal("stale source was relabeled as current", got.Freshness)
		}
	}
}

func TestExternalPlaceCacheRejectsPrivateAssociationScopeAndFalsePublishedFacts(t *testing.T) {
	q, original, at := externalCacheFixture()
	for _, change := range []func(*agentworkspace.OnlinePlaceSearch){
		func(v *agentworkspace.OnlinePlaceSearch) { v.Status = "UNAVAILABLE"; v.Places = nil },
		func(v *agentworkspace.OnlinePlaceSearch) { v.Scope = "viewport" },
		func(v *agentworkspace.OnlinePlaceSearch) { v.CityID = "different-city" },
		func(v *agentworkspace.OnlinePlaceSearch) { v.Places[0].ReviewStatus = "PUBLISHED" },
		func(v *agentworkspace.OnlinePlaceSearch) { v.Places[0].SourceURL = "https://example.com/restaurant" },
		func(v *agentworkspace.OnlinePlaceSearch) { v.LicenseURL = "https://example.com/license" },
		func(v *agentworkspace.OnlinePlaceSearch) { v.Places = append(v.Places, v.Places[0]) },
		func(v *agentworkspace.OnlinePlaceSearch) { *v.Places[0].Location.Latitude = 58 },
		func(v *agentworkspace.OnlinePlaceSearch) { v.ExpiresAt = v.ObservedAt },
	} {
		v, _ := copyExternalPlaceSearch(original)
		change(&v)
		if _, e := publicExternalCacheSearch(q, v, at); !errors.Is(e, arp.ErrInvalid) {
			t.Fatal("invalid/unavailable/scoped public cache admitted", v, e)
		}
	}
	pub, _ := publicExternalCacheSearch(q, original, at)
	pub.Query = "PRIVATE_QUERY"
	raw, _ := json.Marshal(pub)
	if _, e := decodePublicExternalCache(q, raw, 1, at); !errors.Is(e, arp.ErrChanged) {
		t.Fatal("stored private query released")
	}
	pub.Query = ""
	pub.Status = "EMPTY"
	pub.Places = []agentworkspace.ExternalPlaceCandidate{}
	raw, _ = json.Marshal(pub)
	if got, e := decodePublicExternalCache(q, raw, 1, at.Add(30*24*time.Hour)); e != nil || got.Status != "EMPTY" || got.Freshness != "STALE" {
		t.Fatal("empty batch not cached", got, e)
	}
}

type externalCacheUnitTx struct {
	pgx.Tx
	cityError error
	now       time.Time
	calls     []string
	places    map[string][]byte
	batch     []byte
	version   int64
}

func (tx *externalCacheUnitTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	if strings.HasPrefix(sql, "SELECT id FROM cities") {
		tx.calls = append(tx.calls, "city")
		return currentReadonlyUnitRow{values: []any{args[0]}, err: tx.cityError}
	}
	if strings.HasPrefix(sql, "INSERT INTO external_public_place_search_cache") {
		tx.calls = append(tx.calls, "batch")
		tx.batch = append([]byte(nil), args[9].([]byte)...)
		tx.version++
		return currentReadonlyUnitRow{values: []any{tx.version}}
	}
	if sql == "SELECT clock_timestamp()" {
		tx.calls = append(tx.calls, "clock")
		return currentReadonlyUnitRow{values: []any{tx.now}}
	}
	return currentReadonlyUnitRow{err: errors.New("unexpected unit cache SQL")}
}
func (tx *externalCacheUnitTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if !strings.HasPrefix(sql, "INSERT INTO external_public_places") || !strings.Contains(sql, "ON CONFLICT(provider,provider_place_id)") {
		return pgconn.CommandTag{}, errors.New("unexpected source-library write")
	}
	tx.calls = append(tx.calls, "place")
	if tx.places == nil {
		tx.places = map[string][]byte{}
	}
	tx.places[args[0].(string)+":"+args[1].(string)] = append([]byte(nil), args[2].([]byte)...)
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}
func TestExternalPlaceCacheNativeWriterDeduplicatesAndStoresNoPrivateFields(t *testing.T) {
	q, v, at := externalCacheFixture()
	pub, e := publicExternalCacheSearch(q, v, at)
	if e != nil {
		t.Fatal(e)
	}
	tx := &externalCacheUnitTx{now: at}
	for i := int64(1); i <= 2; i++ {
		got, e := storePublicExternalCacheTx(context.Background(), tx, q, pub)
		if e != nil || got.CacheVersion != i {
			t.Fatal(got, e)
		}
	}
	if len(tx.places) != 1 || !reflect.DeepEqual(tx.calls, []string{"city", "place", "batch", "clock", "city", "place", "batch", "clock"}) {
		t.Fatal("identity or native write order changed", tx.calls, tx.places)
	}
	for _, raw := range tx.places {
		var p agentworkspace.ExternalPlaceCandidate
		if json.Unmarshal(raw, &p) != nil || p.CityID != "" || p.ID != "osm:node:101" {
			t.Fatal("library global identity retained query city association")
		}
	}
	if strings.Contains(string(tx.batch), "PRIVATE_") || strings.Contains(string(tx.batch), v.Query) {
		t.Fatal("public SQL payload retained private association")
	}
	blocked := &externalCacheUnitTx{now: at, cityError: pgx.ErrNoRows}
	if _, e := storePublicExternalCacheTx(context.Background(), blocked, q, pub); e == nil || len(blocked.places) != 0 || blocked.batch != nil {
		t.Fatal("hidden catalog city accepted cache writes")
	}
}
