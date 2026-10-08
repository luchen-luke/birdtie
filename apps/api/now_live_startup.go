package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
)

type nowLiveStartupConfig struct {
	OwnerID              string `json:"ownerId"`
	ConfigurationVersion string `json:"configurationVersion"`
	RouteRevision        int64  `json:"routeRevision"`
	SearchPriceVersion   string `json:"searchPriceVersion"`
	ModelPriceVersion    string `json:"modelPriceVersion"`
}
type nowLiveFeatureGate struct{ controller *agentfeature.Controller }

func (g nowLiveFeatureGate) InferenceEnabled(ctx context.Context) bool {
	if ctx == nil || ctx.Err() != nil || g.controller == nil {
		return false
	}
	ticket, e := g.controller.Capture(agentfeature.Enrichment)
	return e == nil && g.controller.Current(ticket) && ctx.Err() == nil
}

// Explicit loopback development activation. No inference is configured when
// the path is absent. The file names immutable native references, not grants.
func loadNowLiveStartup(path, address string, devPhone bool, store *postgres.Store, controller *agentfeature.Controller) (agentworkspace.LiveAnswers, error) {
	if path == "" {
		return nil, nil
	}
	host, _, e := net.SplitHostPort(address)
	if e != nil || !isLoopbackHost(host) || !devPhone || !filepath.IsAbs(path) || filepath.Base(path) != ".env.now.live.local.json" {
		return nil, modelgateway.ErrUnavailable
	}
	info, e := os.Lstat(path)
	if e != nil || !info.Mode().IsRegular() || info.Size() > 4096 {
		return nil, modelgateway.ErrUnavailable
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, modelgateway.ErrUnavailable
	}
	defer f.Close()
	opened, e := f.Stat()
	if e != nil || !os.SameFile(info, opened) || !opened.Mode().IsRegular() {
		return nil, modelgateway.ErrUnavailable
	}
	var config nowLiveStartupConfig
	d := json.NewDecoder(io.LimitReader(f, 4097))
	d.DisallowUnknownFields()
	if e = d.Decode(&config); e != nil {
		return nil, modelgateway.ErrUnavailable
	}
	var extra any
	if d.Decode(&extra) != io.EOF || strings.TrimSpace(config.OwnerID) != config.OwnerID {
		return nil, modelgateway.ErrUnavailable
	}
	model, search, e := loadNowLiveProviders(filepath.Dir(path), os.LookupEnv)
	if e != nil {
		return nil, modelgateway.ErrUnavailable
	}
	native, e := postgres.NewOwnNowLiveAnswers(store, controller, nowLiveFeatureGate{controller: controller}, model, search, postgres.NowLiveAnswerOptions{OwnerID: config.OwnerID, ConfigurationVersion: config.ConfigurationVersion, RouteRevision: config.RouteRevision, SearchPriceVersion: config.SearchPriceVersion, ModelPriceVersion: config.ModelPriceVersion})
	if e != nil {
		return nil, e
	}
	return &nowLiveOwnerBoundAnswers{LiveAnswers: native, ownerID: config.OwnerID}, nil
}

// Explicit runtime environment credentials supersede the old ignored file.
// This only constructs adapters; it performs no request or native approval.
func loadNowLiveProviders(dir string, lookup func(string) (string, bool)) (*modelgateway.TencentTokenHubAdapter, *agenttool.TencentWSAAdapter, error) {
	modelConfig, e := modelgateway.LoadTencentTokenHubConfigFromEnvironment(filepath.Join(dir, ".env.tencent.local.json"), lookup)
	if e != nil {
		return nil, nil, modelgateway.ErrUnavailable
	}
	searchConfig, e := agenttool.LoadTencentWSAConfig(filepath.Join(dir, ".env.wsa.local.json"))
	if e != nil {
		return nil, nil, modelgateway.ErrUnavailable
	}
	model, e := modelgateway.NewTencentTokenHubAdapter(modelConfig, nil)
	if e != nil {
		return nil, nil, modelgateway.ErrUnavailable
	}
	search, e := agenttool.NewTencentWSAAdapter(searchConfig, nil)
	if e != nil {
		return nil, nil, modelgateway.ErrUnavailable
	}
	return model, search, nil
}
