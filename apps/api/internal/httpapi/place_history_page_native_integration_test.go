package httpapi

import (
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

type pageNativeHTTPWire struct {
	Count         int
	SourceVersion string
	NextCursor    *string
	ValidUntil    time.Time
	Items         []struct {
		ID                  string
		AuthorID            *string
		AuthorPublicProfile *json.RawMessage
	}
}

func TestPlaceHistoryPageHTTPNativeRegisteredTwoPages(t *testing.T) {
	if os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" || os.Getenv("BIRDTIE_DATABASE_URL") == "" {
		t.Skip("explicit owned page HTTP native runner required")
	}
	cfg, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil || cfg.ConnConfig.Host != "127.0.0.1" || cfg.ConnConfig.Port != 55432 || !regexp.MustCompile(`^birdtie_nowv4_history_[0-9a-f]{12}$`).MatchString(cfg.ConnConfig.Database) || cfg.ConnConfig.Database != os.Getenv("BIRDTIE_PLACE_HISTORY_DATABASE") {
		t.Fatal("refuse non-owned page HTTP parent")
	}
	f := publicationHTTPNative(t)
	for i := 0; i < 7; i++ {
		m := publicationHTTPNativeDraft(t, f)
		p := publicationHTTPNativePreview(t, f, m.ID)
		w := historicalHTTPCall(f.base, "POST", "/v1/me/moments/"+m.ID+"/publication", publicationHTTPNativeBody(p), f.token)
		if w.Code != 200 {
			t.Fatal("original confirmed publication", w.Code)
		}
	}
	path := "/v1/places/" + f.place + "/social-history/moments?include=public-author"
	first := historicalHTTPCall(f.base, "GET", path, "", f.token)
	if first.Code != 200 {
		t.Fatal("registered first page", first.Code, first.Body.String())
	}
	var one struct{ Data pageNativeHTTPWire }
	if json.Unmarshal(first.Body.Bytes(), &one) != nil || len(one.Data.Items) != 5 || one.Data.NextCursor == nil {
		t.Fatal("first native page malformed")
	}
	next := historicalHTTPCall(f.base, "GET", path+"&cursor="+url.QueryEscape(*one.Data.NextCursor), "", f.token)
	var two struct{ Data pageNativeHTTPWire }
	if next.Code != 200 || json.Unmarshal(next.Body.Bytes(), &two) != nil || len(two.Data.Items) != 2 || two.Data.NextCursor != nil || two.Data.Count != 7 || two.Data.SourceVersion != one.Data.SourceVersion || !two.Data.ValidUntil.Equal(one.Data.ValidUntil) {
		t.Fatal("registered continuation", next.Code, next.Body.String())
	}
	for _, raw := range []string{first.Body.String(), next.Body.String()} {
		if strings.Contains(raw, "occurredAt") || strings.Contains(raw, "privateImages") {
			t.Fatal("private publication metadata widened")
		}
	}
	// These ordinary publishers have no explicitly public Profile label. Text
	// stays public while the original author pair remains null, never owner ID.
	for _, p := range []pageNativeHTTPWire{one.Data, two.Data} {
		for _, m := range p.Items {
			if m.AuthorID != nil || m.AuthorPublicProfile != nil {
				t.Fatal("ordinary owner exposed as public identity")
			}
		}
	}
	// Optional root-owned evidence stores the exact registered native bytes.
	// It is a historical wire sample, never a renewed public access grant.
	if directory := os.Getenv("BIRDTIE_PAGE_WIRE_EVIDENCE_DIR"); directory != "" {
		if !filepath.IsAbs(directory) || !regexp.MustCompile(`^page-wire-native-[0-9a-f]{12}$`).MatchString(filepath.Base(directory)) {
			t.Fatal("refuse unowned wire evidence directory")
		}
		for name, raw := range map[string][]byte{"first.json": first.Body.Bytes(), "next.json": next.Body.Bytes()} {
			file, err := os.OpenFile(filepath.Join(directory, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if err != nil {
				t.Fatal("owned immutable wire evidence", err)
			}
			_, err = file.Write(raw)
			closeErr := file.Close()
			if err != nil || closeErr != nil {
				t.Fatal("native wire capture failed")
			}
		}
	}
}
