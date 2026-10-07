# V4 对既有任务的逐项核验矩阵

日期：2026-10-01。由 `automation/codex_task_queue.json` 生成；每行的验收证据全文及时间戳保留在 live queue。`V4 交叉项` 表示需要复用或扩展的关联，并不把旧 DONE 自动升级为 V4 DONE。旧任务均保留，不删除、不重置。

| 既有 ID | Live 状态 | V4 交叉项 | 既有证据或未完成条件（摘要） |
| --- | --- | --- | --- |
| `BT-AUD-001` | DONE | `BT-V4-AUD-002` | Reviewed Flutter/Go/SQL code paths in docs/product/FUNCTIONAL-MVP-GAP-ANALYSIS.md; 13/13 domains classified with 50 resolvable code links; 39 queue IDs match workbook… |
| `BT-RUN-001` | DONE | `BT-V4-TST-001` | apps/client/tool/run_device_debug.ps1 -CheckOnly passed on ADB device c641566b; explicit adb reverse tcp:3692 to tcp:3692; device curl /readyz HTTP 200 and /v1/cities/… |
| `BT-RUN-002` | DONE | `BT-V4-TST-001` | Go request-ID tests, go test ./..., go vet ./..., go build ./...; Flutter analyze and 27 tests passed; debug APK built and installed on c641566b; device /readyz echoed… |
| `BT-DAT-001` | DONE | `BT-V4-MIG-001` | Migration 021 applied twice idempotently on local PostgreSQL, down migration succeeded and 021 reapplied; core_schema_021_verify.sql transaction passed organization ho… |
| `BT-DAT-002` | DONE | `BT-V4-MIG-001` | Clean isolated DB: 27 migrations PASS; seed 001+002 twice PASS, CityContext/Places/Organizations/future Activities/People/Memberships=1/3/2/5/2/3; isolated API and phy… |
| `BT-AUT-001` | DONE | `BT-V4-PIL-001` | Physical device c641566b local dev-phone login succeeded, secure Session survived force-stop/relaunch, logout remained signed out after another relaunch; API /v1/me id… |
| `BT-ORG-001` | DONE | `BT-V4-ORG-002` | Owner/admin membership persisted in DB and SQL-gated organization profile update passed local API integration: owner 200, admin 200, member 403, nonmember 403, anonymo… |
| `BT-ACT-001` | DONE | `BT-V4-ACTY-001` | Local API+Postgres integration passed: validation 400, member create/edit/publish 403, draft 201 and absent from public list, edit revision 2, publish 200 and visible… |
| `BT-ORG-002` | DONE | `BT-V4-ORG-002` | Chinese-first self-service organization activity console implemented; Flutter analyze 0 issues, 32 tests passed, debug APK built and reinstalled on c641566b; real devi… |
| `BT-SRC-001` | DONE | `BT-V4-OPP-001` | GET city activities supports validated bounds, RFC3339 interval and category filters against PostgreSQL; live API checks returned 2 in bounds, 0 outside, 1 in interval… |
| `BT-PUL-001` | DONE | `BT-V4-NOW-001` | Area Pulse API aggregates real public eligible Activity rows with required viewport and optional RFC3339 time; live API returned populated total 2/category badminton 2… |
| `BT-NOW-001` | DONE | `BT-V4-NOW-001` | Now connects to real Pulse API for viewport counts and stable Activity ID map pins; initial valid viewport or explicit area action requests snapshot, camera movement d… |
| `BT-MAP-001` | DONE | `BT-V4-MAP-002` | Android device c641566b shows real Aberdeen Mapbox basemap and cluster pin; automation/verify_map_keyboard.ps1 passed 20/20 keyboard cycles with Now foreground, compos… |
| `BT-MAP-002` | DONE | `BT-V4-MAP-002` | Native Mapbox semantic marker bitmap covers Activity/Group/Place/Person/Cluster; stable entity and sorted cluster IDs plus incremental annotation fingerprint diff. Fix… |
| `BT-MAP-003` | DONE | `BT-V4-MAP-002` | Camera drag preserves ResultSet/selection and clears stale CTA until latest settled bounds; explicit tap requests Pulse and Agent for those bounds; stale async respons… |
| `BT-NOW-002` | DONE | `BT-V4-NOW-001` | Separate NowPulse and ActiveIntentSummary UI models; top-left Pulse capped at total plus two factual categories, hidden on camera move/active task; structured action/c… |
| `BT-AGT-001` | DONE | `BT-V4-INT-005` | Intent corpus, real API activity/organization/place and signed-in two-turn refine/compare/restore; Go test/vet/build and Flutter analyze/39 tests passed; docs/research… |
| `BT-AGT-002` | DONE | `BT-V4-INT-005` | Go contract unit tests + go test/vet/build, Flutter analyze and parser/action widget tests; live API result refs for 1 organization/2 places/2 activities, empty and un… |
| `BT-AGT-003` | DONE | `BT-V4-INT-005` | Flutter analyze/41 tests, Go test/vet/build, debug APK installed and DevTools attached; device query answer before 2 real test results, Chinese follow-up distance resp… |
| `BT-AGT-004` | DONE | `BT-V4-INT-005` | Signed-in two-turn area API integration retained task/resultSet IDs and badminton/weekend filters through restore; anonymous bounds request worked without local task I… |
| `BT-NOW-003` | DONE | `BT-V4-NOW-001` | Flutter analyze and 66 tests passed; Android debug APK built and installed on c641566b; pin card, Agent results, expanded conversation and detents reviewed on device;… |
| `BT-RSV-001` | DONE | `BT-V4-ACTN-001` | docs/research/2026-10-01-rsvp-device-verification.md: Go test/vet/build, Flutter analyze/43 tests/build, real API duplicate/full/cancelled cases, Android RSVP cancella… |
| `BT-PLN-001` | DONE | `BT-V4-PLN-001` | docs/research/2026-10-01-my-activities-verification.md: real participation list, API upcoming/past, Flutter 44 tests/analyze, Go test/vet/build, APK install and device… |
| `BT-SAV-001` | DONE | `BT-V4-ACTN-001` | docs/research/2026-10-01-save-rsvp-independence.md: E2E API save/unsave without RSVP change, Android detail save/unsave while joined, Go test/vet/build, Flutter analyz… |
| `BT-DET-001` | DONE | `BT-V4-ACTN-001` | docs/research/2026-10-01-activity-detail-verification.md: Flutter analyze/46 tests/debug APK/device; Go test/vet/build; real API full fields; native share, map navigat… |
| `BT-NTF-001` | DONE | `BT-V4-NOT-001` | Migration 023 and atomic participant notifications; idempotent reminder worker; loopback integration script PASS for reminder/edit/cancel/link/read/targeting; Android… |
| `BT-INB-001` | DONE | `BT-V4-CHT-002` | Migration 024 conversation target/backfill and Chinese message notifications; removed fake preview messages; Inbox empty/error/retry/read/link behavior and Flutter wid… |
| `BT-ORG-003` | DONE | `BT-V4-ORG-003` | Public Organization API exposes stored verification state, HTTPS links and upcoming public Activities; private profile 404 and synthetic integration PASS with original… |
| `BT-OAG-001` | DONE | `BT-V4-ORG-001` | Migration 025 and Chinese FAQ admin/public ask UI; verified public FAQ/profile/activity/link answers cite stored sources, unknown and unverified questions do not inven… |
| `BT-ANA-001` | DONE | `BT-V4-ANA-001` | Migration 026 and privacy-limited activity events; Now/Agent impressions and sourced detail openings, server-side idempotent RSVP/save conversions, admin-only aggregat… |
| `BT-SAF-001` | DONE | `BT-V4-SAF-001` | Audited Android manifest, API routes, published Place/Activity/Group and opt-in Person area query; no device GPS permission or public precise tracking route. Added ser… |
| `BT-SAF-002` | DONE | `BT-V4-SAF-003` | Migration 027, owner-private report/support API with rate limit and operator queue; same-transaction person-attributed organization/activity/FAQ audit; Chinese Setting… |
| `BT-PER-001` | DONE | `BT-V4-MAP-002` | Client turn serial ignores stale Agent responses; Pulse serial also invalidates pending area when returning to cached area. Composer permits next send during loading.… |
| `BT-PER-002` | DONE | `BT-V4-MAP-002` | Flutter profile VM timeline captured pan, keyboard and sheet; removed per-frame timer churn and same-zoom marker sync; Flutter analyze and 66 tests pass; updated debug… |
| `BT-TST-001` | DONE | `BT-V4-TST-001` | docs/research/2026-10-01-vertical-slice-e2e.md: repeatable loopback API script twice (retained and cleanup), admin publish/student discovery/Agent/detail/RSVP/Plans PA… |
| `BT-REL-001` | BLOCKED | `BT-V4-PIL-003` | 2026-10-01 Closed Pilot Ready NO. All repository-executable P0 code tasks RUN-003/ORG-004/MAP-004/NTF-002 completed with local evidence, but AUT-002 lacks configured r… |
| `BT-RUN-003` | DONE | `BT-V4-TST-001` | Central BirdtieEnvironment across API and map; flutter analyze PASS; flutter test PASS 72; direct release build rejected; release wrapper rejected missing, localhost a… |
| `BT-AUT-002` | BLOCKED | `BT-V4-PIL-001` | Native Android/iOS OIDC callback, secure 5-minute PKCE pending state, API verified account check, secure session restoration and revocation implemented. Flutter analyz… |
| `BT-ORG-004` | DONE | `BT-V4-ORG-002` | Migration 028 forward 28/28 blank DB, 028 down/reapply and dev seeds PASS. Go test/vet/build PASS. Flutter analyze 0, test 76, debug APK build/install c641566b PASS. a… |
| `BT-MAP-004` | DONE | `BT-V4-SAF-001` | 2026-10-01: migration 029 forward/down/up PASS; loopback API privacy matrix PASS via automation/verify_organization_map.py (pending/private/unverified/hidden/reedited… |
| `BT-NTF-002` | DONE | `BT-V4-NOT-001` | 2026-10-01 code/local acceptance: independent worker while API stopped inserted=1, retry inserted=0, API restart Inbox exactly one; wrong DB port exit 1 status=failed;… |
| `BT-TST-002` | BLOCKED | `BT-V4-TST-001` | 2026-10-01 dependency BT-AUT-002 BLOCKED and real A-H prerequisites absent: verified organizer authority/organization; organizer-confirmed activity and public location… |
| `BT-PIL-001` | TODO | `BT-V4-PIL-002` | 尚未领取；依赖/外部准入见 live queue。 |
| `BT-PIL-002` | TODO | `BT-V4-PIL-002` | 尚未领取；依赖/外部准入见 live queue。 |
| `BT-POL-001` | TODO | `BT-V4-NOW-001` | 尚未领取；依赖/外部准入见 live queue。 |
| `BT-COM-001` | DONE | `BT-V4-COMM-001` | Canonical identity model, COMMUNITY-AND-ACTIVITY-SOCIAL-MODEL.md (10 sections), ADR 0016 and architecture index updated; rg confirmed links and definitions; git diff -… |
| `BT-COM-002` | DONE | `BT-V4-COMM-001` | 030 migration up/down/reapply all psql exit 0 on local Postgres; synthetic two-person Community and membership transaction passed and rolled back; deleting sole owner… |
| `BT-COM-003` | DONE | `BT-V4-ACTY-001` | 031 organizer migration up/down/reapply passed; existing Activity count 9, organizer count 9, all 9 Organization, 0 orphan; synthetic Person and Community organizer in… |
| `BT-COM-004` | DONE | `BT-V4-COMM-001` | Server-side PostgreSQL methods enforce owner/admin/member/outsider boundaries, invite, role, transfer and archive; TestCommunitySocialPermissionsIntegration passed aga… |
| `BT-COM-005` | DONE | `BT-V4-COMM-001` | Community REST routes and PostgreSQL CRUD/membership/request/invite/role/transfer/archive implemented; TestCommunitySocialHTTPIntegration passed against local Postgres… |
| `BT-COM-006` | DONE | `BT-V4-ACTY-002` | 032 migration up/down/reapply passed; shared SQL ACL enforces public, organizer_members and invite_only in list/detail/RSVP and DB participation guard; local Postgres… |
| `BT-COM-007` | DONE | `BT-V4-COMM-001` | 2026-10-01 Flutter test/community_api_test.dart + community_page_test.dart 4/4 PASS; Go postgres/httpapi integration PASS with local PostgreSQL. Android c641566b debug… |
| `BT-COM-008` | DONE | `BT-V4-ACTY-001` | Flutter analyze PASS; flutter test activity_detail_sheet_test.dart activity_organizer_test.dart community_page_test.dart PASS; physical Android c641566b screenshots wo… |
| `BT-COM-009` | DONE | `BT-V4-COMM-001` | Local-only dev-seeds/003_community_social.sql applied twice: INSERT 3+2 then 0+0; DB verifies 3 fixtures/3 active owners/0 Community agents. automation/verify_communit… |
| `BT-COM-010` | DONE | `BT-V4-TST-001` | Flutter analyze PASS; flutter test PASS 87 tests; flutter build apk --debug PASS (app-debug.apk 241659750 bytes). Go test ./... -count=1 with PostgreSQL PASS; go vet .… |
| `BT-COM-011` | DONE | `BT-V4-E2E-003` | Physical Android c641566b 10/10 synthetic scenarios; docs/testing/COMMUNITY-SOCIAL-PHYSICAL-DEVICE-2026-10-01.md; C public RSVP activity 37d7ceac-f86f-4998-acdd-1eae15… |
| `BT-COM-012` | DONE | `BT-V4-COMM-001` | docs/reports/FUNCTIONAL-MVP-COMPLETION-REPORT.md Community section lists all BT-COM-001..012, migration/Go/Flutter files, full verification and separate Community YES… |

## 读法与边界

- 旧 45 项 Functional MVP 工作与新增 12 项 Community 工作合计 57 项：51 DONE、3 TODO、3 BLOCKED。队列无 PARTIAL 状态枚举；功能差距的 PARTIAL 分类在产品差距分析中，不伪装成完成。
- `BT-AUT-002`、`BT-TST-002`、`BT-REL-001` 的 P0 BLOCKED 原样保留。V4 额外的三项 `BT-V4-PIL-*` 是新阶段试点门槛，不应与旧任务重复报为已完成。
- 旧 Connection/Chat 与 Intent/Place 数据结构可复用，V4 对应任务仍需按新增语义重新验收。旧 Community 和 Organization Activity 通过了本地合成数据/真机，不证明 Business 主体、跨城在线意图或真实 CSSA 试点。
- 证据定位：`docs/reports/FUNCTIONAL-MVP-COMPLETION-REPORT.md`、`docs/testing/COMMUNITY-SOCIAL-PHYSICAL-DEVICE-2026-10-01.md`，以及 live queue 每项 `evidence`/`blocked_reason`。
