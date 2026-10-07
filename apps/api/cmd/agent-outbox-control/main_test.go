package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentoutbox"
	"github.com/birdtie/birdtie/apps/api/internal/agentoutboxmaintenance"
	"github.com/jackc/pgx/v5/pgxpool"
)

const testPerson = "89000000-0000-4000-8000-000000000001"

func commandArgs() []string { return []string{"--local-development-only", "--subject", testPerson} }

type emptyStore struct{ calls int }

func (s *emptyStore) ClaimAgentOutboxControlForSubject(_ context.Context, p actorref.PrincipalRef, w string, h agentoutbox.HandlerVersion) (agentoutbox.Record, agentoutbox.Claim, error) {
	s.calls++
	return agentoutbox.Record{}, agentoutbox.Claim{}, agentoutbox.ErrNotFound
}
func (s *emptyStore) ConsumeAgentOutboxControl(context.Context, agentoutbox.Claim) (agentoutbox.ConsumerRecord, error) {
	panic("no claim can consume")
}

func TestAgentOutboxMaintenanceCommandStrictOptions(t *testing.T) {
	for name, args := range map[string][]string{
		"missing": nil, "noLocalAck": {"--subject", testPerson}, "unknown": {"--local-development-only", "--subject", testPerson, "--secret=PASSWORD"}, "actorAlias": {"--local-development-only", "--subject=8900000A-0000-4000-8000-000000000001"},
		"zero": {"--local-development-only", "--subject=00000000-0000-0000-0000-000000000000"}, "trailing": {"--local-development-only", "--subject", testPerson, "PASSWORD"}, "foreignHandler": {"--local-development-only", "--subject", testPerson, "--handler=real-candidate"},
		"zeroBatch": {"--local-development-only", "--subject", testPerson, "--batch=0"}, "hugeBatch": {"--local-development-only", "--subject", testPerson, "--batch=1001"}, "zeroTimeout": {"--local-development-only", "--subject", testPerson, "--timeout=0s"}, "hugeTimeout": {"--local-development-only", "--subject", testPerson, "--timeout=61s"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, ok := parseSettings(args); ok {
				t.Fatal("invalid options accepted")
			}
		})
	}
	if v, ok := parseSettings(commandArgs()); !ok || v.subject.ID != testPerson || v.batch != 25 || v.handler != agentoutbox.HandlerV1 {
		t.Fatal("default closed config rejected")
	}
}

func TestAgentOutboxMaintenanceCommandLocalDatabaseOnly(t *testing.T) {
	for name, url := range map[string]string{"missing": "", "remote": "postgres://user:SECRET@remote.example/db", "unixSocket": "host=/tmp user=user dbname=db", "remoteFallback": "host=127.0.0.1,remote.example user=user dbname=db sslmode=disable", "badSyntax": "postgres://SECRET@[[[", "spaces": " postgres://localhost/db"} {
		t.Run(name, func(t *testing.T) {
			if _, ok := localPoolConfig(url); ok {
				t.Fatal("nonlocal/invalid DSN accepted")
			}
		})
	}
	for _, url := range []string{"postgres://localhost/db?sslmode=disable", "postgres://127.0.0.1/db?sslmode=disable", "postgres://[::1]/db?sslmode=disable"} {
		cfg, ok := localPoolConfig(url)
		if !ok {
			t.Fatal("valid local rejected")
		}
		if _, e := cfg.ConnConfig.LookupFunc(context.Background(), "remote.example"); e == nil {
			t.Fatal("remote lookup accepted")
		}
		if _, e := cfg.ConnConfig.DialFunc(context.Background(), "tcp", "192.0.2.1:5432"); e == nil {
			t.Fatal("remote dial accepted")
		}
	}
}

func TestAgentOutboxMaintenanceCommandAggregationAndPrivateErrors(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "databaseFailure"}[failed], func(t *testing.T) {
			var stdout bytes.Buffer
			calls, closed := 0, 0
			s := &emptyStore{}
			factory := func(context.Context, *pgxpool.Config) (agentoutboxmaintenance.Store, func(), error) {
				calls++
				if failed {
					return nil, func() { closed++ }, errors.New("SECRET_PASSWORD_DSN_PRIVATE_BODY")
				}
				return s, func() { closed++ }, nil
			}
			code := runCommand(context.Background(), commandArgs(), func(string) string { return "postgres://user:SECRET@127.0.0.1/db?sslmode=disable" }, &stdout, factory)
			var result agentoutboxmaintenance.Report
			if json.Unmarshal(stdout.Bytes(), &result) != nil || strings.Contains(stdout.String(), "SECRET") || strings.Contains(stdout.String(), testPerson) {
				t.Fatal("unsafe/invalid aggregate output")
			}
			if calls != 1 || closed != 1 || result.ConfirmedReceipts != 0 || result.BusinessExecution != "UNAVAILABLE" {
				t.Fatal(result, calls, closed)
			}
			if failed {
				if code != 1 || result.Reason != "DATABASE_UNAVAILABLE" || s.calls != 0 {
					t.Fatal(result)
				}
			} else if code != 0 || result.Reason != "NO_ELIGIBLE_WORK" || s.calls != 1 {
				t.Fatal(result)
			}
		})
	}
}

func TestAgentOutboxMaintenanceCommandConfigurationBeforeDatabase(t *testing.T) {
	for name, url := range map[string]string{"absent": "", "remote": "postgres://user:SECRET@remote.example/db", "malformed": "password=SECRET host="} {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			calls := 0
			factory := func(context.Context, *pgxpool.Config) (agentoutboxmaintenance.Store, func(), error) {
				calls++
				return &emptyStore{}, nil, nil
			}
			code := runCommand(context.Background(), commandArgs(), func(string) string { return url }, &out, factory)
			if code != 2 || calls != 0 || strings.Contains(out.String(), "SECRET") {
				t.Fatal("configuration reached DB or leaked input")
			}
		})
	}
}

func TestAgentOutboxMaintenanceCommandGeneratedWorkerIsCanonical(t *testing.T) {
	a, e := newWorkerID()
	b, e2 := newWorkerID()
	if _, e3 := agentoutbox.NormalizeWorkerID(a); e != nil || e2 != nil || e3 != nil || a == b {
		t.Fatal("worker identity unavailable")
	}
}
