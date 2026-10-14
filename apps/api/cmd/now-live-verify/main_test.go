package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
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

// This fixture checks only the closed row parser. Its invented HTML cannot
// pass the pinned official artifact hash or register a native price.
func licensedTariffHTMLUnitFixture() []byte {
	header := []string{"模型名称", "条件（token）", "峰谷计费", "推理输入（元/百万 tokens）", "推理输出（元/百万 tokens）", "缓存命中（元/百万 tokens）"}
	row := func(values []string) string {
		result := "<tr>"
		for _, value := range values {
			result += "<td><span>" + value + "</span></td>"
		}
		return result + "</tr>"
	}
	return []byte(`2026-09-24 20:08:15 <div class="tse-tabs__item is-active" draggable="false"><div class="tse-tabs__item-cont"><div class="tse-tabs__item-label">广州</div></div></div><div class="tse-tabs__cont is-active"><table>` + row(header) + row([]string{"DeepSeek-V4-Pro 0813 正式版", "-", "空闲时段", "4.5", "13.5", "0.15"}) + row([]string{"\ufeff", "\ufeff", "高峰时段", "9", "27", "0.3"}) + "</table></div>")
}
func TestLicensedTariffSelectedGuangzhouModelAndPeakValleyRowsAreExact(t *testing.T) {
	raw := licensedTariffHTMLUnitFixture()
	if validateLicensedTariffHTML(raw) != nil {
		t.Fatal("closed synthetic row shape rejected")
	}
	for name, edit := range map[string]func(string) string{
		"other_region": func(s string) string { return strings.Replace(s, "广州", "新加坡", 1) },
		"direct_supply_not_selected_model": func(s string) string {
			return strings.Replace(s, "DeepSeek-V4-Pro 0813 正式版", "DeepSeek-V4-Pro 0813 正式版 原厂直供", 1)
		},
		"other_revision":        func(s string) string { return strings.Replace(s, "0813", "202606", 1) },
		"changed_peak_input":    func(s string) string { return strings.Replace(s, ">9<", ">8<", 1) },
		"changed_peak_output":   func(s string) string { return strings.Replace(s, ">27<", ">26<", 1) },
		"changed_valley_input":  func(s string) string { return strings.Replace(s, ">4.5<", ">4<", 1) },
		"changed_valley_output": func(s string) string { return strings.Replace(s, ">13.5<", ">13<", 1) },
		"wrong_column_units": func(s string) string {
			return strings.Replace(s, "推理输入（元/百万 tokens）", "推理输入（美元/百万 tokens）", 1)
		},
		"missing_peak":             func(s string) string { return s[:strings.Index(s, "<tr><td><span>\ufeff")] + "</table>" },
		"stale_document_timestamp": func(s string) string { return strings.Replace(s, "2026-09-24 20:08:15", "2026-09-20 20:08:15", 1) },
	} {
		t.Run(name, func(t *testing.T) {
			if validateLicensedTariffHTML([]byte(edit(string(raw)))) == nil {
				t.Fatal("wrong row/region/revision/units approved")
			}
		})
	}
}
func licensedTariffLocalEvidence(t *testing.T) (string, []byte, []byte, time.Time) {
	t.Helper()
	dir := filepath.Join("..", "..", "..", "..", "work", "now-2026-10-09", "provider-tariff-01")
	manifest, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if os.IsNotExist(err) {
		t.Skip("local reviewed artifact absent; no fresh price approved")
	}
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := os.ReadFile(filepath.Join(dir, "models.html"))
	if err != nil {
		t.Fatal(err)
	}
	observed, _ := time.Parse(time.RFC3339Nano, licensedTariffObservedAt)
	return dir, manifest, artifact, observed
}
func TestLicensedTariffActualPinnedSnapshotDeadlineAndUpperBound(t *testing.T) {
	dir, _, artifact, observed := licensedTariffLocalEvidence(t)
	p, err := reviewedLicensedPlacePrice(dir, observed)
	if err != nil || p.Base.Version != licensedPriceVersion || p.Snapshot.ArtifactSHA256 != licensedTariffHash || !p.Snapshot.ObservedAt.Equal(observed) || !p.Base.ExpiresAt.Equal(observed.Add(23*time.Hour)) || p.Base.InputMicrosPerToken != 9 || p.Base.OutputMicrosPerToken != 27 || p.Base.InputTokenCeiling != 16384 || p.Base.OutputTokenCeiling != 768 {
		t.Fatal("actual reviewed price, old age or conservative upper bound changed", err)
	}
	upper, err := modelegressbudget.BoundLive(p, 768, observed)
	if err != nil || upper.Amount.CostMicros != 168192 {
		t.Fatal("one actual model request lost ¥.20 bound", err)
	}
	for _, now := range []time.Time{observed.Add(-time.Nanosecond), observed.Add(23 * time.Hour), observed.Add(24 * time.Hour)} {
		if _, err := reviewedLicensedPlacePrice(dir, now); err == nil {
			t.Fatal("future or expired observation renewed")
		}
	}
	gz, err := gzip.NewReader(bytes.NewReader(artifact))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := io.ReadAll(gz)
	_ = gz.Close()
	if err != nil || validateLicensedTariffHTML(decoded) != nil {
		t.Fatal("actual official selected row could not be checked", err)
	}
	// The original price/probe continues to require its distinct old artifact.
	if _, err := reviewedPrice(dir, observed); err == nil {
		t.Fatal("old immutable price accepted the new artifact implicitly")
	}
}
func TestLicensedTariffArtifactOrManifestTamperingCannotRenewPrice(t *testing.T) {
	_, manifest, artifact, observed := licensedTariffLocalEvidence(t)
	for name, change := range map[string]func([]byte, []byte) ([]byte, []byte){
		"changed_artifact": func(m, a []byte) ([]byte, []byte) { a[20] ^= 1; return m, a },
		"changed_hash": func(m, a []byte) ([]byte, []byte) {
			return bytes.Replace(m, []byte(licensedTariffHash), []byte(strings.Repeat("b", 64)), 1), a
		},
		"changed_source": func(m, a []byte) ([]byte, []byte) {
			return bytes.Replace(m, []byte(modelegressbudget.LiveModelPriceURL), []byte("https://example.invalid/tariff"), 1), a
		},
		"manifest_time_extended": func(m, a []byte) ([]byte, []byte) {
			return bytes.Replace(m, []byte("2026-10-08T21:39:12.997641+00:00"), []byte("2026-10-09T21:39:12.997641+00:00"), 1), a
		},
		"manifest_bytes_wrong": func(m, a []byte) ([]byte, []byte) { return bytes.Replace(m, []byte("180288"), []byte("180287"), 1), a },
		"trailing_json":        func(m, a []byte) ([]byte, []byte) { return append(m, []byte(" {}")...), a },
		"unknown_metadata": func(m, a []byte) ([]byte, []byte) {
			return bytes.Replace(m, []byte(`"providerAPICalls"`), []byte(`"secret":"do not accept", "providerAPICalls"`), 1), a
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			m, a := change(append([]byte(nil), manifest...), append([]byte(nil), artifact...))
			if os.WriteFile(filepath.Join(dir, "manifest.json"), m, 0600) != nil || os.WriteFile(filepath.Join(dir, "models.html"), a, 0600) != nil {
				t.Fatal("test fixture unavailable")
			}
			if _, err := reviewedLicensedPlacePrice(dir, observed); err == nil {
				t.Fatal("tampered evidence approved")
			}
		})
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
	v.ConfigurationVersion = modelegressbudget.LiveSourceAnswerPolicyVersion
	raw, _ = json.Marshal(v)
	_ = os.WriteFile(p, raw, 0600)
	if got, e := loadConfig(p); e != nil || got != v {
		t.Fatal("explicit immutable v2 selection rejected")
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
