package postgres

// A thread container is also used for one-entry posts. Classification follows
// explicit preserved source membership, never visible rows, media or proximity.
func mapContentThreadIdentitySQL(thread string) string {
	return `(CASE WHEN EXISTS(SELECT 1 FROM legacy_native_context_entities imported_thread WHERE imported_thread.kind='thread' AND imported_thread.target_id=` + thread + `.id) THEN EXISTS(
 SELECT 1 FROM legacy_native_context_entities original_thread
 JOIN legacy_native_context_runs original_run ON original_run.run_id=original_thread.run_id
 JOIN legacy_import_runs sealed ON sealed.run_id=original_run.run_id AND sealed.source_file_sha256=original_run.source_file_sha256 AND sealed.manifest_file_sha256=original_run.manifest_file_sha256
 JOIN legacy_native_context_entities member_origin ON member_origin.kind='thread_member' AND member_origin.run_id=original_thread.run_id AND member_origin.owner_account_id=original_thread.owner_account_id AND member_origin.source_parent_key=original_thread.source_key AND member_origin.source_parent_sha256=original_thread.source_sha256
 JOIN moment_thread_members member ON member.thread_id=original_thread.target_id AND member.moment_id=member_origin.target_id
 JOIN legacy_native_adaptation_entities adapted ON adapted.kind='moment' AND adapted.run_id=member_origin.run_id AND adapted.target_id=member_origin.target_id::text AND adapted.owner_account_id=member_origin.owner_account_id AND adapted.source_key=member_origin.source_key AND adapted.source_sha256=member_origin.source_sha256
 JOIN legacy_note_archives archive ON archive.run_id=adapted.run_id AND archive.source_note_id::text=adapted.source_key AND archive.source_sha256=adapted.source_sha256 AND archive.owner_account_id=adapted.owner_account_id AND NOT archive.deleted
 WHERE original_thread.kind='thread' AND original_thread.target_id=` + thread + `.id AND original_thread.owner_account_id=` + thread + `.author_account_id
 AND archive.source_relationships#>>'{thread,sourceThreadId}'=original_thread.source_key
 AND CASE WHEN jsonb_typeof(archive.source_relationships#>'{thread,entryCount}')='number' THEN (archive.source_relationships#>>'{thread,entryCount}')::numeric BETWEEN 2 AND 20 ELSE false END
 ) ELSE (SELECT count(*) FROM moment_thread_members original_member WHERE original_member.thread_id=` + thread + `.id)>=2 END)`
}

// This preserves the map reader's existing independent legacy grant. An edited
// private container cannot lend its new relationships to a foreign reader.
func mapContentThreadAccessSQL(thread, edit, viewer string) string {
	return `(` + thread + `.status<>'withdrawn' AND(` + thread + `.audience='PUBLIC' OR ` + thread + `.author_account_id=` + viewer + `::uuid OR
 (` + thread + `.version=1 AND ` + edit + `.thread_id IS NULL AND EXISTS(SELECT 1 FROM legacy_native_context_entities original
 JOIN legacy_native_context_runs run ON run.run_id=original.run_id
 JOIN legacy_import_runs imported ON imported.run_id=run.run_id AND imported.source_file_sha256=run.source_file_sha256 AND imported.manifest_file_sha256=run.manifest_file_sha256
 WHERE original.kind='thread' AND original.target_id=` + thread + `.id AND original.owner_account_id=` + thread + `.author_account_id))))`
}
