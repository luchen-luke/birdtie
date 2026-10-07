package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
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
