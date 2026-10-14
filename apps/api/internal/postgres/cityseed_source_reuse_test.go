package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/cityseed"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const sourceReuseTestPlaceID = "35d701af-9256-493d-afda-b8789f91f921"

// Synthetic pure-function/transaction doubles exercise native SQL predicates.
// No provider, real database or retained candidate is written by these tests.
func sourceReuseTestFacts() (foundation.Place, cityseed.SubmitInput, []cityseed.PlaceFieldEvidence, time.Time) {
	place, fields, now := localRecommendationTestFacts(1)
	place.ID, place.CityID = sourceReuseTestPlaceID, "aberdeen-gb"
	place.CategoryCode = "restaurant"
	updated, expires := now.Add(-5*time.Minute), now.Add(2*time.Hour)
	place.Source.UpdatedAt, place.Source.VerifiedAt, place.Source.ExpiresAt = updated, &updated, &expires
	rights := fields[0].Permission.SourcePermissionInput
	rights.AcquiredAt, rights.RetentionUntil = now, now.Add(24*time.Hour)
	input := cityseed.SubmitInput{
		Name: place.Name, CategoryCode: "restaurant", AddressLabel: place.AddressLabel,
		Latitude: place.Location.Latitude, Longitude: place.Location.Longitude, LocationPrecision: place.Location.Precision,
		SourceURL: place.Source.Reference, ProviderCode: "openstreetmap", ProviderPlaceID: "node/290633294", Rights: &rights,
		ExpiresAt: now.Add(24 * time.Hour),
	}
	for i := range fields {
		fields[i].PlaceID = place.ID
		input.Fields = append(input.Fields, cityseed.PlaceFieldInput{
			Key: fields[i].FieldKey, Value: append(json.RawMessage(nil), fields[i].Value...), ConflictState: "NONE",
			ObservedAt: now, ValidFrom: now, ValidTo: now.Add(24 * time.Hour), RefreshDue: now.Add(24 * time.Hour),
		})
	}
	return place, input, fields, now
}

func TestCitySeedSourceReuseKeepsOriginalDeadlineAndSemanticallyEqualFacts(t *testing.T) {
	place, input, fields, now := sourceReuseTestFacts()
	input.Fields[2].Value = json.RawMessage(`{"longitude":-2.1,"latitude":57.150,"coordinateSystem":"wgs84"}`)
	until, same := sameCurrentCitySeedSourceFacts(place, input, fields, now)
	if !same || !until.Equal(now.Add(30*time.Minute)) || !until.Before(input.ExpiresAt) {
		t.Fatal("same source facts were not reused with their original earliest deadline", same, until)
	}
	// New acquisition time and TTL are not written back or used as authority.
	if fields[0].Permission.RetentionUntil.Equal(input.ExpiresAt) || fields[0].ObservedAt.Equal(input.Fields[0].ObservedAt) {
		t.Fatal("newly acquired times replaced the independently reviewed original facts")
	}
	before, _ := json.Marshal(fields)
	_, _ = sameCurrentCitySeedSourceFacts(place, input, fields, now)
	after, _ := json.Marshal(fields)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("reuse mutated the original source/field records")
	}
}

func TestCitySeedSourceReuseCurrentDuplicatePreservesOriginalContractAndDistinguishesPending(t *testing.T) {
	if !errors.Is(errCurrentCitySeedSourceDuplicate, cityseed.ErrDuplicate) || !errors.Is(errCurrentCitySeedSourceDuplicate, cityseed.ErrSourceAlreadyCurrent) {
		t.Fatal("published current reuse lost duplicate compatibility or its precise discriminator")
	}
	if errors.Is(cityseed.ErrDuplicate, cityseed.ErrSourceAlreadyCurrent) || errors.Is(seedWriteError(errors.New("test native failure")), cityseed.ErrSourceAlreadyCurrent) {
		t.Fatal("ordinary pending conflict/native failure was mistaken for current published reuse")
	}
	pending := seedWriteError(&pgconn.PgError{Code: "23505"})
	if !errors.Is(pending, cityseed.ErrDuplicate) || errors.Is(pending, cityseed.ErrSourceAlreadyCurrent) {
		t.Fatal("existing pending unique conflict acquired a false published-reuse discriminator")
	}
}

