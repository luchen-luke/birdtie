package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/businessconsole"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

// The embedded interface is never called: these direct tests exercise the
// real post-encode response guard, not a registered route or native Store.
type businessMemberProjectionResponseStore struct {
	businessconsole.Store
	current     businessconsole.Console
	readErr     error
	validateErr error
	reads       int
	validations int
}

func (s *businessMemberProjectionResponseStore) ReadBusinessConsole(context.Context, businessconsole.Access) (businessconsole.Console, error) {
	s.reads++
	return s.current, s.readErr
}
func (s *businessMemberProjectionResponseStore) ValidateBusinessAccess(context.Context, businessconsole.Access) (time.Time, error) {
	s.validations++
	return time.Now(), s.validateErr
}

func businessMemberProjectionResponseFixture() (businessconsole.Access, businessconsole.Console) {
	access := businessconsole.Access{BusinessID: "10000000-0000-4000-8000-000000000001"}
	console := businessconsole.Console{
		Business:          businessconsole.Business{ID: access.BusinessID, Name: "合成本地商家", Role: "owner"},
		Members:           []businessconsole.Member{{PersonID: "20000000-0000-4000-8000-000000000002", Role: "admin", Status: "active", DisplayName: "当前公开的合成姓名"}},
		MembershipVersion: 7,
		CanManage:         true,
		CanManageMembers:  true,
	}
	return access, console
}

func TestBusinessMemberProjectionResponseWithholdsRetiredNames(t *testing.T) {
	for _, change := range []string{"name changed", "field retired", "Session revoked during read", "Session revoked after read", "membership revoked"} {
		t.Run(change, func(t *testing.T) {
			a, initial := businessMemberProjectionResponseFixture()
			current := initial
			current.Members = append([]businessconsole.Member(nil), initial.Members...)
			store := &businessMemberProjectionResponseStore{current: current}
			want, wantValidations := 503, 0
			switch change {
			case "name changed":
				store.current.Members[0].DisplayName = "更新后公开的合成姓名"
			case "field retired":
				store.current.Members[0].DisplayName = ""
			case "Session revoked during read":
				store.readErr, want = identity.ErrUnauthorized, 401
			case "Session revoked after read":
				store.validateErr, want, wantValidations = identity.ErrUnauthorized, 401, 1
			case "membership revoked":
				store.readErr, want = businessconsole.ErrForbidden, 403
			}
			r := httptest.NewRequest("GET", "/v1/businesses/"+a.BusinessID+"/console", nil)
			w := httptest.NewRecorder()
			businessConsoleResult(w, r, a, store, initial)
			if w.Code != want || store.reads != 1 || store.validations != wantValidations {
				t.Fatalf("guard status=%d reads=%d validations=%d", w.Code, store.reads, store.validations)
			}
			if strings.Contains(w.Body.String(), initial.Members[0].DisplayName) || strings.Contains(w.Body.String(), initial.Members[0].PersonID) {
				t.Fatal("former member identity material released after retirement")
			}
		})
	}
}

func TestBusinessMemberProjectionResponseMissingNameKeepsRosterAndVersion(t *testing.T) {
	a, console := businessMemberProjectionResponseFixture()
	console.Members[0].DisplayName = ""
	store := &businessMemberProjectionResponseStore{current: console}
	w := httptest.NewRecorder()
	businessConsoleResult(w, httptest.NewRequest("GET", "/v1/businesses/"+a.BusinessID+"/console", nil), a, store, console)
	if w.Code != 200 || store.reads != 1 || store.validations != 1 {
		t.Fatalf("valid unnamed roster lost management response: status=%d", w.Code)
	}
	var response struct {
		Data struct {
			Members           []map[string]any `json:"members"`
			MembershipVersion int64            `json:"membershipVersion"`
			CanManageMembers  bool             `json:"canManageMembers"`
		} `json:"data"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &response); e != nil {
		t.Fatal(e)
	}
	if len(response.Data.Members) != 1 || response.Data.Members[0]["personId"] != console.Members[0].PersonID || response.Data.MembershipVersion != 7 || !response.Data.CanManageMembers {
		t.Fatal("missing name changed original membership or owner permission")
	}
	if _, exists := response.Data.Members[0]["displayName"]; exists {
		t.Fatal("missing name manufactured a visible label")
	}
}
