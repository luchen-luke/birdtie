BEGIN;

-- Existing conversation requests keep their city and consent semantics.
-- A friend request is explicit, global and creates no chat by itself.
ALTER TABLE connection_requests DROP CONSTRAINT connection_requests_scope_check;
ALTER TABLE connection_requests ADD CONSTRAINT connection_requests_scope_check
    CHECK (scope IN ('conversation','friend'));
ALTER TABLE connection_requests ALTER COLUMN city_id DROP NOT NULL;
ALTER TABLE connection_requests ADD CONSTRAINT connection_request_scope_city_shape
    CHECK ((scope='conversation' AND city_id IS NOT NULL) OR
           (scope='friend' AND city_id IS NULL));

CREATE TABLE person_ties (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    person_a_account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    person_b_account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    request_id uuid NOT NULL UNIQUE REFERENCES connection_requests(id) ON DELETE RESTRICT,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','removed')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT person_tie_sorted_pair CHECK (person_a_account_id < person_b_account_id),
    CONSTRAINT person_tie_unique_pair UNIQUE (person_a_account_id,person_b_account_id)
);
CREATE INDEX person_ties_person_a_active ON person_ties(person_a_account_id,created_at DESC)
    WHERE status='active';
CREATE INDEX person_ties_person_b_active ON person_ties(person_b_account_id,created_at DESC)
    WHERE status='active';

CREATE OR REPLACE FUNCTION birdtie_validate_person_tie() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE sender uuid; recipient uuid; request_scope text; request_state text;
BEGIN
    SELECT sender_account_id,recipient_account_id,scope,state
    INTO sender,recipient,request_scope,request_state
    FROM connection_requests WHERE id=NEW.request_id;
    IF request_scope IS DISTINCT FROM 'friend' OR request_state IS DISTINCT FROM 'accepted' OR
       LEAST(sender,recipient) IS DISTINCT FROM NEW.person_a_account_id OR
       GREATEST(sender,recipient) IS DISTINCT FROM NEW.person_b_account_id THEN
        RAISE EXCEPTION 'Tie requires accepted matching friend request';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM accounts WHERE id=NEW.person_a_account_id
        AND account_type='person' AND status='active') OR
       NOT EXISTS (SELECT 1 FROM accounts WHERE id=NEW.person_b_account_id
        AND account_type='person' AND status='active') THEN
        RAISE EXCEPTION 'Tie requires two active People';
    END IF;
    IF NEW.status='active' AND EXISTS (SELECT 1 FROM account_blocks b
        WHERE (b.blocker_account_id=NEW.person_a_account_id AND
               b.blocked_account_id=NEW.person_b_account_id) OR
              (b.blocker_account_id=NEW.person_b_account_id AND
               b.blocked_account_id=NEW.person_a_account_id)) THEN
        RAISE EXCEPTION 'blocked People cannot establish active Tie';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER person_tie_validate BEFORE INSERT OR UPDATE OF
    person_a_account_id,person_b_account_id,request_id,status ON person_ties
    FOR EACH ROW EXECUTE FUNCTION birdtie_validate_person_tie();

-- No historical accepted conversation is promoted to a durable Tie.
COMMIT;
