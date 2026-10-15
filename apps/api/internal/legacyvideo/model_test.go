package legacyvideo

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPrivateVideoIdentityAndDiagnosticPrivacy(t *testing.T) {
	a := StableID("note-a", "media-a")
	if a != StableID("note-a", "media-a") || a == StableID("note-b", "media-a") || a == StableID("note-a", "media-b") {
		t.Fatal("unstable attachment identity")
	}
	p := Plan{Records: []Record{{SourceNoteID: "PRIVATE-NOTE", SourceMediaID: "PRIVATE-MEDIA", InputSHA256: "PRIVATE-HASH"}}, Counts: Counts{Videos: 1}}
	b, _ := json.Marshal(p)
	if strings.Contains(string(b), "PRIVATE-") {
		t.Fatal("private selector in public diagnostic")
	}
	if _, e := Build("", nil, nil, nil, nil, ""); e == nil {
		t.Fatal("unauthenticated input accepted")
	}
}
