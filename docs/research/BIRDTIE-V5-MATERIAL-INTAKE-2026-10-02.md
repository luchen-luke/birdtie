# Birdtie V5 材料接入记录（2026-10-02）

## 1 范围与当前执行边界

本记录补齐 AGE、AIR、持续交互规则和消费级评估的材料来源、接入位置及验收映射，依据 [137 项逐条对账矩阵](BIRDTIE-V5-MATERIAL-RECONCILIATION-2026-10-02.md) 与 [机器矩阵](../../work/v5-material-reconciliation.json)。本轮没有导入任务、修改功能、运行产品测试或改变发布状态。

本轮读取实时队列时为 **144 项：DONE 96、PARTIAL 3、TODO 37、BLOCKED 7、IN_PROGRESS 1**，当前项为 **BT-V4-SAF-004**。该项实现、回归、真机与终态证据由原执行者收尾，材料接入不抢占、不重新领取、不提前标记完成。这个数量是读取截点；后续原执行者按证据更新状态时，以 `automation/codex_task_queue.json` 为准。

接入前队列 SHA256 为 `8C24BD63BD2021F6526BF254C1DD6210ECE03BBEDF6715D44EA5102C68886659`。本轮仅写本文、UX 索引、canonical §12 的历史范围短注和独立 resolved 记录；保留已有 ID、状态、依赖、证据和未提交内容。

## 2 四份材料的实际路径和 SHA256

以下均由当前仓库实际文件重新计算 SHA256，并与逐条矩阵核对一致；文件取得不等于功能实现或验收通过。

| 材料 | 实际仓库路径 | SHA256 | 接入用途 |
| --- | --- | --- | --- |
| AGE 原文 | `docs/product/BT-V5-AGE-AGENT-ENRICHMENT-EPIC.md` | `1ACD60FEF5642633C7E91B10909931989FFB59FF413BF8F242E5AABA00368218` | 81 项源需求，Profile/Memory/Evidence/Policy/Context 归属与后续接口依赖 |
| AIR 包 | `BirdTie-BT-V5-AIR-Codex-Package-2026-10-02.zip` | `E4251A7A4053CB39FE34E26C1A8D5D8A7F195DA910B0B8B474820B29B043BAD4` | 56 项源需求，受控推理、提案、运行、工具执行和恢复 |
| 持续交互规则来源快照 | `docs/product/BirdTie-Interaction-and-UIUX-Development-Rules-2026-10-02.md` | `20EC0D43442D4D95B9EEEA9C1E325572A09A7538307B66E8C79207039C8781CB` | 持续开发与逐功能质量规则，零功能任务导入 |
| 消费级评估原文 | `docs/product/BirdTie-Consumer-Readiness-Assessment-2026-10-02 (1).md` | `E90AF1A47058DB23C653C88CF325FBD0D9A57774185050EC7620D3BD202B044B` | 五条旅程、十五个领域及真实用户/发布证据参考，零功能任务导入 |

AGE 的原附件路径为 `C:\Users\chens\.codex\attachments\740182d5-1cae-4a04-b288-7f58711b226f\已粘贴的文本.txt`，收录目标为上述 `docs/product/` 原文，来源标记已经保存在文件头。其余材料以本表实际到达路径记录；未记录的外部原路径不作猜测，也未从 Civu 或 `D:\BirdTie` 搬移资料。

AIR 已解包的关键源文件用于逐条对账，仍是规划输入：

| 路径 | SHA256 |
| --- | --- |
| `work/v5-materials/BT-V5-AIR-DESIGN.md` | `839A0AF991B1701B61714EEB34C73C7143A8DCB8BDACDE30A574965C1273CC8C` |
| `work/v5-materials/BT-V5-AIR-REQUIREMENTS.md` | `3AD10255BB7056849215E04763F9D6800E4FC753A155FADBA1DD46134DC63406` |
| `work/v5-materials/BT-V5-AIR-BACKLOG.json` | `1A97E0E50E1E3DB2E2AF71EF91144D2B4EBE62487C3B9816536E2E3863B3E42F` |
| `work/v5-materials/BT-V5-AIR-CODEX-MASTER-PROMPT.md` | `1F657310B7789EC1727B1EF01B05D7D1BEE535B1EFF3A03EFAD9DB0CD2C83D4E` |

## 3 缺材料历史已解决，历史截点保留

[原接入截点](../../work/uiux-rule-integration-checkpoint.json) 当时按无后缀文件名 `BirdTie-Consumer-Readiness-Assessment-2026-10-02.md` 搜索，记录 `NOT_READ_MISSING_ORIGINAL`。这是当时的取得状态，不表示今天仍缺原文。