func TestCitySeedSourceReuseChangedUnknownStaleOrUnpermittedFactsNeedOriginalReview(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*foundation.Place, *cityseed.SubmitInput, *[]cityseed.PlaceFieldEvidence, time.Time)
	}{
		{"changed name", func(_ *foundation.Place, in *cityseed.SubmitInput, _ *[]cityseed.PlaceFieldEvidence, _ time.Time) {
			in.Name = "Changed place"
		}},
		{"changed category", func(_ *foundation.Place, in *cityseed.SubmitInput, _ *[]cityseed.PlaceFieldEvidence, _ time.Time) {
			in.CategoryCode = "cafe"
		}},
		{"changed address", func(_ *foundation.Place, in *cityseed.SubmitInput, _ *[]cityseed.PlaceFieldEvidence, _ time.Time) {
			in.Fields[1].Value = json.RawMessage(`"Changed address"`)
		}},
		{"changed coordinates", func(_ *foundation.Place, in *cityseed.SubmitInput, _ *[]cityseed.PlaceFieldEvidence, _ time.Time) {
			lng := -2.11
			in.Longitude = &lng
		}},
		{"new hours field", func(_ *foundation.Place, in *cityseed.SubmitInput, _ *[]cityseed.PlaceFieldEvidence, _ time.Time) {
			in.Fields = append(in.Fields, cityseed.PlaceFieldInput{Key: "opening_hours", Value: json.RawMessage(`"Mo-Su 09:00-22:30"`), ConflictState: "NONE"})
		}},
		{"missing identity evidence", func(_ *foundation.Place, _ *cityseed.SubmitInput, f *[]cityseed.PlaceFieldEvidence, _ time.Time) {
			*f = (*f)[1:]
		}},
		{"different source", func(_ *foundation.Place, in *cityseed.SubmitInput, _ *[]cityseed.PlaceFieldEvidence, _ time.Time) {
			in.SourceURL = "https://www.openstreetmap.org/node/2"
		}},
		{"changed license", func(_ *foundation.Place, in *cityseed.SubmitInput, _ *[]cityseed.PlaceFieldEvidence, _ time.Time) {
			in.Rights.LicenseVersion = "Changed licence"
		}},
		{"different field source", func(_ *foundation.Place, _ *cityseed.SubmitInput, f *[]cityseed.PlaceFieldEvidence, _ time.Time) {
			(*f)[1].Permission.SourceURL = "https://www.openstreetmap.org/node/2"
		}},
		{"no rights", func(_ *foundation.Place, in *cityseed.SubmitInput, _ *[]cityseed.PlaceFieldEvidence, _ time.Time) {
			in.Rights = nil
		}},
		{"pending original rights", func(_ *foundation.Place, _ *cityseed.SubmitInput, f *[]cityseed.PlaceFieldEvidence, _ time.Time) {
			(*f)[1].Permission.State = cityseed.PermissionPending
		}},
		{"revoked original rights", func(_ *foundation.Place, _ *cityseed.SubmitInput, f *[]cityseed.PlaceFieldEvidence, now time.Time) {
			(*f)[1].Permission.RevokedAt = &now
		}},
		{"no independent review", func(_ *foundation.Place, _ *cityseed.SubmitInput, f *[]cityseed.PlaceFieldEvidence, _ time.Time) {
			(*f)[1].Permission.ReviewedBy = nil
		}},
		{"permission version mismatch", func(_ *foundation.Place, _ *cityseed.SubmitInput, f *[]cityseed.PlaceFieldEvidence, _ time.Time) {
			(*f)[1].Permission.Version++
		}},
		{"permission id mismatch", func(_ *foundation.Place, _ *cityseed.SubmitInput, f *[]cityseed.PlaceFieldEvidence, _ time.Time) {
			(*f)[1].PermissionID = "another-grant"
		}},
		{"different native place", func(_ *foundation.Place, _ *cityseed.SubmitInput, f *[]cityseed.PlaceFieldEvidence, _ time.Time) {
			(*f)[1].PlaceID = "another-place"
		}},
		{"missing native version token", func(_ *foundation.Place, _ *cityseed.SubmitInput, f *[]cityseed.PlaceFieldEvidence, _ time.Time) {
			(*f)[1].RowToken = ""
		}},
		{"stale source field", func(_ *foundation.Place, _ *cityseed.SubmitInput, f *[]cityseed.PlaceFieldEvidence, now time.Time) {
			(*f)[1].RefreshDue = now
		}},
		{"expired field", func(_ *foundation.Place, _ *cityseed.SubmitInput, f *[]cityseed.PlaceFieldEvidence, now time.Time) {
			(*f)[1].ValidTo = now
		}},
		{"expired permission", func(_ *foundation.Place, _ *cityseed.SubmitInput, f *[]cityseed.PlaceFieldEvidence, now time.Time) {
			(*f)[1].Permission.RetentionUntil = now
		}},
		{"expired canonical place", func(p *foundation.Place, _ *cityseed.SubmitInput, _ *[]cityseed.PlaceFieldEvidence, now time.Time) {
			p.Source.ExpiresAt = &now
		}},
		{"conflicting incoming field", func(_ *foundation.Place, in *cityseed.SubmitInput, _ *[]cityseed.PlaceFieldEvidence, _ time.Time) {
			in.Fields[1].ConflictState = "UNRESOLVED"
		}},
		{"duplicate incoming key", func(_ *foundation.Place, in *cityseed.SubmitInput, _ *[]cityseed.PlaceFieldEvidence, _ time.Time) {
			in.Fields = append(in.Fields, in.Fields[0])
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			place, input, fields, now := sourceReuseTestFacts()
			test.edit(&place, &input, &fields, now)
			if _, same := sameCurrentCitySeedSourceFacts(place, input, fields, now); same {
				t.Fatal("changed/unknown/invalid field bypassed the original pending review")
			}
		})
	}
}

