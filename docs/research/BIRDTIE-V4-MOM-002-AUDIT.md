# BT-V4-MOM-002 差距与实施审计

日期：2026-10-03。以官方仓库实际源码及 live task acceptance 为准，不改 queue/common reports。

## 输入材料与去重

- `automation/codex_task_queue.json`：Aggregate Place Memory，依赖 MOM001/PLC002 已 DONE；验收为地点能展示隐私安全近期 Moment/活动安排与适合度来源。
- `work/v5-age051/next-community-worker-audit.md` 与当前 26 精确 lease：复用本人原生 Moment、PublicPlace/Activity、067 七字段审核，没有新增领域实体或 DDL。
- `docs/architecture/MOMENT-CONTEXT-LINKS-V4.md`、`HISTORICAL-MOMENT-TIME-V5.md`、`AGENT-PLACE-MEMORY-V5.md`：私人关联和自述历史不能当公开社交聚合/访问证明。027 私人投影不是公共来源。
- `docs/ux/GLOBAL-UX-INTERACTION-CONTRACT.md`、V4/V5 执行协议：中文、原意图/详情直接路径、具体批准与权威结果、迟到/撤权、真实 evidence。

## 基线分类

| 能力 | 实际基线 | 本轮 |
| --- | --- | --- |
| 本人私人 Moment 原生 CRUD/撤回、历史精度 | REAL，本人同 Tx 当前 Session，普通人可无 Agent | 复用 |
| Moment 明确公开 writer/具体版本确认 | NOT_IMPLEMENTED；旧 schema 支持 public/published，但不能把 raw SQL fixture 称功能 | 新 native preview/CAS/public确认/audit，复用原撤回 |
| 地点近期公开 Moment 摘要 | NOT_IMPLEMENTED | 30 天 published_at，最多 5 节选、隐私安全计数 |
| 原公开 Activity/typed organizer | REAL，当前源码仍是安排而非出席 | 复用 current visibility、取消、主体与 Business source，分组安排 |
| 地点七字段来源/审核/置信度 | REAL，PLC002 067 | 最后 SQL 精确源帧复用，不复制影子资料 |
| 私人 Place Memory | PARTIAL，不能证明 visited/attendance | 不用于公开聚合 |
| 机器目的、模型输入、自动写、访问轨迹 | 无合法运行许可 | 继续 Unavailable，不造授权 |

## 实施及验证过程

API 公共冻结期间仅 work snapshot 与独占 Client 实施。staging compile2 实际编译 0，无功能 test。staging pure1 首次旧 PublishSocialActivity 参数误用编译失败，pure2 修正确切签名后编译/纯约束通过。staging-native1/2/3 的失败分别包括 session idle 形状、清理 FK、原生 editor 状态、Moment public confirmation 负例 fixture、初 Place ID 缺失；这些不称权限漏洞 RED。

staging-native4：fresh001–068、本地合成且自有 DB，真实 75 Test PASS/0FAIL/0SKIP、vet/build0、非空全部 public 完整旧行及 snapshot 源稳定、DBDROP。实际正例使用人类 Create/Preview/Publish，再 public summary/withdraw；067 经真实 Submit/Review；活动经真实 SocialDraft/Publish 后跨实际 endsAt。后续新增最终写后期限与审核来源等待用例正在复验。

staging-native5 新增来源期限用例最初错误地直接修改不可变已审核候选，产生 77 PASS/3 FAIL 事件；改为实际 Submit/Review 短期来源后自然 PG 到期。它是 fixture 构造修复，不冒称授权漏洞 RED。

Client unknown result 专项实际 RED：503 后原 load 再读 draft-only preview，未读权威状态；修复后 GREEN，先本人原 GET 确认 published/withdrawn/privateDraft。另重复审核声明/空资料 RED 与账号切换时同步通知导致 Flutter build 异常 RED 均保存；现拒绝异常资料、即时清除旧批准并延后安全通知，补充回归通过。Client 初 analyze braces 与 fixtures/scroll pump 失败均原样保留，不作为产品授权缺陷证据。

