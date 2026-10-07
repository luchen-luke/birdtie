package httpapi

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPublicMomentHTTPNativeRegisteredLifecycle(t *testing.T) {
	f := publicationHTTPNative(t)
	m := publicationHTTPNativeDraft(t, f)
	path := "/v1/moments/" + m.ID
	if w := historicalHTTPCall(f.base, "GET", path, "", ""); w.Code != 404 {
		t.Fatal("private public route", w.Code, w.Body.String())
	}
	p := publicationHTTPNativePreview(t, f, m.ID)
	if w := historicalHTTPCall(f.base, "POST", "/v1/me/moments/"+m.ID+"/publication", publicationHTTPNativeBody(p), f.token); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, token := range []string{"", f.token} {
		w := historicalHTTPCall(f.base, "GET", path, "", token)
		if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("actual registered public detail", w.Code, w.Body.String())
		}
		var response struct{ Data map[string]any }
		if json.Unmarshal(w.Body.Bytes(), &response) != nil || len(response.Data) != 9 || response.Data["id"] != m.ID || response.Data["placeId"] != f.place {
			t.Fatal("closed native response", w.Body.String())
		}
		for _, canary := range []string{"occurredAt", "1999-01-01", f.account, "activityIds", "context", "memory"} {
			if strings.Contains(w.Body.String(), canary) {
				t.Fatal("private source leaked", canary)
			}
		}
	}
	for _, query := range []string{"?", "?ownerId=" + f.account, "?revision=1"} {
		if w := historicalHTTPCall(f.base, "GET", path+query, "", f.token); w.Code != 400 {
			t.Fatal("query accepted", query, w.Code)
		}
	}
	if w := historicalHTTPCall(f.base, "GET", path, `{"ownerId":"ignored"}`, f.token); w.Code != 400 {
		t.Fatal("GET body accepted", w.Code)
	}
	if w := historicalHTTPCall(f.base, "GET", path, "", "bts1_invalid"); w.Code != 401 {
		t.Fatal("bad session anonymous fallback", w.Code)
	}
	if w := historicalHTTPCall(f.base, "DELETE", "/v1/me/moments/"+m.ID+"?revision=2", "", f.token); w.Code != 204 {
		t.Fatal("actual withdrawal", w.Code, w.Body.String())
	}
	if w := historicalHTTPCall(f.base, "GET", path, "", ""); w.Code != 404 || strings.Contains(w.Body.String(), m.Title) {
		t.Fatal("withdrawn title exposed", w.Code, w.Body.String())
	}
}
