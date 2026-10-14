package content

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

const c147Operation = "10000000-0000-4000-8000-000000000001"
const c147Collection = "20000000-0000-4000-8000-000000000002"
const c147FirstPlace = "30000000-0000-4000-8000-000000000003"
const c147SecondPlace = "40000000-0000-4000-8000-000000000004"

func c147Update() CollectionControlInput {
	return CollectionControlInput{
		SchemaVersion: CollectionControlSchemaVersion, OperationID: c147Operation,
		Action: "UPDATE", Kind: "journey", CollectionID: c147Collection, ExpectedRevision: 7,
		Input: &CollectionControlBody{
			Kind: "journey", CityID: "aberdeen-gb", Title: "我的行程", Summary: "文字 <>& 原样",
			Entries: []CollectionControlEntry{
				{PlaceID: c147FirstPlace, Note: "私人备注", LocationVisibility: "hidden"},
				{PlaceID: c147SecondPlace, Note: "第二站", LocationVisibility: "city"},
			},
		},
	}
}

func c147JSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestCollectionControlNormalizeRetainsOriginalCityAndPrivateInput(t *testing.T) {
	in := c147Update()
	in.OperationID = " " + strings.ToUpper(c147Operation) + " "
	in.CollectionID = strings.ToUpper(c147Collection)
	in.Input.CityID = " aberdeen-gb "
	in.Input.Title = " 我的行程 "
	in.Input.Summary = " 文字 <>& 原样 "
	in.Input.Entries[0].PlaceID = " " + strings.ToUpper(c147FirstPlace) + " "
	in.Input.Entries[0].Note = " 私人备注 "
	original := c147JSON(t, in)
	normalized, err := NormalizeCollectionControl(in)
	if err != nil || !reflect.DeepEqual(normalized, c147Update()) {
		t.Fatal("original collection normalization/city slug not retained", normalized, err)
	}
	if c147JSON(t, in) != original {
		t.Fatal("normalization changed caller material")
	}
	converted := normalized.Input.CollectionInput()
	if _, err := NormalizeCollectionInput(converted); err != nil {
		t.Fatal("canonical legacy input contract rejected normalized control", err)
	}
	for _, entry := range converted.Entries {
		if entry.Available || entry.PlaceName != "" || entry.Latitude != nil || entry.Longitude != nil || entry.SourceExpiresAt != nil {
			t.Fatal("read projection crossed into canonical write input")
		}
	}
	normalized.Input.Entries[0].Note = "new material"
	converted.Entries[1].Note = "other material"
	if c147JSON(t, in) != original || normalized.Input.Entries[1].Note != "第二站" {
		t.Fatal("input conversion or normalization aliases another material slice")
	}
	var body map[string]json.RawMessage
	if json.Unmarshal([]byte(c147JSON(t, c147Update().Input)), &body) != nil || len(body) != 5 {
		t.Fatal("normalized input must have exactly five material fields")
	}
	var entries []map[string]json.RawMessage
	if json.Unmarshal(body["entries"], &entries) != nil || len(entries) != 2 {
		t.Fatal("expected two independent input entries")
	}
	for _, entry := range entries {
		if len(entry) != 3 || entry["placeId"] == nil || entry["note"] == nil || entry["locationVisibility"] == nil {
			t.Fatal("echo contains availability, label, coordinates or source metadata", entry)
		}
	}
	upper := c147Update()
	upper.OperationID, upper.CollectionID, upper.Input.Entries[0].PlaceID = "ABCDEF01-0000-4000-8000-000000000001", "ABCDEF02-0000-4000-8000-000000000002", "ABCDEF03-0000-4000-8000-000000000003"
	canonical, err := NormalizeCollectionControl(upper)
	if err != nil || canonical.OperationID != strings.ToLower(upper.OperationID) || canonical.CollectionID != strings.ToLower(upper.CollectionID) || canonical.Input.Entries[0].PlaceID != strings.ToLower(upper.Input.Entries[0].PlaceID) {
		t.Fatal("original UUID case normalization not retained", err)
	}
}

