# Unified Moment entry audit — 2026-10-16

Scope: current client Moment create/edit/publication routes related to the unified editor. No deployment, full test suite, or device assertions are made here. Root owns final APK/Web 19; social_scope owns the concurrent map material-readiness fix.

## Remaining reachable branches fixed in this batch

- `apps/client/lib/src/legacy/legacy_shell.dart:1819`: all profile create/edit/photo actions now use the same owner/token/origin/identity-epoch/source-bound Now handback. In particular the settings profile (`settings_page.dart:356`) provides no `onOpenOwnedMoment` callback; previously its private edit fell back to `_MomentDraftSheet` and public edit to `PublicMomentControlPage`. Both fallbacks are removed. No-scope buttons are disabled with a clear Now instruction. Old CREATE operation recovery `_MomentOperationRecovery` remains.
- `apps/client/lib/src/workspace/place_detail_sheet.dart:594`: the current `/v1/me/moments` rows bound to the selected place now hand back their original Moment ID. Source guards include read-frame owner/token/workspace/target, list request and row membership. Old `MomentPublicationController`/`MomentPublicationSheet` construction and direct legacy publish/withdraw behavior are removed. Draft and public buttons are now “编辑并检查公开” and “修改或撤回”. This entry itself makes no write request.
- `apps/client/lib/src/legacy/legacy_shell.dart:92` and `public_moment_photo_editor.dart:24`: the reachable original-photo recovery route explicitly sets `recoveryOnly: true`. It can read current state and recover/stop an original operation. It cannot select/initialize new pixels, prepare SAVE/DELETE, or confirm a stale preview after switching into recovery-only mode. Current `canSave`/`canDelete` state does not expose those controls. Existing controller, receipts and media readers remain intact.

## Production call-site sweep after the patch

Read-only `rg` searches across `apps/client/lib/**/*.dart` for constructors, old writer methods and new entry methods found:

- `PublicMomentControlPage`, `MomentPublicationSheet`, `MomentPublicationController`, and `NowPrivateMomentImagePage`: declarations only; no production construction.
- `showPrivateMomentDraft`: declaration only; its internal `_MomentDraftSheet` construction is retained for legacy fixtures. No production caller reaches it. Its remaining CREATE call explicitly supplies `existing: null`.
- `PublicMomentPhotoEditor`: the above profile recovery invocation is the only reachable standalone legacy invocation. The other construction is the old `_creationPanel` renderer in `map_workspace.dart`. `_beginCreation('moment')` returns immediately through `_beginMomentThread` (`map_workspace.dart:1392`); cached/restored non-Moment drafts are strictly keyed/decoded by kind (`now_creation_draft_state.dart:36,81`), while legacy Moment draft migration is handled by `_beginMomentThread`. That old renderer has no new Moment entry.
- `PublicMomentMediaEditor`: current unified entry-local materials (`map_workspace.dart:1089`) and the same legacy creation renderer. The current editor manages media within the selected entry; selecting multiple images does not call `addEntry`. `MomentThreadController.addEntry` is the explicit “再接一条” operation.
- Current `PublicMomentPage` edit/photo actions already use the same Now capability. Its cached media API lifetime and successful whole-thread withdrawal clearing remain unchanged.
- `MomentThreadPanel` whole-thread WITHDRAW remains an intentional separate explicit operation, not a second edit form.
- Old `PrivateMomentController.previewCreate/Update/Publish/PublicUpdate/Withdraw` call sites outside their definitions are confined to the orphan legacy sheet, old creation renderer and unconstructed public-control class. Old operation recovery remains available.

No further reachable old Moment editor/publication branch was found in this scope. This is a source reachability audit, not proof that every app screen was exercised.

## Necessary verification and preserved failures

`work/now-moment-final-entries01` is an actual failed first run: analysis found the unused old private-sheet `existing` optional parameter after removing its last edit caller; the direct compound reached the second place row but tapped before scroll layout settled at the clipped edge. These results are retained unchanged.

`work/now-moment-final-entries02`: scoped analysis exits 0 with no errors/warnings (existing non-fatal style information remains); the single compound `所有详情与本人列表编辑回原Now且旧图片仅核实原操作` passes.

The compound uses the actual production PublicMomentPage, LegacyProfilePage without an edit callback, and PlaceDetailSheet. It verifies all 3 detail controls, profile new/private/public/photo controls and both place controls return to the exact original Now element, preserve its composer state and pass the original target ID. Handoff has zero write requests and zero old `/publication` requests. Owner/token/origin/workspace/source/busy changes refuse handoff; owner ABA invalidates pending loading. Without a scope, controls remain disabled.

The same compound seeds a secure original photo journal and follows the actual production recovery route: original GET returns 404, explicit bodyless STOP succeeds, receipt is STOPPED and original journal clears. Current state deliberately permits SAVE and DELETE, but no selection, preview or deletion control appears before or after recovery. No preview/PUT is sent. The one non-GET is the explicitly confirmed original STOP.

No full legacy test suite, native backend test, Flutter build, server deployment or phone operation was run by this batch. Old tests expecting these removed independent editors require a changed expectation and were not represented as passing. Map material readiness belongs to social_scope; backend/source schema 168 is unchanged.
