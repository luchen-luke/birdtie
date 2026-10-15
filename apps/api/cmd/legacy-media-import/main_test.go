package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/legacymedia"
)

func TestPrivateBlobAndAggregateOutputGuards(t *testing.T) {
	dir := t.TempDir()
	raw := []byte("synthetic selected object, no private user material")
	hash := legacymedia.Hash(raw)
	path := filepath.Join(dir, hash+".blob")
	if e := os.WriteFile(path, raw, 0600); e != nil {
		t.Fatal(e)
	}
	r, e := openPrivateBlob(dir, legacymedia.Record{InputSHA256: hash, InputBytes: int64(len(raw))})
	if e != nil {
		t.Fatal("bounded source rejected", e)
	}
	got, e := io.ReadAll(r)
	closed := r.Close()
	if e != nil || closed != nil || !bytes.Equal(raw, got) {
		t.Fatal("bounded source changed")
	}
	if _, e = openPrivateBlob(dir, legacymedia.Record{InputSHA256: "../escape", InputBytes: 1}); e == nil {
		t.Fatal("selector path traversal accepted")
	}
	if _, e = openPrivateBlob(dir, legacymedia.Record{InputSHA256: hash, InputBytes: 1}); e == nil {
		t.Fatal("source size mismatch accepted")
	}
	var out bytes.Buffer
	if e = writeSummary(&out, "15700000-0000-4000-8000-000000000001", "UNKNOWN", legacymedia.Counts{Images: 135}, nil); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(out.String(), "135") || strings.Contains(out.String(), "\"counts\"") {
		t.Fatal("unknown commit advertised effects")
	}
	out.Reset()
	if e = writeSummary(&out, "15700000-0000-4000-8000-000000000001", "COMMITTED", legacymedia.Counts{Images: 1}, map[string]int{"private_videos_not_adapted": 5}); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(out.String(), "\"mediaImported\":true") || !strings.Contains(out.String(), "\"videosImported\":false") {
		t.Fatal("partial media status misleading")
	}
}
