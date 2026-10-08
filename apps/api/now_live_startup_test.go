package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
)

func TestNowLiveStartupDefaultAndDevelopmentScope(t *testing.T) {
	if v, e := loadNowLiveStartup("", "127.0.0.1:18090", false, nil, nil); e != nil || v != nil {
		t.Fatal("default must remain without provider calls")
	}
	for _, v := range []struct {
		path, address string
		dev           bool
	}{
		{".env.now.live.local.json", "127.0.0.1:18090", true},
		{filepath.Join(t.TempDir(), ".env.now.live.local.json"), "0.0.0.0:18090", true},
		{filepath.Join(t.TempDir(), ".env.now.live.local.json"), "127.0.0.1:18090", false},
		{filepath.Join(t.TempDir(), "wrong.json"), "127.0.0.1:18090", true},
		{filepath.Join(t.TempDir(), ".env.now.live.local.json"), "127.0.0.1:18090", true},
	} {
		if got, e := loadNowLiveStartup(v.path, v.address, v.dev, nil, nil); e == nil || got != nil {
			t.Fatal("invalid local activation admitted")
		}
	}
}

func TestNowLiveProvidersUseExactModelAndRequiredEnvironmentCredential(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		".env.tencent.local.json": `{"provider":"tencent_tokenhub","baseUrl":"https://tokenhub.tencentmaas.com/v1","model":"deepseek-v4-pro-0813","maxOutputTokens":768,"apiKey":"old-unit-placeholder"}`,
		".env.wsa.local.json":     `{"apiKey":"unit-search-placeholder","keyName":"unit-dev"}`,
	}
	for name, data := range files {
		if e := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); e != nil {
			t.Fatal("write synthetic configuration")
		}
	}
	for _, tc := range []struct {
		name, key string
		present   bool
		wantOK    bool
	}{
		{"missing", "", false, false},
		{"empty", "", true, false},
		{"invalid", "unit with spaces", true, false},
		{"updated", "updated-unit-placeholder", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lookups := 0
			lookup := func(name string) (string, bool) {
				lookups++
				if name != "BIRDTIE_TENCENT_TOKENHUB_API_KEY" {
					t.Fatal("unexpected credential variable")
				}
				return tc.key, tc.present
			}
			model, search, e := loadNowLiveProviders(dir, lookup)
			if tc.wantOK {
				if e != nil || model == nil || search == nil || lookups != 1 {
					t.Fatal("environment-backed adapters unavailable")
				}
				got := model.Descriptor()
				if got.ProviderID != "tencent_tokenhub" || got.ModelID != "deepseek-v4-pro-0813" || got.ModelVersion != got.ModelID || got.Mode != modelgateway.Live {
					t.Fatal("selected runtime model changed")
				}
			} else if e == nil || model != nil || search != nil {
				t.Fatal("missing or invalid environment credential used old file fallback")
			}
		})
	}
	if model, search, e := loadNowLiveProviders(dir, nil); e == nil || model != nil || search != nil {
		t.Fatal("nil environment resolver admitted")
	}
}

func TestNowLiveFeatureGateKeepsOriginalControllerBrake(t *testing.T) {
	config, e := agentfeature.LoadConfig(func(string) (string, bool) { return "", false })
	if e != nil {
		t.Fatal(e)
	}
	controller, e := agentfeature.NewController(config)
	if e != nil {
		t.Fatal(e)
	}
	gate := nowLiveFeatureGate{controller: controller}
	if gate.InferenceEnabled(context.Background()) || gate.InferenceEnabled(nil) {
		t.Fatal("default OFF became approval")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if gate.InferenceEnabled(ctx) {
		t.Fatal("cancelled caller became approval")
	}
}
