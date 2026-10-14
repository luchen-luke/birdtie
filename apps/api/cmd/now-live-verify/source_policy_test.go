package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/modelconfiguration"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
)

func TestSourcePolicyDefinitionUsesOriginalTextOnlyContract(t *testing.T) {
	definition, prompt, policy := sourceAnswerPolicyDefinition()
	resolved, err := modelconfiguration.ResolveConfiguration(definition, prompt, policy)
	if err != nil || resolved.Configuration.Version != modelegressbudget.LiveSourceAnswerPolicyVersion || resolved.Configuration.OutputMode != modelgateway.Text || len(resolved.Configuration.ToolAllowlist) != 0 || len(resolved.Configuration.CapabilitiesRequired) != 1 || resolved.Configuration.CapabilitiesRequired[0] != "text" || prompt.Text != modelegressbudget.LiveSourceAnswerPromptV2 {
		t.Fatal("source policy widened capabilities or changed the prompt", err)
	}
}

func TestLicensedPlacePolicyIsSeparateImmutableTextContract(t *testing.T) {
	definition, prompt, policy := licensedPlacePolicyDefinition()
	resolved, err := modelconfiguration.ResolveConfiguration(definition, prompt, policy)
	if err != nil || resolved.Configuration.Version != modelegressbudget.LiveLicensedPlacePolicyVersion || resolved.Configuration.Version == modelegressbudget.LiveSourceAnswerPolicyVersion || resolved.Configuration.OutputMode != modelgateway.Text || len(resolved.Configuration.ToolAllowlist) != 0 || len(resolved.Configuration.CapabilitiesRequired) != 1 || resolved.Configuration.CapabilitiesRequired[0] != "text" || prompt.Text != modelegressbudget.LiveLicensedPlacePrompt {
		t.Fatal("licensed native facts contract was merged into the old WSA version", err)
	}
}

func TestSourcePolicySelectorRequiresExactOriginalBytesAndExclusiveUpdate(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env.now.live.local.json")
	before := []byte("original selector\n")
	_ = os.WriteFile(path, before, 0600)
	config := liveConfig{OwnerID: "unit-original-owner", ConfigurationVersion: modelegressbudget.LiveSourceAnswerPolicyVersion, RouteRevision: 602, SearchPriceVersion: "unit-original-search", ModelPriceVersion: "unit-original-model"}
	if replaceSourcePolicySelector(path, []byte("other bytes"), config) == nil {
		t.Fatal("changed file accepted")
	}
	if replaceSourcePolicySelector(path, before, config) != nil {
		t.Fatal("exact selector update failed")
	}
	got, err := loadConfig(path)
	if err != nil || got != config {
		t.Fatal("original owner, prices or new route changed", err)
	}
	if replaceSourcePolicySelector(path, before, config) == nil {
		t.Fatal("old snapshot replayed")
	}
	current, _ := os.ReadFile(path)
	_ = os.WriteFile(path+".source-policy-update.tmp", []byte("uncertain earlier update"), 0600)
	if replaceSourcePolicySelector(path, current, config) == nil {
		t.Fatal("unknown previous temporary update overwritten")
	}
}

func TestLicensedPolicySelectorPreservesOwnerSearchAndSelectsOnlyFreshNewPrice(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env.now.live.local.json")
	old := liveConfig{OwnerID: "unit-original-owner", ConfigurationVersion: modelegressbudget.LiveSourceAnswerPolicyVersion, RouteRevision: 602, SearchPriceVersion: "unit-original-search", ModelPriceVersion: priceVersion}
	before, _ := json.Marshal(old)
	if os.WriteFile(path, before, 0600) != nil {
		t.Fatal("fixture unavailable")
	}
	next := old
	next.ConfigurationVersion = modelegressbudget.LiveLicensedPlacePolicyVersion
	next.RouteRevision = 603
	next.ModelPriceVersion = licensedPriceVersion
	if replaceSourcePolicySelector(path, before, next) != nil {
		t.Fatal("exact CAS selector replacement failed")
	}
	got, err := loadConfig(path)
	if err != nil || got != next || got.OwnerID != old.OwnerID || got.SearchPriceVersion != old.SearchPriceVersion || old.ModelPriceVersion != priceVersion {
		t.Fatal("price selection rewrote old state, owner or search version", err)
	}
	if activateLicensedPlacePolicy(context.Background(), nil, nil, path, old, &receipt{}, priceVersion) == nil {
		t.Fatal("old price activated on new licensed path")
	}
}