func TestCollectionControlDigestASCIIWireGoldenAndConcreteVersion(t *testing.T) {
	in := c147Update()
	const updateGolden = "cbeea2b46b2093e9e0e91f8dd7d51a65c7ffa9489d8d4d21bfddd2e957d36ee2"
	const withdrawGolden = "2729fe1072757aa3a9e06b0a0dcc91ee943ced2b5c774ffae3823a1e3c6587ab"
	if got := CollectionControlDigest(in); got != updateGolden {
		t.Fatal("closed ASCII/UTF8 base64url tuple changed", got)
	}
	withdraw := in
	withdraw.Action, withdraw.Input = "WITHDRAW", nil
	if got := CollectionControlDigest(withdraw); got != withdrawGolden {
		t.Fatal("withdraw tuple must include explicit null body", got)
	}
	newOperation := in
	newOperation.OperationID = "50000000-0000-4000-8000-000000000005"
	if CollectionControlDigest(newOperation) != updateGolden {
		t.Fatal("payload digest unexpectedly includes separately approved operation ID")
	}
	changes := map[string]func(*CollectionControlInput){
		"revision":  func(v *CollectionControlInput) { v.ExpectedRevision++ },
		"target":    func(v *CollectionControlInput) { v.CollectionID = c147SecondPlace },
		"kind":      func(v *CollectionControlInput) { v.Kind, v.Input.Kind = "theme_map", "theme_map" },
		"city":      func(v *CollectionControlInput) { v.Input.CityID = "edinburgh-gb" },
		"title":     func(v *CollectionControlInput) { v.Input.Title += "新" },
		"summary":   func(v *CollectionControlInput) { v.Input.Summary += "新" },
		"note":      func(v *CollectionControlInput) { v.Input.Entries[0].Note += "新" },
		"precision": func(v *CollectionControlInput) { v.Input.Entries[0].LocationVisibility = "place" },
		"order": func(v *CollectionControlInput) {
			v.Input.Entries[0], v.Input.Entries[1] = v.Input.Entries[1], v.Input.Entries[0]
		},
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			changed := c147Update()
			change(&changed)
			if digest := CollectionControlDigest(changed); digest == "" || digest == updateGolden {
				t.Fatal("approval payload not bound to concrete original material", digest)
			}
		})
	}
	invalid := in
	invalid.SchemaVersion = "old-schema"
	if CollectionControlDigest(invalid) != "" {
		t.Fatal("invalid material obtained a digest")
	}
}

