package agentresultprojection

import (
	"encoding/json"
	"strings"
	"testing"

	ca "github.com/birdtie/birdtie/apps/api/internal/communityavatar"
	"github.com/birdtie/birdtie/apps/api/internal/profileavatar"
	"github.com/birdtie/birdtie/apps/api/internal/saved"
)

func copyMediaAvatar(accountID string) profileavatar.Metadata {
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

func TestCopyMediaCommunityAvatarOriginalEntityAndSavedTarget(t *testing.T) {
	id := "01234567-89ab-4cde-8fab-0123456789ab"
	meta := ca.Metadata{CommunityID: id, Version: 2, ThumbnailPath: ca.Path(id, 2), Width: 128, Height: 128, ByteCount: 1234, SHA256: strings.Repeat("ab", 32)}
	ref := Ref{Type: "community", ID: id}
	item := Item{Entity: ref, Title: "合成社群头像", Scope: AuthorizedView, Detail: &ref, CommunityAvatar: &meta}
	if ValidateItem(item) != nil {
		t.Fatal("original controlled community rejected")
	}
	bookmark := saved.Item{ID: "11234567-89ab-4cde-8fab-0123456789ab", Kind: "group", TargetID: id, Available: true, CommunityAvatar: &meta}
	if !bookmark.ValidCommunityMedia() {
		t.Fatal("original saved community rejected")
	}
	if saved.ValidatePage(saved.Page{Items: []saved.Item{bookmark}}, "") != nil {
		t.Fatal("current canonical page metadata rejected")
	}
	for _, kind := range []string{"person", "place", "activity", "organization", "business", "group", "opportunity"} {
		otherRef := Ref{Type: kind, ID: id}
		wrong := Item{Entity: otherRef, Title: "原无图实体", Scope: AuthorizedView, Detail: &otherRef, Share: &otherRef}
		if kind == "group" {
			wrong.Detail = nil
			wrong.Share = nil
		}
		if kind == "opportunity" {
			activity := Ref{Type: "activity", ID: id}
			wrong.Entity.ID = "own-intent:" + id
			wrong.Scope = SelfPrivate
			wrong.Detail = &activity
			wrong.Share = &activity
		}
		if ValidateItem(wrong) != nil {
			t.Fatalf("media-free %s fixture invalid", kind)
		}
		wrong.CommunityAvatar = &meta
		if ValidateItem(wrong) != ErrInvalid {
			t.Fatalf("community avatar accepted as %s", kind)
		}
	}
	for _, mutate := range []func(*ca.Metadata){
		func(m *ca.Metadata) {
			m.CommunityID = "11234567-89ab-4cde-8fab-0123456789ab"
			m.ThumbnailPath = ca.Path(m.CommunityID, m.Version)
		},
		func(m *ca.Metadata) { m.ThumbnailPath = "https://external.invalid/photo" },
		func(m *ca.Metadata) { m.ThumbnailPath = ca.Path(id, 1) },
		func(m *ca.Metadata) { m.SHA256 = "bad" },
	} {
		bad := meta
		mutate(&bad)
		item.CommunityAvatar = &bad
		bookmark.CommunityAvatar = &bad
		if ValidateItem(item) != ErrInvalid || bookmark.ValidCommunityMedia() {
			t.Fatal("untrusted/mixed community media accepted")
		}
		if saved.ValidatePage(saved.Page{Items: []saved.Item{bookmark}}, "") != saved.ErrInvalidPage {
			t.Fatal("public Saved page validator accepted mismatched metadata")
		}
	}
	bookmark.CommunityAvatar = &meta
	bookmark.Available = false
	if bookmark.ValidCommunityMedia() {
		t.Fatal("invisible source retained media")
	}
	if saved.ValidatePage(saved.Page{Items: []saved.Item{bookmark}}, "") != saved.ErrInvalidPage {
		t.Fatal("invisible page retained metadata")
	}
	bookmark.Available = true
	bookmark.Kind = "place"
	if bookmark.ValidCommunityMedia() {
		t.Fatal("community avatar became Place cover")
	}
	item.CommunityAvatar = &meta
	raw, e := json.Marshal(item)
	if e != nil {
		t.Fatal(e)
	}
	var decoded Item
	if e = json.Unmarshal(raw, &decoded); e != nil || decoded.CommunityAvatar == nil || *decoded.CommunityAvatar != meta || decoded.Avatar != nil || ValidateItem(decoded) != nil {
		t.Fatal("community tuple round trip", e)
	}
}

func TestCopyMediaPersonAvatarBoundToOriginalEntity(t *testing.T) {
	person := Ref{Type: "person", ID: "01234567-89ab-4cde-8fab-0123456789ab"}
	avatar := copyMediaAvatar(person.ID)
	item := Item{Entity: person, Title: "原人物", Scope: AuthorizedView, Detail: &person, Share: &person, Avatar: &avatar}
	if err := ValidateItem(item); err != nil {
		t.Fatalf("original person's canonical avatar rejected: %v", err)
	}

	otherAvatar := copyMediaAvatar("11234567-89ab-4cde-8fab-0123456789ab")
	if !otherAvatar.Valid() {
		t.Fatal("different person's fixture must remain valid metadata")
	}
	item.Avatar = &otherAvatar
	if err := ValidateItem(item); err != ErrInvalid {
		t.Fatalf("different person's avatar accepted on original entity: %v", err)
	}
}

func TestCopyMediaPersonAvatarRejectsUntrustedMetadata(t *testing.T) {
	person := Ref{Type: "person", ID: "01234567-89ab-4cde-8fab-0123456789ab"}
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
			avatar := copyMediaAvatar(person.ID)
			tc.mutate(&avatar)
			item := Item{Entity: person, Title: "原人物", Scope: AuthorizedView, Detail: &person, Avatar: &avatar}
			if err := ValidateItem(item); err != ErrInvalid {
				t.Fatalf("untrusted person avatar metadata accepted: %v", err)
			}
		})
	}
}

