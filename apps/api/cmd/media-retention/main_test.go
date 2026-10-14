package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestPlacePhotoRetentionCLIDefaultOffAndExactOwnedConfiguration(t *testing.T) {
	var out bytes.Buffer
	if e := run(context.Background(), nil, func(string) string { return "" }, &out); e != nil || !strings.Contains(out.String(), "DISABLED") {
		t.Fatal("default opened maintenance", e)
	}
	db, marker := "birdtie_owned_migration_fixture", "birdtie-human-media-owned:fixture"
	valid := "postgres://local:synthetic@127.0.0.1/" + db + "?sslmode=disable"
	if _, e := retentionConfig(valid, "development", "1", db, marker); e != nil {
		t.Fatal("explicit owned configuration", e)
	}
	for _, u := range []string{strings.Replace(valid, "127.0.0.1", "example.invalid", 1), strings.Replace(valid, db, "postgres", 1), valid + "&options=unsafe"} {
		if _, e := retentionConfig(u, "development", "1", db, marker); e == nil {
			t.Fatal("configuration weakened ownership")
		}
	}
	env := map[string]string{"BIRDTIE_MEDIA_RETENTION_ENABLED": "1", "PGOPTIONS": "-c unsafe=1"}
	if e := run(context.Background(), nil, func(k string) string { return env[k] }, &out); e == nil {
		t.Fatal("ambiguous PG environment allowed")
	}
}
