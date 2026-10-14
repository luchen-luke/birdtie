package organization

import (
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"strings"
	"testing"
)

func TestFAQControlClosedInputAndTerminalRevision(t *testing.T) {
	org := "11111111-1111-1111-1111-111111111111"
	op := "22222222-2222-2222-2222-222222222222"
	body := `{"operationId":"` + op + `","action":"CREATE","faqId":"","expectedVersion":0,"question":"开门时间","answer":"上午九点","published":false}`
	v, e := DecodeFAQControl([]byte(body), org, false)
	if e != nil || v.Published == nil || *v.Published || v.ExpectedVersion != 0 {
		t.Fatal(v, e)
	}
	original := FAQControlDigest(org, v)
	v.Answer = "上午十点"
	if FAQControlDigest(org, v) == original {
		t.Fatal("specific answer not approved")
	}
	v.Answer = "上午九点"
	if FAQControlDigest(op, v) == original {
		t.Fatal("organization not bound")
	}
	for n, b := range map[string]string{"missingFalse": strings.Replace(body, `,"published":false`, "", 1), "duplicate": strings.Replace(body, `"published":false`, `"published":false,"published":true`, 1), "unknown": strings.Replace(body, `"published":false`, `"published":false,"role":"owner"`, 1), "null": strings.Replace(body, `"faqId":""`, `"faqId":null`, 1), "privateContext": strings.Replace(body, `"action":"CREATE"`, `"action":"CREATE","sessionGeneration":9`, 1)} {
		t.Run(n, func(t *testing.T) {
			if _, e := DecodeFAQControl([]byte(b), org, false); e == nil {
				t.Fatal("unsafe body accepted")
			}
		})
	}
	delete := FAQControlInput{OperationID: op, Action: "DELETE", FAQID: org, ExpectedVersion: identity.HumanControlMaxVersion}
	if _, e := NormalizeFAQControl(org, delete, false); e != nil {
		t.Fatal("terminal deletion unreachable", e)
	}
	delete.Action = "UPDATE"
	delete.Published = v.Published
	delete.Question = v.Question
	delete.Answer = v.Answer
	if _, e := NormalizeFAQControl(org, delete, false); e == nil {
		t.Fatal("nonterminal revision overflow")
	}
}
