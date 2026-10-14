# Birdtie 开发验证与构建效率审计（2026-10-09）

本次只读核查，没有启动测试、编译、依赖安装、迁移或手机操作，没有修改产品代码或任务队列。以下时间均换算为上海时间；目录日期不能代替回执时间。

## 明确结论

1. 用户要求的“每小项只做相关单元测试，加受影响流程验证”已经于 10 月 6 日写进主仓库规则。10 月 9 日存在按文件和 Go Test 前缀筛选的执行证据，不能说这一要求从未落实。
2. 全量后端测试确实很重。10 月 7 日整合检查点两轮分别耗时 **33.1 分钟、37.6 分钟**，主要耗在 PostgreSQL 集成包。这两轮位于明确的整合检查点，不能据此判定“每个小项都违规跑全量”。
3. 最新材料中，10 月 9 日有 **19 次 APK 构建尝试，累计命令时间 7.0 分钟**，以及 **60 段录屏，视频总时长 100.3 分钟**。重复打包、安装、录屏、导出、抽帧、审阅、冻结证据和调整 API 版本的往返，能解释显著交付成本；单次增量编译通常只有十几秒，不能把全部慢归因于编译器。
4. 一个真实 Gradle 缓存失败及关闭 Kotlin 增量编译的配置可证实；网络下载、磁盘损坏、杀毒软件、持续锁等待或持续内存不足导致缓慢，现有材料不能证实。
5. 11 个 Birdtie API 实例同时保留并监听不同端口，使手机绑定和版本核验复杂。其快照工作集约 482 MiB、启动以来 CPU 合计约 35 秒，不能据此断言它们是当前 CPU 瓶颈。

## 工作副本与规则

| 项目 | 只读结果 | 证据 |
|---|---|---|
| 主仓库 | `codex/birdtie-import`，HEAD `4637cfb`，大量 10 月 9 日未提交改动 | `git status --short`、`git worktree list --porcelain` |
| c535 工作树 | `main`，HEAD `8ff7ae5`，少量未提交旧 UI 改动 | `C:\Users\chens\.codex\worktrees\c535\birdtie` |
| 3e34 工作树 | detached HEAD `8ff7ae5`，干净 | `C:\Users\chens\.codex\worktrees\3e34\birdtie` |
| 逐项验证要求 | 只执行本需求直接相关单元/widget；不重复全 Flutter/Go、全 analyze/vet/build 或数据库整检 | [AGENTS.md](D:/Project/birdtie/AGENTS.md:44)、[V5 协议](D:/Project/birdtie/automation/CODEX_V5_EXECUTION_PROTOCOL.md:24) |
| 工作树规则差异 | 两工作树的 AGENTS 只有早期 20 行版，缺少 10 月 6 日新增验证频率规则 | 各工作树 `AGENTS.md`，只读比对 |

旧模板中的 `go test ./...`、全量 analyze 或迁移步骤，是历史流程和必要整合检查的安排。发现模板不等于发现实际违规执行。新的用户指示优先；本报告没有改写这些历史文件。

## 已执行验证：哪些慢，哪些已缩小范围

### 10 月 7 日整合检查点

读取 `D:\Project\birdtie\work\next-integration-checkpoint-2026-10-07\<stage>\command.json` 的 `startedUTC` 和 `result.json` 的 `finishedUTC` 相减。

| Stage | 真实命令范围 | 命令秒数 | 结果 |
|---|---|---:|---|
| go-test-api01 | `go test -json -count=1 -timeout=60m ./...` | 1983.463 | exit 0，12905 PASS 事件 |
| go-test-api02 | 同上 | 2256.802 | exit 0，12944 PASS 事件 |
| flutter-test-client02 | `flutter test --no-pub --reporter json`，没有文件筛选 | 91.911 | exit 1，保留 29 error 事件 |
| flutter-test-client03 | 同上 | 84.898 | exit 0 |
| flutter-test-client04 | 同上 | 85.014 | exit 0 |
| flutter-test-client05 | 同上 | 107.065 | exit 1，保留 5 error 事件 |
| flutter-test-client06 | 同上 | 101.215 | exit 0 |

