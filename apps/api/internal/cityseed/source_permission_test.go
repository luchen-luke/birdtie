package cityseed

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func permissionUnitInput(now time.Time) SubmitInput {
	un := now.Add(24 * time.Hour)
	return SubmitInput{Name: "SYNTHETIC licensed place", CategoryCode: "restaurant", Summary: "Synthetic unit fixture, not a real business", SourceLabel: "Birdtie unit source", SourceURL: "https://example.invalid/first-party-place", RightsNote: "Synthetic rights declaration for isolated test only", LocationPrecision: "none", ExpiresAt: un, Rights: &SourcePermissionInput{State: PermissionConfirmed, Acquisition: "FIRST_PARTY", OriginProvider: "BIRDTIE_FIRST_PARTY", LicenseURL: "https://example.invalid/license", LicenseVersion: "synthetic-v1", Attribution: "Synthetic Birdtie fixture", AcquiredAt: now.Add(-time.Minute), RetentionUntil: un}, Fields: []PlaceFieldInput{{Key: "name", Value: json.RawMessage(`"SYNTHETIC licensed place"`), ObservedAt: now.Add(-time.Minute), ValidFrom: now.Add(-time.Minute), ValidTo: un, RefreshDue: un, ConflictState: "NONE"}}}
}
func TestPlaceSourcePermissionRequiresActualStructuredRights(t *testing.T) {
	now := time.Now().UTC()
	in := permissionUnitInput(now)
	if _, fields, e := NormalizeSourcePermission(in, now); e != nil || len(fields) != 1 {
		t.Fatal("permitted synthetic source rejected", e)
	}
	for _, change := range []func(*SubmitInput){
		func(i *SubmitInput) { i.Rights = nil }, func(i *SubmitInput) { i.Rights.State = PermissionPending }, func(i *SubmitInput) { i.Rights.State = PermissionProhibited },
		func(i *SubmitInput) { i.Rights.Acquisition = "SEARCH_REWRITE" }, func(i *SubmitInput) { i.Rights.OriginProvider = "UNKNOWN" }, func(i *SubmitInput) { i.Rights.LicenseURL = "" }, func(i *SubmitInput) { i.Rights.LicenseVersion = "UNKNOWN" }, func(i *SubmitInput) { i.Rights.Attribution = "" },
		func(i *SubmitInput) { i.Rights.RetentionUntil = now }, func(i *SubmitInput) { i.Rights.RetentionUntil = i.ExpiresAt.Add(-time.Minute) }, func(i *SubmitInput) { i.Rights.AcquiredAt = now.Add(time.Minute) }, func(i *SubmitInput) { i.Fields = nil },
		func(i *SubmitInput) { i.Fields[0].Key = "sensitive_identity" }, func(i *SubmitInput) { i.Fields[0].Value = json.RawMessage(`"different place"`) }, func(i *SubmitInput) { i.Fields[0].ValidTo = now }, func(i *SubmitInput) { i.Fields = append(i.Fields, i.Fields[0]) },
	} {
		bad := permissionUnitInput(now)
		change(&bad)
		if _, _, e := NormalizeSourcePermission(bad, now); !errors.Is(e, ErrSourcePermission) {
			t.Fatal("unknown/prohibited/missing rights accepted", e)
		}
	}
}
func TestPlaceSourcePermissionRestrictedOriginCannotBeRelabeled(t *testing.T) {
	now := time.Now().UTC()
	for _, origin := range []string{"tencent_wsa", "TENCENT-WSA", "SearchPro", "WSA", "tencent_maas", "ten\u200bcent_wsa"} {
		in := permissionUnitInput(now)
		in.ProviderCode = origin
		if _, _, e := NormalizeSourcePermission(in, now); !errors.Is(e, ErrSourcePermission) {
			t.Fatalf("restricted origin bypassed as first party: %q", origin)
		}
	}
	for _, url := range []string{"https://tokenhub.tencentmaas.com/v1", "https://wsa.tencentcloudapi.com/search", "https://searchpro.tencentcloudapi.com/search", "https://api.tencentmaas.com/search"} {
		in := permissionUnitInput(now)
		in.SourceURL = url
		if _, _, e := NormalizeSourcePermission(in, now); !errors.Is(e, ErrSourcePermission) {
			t.Fatalf("restricted URL bypassed: %q", url)
		}
	}
	for _, acq := range []string{"DIRECT_AUTHORIZATION", "OPEN_LICENSE"} {
		in := permissionUnitInput(now)
		in.Rights.Acquisition = acq
		in.Rights.OriginProvider = "DIRECT_SOURCE"
		if _, _, e := NormalizeSourcePermission(in, now); e != nil {
			t.Fatal("allowed independently reviewable provenance rejected", e)
		}
	}
}
func TestPlaceSourcePermissionFieldFactsAreBoundedAndSeparate(t *testing.T) {
	for _, tt := range []struct {
		key, raw string
		want     bool
	}{{"food_seafood", "true", true}, {"food_spicy", "false", true}, {"food_spicy", `"unknown"`, false}, {"food_seafood", "null", false}, {"opening_hours", `"Hours require current independent evidence"`, true}, {"coordinates", `{"coordinateSystem":"wgs84","latitude":57.14,"longitude":-2.09}`, true}, {"coordinates", `{"coordinateSystem":"gcj02","latitude":57.14,"longitude":-2.09}`, false}, {"coordinates", `{"coordinateSystem":"wgs84","latitude":97,"longitude":-2.09}`, false}, {"coordinates", `{"coordinateSystem":"wgs84","latitude":57.14,"longitude":-2.09,"extra":"private"}`, false}} {
		if got := ValidPlaceFieldValue(tt.key, json.RawMessage(tt.raw)); got != tt.want {
			t.Fatalf("%s %s %t", tt.key, tt.raw, got)
		}
	}
	now := time.Now().UTC()
	in := permissionUnitInput(now)
	in.Fields = append(in.Fields, PlaceFieldInput{Key: "opening_hours", Value: json.RawMessage(`"Synthetic hours, stale"`), ObservedAt: now.Add(-time.Hour), ValidFrom: now.Add(-time.Hour), ValidTo: in.ExpiresAt, RefreshDue: now.Add(-time.Minute), ConflictState: "UNRESOLVED"})
	if _, fields, e := NormalizeSourcePermission(in, now); e != nil || fields[1].ConflictState != "UNRESOLVED" || !fields[1].RefreshDue.Before(now) {
		t.Fatal("field freshness incorrectly promoted", e)
	}
}
func TestPlaceSourcePermissionNeedsIndependentReviewAndCurrentRetention(t *testing.T) {
	now := time.Now().UTC()
	reviewed := now.Add(-time.Minute)
	submitter, reviewer := "submitter", "independent reviewer"
	p := SourcePermission{Version: 1, SourcePermissionInput: permissionUnitInput(now).RightsValue(), DeclaredBy: submitter, ReviewedBy: &reviewer, ReviewedAt: &reviewed}
	if !PermissionUsable(p, now) {
		t.Fatal("current independently reviewed permission rejected")
	}
	for _, change := range []func(*SourcePermission){func(p *SourcePermission) { p.ReviewedBy = nil }, func(p *SourcePermission) { p.ReviewedBy = &submitter }, func(p *SourcePermission) { p.State = PermissionPending }, func(p *SourcePermission) { p.State = PermissionProhibited }, func(p *SourcePermission) { p.RetentionUntil = now }, func(p *SourcePermission) { p.RevokedAt = &reviewed }} {
		bad := p
		change(&bad)
		if PermissionUsable(bad, now) {
			t.Fatal("invalid permission used")
		}
	}
}
func (in SubmitInput) RightsValue() SourcePermissionInput { return *in.Rights }
