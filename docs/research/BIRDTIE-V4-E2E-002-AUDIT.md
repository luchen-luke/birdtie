# BT-V4-E2E-002 本地端到端验收审计

2026-10-05。任务原 AC 保留：Intent → 真实候选 → Place → Activity → Join/Create（适用）→ Plans，并重启验证。本轮只实施隔离本地原生链，现实授权供给、真人操作及 App 真机重启尚未完成；完整任务不得标 DONE，Social Alpha / Closed Pilot / Consumer Beta 不因此放行。

## 当前证据与计划

- OPP002、PLC003、PLN001 已在唯一队列 DONE。既有原生人类机会、地点匹配、具体版本动作 JOIN、持久 RSVP/Plans 可复用，无新权限、模型调用或第二业务台账。
- 源码执行基线仅 `work/v4-pln001-20261005/conversion-native9/source` 的085冻结帧，叠加本项唯一新 Go 测试；不把 OBS 的086未完成 live 改动称已验证。
- 实际使用 registered HTTP：本人 FIND_ACTIVITY 草稿 → 具体预览批准 ACTIVATE → Opportunity → 原 Place/已审核 Venue/Activity 详情一致 ID → 具体当前 JOIN → 原 RSVP/Plans → 两个独立 OS API 进程重启读取相同稳定 ID。
- City、公开 Place、编辑者与审核者角色为 LOCAL_SYNTHETIC 初始化；Venue 提交/审核、Activity 发布、Intent 激活和 JOIN 使用真实领域 API。测试数据不代表现实运营授权。
- 匿名/跨本人/组织、隐藏源、City 到期、双向 Block/旧动作版本拒绝与零副作用；Get/重启仅可正常刷新原 Session idle，不应改变领域数据或身份/Tie。
- 独占隔离库迁移001–085、原 seed 与旧资料、完整 public/catalog/xmin/up/down/reapply、精确清理/DROP；首次失败逐轮保留。UX-CHECK-01/02/03/05/06/07/09/10/11/12/13/14/15/16 适用，其中 UI/辅助技术/真机本轮 NOT_RUN，不能用 HTTP 替代。

## 后续实际缺口

ACTN002 所需权威 attendance/completion writer 当前不存在；going、公开报名、活动结束和到访自声明不等于到场。ACTN003 可复用048双方开关、原关系/参与/社群真源派生当前可见共同历史，但当前老 shared-context 不含 Place 历史且缺编码后完整身份/源复核，旧 Panel 存在 transport 重绑定缺口。它们是原任务的后续需求，不在本项重复导入或抢占实现。

## 验证结果

### 实际命令与原始帧

执行命令（原测试默认并发不改为单线程）：

```powershell
python work/v4-e2e002-intent-plans/verify-native.py --round native1 --pattern '^TestV4IntentOpportunityPlacePlans' --schema 85
python work/v4-e2e002-intent-plans/verify-native.py --round native2 --pattern '^TestV4IntentOpportunityPlacePlans' --schema 85
python work/v4-e2e002-intent-plans/verify-native.py --round native3 --pattern '^TestV4IntentOpportunityPlacePlans' --schema 85
```

`python` 的实际二进制为 `C:/Users/chens/.cache/codex-runtimes/codex-primary-runtime/dependencies/python/python.exe`。runner 原样保存 Go/CLI 命令、PID、stdout/stderr、数据库 ownership、result 与执行源码。

| 帧 | test 结果 | vet/build/2 CLI | 解释 |
| --- | --- | --- | --- |
| native1 | 5 PASS / 4 FAIL / 0 SKIP，exit1 | 全0 | 3个负例叶子及父用例失败：测试原以409期待双向Block/City到期，原writer正确返回404以隐藏失效或未授权目标；生产源码未改。正向链及两次真实进程重启已通过。 |
| native2 | 9 PASS / 0 FAIL-SKIP，exit0 | 全0 | 分别精确断言Block/City404、可见目标source变化409，保留完整零写断言；保存子进程18次实际GET输出。 |
| native3 | 11 PASS / 0 FAIL-SKIP，exit0 | 全0 | 新增实际主办方改为invite_only后无邀请的404，以及原logout后旧JOIN401；最终冻结源码。 |

native3 的11事件分为9个实际功能叶子（正链、7个当前源负例、身份/工作区边界）、1个负例组父事件、1个无child环境的启动门禁用例。child实际分支由正链启动两个 OS 进程真正运行，不以该空分支 PASS 替代重启。

最终两个 OS PID 为36952、19248，各通过原registered HTTP读取9个路径；总18次GET，包括本人原Intent、Opportunity、Place匹配/详情/Venue、Activity、原Participation、原Plan及空Tie。父用例115个registered请求含每个独立fixture的领域准备/审核/发布与负例；它不是115个现实用户操作。`X-Request-ID` 为本测试生成的脱敏客户端关联标记，不冒充086服务器trace。具体日志提取在 `work/v4-e2e002-intent-plans/actual-http-restarts.json`。

所有原fixture域操作都是隔离本地合成领域操作。普通JOIN仍不改变Intent ACTIVE为CONVERTED，本项未假称已执行085转换；085既有域保持其原合同。

### 数据与源码核验

- 最终唯一 Go SHA：`e39ba99d3138a54ad88f3971abf2dec4e13c4ac434aeb6c70b0d421092d47475`，冻结文件 `work/v4-e2e002-intent-plans/source-freeze1.json`。
- native3 的881输入全部逐SHA：原PLN native9的880原字节（包含3个原seed）加新测试。878个Go/SQL/mod输入前后相同；此结论只针对明确复制帧，不代表移动的OBS live源。
- fresh001–085、保留旧资料、085 up/down/reapply原public全列投影/原Participation xmin、7项可见semantic catalog均核同；完整原public/catalog在所有新fixture精确清理后保持。
- 新fixture清理只删除自己的活动/报名/计划/通知/审核Venue/Place/编辑者及Block，再由原helper清自己的City/身份；owned子库原所有public行（含sessions）与创建fixture前相等。正常HTTP idle刷新仅在请求间域无写比较中排除sessions，身份摘要仍比较Session ID/owner/digest/authMethod/createdAt/absoluteExpiry/revokedAt，不以这个例外排除域表。
- 父owned库 `birdtie_notify038_b7b489ff8e5d` 已实际 `DROP DATABASE ... WITH (FORCE)` exit0。psql-q清理stdout为空，不能将空输出单独称存在性复核；实际runner返回码和ownership/result已保存。

### 完成分类与恢复条件

**本地原生端到端切片通过；完整 BT-V4-E2E-002 建议 PARTIAL。** 原real-world AC需要实际获授权Place/Venue/Activity、真人在Birdtie原入口选择候选/检查和JOIN/计划，并保存 App/API 重启证据。当前没有现实供给、真人到场或运营证据，不将合成审核者身份当商业审核授权。根代理后续需以与OBS完成后的实际源帧联合回归，未运行整仓Go不称全仓绿。

本轮无Flutter产品改动，Flutter analyze/test/build未重复执行；真机/UI截图/App重启/TalkBack、race、真实IdP/HTTPS/地图配置/部署/运营/现实参与者均 NOT_RUN。规则引擎结果不冒称已配置大模型。Closed Pilot / Consumer Beta 均 **NO**。
