BEGIN;
DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM agent_domain_outbox) OR EXISTS(SELECT 1 FROM agent_consumer_inbox)
      OR EXISTS(SELECT 1 FROM agent_effect_ledger) THEN
        RAISE EXCEPTION '064 down refuses to erase outbox, consumer receipts or effect controls';
    END IF;
END $$;
DROP TABLE agent_effect_ledger;
DROP TABLE agent_consumer_inbox;
DROP TABLE agent_domain_outbox;
DROP FUNCTION birdtie_agent_effect_writer_unavailable();
DROP FUNCTION birdtie_guard_agent_consumer_inbox();
DROP FUNCTION birdtie_guard_agent_outbox();
-- No original content, identity, Agent, Memory, candidate or native version is changed.
COMMIT;
