package main

import (
	"context"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
	"testing"
)

func TestSystemPushStartupOffDoesNotReadCredentialConfiguration(t *testing.T) {
	cfg, err := pgxpool.ParseConfig("postgres://unit:unit@127.0.0.1:1/unit?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	cfg.MinConns = 0
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	calls := 0
	gateway, err := loadSystemPushStartup(postgres.New(pool, false), func(name string) (string, bool) {
		calls++
		if name != "BIRDTIE_SYSTEM_PUSH_ENABLED" {
			t.Fatal("OFF read credential configuration")
		}
		return "false", true
	})
	if err != nil || gateway == nil || calls != 1 {
		t.Fatal("OFF startup unavailable")
	}
}

func TestSystemPushStartupEnabledMissingConfigurationFails(t *testing.T) {
	gateway, err := loadSystemPushStartup(nil, func(name string) (string, bool) {
		if name == "BIRDTIE_SYSTEM_PUSH_ENABLED" {
			return "true", true
		}
		return "", false
	})
	if err == nil || gateway != nil {
		t.Fatal("missing trusted configuration admitted")
	}
}
