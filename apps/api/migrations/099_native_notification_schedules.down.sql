BEGIN;
DO $$ BEGIN IF EXISTS(SELECT 1 FROM native_notification_schedules) OR EXISTS(SELECT 1 FROM native_notification_schedule_slots)
 OR EXISTS(SELECT 1 FROM native_notification_schedule_deliveries) OR EXISTS(SELECT 1 FROM audit_events WHERE purpose IN ('human_notification_schedule_edit','native_notification_schedule_delivery'))
 THEN RAISE EXCEPTION '099 down refuses to erase schedule preferences, delivery history or touch budgets';END IF;END $$;
DROP FUNCTION birdtie_native_notification_visible(native_notification_decisions);
ALTER FUNCTION birdtie_native_notification_visible_v098(native_notification_decisions) RENAME TO birdtie_native_notification_visible;
DROP FUNCTION birdtie_notification_schedule_delivery_allowed(uuid,native_notification_decisions,boolean);
DROP TRIGGER native_notification_schedule_delivery_guard ON native_notification_schedule_deliveries;
DROP FUNCTION birdtie_guard_notification_schedule_delivery();
DROP TABLE native_notification_schedule_deliveries;
DROP FUNCTION birdtie_notification_digest_current(native_notification_decisions);
DROP TRIGGER native_notification_schedule_slot_guard ON native_notification_schedule_slots;
DROP FUNCTION birdtie_guard_notification_schedule_slot();
DROP FUNCTION birdtie_notification_schedule_wall_time(jsonb,date);
DROP FUNCTION birdtie_notification_schedule_current(native_notification_schedule_slots,boolean);
DROP TABLE native_notification_schedule_slots;
DROP FUNCTION birdtie_notification_schedule_quiet(jsonb,timestamptz);
DROP TRIGGER native_notification_schedule_guard ON native_notification_schedules;
DROP FUNCTION birdtie_guard_notification_schedule();
DROP TABLE native_notification_schedules;
COMMIT;
