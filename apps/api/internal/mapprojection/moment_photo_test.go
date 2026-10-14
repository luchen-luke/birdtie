package mapprojection

import (
	"encoding/json"
	mphoto "github.com/birdtie/birdtie/apps/api/internal/momentphoto"
	"strings"
	"testing"
	"time"
)

func momentMediaMapView() View {
	v := mediaMapView()
	v.SchemaVersion = MomentMediaSchemaVersion
	item := &v.Items[0]
	item.Kind = "MOMENT"
	item.Entity.Type = "MOMENT"
	item.Detail.Type = "MOMENT"
	item.PlacePhoto = nil
	item.RecentMoments = nil
	photoID := "22222222-2222-4222-8222-222222222222"
	item.MomentPhoto = &mphoto.Metadata{MomentID: item.ID, MomentRevision: 3, PhotoID: photoID, Version: 2, ThumbnailPath: mphoto.Path(item.ID, 3, photoID, 2), Width: 320, Height: 180, ByteCount: 100, SHA256: strings.Repeat("b", 64), ExpiresAt: v.ObservedAt.Add(time.Minute), Attribution: "隔离测试素材"}
	return v
}

func TestPublicMomentPhotoMapClosedScopeAndIndependentRetirement(t *testing.T) {
	q := Query{CityID: "aberdeen-gb", West: -2.2, South: 57.1, East: -2, North: 57.3, MomentMedia: true}
	v := momentMediaMapView()
	if ValidateViewForQuery(v, q) != nil {
		t.Fatal("valid negotiated v4")
	}
	out := RetireExpiredMedia(v, v.ObservedAt.Add(time.Minute))
	if out.Items[0].MomentPhoto != nil || out.Items[0].ID != v.Items[0].ID || out.Items[0].Anchor != v.Items[0].Anchor || out.Items[0].SourceVersion != v.Items[0].SourceVersion || out.ValidUntil != v.ValidUntil || v.Items[0].MomentPhoto == nil {
		t.Fatal("photo retired base or mutated original receipt")
	}
	v.Scope = PublicAuthor
	v.AuthorID = v.Items[0].ID
	q.AuthorID = v.AuthorID
	if ValidateViewForQuery(v, q) != nil {
		t.Fatal("current explicit author v4 same Moment")
	}
	q.MomentMedia = false
	if ValidateViewForQuery(v, q) == nil {
		t.Fatal("new schema accepted for old read")
	}
	for _, mode := range []string{"v1", "v2", "v3", "private", "wrong-moment", "wrong-path-revision", "new-lease", "place"} {
		t.Run(mode, func(t *testing.T) {
			x := momentMediaMapView()
			switch mode {
			case "v1":
				x.SchemaVersion = SchemaVersion
			case "v2":
				x.SchemaVersion = PublicAuthorSchemaVersion
				x.Scope = PublicAuthor
				x.AuthorID = x.Items[0].ID
			case "v3":
				x.SchemaVersion = MediaSchemaVersion
			case "private":
				x.Scope = SelfPrivate
			case "wrong-moment":
				x.Items[0].MomentPhoto.MomentID = x.Items[0].MomentPhoto.PhotoID
			case "wrong-path-revision":
				x.Items[0].MomentPhoto.MomentRevision++
			case "new-lease":
				x.Items[0].MomentPhoto.ExpiresAt = x.ObservedAt.Add(62 * time.Second)
			case "place":
				x.Items[0].Kind = "PLACE"
				x.Items[0].Entity.Type = "PLACE"
				x.Items[0].Detail.Type = "PLACE"
			}
			if ValidateView(x) == nil {
				t.Fatal("bad optional public photo admitted", mode)
			}
		})
	}
	old := momentMediaMapView()
	old.SchemaVersion = SchemaVersion
	old.Items[0].MomentPhoto = nil
	b, e := json.Marshal(old)
	if e != nil || strings.Contains(string(b), "momentPhoto") || ValidateView(old) != nil {
		t.Fatal("bare original v1 changed")
	}
	q.MomentMedia = true
	q.Private = true
	if ValidateQuery(q) == nil {
		t.Fatal("private public photo query")
	}
	q.Private = false
	q.Media = true
	if ValidateQuery(q) == nil {
		t.Fatal("ambiguous two protocol selectors")
	}
}
