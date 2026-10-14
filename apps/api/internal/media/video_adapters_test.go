package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestVideoMaterialHTTPAdaptersQuarantinePrivateRevokeAndScan(t *testing.T) {
	ctx := context.Background()
	b := videoTestBytes()
	h := sha256.Sum256(b)
	expires := time.Now().UTC().Add(time.Minute)
	var base string
	private := []byte(nil)
	revoked, deleted := 0, 0
	verdict := "clean"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer synthetic-service-token" {
			t.Error("missing service credential")
			w.WriteHeader(401)
			return
		}
		switch r.Method + " " + r.URL.Path {
		case "POST /v1/quarantine-write-intents":
			var v map[string]any
			_ = json.NewDecoder(r.Body).Decode(&v)
			if v["objectKey"] != "opaque-q" || v["sha256"] != hex.EncodeToString(h[:]) {
				t.Error("signing proof not bound")
			}
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(map[string]any{"url": base + "/put-proof", "headers": map[string]string{"X-Upload-Proof": "synthetic-proof"}, "expiresAt": expires})
		case "GET /v1/quarantine/opaque-q":
			w.Write(b)
		case "PUT /v1/private/opaque-p":
			private, _ = io.ReadAll(r.Body)
			w.WriteHeader(201)
		case "GET /v1/private/opaque-p":
			w.Write(private)
		case "DELETE /v1/quarantine-write-intents/" + videoTestID:
			revoked++
			w.WriteHeader(204)
		case "DELETE /v1/quarantine/opaque-q", "DELETE /v1/private/opaque-p":
			deleted++
			w.WriteHeader(204)
		case "POST /scan":
			read, _ := io.ReadAll(r.Body)
			if !bytes.Equal(read, b) {
				t.Error("scanner received changed bytes")
			}
			json.NewEncoder(w).Encode(map[string]string{"verdict": verdict})
		default:
			t.Error("unexpected provider route", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer ts.Close()
	base = ts.URL
	st := &HTTPVideoStorage{BaseURL: base, UploadOrigin: base, Token: "synthetic-service-token", Development: true}
	plan, e := st.SignQuarantineWrite(ctx, UploadIntent{ID: videoTestID, OwnerID: videoTestOwner, ObjectKey: "opaque-q", MIMEType: "video/mp4", MaxBytes: int64(len(b)), SHA256Hex: hex.EncodeToString(h[:]), ExpiresAt: expires})
	if e != nil || plan.URL != base+"/put-proof" {
		t.Fatal(plan, e)
	}
	q, info, e := st.OpenQuarantine(ctx, "opaque-q")
	if e != nil {
		t.Fatal(e)
	}
	got, _ := io.ReadAll(q)
	q.Close()
	if !bytes.Equal(got, b) || info.ByteSize != int64(len(b)) {
		t.Fatal("quarantine size mismatch")
	}
	if e = st.PutPrivate(ctx, "opaque-p", bytes.NewReader(b)); e != nil {
		t.Fatal(e)
	}
	p, _, e := st.OpenPrivate(ctx, "opaque-p")
	if e != nil {
		t.Fatal(e)
	}
	got, _ = io.ReadAll(p)
	p.Close()
	if !bytes.Equal(got, b) {
		t.Fatal("private bytes changed")
	}
	sc := &HTTPVideoScanner{URL: base + "/scan", Token: "synthetic-service-token", Development: true}
	if v, e := sc.Scan(ctx, bytes.NewReader(b)); e != nil || v != ScanClean {
		t.Fatal(v, e)
	}
	verdict = "pending"
	if v, e := sc.Scan(ctx, bytes.NewReader(b)); e == nil || v == ScanClean {
		t.Fatal("scanner pending is clean")
	}
	if st.RevokeQuarantineWrite(ctx, videoTestID) != nil || st.DeleteQuarantine(ctx, "opaque-q") != nil || st.DeletePrivate(ctx, "opaque-p") != nil || revoked != 1 || deleted != 2 {
		t.Fatal("cleanup not completed")
	}
	if st.PutPublicDerivative(ctx, "public", bytes.NewReader(b)) == nil {
		t.Fatal("private video granted public write")
	}
}
func TestVideoMaterialAdaptersDenyRedirectsUntrustedOriginsAndDefaultEnable(t *testing.T) {
	hits := 0
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++; w.WriteHeader(200) }))
	defer receiver.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, receiver.URL, 307) }))
	defer redirect.Close()
	st := &HTTPVideoStorage{BaseURL: redirect.URL, Token: "test-token", Development: true}
	if _, _, e := st.OpenQuarantine(context.Background(), "opaque"); e == nil || hits != 0 {
		t.Fatal("followed redirect", e, hits)
	}
	for _, u := range []string{"http://example.com", "https://user:secret@example.com", "https://example.com?q=1", "file:///tmp/video", "https://example.com#fragment"} {
		if _, e := videoServiceURL(u, false); e == nil {
			t.Fatal("unsafe service origin", u)
		}
	}
	for _, key := range []string{"../x", "/absolute", "x\\y", "x?y", ""} {
		if videoObjectKey(key) {
			t.Fatal("unsafe object key")
		}
	}
	r, e := VideoRuntimeFromEnvironment(func(string) string { return "" })
	if e != nil || r.Available() {
		t.Fatal("default enabled", e)
	}
	r, e = VideoRuntimeFromEnvironment(func(k string) string {
		if k == "BIRDTIE_VIDEO_ENABLED" {
			return "1"
		}
		return ""
	})
	if e == nil || r.Available() {
		t.Fatal("missing provider configuration enabled")
	}
}
func TestVideoMaterialFFmpegRealDecodeAndClosedLimits(t *testing.T) {
	decoder, e := exec.LookPath("ffmpeg")
	if e != nil {
		t.Skip("NOT_RUN: ffmpeg not installed")
	}
	probe, e := exec.LookPath("ffprobe")
	if e != nil {
		t.Skip("NOT_RUN: ffprobe not installed")
	}
	decoder, _ = filepath.Abs(decoder)
	probe, _ = filepath.Abs(probe)
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	dir := t.TempDir()
	file := filepath.Join(dir, "synthetic.mp4")
	cmd := exec.CommandContext(ctx, decoder, "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "color=c=blue:s=64x64:r=10", "-t", "1", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-movflags", "+faststart", "-y", file)
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("synthetic encode failed: %v %s", e, out)
	}
	b, e := os.ReadFile(file)
	if e != nil {
		t.Fatal(e)
	}
	inspector := &FFmpegVideoInspector{FFprobePath: probe, FFmpegPath: decoder, TempDir: dir}
	d, e := inspector.Inspect(ctx, b)
	if e != nil || d.Width != 64 || d.Height != 64 || d.DurationMS != 1000 {
		t.Fatal("real decoder", d, e)
	}
	damaged := append([]byte(nil), b...)
	// Keep valid MP4 box lengths/metadata but destroy its actual sample payload.
	for i := 8; i < len(damaged)-4; i++ {
		if string(damaged[i:i+4]) == "mdat" {
			for j := i + 4; j < len(damaged); j++ {
				damaged[j] = 0
			}
			break
		}
	}
	if ValidateMP4Signature(damaged) != nil {
		t.Fatal("damage should leave signature intact")
	}
	if _, e = inspector.Inspect(ctx, damaged); e == nil {
		t.Fatal("signature-only validation skipped real decode")
	}
	entries, e := os.ReadDir(dir)
	if e != nil || len(entries) != 1 {
		t.Fatal("temporary private original leaked", entries, e)
	}
}