func TestCitySeedSourceReuseMissingSameSourceHoursRequiresReviewButOtherSourceDoesNotCountAsDeletion(t *testing.T) {
	place, input, fields, now := sourceReuseTestFacts()
	hours := fields[0]
	hours.ID, hours.FieldKey, hours.Value = "test-original-hours", "opening_hours", json.RawMessage(`"Mo-Su 09:00-22:30"`)
	withHours := append(append([]cityseed.PlaceFieldEvidence(nil), fields...), hours)
	if _, same := sameCurrentCitySeedSourceFacts(place, input, withHours, now); same {
		t.Fatal("same-source current hours omitted from the new observation were called unchanged")
	}
	input.Fields = append(input.Fields, cityseed.PlaceFieldInput{Key: "opening_hours", Value: hours.Value, ConflictState: "NONE"})
	if _, same := sameCurrentCitySeedSourceFacts(place, input, withHours, now); !same {
		t.Fatal("complete unchanged same-source hours could not be reused")
	}
	input.Fields = input.Fields[:len(input.Fields)-1]
	hours.Permission.SourceURL = "https://restaurant.co.uk/opening-hours"
	otherHours := append(append([]cityseed.PlaceFieldEvidence(nil), fields...), hours)
	if _, same := sameCurrentCitySeedSourceFacts(place, input, otherHours, now); !same {
		t.Fatal("another source's additional hours were treated as a deletion by this provider")
	}
}

type sourceReuseUnitRows struct {
	pgx.Rows
	values [][]any
	index  int
}

func (r *sourceReuseUnitRows) Next() bool { r.index++; return r.index <= len(r.values) }
func (r *sourceReuseUnitRows) Scan(v ...any) error {
	return (currentReadonlyUnitRow{values: r.values[r.index-1]}).Scan(v...)
}
func (r *sourceReuseUnitRows) Close()     {}
func (r *sourceReuseUnitRows) Err() error { return nil }

func sourceReuseUnitFieldRow(f cityseed.PlaceFieldEvidence) []any {
	p := f.Permission
	return []any{f.ID, f.PlaceID, f.SourceCandidateID, f.FieldKey, f.Value, f.ObservedAt, f.VerifiedAt, f.ValidFrom, f.ValidTo, f.RefreshDue, f.PermissionID, f.PermissionVersion, f.Version, f.ConflictState,
		p.ID, p.CandidateID, p.Version, p.State, p.Acquisition, p.OriginProvider, p.SourceURL, p.LicenseURL, p.LicenseVersion, p.Attribution, p.AcquiredAt, p.RetentionUntil, p.DeclaredBy, p.ReviewedBy, p.ReviewedAt, p.RevokedAt, p.CreatedAt, p.UpdatedAt, f.RowToken}
}

