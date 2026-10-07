package httpapi

import (
	"context"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	sc "github.com/birdtie/birdtie/apps/api/internal/socialcontext"
	"net/http/httptest"
	"testing"
)

type malformedSharedHistory struct {
	*postgres.Store
	kind string
}

func (s *malformedSharedHistory) SharedSocialContextCurrent(ctx context.Context, a sc.CurrentAccess) (sc.Signals, sc.CurrentValidation, error) {
	v, c, e := s.Store.SharedSocialContextCurrent(ctx, a)
	if e != nil {
		return v, c, e
	}
	switch s.kind {
	case "nilClosure":
		c = nil
	case "owner":
		v.ViewerID = a.TargetID
	case "target":
		v.TargetID = a.ViewerID
	case "attendance":
		v.Attendance = "CONFIRMED"
	case "deadline":
		v.ValidUntil = v.ObservedAt
	case "visit":
		v.Visit = "VISITED"
	}
	return v, c, nil
}
func TestSharedHistoryHTTPMalformedNativeResponseFailsClosed(t *testing.T) {
	f := intentPlacePlansNative(t)
	f.exec(`UPDATE user_profiles SET visibility='public' WHERE account_id=ANY($1::uuid[])`, f.accountIDs[:2])
	for _, kind := range []string{"nilClosure", "owner", "target", "attendance", "deadline", "visit"} {
		t.Run(kind, func(t *testing.T) {
			s := &server{access: f.store, socialContext: &malformedSharedHistory{Store: f.store, kind: kind}}
			r := httptest.NewRequest("GET", "/shared-context", nil)
			r.SetPathValue("accountID", f.accountIDs[0])
			r.Header.Set("Authorization", "Bearer "+f.tokens[1])
			w := httptest.NewRecorder()
			s.sharedSocialContext(w, r)
			if w.Code != 503 {
				t.Fatal("malformed response became authority", kind, w.Code, w.Body.String())
			}
		})
	}
}
