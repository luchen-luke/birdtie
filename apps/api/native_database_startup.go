package main

import (
	"context"
	"errors"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var errNativeDatabaseBoundary = errors.New("native_database_boundary_unavailable")
var nativeDatabaseName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

func allowedNativeDatabaseName(name string) bool {
	return nativeDatabaseName.MatchString(name) && !strings.HasPrefix(name, "civu") &&
		name != "postgres" && name != "template0" && name != "template1"
}

// An explicit target is required even for an owned loopback QA database. A name
// prefix is not proof that an unknown database is safe to run Birdtie against.
func loadNativeDatabaseName(lookup func(string) (string, bool), configuredName string) (string, error) {
	name, ok := lookup("BIRDTIE_NATIVE_DATABASE_NAME")
	if !ok || !allowedNativeDatabaseName(name) || name != configuredName {
		return "", errNativeDatabaseBoundary
	}
	return name, nil
}

func loadBackgroundMaintenance(lookup func(string) (string, bool)) (bool, error) {
	value, ok := lookup("BIRDTIE_BACKGROUND_MAINTENANCE")
	if !ok {
		return false, nil
	}
	switch strings.TrimSpace(value) {
	case "false":
		return false, nil
	case "true":
		return true, nil
	default:
		return false, errors.New("background_maintenance_configuration_invalid")
	}
}

type nativeDatabaseReader interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type nativeDatabasePinger struct {
	database     nativeDatabaseReader
	expectedName string
	startupOID   int64
}

// This reads only pg_catalog metadata, never account/session/source rows. It
// checks the native schema required by this package, not a migration ledger or
// a claim that all product migrations, data, ACLs or media have been accepted.
const nativeDatabaseProbeSQL = `WITH required(table_name,column_name,type_name) AS (VALUES
 ('legacy_map_visibility_runs','projection_sha256','text'),
 ('legacy_map_place_facts','coordinate_proof','text'),
 ('legacy_map_place_facts','native_binding','text'),
 ('legacy_map_anchor_runs','projection_sha256','text'),
 ('legacy_map_anchor_facts','native_binding','text'),
 ('legacy_map_anchor_facts','coordinate_proof','text'),
 ('legacy_private_image_recovery_items','image_id','uuid'),
 ('sessions','development_credential_kind','text'),
 ('moment_location_links','id','uuid'),('moment_location_links','moment_id','uuid'),
 ('journeys','detail_schema','text'),('journey_itinerary_entries','material','jsonb'),
 ('legacy_journey_adaptations','target_id','uuid'),('private_moment_videos','content','bytea'),
 ('private_moment_videos','original','bytea'),('legacy_private_video_import_items','video_id','uuid'),
 ('legacy_private_media_import_items','image_id','uuid'),('legacy_private_media_import_items','owner_account_id','uuid'),
 ('legacy_private_media_import_items','moment_id','uuid'),('legacy_private_media_import_items','source_input_sha256','text'),
 ('legacy_private_media_import_items','source_input_bytes','bigint'),
 ('accounts','id','uuid'),('accounts','account_type','text'),('accounts','status','text'),
 ('agents','id','uuid'),('agents','agent_type','text'),('agents','principal_account_id','uuid'),('agents','status','text'),
 ('sessions','id','uuid'),('sessions','account_id','uuid'),('sessions','token_sha256','bytea'),
 ('sessions','authentication_method','text'),('sessions','expires_at','timestamp with time zone'),
 ('sessions','idle_expires_at','timestamp with time zone'),('sessions','revoked_at','timestamp with time zone'),
 ('moments','id','uuid'),('moments','author_account_id','uuid'),('moments','revision','bigint'),
 ('moments','city_id','text'),('moments','place_id','uuid'),('moments','status','text'),('moments','visibility','text'),
 ('human_moment_control_operations','actor_account_id','uuid'),('human_moment_control_operations','operation_id','uuid'),
 ('human_moment_control_operations','status','text'),('human_moment_control_operations','action','text'),
 ('human_moment_control_operations','moment_id','uuid'),('human_moment_control_operations','expected_revision','bigint'),
 ('human_moment_control_operations','applied_revision','bigint'),('human_moment_control_operations','payload_sha256','bytea'),
 ('human_moment_control_operations','committed_at','timestamp with time zone'),
 ('public_moment_photos','moment_id','uuid'),('public_moment_photos','photo_id','uuid'),
 ('public_moment_photos','version','bigint'),('public_moment_photos','approved_moment_revision','bigint'),
 ('public_moment_photos','owner_account_id','uuid'),('public_moment_photos','thumbnail','bytea'),
 ('public_moment_photos','sha256','character(64)'),('public_moment_photos','rights','jsonb'),
 ('public_moment_photos','retention_until','timestamp with time zone'),('public_moment_photos','source_binding','text'),
 ('public_moment_photos','attribution','text'),('public_moment_photos','status','text'),
 ('public_moment_photos','updated_at','timestamp with time zone'),
 ('public_moment_photo_operations','actor_account_id','uuid'),('public_moment_photo_operations','operation_id','uuid'),
 ('public_moment_photo_operations','moment_id','uuid'),('public_moment_photo_operations','status','text'),
 ('public_moment_photo_operations','action','text'),('public_moment_photo_operations','photo_id','uuid'),
 ('public_moment_photo_operations','expected_moment_revision','bigint'),('public_moment_photo_operations','expected_photo_version','bigint'),
 ('public_moment_photo_operations','applied_photo_version','bigint'),('public_moment_photo_operations','payload_sha256','bytea'),
 ('public_moment_photo_operations','committed_at','timestamp with time zone'),
 ('legacy_import_runs','run_id','uuid'),('legacy_import_runs','source_system','text'),
 ('legacy_import_runs','source_protocol','text'),('legacy_import_runs','backup_cipher_sha256','text'),
 ('legacy_import_runs','schema_sha256','text'),('legacy_import_runs','snapshot_sha256','text'),
 ('legacy_import_runs','source_file_sha256','text'),('legacy_import_runs','manifest_file_sha256','text'),
 ('legacy_import_runs','source_header','jsonb'),('legacy_import_runs','source_counts','jsonb'),
 ('legacy_import_runs','deferred_counts','jsonb'),('legacy_import_runs','created_at','timestamp with time zone'),
 ('legacy_entity_mappings','source_system','text'),('legacy_entity_mappings','entity_kind','text'),
 ('legacy_entity_mappings','source_id','uuid'),('legacy_entity_mappings','target_id','uuid'),
 ('legacy_entity_mappings','owner_account_id','uuid'),('legacy_entity_mappings','run_id','uuid'),
 ('legacy_entity_mappings','source_version','bigint'),('legacy_entity_mappings','source_sha256','text'),
 ('legacy_entity_mappings','source_status','text'),('legacy_entity_mappings','source_data_origin','text'),
 ('legacy_entity_mappings','claim_status','text'),('legacy_entity_mappings','created_at','timestamp with time zone'),
 ('legacy_note_archives','id','uuid'),('legacy_note_archives','source_system','text'),
 ('legacy_note_archives','source_note_id','uuid'),('legacy_note_archives','owner_account_id','uuid'),
 ('legacy_note_archives','run_id','uuid'),('legacy_note_archives','source_version','bigint'),
 ('legacy_note_archives','source_sha256','text'),('legacy_note_archives','deleted','boolean'),
 ('legacy_note_archives','title','text'),('legacy_note_archives','body','text'),
 ('legacy_note_archives','recorded_start_date','text'),('legacy_note_archives','recorded_end_date','text'),
 ('legacy_note_archives','source_visibility','text'),('legacy_note_archives','source_moderation_status','text'),
 ('legacy_note_archives','source_relationships','jsonb'),('legacy_note_archives','created_at','timestamp with time zone'),
 ('personal_collection_control_operations','owner_account_id','uuid'),('personal_collection_control_operations','operation_id','uuid'),
 ('personal_collection_control_operations','status','text'),('personal_collection_control_operations','action','text'),
 ('personal_collection_control_operations','kind','text'),('personal_collection_control_operations','target_id','uuid'),
 ('personal_collection_control_operations','expected_revision','bigint'),('personal_collection_control_operations','applied_revision','bigint'),
 ('personal_collection_control_operations','payload_digest','text'),('personal_collection_control_operations','committed_at','timestamp with time zone')
 ), required_trigger(table_name,trigger_name,function_name,deferred) AS (VALUES
 ('legacy_map_anchor_runs','legacy_map_anchor_runs_immutable','birdtie_legacy_immutable',false),
 ('legacy_map_anchor_facts','legacy_map_anchor_facts_immutable','birdtie_legacy_immutable',false),
 ('legacy_map_anchor_runs','legacy_map_anchor_run_binding','birdtie_legacy_map_anchor_guard',true),
 ('legacy_map_anchor_facts','legacy_map_anchor_fact_binding','birdtie_legacy_map_anchor_guard',true),
 ('legacy_map_visibility_runs','legacy_map_run_immutable','birdtie_legacy_immutable',false),
 ('legacy_map_place_facts','legacy_map_place_immutable','birdtie_legacy_immutable',false),
 ('legacy_map_visibility_runs','legacy_map_run_binding','birdtie_legacy_map_binding_guard',true),
 ('legacy_map_place_facts','legacy_map_place_binding','birdtie_legacy_map_binding_guard',true),
 ('human_moment_control_operations','human_moment_control_immutable','birdtie_guard_human_moment_public_control',false),
 ('legacy_import_runs','legacy_run_immutable','birdtie_legacy_immutable',false),
 ('legacy_entity_mappings','legacy_mapping_immutable','birdtie_legacy_immutable',false),
 ('legacy_note_archives','legacy_note_immutable','birdtie_legacy_immutable',false),
 ('legacy_entity_mappings','legacy_mapping_binding','birdtie_legacy_import_binding',true),
 ('legacy_note_archives','legacy_archive_binding','birdtie_legacy_import_binding',true),
 ('legacy_import_runs','legacy_run_complete','birdtie_legacy_run_complete',true),
 ('personal_collection_control_operations','personal_collection_control_operation_guard','birdtie_guard_collection_control_operation',false)
 )
 SELECT current_database(),
 (SELECT oid::bigint FROM pg_catalog.pg_database WHERE datname=current_database()),
 EXISTS(SELECT 1 FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace
        WHERE n.nspname='public' AND c.relname IN('users','civu_schema_migrations')),
 ARRAY(SELECT r.table_name||'.'||r.column_name FROM required r
       LEFT JOIN pg_catalog.pg_namespace n ON n.nspname='public'
       LEFT JOIN pg_catalog.pg_class c ON c.relnamespace=n.oid AND c.relname=r.table_name AND c.relkind IN('r','p')
       LEFT JOIN pg_catalog.pg_attribute a ON a.attrelid=c.oid AND a.attname=r.column_name AND a.attnum>0 AND NOT a.attisdropped
       WHERE a.attname IS NULL OR pg_catalog.format_type(a.atttypid,a.atttypmod)<>r.type_name
       ORDER BY r.table_name,r.column_name)
 || ARRAY(SELECT r.table_name||'.'||r.trigger_name FROM required_trigger r
       LEFT JOIN pg_catalog.pg_namespace n ON n.nspname='public'
       LEFT JOIN pg_catalog.pg_class c ON c.relnamespace=n.oid AND c.relname=r.table_name
       LEFT JOIN pg_catalog.pg_trigger t ON t.tgrelid=c.oid AND t.tgname=r.trigger_name
       LEFT JOIN pg_catalog.pg_proc f ON f.oid=t.tgfoid
       WHERE t.oid IS NULL OR t.tgenabled<>'O' OR f.proname<>r.function_name OR f.pronamespace<>n.oid
        OR t.tgdeferrable<>r.deferred OR t.tginitdeferred<>r.deferred
       ORDER BY r.table_name,r.trigger_name)
 || ARRAY(SELECT 'content_map.contract'
          WHERE public.birdtie_content_map_contract() IS DISTINCT FROM 'map-content-v2')
 || ARRAY(SELECT 'legacy_map_visibility.contract'
          WHERE public.birdtie_legacy_map_visibility_contract() IS DISTINCT FROM 'map-content-v1')
 || ARRAY(SELECT 'city_public_moment.contract'
          WHERE public.birdtie_city_public_moment_contract() IS DISTINCT FROM 'city-public-moment-v1')
 || ARRAY(SELECT 'moment_interaction.contract'
          WHERE public.birdtie_moment_interaction_contract() IS DISTINCT FROM 'moment-interaction-v1')
 || ARRAY(SELECT 'moment_locations.contract'
          WHERE public.birdtie_moment_location_contract() IS DISTINCT FROM 'moment-locations-v1')
 || ARRAY(SELECT 'public_moment_media.contract'
          WHERE public.birdtie_public_moment_media_contract() IS DISTINCT FROM 'public-moment-media-v1')
 || ARRAY(SELECT 'moment_thread.contract'
          WHERE public.birdtie_moment_thread_contract() IS DISTINCT FROM 'moment-thread-v1')
 || ARRAY(SELECT 'moment_thread_editing.contract'
          WHERE public.birdtie_moment_thread_editing_contract() IS DISTINCT FROM 'moment-thread-editing-v1')
 || ARRAY(SELECT 'legacy_dev_phone.contract'
          WHERE public.birdtie_legacy_dev_phone_contract() IS DISTINCT FROM 'legacy-dev-phone-binding-v1')
 || ARRAY(SELECT 'legacy_dev_email.contract'
          WHERE public.birdtie_legacy_dev_email_contract() IS DISTINCT FROM 'legacy-dev-email-binding-v1')
 || ARRAY(SELECT 'public_moment_video.contract'
          WHERE public.birdtie_public_moment_video_contract() IS DISTINCT FROM 'public-moment-video-v2')
 || ARRAY(SELECT 'public_moment_video_carry.contract'
          WHERE public.birdtie_public_moment_video_carry_contract() IS DISTINCT FROM 'public-moment-video-carry-v1')
 || ARRAY(SELECT 'optional_moment_content.contract'
          WHERE public.birdtie_optional_moment_content_contract() IS DISTINCT FROM 'moment-optional-content-v1')
 || ARRAY(SELECT 'journey_details.contract' WHERE public.birdtie_journey_details_contract() IS DISTINCT FROM 'journey-native-details-v1')
 || ARRAY(SELECT 'legacy_private_video.contract' WHERE public.birdtie_legacy_private_video_contract() IS DISTINCT FROM 'private-moment-video-v1')
 || ARRAY(SELECT 'legacy_native_context.contract' WHERE public.birdtie_legacy_context_contract() IS DISTINCT FROM 'legacy-native-context-v1')
 || ARRAY(SELECT 'legacy_social.contract' WHERE public.birdtie_legacy_social_contract() IS DISTINCT FROM 'legacy-social-native-v1')
 || ARRAY(SELECT 'legacy_history.contract' WHERE public.birdtie_legacy_history_contract() IS DISTINCT FROM 'legacy-social-history-v1')
 || ARRAY(SELECT 'external_location.contract' WHERE public.birdtie_external_location_contract() IS DISTINCT FROM 'external-location-reference-v1')
 || ARRAY(SELECT 'legacy_reference_media.contract' WHERE public.birdtie_legacy_reference_media_contract() IS DISTINCT FROM 'legacy-reference-media-v1')
 || ARRAY(SELECT 'legacy_private_image_recovery.contract' WHERE public.birdtie_legacy_private_image_recovery_contract() IS DISTINCT FROM 'legacy-private-image-recovery-v1')`

func newNativeDatabasePinger(ctx context.Context, database nativeDatabaseReader, expectedName string) (*nativeDatabasePinger, error) {
	if database == nil || !allowedNativeDatabaseName(expectedName) {
		return nil, errNativeDatabaseBoundary
	}
	pinger := &nativeDatabasePinger{database: database, expectedName: expectedName}
	oid, err := pinger.probe(ctx)
	if err != nil {
		return nil, errNativeDatabaseBoundary
	}
	pinger.startupOID = oid
	return pinger, nil
}

func (p *nativeDatabasePinger) probe(ctx context.Context) (int64, error) {
	var name string
	var oid int64
	var legacySource bool
	var missing []string
	if p == nil || p.database == nil || ctx == nil {
		return 0, errNativeDatabaseBoundary
	}
	err := p.database.QueryRow(ctx, nativeDatabaseProbeSQL).Scan(&name, &oid, &legacySource, &missing)
	if err != nil || ctx.Err() != nil || name != p.expectedName || oid < 1 || legacySource || len(missing) != 0 {
		return 0, errNativeDatabaseBoundary
	}
	return oid, nil
}

// /readyz uses this same guard on every call. A connected socket cannot make a
// changed database identity or removed native schema ready again.
func (p *nativeDatabasePinger) Ping(ctx context.Context) error {
	oid, err := p.probe(ctx)
	if err != nil || p.startupOID < 1 || oid != p.startupOID {
		return errNativeDatabaseBoundary
	}
	return nil
}

type backgroundMaintenanceStore interface {
	EnqueueStartsSoonReminders(context.Context) (int64, error)
	PurgeExpiredAgentVisualPixels(context.Context) (int64, error)
}

// Disabling the scheduler does not disable approved request handlers. No
// goroutine, ticker, reminder or purge is created until explicitly enabled.
func startBackgroundMaintenance(ctx context.Context, enabled bool, store backgroundMaintenanceStore) {
	if !enabled || store == nil || ctx == nil || ctx.Err() != nil {
		return
	}
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			if ctx.Err() != nil {
				return
			}
			runCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			started := time.Now()
			count, err := store.EnqueueStartsSoonReminders(runCtx)
			if err != nil && ctx.Err() == nil {
				// Database errors may contain private SQL parameters or a DSN.
				log.Printf("activity_reminder_run status=unavailable source=api duration_ms=%d", time.Since(started).Milliseconds())
			} else if err == nil {
				log.Printf("activity_reminder_run status=ok source=api inserted=%d duration_ms=%d", count, time.Since(started).Milliseconds())
			}
			if runCtx.Err() == nil {
				if _, cleanupErr := store.PurgeExpiredAgentVisualPixels(runCtx); cleanupErr != nil && ctx.Err() == nil {
					log.Print("agent_visual_cleanup status=unavailable")
				}
			}
			cancel()
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}
