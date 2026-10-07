package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/cityseed"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

// These transport spies prove strict selectors and error behavior only. They
// do not implement PostgreSQL authorization or a production identity service.
const (
	cityActivityHTTPActor     = "81000000-0000-4000-8000-000000000001"
	cityActivityHTTPPeer      = "81000000-0000-4000-8000-000000000002"
	cityActivityHTTPCandidate = "81000000-0000-4000-8000-000000000003"
	cityActivityHTTPSelector  = "A1000000-0000-4000-8000-000000000004"
)

type cityActivityHTTPSpy struct {
	cityseed.Store
	err                                 error
	submitCalls, listCalls, reviewCalls int
	actorID, cityID, candidateID        string
	input                               cityseed.ActivityInput
	review                              cityseed.ActivityReviewInput
}

func (s *cityActivityHTTPSpy) SubmitActivity(_ context.Context, actorID, cityID string, input cityseed.ActivityInput) (cityseed.ActivityCandidate, error) {
	s.submitCalls++
	s.actorID, s.cityID, s.input = actorID, cityID, input
	return cityseed.ActivityCandidate{ID: cityActivityHTTPCandidate, CityID: cityID, SubmittedBy: actorID, Status: "pending", Organizer: input.Organizer}, s.err
}

func (s *cityActivityHTTPSpy) ListActivityCandidates(_ context.Context, actorID, cityID string) ([]cityseed.ActivityCandidate, error) {
	s.listCalls++
	s.actorID, s.cityID = actorID, cityID
	return []cityseed.ActivityCandidate{{ID: cityActivityHTTPCandidate, Status: "pending"}}, s.err
}

func (s *cityActivityHTTPSpy) ReviewActivity(_ context.Context, actorID, candidateID string, input cityseed.ActivityReviewInput) (cityseed.ActivityCandidate, error) {
	s.reviewCalls++
	s.actorID, s.candidateID, s.review = actorID, candidateID, input
	status := "published"
	if input.Decision == "reject" {
		status = "rejected"
	}
	return cityseed.ActivityCandidate{ID: candidateID, Status: status}, s.err
}

func (s *cityActivityHTTPSpy) calls() int { return s.submitCalls + s.listCalls + s.reviewCalls }

func cityActivityHTTPFixture(t *testing.T) (*server, *privateProfileHTTPAccess, *cityActivityHTTPSpy, string) {
	t.Helper()
	token, digest, err := identity.NewToken()
	if err != nil {
		t.Fatal("cannot create local boundary credential")
	}
	access := &privateProfileHTTPAccess{actor: identity.Actor{ID: cityActivityHTTPActor, AccountType: "person"}, digest: digest}
	store := &cityActivityHTTPSpy{}
	return &server{access: access, seed: store}, access, store, token
}

func cityActivityHTTPBody(t *testing.T, extra string) string {
	t.Helper()
	now := time.Now().UTC()
	input := cityseed.ActivityInput{Title: "合成测试活动", Summary: "仅用于本地传输边界测试", HostLabel: "独立主办方标签", StartsAt: now.Add(24 * time.Hour), EndsAt: now.Add(26 * time.Hour), TimeZone: "Europe/London", SourceLabel: "合成线索来源", SourceURL: "https://example.invalid/authorized-event", RightsNote: "合成测试资料，仅限可移除的本地验收。", ExpiresAt: now.Add(7 * 24 * time.Hour)}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal("cannot marshal synthetic candidate")
	}
	if extra == "" {
		return string(raw)
	}
	return strings.TrimSuffix(string(raw), "}") + "," + extra + "}"
}

func cityActivityHTTPServe(s *server, method, route, body, token, contentType string, alter func(*http.Request)) *httptest.ResponseRecorder {
	r := privateProfileHTTPRequest(method, route, body, token, contentType)
	r.SetPathValue("cityID", "aberdeen-gb")
	r.SetPathValue("candidateID", cityActivityHTTPCandidate)
	if alter != nil {
		alter(r)
	}
	w := httptest.NewRecorder()
	if method == http.MethodGet {
		s.listActivityCandidates(w, r)
	} else if strings.Contains(route, "/review") {
		s.reviewActivityCandidate(w, r)
	} else {
		s.submitActivityCandidate(w, r)
	}
	return w
}

