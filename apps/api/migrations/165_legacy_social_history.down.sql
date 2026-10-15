BEGIN;
DO $$BEGIN IF EXISTS(SELECT 1 FROM legacy_social_history_runs)THEN RAISE EXCEPTION 'preserved history cannot be discarded';END IF;END$$;
DROP TRIGGER historical_inbox_insert ON inbox_items;DROP FUNCTION birdtie_guard_historical_inbox();ALTER TABLE inbox_items DROP CONSTRAINT inbox_legacy_history_shape;ALTER TABLE inbox_items DROP COLUMN origin;
ALTER TABLE inbox_items DROP CONSTRAINT inbox_items_resource_type_check;ALTER TABLE inbox_items ADD CONSTRAINT inbox_items_resource_type_check CHECK(resource_type IN('place_candidate','activity_candidate','community','connection_request','conversation_message','activity_change','activity_cancelled','activity_reminder','community_message','activity_message','organization_membership','agent_task','opportunity_available','business_claim_review','business_update','moment_like','moment_comment'));
DROP TABLE personal_notification_preferences,historical_notification_facts,personal_journey_history_messages,personal_journey_history_threads,legacy_social_history_facts,legacy_social_history_runs;
DROP FUNCTION birdtie_check_legacy_history_fact(),birdtie_legacy_history_counts(),birdtie_legacy_history_contract();
COMMIT;
