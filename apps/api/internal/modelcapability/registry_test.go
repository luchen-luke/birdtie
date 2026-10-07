package modelcapability

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func fixtureRecord(now time.Time) Record {
	return Record{Key: Key{"fake-fixture", "fake-text", "fixture-v1", "text-wire.v1"}, Text: Supported,
		Capabilities: Capabilities{Supported, Supported, Supported, Supported, Supported, Supported},
		ValidatedAt:  now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), Regions: []Region{EU}, Evidence: OfflineContract, QualityRank: 10}
}
func registry(t *testing.T, records ...Record) *Registry {
	t.Helper()
	r, e := NewRegistry(records)
	if e != nil {
		t.Fatal(e)
	}
	return r
}

func TestRegistryClosedVersionedValidation(t *testing.T) {
	now := time.Now().UTC()
	cases := []struct {
		name   string
		change func(*Record)
	}{
		{"empty_provider", func(r *Record) { r.Key.Provider = "" }}, {"url_provider", func(r *Record) { r.Key.Provider = "https://fake.invalid" }},
		{"latest", func(r *Record) { r.Key.Version = "latest" }}, {"default", func(r *Record) { r.Key.Version = "default" }},
		{"auto", func(r *Record) { r.Key.Version = "auto" }}, {"alias_latest", func(r *Record) { r.Key.Version = "fixture-latest" }},
		{"compatible", func(r *Record) { r.Key.WireContract = "openai-compatible" }}, {"empty_wire", func(r *Record) { r.Key.WireContract = "" }},
		{"compatible_provider", func(r *Record) { r.Key.Provider = "openai-compatible" }},
		{"compatible_model", func(r *Record) { r.Key.Model = "openai-compatible" }},
		{"empty_text", func(r *Record) { r.Text = "" }}, {"self_verified", func(r *Record) { r.Evidence = "VERIFIED" }},
		{"live_verified", func(r *Record) { r.Evidence = "LIVE_VERIFIED" }}, {"zero_checked", func(r *Record) { r.ValidatedAt = time.Time{} }},
		{"expires_equal", func(r *Record) { r.ExpiresAt = r.ValidatedAt }}, {"too_old", func(r *Record) { r.ExpiresAt = r.ValidatedAt.Add(MaxValidationAge + time.Nanosecond) }},
		{"no_region", func(r *Record) { r.Regions = nil }}, {"unknown_region", func(r *Record) { r.Regions = []Region{"ANY"} }},
		{"duplicate_region", func(r *Record) { r.Regions = []Region{EU, EU} }}, {"quality_negative", func(r *Record) { r.QualityRank = -1 }},
		{"quality_high", func(r *Record) { r.QualityRank = 1001 }}, {"negative_cost", func(r *Record) { v := int64(-1); r.CostEstimateMicros = &v }},
		{"huge_cost", func(r *Record) { v := int64(1_000_000_000_001); r.CostEstimateMicros = &v }},
		{"doc_no_reference", func(r *Record) { r.Evidence = DocumentationChecked }}, {"http_reference", func(r *Record) { r.References = []string{"http://example.invalid/docs"} }},
		{"credential_reference", func(r *Record) { r.References = []string{"https://secret@example.invalid/docs"} }},
		{"query_reference", func(r *Record) { r.References = []string{"https://example.invalid/?api_key=secret"} }},
		{"fragment_reference", func(r *Record) { r.References = []string{"https://example.invalid/#secret"} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := fixtureRecord(now)
			tc.change(&r)
			if _, e := NewRegistry([]Record{r}); !errors.Is(e, ErrInvalid) {
				t.Fatal("invalid registry admitted", e)
			}
		})
	}
	for _, field := range []string{"vision", "tools", "schema", "streaming", "state", "storage"} {
		t.Run("empty_"+field, func(t *testing.T) {
			r := fixtureRecord(now)
			switch field {
			case "vision":
				r.Capabilities.Vision = ""
			case "tools":
				r.Capabilities.Tools = ""
			case "schema":
				r.Capabilities.Schema = ""
			case "streaming":
				r.Capabilities.Streaming = ""
			case "state":
				r.Capabilities.State = ""
			case "storage":
				r.Capabilities.Storage = ""
			}
			if _, e := NewRegistry([]Record{r}); e == nil {
				t.Fatal("empty support admitted")
			}
		})
	}
	r := fixtureRecord(now)
	if _, e := NewRegistry([]Record{r, r}); e == nil {
		t.Fatal("duplicate exact key admitted")
	}
	many := make([]Record, MaxRecords+1)
	if _, e := NewRegistry(many); e == nil {
		t.Fatal("unbounded registry")
	}
}

func TestRegistryImmutableAndDigestBound(t *testing.T) {
	now := time.Now().UTC()
	a := fixtureRecord(now)
	v := int64(12)
	a.CostEstimateMicros = &v
	a.References = []string{"https://example.invalid/docs"}
	b := fixtureRecord(now)
	b.Key.Version = "fixture-v2"
	r := registry(t, a, b)
	reverse := registry(t, b, a)
	if r.Digest() != reverse.Digest() {
		t.Fatal("digest order nondeterministic")
	}
	digest := r.Digest()
	a.Regions[0] = US
	a.References[0] = "https://changed.invalid"
	v = 999
	out := r.Records()
	out[0].Regions[0] = UK
	*out[0].CostEstimateMicros = 0
	out[0].References[0] = "https://changed-again.invalid"
	stable := r.Records()
	if r.Digest() != digest || stable[0].Regions[0] != EU || *stable[0].CostEstimateMicros != 12 || stable[0].References[0] != "https://example.invalid/docs" {
		t.Fatal("alias changed trusted registry")
	}
	b.Capabilities.Vision = Unsupported
	if registry(t, stable[0], b).Digest() == digest {
		t.Fatal("capability change did not bind digest")
	}
	var nilRegistry *Registry
	if nilRegistry.Digest() != "" || nilRegistry.Records() != nil {
		t.Fatal("nil registry")
	}
}

func TestControlObjectsRejectJSONClaims(t *testing.T) {
	for _, object := range []any{Record{}, Registry{}, OfflineGrant{}} {
		if _, e := json.Marshal(object); !errors.Is(e, ErrServerOnly) {
			t.Fatal("control marshalled", e)
		}
	}
	r := fixtureRecord(time.Now())
	reg := *registry(t, r)
	g := OfflineGrant{State: "ALLOWED"}
	for _, object := range []any{&r, &reg, &g} {
		if e := json.Unmarshal([]byte(`{"Verified":true,"State":"ALLOWED"}`), object); !errors.Is(e, ErrServerOnly) {
			t.Fatal("client control claim admitted", e)
		}
	}
	if r.Key.Provider != "" || reg.Digest() != "" || g.State != "" {
		t.Fatal("rejected control retained authority")
	}
}

func TestRequestDigestExactInputs(t *testing.T) {
	now := time.Now().UTC()
	r := fixtureRequest(now, "TEXT")
	d := RequestDigest(r, Requirements{})
	copy := r
	copy.DeadlineAt = copy.DeadlineAt.In(time.FixedZone("offset", 3600))
	if RequestDigest(copy, Requirements{}) != d {
		t.Fatal("equivalent UTC differs")
	}
	copy.RunID = "74000000-0000-4000-8000-000000000099"
	if RequestDigest(copy, Requirements{}) == d {
		t.Fatal("run input not bound")
	}
	if RequestDigest(r, Requirements{Vision: true}) == d {
		t.Fatal("modality requirement not bound")
	}
}