func cityActivityHTTPError(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status=%d want=%d", w.Code, status)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("candidate error must not be cached")
	}
	var envelope struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil || envelope.Error.Code != code {
		t.Fatalf("error code=%q want=%q", envelope.Error.Code, code)
	}
}

func TestCityActivityOrganizerHTTPExplicitAndLegacy(t *testing.T) {
	for _, tt := range []struct {
		name, extra string
		kind        actorref.Type
		id          string
	}{
		{"legacy omitted", "", "", ""},
		{"legacy explicit null", `"organizer":null`, "", ""},
		{"explicit own person", `"organizer":{"type":"PERSON","id":"` + cityActivityHTTPActor + `"}`, actorref.Person, cityActivityHTTPActor},
		{"normalized community", `"organizer":{"type":" community ","id":"` + cityActivityHTTPSelector + `"}`, actorref.Community, strings.ToLower(cityActivityHTTPSelector)},
		{"explicit organization", `"organizer":{"type":"ORGANIZATION","id":"` + cityActivityHTTPSelector + `"}`, actorref.Organization, strings.ToLower(cityActivityHTTPSelector)},
		{"explicit business", `"organizer":{"type":"BUSINESS","id":"` + cityActivityHTTPSelector + `"}`, actorref.Business, strings.ToLower(cityActivityHTTPSelector)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, _, spy, token := cityActivityHTTPFixture(t)
			w := cityActivityHTTPServe(s, http.MethodPost, "/v1/cities/aberdeen-gb/activity-candidates", cityActivityHTTPBody(t, tt.extra), token, "application/json; charset=utf-8", nil)
			if w.Code != http.StatusCreated || spy.submitCalls != 1 || spy.actorID != cityActivityHTTPActor || spy.cityID != "aberdeen-gb" {
				t.Fatal("explicit command did not preserve authenticated actor and city")
			}
			if tt.kind == "" {
				if spy.input.Organizer != nil {
					t.Fatal("legacy missing selector must remain unknown")
				}
			} else if spy.input.Organizer == nil || spy.input.Organizer.Type != tt.kind || spy.input.Organizer.ID != tt.id {
				t.Fatal("explicit typed selector was not normalized")
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("candidate response must not be cached")
			}
		})
	}
	for _, decision := range []string{"publish", "reject"} {
		t.Run("review "+decision, func(t *testing.T) {
			s, _, spy, token := cityActivityHTTPFixture(t)
			w := cityActivityHTTPServe(s, http.MethodPost, "/v1/activity-candidates/"+cityActivityHTTPCandidate+"/review", `{"decision":"`+decision+`","note":"  已核对合成线索资料及发布范围，仅本地测试。  "}`, token, "application/json", nil)
			if w.Code != http.StatusOK || spy.reviewCalls != 1 || spy.review.Decision != decision || spy.review.Note != strings.TrimSpace(spy.review.Note) || spy.actorID != cityActivityHTTPActor {
				t.Fatal("review command changed legacy decision contract")
			}
		})
	}
	t.Run("list pending", func(t *testing.T) {
		s, _, spy, token := cityActivityHTTPFixture(t)
		w := cityActivityHTTPServe(s, http.MethodGet, "/v1/cities/aberdeen-gb/activity-candidates", "", token, "", nil)
		if w.Code != http.StatusOK || spy.listCalls != 1 || spy.actorID != cityActivityHTTPActor {
			t.Fatal("pending list lost legacy authenticated route")
		}
	})
}

