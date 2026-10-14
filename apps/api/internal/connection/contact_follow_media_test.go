package connection

import (
	ca "github.com/birdtie/birdtie/apps/api/internal/communityavatar"
	"github.com/birdtie/birdtie/apps/api/internal/follow"
	"strings"
	"testing"
	"time"
)

func TestContactFollowMediaValidationRetainsOriginalPageBoundary(t *testing.T) {
	id := "10000000-0000-4000-8000-000000000001"
	stamp := time.Now().UTC()
	m := ca.Metadata{CommunityID: id, Version: 1, ThumbnailPath: ca.Path(id, 1), Width: 128, Height: 128, ByteCount: 1024, SHA256: strings.Repeat("0", 64)}
	r := follow.Record{ID: "20000000-0000-4000-8000-000000000002", Target: follow.Target{Type: follow.Community, ID: id}, CreatedAt: stamp, CommunityAvatar: &m}
	p := ContactPage[follow.Record]{Items: []follow.Record{r}}
	if ValidateContactPage(p, PageRequest{Limit: 20}, contactOwner, ContactFollows, ContactFollowRow) != nil {
		t.Fatal("current community row rejected")
	}
	p.Items[0].Target.Type = follow.Business
	if ValidateContactPage(p, PageRequest{Limit: 20}, contactOwner, ContactFollows, ContactFollowRow) == nil {
		t.Fatal("cross-type media passed page validator")
	}
}
