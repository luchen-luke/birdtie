package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
	"github.com/birdtie/birdtie/apps/api/internal/modelconfiguration"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
	"github.com/jackc/pgx/v5"
)

type ModelConfigurationRoute struct {
	TaskKind   modelgateway.TaskKind
	OutputMode modelgateway.OutputMode
	Version    string
	Revision   int64
}
type TaskModelBinding struct {
	TaskID          string
	Reference       modelconfiguration.RunReference
	SourceVersion   agentevent.SourceVersion
	SourceUpdatedAt time.Time
	CreatedAt       time.Time
	ExecutionStatus string
}

func configurationStorageError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return modelconfiguration.ErrMissing
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return modelconfiguration.ErrUnavailable
}

func readModelConfiguration(ctx context.Context, tx pgx.Tx, version string) (modelconfiguration.ResolvedConfiguration, error) {
	var raw []byte
	var p modelconfiguration.PromptDefinition
	var policy modelconfiguration.PolicyVersionReference
	var fingerprint string
	err := tx.QueryRow(ctx, `SELECT c.definition,p.version,p.prompt_text,policy.version,policy.artifact_sha256,c.fingerprint
	 FROM model_configuration_versions c JOIN model_prompt_versions p ON p.version=c.prompt_version
	 JOIN model_configuration_policy_versions policy ON policy.version=c.policy_version WHERE c.version=$1
	 FOR SHARE OF c,p,policy`, version).Scan(&raw, &p.Version, &p.Text, &policy.Version, &policy.ArtifactSHA256, &fingerprint)
	if err != nil {
		return modelconfiguration.ResolvedConfiguration{}, configurationStorageError(err)
	}
	c, err := modelconfiguration.DecodeConfiguration(raw)
	if err != nil {
		return modelconfiguration.ResolvedConfiguration{}, modelconfiguration.ErrInvalid
	}
	resolved, err := modelconfiguration.ResolveConfiguration(c, p, policy)
	if err != nil || resolved.Fingerprint != fingerprint {
		return modelconfiguration.ResolvedConfiguration{}, modelconfiguration.ErrInvalid
	}
	return resolved, nil
}

// RegisterModelConfiguration is a trusted, local server maintenance primitive;
// there is no HTTP/UI/model tool exposing this configuration mutation.
func (s *Store) RegisterModelConfiguration(ctx context.Context, c modelconfiguration.Configuration, p modelconfiguration.PromptDefinition, policy modelconfiguration.PolicyVersionReference) (modelconfiguration.ResolvedConfiguration, error) {
	resolved, err := modelconfiguration.ResolveConfiguration(c, p, policy)
	if err != nil {
		return modelconfiguration.ResolvedConfiguration{}, err
	}
	if s == nil || s.pool == nil {
		return modelconfiguration.ResolvedConfiguration{}, modelconfiguration.ErrUnavailable
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return modelconfiguration.ResolvedConfiguration{}, configurationStorageError(err)
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `INSERT INTO model_prompt_versions(version,prompt_text) VALUES($1,$2) ON CONFLICT DO NOTHING`, p.Version, p.Text)
	if err != nil {
		return modelconfiguration.ResolvedConfiguration{}, configurationStorageError(err)
	}
	var storedPrompt string
	if err = tx.QueryRow(ctx, `SELECT prompt_text FROM model_prompt_versions WHERE version=$1 FOR SHARE`, p.Version).Scan(&storedPrompt); err != nil {
		return modelconfiguration.ResolvedConfiguration{}, configurationStorageError(err)
	}
	if storedPrompt != p.Text {
		return modelconfiguration.ResolvedConfiguration{}, modelconfiguration.ErrConflict
	}
	_, err = tx.Exec(ctx, `INSERT INTO model_configuration_policy_versions(version,artifact_sha256) VALUES($1,$2) ON CONFLICT DO NOTHING`, policy.Version, policy.ArtifactSHA256)
	if err != nil {
		return modelconfiguration.ResolvedConfiguration{}, configurationStorageError(err)
	}
	var storedPolicy string
	if err = tx.QueryRow(ctx, `SELECT artifact_sha256 FROM model_configuration_policy_versions WHERE version=$1 FOR SHARE`, policy.Version).Scan(&storedPolicy); err != nil {
		return modelconfiguration.ResolvedConfiguration{}, configurationStorageError(err)
	}
	if storedPolicy != policy.ArtifactSHA256 {
		return modelconfiguration.ResolvedConfiguration{}, modelconfiguration.ErrConflict
	}
	raw, _ := json.Marshal(resolved.Configuration)
	_, err = tx.Exec(ctx, `INSERT INTO model_configuration_versions(version,prompt_version,policy_version,task_kind,output_mode,fingerprint,definition)
	 VALUES($1,$2,$3,$4,$5,$6,$7::jsonb) ON CONFLICT DO NOTHING`, resolved.Configuration.Version, p.Version, policy.Version, c.TaskKind, c.OutputMode, resolved.Fingerprint, raw)
	if err != nil {
		return modelconfiguration.ResolvedConfiguration{}, configurationStorageError(err)
	}
	stored, err := readModelConfiguration(ctx, tx, c.Version)
	if err != nil {
		return modelconfiguration.ResolvedConfiguration{}, err
	}
	if stored.Fingerprint != resolved.Fingerprint {
		return modelconfiguration.ResolvedConfiguration{}, modelconfiguration.ErrConflict
	}
	if err = tx.Commit(ctx); err != nil {
		return modelconfiguration.ResolvedConfiguration{}, configurationStorageError(err)
	}
	return stored, nil
}

