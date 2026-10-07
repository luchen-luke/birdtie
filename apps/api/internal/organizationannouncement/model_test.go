package organizationannouncement

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestOrganizationAnnouncementClosedDraft(t *testing.T) {
	now := time.Date(2026, 10, 3, 1, 0, 0, 0, time.UTC)
	in := DraftInput{Title: " 合成公告 ", Body: " 不代表真实活动\n请阅读 ", ValidUntil: now.Add(time.Hour)}
	normalized, e := NormalizeDraft(in, now)
	if e != nil || normalized.Title != "合成公告" {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(in)
	if _, e = DecodeDraft(raw); e != nil {
		t.Fatal(e)
	}
	for name, mutate := range map[string]func(*DraftInput){"negative": func(i *DraftInput) { i.ExpectedRevision = -1 }, "expired": func(i *DraftInput) { i.ValidUntil = now }, "long": func(i *DraftInput) { i.Body = strings.Repeat("x", 6001) }, "empty": func(i *DraftInput) { i.Title = " " }, "control": func(i *DraftInput) { i.Body = "x\x00" }, "overyear": func(i *DraftInput) { i.ValidUntil = now.Add(366 * 24 * time.Hour) }} {
		t.Run(name, func(t *testing.T) {
			bad := in
			mutate(&bad)
			if _, e := NormalizeDraft(bad, now); e == nil {
				t.Fatal("bad draft accepted")
			}
		})
	}
	for _, raw := range []string{`{}`, `{"expectedRevision":1,"confirmed":true}`, `{"expectedRevision":1,"expectedRevision":1}`, `{"expectedRevision":null}`, `{"expectedRevision":1,"organizationId":"x"}`, `{"expectedRevision":1,"modelPermission":true}`} {
		if _, e := DecodeRevision([]byte(raw)); e == nil {
			t.Fatal("authority/duplicate accepted", raw)
		}
	}
	if _, e := DecodeRevision([]byte(`{"expectedRevision":1}`)); e != nil {
		t.Fatal(e)
	}
	if _, e := DecodePublish([]byte(`{"expectedRevision":1,"previewId":"94000000-0000-4000-8000-000000000001"}`)); e != nil {
		t.Fatal(e)
	}
}
func TestOrganizationAnnouncementPrivatePublicShapes(t *testing.T) {
	now := time.Date(2026, 10, 3, 1, 0, 0, 0, time.UTC)
	id := "94000000-0000-4000-8000-000000000001"
	r := Record{SchemaVersion: Schema, ID: id, OrganizationID: id, OrganizationAccountID: id, Revision: 1, Title: "合成公告", Body: "合成声明", Audience: "PUBLIC", State: Draft, ValidUntil: now.Add(time.Hour), CreatedBy: id, UpdatedBy: id, CreatedAt: now, UpdatedAt: now}
	if ValidateRecord(r) != nil {
		t.Fatal("draft invalid")
	}
	r.PublicationContext = json.RawMessage(`{"private":"never serialize"}`)
	raw, _ := json.Marshal(r)
	if strings.Contains(string(raw), "never serialize") {
		t.Fatal("private publication context leaked")
	}
	if ValidateRecord(r) == nil {
		t.Fatal("draft publication context accepted")
	}
	public := PublicRecord{SchemaVersion: Schema, ID: id, OrganizationID: id, OrganizationName: "合成组织", Revision: 2, Title: "合成公告", Body: "人工声明", State: Published, Audience: "PUBLIC", PublishedAt: now, ValidUntil: now.Add(time.Hour), Disclaimer: Disclaimer}
	if ValidatePublic(public, id, id, now) != nil {
		t.Fatal("public invalid")
	}
	if ValidatePublic(public, id, id, now.Add(time.Hour)) == nil {
		t.Fatal("expired accepted")
	}
	public.State = Draft
	if ValidatePublic(public, id, id, now) == nil {
		t.Fatal("draft public accepted")
	}
}

func TestOrganizationAnnouncementPublicSessionBindingClosed(t *testing.T) {
	id := "94000000-0000-4000-8000-000000000001"
	var digest [32]byte
	digest[0] = 1
	for _, a := range []PublicAccess{{}, {ViewerID: id, SessionDigest: digest}, {ViewerID: id, SessionDigest: digest, ExpectedSessionSnapshot: strings.Repeat("a", 64)}} {
		if ValidatePublicAccess(a) != nil {
			t.Fatal("valid binding rejected")
		}
	}
	for _, a := range []PublicAccess{{ViewerID: id}, {SessionDigest: digest}, {ExpectedSessionSnapshot: strings.Repeat("a", 64)}, {ViewerID: id, SessionDigest: digest, ExpectedSessionSnapshot: "bad"}, {ViewerID: id, SessionDigest: digest, ExpectedSessionSnapshot: strings.Repeat("A", 64)}} {
		if ValidatePublicAccess(a) == nil {
			t.Fatal("session spoof accepted")
		}
	}
	raw, _ := json.Marshal(PublicRecord{SessionSnapshot: strings.Repeat("a", 64)})
	if strings.Contains(string(raw), strings.Repeat("a", 64)) {
		t.Fatal("internal session snapshot disclosed")
	}
}