func TestCollectionControlClosedJSONRejectsAuthorityAndMalformedMaterial(t *testing.T) {
	raw := c147JSON(t, c147Update())
	if got, err := DecodeCollectionControlInput([]byte(raw)); err != nil || !reflect.DeepEqual(got, c147Update()) {
		t.Fatal("valid closed input rejected", err)
	}
	cases := map[string]string{
		"missing schema":      strings.Replace(raw, `"schemaVersion":"`+CollectionControlSchemaVersion+`",`, "", 1),
		"wrong schema":        strings.Replace(raw, CollectionControlSchemaVersion, "v0", 1),
		"unknown outer":       strings.Replace(raw, `"action":`, `"ownerAccountId":"other","action":`, 1),
		"duplicate outer":     strings.Replace(raw, `"action":"UPDATE"`, `"action":"UPDATE","action":"WITHDRAW"`, 1),
		"null selector":       strings.Replace(raw, `"collectionId":"`+c147Collection+`"`, `"collectionId":null`, 1),
		"wrong scalar":        strings.Replace(raw, `"expectedRevision":7`, `"expectedRevision":"7"`, 1),
		"fractional revision": strings.Replace(raw, `"expectedRevision":7`, `"expectedRevision":7.5`, 1),
		"exponent revision":   strings.Replace(raw, `"expectedRevision":7`, `"expectedRevision":7e0`, 1),
		"negative revision":   strings.Replace(raw, `"expectedRevision":7`, `"expectedRevision":-1`, 1),
		"max update":          strings.Replace(raw, `"expectedRevision":7`, `"expectedRevision":9000000000000000`, 1),
		"unsafe revision":     strings.Replace(raw, `"expectedRevision":7`, `"expectedRevision":9007199254740992`, 1),
		"legacy create":       strings.Replace(raw, `"action":"UPDATE"`, `"action":"CREATE"`, 1),
		"missing body":        strings.Replace(raw, `,"input":`+c147JSON(t, c147Update().Input), "", 1),
		"null body":           strings.Replace(raw, `"input":`+c147JSON(t, c147Update().Input), `"input":null`, 1),
		"unknown body":        strings.Replace(raw, `"cityId":`, `"availability":true,"cityId":`, 1),
		"duplicate body":      strings.Replace(raw, `"cityId":"aberdeen-gb"`, `"cityId":"aberdeen-gb","cityId":"other"`, 1),
		"body kind mismatch":  strings.Replace(raw, `"input":{"kind":"journey"`, `"input":{"kind":"theme_map"`, 1),
		"missing summary":     strings.Replace(raw, `,"summary":"文字 \u003c\u003e\u0026 原样"`, "", 1),
		"null summary":        strings.Replace(raw, `"summary":"文字 \u003c\u003e\u0026 原样"`, `"summary":null`, 1),
		"unknown entry":       strings.Replace(raw, `"placeId":`, `"available":false,"placeId":`, 1),
		"source lease entry":  strings.Replace(raw, `"placeId":`, `"sourceExpiresAt":"2026-10-15T00:00:00Z","placeId":`, 1),
		"duplicate entry":     strings.Replace(raw, `"note":"私人备注"`, `"note":"私人备注","note":"other"`, 1),
		"missing entry note":  strings.Replace(raw, `,"note":"私人备注"`, "", 1),
		"null entry":          strings.Replace(raw, c147JSON(t, c147Update().Input.Entries[0]), "null", 1),
		"duplicate place":     strings.Replace(raw, c147SecondPlace, c147FirstPlace, 1),
		"nul text":            strings.Replace(raw, `"note":"私人备注"`, `"note":"\u0000"`, 1),
		"lone high surrogate": strings.Replace(raw, `"note":"私人备注"`, `"note":"\ud800"`, 1),
		"lone low surrogate":  strings.Replace(raw, `"note":"私人备注"`, `"note":"\udc00"`, 1),
		"reversed surrogates": strings.Replace(raw, `"note":"私人备注"`, `"note":"\udc00\ud800"`, 1),
		"trailing document":   raw + `{}`,
		"oversize":            raw + strings.Repeat(" ", MaxCollectionControlRequestBytes),
	}
	for name, bad := range cases {
		t.Run(name, func(t *testing.T) {
			if bad == raw {
				t.Fatal("negative fixture did not change input")
			}
			if _, err := DecodeCollectionControlInput([]byte(bad)); err == nil {
				t.Fatal("nonclosed/invalid input accepted")
			}
		})
	}
	malformedUTF8 := bytes.Replace([]byte(raw), []byte(`"note":"私人备注"`), []byte{'"', 'n', 'o', 't', 'e', '"', ':', '"', 0xff, '"'}, 1)
	if _, err := DecodeCollectionControlInput(malformedUTF8); err == nil {
		t.Fatal("malformed UTF8 repaired and accepted")
	}
	paired := strings.Replace(raw, `"note":"私人备注"`, `"note":"\ud83d\ude00"`, 1)
	if got, err := DecodeCollectionControlInput([]byte(paired)); err != nil || got.Input.Entries[0].Note != "😀" {
		t.Fatal("valid UTF16 pair rejected", err)
	}
	commit := c147JSON(t, CollectionControlSubmission{CollectionControlInput: c147Update(), Approval: "opaque.approval"})
	if _, err := DecodeCollectionControlSubmission([]byte(commit)); err != nil {
		t.Fatal("valid submission rejected", err)
	}
	if _, err := DecodeCollectionControlSubmission([]byte(raw)); err == nil {
		t.Fatal("submission without concrete approval accepted")
	}
	if _, err := DecodeCollectionControlInput([]byte(commit)); err == nil {
		t.Fatal("approval silently accepted at preview port")
	}
	for _, approval := range []string{"", " leading", "trailing ", "a\nb", "a\tb", "\x00", strings.Repeat("a", 8193)} {
		bad := c147JSON(t, CollectionControlSubmission{CollectionControlInput: c147Update(), Approval: approval})
		if _, err := DecodeCollectionControlSubmission([]byte(bad)); err == nil {
			t.Fatal("invalid approval accepted")
		}
	}
}

