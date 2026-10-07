# Release & Quality Gates

Status: proposed release gates, to be checked against the current implementation in [Functional MVP Gap Analysis](FUNCTIONAL-MVP-GAP-ANALYSIS.md). Source: `Birdtie_Execution_and_CSSA_Partner_Pack.zip!/Birdtie_Execution_and_CSSA_Partner_Pack/docs/05_RELEASE_QUALITY_GATES.md` (imported 2026-09-30). These gates do not override existing accepted domain contracts.

2026-10-01 local release review: [closed-pilot gate report](../research/2026-10-01-closed-pilot-release-gate.md) records passing local builds/tests and the remaining real-event, organization verification, operator and production-service conditions. The closed pilot is not released.

## 1. Purpose

防止“编译通过 = 功能完成”。每个 requirement 必须通过功能、数据、状态、测试和真机验证。

## 2. Requirement Done Definition

只有以下全部满足才标 DONE：

- acceptance criteria 全部满足
- no mock/hardcoded production result
- backend / persistence 完成（适用时）
- auth/permission 完成（适用时）
- loading/empty/error 完整
- automated test 或 deterministic verification
- analyzer/lint pass
- critical path real-device pass
- documentation 与代码一致

## 3. Global Build Gate

按仓库实际脚本优先；至少适用：

```bash
flutter pub get
flutter analyze
flutter test
flutter build apk --debug
go test ./...
go vet ./...
```

若项目已有 format/lint/integration/Makefile/CI，使用项目命令。

## 4. Now Runtime Gate

- keyboard open/close x20: no marker flicker/recreate
- typing: no result clear
- map move: no selection clear
- map move: no auto destructive search
- Search this area explicit
- stale response never overwrites latest
- one request failure -> one primary error state
- map remains interactive during result sheet/composer states

## 5. Functional Vertical Slice Gate

必须用真实 API/DB 验证：

1. Organization admin creates activity
2. record persisted
3. published activity becomes discoverable
4. user query finds it
5. map pin/entity card opens correct activity
6. detail shows authoritative data
7. RSVP persists
8. Plans reads persisted participation
9. app restart retains state
10. update/cancel propagates appropriately

## 6. Organization Gate

- Draft not public
- unauthorized user cannot edit
- publish under 3 min in usability trial
- edit/cancel works
- participant update notification works

## 7. Pilot Gate

Before CSSA live pilot:

### Content
- verified CSSA page prepared
- at least 1 real pilot event
- fallback/support contact

### Reliability
- no known P0 blockers
- Crash-free internal smoke stable
- API/search stable
- logs/diagnostics available

### Safety
- privacy copy exists
- report/support path exists
- no public real-time precise user location

### Operations
- rollback/manual support plan
- one person assigned to watch pilot logs
- feedback form/interview script ready

## 8. Severity

### P0
Blocks core journey, data integrity, login, publish, discovery, RSVP, privacy, or causes crash/data loss.

### P1
Major UX/reliability issue with workaround; must fix before broader beta.

### P2
Polish/optimization; may ship in closed pilot if non-disruptive.

## 9. Stop Conditions for Codex Automation

Codex must stop and mark BLOCKED instead of inventing behavior when：
- required secret/credential unavailable
- external service contract unknown
- migration could destroy user data without approval
- canonical docs conflict materially
- acceptance test cannot be made truthful without missing product decision

禁止通过 mock success 把 BLOCKED 伪装成 DONE。