func TestCityActivityOrganizerHTTPStrictWire(t *testing.T) {
	base := cityActivityHTTPBody(t, "")
	for _, tt := range []struct {
		name, raw string
		code      string
	}{
		{"duplicate top title", cityActivityHTTPBody(t, `"title":"覆盖标题"`), "invalid_body"},
		{"duplicate organizer", cityActivityHTTPBody(t, `"organizer":null,"organizer":{"type":"PERSON","id":"`+cityActivityHTTPActor+`"}`), "invalid_body"},
		{"case top", cityActivityHTTPBody(t, `"Organizer":null`), "invalid_body"},
		{"duplicate nested type", cityActivityHTTPBody(t, `"organizer":{"type":"PERSON","type":"ORGANIZATION","id":"`+cityActivityHTTPActor+`"}`), "invalid_body"},
		{"duplicate nested id", cityActivityHTTPBody(t, `"organizer":{"type":"PERSON","id":"`+cityActivityHTTPActor+`","id":"`+cityActivityHTTPPeer+`"}`), "invalid_body"},
		{"case nested key", cityActivityHTTPBody(t, `"organizer":{"Type":"PERSON","id":"`+cityActivityHTTPActor+`"}`), "invalid_body"},
		{"name claimed", cityActivityHTTPBody(t, `"organizer":{"type":"PERSON","id":"`+cityActivityHTTPActor+`","name":"伪造授权姓名"}`), "invalid_body"},
		{"verified claimed", cityActivityHTTPBody(t, `"organizer":{"type":"PERSON","id":"`+cityActivityHTTPActor+`","verified":true}`), "invalid_body"},
		{"owner claimed", cityActivityHTTPBody(t, `"ownerId":"`+cityActivityHTTPActor+`"`), "invalid_body"},
		{"confirmed claimed", cityActivityHTTPBody(t, `"confirmed":true`), "invalid_body"},
		{"missing nested type", cityActivityHTTPBody(t, `"organizer":{"id":"`+cityActivityHTTPActor+`"}`), "invalid_body"},
		{"missing nested id", cityActivityHTTPBody(t, `"organizer":{"type":"PERSON"}`), "invalid_body"},
		{"nested null type", cityActivityHTTPBody(t, `"organizer":{"type":null,"id":"`+cityActivityHTTPActor+`"}`), "invalid_body"},
		{"nested null id", cityActivityHTTPBody(t, `"organizer":{"type":"PERSON","id":null}`), "invalid_body"},
		{"nested wrong type", cityActivityHTTPBody(t, `"organizer":{"type":true,"id":"`+cityActivityHTTPActor+`"}`), "invalid_body"},
		{"nested numeric id", cityActivityHTTPBody(t, `"organizer":{"type":"PERSON","id":42}`), "invalid_body"},
		{"selector array", cityActivityHTTPBody(t, `"organizer":[]`), "invalid_body"},
		{"selector string", cityActivityHTTPBody(t, `"organizer":"PERSON"`), "invalid_body"},
		{"unsupported kind", cityActivityHTTPBody(t, `"organizer":{"type":"CITY","id":"`+cityActivityHTTPActor+`"}`), "invalid_organizer"},
		{"invalid uuid", cityActivityHTTPBody(t, `"organizer":{"type":"PERSON","id":"not-an-id"}`), "invalid_organizer"},
		{"zero uuid", cityActivityHTTPBody(t, `"organizer":{"type":"PERSON","id":"00000000-0000-0000-0000-000000000000"}`), "invalid_organizer"},
		{"scalar null", strings.Replace(base, `"title":"合成测试活动"`, `"title":null`, 1), "invalid_body"},
		{"scalar object", strings.Replace(base, `"title":"合成测试活动"`, `"title":{}`, 1), "invalid_body"},
		{"top null", `null`, "invalid_body"},
		{"top array", `[]`, "invalid_body"},
		{"multiple documents", base + ` {}`, "invalid_body"},
		{"malformed", `{"organizer":`, "invalid_body"},
		{"empty", ``, "invalid_body"},
		{"oversize", strings.Repeat(" ", 8193) + base, "invalid_body"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, _, spy, token := cityActivityHTTPFixture(t)
			w := cityActivityHTTPServe(s, http.MethodPost, "/v1/cities/aberdeen-gb/activity-candidates", tt.raw, token, "application/json", nil)
			cityActivityHTTPError(t, w, http.StatusBadRequest, tt.code)
			if spy.calls() != 0 {
				t.Fatal("invalid selector reached a domain write")
			}
		})
	}
	for _, tt := range []struct{ name, raw string }{
		{"organizer override", `{"decision":"publish","note":"合成审核说明足够长，仅本地验收","organizer":{"type":"PERSON","id":"` + cityActivityHTTPActor + `"}}`},
		{"duplicate decision", `{"decision":"reject","decision":"publish","note":"合成审核说明足够长，仅本地验收"}`},
		{"case key", `{"Decision":"publish","note":"合成审核说明足够长，仅本地验收"}`},
		{"reviewer asserted", `{"decision":"publish","note":"合成审核说明足够长，仅本地验收","reviewedBy":"` + cityActivityHTTPActor + `"}`},
		{"null note", `{"decision":"publish","note":null}`},
		{"multiple", `{"decision":"reject","note":"合成审核说明足够长，仅本地验收"}{}`},
	} {
		t.Run("review "+tt.name, func(t *testing.T) {
			s, _, spy, token := cityActivityHTTPFixture(t)
			w := cityActivityHTTPServe(s, http.MethodPost, "/v1/activity-candidates/"+cityActivityHTTPCandidate+"/review", tt.raw, token, "application/json", nil)
			cityActivityHTTPError(t, w, http.StatusBadRequest, "invalid_body")
			if spy.calls() != 0 {
				t.Fatal("reviewer override reached publication")
			}
		})
	}
}