Go 两轮合计 70.7 分钟，Flutter 五轮合计 7.8 分钟。PASS 数含测试父子事件，不是新增功能数。

原始 `raw.log` 的 package PASS 事件：`internal/postgres` 分别 **1968.712 / 2247.360 秒**，`internal/httpapi` 分别 **433.304 / 495.788 秒**。各包可能并行，不能把包时间相加当作命令墙钟。Postgres 包几乎覆盖全量命令持续时间，说明主要成本是数据库集成测试，并非单独 Go 编译。许多测试刻意等待撤权/期限/锁等场景，例如 [agent_candidate_pipeline_integration_test.go](D:/Project/birdtie/apps/api/internal/postgres/agent_candidate_pipeline_integration_test.go:559)；本次没有进一步给每种等待做性能归因。

可复核证据：[api01 命令](D:/Project/birdtie/work/next-integration-checkpoint-2026-10-07/go-test-api01/command.json:2)、[api01 结果](D:/Project/birdtie/work/next-integration-checkpoint-2026-10-07/go-test-api01/result.json:2)、[api02 命令](D:/Project/birdtie/work/next-integration-checkpoint-2026-10-07/go-test-api02/command.json:2)、[api02 结果](D:/Project/birdtie/work/next-integration-checkpoint-2026-10-07/go-test-api02/result.json:2)。运行器 [run.py](D:/Project/birdtie/work/next-integration-checkpoint-2026-10-07/run.py:88) 明确是整合检查点。

### 10 月 9 日定向单元执行已经存在

- [root-client-unit-final-02/receipt.json](D:/Project/birdtie/work/now-2026-10-09/root-client-unit-final-02/receipt.json:3)：指定六个相关 Flutter test 文件，`--no-pub`，exit 0。
- [root-http-unit-final-02/receipt.json](D:/Project/birdtie/work/now-2026-10-09/root-http-unit-final-02/receipt.json:3)：只执行 `./internal/httpapi`，`-run '^Test(OpenPlaceAcquisition|PlaceHoursUnavailableStopsAtExplicitNativeFailure)'`，exit 0。
- [reply-source-refresh-01/SUMMARY.md](D:/Project/birdtie/work/now-2026-10-09/reply-source-refresh-01/SUMMARY.md:6)：仅两个 Flutter 文件，53 PASS，exit 0。

这说明近期部分修复已经缩小了测试范围。剩余效率问题需要控制打包、真机与证据迭代频率，不能靠删掉测试解决。

## APK 构建量化

来源：`work\now-2026-10-08` 和 `work\now-2026-10-09` 下全部 `build*-client-*\receipt.json` 与同目录 `build.log`。19 个回执实际都在上海 **10 月 9 日 01:09–14:08**，不是分属两个自然日。

| 构建 | Gradle 秒数 | 命令墙钟秒数 | Exit |
|---|---:|---:|---:|
| 15-01 | 22.3 | 24.97 | 1 |
| 15-02 | 84.5 | 87.66 | 0 |
| 16-01 | 17.1 | 21.57 | 0 |
| 17-01 | 17.9 | 22.16 | 0 |
| 18-01 | 17.4 | 21.55 | 0 |
| 19-01 | 23.0 | 27.26 | 0 |
| 20-01 | 13.3 | 17.48 | 0 |
| 21-01 | 19.1 | 22.17 | 0 |
| 22-01 | 10.3 | 13.53 | 0 |
| 23-01 | 12.0 | 15.12 | 0 |
| 24-01 | 9.6 | 12.77 | 0 |
| 25-01 | 9.8 | 12.70 | 0 |
| 26-01 | 9.5 | 12.43 | 0 |
| 27-01 | 12.8 | 15.81 | 0 |
| 28-01 | 9.8 | 12.80 | 0 |
| 30-01 | 9.8 | 12.79 | 0 |
| 31-01 | 9.6 | 12.76 | 0 |
| 32-01 | 9.5 | 12.51 | 0 |
| 33-01 | 41.3 | 44.25 | 0 |
| 合计 | **358.6** | **422.29** | 18 成功、1 失败 |

