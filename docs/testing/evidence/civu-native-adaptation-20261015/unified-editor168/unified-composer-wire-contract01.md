# Unified composer wire contract 01

2026-10-15. Backend implementation contract; existing three actions retain original digest and receipt bytes. New startup marker `public.birdtie_moment_thread_editing_contract()` = `moment-thread-editing-v1` in migration 168.

## EDIT request

Same MOMENT_THREAD family and preview / PUT / GET / stop endpoints. All original top-level fields are required. Additionally EDIT requires `baseMomentId` (string) and `removedItems` (array).

```
{operationId,action:"EDIT",threadId,baseMomentId,localScope,audience:"PUBLIC",
 expectedVersion,items:[{momentId,expectedRevision,restore,input,media}],
 removedItems:[{momentId,expectedRevision}],sourceVersion?,approval?}
```

`threadId==''`: expectedVersion=0. `baseMomentId==''` means all new. Otherwise baseMomentId is one existing standalone owner Moment included in items; its original ID becomes immutable root. It must have no historical thread membership. `threadId!=''`: baseMomentId='', expectedVersion>=1. All original active members must be explicitly retained in items or explicitly listed in removedItems. Historical retired members remain retired unless explicitly included with restore=true. No cross-thread adoption. New items use momentId='',expectedRevision=0,restore=false. Existing items use original UUID and current revision. Items 1..20 in desired display order. Last entry cannot be deleted through EDIT; whole withdrawal is distinct WITHDRAW. Newly added entries get new immutable historical positions; existing historical positions never change.

Each input uses existing closed MomentInput: cityId/placeId/title/body/timePrecision/locationPrecision, optional occurredAt/locations. EDIT additionally accepts original activityId/communityId/organizationId: existing associations must be explicitly echoed unchanged and pass their original current authority gates; no automatic association or replacement is introduced. Title/city/place can be empty; locations may be [] or independently 0..N cross-city. No route is generated. Each published entry requires meaningful body or currently publishable media. Public approval is explicit for every draft/private entry being included. restore=false rejects withdrawn targets; restore=true requires withdrawn target and explicit supplied input. No implicit restoration. Restored locations use newly selected link IDs (withdrawal retired the old IDs). removedItems has at most 20 unique entries.

Media key is required: null means preserve current published media (valid only existing published target); [] explicitly removes media; nonempty array selects freshly ready staged media with original rights, localScope and current source gates. New entries and explicit restoration require [] or new attachments, never null. A failed/unresolved local upload stays local and prevents preview, never becomes an omitted attachment. Preserve media current snapshot is returned as preview `preservedMedia` aligned 1:1 with items (null or existing media Set); `materials`, `contexts`, `locations` also remain aligned; preview members is [] for EDIT.

## New digest (canonical JSON SHA256, same MomentControlHash)

```
["moment-thread-edit-v1", operationId,"EDIT",threadId,baseMomentId,localScope,
 "PUBLIC",expectedVersion,
 items.map(i=>[i.momentId,i.expectedRevision,i.restore,
              MomentControlInputHash(i.input),
              i.media===null ? null : i.media.map(a=>[a.mediaId,a.mediaVersion,a.rights.digest])]),
 removedItems.map(i=>[i.momentId,i.expectedRevision])]
```

Input order and removal order are bound. Old CREATE_PUBLISH/PUBLISH/WITHDRAW digests are unchanged. Same operationId same digest returns original success, even after current state changes; differing digest fails. Unknown results only GET original operation; STOP keeps immutable old STOPPED v1.

## Receipt

EDIT success schema `moment-thread-operation-v2`, all original keys plus required `removedMembers` array. RootMomentId stays original even if it is no longer displayed. `members` includes only target displayed items, in current display order; each has original member keys {momentId,status,position,appliedRevision,mediaSetVersion,media}, position=0..n-1 display index, status=published. `removedMembers` includes explicit removal outcomes, status=withdrawn, position=immutable historical position. Old receipts are unchanged (no removedMembers). WITHDRAW on an edited thread uses v2 with members containing all current display entries withdrawn and removedMembers=[]; its root may not equal members[0]. Existing nonedited whole-withdraw stays v1. Neither GET history nor retry changes original receipt.

## Current reads

Unedited thread keeps original v1. Edited thread public schema=`moment-thread-view-v2`, identical top-level keys, members 1..20 in current display order. rootMomentId is original root only if currently publicly available and active, otherwise null. An unavailable current entry remains an UNAVAILABLE placeholder with null ID/revision0; retired entries are excluded. By hidden/retired Moment ID cannot discover siblings. One active member displays as ordinary Moment.

Edited owned schema=`owned-moment-thread-v2`, original keys plus required retiredMembers array. members current display order using existing position shape. RetiredMembers entries {position,momentId,revision,status:"RETIRED"}; position immutable historical position (may exceed 19), ID/revision visible only to original owner. This includes excluded rows and withdrawn entries that still occupy an UNAVAILABLE current slot after independent/whole withdrawal. The full historical retired list has no 20-entry truncation. Owned rootMomentId retains original ID. Owner can GET existing `/v1/me/moments/{id}` for title/body/time/status (including withdrawn); locations/media remain separately source-gated. Restoring withdrawn media requires explicit ready new selection, because withdrawal retired original published derivatives. Legacy entryContexts/legs retain their original positions and IDs; they are never rewritten into routes by EDIT.

Current preview source binds owner/session, thread version and row stamp, all historical membership/current Moments, display row versions/retirement, each selected source, media carry current source/rights/expiry, all location targets and permissions. One transaction updates original Moments with CAS, media+locations, explicit retirements, append-only historical membership, current display and thread version, and immutable operation receipt. Final gate rechecks source-dependent targets and resulting effect before commit.