type sourceReuseUnitTx struct {
	pgx.Tx
	place     foundation.Place
	fields    []cityseed.PlaceFieldEvidence
	queries   []string
	args      [][]any
	boundCity string
	missing   bool
	failure   error
	cityUntil *time.Time
}

func (x *sourceReuseUnitTx) QueryRow(_ context.Context, q string, args ...any) pgx.Row {
	x.queries = append(x.queries, q)
	x.args = append(x.args, args)
	if x.failure != nil {
		return currentReadonlyUnitRow{err: x.failure}
	}
	if strings.Contains(q, "FROM place_external_refs r") {
		if x.missing {
			return currentReadonlyUnitRow{err: pgx.ErrNoRows}
		}
		city := x.place.CityID
		if x.boundCity != "" {
			city = x.boundCity
		}
		return currentReadonlyUnitRow{values: []any{x.place.ID, city, x.place.Source.Reference}}
	}
	if strings.HasPrefix(q, "SELECT expires_at FROM cities") {
		return currentReadonlyUnitRow{values: []any{x.cityUntil}}
	}
	p := x.place
	return currentReadonlyUnitRow{values: []any{p.ID, p.CityID, p.Name, p.CategoryCode, p.Summary, p.AddressLabel,
		p.Location.CoordinateSystem, p.Location.Precision, p.Location.Latitude, p.Location.Longitude,
		p.Source.Label, p.Source.Reference, p.Source.Maintainer, p.Source.UpdatedAt, p.Source.VerifiedAt, p.Source.ExpiresAt}}
}
func (x *sourceReuseUnitTx) Query(_ context.Context, q string, args ...any) (pgx.Rows, error) {
	x.queries = append(x.queries, q)
	x.args = append(x.args, args)
	rows := &sourceReuseUnitRows{}
	for _, f := range x.fields {
		rows.values = append(rows.values, sourceReuseUnitFieldRow(f))
	}
	return rows, nil
}

func TestCitySeedSourceReuseNativeSQLLocksOriginalIdentityAndCurrentRightsBeforePendingIndex(t *testing.T) {
	p, input, fields, now := sourceReuseTestFacts()
	cityDeadline := now.Add(10 * time.Minute)
	x := &sourceReuseUnitTx{place: p, fields: fields, cityUntil: &cityDeadline}
	proof, err := currentCitySeedSourceReuseTx(context.Background(), x, p.CityID, input, now)
	if err != nil || proof.placeID != p.ID || !proof.validUntil.Equal(cityDeadline) || len(x.queries) != 4 {
		t.Fatal("native published reuse proof was not captured", proof, err, len(x.queries))
	}
	if !reflect.DeepEqual(x.args[0], []any{input.ProviderCode, input.ProviderPlaceID}) || !strings.Contains(x.queries[0], "FOR SHARE OF r,p") {
		t.Fatal("provider/node identity was replaced by name lookup or left unlocked")
	}
	for _, predicate := range []string{"p.city_id=$2", "p.source_ref=$3", "p.publication_status='published'", "city.publication_status='published'", "p.expires_at>clock_timestamp()", "rights.revoked_at IS NULL", "rights.version=f.permission_version", "FOR SHARE OF p,city"} {
		if !strings.Contains(x.queries[1], predicate) {
			t.Fatal("canonical native visibility boundary missing", predicate)
		}
	}
	for _, predicate := range []string{"p.state='CONFIRMED'", "p.revoked_at IS NULL", "p.version=f.permission_version", "p.reviewed_by<>p.declared_by", "f.refresh_due>clock_timestamp()", "f.conflict_state='NONE'", "FOR SHARE OF f,p,c,pl,city"} {
		if !strings.Contains(x.queries[2], predicate) {
			t.Fatal("original independently reviewed field boundary missing", predicate)
		}
	}
	if !reflect.DeepEqual(x.args[2][1], []string{"name", "address", "coordinates", "food_seafood", "food_spicy", "opening_hours"}) {
		t.Fatal("native comparison read only incoming keys and could miss a deleted source field", x.args[2])
	}
	for _, q := range x.queries {
		if strings.Contains(q, "INSERT") || strings.Contains(q, "UPDATE") || strings.Contains(q, "status = 'pending'") {
			t.Fatal("published reuse consulted pending uniqueness or wrote/renewed source records", q)
		}
	}
}

