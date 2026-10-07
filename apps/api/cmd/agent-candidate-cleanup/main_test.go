package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestCandidateCleanupRejectsNonLocalFallbacksAndAmbiguousConfig(t *testing.T) {
	good := "postgres://test:test@127.0.0.1:55432/owned?sslmode=disable"
	if _, e := localConfig(good, "development", "1"); e != nil {
		t.Fatal(e)
	}
	for _, raw := range []string{"", "host=localhost dbname=owned", strings.Replace(good, "127.0.0.1", "localhost", 1), strings.Replace(good, "127.0.0.1", "192.168.1.10", 1), strings.Replace(good, "127.0.0.1", "127.0.0.1,remote.example", 1), good + "&host=remote.example", good + "&options=-c%20session_replication_role%3Dreplica", good + "&sslmode=disable", strings.Replace(good, "disable", "prefer", 1), good + "#hidden"} {
		if _, e := localConfig(raw, "development", "1"); e == nil {
			t.Fatal("accepted", raw)
		}
	}
	for _, env := range []string{"", "production", "release", "staging"} {
		if _, e := localConfig(good, env, "1"); e == nil {
			t.Fatal(env)
		}
	}
	if _, e := localConfig(good, "development", "0"); e == nil {
		t.Fatal("not disposable")
	}
}
func TestCandidateCleanupBoundsAndNoSecretErrors(t *testing.T) {
	env := func(k string) string {
		if k == "BIRDTIE_DATABASE_URL" {
			return "PRIVATE_PASSWORD_DATABASE_CANARY"
		}
		return ""
	}
	for _, args := range [][]string{{"-batch", "0"}, {"-batch", "101"}, {"-deadline", "0s"}, {"-deadline", "1h"}, {"extra"}, {"-enable-model"}, {}} {
		var out bytes.Buffer
		e := run(context.Background(), args, env, &out)
		if e == nil || out.Len() != 0 || strings.Contains(e.Error(), "CANARY") {
			t.Fatal(args, out.String(), e)
		}
	}
	if e := run(context.Background(), nil, func(k string) string {
		if k == "PGHOST" {
			return "localhost"
		}
		return ""
	}, &bytes.Buffer{}); e == nil {
		t.Fatal("environment fallback")
	}
}
