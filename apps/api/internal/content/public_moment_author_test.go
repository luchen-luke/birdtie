package content

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func publicMomentAuthorFixture() PublicMomentWithAuthor {
	return PublicMomentWithAuthor{PublicMoment: PublicMoment{SchemaVersion: PublicMomentAuthorSchemaVersion, ID: "11111111-1111-4111-8111-111111111111", Title: "公开分享", Body: "本人分享", PlaceID: "22222222-2222-4222-8222-222222222222", PlaceName: "公开地点", CityID: "test", Revision: 2, PublishedAt: time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC), SourceBinding: "native-proof"}}
}

func TestPublicMomentAuthorClosedNullableWire(t *testing.T) {
	for _, visible := range []bool{false, true} {
		p := publicMomentAuthorFixture()
		if visible {
			id := "33333333-3333-4333-8333-333333333333"
			p.AuthorID = &id
			p.AuthorPublicProfile = &PublicMomentAuthor{AccountID: id, DisplayName: "公开作者"}
		}
		if err := ValidatePublicMomentWithAuthor(p, p.ID); err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(p)
		var wire map[string]json.RawMessage
		if json.Unmarshal(raw, &wire) != nil || len(wire) != 11 || wire["authorId"] == nil || wire["authorPublicProfile"] == nil {
			t.Fatal("v2 must retain both nullable keys", string(raw))
		}
		if !visible && (string(wire["authorId"]) != "null" || string(wire["authorPublicProfile"]) != "null") {
			t.Fatal("unavailable author must be explicit null", string(raw))
		}
		if visible {
			var author map[string]json.RawMessage
			if json.Unmarshal(wire["authorPublicProfile"], &author) != nil || len(author) != 2 {
				t.Fatal("author profile has unauthorized fields")
			}
		}
		p.PublicMoment.SchemaVersion = "public-moment-v1"
		legacy, _ := json.Marshal(p.PublicMoment)
		if strings.Contains(string(legacy), "author") || strings.Contains(string(legacy), "native-proof") {
			t.Fatal("legacy wire changed", string(legacy))
		}
	}
}

func TestPublicMomentAuthorRejectsInconsistentOrPrivateFallback(t *testing.T) {
	for _, mode := range []string{"id_only", "profile_only", "mismatch", "bad_id", "empty", "space", "control", "long", "bad_schema", "missing_proof"} {
		t.Run(mode, func(t *testing.T) {
			p := publicMomentAuthorFixture()
			id := "33333333-3333-4333-8333-333333333333"
			p.AuthorID = &id
			p.AuthorPublicProfile = &PublicMomentAuthor{AccountID: id, DisplayName: "公开作者"}
			switch mode {
			case "id_only":
				p.AuthorPublicProfile = nil
			case "profile_only":
				p.AuthorID = nil
			case "mismatch":
				p.AuthorPublicProfile.AccountID = p.PlaceID
			case "bad_id":
				id = "private-handle"
			case "empty":
				p.AuthorPublicProfile.DisplayName = ""
			case "space":
				p.AuthorPublicProfile.DisplayName = " 作者 "
			case "control":
				p.AuthorPublicProfile.DisplayName = "作者\n秘密"
			case "long":
				p.AuthorPublicProfile.DisplayName = strings.Repeat("中", 27)
			case "bad_schema":
				p.SchemaVersion = "public-moment-v1"
			case "missing_proof":
				p.SourceBinding = ""
			}
			if ValidatePublicMomentWithAuthor(p, p.ID) == nil {
				t.Fatal("invalid author accepted")
			}
		})
	}
}
