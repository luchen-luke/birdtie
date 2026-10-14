package httpapi

import (
	"bytes"
	"context"
	ec "github.com/birdtie/birdtie/apps/api/internal/entitycover"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/supplierprofile"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func coverFrameMedia(t *testing.T) (ec.Metadata, []byte) {
	t.Helper()
	var b bytes.Buffer
	if e := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 320, 180))); e != nil {
		t.Fatal(e)
	}
	d, e := ec.Prepare(b.Bytes(), "image/png")
	if e != nil {
		t.Fatal(e)
	}
	r := ec.Ref{Type: "activity", ID: communityAvatarID}
	return ec.Metadata{Ref: r, Version: 1, ThumbnailPath: ec.Path(r, 1), Width: 320, Height: 180, ByteCount: len(d.Bytes), SHA256: d.SHA256, ExpiresAt: time.Now().Add(40 * time.Second)}, d.Bytes
}
func TestEntityCoverOriginalContentProofAndSessionRetirement(t *testing.T) {
	for _, retire := range []bool{false, true} {
		t.Run(map[bool]string{false: "firstContentProof", true: "originalSessionABA"}[retire], func(t *testing.T) {
			m, b := coverFrameMedia(t)
			p := &coverHTTPSpy{current: ec.State{Version: 1, Cover: &m, SourceProof: "FRESH_STATE_NOT_CONTENT"}, originalProof: "FIRST_CONTENT_SOURCE", bytes: b}
			if retire {
				p.onOriginal = func() { p.frameGeneration = 3 }
			}
			w := httptest.NewRecorder()
			coverHTTPHandler(p).ServeHTTP(w, avatarRequest("GET", "/v1/activities/"+communityAvatarID+"/cover/1/content", ""))
			if retire {
				if w.Code != 409 || w.Header().Get("Content-Type") == "image/jpeg" || bytes.Equal(w.Body.Bytes(), b) {
					t.Fatal("original ABA pixels escaped", w.Code)
				}
			} else if w.Code != 200 || p.receivedProof != "FIRST_CONTENT_SOURCE" || p.stateReads != 0 || !bytes.Equal(w.Body.Bytes(), b) {
				t.Fatal("borrowed new read proof", w.Code, p.receivedProof, p.stateReads)
			}
			if p.frameCaptureCount != 1 {
				t.Fatal("recaptured original authorization")
			}
		})
	}
}

type coverFrameCatalog struct {
	foundation.PublicCatalog
	supplierprofile.Store
	p           *coverHTTPSpy
	activity    foundation.Activity
	reads       int
	retire      bool
	changeTitle bool
	shorten     bool
}

func (c *coverFrameCatalog) GetCity(context.Context, string) (foundation.City, error) {
	return foundation.City{ID: "aberdeen-gb"}, nil
}
func (c *coverFrameCatalog) AreaPulse(context.Context, string, string, foundation.ActivitySearchFilter) (foundation.AreaPulse, error) {
	return foundation.AreaPulse{Total: 1}, nil
}
func (c *coverFrameCatalog) FindActivities(ctx context.Context, _ string, _ string, _ foundation.ActivitySearchFilter) ([]foundation.Activity, error) {
	if _, ok := ec.OriginalSession(ctx, ec.Access{AccountID: avatarTestOwner, AccountType: "person"}); ok {
		panic("digest omitted frame must not match")
	}
	if !ec.HasOriginalSession(ctx) {
		return nil, ec.ErrChanged
	}
	if c.retire {
		c.p.frameGeneration = 2
	}
	return []foundation.Activity{c.activity}, nil
}
func (c *coverFrameCatalog) ReadPublicBusiness(ctx context.Context, _ string, _ string) (supplierprofile.Business, error) {
	if !ec.HasOriginalSession(ctx) {
		return supplierprofile.Business{}, ec.ErrChanged
	}
	c.reads++
	a := c.activity
	m := *a.Cover
	a.Cover = &m
	if c.reads > 1 {
		if c.changeTitle {
			a.Title = "Changed current source"
		}
		m.ExpiresAt = m.ExpiresAt.Add(10 * time.Second)
		if c.shorten {
			m.ExpiresAt = time.Now().Add(time.Second)
		}
	}
	if c.retire {
		c.p.frameGeneration = 2
	}
	return supplierprofile.Business{ID: avatarTestOwner, Name: "合成商家", UpcomingActivities: []foundation.Activity{a}}, nil
}
func TestEntityCoverCatalogOriginalFrameAndBusinessFixedLease(t *testing.T) {
	for _, tc := range []struct {
		name                           string
		business, retire, title, short bool
		want                           int
	}{{"pulseABA", false, true, false, false, 409}, {"pulseCurrent", false, false, false, false, 200}, {"businessABA", true, true, false, false, 409}, {"businessNormalLease", true, false, false, false, 200}, {"businessChangedText", true, false, true, false, 503}, {"businessDeadlineShortened", true, false, false, true, 409}} {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := coverFrameMedia(t)
			p := &coverHTTPSpy{}
			c := &coverFrameCatalog{p: p, activity: foundation.Activity{ID: communityAvatarID, Title: "合成封面活动", Cover: &m, CoverSourceProof: "SQL_PURE_SOURCE"}, retire: tc.retire, changeTitle: tc.title, shorten: tc.short}
			s := &server{access: p, catalog: c}
			var h http.HandlerFunc
			var r *http.Request
			if tc.business {
				h = s.getPublicBusiness
				r = avatarRequest("GET", "/v1/businesses/"+avatarTestOwner, "")
				r.SetPathValue("businessID", avatarTestOwner)
			} else {
				h = s.areaPulse
				r = avatarRequest("GET", "/?bounds=-3,57,-1,58", "")
				r.SetPathValue("cityID", "aberdeen-gb")
			}
			w := httptest.NewRecorder()
			h(w, r)
			if w.Code != tc.want {
				t.Fatal("catalog original frame/fixed lease", w.Code, w.Body.String())
			}
			if tc.want != 200 && bytes.Contains(w.Body.Bytes(), []byte("thumbnailPath")) {
				t.Fatal("retired catalog metadata escaped")
			}
			if p.frameCaptureCount != 1 {
				t.Fatal("late catalog recaptured frame", p.frameCaptureCount)
			}
		})
	}
}
