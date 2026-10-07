BEGIN;

-- Preserve every Place UUID. An address is editorial, sourced input, never
-- inferred from a person, Moment, map viewport or reverse geocoder.
ALTER TABLE place_candidates ADD COLUMN address_label text
    CHECK (address_label IS NULL OR
      (length(trim(address_label)) BETWEEN 1 AND 240 AND location_precision='point'));
ALTER TABLE places ADD COLUMN address_label text
    CHECK (address_label IS NULL OR
      (length(trim(address_label)) BETWEEN 1 AND 240 AND location_precision='point'));

COMMIT;
