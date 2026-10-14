package postgres

import "strings"

// Apply new retained-field permissions to the original public Place readers.
// A legacy NULL source is preserved as legacy data; it is never made a
// confirmed local-answer fact. Once a place has a new permission-bound source,
// its copied public fields require current matching independently reviewed
// evidence. Revocation cannot leave those copies visible in another reader.
func currentRetainedPlaceVisibilitySQL(alias, clock string) string {
	return strings.NewReplacer("@P@", alias, "@N@", clock).Replace(`(
 NOT EXISTS(SELECT 1 FROM place_sources managed_source WHERE managed_source.place_id=@P@.id AND managed_source.permission_id IS NOT NULL)
 OR (
 EXISTS(SELECT 1 FROM place_field_evidence f JOIN place_source_permissions rights ON rights.id=f.permission_id JOIN place_candidates origin ON origin.id=f.source_candidate_id
 WHERE f.place_id=@P@.id AND rights.source_url=@P@.source_ref AND origin.permission_id=rights.id AND origin.status IN('published','linked_duplicate') AND rights.state='CONFIRMED' AND rights.revoked_at IS NULL
 AND rights.version=f.permission_version AND rights.reviewed_by IS NOT NULL AND rights.reviewed_by<>rights.declared_by AND rights.reviewed_at<=@N@ AND rights.retention_until>@N@
 AND f.valid_from<=@N@ AND f.valid_to>@N@ AND f.refresh_due>@N@ AND f.conflict_state='NONE' AND f.observed_at<=@N@ AND f.verified_at<=@N@
 AND f.field_key='name' AND f.value=to_jsonb(@P@.name))
 AND (coalesce(@P@.address_label,'')='' OR EXISTS(SELECT 1 FROM place_field_evidence f JOIN place_source_permissions rights ON rights.id=f.permission_id JOIN place_candidates origin ON origin.id=f.source_candidate_id
 WHERE f.place_id=@P@.id AND rights.source_url=@P@.source_ref AND origin.permission_id=rights.id AND origin.status IN('published','linked_duplicate') AND rights.state='CONFIRMED' AND rights.revoked_at IS NULL
 AND rights.version=f.permission_version AND rights.reviewed_by IS NOT NULL AND rights.reviewed_by<>rights.declared_by AND rights.reviewed_at<=@N@ AND rights.retention_until>@N@
 AND f.valid_from<=@N@ AND f.valid_to>@N@ AND f.refresh_due>@N@ AND f.conflict_state='NONE' AND f.observed_at<=@N@ AND f.verified_at<=@N@
 AND f.field_key='address' AND f.value=to_jsonb(@P@.address_label)))
 AND (@P@.location_precision<>'point' OR EXISTS(SELECT 1 FROM place_field_evidence f JOIN place_source_permissions rights ON rights.id=f.permission_id JOIN place_candidates origin ON origin.id=f.source_candidate_id
 WHERE f.place_id=@P@.id AND rights.source_url=@P@.source_ref AND origin.permission_id=rights.id AND origin.status IN('published','linked_duplicate') AND rights.state='CONFIRMED' AND rights.revoked_at IS NULL
 AND rights.version=f.permission_version AND rights.reviewed_by IS NOT NULL AND rights.reviewed_by<>rights.declared_by AND rights.reviewed_at<=@N@ AND rights.retention_until>@N@
 AND f.valid_from<=@N@ AND f.valid_to>@N@ AND f.refresh_due>@N@ AND f.conflict_state='NONE' AND f.observed_at<=@N@ AND f.verified_at<=@N@
 AND f.field_key='coordinates' AND f.value=jsonb_build_object('coordinateSystem',@P@.coordinate_system,'latitude',@P@.latitude,'longitude',@P@.longitude)))
 ))`)
}

// A bounded public projection must also expire before any retained evidence
// used to keep the new copied Place fields visible. No lifetime is renewed by
// copying the fields into a canonical row.
func currentRetainedPlaceDeadlineSQL(alias, clock string) string {
	return strings.NewReplacer("@P@", alias, "@N@", clock).Replace(`least(@P@.expires_at,
 (SELECT min(least(f.valid_to,f.refresh_due,rights.retention_until)) FROM place_field_evidence f JOIN place_source_permissions rights ON rights.id=f.permission_id
 WHERE f.place_id=@P@.id AND rights.source_url=@P@.source_ref AND f.field_key IN('name','address','coordinates') AND rights.state='CONFIRMED' AND rights.revoked_at IS NULL AND rights.version=f.permission_version
 AND f.valid_from<=@N@ AND f.valid_to>@N@ AND f.refresh_due>@N@ AND rights.retention_until>@N@ AND f.conflict_state='NONE'))`)
}
