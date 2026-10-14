package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/cityseed"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"github.com/jackc/pgx/v5"
)

// Synthetic permission rows only. No URL fetch, candidate write or review.
func TestLicensedPlaceFieldsRequireExactReviewedOpenLicence(t *testing.T) {
	fixture := func() (foundation.Place, []cityseed.PlaceFieldEvidence) {
		p, f, _ := localRecommendationTestFacts(1)
		for i := range f {
			f[i].Permission.OriginProvider = "OPEN_DATA_DOWNLOAD"
			f[i].Permission.LicenseVersion = "ODbL-1.0"
		}
		return p, f
	}
	_, _, now := localRecommendationTestFacts(1)
	p, f := fixture()
	if !licensedPlaceFieldsAllowed(p, f, now) {
		t.Fatal("reviewed open facts rejected")
	}
	edits := map[string]func([]cityseed.PlaceFieldEvidence){
		"unknown_model_rights":                     func(f []cityseed.PlaceFieldEvidence) { f[0].Permission.LicenseVersion = "Another-license" },
		"direct_authorization_no_processing_scope": func(f []cityseed.PlaceFieldEvidence) { f[0].Permission.Acquisition = "DIRECT_AUTHORIZATION" },
		"wsa_origin":      func(f []cityseed.PlaceFieldEvidence) { f[0].Permission.OriginProvider = "tencent_wsa" },
		"pending":         func(f []cityseed.PlaceFieldEvidence) { f[0].Permission.State = cityseed.PermissionPending },
		"unreviewed":      func(f []cityseed.PlaceFieldEvidence) { f[0].Permission.ReviewedAt = nil },
		"withdrawn":       func(f []cityseed.PlaceFieldEvidence) { f[0].Permission.RevokedAt = &now },
		"expired":         func(f []cityseed.PlaceFieldEvidence) { f[0].ValidTo = now },
		"conflict":        func(f []cityseed.PlaceFieldEvidence) { f[0].ConflictState = "UNRESOLVED" },
		"changed_version": func(f []cityseed.PlaceFieldEvidence) { f[0].Permission.Version++ },
		"wrong_canonical": func(f []cityseed.PlaceFieldEvidence) { f[0].Value = json.RawMessage(`"other"`) },
	}
	for name, edit := range edits {
		t.Run(name, func(t *testing.T) {
			p, f := fixture()
			edit(f)
			if licensedPlaceFieldsAllowed(p, f, now) {
				t.Fatal("unsafe field passed model processing boundary")
			}
		})
	}
}

// Read-only SQL doubles; these synthetic fields never enter an intake or DB.
type licensedFieldUnitTx struct {
	sourceReuseUnitTx
	cityRaw, placeRaw         []byte
	missingCity, missingPlace bool
}

