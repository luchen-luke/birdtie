package content

import (
	"encoding/json"
	mp "github.com/birdtie/birdtie/apps/api/internal/momentphoto"
	"strings"
	"testing"
	"time"
)

func TestPublicMomentPhotoDistinctVersionAndIndependentLease(t *testing.T) {
	now := time.Now().UTC()
	id := "11111111-1111-4111-8111-111111111111"
	place := "22222222-2222-4222-8222-222222222222"
	base := PublicMoment{SchemaVersion: "public-moment-v1", ID: id, Title: "明确公开文字", PlaceID: place, PlaceName: "原关联地点", CityID: "aberdeen-gb", Revision: 2, PublishedAt: now.Add(-time.Second), SourceBinding: "native-only"}
	v := PublicMomentWithPhoto{PublicMomentWithAuthor: PublicMomentWithAuthor{PublicMoment: base}, ObservedAt: now, ValidUntil: now.Add(40 * time.Second)}
	v.SchemaVersion = PublicMomentPhotoSchemaVersion
	if ValidatePublicMomentWithPhoto(v, id) != nil {
		t.Fatal("explicit version unavailable")
	}
	photoID := "33333333-3333-4333-8333-333333333333"
	v.Photo = &mp.Metadata{MomentID: id, MomentRevision: 2, PhotoID: photoID, Version: 1, ThumbnailPath: mp.Path(id, 2, photoID, 1), Width: 320, Height: 180, ByteCount: 200, SHA256: strings.Repeat("a", 64), ExpiresAt: now.Add(30 * time.Second)}
	if ValidatePublicMomentWithPhoto(v, id) != nil {
		t.Fatal("same current parent photo rejected")
	}
	v.Photo.MomentRevision = 3
	v.Photo.ThumbnailPath = mp.Path(id, 3, photoID, 1)
	if ValidatePublicMomentWithPhoto(v, id) == nil {
		t.Fatal("another text revision accepted")
	}
	raw, _ := json.Marshal(base)
	if strings.Contains(string(raw), "photo") || strings.Contains(string(raw), "observedAt") {
		t.Fatal("legacy closed v1 expanded")
	}
	if _, e := json.Marshal(PublicMomentPhotoReceipt{View: v}); e == nil {
		t.Fatal("native receipt serializable to machine/http")
	}
}