func TestCitySeedSourceReuseNativeReadsOriginalHoursEvenWhenIncomingOnlyContainsIdentity(t *testing.T) {
	p, input, fields, now := sourceReuseTestFacts()
	hours := fields[0]
	hours.ID, hours.FieldKey, hours.Value = "test-original-hours", "opening_hours", json.RawMessage(`"Mo-Su 09:00-22:30"`)
	x := &sourceReuseUnitTx{place: p, fields: append(fields, hours)}
	proof, err := currentCitySeedSourceReuseTx(context.Background(), x, p.CityID, input, now)
	if err != nil || proof.placeID != "" || len(x.queries) != 3 {
		t.Fatal("original same-source current hours were hidden by the incoming input keyset", proof, err, len(x.queries))
	}
	if !reflect.DeepEqual(x.args[2][1], []string{"name", "address", "coordinates", "food_seafood", "food_spicy", "opening_hours"}) {
		t.Fatal("original source's full supported fields were not read", x.args[2])
	}
}

func TestCitySeedSourceReuseNativeConflictFailureAndMissingDoNotBecomeFalseReuse(t *testing.T) {
	p, input, fields, now := sourceReuseTestFacts()
	for _, test := range []struct {
		name string
		tx   sourceReuseUnitTx
		err  error
	}{
		{"different city", sourceReuseUnitTx{place: p, boundCity: "edinburgh-gb"}, cityseed.ErrConflict},
		{"failed lookup", sourceReuseUnitTx{place: p, failure: errors.New("synthetic native lookup error")}, errors.New("synthetic native lookup error")},
		{"no external identity", sourceReuseUnitTx{place: p, missing: true}, nil},
		{"missing original facts", sourceReuseUnitTx{place: p}, nil},
		{"changed facts", sourceReuseUnitTx{place: p, fields: fields[1:]}, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			proof, err := currentCitySeedSourceReuseTx(context.Background(), &test.tx, p.CityID, input, now)
			if proof.placeID != "" || (test.err == nil && err != nil) || (test.err != nil && (err == nil || err.Error() != test.err.Error())) {
				t.Fatal("conflict/error/unknown was reported as current published reuse", proof, err)
			}
		})
	}
	input.ProviderCode = ""
	x := &sourceReuseUnitTx{}
	if proof, err := currentCitySeedSourceReuseTx(context.Background(), x, p.CityID, input, now); err != nil || proof.placeID != "" || len(x.queries) != 0 {
		t.Fatal("provider-less existing route unexpectedly dispatched reuse SQL")
	}
}

func TestCitySeedSourceReuseSubmitOrderRetainsNativeACLAndFinalFenceBeforeAnyNewContentWrite(t *testing.T) {
	source, err := os.ReadFile("cityseed.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "cityseed.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	var calls []string
	for _, declaration := range file.Decls {
		fn, ok := declaration.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "Submit" {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch f := call.Fun.(type) {
			case *ast.Ident:
				calls = append(calls, f.Name)
			case *ast.SelectorExpr:
				calls = append(calls, f.Sel.Name)
			}
			return true
		})
	}
	index := func(name string) int {
		for i, call := range calls {
			if call == name {
				return i
			}
		}
		return -1
	}
	if !(index("lockCitySeedHumanTx") >= 0 && index("lockCitySeedHumanTx") < index("currentCitySeedSourceReuseTx") &&
		index("currentCitySeedSourceReuseTx") < index("finalCitySeedSessionTx") && index("finalCitySeedSessionTx") < index("insertCandidatePermissionTx")) {
		t.Fatal("Submit reuse bypassed native ACL/final-session or happened after content insertion", calls)
	}
	text := string(source)
	for _, fence := range []string{"!reused.validUntil.After(nativeNow)", "ctx.Err() != nil", "return cityseed.Candidate{}, errCurrentCitySeedSourceDuplicate"} {
		if !strings.Contains(text, fence) {
			t.Fatal("published duplicate final expiry fence or original error contract missing", fence)
		}
	}
}
