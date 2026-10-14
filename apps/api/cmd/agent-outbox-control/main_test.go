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

type mixedCommandStore struct {
	emptyStore
	mixedCalls int
}

func (s *mixedCommandStore) ClaimMixedAgentOutboxControlForSubject(context.Context, actorref.PrincipalRef, string) (agentoutbox.Record, agentoutbox.Claim, error) {
	s.mixedCalls++
	return agentoutbox.Record{}, agentoutbox.Claim{}, agentoutbox.ErrNotFound
}
func TestMixedMaintenanceUnitCommandExplicitModeAndClosedOptions(t *testing.T) {
	args := append(commandArgs(), "--mixed-handlers")
	settings, ok := parseSettings(args)
	if !ok || !settings.mixedHandlers || settings.handler != agentoutbox.HandlerV1 || settings.subject.ID != testPerson {
		t.Fatal("explicit mixed mode not captured")
	}
	if defaults, ok := parseSettings(commandArgs()); !ok || defaults.mixedHandlers {
		t.Fatal("mixed default activated")
	}
	for _, handler := range []agentoutbox.HandlerVersion{agentoutbox.TaskRequestHandler, agentoutbox.ParticipationHandler} {
		fixed := append(commandArgs(), "--handler="+string(handler))
		if parsed, ok := parseSettings(fixed); !ok || parsed.handler != handler || parsed.mixedHandlers {
			t.Fatal("registered native maintenance handler unavailable", handler)
		}
		if _, ok := parseSettings(append(fixed, "--mixed-handlers")); ok {
			t.Fatal("mixed mode accepted explicit native handler", handler)
		}
	}
	for _, bad := range [][]string{append(args, "--handler=mom-control-v1"), append(args, "--handler=memory-invalidation-v1"), append(args, "--handler=real-candidate"), {"--mixed-handlers", "--subject", testPerson}, {"--local-development-only", "--mixed-handlers"}, append(commandArgs(), "--mixed-handlers=unknown")} {
		if _, ok := parseSettings(bad); ok {
			t.Fatal("ambiguous or unbound mixed CLI accepted")
		}
	}
	for _, mixed := range []bool{false, true} {
		var out bytes.Buffer
		s := &mixedCommandStore{}
		factory := func(context.Context, *pgxpool.Config) (agentoutboxmaintenance.Store, func(), error) {
			return s, func() {}, nil
		}
		actualArgs := commandArgs()
		if mixed {
			actualArgs = append(actualArgs, "--mixed-handlers")
		}
		code := runCommand(context.Background(), actualArgs, func(string) string { return "postgres://user:SECRET@127.0.0.1/db?sslmode=disable" }, &out, factory)
		var report agentoutboxmaintenance.Report
		if code != 0 || json.Unmarshal(out.Bytes(), &report) != nil || report.BusinessExecution != "UNAVAILABLE" || strings.Contains(out.String(), "SECRET") || strings.Contains(out.String(), testPerson) || report.ConfirmedReceipts != 0 || (mixed && (s.mixedCalls != 1 || s.calls != 0)) || (!mixed && (s.mixedCalls != 0 || s.calls != 1)) {
			t.Fatal("CLI did not use explicit native mode", code, report, s.calls, s.mixedCalls)
		}
	}
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