当前实际文件有 ` (1)` 后缀，原文已完整读取并核对上述哈希，当前状态为 **MATERIAL_AVAILABLE / RESOLVED**。新增 [消费材料 resolved 记录](../../work/v5-material-consumer-resolved.json) 指向真实文件，并保留原截点引用；没有覆盖、重写或删除历史 checkpoint。

消费原文及 UIUX 来源中“当时无法取得 AGE 完整编号/当前仓库”的表述同样属于来源当时的证据范围。现在 AGE 81 项和 AIR 56 项已取得并逐条对账，按现有矩阵的源编号引用，不编造编号，也不由原文历史表述推断当前实现。材料缺失的解除只解决材料读取，不能解除真实身份、试点供给或 AI 启用门禁。

## 4 源需求对账与实时任务分别计数

AGE 81 项与 AIR 56 项共 **137 项源需求**。逐条审计处置为 **REUSE 6 / EXTEND 81 / NEW 50**，描述每条源需求与既有能力的关系；不是实时队列的任务状态、产品完成率或 50 个已批准新任务。

逐条矩阵读取截点的限定实现评估为 REAL 3、PARTIAL 80、NOT_IMPLEMENTED 48、BLOCKED_EXTERNAL 6，也不是实时队列 DONE/PARTIAL 的替代值。REUSE 项仍可能需要增量回归或外部证据；EXTEND 不重置原任务 DONE；NEW 先经现有 ID 去重和依赖评审。本轮没有把 137 项与实时 144 项相加、没有导入副本、没有改变优先级。

消费评估和 UX-CHECK-01 至 16 是验收/评审定位，**不是任务 ID**。原文建议样本、指标与试点范围也不自动转为新 backlog、数值承诺、豁免或对外授权。

## 5 已接入的持续规则位置

| 位置 | 持续职责 | 本轮核验 |
| --- | --- | --- |
| [AGENTS.md](../../AGENTS.md) 的“Birdtie 持续交互与 UIUX 规则” | 每项用户可见开发先读唯一正文，中文优先、明确主体、当前版本确认、证据边界 | 已有短链接与持续约束，未重新建立第二正文 |
| [V4 执行协议](../../automation/CODEX_V4_EXECUTION_PROTOCOL.md) 的“UIUX 与 V5 增量接入” | 当前 atomic task 优先，安全检查点对账，连续领取，原 ID/状态/证据保留 | AGE/AIR 按真实接口依赖接续；消费评估只补核验与验收 |
| [Global UX Interaction Contract](../ux/GLOBAL-UX-INTERACTION-CONTRACT.md) | 唯一维护的跨产品交互与可访问性正文，含 UX-CHECK-01 至 16 | 保留地图/键盘/实体稳定 ID；§12 新注解释 AGE 缺编号的历史范围 |
| [UX 索引](../ux/README.md) | 发现 canonical、来源对账与消费验收参考 | 加入本文与对账矩阵链接，不复制规则正文 |

每项用户可见功能引用适用检查，并说明不适用范围；中文主要语言、Now 主入口和普通直接路径持续有效。草稿、用户对具体版本的批准、真正提交与权威结果分别呈现；账号/组织切换、撤权、旧确认和迟到响应不能复用旧批准。未来 AGE/AIR 接口依赖与源例子不视为已开放能力。

## 6 消费评估：五条旅程验收映射

以下附到现有任务及已取得的源需求，不创建任务。完整状态、代码/测试索引及证据边界见对账矩阵 §7；本表为验收参考，未声明本轮完成真实用户测试。

| 原章节 / 用户结果 | 现有任务或源需求锚点 | 最小应交证据 |
| --- | --- | --- |
| 3.1 新用户发现活动并真正参加 | `BT-V4-E2E-002`、`BT-V4-PIL-001/002/003`、`BT-RSV-001`、`BT-PLN-001` | 真实身份、授权供给、发现→详情→报名→重启后一致 Plans；变更、无供给及失败分支；报名与出席分离 |
| 3.2 组织者发布并处理临时变化 | `BT-V4-ORG-002`、`BT-ORG-004`、`BT-V4-E2E-003`、`BT-NTF-002`、`BT-V4-PIL-002` | 当前组织身份与预览、普通用户可见权威版本、改期/取消、通知失败补救及负责人；排队不等于送达 |
| 3.3 活动后安全联系和分享 | `BT-V4-TIE-001`、`BT-V4-CHT-003`、`BT-V4-SAF-002/003`、`BT-V4-ACTN-002/003`、`BT-V4-E2E-001` | 自愿请求与接受；分享后删源、转私密、退群、Block 的旧卡片/缓存/深链/Agent 实时权限；未知出席不得推断 |
| 3.4 拒绝定位/AI 仍完成普通协调 | `BT-V4-CTX-001`、`BT-V4-INT-002`、`BT-V4-NOW-005/006`、`BT-V4-E2E-004` | 无 GPS、无 AI、地图或模型失败时手动上下文、普通列表、详情、报名和 Plans；线上不要求地图点 |
| 3.5 主动启用 AI 记忆并撤回 | `BT-V4-SAF-004`；源 `AGE-004/011/063`、`BT-V5-AIR-043/044/050` | 分开分析、模型出口、记忆保存与公开同意；候选来源与审阅；纠正/删源/撤权和在途失效闭环；对应能力未实现/未放行保持关闭 |

