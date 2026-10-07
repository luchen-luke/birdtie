-- Activity location is explicit. Existing rows without a place retain unknown status.
ALTER TABLE activities ADD COLUMN modality text NOT NULL DEFAULT 'unspecified';
ALTER TABLE activities ADD COLUMN physical_place_status text NOT NULL DEFAULT 'unknown';
ALTER TABLE activities ADD COLUMN venue_place_id uuid REFERENCES venues(place_id);

UPDATE activities SET modality='in_person', physical_place_status='confirmed'
WHERE place_id IS NOT NULL;

ALTER TABLE activities ADD CONSTRAINT activity_physical_place_consistent CHECK (
    (modality='unspecified' AND physical_place_status='unknown' AND place_id IS NULL AND venue_place_id IS NULL) OR
    (modality IN ('in_person','hybrid') AND physical_place_status='confirmed' AND place_id IS NOT NULL
        AND (venue_place_id IS NULL OR venue_place_id=place_id)) OR
    (modality IN ('in_person','hybrid') AND physical_place_status='tbd' AND place_id IS NULL AND venue_place_id IS NULL) OR
    (modality='online' AND physical_place_status='not_applicable' AND place_id IS NULL AND venue_place_id IS NULL)
);

CREATE OR REPLACE FUNCTION birdtie_activity_place_venue_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    -- Compatibility for trusted legacy imports that provide a Place but omit new fields.
    IF TG_OP='INSERT' AND NEW.modality='unspecified' AND NEW.physical_place_status='unknown'
        AND NEW.place_id IS NOT NULL THEN
        NEW.modality := 'in_person';
        NEW.physical_place_status := 'confirmed';
    END IF;
    IF NEW.place_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM places p WHERE p.id=NEW.place_id AND p.city_id=NEW.city_id
          AND p.publication_status='published' AND (p.expires_at IS NULL OR p.expires_at>now())
    ) THEN
        RAISE EXCEPTION 'activity place must be current and public';
    END IF;
    IF NEW.venue_place_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM venues v JOIN places p ON p.id=v.place_id
        JOIN venue_candidates vc ON vc.id=v.source_candidate_id
        WHERE v.place_id=NEW.venue_place_id AND v.city_id=NEW.city_id
          AND v.expires_at>now() AND vc.status='approved'
          AND vc.place_id=v.place_id AND vc.city_id=v.city_id
          AND vc.reviewed_by=v.reviewed_by
          AND p.publication_status='published'
          AND (p.expires_at IS NULL OR p.expires_at>now())
    ) THEN
        RAISE EXCEPTION 'activity venue must be current and reviewed';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER activity_place_venue_guard BEFORE INSERT OR UPDATE OF
    city_id,place_id,venue_place_id,modality,physical_place_status,publication_status ON activities
    FOR EACH ROW EXECUTE FUNCTION birdtie_activity_place_venue_guard();
