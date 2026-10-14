package safety

import (
	"strings"
	"testing"
)

func TestReportControlAppendOnlyNoInventedCAS(t *testing.T) {
	op := "22222222-2222-2222-2222-222222222222"
	body := `{"operationId":"` + op + `","action":"SUBMIT","targetType":"general","targetId":"","reason":"technical","details":"这是一份足够详细的故障说明"}`
	v, e := DecodeReportControl([]byte(body), false)
	if e != nil {
		t.Fatal(e)
	}
	digest := ReportControlDigest(v)
	v.Details += "，另一问题"
	if digest == ReportControlDigest(v) {
		t.Fatal("details not bound")
	}
	for n, b := range map[string]string{"fakeRevision": strings.Replace(body, `"action":"SUBMIT"`, `"action":"SUBMIT","expectedVersion":0`, 1), "edit": strings.Replace(body, `"SUBMIT"`, `"EDIT"`, 1), "withdraw": strings.Replace(body, `"SUBMIT"`, `"WITHDRAW"`, 1), "short": strings.Replace(body, `这是一份足够详细的故障说明`, `短`, 1), "nonnullGeneralTarget": strings.Replace(body, `"targetId":""`, `"targetId":"`+op+`"`, 1), "duplicate": strings.Replace(body, `"reason":"technical"`, `"reason":"technical","reason":"other"`, 1)} {
		t.Run(n, func(t *testing.T) {
			if _, e := DecodeReportControl([]byte(b), false); e == nil {
				t.Fatal("unsafe report accepted")
			}
		})
	}
}
