package main

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/legacyvideo"
	"strings"
	"testing"
)

func TestPrivateVideoCLIDefaultAndUnknownFacts(t *testing.T) {
	var b bytes.Buffer
	if e := run(context.Background(), []string{}, func(string) string { return "" }, &b); e == nil {
		t.Fatal("missing protected inputs accepted")
	}
	if e := writeSummary(&b, "original-run", "UNKNOWN", legacyvideo.Counts{Videos: 999}, nil); e != nil {
		t.Fatal(e)
	}
	var out map[string]any
	if json.Unmarshal(b.Bytes(), &out) != nil || out["counts"] != nil || out["mediaImported"] != nil || out["countsKnown"] != false {
		t.Fatal("unknown commit invented effect")
	}
	b.Reset()
	if writeSummary(&b, "original-run", "COMMITTED", legacyvideo.Counts{Videos: 5}, nil) != nil || !strings.Contains(b.String(), `"videosImported":true`) {
		t.Fatal("original committed receipt wrong")
	}
}
