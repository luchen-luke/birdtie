package httpapi

import (
	"context"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/saved"
	"strings"
	"testing"
)

type savedSearchHTTPSpy struct {
	*savedPageHTTPSpy
	query     saved.Query
	sourceErr error
	validated int
}

func (p *savedSearchHTTPSpy) SearchHumanSavedPage(_ context.Context, d [32]byte, a identity.Actor, q saved.Query, c string) (saved.Page, error) {
	p.query = q
	return p.ListHumanSavedPage(context.Background(), d, a, c)
}
func (p *savedSearchHTTPSpy) ValidateHumanSavedSearchPage(_ context.Context, _ [32]byte, _ identity.Actor, _ saved.Query, _ string, _ saved.Page) error {
	p.validated++
	return p.sourceErr
}
func TestSavedSearchRegisteredQueryAndFinalSourceSession(t *testing.T) {
	for _, change := range []string{"current", "source", "Session"} {
		t.Run(change, func(t *testing.T) {
			_, old, a, token := savedPageHTTP(t)
			p := &savedSearchHTTPSpy{savedPageHTTPSpy: old}
			p.page.Items[0].CityID = "aberdeen-gb"
			h := New(nil, a, nil, nil, nil, nil, nil, nil, p, nil, nil, nil, false, nil, nil, nil)
			want := 200
			if change == "source" {
				p.sourceErr = saved.ErrPageChanged
				want = 409
			}
			if change == "Session" {
				a.finalErr = identity.ErrUnauthorized
				want = 401
			}
			w := messagePolicyUnitRequest(h, "GET", "/v1/me/saved/page?q=%25_&kind=place&cityId=aberdeen-gb", "", token, nil)
			if w.Code != want || p.query.Text != "%_" || p.query.CityID != "aberdeen-gb" || p.validated != 1 {
				t.Fatal("server search/guard", w.Code, p.query)
			}
			if want != 200 && strings.Contains(w.Body.String(), "备注") {
				t.Fatal("retired human result released")
			}
		})
	}
}
func TestSavedSearchRegisteredRejectsUnknownDuplicateAndOldFilteredCursor(t *testing.T) {
	for _, query := range []string{"q=x&q=y", "note=private", "q=x&cursor=" + humanHTTPOp, "kind=person", "q=%0A", "q=" + strings.Repeat("a", 241), "cityId=x&cityId=y"} {
		_, old, a, token := savedPageHTTP(t)
		p := &savedSearchHTTPSpy{savedPageHTTPSpy: old}
		h := New(nil, a, nil, nil, nil, nil, nil, nil, p, nil, nil, nil, false, nil, nil, nil)
		w := messagePolicyUnitRequest(h, "GET", "/v1/me/saved/page?"+query, "", token, nil)
		if w.Code != 400 || p.calls != 0 {
			t.Fatal("invalid query reached private source", query, w.Code)
		}
	}
}
