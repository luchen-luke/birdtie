package sponsoredopportunity

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const testID = "22222222-2222-4222-8222-222222222222"

func validInput() SubmitInput {
	now := time.Date(2026, 10, 3, 1, 0, 0, 0, time.UTC)
	return SubmitInput{OperationID: testID, CityID: "synthetic-city", TargetType: "PLACE", TargetID: testID, Statement: "合成商业展示声明", SourceURL: "https://qa.example/source", RightsNote: "本地合成审核", ObservedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), SourceSnapshot: strings.Repeat("a", 64), Confirmed: true}
}
func TestSponsoredClosedInput(t *testing.T) {
	in := validInput()
	now := in.ObservedAt.Add(time.Minute)
	if ValidateSubmit(in, now) != nil {
		t.Fatal(in)
	}
	raw, _ := json.Marshal(in)
	if _, e := DecodeSubmit(raw); e != nil {
		t.Fatal(e)
	}
	cases := map[string]func(*SubmitInput){"unknown target": func(i *SubmitInput) { i.TargetType = "PERSON" }, "future observed": func(i *SubmitInput) { i.ObservedAt = now.Add(time.Second) }, "expired": func(i *SubmitInput) { i.ExpiresAt = now }, "overlong": func(i *SubmitInput) { i.ExpiresAt = now.Add(31 * 24 * time.Hour) }, "no approval": func(i *SubmitInput) { i.Confirmed = false }, "http": func(i *SubmitInput) { i.SourceURL = "http://qa.example" }, "credential": func(i *SubmitInput) { i.SourceURL = "https://a:b@qa.example" }, "fragment": func(i *SubmitInput) { i.SourceURL = "https://qa.example/#x" }, "private multiline": func(i *SubmitInput) { i.Statement = "a\nb" }, "invalid snapshot": func(i *SubmitInput) { i.SourceSnapshot = "version1" }, "zero id": func(i *SubmitInput) { i.OperationID = "00000000-0000-0000-0000-000000000000" }}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			i := in
			mutate(&i)
			if ValidateSubmit(i, now) == nil {
				t.Fatal("invalid declaration accepted", name)
			}
		})
	}
	for name, v := range map[string]string{"duplicate": strings.Replace(string(raw), `"confirmed":true`, `"confirmed":true,"confirmed":true`, 1), "case alias": strings.Replace(string(raw), `"confirmed"`, `"Confirmed"`, 1), "null": strings.Replace(string(raw), `"confirmed":true`, `"confirmed":null`, 1), "unknown": strings.TrimSuffix(string(raw), "}") + `,"permission":true}`, "trailing": string(raw) + ` {}`, "invalid utf8": string(raw[:len(raw)-1]) + string([]byte{255}) + "}"} {
		t.Run(name, func(t *testing.T) {
			if _, e := DecodeSubmit([]byte(v)); e == nil {
				t.Fatal("invalid wire accepted")
			}
		})
	}
}
func TestSponsoredDisclosure(t *testing.T) {
	i := validInput()
	p := Public{ID: testID, Revision: 2, Kind: "SPONSORED", Label: "赞助", Sponsor: Sponsor{Type: "BUSINESS", ID: testID, Name: "合成商家"}, Target: Target{Type: "PLACE", ID: testID, Title: "公开场地"}, Source: Source{URL: i.SourceURL, ObservedAt: i.ObservedAt, ReviewedAt: i.ObservedAt.Add(time.Minute), ExpiresAt: i.ExpiresAt}, CheckedAt: i.ObservedAt.Add(2 * time.Minute)}
	if ValidatePublic(p) != nil {
		t.Fatal(p)
	}
	d := Empty(true)
	d.SponsoredOpportunities = []Public{p}
	if ValidateDisclosure(d, []Target{p.Target}) != nil {
		t.Fatal(d)
	}
	for _, name := range []string{"wrong label", "wrong kind", "unknown sponsor", "unrelated ref", "private target", "past expiry", "future review", "duplicate", "unavailable nonempty"} {
		t.Run(name, func(t *testing.T) {
			q := p
			x := d
			x.SponsoredOpportunities = []Public{q}
			targets := []Target{p.Target}
			switch name {
			case "wrong label":
				x.SponsoredOpportunities[0].Label = "推荐"
			case "wrong kind":
				x.SponsoredOpportunities[0].Kind = "ORGANIC"
			case "unknown sponsor":
				x.SponsoredOpportunities[0].Sponsor.Type = "ORGANIZATION"
			case "unrelated ref":
				targets = nil
			case "private target":
				x.SponsoredOpportunities[0].Target.Type = "PERSON"
			case "past expiry":
				x.SponsoredOpportunities[0].Source.ExpiresAt = p.CheckedAt
			case "future review":
				x.SponsoredOpportunities[0].Source.ReviewedAt = p.CheckedAt.Add(time.Second)
			case "duplicate":
				x.SponsoredOpportunities = append(x.SponsoredOpportunities, p)
			case "unavailable nonempty":
				x.SponsoredStatus = "unavailable"
			}
			if ValidateDisclosure(x, targets) == nil {
				t.Fatal("bad disclosure accepted")
			}
		})
	}
	for _, d := range []Disclosure{Empty(true), Empty(false)} {
		if ValidateDisclosure(d, nil) != nil {
			t.Fatal(d)
		}
	}
}
func TestSponsoredReviewAndAccess(t *testing.T) {
	i := ReviewInput{ExpectedRevision: 1, Snapshot: strings.Repeat("a", 64), Decision: "approve", Note: "独立确认具体版本", Confirmed: true}
	raw, _ := json.Marshal(i)
	if _, e := DecodeReview(raw); e != nil || ValidateReview(i, false) != nil {
		t.Fatal(e)
	}
	for _, decision := range []string{"auto", "revoke", ""} {
		q := i
		q.Decision = decision
		if ValidateReview(q, false) == nil {
			t.Fatal(decision)
		}
	}
	i.Decision = "revoke"
	if ValidateReview(i, true) != nil {
		t.Fatal(i)
	}
	if ValidateAccess(Access{}, true) != nil || ValidateAccess(Access{}, false) == nil {
		t.Fatal("anonymous write accepted")
	}
	a := Access{ActorID: testID, AccountType: "person", SessionDigest: [32]byte{1}}
	if ValidateAccess(a, false) != nil {
		t.Fatal(a)
	}
	if _, e := json.Marshal(a); e == nil {
		t.Fatal("authority serialized")
	}
	if json.Unmarshal([]byte(`{"ActorID":"`+testID+`"}`), &a) == nil || a != (Access{}) {
		t.Fatal("client authority accepted")
	}
}
