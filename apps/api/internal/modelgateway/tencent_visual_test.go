package modelgateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type visualRoundTripper func(*http.Request) (*http.Response, error)

func (f visualRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func visualProviderResponse(t *testing.T) []byte {
	t.Helper()
	report := `{"summary":"合成画面","observations":[{"kind":"VISIBLE_TEXT","value":"hello","confidence":{"kind":"QUALITATIVE","level":"LOW"}}],"unknowns":["拍摄地点未知"]}`
	raw, e := json.Marshal(map[string]any{"id": "synthetic-visual", "model": TencentVisionModel, "object": "chat.completion", "choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": report}, "finish_reason": "stop"}}, "usage": map[string]int{"prompt_tokens": 300, "completion_tokens": 70, "total_tokens": 370}})
	if e != nil {
		t.Fatal(e)
	}
	return raw
}
func TestTencentVisualActualAdapterFixedURLAndNoReplay(t *testing.T) {
	calls, guards := 0, 0
	transport := visualRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.String() != tencentChatEndpoint || r.GetBody != nil || !r.Close || r.Header.Get("Authorization") != "Bearer SYNTHETIC-VISION-SECRET" {
			t.Fatal("transport escaped closed route")
		}
		b := make([]byte, 7)
		read := 0
		for {
			n, e := r.Body.Read(b)
			read += n
			if e == io.EOF {
				break
			}
			if e != nil {
				return nil, e
			}
		}
		if read != int(r.ContentLength) {
			t.Fatal("wire changed")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(string(visualProviderResponse(t))))}, nil
	})
	a, _ := NewTencentVisionAdapter(visualTestConfig(t), transport)
	p, _ := a.Prepare(visualTestRequest(t))
	result, e := a.complete(context.Background(), p, func(context.Context) error { guards++; return nil })
	if e != nil || result.Status != Completed || result.Report == nil || result.Usage.CostStatus != "UNKNOWN" || calls != 1 || guards < 10 {
		t.Fatalf("guarded complete %s %v calls=%d guards=%d", result.Status, e, calls, guards)
	}
}
func TestTencentVisualRejectsToolOutputWrongModelMalformedAndTruncation(t *testing.T) {
	base := string(visualProviderResponse(t))
	for _, raw := range []string{strings.Replace(base, TencentVisionModel, TencentTokenHubModel, 1), strings.Replace(base, `"role":"assistant"`, `"role":"assistant","tool_calls":[{"id":"x"}]`, 1), strings.Replace(base, `"stop"`, `"length"`, 1), strings.Replace(base, `"index":0`, `"index":0,"index":1`, 1), strings.Replace(base, `"prompt_tokens":300`, `"prompt_tokens":24577`, 1)} {
		if _, e := normalizeTencentVisual([]byte(raw), 768); e == nil {
			t.Fatal("unsafe provider output accepted")
		}
	}
}
func TestTencentVisualRefusalAndMissingUsageStayUnknownCost(t *testing.T) {
	raw := strings.Replace(string(visualProviderResponse(t)), `"stop"`, `"content_filter"`, 1)
	result, e := normalizeTencentVisual([]byte(raw), 768)
	if e != nil || result.Status != Refused || result.Report != nil || result.Usage.CostStatus != "UNKNOWN" {
		t.Fatal("refusal normalized as success")
	}
	var envelope map[string]any
	_ = json.Unmarshal(visualProviderResponse(t), &envelope)
	delete(envelope, "usage")
	b, _ := json.Marshal(envelope)
	result, e = normalizeTencentVisual(b, 768)
	if e != nil || result.Usage.Status != "UNKNOWN" || result.Usage.InputTokens != nil {
		t.Fatal("invented provider usage")
	}
}
