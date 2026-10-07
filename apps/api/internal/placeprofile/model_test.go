package placeprofile

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func fixture() (SubmitInput, time.Time) {
	at := time.Date(2026, 10, 3, 1, 0, 0, 0, time.UTC)
	return SubmitInput{OperationID: "00000000-0000-4000-8000-000000000001", Facts: Facts{Vibe: []string{"quiet"}, GoodFor: []string{"badminton"}, Price: &Price{"GBP", 0, 1200, "per_person"}, Accessibility: &Accessibility{"yes", "unknown"}, GroupSize: &GroupSize{2, 12}, Reservation: &Reservation{Support: "contact"}, Suitability: []string{"badminton"}}, Source: SourceInput{"本地合成场地资料", "https://example.invalid/local-semantic", "仅供隔离测试的虚构来源，不是运营凭据", at.Add(-time.Hour), at.Add(time.Hour)}, Confidence: Assessment{"EDITOR_ASSESSMENT_UNCALIBRATED", "MEDIUM"}}, at
}
func TestPlaceProfileSevenClaims(t *testing.T) {
	in, at := fixture()
	if e := ValidateSubmit(in, at); e != nil {
		t.Fatal(e)
	}
	p := Public{SchemaVersion: SchemaVersion, PlaceID: in.OperationID, CityID: "aberdeen-gb", Version: 1, Facts: in.Facts, Source: Source{in.Source.Label, in.Source.URL, in.Source.ObservedAt, at, in.Source.ExpiresAt}, Confidence: in.Confidence}
	if e := ValidatePublic(p, at); e != nil {
		t.Fatal(e)
	}
	cs := p.Claims()
	if len(cs) != 7 {
		t.Fatal(cs)
	}
	for k, c := range cs {
		if c.State != "REVIEWED_DECLARATION" || c.Source == nil || c.Confidence == nil || c.Value == nil {
			t.Fatal(k, c)
		}
	}
	p.Facts = Facts{GoodFor: []string{"chat"}}
	for k, c := range p.Claims() {
		if k == "good_for" {
			continue
		}
		if c.State != "UNKNOWN" || c.Value != nil || c.Source != nil || c.Confidence != nil {
			t.Fatal(k, c)
		}
	}
}
func TestPlaceProfileInvalidFacts(t *testing.T) {
	in, _ := fixture()
	mut := map[string]func(*Facts){"empty": func(f *Facts) { *f = Facts{} }, "duplicate": func(f *Facts) { f.Vibe = []string{"quiet", "quiet"} }, "code": func(f *Facts) { f.GoodFor = []string{"BADMINTON"} }, "largearray": func(f *Facts) { f.Vibe = make([]string, 17) }, "currency": func(f *Facts) { f.Price.Currency = "gbp" }, "negative": func(f *Facts) { f.Price.MinMinor = -1 }, "range": func(f *Facts) { f.Price.MaxMinor = -1 }, "max": func(f *Facts) { f.Price.MaxMinor = 100000001 }, "unit": func(f *Facts) { f.Price.Unit = "unknown" }, "access": func(f *Facts) { f.Accessibility.StepFree = "inferred" }, "accessunknown": func(f *Facts) { f.Accessibility = &Accessibility{"unknown", "unknown"} }, "groupzero": func(f *Facts) { f.GroupSize.Min = 0 }, "groupreverse": func(f *Facts) { f.GroupSize.Max = 1 }, "grouphuge": func(f *Facts) { f.GroupSize.Max = 1001 }, "reservationunknown": func(f *Facts) { f.Reservation.Support = "unknown" }, "urlmissing": func(f *Facts) { f.Reservation.Support = "external_url" }, "urlpresent": func(f *Facts) { s := "https://example.invalid"; f.Reservation.URL = &s }}
	for name, fn := range mut {
		t.Run(name, func(t *testing.T) {
			b, _ := json.Marshal(in.Facts)
			var f Facts
			json.Unmarshal(b, &f)
			fn(&f)
			if ValidateFacts(f) == nil {
				t.Fatal("accepted")
			}
		})
	}
}
func TestPlaceProfileInvalidSubmit(t *testing.T) {
	in, at := fixture()
	mut := map[string]func(*SubmitInput){"op": func(i *SubmitInput) { i.OperationID = "fixture" }, "version": func(i *SubmitInput) { i.ExpectedVersion = -1 }, "kind": func(i *SubmitInput) { i.Confidence.Kind = "PROBABILITY" }, "level": func(i *SubmitInput) { i.Confidence.Level = "100%" }, "label": func(i *SubmitInput) { i.Source.Label = " 私密姓名 " }, "rights": func(i *SubmitInput) { i.Source.RightsNote = "short" }, "http": func(i *SubmitInput) { i.Source.URL = "http://example.invalid" }, "user": func(i *SubmitInput) { i.Source.URL = "https://secret@example.invalid" }, "future": func(i *SubmitInput) { i.Source.ObservedAt = at.Add(time.Nanosecond) }, "expired": func(i *SubmitInput) { i.Source.ExpiresAt = at }, "longexpiry": func(i *SubmitInput) { i.Source.ExpiresAt = at.Add(366 * 24 * time.Hour) }}
	for name, fn := range mut {
		t.Run(name, func(t *testing.T) {
			b, _ := json.Marshal(in)
			var c SubmitInput
			json.Unmarshal(b, &c)
			fn(&c)
			if ValidateSubmit(c, at) == nil {
				t.Fatal("accepted")
			}
		})
	}
}
func TestPlaceProfileStrictWire(t *testing.T) {
	in, _ := fixture()
	b, _ := json.Marshal(in)
	if _, e := DecodeSubmit(b); e != nil {
		t.Fatal(e)
	}
	s := string(b)
	cases := map[string]string{"duplicate": strings.Replace(s, `"expectedVersion":0`, `"expectedVersion":0,"expectedVersion":0`, 1), "case": strings.Replace(s, `"facts"`, `"Facts"`, 1), "nestedduplicate": strings.Replace(s, `"minMinor":0`, `"minMinor":0,"minMinor":0`, 1), "nestedcase": strings.Replace(s, `"currency"`, `"Currency"`, 1), "unknown": strings.Replace(s, `"facts":`, `"privateMemory":{},"facts":`, 1), "nullfacts": strings.Replace(s, `"facts":{`, `"facts":null,"removed":{`, 1), "nullsource": strings.Replace(s, `"source":{`, `"source":null,"removed":{`, 1), "missingrequired": strings.Replace(s, `"expectedVersion":0,`, ``, 1), "tail": s + `{}`, "wrongtype": strings.Replace(s, `"minMinor":0`, `"minMinor":"0"`, 1), "fraction": strings.Replace(s, `"minMinor":0`, `"minMinor":0.5`, 1), "badutf8": string([]byte{'{', '"', 0xff, '"', ':', '1', '}'}), "oversized": strings.Repeat(" ", MaxInputBytes) + s}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, e := DecodeSubmit([]byte(raw)); e == nil {
				t.Fatal("accepted")
			}
		})
	}
}
func TestPlaceProfileReviewAndWithdraw(t *testing.T) {
	v := ReviewInput{"approve", 1, 0, "本次明确审核这份具体版本"}
	if ValidateReview(v) != nil {
		t.Fatal(v)
	}
	b, _ := json.Marshal(v)
	if _, e := DecodeReview(b); e != nil {
		t.Fatal(e)
	}
	for _, v := range []ReviewInput{{"approve", 0, 0, "具体审核资料内容"}, {"publish", 1, 0, "具体审核资料内容"}, {"reject", 1, -1, "具体审核资料内容"}, {"reject", 1, 0, "short"}} {
		if ValidateReview(v) == nil {
			t.Fatal(v)
		}
	}
	if ValidateWithdraw(WithdrawInput{1, "明确撤回该版本公开语义资料"}) != nil || ValidateWithdraw(WithdrawInput{0, "明确撤回该版本公开语义资料"}) == nil {
		t.Fatal("withdraw")
	}
}