## 7 消费评估：十五领域验收映射

| 原章节 / 领域 | 现有任务或源需求锚点 | 待补或应复用的验收证据 |
| --- | --- | --- |
| 4.1 承诺、信息架构、导航 | `BT-V4-NOW-001`、`BT-V4-ACTN-001` | 一句话页面职责、当前真实路径与截图、无提示完成观察；不凭本表重做导航 |
| 4.2 首次使用、登录、恢复 | `BT-AUT-002`、`BT-V4-PIL-001` | 生产 IdP/HTTPS、拟发布真机登录与过期恢复、切账号无旧私人内容、删除/恢复与深链按发行范围核验 |
| 4.3 真实供给、首次价值、空态 | `BT-V4-PIL-002`、`BT-V4-OPP-001` | 已确认活动事实/组织责任/有效时段；无供给、权限错误、网络失败分别表达；非合成首次价值 |
| 4.4 搜索、推荐、解释 | `BT-V4-OPP-003/004` | 授权规则与简短事实理由、真实查询可行动结果、修改条件路径；不披露私密关系或推断兴趣 |
| 4.5 详情与行动状态 | `BT-RSV-001`、`BT-V4-ACTN-001` | 真实决策信息、时区/资格/未知状态；服务端结果确认再显示成功，详情与 Plans 一致 |
| 4.6 组织工作流与运营 | `BT-V4-ORG-002`、`BT-ORG-004`、`BT-V4-E2E-003` | 普通组织者独立发布/改期/取消；成员撤权和身份隔离；人工边界与负责人 |
| 4.7 Plans、提醒、变更送达 | `BT-PLN-001`、`BT-NTF-002`、`BT-V4-NOT-001` | 持久事件→尝试→通道回执→用户可见分别观察；生产调度告警、失败补救和跨设备一致 |
| 4.8 出席、连接、聊天、分享 | `BT-V4-ACTN-002/003`、`BT-V4-CHT-003`、`BT-V4-E2E-001` | 出席独立来源/同意/纠错，自愿连接，离线恢复，分享读时 ACL；报名/照片/位置不冒充出席 |
| 4.9 隐私、同意、AI 身份 | `BT-V4-PRV-001`、`BT-V4-SOC-001`、`BT-V4-SAF-004` | 代表谁、用了什么、给谁、是否操作；分别同意与撤回；新朋友授权不等于模型/记忆授权 |
| 4.10 滥用预防、举报、支持 | `BT-V4-SAF-003`、`BT-V4-PIL-002` | 开放表面的真实举报/Block、跨路径阻止、值守和处理演练；有按钮不等于治理已运行 |
| 4.11 无 AI 降级与恢复 | `BT-V4-NOW-005`、`BT-V4-E2E-004`；源 `BT-V5-AIR-010` | 普通路径独立有效；空/错/取消/结果未知区分，输入保留与安全核实后重试 |
| 4.12 真机、可访问性、跨场景 | `BT-PER-001/002`、`BT-V4-MAP-002`、`BT-V4-E2E-004` | 当前构建/角色/尺寸截图、弱网/键盘/重启、大字体/TalkBack/拟支持平台；未测范围保留待核验 |
| 4.13 正确性、安全、恢复 | `BT-V4-MIG-001`、`BT-V4-E2E-003`、`BT-V4-SAF-004`；源 `BT-V5-AIR-040` | 隔离库授权/并发/迁移、源删除与缓存/索引/在途传播；未来批准/效果账本不由现 `confirmed:true` 替代 |
| 4.14 发布、监控、运营 | `BT-REL-001`、`BT-V4-PIL-003`、`BT-V4-OBS-001` | 真实版本/环境、监控日志、告警/回退/停用演练、负责人与支持；Debug 不作正式发布证据 |
| 4.15 价值、留存、持续供给 | `BT-V4-ANA-001`、`BT-V4-PIL-004` | 真 cohort、明确分母/完整窗口、非合成供给、无提示独立完成；无供给/产品失败/主动放弃分开 |

