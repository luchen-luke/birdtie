package modelgateway

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"strings"
	"testing"
	"time"
)

func visualTestRequest(t *testing.T) VisualRequest {
	t.Helper()
	var pixels bytes.Buffer
	if e := png.Encode(&pixels, image.NewRGBA(image.Rect(0, 0, 4, 4))); e != nil {
		t.Fatal(e)
	}
	h := sha256.Sum256(pixels.Bytes())
	return VisualRequest{Pixels: pixels.Bytes(), MIME: "image/png", SHA256: hex.EncodeToString(h[:]), Width: 4, Height: 4, DeadlineAt: time.Now().Add(time.Minute), MaxOutputTokens: 768}
}
func visualTestConfig(t *testing.T) TencentVisionConfig {
	t.Helper()
	c, e := ParseTencentVisionConfig([]byte(`{"provider":"tencent_tokenhub","baseUrl":"https://tokenhub.tencentmaas.com/v1","model":"hy-vision-2.0-instruct","region":"APAC","maxOutputTokens":768}`), "SYNTHETIC-VISION-SECRET")
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func TestVisualPreparedPureImageAndTextUseSameClosedArray(t *testing.T) {
	for _, text := range []string{"", "读取这张图片里的字"} {
		a, e := NewTencentVisionAdapter(visualTestConfig(t), nil)
		if e != nil {
			t.Fatal(e)
		}
		r := visualTestRequest(t)
		r.UserText = text
		p, e := a.Prepare(r)
		if e != nil {
			t.Fatal(e)
		}
		var wire map[string]json.RawMessage
		if json.Unmarshal(p.ExactWire(), &wire) != nil || len(wire) != 6 {
			t.Fatal("unexpected wire fields")
		}
		var messages []struct {
			Role    string
			Content json.RawMessage
		}
		if json.Unmarshal(wire["messages"], &messages) != nil || len(messages) != 2 || messages[1].Role != "user" {
			t.Fatal("messages")
		}
		var content []map[string]any
		if json.Unmarshal(messages[1].Content, &content) != nil || len(content) != 1+boolInt(text != "") {
			t.Fatal("pure image is not a synthetic question")
		}
		if content[0]["type"] != "image_url" || !strings.HasPrefix(content[0]["image_url"].(map[string]any)["url"].(string), "data:image/png;base64,") {
			t.Fatal("actual pixels absent")
		}
		if text != "" && (content[1]["type"] != "text" || content[1]["text"] != text) {
			t.Fatal("text changed")
		}
		exact := p.ExactWire()
		exact[0] = '!'
		r.Pixels[0] = 0
		if !p.valid(time.Now()) {
			t.Fatal("preparation mutated")
		}
		for _, v := range []any{p, a, visualTestConfig(t)} {
			if strings.Contains(fmt.Sprintf("%+#v", v), "SYNTHETIC-VISION-SECRET") {
				t.Fatal("secret format")
			}
		}
		if _, e = json.Marshal(p); e == nil {
			t.Fatal("preparation serialized")
		}
		if p.InputTokenBound() != 24576 || p.Descriptor().ModelID == TencentTokenHubModel {
			t.Fatal("borrowed TEXT capability")
		}
	}
}
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
func TestVisualPrepareRejectsHashDimensionsMIMEAndUnboundedInput(t *testing.T) {
	a, _ := NewTencentVisionAdapter(visualTestConfig(t), nil)
	for _, edit := range []func(*VisualRequest){func(r *VisualRequest) { r.SHA256 = strings.Repeat("0", 64) }, func(r *VisualRequest) { r.Width = 5 }, func(r *VisualRequest) { r.MIME = "image/svg+xml" }, func(r *VisualRequest) { r.UserText = strings.Repeat("字", 81) }, func(r *VisualRequest) { r.DeadlineAt = time.Now().Add(3 * time.Minute) }, func(r *VisualRequest) { r.MaxOutputTokens = 769 }} {
		r := visualTestRequest(t)
		edit(&r)
		if _, e := a.Prepare(r); e == nil {
			t.Fatal("invalid visual input accepted")
		}
	}
}
func TestVisualReportRejectsModelSourceTimeFactsAndDuplicateKeys(t *testing.T) {
	good := `{"summary":"图中可见一个合成对象","observations":[{"kind":"VISIBLE_OBJECT","value":"合成对象","confidence":{"kind":"QUALITATIVE","level":"UNKNOWN"}}],"unknowns":["地点未知"]}`
	if _, e := parseVisualReport([]byte(good)); e != nil {
		t.Fatal(e)
	}
	for _, raw := range []string{strings.Replace(good, `"value":"合成对象"`, `"value":"合成对象","source":{"type":"CONFIRMED_GPS"}`, 1), strings.Replace(good, `"unknowns":`, `"observedAt":"2026-01-01T00:00:00Z","unknowns":`, 1), strings.Replace(good, `"UNKNOWN"`, `0.99`, 1), strings.Replace(good, `"summary":`, `"summary":"duplicate","summary":`, 1)} {
		if _, e := parseVisualReport([]byte(raw)); e == nil {
			t.Fatal("model manufactured native facts")
		}
	}
}
func TestVisualConfigNeverAcceptsOldTextRouteOrEmbeddedKey(t *testing.T) {
	for _, raw := range []string{`{"provider":"tencent_tokenhub","baseUrl":"https://tokenhub.tencentmaas.com/v1","model":"hy3","region":"CN","maxOutputTokens":768}`, `{"provider":"tencent_tokenhub","baseUrl":"https://attacker.test","model":"hy-vision-2.0-instruct","region":"CN","maxOutputTokens":768}`, `{"provider":"tencent_tokenhub","baseUrl":"https://tokenhub.tencentmaas.com/v1","model":"hy-vision-2.0-instruct","region":"CN","maxOutputTokens":768,"apiKey":"secret"}`} {
		if _, e := ParseTencentVisionConfig([]byte(raw), "synthetic"); e == nil {
			t.Fatal("invalid trusted config accepted")
		}
	}
	if _, e := LoadTencentVisionConfigFromEnvironment("ignored-path", func(string) (string, bool) { return "", false }); e == nil {
		t.Fatal("credential fallback")
	}
}
