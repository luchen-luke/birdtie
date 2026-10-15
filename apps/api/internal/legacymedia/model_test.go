package legacymedia

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
)

func TestPrivateMediaSelectionPreservesOwnerDeletionAndOrder(t *testing.T) {
	owner := "15700000-0000-4000-8000-000000000001"
	note := "15700000-0000-4000-8000-000000000002"
	deleted := "15700000-0000-4000-8000-000000000003"
	asset := "15700000-0000-4000-8000-000000000004"
	base := legacymigration.Plan{Owners: []legacymigration.OwnerPlan{{SourceID: owner}}, Notes: []legacymigration.NotePlan{{SourceID: note, OwnerSourceID: owner, SourceSHA256: strings.Repeat("a", 64)}, {SourceID: deleted, OwnerSourceID: owner, Deleted: true}}}
	size := int64(123)
	sha := strings.Repeat("b", 64)
	a := map[string]any{"id": asset, "owner_id": owner, "size_bytes": json.Number("123"), "status": "ready", "content_type": "image/jpeg", "storage_key": "synthetic-key"}
	tables := map[string][]map[string]any{"media_assets": {a}, "note_media": {{"note_id": note, "media_id": asset, "position": json.Number("7")}, {"note_id": deleted, "media_id": asset, "position": json.Number("0")}}}
	selectors := []selector{{MediaID: asset, OwnerID: owner, Role: "original", Status: "ready", SourceMIME: "image/jpeg", MIME: "image/jpeg", SourceBytes: &size, Bytes: &size, SHA256: sha, Matched: true, StorageKey: "synthetic-key", Member: "private/synthetic-key"}}
	p := Plan{Deferred: map[string]int{}}
	if e := selectRecords(&p, base, tables, selectors); e != nil || p.Counts != (Counts{1, 1, 1}) || len(p.Records) != 1 || p.Records[0].Position != 7 || p.Deferred["deleted_note_relations"] != 1 {
		t.Fatal("selection lost original owner/order/deletion", e, p.Counts)
	}
	if p.Records[0].ImageID != stableImageID(note, asset) || stableImageID(deleted, asset) == p.Records[0].ImageID {
		t.Fatal("identity not stable per original attachment")
	}
	selectors[0].OwnerID = "different-owner"
	p = Plan{Deferred: map[string]int{}}
	if e := selectRecords(&p, base, tables, selectors); e != nil || p.Counts.Images != 0 || p.Deferred["owner_or_selector_unavailable"] != 1 {
		t.Fatal("foreign owner migrated")
	}
	selectors[0].OwnerID = owner
	tables["note_media"] = append(tables["note_media"], tables["note_media"][0])
	p = Plan{Deferred: map[string]int{}}
	if selectRecords(&p, base, tables, selectors) == nil {
		t.Fatal("duplicate source position accepted")
	}
	raw, _ := json.Marshal(Plan{Records: []Record{{SourceMediaID: "private-selector", InputSHA256: "private-digest"}}, Counts: Counts{Images: 1}})
	if strings.Contains(string(raw), "private-") {
		t.Fatal("private selector in diagnostics")
	}
}

func TestAuthenticatedMediaRejectsUnpinnedOrInvalidSource(t *testing.T) {
	if _, e := Build("15700000-0000-4000-8000-000000000001", []byte("{}"), []byte("{}"), []byte("{}"), []byte("{}"), Hash([]byte("{}"))); e == nil {
		t.Fatal("unverified preservation source accepted")
	}
	if ValidSHA(strings.Repeat("A", 64)) || ValidSHA("") || !ValidSHA(strings.Repeat("a", 64)) {
		t.Fatal("digest identity not exact")
	}
}
