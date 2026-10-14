package httpapi

import (
	ca "github.com/birdtie/birdtie/apps/api/internal/communityavatar"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	"github.com/birdtie/birdtie/apps/api/internal/follow"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"strings"
	"testing"
	"time"
)

func TestFollowCommunityAvatarRegisteredPageCurrentAndRetiredResponse(t *testing.T) {
	for _, change := range []string{"current", "Session", "source", "cross-type"} {
		t.Run(change, func(t *testing.T) {
			h, p, token := contactPageHTTP(t)
			id := "10000000-0000-4000-8000-000000000001"
			m := ca.Metadata{CommunityID: id, Version: 1, ThumbnailPath: ca.Path(id, 1), Width: 128, Height: 128, ByteCount: 1024, SHA256: strings.Repeat("0", 64)}
			p.follows.Items = []follow.Record{{ID: "20000000-0000-4000-8000-000000000002", Target: follow.Target{Type: follow.Community, ID: id}, CreatedAt: time.Now().UTC(), CommunityAvatar: &m, SourceProof: "PRIVATE_FRAME"}}
			want := 200
			switch change {
			case "Session":
				p.finalErr = identity.ErrUnauthorized
				want = 401
			case "source":
				p.finalErr = connection.ErrConflict
				want = 409
			case "cross-type":
				p.follows.Items[0].Target.Type = follow.Business
				want = 503
			}
			w := messagePolicyUnitRequest(h, "GET", "/v1/me/follows/page", "", token, nil)
			if w.Code != want {
				t.Fatal("registered page response", w.Code)
			}
			if strings.Contains(w.Body.String(), "PRIVATE_FRAME") || want != 200 && strings.Contains(w.Body.String(), "communityAvatar") {
				t.Fatal("retired metadata or private source released")
			}
			if want == 200 && !strings.Contains(w.Body.String(), m.ThumbnailPath) {
				t.Fatal("approved content path absent")
			}
		})
	}
}
