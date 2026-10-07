package socialintent

import (
	"encoding/json"
	"testing"
	"time"
)

func creationTestInput() DraftInput {
	return DraftInput{OperationID: "11111111-1111-4111-8111-111111111111", Type: "FIND_ACTIVITY", Title: " 周末羽毛球 ", Audience: "PRIVATE", Modality: "ONLINE", Constraints: json.RawMessage(`{"onlinePlatform":" Zoom ","startsAt":"2030-01-01T18:00:00+08:00","endsAt":"2030-01-01T19:00:00+08:00"}`), ExpiresAt: time.Date(2030, 1, 2, 1, 2, 3, 123456000, time.UTC)}
}
func TestSocialIntentCreationStaticContract(t *testing.T) {
	base := creationTestInput()
	for _, x := range []struct {
		name   string
		change func(*DraftInput)
		source string
		valid  bool
	}{
		{"private", func(*DraftInput) {}, "", true}, {"expired static still readable", func(p *DraftInput) { p.ExpiresAt = time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC) }, "", true},
		{"no key", func(p *DraftInput) { p.OperationID = "" }, "", false}, {"bad key", func(p *DraftInput) { p.OperationID = "title" }, "", false},
		{"public rejected", func(p *DraftInput) { p.Audience = "PUBLIC" }, "", false}, {"city target", func(p *DraftInput) { p.CityID = "aberdeen-gb" }, "", false},
		{"blank title", func(p *DraftInput) { p.Title = " " }, "", false}, {"modality unknown", func(p *DraftInput) { p.Modality = "" }, "", false},
		{"source valid", func(*DraftInput) {}, "22222222-2222-4222-8222-222222222222", true}, {"source wrong type", func(p *DraftInput) { p.Type = "OTHER" }, "22222222-2222-4222-8222-222222222222", false},
	} {
		t.Run(x.name, func(t *testing.T) {
			in := base
			x.change(&in)
			_, e := NormalizeCreation(in, x.source)
			if (e == nil) != x.valid {
				t.Fatal(e)
			}
		})
	}
}
func TestSocialIntentCreationDigestCanonicalAndBound(t *testing.T) {
	const owner = "33333333-3333-4333-8333-333333333333"
	base := creationTestInput()
	one, e := CreationDigest(owner, "", base)
	if e != nil {
		t.Fatal(e)
	}
	other := base
	other.OperationID = "44444444-4444-4444-8444-444444444444"
	other.Title = "周末羽毛球"
	other.Constraints = json.RawMessage(`{"endsAt":"2030-01-01T11:00:00Z","onlinePlatform":"Zoom","startsAt":"2030-01-01T10:00:00Z"}`)
	two, e := CreationDigest(owner, "", other)
	if e != nil || one != two {
		t.Fatal("equivalent canonical request", one, two, e)
	}
	t.Log("CROSS_LANGUAGE_CANONICAL_VECTOR", one)
	for _, change := range []func(*DraftInput){func(p *DraftInput) { p.Title = "另一个需求" }, func(p *DraftInput) { p.ExpiresAt = p.ExpiresAt.Add(time.Microsecond) }, func(p *DraftInput) { p.Type = "OTHER" }} {
		p := base
		change(&p)
		got, _ := CreationDigest(owner, "", p)
		if got == one {
			t.Fatal("content not bound")
		}
	}
	for _, x := range []struct{ owner, source string }{{"44444444-4444-4444-8444-444444444444", ""}, {owner, "22222222-2222-4222-8222-222222222222"}} {
		got, _ := CreationDigest(x.owner, x.source, base)
		if got == one {
			t.Fatal("owner/source not bound")
		}
	}
}
