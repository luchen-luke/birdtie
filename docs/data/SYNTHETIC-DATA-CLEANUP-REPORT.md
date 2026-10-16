# Synthetic data cleanup — DGS-01 / DGS-06 / DGS-07 / DGS-08

Date: 2026-10-16. Current status: **BOUNDED_PRODUCTION_CLEANUP_PASS**. Prevention of future synthetic injection and client-visible cache acceptance remain separate unfinished checks.

This report covers a bounded cleanup of proven historical remote acceptance fixtures. It does not classify content by name, text prefix, author, or date alone. Unknown content, real Civu content, user accounts, and shared media remain protected.

## Runtime and provenance

- Native database: `birdtie_native_hz_20261015`, OID `54087`; schema maximum/count `173`.
- Active API release: `/opt/birdtie/releases/social173-20261016-build30`; SHA-256 `4cc70262b66cffa0061a79fa91fdc7f622de23aedd5552a713b5e1070a7b9900`; unit active/running.
- Original Civu service was observed failed/failed. This audit made no change to that service or to Civu data.
- Protected original `source.json` SHA-256: `ba6b9b5d2d404ada806a858cbc0931628a5958c4b2abdbd8b7d50b62b4e6f40d`; binding to its manifest verified. This original source backup is **not** the target-native cleanup backup.
- Actual runtime has fixed-code development phone authentication enabled, as requested by the user. It is not disabled by cleanup. Background maintenance is disabled.
- Exact local `dev-seeds` UUIDs and `dev-seed://` source references matched **zero** current target rows. The presence of seed SQL in the repository does not prove remote contamination.
- `apps/api/compose.yaml` mounts local seed files read-only; the inspected production compose/deployment path does not auto-apply these seeds. `cmd/now-live-prepare` requires its named local-only database prefix and loopback connection. Historical manually run remote acceptance scripts are a separate, proven source of fixtures; they were read, never re-executed during this audit.

## Exact scope

Five immutable prior receipts are bound by SHA-256 in `scripts/audit_birdtie_synthetic_data.py`: remote friend/chat flows 01/02, media/thread flow 01, cross-city 166, and multi-location 166. Their exact IDs plus original message/share operation IDs identify the candidate roots. Six script identities are used as an additional ownership check; they are never bulk deletion selectors.

Before cleanup, the 110 moments divided into:

| Classification | Count | Treatment |
|---|---:|---|
| Verified Civu native moment ledger | 83 | Preserve |
| Exact receipt synthetic roots, withdrawn/private | 8 | Removed in the bounded cleanup |
| Insufficient per-item evidence under confirmed script accounts | 18 | Preserve; 15 withdrawn and 3 draft |
| Other native/user-new origin not proven | 1 | Preserve; withdrawn |

The earlier preliminary data03 audit used `kind='note'` against the native-adaptation ledger. That was incorrect: native moment mappings use `kind='moment'`. Corrected data05 and the executable manifest supersede that classification; the preliminary output is retained as a failure record.

The reviewed closure contained **22 tables / 103 rows**, all removed in the approved production transaction at 2026-10-16 09:22:51 UTC:

| Category | Rows |
|---|---:|
| Moments / threads / thread members / thread operations | 8 / 1 / 2 / 2 |
| Conversation / exact operation-proven messages / member states | 1 / 3 / 2 |
| Requests / decision receipts / remaining tie / close selections | 2 / 2 / 1 / 2 |
| Inbox / notification decisions / entity-share operation | 7 / 7 / 1 |
| Moment control operations | 20 |
| Location links / epochs / compatibility facts | 10 / 8 / 6 |
| Public media sets / attachments / operations / retired video identity | 3 / 11 / 3 / 1 |

The thread has no member outside the eight exact moment roots. The conversation has no non-synthetic member and no message lacking exact operation evidence. There are no comments, likes, real-user references, or legacy facts in this closure. Immutable general audit history is retained. No account, bucket object, shared file, or private source original is deleted.

## Safeguards and recovery

`audit_birdtie_synthetic_data.py` defaults to SELECT-only transactions. `purge_birdtie_synthetic_data.py` also defaults to dry-run; backup preparation and application require separate flags.

The manifest checks database identity/owner/marker, schema, active release and binary hash, original-source hash, root IDs, receipt hashes, row hashes, 78 reference-table predicates, and 14 global account/media/import protection digests. Foreign-key and semantic references form the bounded closure. Any unsupported table, legacy source, non-synthetic actor, new reference, or changed protected row blocks application.

The deletion transaction locks its affected/reference tables, checks the manifest again, keeps foreign keys enabled, and pauses only twelve named DELETE guards that otherwise prohibit deleting even test fixture history. All twelve are restored before commit; any error rolls the transaction back. The existing moment-memory invalidation trigger remains active and must insert exactly eight invalidations. No `session_replication_role`, blanket trigger disable, or `TRUNCATE` is used.

Full native backup: **502,639,481 bytes**, protected mode `0600`, SHA-256 `0ec21f783bb0c05a255fccb78203e12287307baf5cc6dbb6e8bf4e2525414319`. Its protected server path is recorded in the backup receipt. A separately marked restore database, OID `75920`, reproduced the selected and protected row digests; the actual cleanup and subsequent no-op check both succeeded there. The disposable restore database was dropped after verification.

The first restore attempts exposed a pre-existing compatibility issue: migration 146's validation functions call `birdtie_legacy_fixed_keys` without a schema qualification, while normal `pg_restore` sets an empty search path. The restore pipeline adjusts exactly the SQL header's search path to `public,pg_catalog`; it does not change the original dump, COPY rows, SQL function bodies, or production schema. The failed attempts remain recorded.

