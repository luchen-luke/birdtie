package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
)

func TestVerifyOptionsRequireExactIsolatedLoopback(t *testing.T) {
	o := options{database: "birdtie_ui_live_20261008_unit", dir: t.TempDir(), tariffs: t.TempDir(), output: filepath.Join(t.TempDir(), "new-receipt.json"), action: "probe"}
	valid := "postgres://unit:unit@127.0.0.1:55432/" + o.database
	if _, e := validateOptions(o, valid); e != nil {
		t.Fatal("valid isolated scope rejected")
	}
	for _, dsn := range []string{"", "https://127.0.0.1/" + o.database, "postgres://unit@public.example/" + o.database, "postgres://unit@127.0.0.1/production", "postgres://127.0.0.1/" + o.database,
		valid + "?host=public.example&dbname=production", valid + "?database=production", valid + "?service=other", valid + "?sslmode=disable&sslmode=require", valid + "?sslmode=%zz"} {
		if _, e := validateOptions(o, dsn); e == nil {
			t.Fatal("nonisolated or invalid scope admitted")
		}
	}
	u, e := validateOptions(o, valid+"?sslmode=disable")
	if e != nil {
		t.Fatal("explicit local TLS selection rejected")
	}
	if pool, e := validatedPoolConfig(u, o.database); e != nil || pool.ConnConfig.Database != o.database || !localHost(pool.ConnConfig.Host) {
		t.Fatal("effective pgx connection changed isolated destination")
	}
	for _, action := range []string{"", "retry", "chain", "publish"} {
		other := o
		other.action = action
		if _, e := validateOptions(other, valid); e == nil {
			t.Fatal("unknown action admitted")
		}
	}
}

func TestVerifyReceiptIsExclusiveAndDataOnly(t *testing.T) {
	p := filepath.Join(t.TempDir(), "receipt.json")
	v := receipt{Action: "probe", Stage: "native-model-completed", HTTPStatus: 200, Model: "deepseek-v4-pro-0813", UpperMicros: 148320, Usage: modelgateway.Usage{Status: "UNKNOWN", CostStatus: "UNKNOWN"}}
	if saveReceipt(p, v) != nil {
		t.Fatal("first receipt unavailable")
	}
	before, e := os.ReadFile(p)
	if e != nil {
		t.Fatal("read receipt")
	}
	if saveReceipt(p, receipt{Status: "overwrite"}) == nil {
		t.Fatal("original receipt replaced")
	}
	after, e := os.ReadFile(p)
	if e != nil || string(before) != string(after) || !json.Valid(before) {
		t.Fatal("exclusive receipt changed")
	}
}

func TestVerifyLocalConfigRequiresOriginalImmutableRoute(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".env.now.live.local.json")
	v := liveConfig{OwnerID: "unit-owner", ConfigurationVersion: "now_public_sources_20261008_v1", RouteRevision: 601, SearchPriceVersion: "unit_search", ModelPriceVersion: "unit_model"}
	raw, _ := json.Marshal(v)
	if os.WriteFile(p, raw, 0600) != nil {
		t.Fatal("write fixture")
	}
	got, e := loadConfig(p)
	if e != nil || got != v {
		t.Fatal("native selectors changed")
	}
	v.ConfigurationVersion = "other-route"
	raw, _ = json.Marshal(v)
	_ = os.WriteFile(p, raw, 0600)
	if _, e := loadConfig(p); e == nil {
		t.Fatal("alternate route admitted")
	}
}

func TestVerifyTariffRequiresActualReviewedArtifact(t *testing.T) {
	if _, e := reviewedPrice(t.TempDir(), time.Now()); e == nil {
		t.Fatal("missing tariff admitted")
	}
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(`{"artifacts":[{"name":"models","sourceURL":"https://cloud.tencent.com/document/product/1823/130055","sha256":"fake","observedAtUTC":"2026-10-08T00:00:00Z"}]}`), 0600)
	_ = os.WriteFile(filepath.Join(dir, "models.html"), []byte("synthetic tariff"), 0600)
	if _, e := reviewedPrice(dir, time.Now()); e == nil {
		t.Fatal("unreviewed tariff bytes admitted")
	}
}
