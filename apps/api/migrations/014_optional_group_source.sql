-- Owner-created Groups may have no external source URL. The maintainer still
-- identifies the publishing owner; an optional external source is not verified.
ALTER TABLE communities DROP CONSTRAINT IF EXISTS community_source_present;
ALTER TABLE communities ADD CONSTRAINT community_source_present CHECK (
    length(maintainer_label) > 0 AND
    (source_ref = '' OR length(source_label) > 0)
);
