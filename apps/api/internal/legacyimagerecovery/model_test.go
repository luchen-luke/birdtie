package legacyimagerecovery

import (
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
	"strings"
	"testing"
)

func TestRecoverySelectsOnlyClosedMetadataDefects(t *testing.T) {
	owner, note, asset := "16100000-0000-4000-8000-000000000001", "16100000-0000-4000-8000-000000000002", "16100000-0000-4000-8000-000000000003"
	base := legacymigration.Plan{Owners: []legacymigration.OwnerPlan{{SourceID: owner}}, Notes: []legacymigration.NotePlan{{SourceID: note, OwnerSourceID: owner, SourceSHA256: strings.Repeat("a", 64)}}}
	zero, size := int64(0), int64(123)
	a := map[string]any{"id": asset, "owner_id": owner, "kind": "image", "status": "ready", "content_type": "", "size_bytes": json.Number("0"), "storage_key": "synthetic"}
	tables := map[string][]map[string]any{"media_assets": {a}, "note_media": {{"note_id": note, "media_id": asset, "position": json.Number("7")}}}
	s := selector{MediaID: asset, OwnerID: owner, Role: "original", Status: "ready", StorageKey: "synthetic", Member: "media/synthetic", MIME: "image/jpeg", SourceBytes: &zero, Bytes: &size, SHA256: strings.Repeat("b", 64), Reasons: []string{"NOT_SCANNED", "RIGHTS_NOT_REVIEWED", "SOURCE_MIME_UNKNOWN", "SOURCE_SIZE_UNKNOWN"}}
	p := Plan{Deferred: map[string]int{}}
	if e := selectRecords(&p, base, tables, []selector{s}); e != nil || p.Counts != (Counts{Images: 1, JPEG: 1, Notes: 1, Owners: 1}) || p.Records[0].Position != 7 || *p.Records[0].SourceDeclaredBytes != 0 {
		t.Fatal("JPEG recovery lost exact original facts", e)
	}
	firstID := p.Records[0].ImageID
	a["content_type"], a["size_bytes"] = "image/heic", json.Number("123")
	s.SourceMIME, s.MIME, s.SourceBytes = "image/heic", "video/mp4", &size
	s.Reasons = []string{"NOT_SCANNED", "RIGHTS_NOT_REVIEWED", "MIME_MISMATCH"}
	p = Plan{Deferred: map[string]int{}}
	if e := selectRecords(&p, base, tables, []selector{s}); e != nil || p.Counts.HEIC != 1 || p.Records[0].Format != "image/heic" || p.Records[0].ImageID != firstID {
		t.Fatal("HEIC was treated as a video or unstable attachment", e)
	}
	s.Reasons = append(s.Reasons, "ARCHIVE_OBJECT_MISSING")
	p = Plan{Deferred: map[string]int{}}
	if e := selectRecords(&p, base, tables, []selector{s}); e != nil || p.Counts.Images != 0 {
		t.Fatal("missing archive or arbitrary mismatch admitted")
	}
	s.Reasons = s.Reasons[:3]
	s.OwnerID = "foreign"
	p = Plan{Deferred: map[string]int{}}
	if selectRecords(&p, base, tables, []selector{s}) == nil {
		t.Fatal("owner mismatch admitted")
	}
	raw, _ := json.Marshal(Plan{Counts: Counts{Images: 1}, Records: []Record{{Format: "private-selector"}}})
	if strings.Contains(string(raw), "private-selector") {
		t.Fatal("selector leaked")
	}
	if _, e := Build("16100000-0000-4000-8000-000000000001", []byte("{}"), []byte("{}"), []byte("{}"), []byte("{}"), strings.Repeat("a", 64)); e == nil {
		t.Fatal("unpinned preservation accepted")
	}
}
