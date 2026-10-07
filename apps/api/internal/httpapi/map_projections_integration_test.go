package httpapi

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMapProjectionHTTPNativeRegisteredPublicAndPrivateACL(t *testing.T) {
	f := publicationHTTPNative(t)
	f.base.exec(`UPDATE places SET coordinate_system='wgs84',location_precision='point',latitude=57.2,longitude=-2.1 WHERE id=$1`, f.place)
	m := publicationHTTPNativeDraft(t, f)
	p := publicationHTTPNativePreview(t, f, m.ID)
	if w := historicalHTTPCall(f.base, "POST", "/v1/me/moments/"+m.ID+"/publication", publicationHTTPNativeBody(p), f.token); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	bounds := "?west=-2.2&south=57.1&east=-2&north=57.3"
	public := "/v1/cities/" + f.city + "/map-layers" + bounds
	private := "/v1/me/cities/" + f.city + "/map-opportunities" + bounds
	for _, token := range []string{"", f.token} {
		w := historicalHTTPCall(f.base, "GET", public, "", token)
		if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("registered public", w.Code, w.Body.String())
		}
		var x struct {
			Data struct {
				SchemaVersion, Scope string
				Items                []struct {
					Kind, ID             string
					EntityRef, DetailRef struct{ Type, ID string }
				}
			}
		}
		if json.Unmarshal(w.Body.Bytes(), &x) != nil || x.Data.SchemaVersion != "typed-map-layers-v1" || x.Data.Scope != "PUBLIC" || len(x.Data.Items) != 2 {
			t.Fatal("closed projection", w.Body.String())
		}
		for _, i := range x.Data.Items {
			if i.ID != i.EntityRef.ID || i.ID != i.DetailRef.ID || i.Kind != i.EntityRef.Type {
				t.Fatal("stable entity identities", i)
			}
		}
		for _, canary := range []string{f.account, "1999-01-01", "只有本人检查后公開的正文", "creatorAccountId", "token_sha256", "xmin", "Proof", "Seal", "OPPORTUNITY"} {
			if strings.Contains(w.Body.String(), canary) {
				t.Fatal("private source leak", canary)
			}
		}
	}
	for _, path := range []string{public, private} {
		if w := historicalHTTPCall(f.base, "GET", path, "", "bts1_invalid"); w.Code != 401 {
			t.Fatal("invalid supplied auth anonymous fallback", w.Code)
		}
	}
	if w := historicalHTTPCall(f.base, "GET", private, "", ""); w.Code != 401 {
		t.Fatal("anonymous private", w.Code)
	}
	if w := historicalHTTPCall(f.base, "GET", private, "", f.token); w.Code != 200 || !strings.Contains(w.Body.String(), `"scope":"SELF_PRIVATE"`) {
		t.Fatal("ordinary owner without Machine grant", w.Code, w.Body.String())
	}
	for _, suffix := range []string{"&ownerId=" + f.account, "&west=-2.2", "&west=null", "&bad=%xx", "&bad=x;y"} {
		if w := historicalHTTPCall(f.base, "GET", public+suffix, "", f.token); w.Code != 400 {
			t.Fatal("strict query", suffix, w.Code, w.Body.String())
		}
	}
	if w := historicalHTTPCall(f.base, "GET", public, `{"ownerId":"ignored"}`, f.token); w.Code != 400 {
		t.Fatal("GET body accepted", w.Code)
	}
	if w := historicalHTTPCall(f.base, "DELETE", "/v1/me/moments/"+m.ID+"?revision=2", "", f.token); w.Code != 204 {
		t.Fatal("withdraw", w.Code, w.Body.String())
	}
	if w := historicalHTTPCall(f.base, "GET", public, "", ""); w.Code != 200 || strings.Contains(w.Body.String(), m.ID) {
		t.Fatal("withdrawn public marker", w.Code, w.Body.String())
	}
	f.base.exec(`UPDATE cities SET publication_status='draft' WHERE id=$1`, f.city)
	if w := historicalHTTPCall(f.base, "GET", public, "", f.token); w.Code != 404 {
		t.Fatal("hidden city", w.Code, w.Body.String())
	}
}