func (s *Store) ReadModelConfiguration(ctx context.Context, version string) (modelconfiguration.ResolvedConfiguration, error) {
	if !modelconfiguration.ValidVersion(version) {
		return modelconfiguration.ResolvedConfiguration{}, modelconfiguration.ErrInvalid
	}
	if s == nil || s.pool == nil {
		return modelconfiguration.ResolvedConfiguration{}, modelconfiguration.ErrUnavailable
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return modelconfiguration.ResolvedConfiguration{}, configurationStorageError(err)
	}
	defer tx.Rollback(ctx)
	c, err := readModelConfiguration(ctx, tx, version)
	if err != nil {
		return modelconfiguration.ResolvedConfiguration{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return modelconfiguration.ResolvedConfiguration{}, configurationStorageError(err)
	}
	return c, nil
}

func (s *Store) ActivateModelConfiguration(ctx context.Context, version string, expectedRevision int64) (ModelConfigurationRoute, error) {
	if expectedRevision < 0 || expectedRevision == math.MaxInt64 || !modelconfiguration.ValidVersion(version) {
		return ModelConfigurationRoute{}, modelconfiguration.ErrInvalid
	}
	if s == nil || s.pool == nil {
		return ModelConfigurationRoute{}, modelconfiguration.ErrUnavailable
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ModelConfigurationRoute{}, configurationStorageError(err)
	}
	defer tx.Rollback(ctx)
	c, err := readModelConfiguration(ctx, tx, version)
	if err != nil {
		return ModelConfigurationRoute{}, err
	}
	if expectedRevision == 0 {
		_, err = tx.Exec(ctx, `INSERT INTO model_configuration_routes(task_kind,output_mode,version,revision) VALUES($1,$2,$3,1) ON CONFLICT DO NOTHING`, c.Configuration.TaskKind, c.Configuration.OutputMode, version)
		if err != nil {
			return ModelConfigurationRoute{}, configurationStorageError(err)
		}
	}
	var route ModelConfigurationRoute
	err = tx.QueryRow(ctx, `SELECT task_kind,output_mode,version,revision FROM model_configuration_routes WHERE task_kind=$1 AND output_mode=$2 FOR UPDATE`, c.Configuration.TaskKind, c.Configuration.OutputMode).Scan(&route.TaskKind, &route.OutputMode, &route.Version, &route.Revision)
	if err != nil {
		return ModelConfigurationRoute{}, configurationStorageError(err)
	}
	if expectedRevision == 0 {
		if route.Version != version || route.Revision != 1 {
			return ModelConfigurationRoute{}, modelconfiguration.ErrConflict
		}
	} else {
		if route.Revision != expectedRevision {
			return ModelConfigurationRoute{}, modelconfiguration.ErrConflict
		}
		if route.Version != version {
			err = tx.QueryRow(ctx, `UPDATE model_configuration_routes SET version=$3,revision=revision+1,updated_at=clock_timestamp() WHERE task_kind=$1 AND output_mode=$2 AND revision=$4 RETURNING revision`, route.TaskKind, route.OutputMode, version, expectedRevision).Scan(&route.Revision)
			if err != nil {
				return ModelConfigurationRoute{}, configurationStorageError(err)
			}
			route.Version = version
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return ModelConfigurationRoute{}, configurationStorageError(err)
	}
	return route, nil
}

type currentConfigurationTask struct {
	agent          agentcognitive.AgentReference
	version        agentevent.SourceVersion
	updatedAt, now time.Time
	sessionID      string
	status         string
}

func lockModelConfigurationTask(ctx context.Context, tx pgx.Tx, access agentevent.Access, taskID string, devPhone bool) (currentConfigurationTask, error) {
	if access.SessionDigest == ([32]byte{}) {
		return currentConfigurationTask{}, modelconfiguration.ErrDenied
	}
	ref, err := actorref.Parse("PERSON", taskID)
	if err != nil || ref.ID != taskID || taskID == "00000000-0000-0000-0000-000000000000" {
		return currentConfigurationTask{}, modelconfiguration.ErrInvalid
	}
	// UTC stabilizes to_jsonb(t)'s timestamptz formatting across pool/session
	// TimeZones. This is the real Task snapshot token, not metadata's revision.
	if _, err = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); err != nil {
		return currentConfigurationTask{}, configurationStorageError(err)
	}
	var v currentConfigurationTask
	var ownerID, agentID string
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT ses.id,actor.id,ag.id,t.updated_at,clock_timestamp(),t.status,to_jsonb(t)
	 FROM agent_tasks t `+agentEventCurrentPrincipalSQL+`
	 WHERE t.id=$1 AND t.owner_account_id=actor.id AND t.principal_type='person' AND t.acting_user_account_id=actor.id
	 `+agentEventCurrentSessionSQL+` AND NOT EXISTS(SELECT 1 FROM agents other WHERE other.principal_account_id=actor.id
	 AND other.agent_type='personal' AND other.status='active' AND other.id<>ag.id) FOR SHARE OF ses,actor,ag,ap,t`, taskID, access.SessionDigest[:], devPhone).Scan(&v.sessionID, &ownerID, &agentID, &v.updatedAt, &v.now, &v.status, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return currentConfigurationTask{}, modelconfiguration.ErrDenied
	}
	if err != nil {
		return currentConfigurationTask{}, configurationStorageError(err)
	}
	principal, err := actorref.ParsePrincipal("PERSON", ownerID)
	if err != nil {
		return currentConfigurationTask{}, modelconfiguration.ErrUnavailable
	}
	v.agent = agentcognitive.AgentReference{AgentID: agentID, Principal: principal, Role: agentruntime.PersonalAgent}
	v.version, err = agentevent.QueryVersion(v.updatedAt, raw)
	if err != nil || v.updatedAt.After(v.now) {
		return currentConfigurationTask{}, modelconfiguration.ErrInvalid
	}
	return v, nil
}

func recheckModelBindingSession(ctx context.Context, tx pgx.Tx, sessionID string, request *modelgateway.Request) error {
	var valid bool
	var checkedAt time.Time
	// Session and request are checked against the same current database clock
	// after all potentially blocking source/configuration locks were acquired.
	err := tx.QueryRow(ctx, `WITH current_clock AS MATERIALIZED (SELECT clock_timestamp() AS checked_at)
	 SELECT revoked_at IS NULL AND expires_at>checked_at AND idle_expires_at>checked_at,checked_at
	 FROM sessions CROSS JOIN current_clock WHERE id=$1`, sessionID).Scan(&valid, &checkedAt)
	if err != nil {
		return configurationStorageError(err)
	}
	if !valid {
		return modelconfiguration.ErrDenied
	}
	if request != nil && modelgateway.ValidateRequest(*request, checkedAt) != nil {
		return modelconfiguration.ErrInvalid
	}
	return nil
}

func readTaskModelBinding(ctx context.Context, tx pgx.Tx, bindingID string) (TaskModelBinding, error) {
	var b TaskModelBinding
	var version, fp, agentID, ownerID string
	err := tx.QueryRow(ctx, `SELECT binding_id,task_id,configuration_version,configuration_fingerprint,agent_id,owner_id,source_token,source_updated_at,created_at,execution_status FROM agent_task_model_bindings WHERE binding_id=$1 FOR SHARE`, bindingID).Scan(&b.Reference.RunID, &b.TaskID, &version, &fp, &agentID, &ownerID, &b.SourceVersion.Token, &b.SourceUpdatedAt, &b.CreatedAt, &b.ExecutionStatus)
	if err != nil {
		return TaskModelBinding{}, configurationStorageError(err)
	}
	c, err := readModelConfiguration(ctx, tx, version)
	if err != nil {
		return TaskModelBinding{}, err
	}
	if c.Fingerprint != fp {
		return TaskModelBinding{}, modelconfiguration.ErrInvalid
	}
	principal, err := actorref.ParsePrincipal("PERSON", ownerID)
	if err != nil {
		return TaskModelBinding{}, modelconfiguration.ErrInvalid
	}
	v := c.Configuration
	b.Reference = modelconfiguration.RunReference{SchemaVersion: modelconfiguration.ReferenceVersion, RunID: b.Reference.RunID, Agent: agentcognitive.AgentReference{AgentID: agentID, Principal: principal, Role: agentruntime.PersonalAgent}, ConfigurationVersion: version, ConfigurationFingerprint: fp, PromptVersion: v.PromptVersion, InputSchemaVersion: v.InputSchemaVersion, OutputSchemaVersion: v.OutputSchemaVersion, ToolAllowlist: append([]string{}, v.ToolAllowlist...), PolicyVersion: v.PolicyVersion, CapabilitiesRequired: append([]string{}, v.CapabilitiesRequired...), TaskKind: v.TaskKind, OutputMode: v.OutputMode}
	b.SourceVersion.Kind = agentevent.UpdatedAtDigestVersion
	if modelconfiguration.ValidateReference(b.Reference, c) != nil || b.ExecutionStatus != "UNAVAILABLE" || !modelconfiguration.ValidDigest(b.SourceVersion.Token) {
		return TaskModelBinding{}, modelconfiguration.ErrInvalid
	}
	return b, nil
}

// BindModelTaskConfiguration stores only fixed pre-run metadata for a real own
// native Task. It does not create an AgentRun, authorize input or invoke a model.
func (s *Store) BindModelTaskConfiguration(ctx context.Context, access agentevent.Access, taskID, expectedVersion string, expectedRouteRevision int64, request modelgateway.Request) (TaskModelBinding, error) {
	if s == nil || s.pool == nil {
		return TaskModelBinding{}, modelconfiguration.ErrUnavailable
	}
	if request.Agent.Principal.Type != actorref.Person || request.Agent.Role != agentruntime.PersonalAgent {
		return TaskModelBinding{}, modelconfiguration.ErrUnavailable
	}
	if modelgateway.ValidateRequest(request, time.Now()) != nil {
		return TaskModelBinding{}, modelconfiguration.ErrInvalid
	}
	if !modelconfiguration.ValidVersion(expectedVersion) || expectedRouteRevision < 1 {
		return TaskModelBinding{}, modelconfiguration.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return TaskModelBinding{}, configurationStorageError(err)
	}
	defer tx.Rollback(ctx)
	current, err := lockModelConfigurationTask(ctx, tx, access, taskID, s.devPhoneEnabled)
	if err != nil {
		return TaskModelBinding{}, err
	}
	if current.status != "ACTIVE" || current.agent != request.Agent {
		return TaskModelBinding{}, modelconfiguration.ErrDenied
	}
	// Exact existing retry can read its old pinned version after an activation
	// change, provided current own source/version is unchanged. A new binding
	// must use the currently activated exact route/version/revision instead.
	existing, existingErr := readTaskModelBinding(ctx, tx, request.RunID)
	if existingErr == nil {
		if existing.TaskID != taskID || existing.Reference.Agent != current.agent || existing.Reference.ConfigurationVersion != expectedVersion || existing.SourceVersion != current.version {
			return TaskModelBinding{}, modelconfiguration.ErrConflict
		}
		c, err := readModelConfiguration(ctx, tx, expectedVersion)
		if err != nil {
			return TaskModelBinding{}, err
		}
		ref, err := modelconfiguration.BindRequest(c, request, current.now)
		if err != nil || ref.ConfigurationFingerprint != existing.Reference.ConfigurationFingerprint {
			return TaskModelBinding{}, modelconfiguration.ErrDenied
		}
		if err = recheckModelBindingSession(ctx, tx, current.sessionID, &request); err != nil {
			return TaskModelBinding{}, err
		}
		if err = tx.Commit(ctx); err != nil {
			return TaskModelBinding{}, configurationStorageError(err)
		}
		return existing, nil
	}
	if !errors.Is(existingErr, modelconfiguration.ErrMissing) {
		return TaskModelBinding{}, existingErr
	}
	var activeVersion string
	var revision int64
	err = tx.QueryRow(ctx, `SELECT version,revision FROM model_configuration_routes WHERE task_kind=$1 AND output_mode=$2 FOR SHARE`, request.TaskKind, request.OutputMode).Scan(&activeVersion, &revision)
	if err != nil {
		return TaskModelBinding{}, configurationStorageError(err)
	}
	if activeVersion != expectedVersion || revision != expectedRouteRevision {
		return TaskModelBinding{}, modelconfiguration.ErrConflict
	}
	c, err := readModelConfiguration(ctx, tx, activeVersion)
	if err != nil {
		return TaskModelBinding{}, err
	}
	ref, err := modelconfiguration.BindRequest(c, request, current.now)
	if err != nil {
		return TaskModelBinding{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO agent_task_model_bindings(binding_id,task_id,owner_id,owner_type,agent_id,actor_id,configuration_version,configuration_fingerprint,source_token,source_updated_at)
	 VALUES($1,$2,$3,'PERSON',$4,$3,$5,$6,$7,$8) ON CONFLICT DO NOTHING`, ref.RunID, taskID, current.agent.Principal.ID, current.agent.AgentID, activeVersion, c.Fingerprint, current.version.Token, current.updatedAt)
	if err != nil {
		return TaskModelBinding{}, configurationStorageError(err)
	}
	stored, err := readTaskModelBinding(ctx, tx, ref.RunID)
	if err != nil {
		return TaskModelBinding{}, err
	}
	if stored.TaskID != taskID || stored.Reference.Agent != current.agent || stored.Reference.ConfigurationVersion != activeVersion || stored.SourceVersion != current.version {
		return TaskModelBinding{}, modelconfiguration.ErrConflict
	}
	if err = recheckModelBindingSession(ctx, tx, current.sessionID, &request); err != nil {
		return TaskModelBinding{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return TaskModelBinding{}, configurationStorageError(err)
	}
	return stored, nil
}

// ReadPinnedModelTaskConfiguration returns owner-readable historical metadata;
// a changed Task can still locate the original immutable configuration. This is
// not a current-execution grant. Revalidation separately checks source/version.
func (s *Store) ReadPinnedModelTaskConfiguration(ctx context.Context, access agentevent.Access, taskID, bindingID string, revalidate bool) (TaskModelBinding, error) {
	if s == nil || s.pool == nil {
		return TaskModelBinding{}, modelconfiguration.ErrUnavailable
	}
	ref, idErr := actorref.Parse("PERSON", bindingID)
	if idErr != nil || ref.ID != bindingID || bindingID == "00000000-0000-0000-0000-000000000000" {
		return TaskModelBinding{}, modelconfiguration.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return TaskModelBinding{}, configurationStorageError(err)
	}
	defer tx.Rollback(ctx)
	current, err := lockModelConfigurationTask(ctx, tx, access, taskID, s.devPhoneEnabled)
	if err != nil {
		return TaskModelBinding{}, err
	}
	stored, err := readTaskModelBinding(ctx, tx, bindingID)
	if err != nil {
		return TaskModelBinding{}, err
	}
	if stored.TaskID != taskID || stored.Reference.Agent != current.agent {
		return TaskModelBinding{}, modelconfiguration.ErrDenied
	}
	if revalidate && (current.status != "ACTIVE" || stored.SourceVersion != current.version || !stored.SourceUpdatedAt.Equal(current.updatedAt)) {
		return TaskModelBinding{}, modelconfiguration.ErrDenied
	}
	if err = recheckModelBindingSession(ctx, tx, current.sessionID, nil); err != nil {
		return TaskModelBinding{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return TaskModelBinding{}, configurationStorageError(err)
	}
	return stored, nil
}