本轮只解除“消费原文缺失”。当前源码/测试已有细分证据应复用原任务，不能从评估的“待核验”推断存在漏洞，也不能从工程 PASS 推断消费者体验、出席、送达或留存已验证。真实截图不等于无提示用户完成；严重越权、未经授权执行、假成功或重复效果不能被总体平均数抵消。

## 8 安全检查点后的依赖安排

本表是逐条矩阵 §6 的接续建议，不导入或重排实时队列。由原执行者完成当前 SAF004 的实现、验证、证据及状态后，使用现有工具/schema 做增量导入 dry-run 与去重，只有确认未覆盖需求才追加。

1. 先完成当前原子任务检查点，确认矩阵、ADR/职责及兼容范围，保留原队列。
2. 交错建设 AGE 的最小 Profile/Memory/Evidence 权威接口和 AIR 的网关、安全、预算/出口约束；不重建既有 Agent 身份。受控假网关/离线代码可独立推进，live 供应商批准与凭据另列。
3. 共享一个事件/outbox 边界，提供最小当前授权 Context 与持久 AgentRun；`AIR-022/023` 读 AGE 最小权威接口，不等完整 learner，也不另造记忆真源。
4. 完成输出验证、有限 Planner/Tool、沙箱批准/效果与未知结果恢复。必要幂等、撤权竞争、脱敏和恢复属于本期门槛，不能因完整观测任务为 P1 而后置。
5. `AGE-004/005/007` 基础写合同先于 `AIR-043` 文字候选；`AGE-020` 高层管线之后消费结果，避免循环。接入确认/纠正/删源与客户端审阅后才建立文字闭环。
6. P0 只读活动 adapter 直接复用现有活动检索，不等待整项 AIR-039、第二供应商、视觉或 A2A。离线评测、默认关闭开关、本地交付和 live canary 分开判定。
7. 后续视觉、Attention、真实写、双供应商与 A2A 按真实依赖和独立开关推进；A2A/原生预订仍遵循 V4 Post-Pilot gate，材料取得不提前开放。

这里的 AIR 简称对应源编号 `BT-V5-AIR-*`，AGE 保留源编号 `AGE-*`；归一化编号和建议能力序可查机器矩阵，不能当成已经导入的实时任务。

## 9 放行结论与恢复条件

**Closed Pilot Ready：NO；Consumer Beta Ready：NO。** 真实 IdP/HTTPS 与拟发布真机、核验组织及主办方授权活动、有效生产 API/地图、部署与日志访问、值守/备用支持、提醒调度告警及真实 A→H 仍需实际证据。开发 seed、Debug、合成 E2E、材料包格式验证和 UI 规则接入都不替代这些条件。

AGE Foundation 与 AIR P0/live 能力以对应任务和门槛核验；本轮没有模型出口、视觉、记忆写入、真实自动动作或 live A2A 验收，不因接入材料宣称已启用。外部配置只阻相关 live/试点范围，其他满足依赖的仓库工作继续按原协议执行。

验证仅限本记录的路径存在、文件哈希、JSON 解析、5/15 映射、规则引用、144 项队列及历史 checkpoint 未被本轮改写；独立机器记录见 [resolved JSON](../../work/v5-material-consumer-resolved.json)。

## 10 实际安全检查点与增量接续

以上§1–9保留材料取得时的历史审计口径；现在SAF004已实际DONE，V5 Phase0已接续。唯一live队列252项，144原对象完整相等，137source最终6REUSE/3VERIFY/128APPEND→108新任务，二次dry新增0、原状态/证据/owner/lock保留。5个LIVE仍BLOCKED、3VERIFY须后续权威Profile/Memory及实际Runtime证据，不从旧DONE推断。

源文件原文未移动/删除；AIR设计/需求/原规划JSON选取进入docs对应目录，[复制来源收据](../../work/v5-canonical-source-receipt.json)留原路径/hash。唯一认知[ADR](../decisions/ADR-AGENT-COGNITIVE-ARCHITECTURE.md)、[Memory](../architecture/AGENT-MEMORY-ARCHITECTURE.md)、[个人Agent](../product/PERSONAL-AGENT-ENRICHMENT.md)区分当前实现与目标；[V5接续协议](../../automation/CODEX_V5_EXECUTION_PROTOCOL.md)继承原V4，不建第二状态队列。

112认知合同/真实库、180并发限定、26导入/20调度/workflow、三轮默认并发全量Go各1224、vet/build及052迁移兼容PASS，实际证据见[Phase0验收](../testing/evidence/v5-integration-2026-10-02/README.md)。本期仅共享边界、现有本人窄只读及默认Unavailable认知端口；没有Memory或provider实现。按实际接口依赖立即领取后续Profile/Memory/Evidence及AIR安全约束，AGE/AIR无需机械串行，原V4仍保留。ClosedPilot/ConsumerBeta均NO。
