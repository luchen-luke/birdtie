package main

import (
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentvisual"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"io"
	"net"
	"os"
	"path/filepath"
)

// Selecting trusted transport settings never creates a fresh tariff, global
// funding grant, feature flag or pixel consent. All are checked by native code.
func loadAgentVisualStartup(path, address string, devPhone bool, store *postgres.Store, features *agentfeature.Controller) (agentvisual.Gateway, error) {
	if path == "" {
		return postgres.NewAgentVisualGateway(store, postgres.AgentVisualOptions{}), nil
	}
	host, _, e := net.SplitHostPort(address)
	if e != nil || !devPhone || !isLoopbackHost(host) || !filepath.IsAbs(path) || filepath.Base(path) != ".env.agent.visual.local.json" {
		return nil, modelgateway.ErrUnavailable
	}
	info, e := os.Lstat(path)
	if e != nil || !info.Mode().IsRegular() || info.Size() > 4096 {
		return nil, modelgateway.ErrUnavailable
	}
	file, e := os.Open(path)
	if e != nil {
		return nil, modelgateway.ErrUnavailable
	}
	defer file.Close()
	opened, e := file.Stat()
	if e != nil || !os.SameFile(info, opened) || !opened.Mode().IsRegular() {
		return nil, modelgateway.ErrUnavailable
	}
	var selected struct {
		PriceVersion string `json:"priceVersion"`
	}
	decoder := json.NewDecoder(io.LimitReader(file, 4097))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&selected) != nil || selected.PriceVersion == "" || len(selected.PriceVersion) > 120 {
		return nil, modelgateway.ErrUnavailable
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, modelgateway.ErrUnavailable
	}
	config, e := modelgateway.LoadTencentVisionConfigFromEnvironment(filepath.Join(filepath.Dir(path), ".env.tencent.vision.local.json"), os.LookupEnv)
	if e != nil {
		return nil, modelgateway.ErrUnavailable
	}
	adapter, e := modelgateway.NewTencentVisionAdapter(config, nil)
	if e != nil {
		return nil, modelgateway.ErrUnavailable
	}
	return postgres.NewAgentVisualGateway(store, postgres.AgentVisualOptions{Adapter: adapter, Gate: nowLiveFeatureGate{controller: features}, PriceVersion: selected.PriceVersion, Region: config.Region()}), nil
}