func TestCollectionControlOriginalLimitsAndTerminalWithdraw(t *testing.T) {
	in := c147Update()
	in.ExpectedRevision = MaxCollectionControlRevision - 1
	if _, err := NormalizeCollectionControl(in); err != nil {
		t.Fatal("last safe UPDATE rejected", err)
	}
	in.ExpectedRevision++
	if _, err := NormalizeCollectionControl(in); err == nil {
		t.Fatal("UPDATE could exceed canonical Web-safe revision")
	}
	in.Action, in.Input = "WITHDRAW", nil
	if _, err := DecodeCollectionControlInput([]byte(c147JSON(t, in))); err != nil {
		t.Fatal("terminal WITHDRAW at maximum is unreachable", err)
	}
	for _, bad := range []string{
		strings.TrimSuffix(c147JSON(t, in), "}") + `,"input":{}}`,
		strings.TrimSuffix(c147JSON(t, in), "}") + `,"input":null}`,
	} {
		if _, err := DecodeCollectionControlInput([]byte(bad)); err == nil {
			t.Fatal("WITHDRAW smuggled an update body")
		}
	}
	changes := map[string]func(*CollectionControlInput){
		"title bytes":          func(v *CollectionControlInput) { v.Input.Title = strings.Repeat("中", 54) },
		"summary bytes":        func(v *CollectionControlInput) { v.Input.Summary = strings.Repeat("a", 5001) },
		"note bytes":           func(v *CollectionControlInput) { v.Input.Entries[0].Note = strings.Repeat("中", 667) },
		"city bytes":           func(v *CollectionControlInput) { v.Input.CityID = strings.Repeat("a", 81) },
		"city control":         func(v *CollectionControlInput) { v.Input.CityID = "aberdeen\ngb" },
		"empty title":          func(v *CollectionControlInput) { v.Input.Title = " " },
		"empty entries":        func(v *CollectionControlInput) { v.Input.Entries = nil },
		"entry budget":         func(v *CollectionControlInput) { v.Input.Entries = make([]CollectionControlEntry, 51) },
		"unapproved precision": func(v *CollectionControlInput) { v.Input.Entries[0].LocationVisibility = "exact" },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			bad := c147Update()
			change(&bad)
			if _, err := NormalizeCollectionControl(bad); err == nil {
				t.Fatal("canonical original input limit bypassed")
			}
		})
	}
}

type c147Proof struct{}

func (c147Proof) RevalidateHumanControl(context.Context) error { return nil }

func TestCollectionControlPreviewSpecificNormalizedMaterialAndLease(t *testing.T) {
	now := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	in := c147Update()
	preview := CollectionControlPreview{
		HumanControlPreview: identity.HumanControlPreview{OperationID: in.OperationID, Action: in.Action, PayloadDigest: CollectionControlDigest(in), ObservedAt: now, ExpiresAt: now.Add(30 * time.Second), Approval: "specific.approval", ResponseProof: c147Proof{}},
		SchemaVersion:       CollectionControlSchemaVersion, Kind: in.Kind, CollectionID: in.CollectionID, ExpectedRevision: in.ExpectedRevision, NormalizedInput: in.Input,
	}
	if err := ValidateCollectionControlPreview(preview, in, now.Add(time.Second)); err != nil {
		t.Fatal("valid concrete preview rejected", err)
	}
	changes := map[string]func(*CollectionControlPreview){
		"operation":          func(v *CollectionControlPreview) { v.OperationID = c147SecondPlace },
		"action":             func(v *CollectionControlPreview) { v.Action = "WITHDRAW" },
		"target":             func(v *CollectionControlPreview) { v.CollectionID = c147SecondPlace },
		"kind":               func(v *CollectionControlPreview) { v.Kind = "theme_map" },
		"revision":           func(v *CollectionControlPreview) { v.ExpectedRevision++ },
		"digest":             func(v *CollectionControlPreview) { v.PayloadDigest = strings.Repeat("0", 64) },
		"expired":            func(v *CollectionControlPreview) { v.ExpiresAt = now.Add(time.Second) },
		"renewed lease":      func(v *CollectionControlPreview) { v.ExpiresAt = now.Add(30*time.Second + time.Nanosecond) },
		"future observation": func(v *CollectionControlPreview) { v.ObservedAt = now.Add(2 * time.Second) },
		"missing body":       func(v *CollectionControlPreview) { v.NormalizedInput = nil },
		"different body": func(v *CollectionControlPreview) {
			changed := c147Update().Input
			changed.Title = "different"
			v.NormalizedInput = changed
		},
		"missing approval": func(v *CollectionControlPreview) { v.Approval = "" },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			bad := preview
			change(&bad)
			if ValidateCollectionControlPreview(bad, in, now.Add(time.Second)) == nil {
				t.Fatal("changed material or stale approval became executable")
			}
		})
	}
	raw := c147JSON(t, preview)
	if strings.Contains(raw, "ResponseProof") || strings.Contains(raw, "responseProof") || strings.Contains(raw, "available") || strings.Contains(raw, `"input":`) || !strings.Contains(raw, `"normalizedInput":`) {
		t.Fatal("preview wire field contract or private proof leaked", raw)
	}
	withdraw := in
	withdraw.Action, withdraw.Input = "WITHDRAW", nil
	preview.Action, preview.NormalizedInput = "WITHDRAW", nil
	preview.PayloadDigest = CollectionControlDigest(withdraw)
	if ValidateCollectionControlPreview(preview, withdraw, now) != nil || strings.Contains(c147JSON(t, preview), "normalizedInput") {
		t.Fatal("withdraw preview must not expose an editable body")
	}
}