func (x *licensedFieldUnitTx) QueryRow(ctx context.Context, q string, args ...any) pgx.Row {
	if strings.Contains(q, "SELECT c.id,c.name,c.country_code") {
		x.queries = append(x.queries, q)
		if x.missingCity {
			return currentReadonlyUnitRow{err: pgx.ErrNoRows}
		}
		return currentReadonlyUnitRow{values: []any{x.place.CityID, "Aberdeen", "GB", x.cityUntil, x.cityRaw}}
	}
	if strings.Contains(q, "SELECT p.id,p.name,p.expires_at") {
		x.queries = append(x.queries, q)
		if x.missingPlace {
			return currentReadonlyUnitRow{err: pgx.ErrNoRows}
		}
		return currentReadonlyUnitRow{values: []any{x.place.ID, x.place.Name, x.place.Source.ExpiresAt, x.placeRaw}}
	}
	return x.sourceReuseUnitTx.QueryRow(ctx, q, args...)
}
func licensedFieldFixture(at time.Time) (*licensedFieldUnitTx, licensedPlaceBatchData) {
	p, f, _ := localRecommendationTestFacts(1)
	p.ID, p.CityID = sourceReuseTestPlaceID, "aberdeen-gb"
	verified, placeUntil, cityUntil := at.Add(-time.Minute), at.Add(10*time.Second), at.Add(15*time.Second)
	p.Source.UpdatedAt, p.Source.VerifiedAt, p.Source.ExpiresAt = verified, &verified, &placeUntil
	for i := range f {
		f[i].ID = fmt.Sprintf("00000000-0000-4000-8000-%012d", i+1)
		f[i].PlaceID = p.ID
		f[i].PermissionID = "00000000-0000-4000-8000-000000000101"
		f[i].Permission.ID = f[i].PermissionID
		f[i].ObservedAt, f[i].VerifiedAt, f[i].ValidFrom = at.Add(-2*time.Minute), verified, at.Add(-2*time.Minute)
		f[i].ValidTo, f[i].RefreshDue = at.Add(time.Hour), at.Add(30*time.Minute)
		f[i].Permission.OriginProvider, f[i].Permission.LicenseVersion = "OPEN_DATA_DOWNLOAD", "ODbL-1.0"
		f[i].Permission.AcquiredAt, f[i].Permission.RetentionUntil = at.Add(-time.Hour), at.Add(2*time.Hour)
		f[i].Permission.ReviewedAt = &verified
	}
	cityRaw, _ := json.Marshal(map[string]any{"city": p.CityID, "name": "Aberdeen", "country": "GB", "expiry": cityUntil, "context": "test-only-context", "cityRow": "test-only-1"})
	placeRaw, _ := json.Marshal(map[string]any{"id": p.ID, "name": p.Name, "expiry": placeUntil, "row": "test-only-2"})
	x := &licensedFieldUnitTx{sourceReuseUnitTx: sourceReuseUnitTx{place: p, fields: f, cityUntil: &cityUntil}, cityRaw: cityRaw, placeRaw: placeRaw}
	sources := []modelegressbudget.LivePublicSource{}
	place, _ := licensedPublicPlaceProjection(p, f, &sources)
	d := licensedPlaceBatchData{Schema: licensedPlaceBatchSchema, Context: modelegressbudget.LiveLicensedPlaceContext{Schema: modelegressbudget.LiveLicensedPlaceContextSchema, City: modelegressbudget.LiveSelectedCity{ID: p.CityID, Name: "Aberdeen", CountryCode: "GB"}, Slots: modelegressbudget.LiveResolvedSlots{Operation: "FIND_PLACE", Target: "FIND_PLACE"}, Places: []modelegressbudget.LiveLicensedPlace{place}, Sources: sources}, Places: []licensedPlaceGeneration{{ID: p.ID, Generation: liveDigestBytes("birdtie.native-public-place-followup.v1", placeRaw)}}, CityProof: liveDigestBytes("birdtie.licensed-native-city.v1", cityRaw), FieldProof: localFieldDigest(f, nil), QueryDigest: strings.Repeat("a", 64), DeadlineAt: at.Add(-time.Minute)}
	for _, field := range f {
		d.Facts = append(d.Facts, agentworkspace.LocalPlaceFactRef{EvidenceID: field.ID, PlaceID: p.ID, Field: field.FieldKey, Version: field.Version, PermissionID: field.PermissionID})
	}
	return x, d
}
func TestLicensedPlaceStoredBatchClosedShapeAndWholeEntityProof(t *testing.T) {
	_, original := licensedFieldFixture(time.Now().UTC())
	raw, _ := json.Marshal(original)
	if _, err := readLicensedPlaceBatch(raw); err != nil {
		t.Fatal("valid native shape rejected", err)
	}
	for name, edit := range map[string]func(*licensedPlaceBatchData){
		"dangling_field":     func(d *licensedPlaceBatchData) { d.Facts[0].PlaceID = "00000000-0000-4000-8000-000000000999" },
		"missing_name":       func(d *licensedPlaceBatchData) { d.Facts = d.Facts[1:] },
		"duplicate_evidence": func(d *licensedPlaceBatchData) { d.Facts = append(d.Facts, d.Facts[0]) },
		"generation_missing": func(d *licensedPlaceBatchData) { d.Places[0].Generation = "" },
		"zero_hash":          func(d *licensedPlaceBatchData) { d.CityProof = strings.Repeat("0", 64) },
		"unknown_field":      func(d *licensedPlaceBatchData) { d.Facts[0].Field = "private_memory" },
		"raw_source":         func(d *licensedPlaceBatchData) { d.Context.Sources[0].Passage = "supplier raw" },
	} {
		t.Run(name, func(t *testing.T) {
			var d licensedPlaceBatchData
			_ = json.Unmarshal(raw, &d)
			edit(&d)
			changed, _ := json.Marshal(d)
			if _, err := readLicensedPlaceBatch(changed); err == nil {
				t.Fatal("malformed proof admitted")
			}
		})
	}
	for _, changed := range [][]byte{append(append([]byte(nil), raw...), []byte(` {}`)...), []byte(strings.Replace(string(raw), `"schema":`, `"privateHistory":"secret","schema":`, 1)), []byte(strings.Replace(string(raw), `"sourceNumbers":`, `"memory":"secret","sourceNumbers":`, 1))} {
		if _, err := readLicensedPlaceBatch(changed); err == nil {
			t.Fatal("unknown or trailing data admitted")
		}
	}
}
func TestLicensedPlaceHistoryFreshFactsSurviveConsumedDispatchDeadline(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	x, d := licensedFieldFixture(now)
	fields, until, err := validateLicensedPlaceFieldsTx(context.Background(), x, d, now)
	if err != nil || len(fields) != 3 || !until.Equal(*x.place.Source.ExpiresAt) || !d.DeadlineAt.Before(now) {
		t.Fatal("expired dispatch preview blocked fresh native history or extended original place TTL", until, err)
	}
	for _, q := range x.queries {
		if strings.Contains(q, "INSERT") || strings.Contains(q, "UPDATE ") {
			t.Fatal("history renewed retained state", q)
		}
	}
	if !strings.Contains(strings.Join(x.queries, "\n"), "FOR SHARE OF f,p,c,pl,city") {
		t.Fatal("current field/permission rows not locked")
	}
}
func TestLicensedPlaceBeforeAndAfterModelRevalidationRejectsChangedSource(t *testing.T) {
	for name, edit := range map[string]func(*licensedFieldUnitTx){
		"deleted_city":             func(x *licensedFieldUnitTx) { x.missingCity = true },
		"deleted_place":            func(x *licensedFieldUnitTx) { x.missingPlace = true },
		"changed_city_generation":  func(x *licensedFieldUnitTx) { x.cityRaw = append(x.cityRaw, ' ') },
		"changed_place_generation": func(x *licensedFieldUnitTx) { x.placeRaw = append(x.placeRaw, ' ') },
		"changed_field_version":    func(x *licensedFieldUnitTx) { x.fields[1].Version++ },
		"changed_native_row_token": func(x *licensedFieldUnitTx) { x.fields[1].RowToken = "changed" },
		"changed_field_source": func(x *licensedFieldUnitTx) {
			x.fields[1].Permission.SourceURL = "https://www.openstreetmap.org/node/2"
		},
		"revoked_permission":         func(x *licensedFieldUnitTx) { at := time.Now().UTC(); x.fields[1].Permission.RevokedAt = &at },
		"pending_permission":         func(x *licensedFieldUnitTx) { x.fields[1].Permission.State = cityseed.PermissionPending },
		"changed_permission_version": func(x *licensedFieldUnitTx) { x.fields[1].Permission.Version++ },
		"field_expired_during_model": func(x *licensedFieldUnitTx) { x.fields[1].ValidTo = time.Now().UTC().Add(-time.Second) },
		"refresh_due_during_model":   func(x *licensedFieldUnitTx) { x.fields[1].RefreshDue = time.Now().UTC().Add(-time.Second) },
		"grant_expired_during_model": func(x *licensedFieldUnitTx) {
			x.fields[1].Permission.RetentionUntil = time.Now().UTC().Add(-time.Second)
		},
		"missing_identity_field": func(x *licensedFieldUnitTx) { x.fields = x.fields[1:] },
	} {
		t.Run(name, func(t *testing.T) {
			now := time.Now().UTC()
			x, d := licensedFieldFixture(now)
			edit(x)
			if _, _, err := validateLicensedPlaceFieldsTx(context.Background(), x, d, now); err == nil {
				t.Fatal("changed/withdrawn/expired native source retained authority")
			}
		})
	}
}
func TestLicensedPlaceMigrationPreservesOriginalSourceCallProofAndGuardedDown(t *testing.T) {
	forward, err := os.ReadFile("../../migrations/115_model_egress_licensed_places.sql")
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile("../../migrations/110_model_egress_resolved_public_search.sql")
	if err != nil {
		t.Fatal(err)
	}
	down, err := os.ReadFile("../../migrations/115_model_egress_licensed_places.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	normalize := func(raw []byte) string { return strings.ReplaceAll(string(raw), "\r\n", "\n") }
	f, o, d := normalize(forward), normalize(original), normalize(down)
	start := strings.Index(o, " IF NEW.scope IN ('SELF_TASK_QUERY','SELF_TASK_PUBLIC_RESOLVED_SEARCH')")
	if start < 0 {
		t.Fatal("original source guard not found")
	}
	end := strings.Index(o[start:], "END $$;")
	if end < 0 || !strings.Contains(f, o[start:start+end+len("END $$;")]) {
		t.Fatal("original real CALL reservation/source proof was weakened")
	}
	for _, p := range []string{"p.reviewed_by<>p.declared_by", "p.revoked_at IS NULL", "f.permission_version=p.version", "f.refresh_due>=NEW.expires_at", "pl.expires_at>=NEW.expires_at", "FOR SHARE OF f,p,c,pl,ci", "p.license_version='ODbL-1.0'", "billing_kind='TOKEN'"} {
		if !strings.Contains(f, p) {
			t.Fatal("native source reference boundary absent", p)
		}
	}
	guard := strings.Index(d, "retained licensed-place records prevent rollback")
	if guard < 0 || guard > strings.Index(d, "ALTER TABLE") || strings.Contains(strings.ToUpper(d), "DELETE FROM") || !strings.HasPrefix(d, "BEGIN;") || !strings.HasSuffix(strings.TrimSpace(d), "COMMIT;") {
		t.Fatal("down migration can discard retained facts or is unguarded")
	}
}
func TestLicensedPlaceProjectionExcludesPrivateOrRawSourceMaterial(t *testing.T) {
	p, f, _ := localRecommendationTestFacts(1)
	p.Summary = "PRIVATE_SENTINEL"
	for i := range f {
		f[i].Permission.Attribution = "OpenStreetMap contributors"
	}
	sources := licensedPlaceBatchData{}.Context.Sources
	projection, e := licensedPublicPlaceProjection(p, f, &sources)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(struct {
		Place   any
		Sources any
	}{projection, sources})
	for _, forbidden := range []string{"PRIVATE_SENTINEL", p.ID, f[0].PermissionID, "RowToken", "memory", "passage\":\"Name"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatal("unnecessary native/private material escaped", forbidden)
		}
	}
	if len(sources) != 1 || len(projection.SourceNumbers) != 1 {
		t.Fatal("source URL association lost")
	}
}
