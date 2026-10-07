package nowcontextquery

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNowOnlineStrictInput(t *testing.T) {
	good := `{"contextId":"a1700000-0000-4000-8000-000000000001","query":"找阅读","taskId":"","expectedTaskUpdatedAt":""}`
	if _, e := Decode([]byte(good)); e != nil {
		t.Fatal(e)
	}
	for _, raw := range []string{strings.Replace(good, `"query":"找阅读"`, `"query":null`, 1), strings.Replace(good, `"query":"找阅读"`, `"query":"找阅读","query":"私人"`, 1), strings.Replace(good, `"query"`, `"Query"`, 1), strings.Replace(good, `"taskId":""`, `"taskId":"foreign"`, 1), strings.Replace(good, `"query":"找阅读"`, `"query":" "`, 1), strings.TrimSuffix(good, "}") + `,"ownerId":"forged"}`, good + `{}`} {
		if _, e := Decode([]byte(raw)); e == nil {
			t.Errorf("invalid input accepted: %s", raw)
		}
	}
	if _, e := json.Marshal(Access{}); e == nil {
		t.Fatal("access serialized")
	}
}
func TestNowOnlineRulesNoDistanceOrInference(t *testing.T) {
	if !Matches("帮我找线上阅读", "周末阅读") || Matches("阅读", "羽毛球") {
		t.Fatal("literal matching")
	}
	if !strings.Contains(Answer("近一点的呢？", 0, false), "不提供距离") {
		t.Fatal("invented online distance")
	}
	if !strings.Contains(Answer("阅读", 0, false), "没有找到") {
		t.Fatal("fake empty result")
	}
}
