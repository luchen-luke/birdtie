package follow

import (
	"encoding/json"
	ca "github.com/birdtie/birdtie/apps/api/internal/communityavatar"
	av "github.com/birdtie/birdtie/apps/api/internal/profileavatar"
	"strings"
	"testing"
)

func TestFollowCommunityAvatarOriginalTargetAndPrivateProofBoundary(t *testing.T) {
	id := "10000000-0000-4000-8000-000000000001"
	m := ca.Metadata{CommunityID: id, Version: 1, ThumbnailPath: ca.Path(id, 1), Width: 128, Height: 128, ByteCount: 1024, SHA256: strings.Repeat("0", 64)}
	r := Record{Target: Target{Type: Community, ID: id}, CommunityAvatar: &m, SourceProof: "PRIVATE_SOURCE_EPOCH"}
	if !r.ValidMedia() {
		t.Fatal("approved original community rejected")
	}
	b, _ := json.Marshal(r)
	if strings.Contains(string(b), "PRIVATE_SOURCE_EPOCH") || !strings.Contains(string(b), "communityAvatar") {
		t.Fatal("private proof crossed wire")
	}
	for _, kind := range []string{Person, Organization, Business} {
		bad := r
		bad.Target.Type = kind
		if bad.ValidMedia() {
			t.Fatal("community image borrowed by other entity")
		}
	}
	bad := r
	wrong := m
	wrong.CommunityID = "20000000-0000-4000-8000-000000000002"
	bad.CommunityAvatar = &wrong
	if bad.ValidMedia() {
		t.Fatal("other community image accepted")
	}
	wrong = m
	wrong.ThumbnailPath = "https://example.invalid/photo"
	bad.CommunityAvatar = &wrong
	if bad.ValidMedia() {
		t.Fatal("arbitrary URL accepted")
	}
	bad = r
	bad.Avatar = &av.Metadata{AccountID: id, Version: 1, ThumbnailPath: av.Path(id, 1), Width: 128, Height: 128, ByteCount: 1024, SHA256: m.SHA256}
	if bad.ValidMedia() {
		t.Fatal("person image mixed into community")
	}
	if !(Record{Target: Target{Type: Community, ID: id}}).ValidMedia() {
		t.Fatal("honest no-image legacy row lost")
	}
}
