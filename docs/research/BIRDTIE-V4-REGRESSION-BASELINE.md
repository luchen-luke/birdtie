# Birdtie V4 旧业务回归基线

日期：2026-10-01。范围：本地开发 PostgreSQL、Go API、Flutter Debug 和合成资料。目的：V4 扩展不能破坏既有 Organization→Activity→Discovery/Agent→RSVP→Plans 链路，以及 Community 主办方和地图生命周期。真实身份/CSSA/正式部署仍需单独验收。

## 每类变更的最小检查

| 变更 | 必跑检查 | 必须保留的行为 |
| --- | --- | --- |
| 纯文档/队列 | `taskctl.py validate`、文档链接/状态核对 | 旧任务状态、证据、依赖和 canonical 身份边界不被覆盖。 |
| Go domain/API/授权 | `go test ./... -count=1`（设置本地测试 DB）、`go vet ./...`、`go build ./...` | 401/403/404 与授权；旧 Organization/Community Activity、Agent ResultSet、报名/计划仍走真实 DB。 |
| DB 迁移/seed | disposable DB 前向、当次迁移 down/reapply、SQL invariants、`automation/verify_community_migrations.ps1`；新迁移须扩展脚本 | 旧 Activity ID/数量、三类 organizer 唯一性、Community owner、组织权限、私密结果过滤和 seed 幂等。旧 001–020 无统一 down，不能声称全库可逆。 |
| Flutter model/UI | `flutter analyze`、`flutter test`、受影响真机流程 | 中文主要界面、加载/空/错/权限、主办方显示与跳转、真实 API 状态。 |
| 地图/Now | Flutter 地图/键盘/Agent 相关测试和真机手势 | 地图实例保持、Pin/选择不随输入或拖图清除、显式“搜索此区域”、旧响应不覆盖新结果。 |
| 阶段/Gate | Debug APK build、Go test/vet/build、迁移、seed、`verify_vertical_slice.ps1`、相应真机路径 | 本地闭环 PASS 才能继续；正式 release/Closed Pilot 另需非本机 HTTPS/IdP/真实活动证据。 |

## 可复现命令

在 `apps/api`：设置 `BIRDTIE_DATABASE_URL=postgres://birdtie:birdtie_local_only@127.0.0.1:55432/birdtie?sslmode=disable` 后执行 `go test ./... -count=1`、`go vet ./...`、`go build ./...`。在 `apps/client`：`flutter analyze`、`flutter test`。本地 Debug APK：使用被忽略的 `.env.maps.mobile.local.json` 提供既有地图令牌，`BIRDTIE_ENVIRONMENT=development` 和明确本地 API；令牌值不写入报告。仓库根目录：`automation/verify_community_migrations.ps1`；PowerShell 7 执行 `pwsh -NoProfile -File automation/verify_vertical_slice.ps1 -ApiBase http://127.0.0.1:3696`（脚本只允许 loopback，创建后取消合成活动）。

用 `C:\Users\chens\.cache\codex-runtimes\codex-primary-runtime\dependencies\python\python.exe automation/taskctl.py validate` 检查队列。Windows PowerShell 5.1 不支持 vertical slice 脚本使用的 `-SkipHttpErrorCheck`；需要 `pwsh` 7。地图 Debug 构建不构成正式 Mapbox 授权或 release 包验收。

## 本次实际结果

| 检查 | 结果 |
| --- | --- |
| `flutter analyze` | PASS，0 issues（64.3s）。 |
| `flutter test` | PASS，87 tests。 |
| `flutter build apk --debug`（本地地图配置、开发 API） | PASS；`app-debug.apk` 241,659,966 bytes。构建时 Mapbox Flutter 插件给出未来 Kotlin Gradle Plugin 迁移警告，当前构建成功。 |
| `go test ./... -count=1`（本地 PostgreSQL） | PASS；含 postgres/httpapi 集成测试。 |
| `go vet ./...`、`go build ./...` | PASS。 |
| disposable DB 001–032、030–032 down/reapply、seed 两次 | PASS；旧 Activity `5→5`，5 个 organizer。 |
| Organization→Activity→Discovery/Agent→RSVP→Plans/Save 合成 E2E | PASS；脚本取消合成活动。 |
| Android Community 十项场景 | [10/10 本地真机 PASS](../testing/COMMUNITY-SOCIAL-PHYSICAL-DEVICE-2026-10-01.md)。 |

本表是 V4 Foundation 的代码回归证据。未来任务必须记录执行时间、命令、结果和受影响行为；无真实生产条件时不把任何本地结果写为 Closed Pilot Ready。
