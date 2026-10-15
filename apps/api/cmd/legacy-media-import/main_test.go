package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/legacyimagerecovery"
	"github.com/birdtie/birdtie/apps/api/internal/legacymedia"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
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

func TestRecoverySummaryAndFlagIsolation(t *testing.T) {
	var out bytes.Buffer
	if e := writeRecoverySummary(&out, postgres.LegacyPrivateImageRecoveryResult{Status: "UNKNOWN", Counts: legacyimagerecovery.Counts{Images: 31}}); e != nil || strings.Contains(out.String(), "31") || strings.Contains(out.String(), "\"counts\"") {
		t.Fatal("unknown recovery claims effects")
	}
	if e := run(context.Background(), []string{"--heif-convert", "unexpected"}, func(string) string { return "" }, &out); e == nil {
		t.Fatal("recovery decoder options silently accepted by original157 mode")
	}
	if e := run(context.Background(), []string{"--metadata-recovery", "--apply", "--verify-blobs"}, func(string) string { return "" }, &out); e == nil {
		t.Fatal("verify mixed with mutation")
	}
}
