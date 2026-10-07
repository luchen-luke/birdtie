BEGIN;

-- Activity uses the existing moment_activity_links relation from 006. These
-- two additive relations keep each referenced principal under a real FK.
CREATE TABLE moment_community_links (
    moment_id uuid PRIMARY KEY REFERENCES moments(id) ON DELETE CASCADE,
    community_id uuid NOT NULL REFERENCES communities(id),
    author_confirmed_at timestamptz NOT NULL
);
CREATE INDEX moment_community_links_community ON moment_community_links(community_id, moment_id);

CREATE TABLE moment_organization_links (
    moment_id uuid PRIMARY KEY REFERENCES moments(id) ON DELETE CASCADE,
    organization_id uuid NOT NULL REFERENCES organizations(id),
    author_confirmed_at timestamptz NOT NULL
);
CREATE INDEX moment_organization_links_organization ON moment_organization_links(organization_id, moment_id);

COMMIT;
