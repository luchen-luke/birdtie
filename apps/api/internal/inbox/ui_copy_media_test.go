package inbox

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/profileavatar"
)

func copyMediaActorAvatar(accountID string) profileavatar.Metadata {
	return profileavatar.Metadata{
		AccountID:     accountID,
		Version:       3,
		ThumbnailPath: profileavatar.Path(accountID, 3),
		Width:         profileavatar.Size,
		Height:        profileavatar.Size,
		ByteCount:     1234,
		SHA256:        strings.Repeat("ab", 32),
	}
}

func TestCopyMediaInboxAvatarBoundToOriginalActor(t *testing.T) {
	actorID := "01234567-89ab-4cde-8fab-0123456789ab"
	for _, resourceType := range []string{"connection_request", "conversation_message"} {
		t.Run(resourceType, func(t *testing.T) {
			avatar := copyMediaActorAvatar(actorID)
			item := Item{ResourceType: resourceType, ResourceID: "original-source", ActorID: &actorID, ActorAvatar: &avatar}
			if !item.ValidActorMedia() {
				t.Fatal("canonical original actor avatar rejected")
			}
			otherAvatar := copyMediaActorAvatar("11234567-89ab-4cde-8fab-0123456789ab")
			if !otherAvatar.Valid() {
				t.Fatal("different actor fixture must remain valid metadata")
			}
			item.ActorAvatar = &otherAvatar
			if item.ValidActorMedia() {
				t.Fatal("different actor's canonical avatar accepted")
			}
		})
	}
}

func TestCopyMediaInboxAvatarRequiresCompleteActorTuple(t *testing.T) {
	actorID := "01234567-89ab-4cde-8fab-0123456789ab"
	avatar := copyMediaActorAvatar(actorID)
	for _, resourceType := range []string{"connection_request", "conversation_message"} {
		t.Run(resourceType, func(t *testing.T) {
			if (Item{ResourceType: resourceType, ActorID: &actorID}).ValidActorMedia() {
				t.Fatal("actor ID without actor avatar accepted")
			}
			if (Item{ResourceType: resourceType, ActorAvatar: &avatar}).ValidActorMedia() {
				t.Fatal("actor avatar without actor ID accepted")
			}
			emptyID := ""
			if (Item{ResourceType: resourceType, ActorID: &emptyID, ActorAvatar: &avatar}).ValidActorMedia() {
				t.Fatal("empty actor ID accepted")
			}
		})
	}
}

func TestCopyMediaInboxAvatarRejectsUntrustedMetadataAndSource(t *testing.T) {
	actorID := "01234567-89ab-4cde-8fab-0123456789ab"
	cases := []struct {
		name   string
		mutate func(*profileavatar.Metadata)
	}{
		{"external_path", func(m *profileavatar.Metadata) { m.ThumbnailPath = "https://external.invalid/avatar.jpg" }},
		{"other_account_path", func(m *profileavatar.Metadata) {
			m.ThumbnailPath = profileavatar.Path("11234567-89ab-4cde-8fab-0123456789ab", m.Version)
		}},
		{"stale_version_path", func(m *profileavatar.Metadata) { m.ThumbnailPath = profileavatar.Path(m.AccountID, m.Version-1) }},
		{"short_digest", func(m *profileavatar.Metadata) { m.SHA256 = strings.Repeat("a", 63) }},
		{"non_hex_digest", func(m *profileavatar.Metadata) { m.SHA256 = strings.Repeat("z", 64) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			avatar := copyMediaActorAvatar(actorID)
			tc.mutate(&avatar)
			item := Item{ResourceType: "conversation_message", ActorID: &actorID, ActorAvatar: &avatar}
			if item.ValidActorMedia() {
				t.Fatal("untrusted actor avatar metadata accepted")
			}
		})
	}
	for _, resourceType := range []string{"", "activity", "place", "community", "organization", "business", "opportunity", "conversation", "CONNECTION_REQUEST"} {
		t.Run("source_"+resourceType, func(t *testing.T) {
			avatar := copyMediaActorAvatar(actorID)
			item := Item{ResourceType: resourceType, ActorID: &actorID, ActorAvatar: &avatar}
			if item.ValidActorMedia() {
				t.Fatalf("person avatar accepted on unsupported source %q", resourceType)
			}
		})
	}
}

func TestCopyMediaInboxAvatarOptionalJSONCompatibility(t *testing.T) {
	for _, raw := range []string{
		`{"id":"legacy-item","resourceType":"community","resourceId":"original-community"}`,
		`{"id":"legacy-item","resourceType":"conversation_message","resourceId":"original-message","actorId":null,"actorAvatar":null}`,
	} {
		var item Item
		if err := json.Unmarshal([]byte(raw), &item); err != nil {
			t.Fatal(err)
		}
		if item.ActorID != nil || item.ActorAvatar != nil || !item.ValidActorMedia() {
			t.Fatal("legacy inbox item without actor media lost compatibility")
		}
		encoded, err := json.Marshal(item)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(encoded, &fields); err != nil {
			t.Fatal(err)
		}
		if _, ok := fields["actorId"]; ok {
			t.Fatal("absent actor ID must not be emitted")
		}
		if _, ok := fields["actorAvatar"]; ok {
			t.Fatal("absent actor avatar must not be emitted")
		}
	}

	actorID := "01234567-89ab-4cde-8fab-0123456789ab"
	avatar := copyMediaActorAvatar(actorID)
	original := Item{ResourceType: "conversation_message", ResourceID: "original-message", ActorID: &actorID, ActorAvatar: &avatar}
	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Item
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ActorID == nil || *decoded.ActorID != actorID || decoded.ActorAvatar == nil || *decoded.ActorAvatar != avatar || decoded.ResourceID != original.ResourceID || !decoded.ValidActorMedia() {
		t.Fatal("canonical actor tuple or original resource lost during JSON round trip")
	}
}
