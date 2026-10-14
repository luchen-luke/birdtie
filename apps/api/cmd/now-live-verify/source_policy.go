package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"

	"github.com/birdtie/birdtie/apps/api/internal/modelconfiguration"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func sourceAnswerPolicyDefinition() (modelconfiguration.Configuration, modelconfiguration.PromptDefinition, modelconfiguration.PolicyVersionReference) {
	return answerPolicyDefinition(modelegressbudget.LiveSourceAnswerPolicyVersion, modelegressbudget.LiveSourceAnswerPromptV2)
}

func licensedPlacePolicyDefinition() (modelconfiguration.Configuration, modelconfiguration.PromptDefinition, modelconfiguration.PolicyVersionReference) {
	return answerPolicyDefinition(modelegressbudget.LiveLicensedPlacePolicyVersion, modelegressbudget.LiveLicensedPlacePrompt)
}

func answerPolicyDefinition(version, prompt string) (modelconfiguration.Configuration, modelconfiguration.PromptDefinition, modelconfiguration.PolicyVersionReference) {
	digest := sha256.Sum256([]byte(prompt))
	return modelconfiguration.Configuration{SchemaVersion: modelconfiguration.SchemaVersion,
			Version: version, TaskKind: modelgateway.ActivityQuery, PromptVersion: version,
			InputSchemaVersion: "air.messages.v1", OutputSchemaVersion: "air.answer.v1",
			OutputMode: modelgateway.Text, ToolAllowlist: []string{}, PolicyVersion: version,
			CapabilitiesRequired: []string{"text"}},
		modelconfiguration.PromptDefinition{Version: version, Text: prompt},
		modelconfiguration.PolicyVersionReference{Version: version, ArtifactSHA256: hex.EncodeToString(digest[:])}
}

// This explicit maintenance action uses the original immutable registry and
// route CAS only. It sends nothing and does not rewrite a Task, Run, grant,
// reservation or old configuration. Run only after stopping the old server at
// its natural idle checkpoint; the running process's cached config is not fixed
// by replacing this local file.
func activateSourceAnswerPolicy(ctx context.Context, store *postgres.Store, pool *pgxpool.Pool, path string, config liveConfig, r *receipt) error {
	return activateAnswerPolicy(ctx, store, pool, path, config, r, sourceAnswerPolicyDefinition, "source-policy", "")
}

func activateLicensedPlacePolicy(ctx context.Context, store *postgres.Store, pool *pgxpool.Pool, path string, config liveConfig, r *receipt, freshPriceVersion string) error {
	if freshPriceVersion != licensedPriceVersion {
		return modelgateway.ErrInvalid
	}
	return activateAnswerPolicy(ctx, store, pool, path, config, r, licensedPlacePolicyDefinition, "licensed-place-policy", freshPriceVersion)
}

func activateAnswerPolicy(ctx context.Context, store *postgres.Store, pool *pgxpool.Pool, path string, config liveConfig, r *receipt, definitionFor func() (modelconfiguration.Configuration, modelconfiguration.PromptDefinition, modelconfiguration.PolicyVersionReference), stagePrefix string, freshPriceVersion string) error {
	r.Stage = stagePrefix + "-idle-checkpoint"
	var runs, transactions int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM model_request_runs WHERE state IN ('PLANNED','RUNNING')),
 (SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND xact_start IS NOT NULL)`).Scan(&runs, &transactions); err != nil || runs != 0 || transactions != 0 {
		return modelgateway.ErrUnavailable
	}
	before, err := os.ReadFile(path)
	if err != nil {
		return modelgateway.ErrInvalid
	}
	if old, err := loadConfig(path); err != nil || old != config {
		return modelgateway.ErrInvalid
	}
	var version string
	var revision int64
	if err = pool.QueryRow(ctx, `SELECT version,revision FROM model_configuration_routes WHERE task_kind=$1 AND output_mode=$2`, modelgateway.ActivityQuery, modelgateway.Text).Scan(&version, &revision); err != nil || version != config.ConfigurationVersion || revision != config.RouteRevision {
		return modelgateway.ErrInvalid
	}
	r.Stage = stagePrefix + "-immutable-registration"
	definition, prompt, policy := definitionFor()
	if _, err = store.RegisterModelConfiguration(ctx, definition, prompt, policy); err != nil {
		return err
	}
	r.Stage = stagePrefix + "-route-cas"
	route, err := store.ActivateModelConfiguration(ctx, definition.Version, revision)
	if err != nil {
		return err
	}
	r.ConfigurationVersion, r.RouteRevision = route.Version, route.Revision
	config.ConfigurationVersion, config.RouteRevision = route.Version, route.Revision
	if freshPriceVersion != "" {
		config.ModelPriceVersion = freshPriceVersion
	}
	r.Stage = stagePrefix + "-local-selector"
	if err = replaceSourcePolicySelector(path, before, config); err != nil {
		// Route may already have committed. Do not retry or revive the old server
		// automatically; preserve the receipt and reconcile this exact revision.
		return err
	}
	r.Stage, r.Status = stagePrefix+"-registered-server-restart-required", "PASS"
	return nil
}

func replaceSourcePolicySelector(path string, before []byte, config liveConfig) error {
	current, err := os.ReadFile(path)
	if err != nil || string(current) != string(before) {
		return modelgateway.ErrInvalid
	}
	out, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return modelgateway.ErrInvalid
	}
	temporary := path + ".source-policy-update.tmp"
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return modelgateway.ErrInvalid
	}
	_, writeErr := file.Write(append(out, '\n'))
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		return modelgateway.ErrInvalid
	}
	current, err = os.ReadFile(path)
	if err != nil || string(current) != string(before) {
		return modelgateway.ErrInvalid
	}
	return os.Rename(temporary, path)
}
