package main

import (
	"bytes"
	"context"
	"errors"
	ns "github.com/birdtie/birdtie/apps/api/internal/agentnotificationschedule"
	"strings"
	"testing"
)

type cliUnitStore struct {
	r, d      int
	digestErr error
}

func (s *cliUnitStore) EnqueueStartsSoonReminders(context.Context) (int64, error) {
	s.r++
	return 2, nil
}
func (s *cliUnitStore) ProcessNotificationSchedules(context.Context, int) (ns.RunResult, error) {
	s.d++
	return ns.RunResult{Owners: 1, Slots: 1, Delivered: 1}, s.digestErr
}
func TestNotificationScheduleUnitCLI(t *testing.T) {
	env := func(k string) string {
		if k == "BIRDTIE_DATABASE_URL" {
			return "PRIVATE_DSN_CANARY"
		}
		return ""
	}
	for _, tc := range []struct {
		name      string
		digestErr error
	}{{"success", nil}, {"AIR_failure", errors.New("PRIVATE_ERROR_CANARY")}} {
		t.Run(tc.name, func(t *testing.T) {
			s := &cliUnitStore{digestErr: tc.digestErr}
			closed := false
			var out bytes.Buffer
			e := runWithConnector(context.Background(), nil, env, &out, func(context.Context, string) (scheduleCycleStore, func(), error) {
				return s, func() { closed = true }, nil
			})
			if s.r != 1 || s.d != 1 || !closed || strings.Contains(out.String(), "CANARY") || !strings.Contains(out.String(), "reminders_ok=true reminder_inserted=2") {
				t.Fatal(out.String(), e)
			}
			if tc.digestErr != nil && e != errRun || tc.digestErr == nil && e != nil {
				t.Fatal(e)
			}
			if tc.digestErr != nil && (!strings.Contains(out.String(), "digest_counts_known=false") || !strings.Contains(out.String(), "inbox_inserted=UNKNOWN")) {
				t.Fatal("failure is not zero effects", out.String())
			}
		})
	}
	for _, args := range [][]string{{"-owners", "0"}, {"-owners", "101"}, {"-deadline", "0s"}, {"-deadline", "1h"}, {"-enable-model"}, {"extra"}} {
		called := false
		e := runWithConnector(context.Background(), args, env, &bytes.Buffer{}, func(context.Context, string) (scheduleCycleStore, func(), error) { called = true; return nil, nil, nil })
		if e != errConfiguration || called {
			t.Fatal(args, e)
		}
	}
	if e := runWithConnector(context.Background(), nil, func(k string) string {
		if k == "PGHOST" {
			return "remote"
		}
		return env(k)
	}, &bytes.Buffer{}, connect); e != errConfiguration {
		t.Fatal("reject ambiguous fallback")
	}
	var out bytes.Buffer
	if e := runWithConnector(context.Background(), nil, env, &out, func(context.Context, string) (scheduleCycleStore, func(), error) {
		return nil, nil, errors.New("PRIVATE_DSN_CANARY")
	}); e != errRun || out.Len() != 0 {
		t.Fatal("sanitized connect failure")
	}
}
