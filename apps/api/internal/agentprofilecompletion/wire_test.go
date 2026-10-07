package agentprofilecompletion

import (
	"strings"
	"testing"
)

func TestProfileMemoryCompletionClosedWire(t *testing.T) {
	good := `{"previewId":"` + testPreview + `","memoryId":"` + testMemory + `","memoryVersion":1,"expectedProfileVersion":1}`
	if _, e := DecodePreviewInput([]byte(good)); e != nil {
		t.Fatal(e)
	}
	for _, raw := range []string{"", `null`, `[]`, good + good, strings.Replace(good, `"memoryVersion":1`, `"memoryVersion":1,"memoryVersion":1`, 1), strings.Replace(good, `"memoryVersion":1`, `"memoryVersion":null`, 1), strings.Replace(good, `"memoryVersion":1`, `"memoryVersion":1.0`, 1), strings.Replace(good, `"memoryVersion":1`, `"memoryVersion":"1"`, 1), strings.Replace(good, `"memoryVersion":1`, `"memoryVersion":0`, 1), strings.Replace(good, `"memoryVersion":1`, `"memoryVersion":9223372036854775808`, 1), strings.Replace(good, `"expectedProfileVersion":1`, `"expectedProfileVersion":9223372036854775807`, 1), strings.Replace(good, `"expectedProfileVersion":1`, `"ownerId":"`+testOwner+`","expectedProfileVersion":1`, 1), strings.Replace(good, `"expectedProfileVersion":1`, `"confirmed":true,"expectedProfileVersion":1`, 1), strings.Replace(good, `"expectedProfileVersion":1`, `"after":["guess"],"expectedProfileVersion":1`, 1), strings.Repeat("a", MaxBodyBytes+1)} {
		if _, e := DecodePreviewInput([]byte(raw)); e == nil {
			t.Fatal("nonclosed preview input accepted")
		}
	}
	accept := `{"planDigest":"` + strings.Repeat("a", 64) + `"}`
	if _, e := DecodeAcceptInput([]byte(accept)); e != nil {
		t.Fatal(e)
	}
	for _, raw := range []string{`{}`, `{"planDigest":null}`, `{"planDigest":1}`, strings.Replace(accept, `"planDigest":`, `"confirmed":true,"planDigest":`, 1), strings.Replace(accept, "a", "A", 1), accept + `null`, strings.Replace(accept, `}`, `,"planDigest":"`+strings.Repeat("a", 64)+`"}`, 1)} {
		if _, e := DecodeAcceptInput([]byte(raw)); e == nil {
			t.Fatal("nonclosed accept input accepted")
		}
	}
}