计时口径：Gradle 是日志 `Running Gradle task 'assembleDebug'...` 行末时间；墙钟取回执 `completedAtUTC - startedAtUTC`。不含之前的源快照、之后的 APK 复制/哈希、安装、手机操作、录屏导出/审查。因此 7.0 分钟只表示这些构建命令累计时间，不表示整个交付只耗 7 分钟。

例证：[build15 初次失败](D:/Project/birdtie/work/now-2026-10-08/build15-client-01/build.log:299)、[build15 修后](D:/Project/birdtie/work/now-2026-10-08/build15-client-02/build.log:12)、[build26](D:/Project/birdtie/work/now-2026-10-09/build26-client-01/build.log:12)、[build33](D:/Project/birdtie/work/now-2026-10-09/build33-client-01/receipt.json:2)。未发现 build29-client 回执，不补造这一轮。

构建脚本 [build17_client.py](D:/Project/birdtie/work/now-2026-10-09/build17_client.py:22) 每次调用 `flutter build apk --debug`；它还冻结全部 lib/assets/Android 相关文件并复制 APK。这种按每个窄修复重新打包及保存大批证据的做法，增加固定往返成本。材料不足以判断每一次是否均有必要，也没有把这些 APK 命令误称为全测试套件。

## 录屏与运行版本成本

同一组两个原目录下共 **60 个 `*.stop.json`**，其 `metadata.format.duration` 合计 **6018.432394 秒，即 100.3 分钟**。60 个视频路径、60 个 SHA256 均唯一；停止回执全部属于上海 10 月 9 日，最早 01:25:47，最晚 11:05:52。这里统计的是材料中已录视频的总长度，不能直接视为 100.3 分钟净开发延误，也不能判定所有视频多余；重复走过相似流程的视频仍是不同原片，并非按用户旅程去重。

视频例证：[183.449 秒片段](D:/Project/birdtie/work/now-2026-10-09/device31-apk26-01/api31-apk26-real-agent-02.stop.json:22)、[180.193 秒片段](D:/Project/birdtie/work/now-2026-10-09/device27-apk20-01/api27-apk20-chinese-address-03.stop.json:22)。[video-audit-01/SUMMARY.md](D:/Project/birdtie/work/now-2026-10-09/video-audit-01/SUMMARY.md:16) 还记录录屏截止与关键操作错过录制窗口，导致再次取证的实例。UI/地图/中文输入中也有真实失败，不能为提速抹去这些失败。

进程快照核查到 API16、API20、API26–34 共 11 个实例，分别监听 18091、18095、18101–18109。工作集合计 505872384 bytes，私有内存 635011072 bytes；启动以来 CPU 合计 35.375 秒。它们是版本和端口管理负担，现有快照不支持“旧 API 把 CPU 拖满”。[build_start21.py](D:/Project/birdtie/work/now-2026-10-09/build_start21.py:40) 明确保留旧 API16；[device17.py](D:/Project/birdtie/work/now-2026-10-09/device17.py:50) 为每一版切换 ADB reverse，证明证据和运行版本需要额外绑定核验。

## 构建环境问题：已证实与未证实

| 项目 | 判断 | 证据与边界 |
|---|---|---|
| Gradle 不可变 transform 缓存被改动 | 已发生一次真实失败 | [build15-client-01/build.log:9](D:/Project/birdtie/work/now-2026-10-08/build15-client-01/build.log:9)，有 17 个相关缓存目录隔离记录 |
| 缓存修复后变慢 | 局部可观察 | 初次失败 22.3 秒，修后构建 84.5 秒；[receipt-resume.json](D:/Project/birdtie/work/now-2026-10-08/gradle15-quarantine-01/receipt-resume.json:3) 记录原目录17、删除0、可恢复、idle daemon已停止；不能确定具体修复各步贡献 |
| Kotlin 增量编译关闭 | 明确配置 | [gradle.properties:9](D:/Project/birdtie/apps/client/android/gradle.properties:9)，备注指出曾有 Mapbox Windows 增量缓存关闭失败；会失去该部分增量收益，不表示全部 Gradle 缓存关闭 |
| 内存配置 | 明确配置，未证实瓶颈 | [gradle.properties:1](D:/Project/birdtie/apps/client/android/gradle.properties:1) 最大堆8G、Metaspace4G。审计快照机器约32GB内存、空闲约6.5GB，java工作集约2.5GB；没有 OOM 证据 |
| 多套 Gradle home | 存在历史目录与模板 | `.gradle-user-home`、其 `repair`、`work/gradle-community-cache`、用户默认 `.gradle`；10月7日模板使用 `C:/bt-g07`。不能仅凭目录存在断言10月9日每次都换缓存 |
| 网络下载缓慢 | 未证实 | 日志的镜像提示不是下载时长；没有足够下载计时/重试证据 |
| 外部进程/磁盘/杀毒修改缓存 | 未证实 | Gradle 错误文本列出可能原因，不能当作实际原因 |
| 持续构建锁等待 | 未证实 | 没有已读日志证明持续 lock 等待；本次未进行新 profiling |