func TestCityActivityOrganizerHTTPCurrentActorBoundary(t *testing.T) {
	for _, route := range []struct{ name, method, path, body string }{
		{"submit", http.MethodPost, "/v1/cities/aberdeen-gb/activity-candidates", cityActivityHTTPBody(t, "")},
		{"list", http.MethodGet, "/v1/cities/aberdeen-gb/activity-candidates", ""},
		{"review", http.MethodPost, "/v1/activity-candidates/" + cityActivityHTTPCandidate + "/review", `{"decision":"publish","note":"合成审核说明足够长，仅本地验收"}`},
	} {
		for _, tt := range []struct {
			name, kind, query, header string
			omit, expired, workspace  bool
			status                    int
			code                      string
		}{
			{name: "anonymous", omit: true, status: 401, code: "unauthorized"},
			{name: "expired or revoked", expired: true, status: 401, code: "unauthorized"},
			{name: "organization principal", kind: "organization", status: 403, code: "person_account_required"},
			{name: "business principal", kind: "business", status: 403, code: "person_account_required"},
			{name: "organization workspace", workspace: true, header: cityActivityHTTPPeer, status: 403, code: "person_account_required"},
			{name: "empty workspace selector", workspace: true, status: 403, code: "person_account_required"},
			{name: "owner query", query: "?ownerId=" + cityActivityHTTPPeer, status: 400, code: "invalid_query"},
			{name: "organizer query", query: "?organizerId=" + cityActivityHTTPPeer, status: 400, code: "invalid_query"},
		} {
			t.Run(route.name+" "+tt.name, func(t *testing.T) {
				s, access, spy, token := cityActivityHTTPFixture(t)
				if tt.omit {
					token = ""
				}
				if tt.expired {
					access.err = identity.ErrUnauthorized
				}
				if tt.kind != "" {
					access.actor.AccountType = tt.kind
				}
				w := cityActivityHTTPServe(s, route.method, route.path+tt.query, route.body, token, "application/json", func(r *http.Request) {
					if tt.workspace {
						r.Header.Set("X-Birdtie-Organization-Workspace", tt.header)
					}
				})
				cityActivityHTTPError(t, w, tt.status, tt.code)
				if spy.calls() != 0 {
					t.Fatal("invalid actor or query reached candidate domain")
				}
			})
		}
	}
	t.Run("another person cannot be selected", func(t *testing.T) {
		s, _, spy, token := cityActivityHTTPFixture(t)
		w := cityActivityHTTPServe(s, http.MethodPost, "/v1/cities/aberdeen-gb/activity-candidates", cityActivityHTTPBody(t, `"organizer":{"type":"PERSON","id":"`+cityActivityHTTPPeer+`"}`), token, "application/json", nil)
		cityActivityHTTPError(t, w, 403, "organizer_permission_required")
		if spy.calls() != 0 {
			t.Fatal("another person's selector reached submit")
		}
	})
}