production1 使用实际官方 Go 源与根代理注册的三个路径，fresh001–068 两轮各 80 Test PASS、0 FAIL/测试 SKIP、目标 vet/build0；完整旧非空 public 行保持、13 owned 与本轮所有 API SHA 稳定，自有库 DROP。最终五个目标 Flutter 文件 20 PASS；获分配十个 Dart 文件 analyze0。全客户端 analyze7 因并行 inbox 两处 lint exit1，明确留给相应 owner 处理，不越权修改。

## 本轮边界

不修改旧 native Moment/人类入口、server、live queue、共用总报告或 DDL；根已负责三新路由注册并进行独立验收。源范围功能已实际验证；完整默认 Go/最终全客户端 Flutter Build/真机/进程重启分别仍待根记录，不把 work 草稿、合成来源与 fake HTTP suite 称上线或 CSSA 试点。正式证据在 `docs/testing/evidence/place-social-history-2026-10-03`。Published 认知事件、私人消费权限、到访/出席事实与模型推理均不在本轮完成范围。

## 实际工作区入口复核与修复

根代理初 185 Client/94247 APK 帧验收期间发现真实 MapWorkspace `_openPlaceDetails` 仅转发个人 bearer，未转发当前组织。本人 HTTP 权限负例不能替代这个真实 UI 入口。任务因此继续，根通过受控 lease 将 map_workspace.dart 从 NOT001 转给本任务（转移前 SHA ACBD6A67496206D5595A7010AF4E8CCA5F998CA1394A9C4EF6BC0AE23FC37DCC）；保留 Inbox 实现。唯一 live 状态由根处理。

实际 Map transport/widget 负控 `workspace-map-boundary-red2.log` 仅撤掉 getter/notifier 接线，组织后 `/me/moments` 实际再次读取（expected1/actual2），随后精确恢复原字节。red1 的 Python CRLF 匹配断言失败没有完成 mutation，后续测试绿，准确分类为 runner 失败而非权限 RED。原主体改变兼容测试如今要求当前重新读取，Mock 对新 token 返回真实不同空结果，仍断言旧私人正文消失。

workspace-target2 两个 scroll/返回按钮 fixture 失败、target3 既有800×600 Sidebar overflow50px、workspace-final2 四条新增测试 lint、final3 未改中的一条 lint均原样保留。首次从仓库根执行 analyze 扫描 work snapshot 的34660问题属于命令cwd错误，不算产品分析失败。最终从 apps/client 运行 workspace-final4：11文件 analyze0、五目标两轮各28功能PASS/0FAIL-SKIP（另5loading）、11owned与167 lib/test源稳定、旧冻结13Go SHA均相同。同Actor清旧权威结果显示另有真实RED→GREEN。

工作区 Person/Org和观测epoch分离，组织仅原公开资料/公开历史投影，无私人列表或发布批准；回个人重新读取。当前接口不授模型、Memory、认知目的或动作权限。新完整Flutter/新APK/组织真机结果与App/API重启需要根的单独证据，不能沿用旧个人帧。

## 当前工作区真机补证

根新完整188Client实际analyze/test/Debug0，316具名PASS和69loading分别记录；worker只读原文并实际手机操作安装APK7d2a7ce…，以pm path/sha256sum确认手机base.apk字节相同。UI本地测试登录、新建明确LOCAL_QA私人Moment2edfb2af…rev1、实际Place详情预览取消零领域写、UI创建本地QA组织07755322…与真实owner成员，实际组织入口隐藏个人来源，回个人新读后具体rev1勾选公开，native为rev2/public且公开摘要count1。原DELETE UI具体rev2勾选撤回，native为rev3/withdrawn/private，摘要count0。旧910107…rev5整行不变，audit10/11/12与匿名读取请求ID保存，写请求ID不伪造。

Android1.6大字真实滚动可达撤回操作并实恢复1.0；App/API仍运行交还根代理。Restart/TalkBack/外接键盘/横屏未跑，先前自动审核拒绝compound停止/重启后没有重试。采集脚本DTO/envelope/path/JSONL分类首次假设失败全保存，不作为功能或授权RED。13Go+11Dart源码保持冻结；新243文件不可变device-workspace3-final3清单与证据见本任务DEVICE-VALIDATION.md。完整当前Go回归和live状态继续由根核证；Closed Pilot/Consumer Beta保持NO。