## 以后每个小项执行的具体政策

- 先明确本次改动影响的函数、controller、widget 和入口，只跑这些测试以及必要的邻接测试。无需每项全量 Flutter/Go、全量 analyze/vet/build、迁移整检。
- 失败先定向复现并修复；修后重跑原失败和直接受影响测试。通过后没有新修改、失败或风险依据，不重复同一组检查。
- 每项记录目录、命令、退出码、覆盖范围及未运行范围。单元通过表示该覆盖范围通过；完整流程、UI 验收、真机或发布未运行就写 NOT_RUN。
- UI 窄改用必要 widget 测试和一次受影响流程查看；批次统一做关键截图核验。完整长录屏集中到需要正式验收的功能检查点，避免每个字号/间距/窄修复都重新导出、抽帧和写大型审计材料。
- 同一调试设备保持一个 Flutter 调试会话，普通 Dart/UI 改动使用现有热重载；原生代码、依赖、平台配置或编译期定义改变时再重启/构建。需要原生地图/键盘的流程保留该范围真机核验，手机离线则记录未测并继续独立工作。
- 一批逻辑闭合的修改统一打一个验收包。运行 API 仅保留当前需要的版本和确实仍在核验的基线，完成核验后由负责人明确结束旧实例；原二进制和失败证据仍可保存。本审计没有结束任何现有进程。
- 全量检查保留在跨模块整合/发布等明确检查点，先列出其目的和范围，避免把历史 `verify` 模板当作每项强制步骤。新的登录权限、持久化/迁移或真实地图行为涉及的专项验证按实际影响单独安排，不以纯单元测试替代应有验收。

以下是可直接采用的现有命令示例，本次未执行：

```powershell
# 工作目录 D:\Project\birdtie\apps\client
# Now 外壳/排版的两组相关 widget 单元测试
flutter test --no-pub test/now_material_workspace_test.dart test/map_workspace_shell_test.dart

# 回复来源刷新，只跑两组相关测试
flutter test --no-pub test/reply_source_refresh_test.dart test/reply_projection_refresh_test.dart
```

```powershell
# 工作目录 D:\Project\birdtie\apps\api
# 地点追问/空结果精炼的领域单元测试
go test ./internal/agentworkspace -run '^Test(LocalPlace|EmptyPlaceRefinement)' -count=1

# 当前地点获取/营业时间失败的 HTTP 边界单元测试
go test ./internal/httpapi -run '^Test(OpenPlaceAcquisition|PlaceHoursUnavailableStopsAtExplicitNativeFailure)' -count=1
```

这些命令对应现存测试；应按实际下一项进一步缩小或补充，而不是把四条全部固定套在每项上。`--no-pub` 适用于依赖未变且已解析的日常改动；`pubspec` 或锁文件确实变动时再解析相关依赖。Go `-count=1` 在当前所选范围内真实执行，不扩大成 `./...`。

## 审计边界

本报告读了规则、Git/工作树状态、配置、已有整合/单元回执、构建日志、录屏元数据和一个进程快照。没有对整个历史聊天逐次命令重放，没有测实时网络或磁盘，也没有独立复跑测试。不能断言全部历史小项的验证均过量或全部构建皆可省略。**可以确定的是：全量数据库测试成本高，近期增量编译通常很短，而频繁真机与证据迭代产生了大量额外往返；把日常验证控制在直接受影响范围，是当前应执行的用户既有要求。**
