-- Private intent to attend an Activity. This is neither registration nor
-- participation and does not change the public Activity object.
CREATE TABLE IF NOT EXISTS activity_plans (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    activity_id uuid NOT NULL REFERENCES activities(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT activity_plan_unique_owner_target UNIQUE (owner_account_id, activity_id)
);
CREATE INDEX IF NOT EXISTS activity_plans_owner_recent
    ON activity_plans(owner_account_id, created_at DESC, id DESC);