func TestCollectionControlImmutableReceiptAndBodylessStopShape(t *testing.T) {
	now := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	expected := int64(7)
	valid := CollectionControlReceipt{SchemaVersion: CollectionControlSchemaVersion, OperationID: c147Operation, Status: "SUCCEEDED", Action: "UPDATE", Kind: "journey", CollectionID: c147Collection, ExpectedRevision: &expected, AppliedRevision: 8, PayloadDigest: CollectionControlDigest(c147Update()), CommittedAt: &now, ResponseProof: c147Proof{}}
	if !ValidCollectionControlReceipt(valid, c147Operation) {
		t.Fatal("valid immutable original fact rejected")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(c147JSON(t, valid)), &fields) != nil || len(fields) != 10 || fields["collectionId"] == nil || fields["id"] != nil || fields["input"] != nil || fields["approval"] != nil || fields["responseProof"] != nil {
		t.Fatal("receipt contains material, approval or wrong target field", fields)
	}
	stop := CollectionControlReceipt{SchemaVersion: CollectionControlSchemaVersion, OperationID: c147Operation, Status: "STOPPED", ResponseProof: c147Proof{}}
	if !ValidCollectionControlReceipt(stop, c147Operation) {
		t.Fatal("zero-effect self stop rejected")
	}
	fields = nil
	if json.Unmarshal([]byte(c147JSON(t, stop)), &fields) != nil || len(fields) != 3 || fields["schemaVersion"] == nil || fields["operationId"] == nil || fields["status"] == nil {
		t.Fatal("STOPPED wire must be resource-free", fields)
	}
	for _, mutate := range []func(*CollectionControlReceipt){
		func(v *CollectionControlReceipt) { v.CollectionID = c147Collection },
		func(v *CollectionControlReceipt) { v.ExpectedRevision = &expected },
		func(v *CollectionControlReceipt) { v.AppliedRevision = 8 },
		func(v *CollectionControlReceipt) { v.PayloadDigest = valid.PayloadDigest },
		func(v *CollectionControlReceipt) { v.CommittedAt = &now },
	} {
		bad := stop
		mutate(&bad)
		if ValidCollectionControlReceipt(bad, c147Operation) {
			t.Fatal("STOPPED claimed resource facts or a prior effect")
		}
	}
	changes := map[string]func(*CollectionControlReceipt){
		"schema":           func(v *CollectionControlReceipt) { v.SchemaVersion = "old" },
		"operation":        func(v *CollectionControlReceipt) { v.OperationID = c147SecondPlace },
		"target":           func(v *CollectionControlReceipt) { v.CollectionID = "current object" },
		"missing expected": func(v *CollectionControlReceipt) { v.ExpectedRevision = nil },
		"wrong revision":   func(v *CollectionControlReceipt) { v.AppliedRevision++ },
		"unknown status":   func(v *CollectionControlReceipt) { v.Status = "UNKNOWN" },
		"unknown action":   func(v *CollectionControlReceipt) { v.Action = "CREATE" },
		"short digest":     func(v *CollectionControlReceipt) { v.PayloadDigest = "abc" },
		"upper digest":     func(v *CollectionControlReceipt) { v.PayloadDigest = strings.ToUpper(v.PayloadDigest) },
		"missing commit":   func(v *CollectionControlReceipt) { v.CommittedAt = nil },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			bad := valid
			change(&bad)
			if ValidCollectionControlReceipt(bad, c147Operation) {
				t.Fatal("current object or malformed fields impersonated original receipt")
			}
		})
	}
	max := MaxCollectionControlRevision
	valid.Action, valid.ExpectedRevision, valid.AppliedRevision = "WITHDRAW", &max, max
	if !ValidCollectionControlReceipt(valid, c147Operation) {
		t.Fatal("saturating terminal withdraw receipt rejected")
	}
	valid.AppliedRevision++
	if ValidCollectionControlReceipt(valid, c147Operation) {
		t.Fatal("terminal withdraw produced an out-of-range revision")
	}
}