func TestCityActivityOrganizerHTTPRecoverableErrorsAndLegacyGuards(t *testing.T) {
	for _, tt := range []struct {
		name          string
		err           error
		status        int
		code, message string
	}{
		{"missing explicit organizer", cityseed.ErrOrganizerRequired, 409, "organizer_required", "请由原提交者明确选择有权管理的主办方后重新提交此线索。"},
		{"revoked organizer authority", cityseed.ErrOrganizerUnavailable, 409, "organizer_unavailable", "主办方当前不可用或管理权限已变更，请原提交者重新检查后提交。"},
		{"invalid organizer", cityseed.ErrInvalidOrganizer, 400, "invalid_organizer", "主办方信息不完整或格式不正确。"},
		{"city editor denied", cityseed.ErrForbidden, 403, "reviewer_required", ""},
		{"already reviewed", cityseed.ErrConflict, 409, "review_conflict", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, _, spy, token := cityActivityHTTPFixture(t)
			spy.err = fmt.Errorf("SYNTHETIC_INTERNAL_NO_ECHO: %w", tt.err)
			w := cityActivityHTTPServe(s, http.MethodPost, "/v1/activity-candidates/"+cityActivityHTTPCandidate+"/review", `{"decision":"publish","note":"已核对合成线索，明确仅供本地验收。"}`, token, "application/json", nil)
			cityActivityHTTPError(t, w, tt.status, tt.code)
			if spy.reviewCalls != 1 || strings.Contains(w.Body.String(), "SYNTHETIC_INTERNAL_NO_ECHO") || (tt.message != "" && !strings.Contains(w.Body.String(), tt.message)) {
				t.Fatal("recoverable error lost fixed Chinese explanation or leaked backend detail")
			}
		})
	}
	for _, tt := range []struct {
		name, method, path, body, media string
		alter                           func(*http.Request)
		status                          int
		code                            string
		noSeed                          bool
	}{
		{name: "media required", method: http.MethodPost, path: "/v1/cities/aberdeen-gb/activity-candidates", body: cityActivityHTTPBody(t, ""), media: "text/plain", status: 415, code: "json_required"},
		{name: "empty city", method: http.MethodGet, path: "/v1/cities/aberdeen-gb/activity-candidates", alter: func(r *http.Request) { r.SetPathValue("cityID", "") }, status: 400, code: "invalid_city_id"},
		{name: "long city", method: http.MethodGet, path: "/v1/cities/aberdeen-gb/activity-candidates", alter: func(r *http.Request) { r.SetPathValue("cityID", strings.Repeat("a", 81)) }, status: 400, code: "invalid_city_id"},
		{name: "invalid candidate", method: http.MethodPost, path: "/v1/activity-candidates/bad/review", body: `{}`, media: "application/json", alter: func(r *http.Request) { r.SetPathValue("candidateID", "bad") }, status: 400, code: "invalid_candidate_id"},
		{name: "bad decision", method: http.MethodPost, path: "/v1/activity-candidates/" + cityActivityHTTPCandidate + "/review", body: `{"decision":"approve","note":"合成审核说明足够长，仅本地验收"}`, media: "application/json", status: 400, code: "invalid_review"},
		{name: "short note", method: http.MethodPost, path: "/v1/activity-candidates/" + cityActivityHTTPCandidate + "/review", body: `{"decision":"publish","note":"短"}`, media: "application/json", status: 400, code: "invalid_review"},
		{name: "missing candidate fields", method: http.MethodPost, path: "/v1/cities/aberdeen-gb/activity-candidates", body: `{}`, media: "application/json", status: 400, code: "invalid_activity_candidate"},
		{name: "capability unavailable", method: http.MethodGet, path: "/v1/cities/aberdeen-gb/activity-candidates", status: 503, code: "city_activity_unavailable", noSeed: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, _, spy, token := cityActivityHTTPFixture(t)
			if tt.noSeed {
				s.seed = nil
			}
			w := cityActivityHTTPServe(s, tt.method, tt.path, tt.body, token, tt.media, tt.alter)
			cityActivityHTTPError(t, w, tt.status, tt.code)
			if spy.calls() != 0 {
				t.Fatal("invalid transport request reached domain")
			}
		})
	}
}