func TestCopyMediaPersonAvatarCannotBecomeAnotherEntityCover(t *testing.T) {
	accountID := "01234567-89ab-4cde-8fab-0123456789ab"
	for _, kind := range []string{"activity", "place", "community", "organization", "business", "group", "opportunity"} {
		t.Run(kind, func(t *testing.T) {
			ref := Ref{Type: kind, ID: accountID}
			item := Item{Entity: ref, Title: "原实体", Scope: AuthorizedView, Detail: &ref, Share: &ref}
			if kind == "group" {
				item.Detail, item.Share = nil, nil
			}
			if kind == "opportunity" {
				activity := Ref{Type: "activity", ID: "original-activity"}
				item.Entity.ID = "own-intent:" + activity.ID
				item.Scope, item.Detail, item.Share = SelfPrivate, &activity, &activity
			}
			if err := ValidateItem(item); err != nil {
				t.Fatalf("original media-free entity fixture rejected: %v", err)
			}
			avatar := copyMediaAvatar(accountID)
			item.Avatar = &avatar
			if err := ValidateItem(item); err != ErrInvalid {
				t.Fatalf("person avatar accepted as %s cover: %v", kind, err)
			}
		})
	}
}

func TestCopyMediaPersonAvatarOptionalJSONCompatibility(t *testing.T) {
	for _, raw := range []string{
		`{"entityRef":{"type":"person","id":"legacy-person"},"title":"原人物","summary":"","scope":"AUTHORIZED_VIEW"}`,
		`{"entityRef":{"type":"person","id":"legacy-person"},"title":"原人物","summary":"","scope":"AUTHORIZED_VIEW","avatar":null}`,
	} {
		var item Item
		if err := json.Unmarshal([]byte(raw), &item); err != nil {
			t.Fatal(err)
		}
		if item.Avatar != nil || ValidateItem(item) != nil {
			t.Fatal("legacy projection without avatar lost compatibility")
		}
		encoded, err := json.Marshal(item)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(encoded, &fields); err != nil {
			t.Fatal(err)
		}
		if _, ok := fields["avatar"]; ok {
			t.Fatal("absent avatar must not be emitted")
		}
	}

	person := Ref{Type: "person", ID: "01234567-89ab-4cde-8fab-0123456789ab"}
	avatar := copyMediaAvatar(person.ID)
	original := Item{Entity: person, Title: "原人物", Scope: AuthorizedView, Avatar: &avatar}
	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Item
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Avatar == nil || *decoded.Avatar != avatar || decoded.Entity != person || ValidateItem(decoded) != nil {
		t.Fatal("canonical avatar or original entity lost during JSON round trip")
	}
}