## Verified / not run

| Check | Result |
|---|---|
| Six direct safety tests: scope/unknown dependencies, actor exclusions, FK order/cycles, restore requirement, identifier escaping, restore-header boundary | PASS |
| Current exact manifest and 0 unreviewed references | PASS |
| Full backup, actual isolated restore, source/target digest comparison | PASS |
| Actual isolated 103-row cleanup; protected rows unchanged; FKs and triggers retained | PASS |
| Second isolated check: ALREADY_CLEAN, no new invalidations | PASS |
| Disposable restore database cleanup | PASS |
| Production apply / post-apply reconciliation | PASS; 103 rows removed, 102 other moments retained |
| Production second application | ALREADY_CLEAN; no additional deletion or invalidation |
| Client cache and user-visible post-cleanup acceptance | NOT_RUN |
| New identity/media migration or broader social UI acceptance | NOT_RUN in this audit |

## Civu reconciliation limits

The protected source contains 20 users, 103 notes, and 407 media asset rows. Of the notes, 83 are undeleted/public/approved, 19 deleted/public, and one deleted/private. Current native adaptation has 83 moment mappings; all 103 note archives remain. Native imported moments are stored as private drafts; their eligible original public read path is separately bound to original visibility, original content/revision, moderation/deletion and current permissions. These storage flags are not evidence of either blanket publication or failed migration.

Existing ledgers include 5,245 adapted places, 247 unplaced-place contexts, 5,492 map coordinate facts, 16 independent map anchors, 22 native threads, and 83 author facts. Media-ledger counts include 135 private-image import items, 31 recovery items, five video items, and 135 reference assets; these are different/overlapping units and must not be summed into a claim that all 407 assets have been read or exposed.

Existing social ledgers contain 21 follows, five friendships, 11 conversations, 39 messages, 23 memberships, one image, and one request; separate preserved history contains eight threads, 41 messages, and 110 notifications. These ledger counts establish imported facts, not completed end-to-end UI or permission acceptance.

## Evidence and execution

- [Exact manifest](../testing/evidence/dgs-20261016/data-cleanup/manifest02.json), canonical manifest SHA-256 `22333e1ce5e441f90c7879e0db7fe66c866483111b599a2328f3ab1a56eba802`.
- [Backup / restore / isolated cleanup proof](../testing/evidence/dgs-20261016/data-cleanup/backup-restore04.json).
- [Direct tests](../testing/evidence/dgs-20261016/data-cleanup/direct-tests01.json).
- [Restore failure](../testing/evidence/dgs-20261016/data-cleanup/backup-restore03.json), [earlier failure](../testing/evidence/dgs-20261016/data-cleanup/backup-restore02.json), [first-attempt residue check](../testing/evidence/dgs-20261016/data-cleanup/backup-attempt01-state.json).
- [Actual DELETE trigger review](../testing/evidence/dgs-20261016/data-cleanup/delete-trigger-audit01.json).

After the main task's review, this bounded operation was executed:

```text
python scripts/purge_birdtie_synthetic_data.py --manifest docs/testing/evidence/dgs-20261016/data-cleanup/manifest02.json --apply --backup-receipt docs/testing/evidence/dgs-20261016/data-cleanup/backup-restore04.json --output docs/testing/evidence/dgs-20261016/data-cleanup/apply01.json
```

No production application is implied by the successful restore rehearsal.

The subsequent [production apply](../testing/evidence/dgs-20261016/data-cleanup/apply01.json), [second application](../testing/evidence/dgs-20261016/data-cleanup/apply02.json), and [read-only reconciliation](../testing/evidence/dgs-20261016/data-cleanup/post-apply01.json) establish the actual result. They confirm 83 Civu moments plus 19 unknown moments retained, all 39 legacy conversation messages retained, all 29 accounts retained, 14 global protection digests unchanged, foreign-key triggers enabled, all twelve temporarily paused guards restored, and exactly eight memory invalidations inserted. The API remains active/running on the same release/schema; the protected Civu source hash is unchanged. There are no remaining disposable restore databases.

## Remaining injection-prevention gaps

The cleanup does **not** prove that future fixture creation is disabled. [The gap audit](../testing/evidence/dgs-20261016/data-cleanup/injection-gaps01.json) identifies concrete remaining entry points:

1. `apps/api/dev-seeds/001_badminton.sql`, `002_functional_mvp.sql`, and `003_community_social.sql` have local-only comments, but no executable database-identity or explicit fixture-opt-in guard. An administrator can still manually apply them to an incorrect target.
2. `apps/api/internal/devauth/model.go` normalizes a phone identity and `internal/postgres/dev_phone.go` can enroll or re-authenticate it under the fixed-code mode. The current implementation has no explicit default-deny boundary for the already proven fixture identities/accounts. Preserve the user's fixed-code login while separating fixture authorization from real account login.
3. Historical `work/remote_*` and `work/verify_remote_social*` acceptance scripts target the remote public API; some run their flow immediately when invoked. They were not re-executed and were not edited by this task. A shared, default-deny fixture/environment guard is still needed before any future reuse.

The checked repository local social E2E runner and `now-live-prepare` already enforce isolated/loopback environments. Personal Agent preferences named “seed” and permission-reviewed city catalog candidates are legitimate features and must not be disabled merely because their paths contain that word. No new auth/service/config changes were included in this cleanup.
