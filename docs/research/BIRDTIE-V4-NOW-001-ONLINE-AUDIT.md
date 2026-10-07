# BT-V4-NOW-001 线上查询增量审计

日期：2026-10-04；owner sponsored_trust。原 PARTIAL 完整对象及既往证据保留在根代理 original-now001-partial-online1.json；唯一 live 队列由根代理维护。

## 原要求与实际差距

原 goal：保留 map + agent + action，支持非地图 online/cross-city 社交路由。唯一原 AC：Now 能显示当前情境、活跃意图、Agent 结果和动作，而不假定所有结果均为本地地图点。verify：Flutter integration + regression。OPP-001/CTX-001/TST-001 全部 DONE；原 Social Alpha 分类并非真人正式发布门槛。

旧实现已有中文 ONLINE 私人草稿界面、Context 明确声明、稳定地图和受限布局修复，但 RemoteAgentTaskSource 与 API Task 查询只沿 /v1/cities/{cityId}/agent/tasks，不能从真实 ONLINE 声明完成查询→持久任务→追问→原详情。已有 private/friend human ACL 不是模型用途许可；不能复用 AGE033 将未选私人内容送给查询。实际可复用 PUBLIC ACTIVE social_intent、typed Context 和原 agent_tasks，无需新授权账本/DDL。

计划：严格闭集最小 DTO→本人当前 native 来源与版本复核→原 Task 保存/恢复/CAS→ONLINE 选择与规则先回答→稳定只读 Intent 详情→保留CITY地图/Pin/selection→权限、撤回、迟到、未知结果、小屏与旧回归→源冻结。API 冻结期间仅专属 staged-source；根079-release后落入已登记范围。server 四路由由根登记；MapCanvas与Conversation新增精确范围由根扩 lease，未越范围。

## 复用与实施

| 来源 | 处理 |
| --- | --- |
| contexts / person_contexts 与 PersonContextsPage | REUSE 明确本人 ONLINE declaration；不作成员/运营证明 |
| social_intents / 原 native Draft→Activate | REUSE PUBLIC ACTIVE finite 来源，不公开私人草稿 |
| agent_tasks / agent_workspace / AGENT_TASK_COMPLETED | EXTEND 原账本真实 ONLINE typedContext、CAS、当前来源恢复 |
| MapWorkspace / MapCanvas / Composer / ResultSheet / Conversation | EXTEND 当前入口与标准组件，ONLINE 无 MapEffects，稳定地图 |
| nowcontextquery / native PG / HTTP / Flutter transport | NEW 精确公开规则读取封套，非模型/认知/通用工具权限 |
| Civu | 未复制或调用；本轮不涉及 Civu reuse |

## 真实失败与修复链

- 初期 compile1/compile2：WIP 编译错误和纯规则文案断言失配；保存准确分类，不计原生通过。
- native1：14 PASS/3 FAIL，两个 fixture 使用不存在的 Agent 状态 paused 及父失败；改为领域实际 suspended，没有放宽权限断言。
- native2：root 同批业务通知测试 identity.DigestToken 不存在导致编译失败，0 native；root实际SHA256修正后重跑。native3 17/native4 27 PASS。
- 根只读审查发现选项列表仅末检查Session：源组装后撤回/ABA仍可能返旧本人标签。独立旧 native4 产品镜像加入同等 source-change 测试：options-old-red1 缺原seed启动失败，ownedDROP；补原输入后 options-old-red2 四子+父 5 FAIL、HTTP200泄漏真实 fixture 私密标签。加入源行锁/完整集合 sealed frame/HTTP native revalidate 后 native5 32、native6 34 PASS。
- 首轮客户端：UTF-8 fixture、Map<String,String>字段类型与借用client探针错误分别保存，修实际 fixture 而不豁免 closed DTO。新增卡片未包 Material 的真实 Ink 断言已修。Map ONLINE 真正提交被旧 City empty-config guard 拦截：将 ONLINE 分支置 City guard 之前，真整页选择→查询→详情通过。
- client-final1 analyze3 curly info、97 PASS/1 FAIL；final2 analyze0、96 PASS/2 FAIL；新大字试验曾误用 expanded 对话而非 medium 结果，旧路线 lazy ListView 未滚动，纠正测试过程。final3 100 PASS/1 FAIL 与 diagnostic3/4 证明 medium 下大头部 NestedScrollView 内body临时出现、settle后卸载，无法稳定触达卡片。结果模式改单一 CustomScrollView；final4 100 PASS/1 FAIL 为长tile中心处于viewport外，使用真实可见交集至少48dp并实际tap。未隐藏 warning 或关闭命中检查。
- ONLINE Conversation 旧文案显示0活动/0地点：online-conversation-red1 真实1 FAIL后最小修为实际公开意图数量；旧CITY计数保留。
- 最终 client-final5 90功能+11loading/0 FAIL-SKIP、analyze0，257源稳定；native6 34 PASS/vet-build0/783源稳定/public完整行同值/ownedDROP。日志均不可覆盖。

## 适用 UX 核验

| 检查 | 本轮证据/分界 |
| --- | --- |
| 01、02、03 | 中文明确线上入口，原节点选择及追问复用；无模型问答或重复补全访谈 |
| 04、05 | 当前 Context+Task 原ID，规则先回答/卡片/重新GET原Intent ID，PUBLIC受众且无邀请 |
| 06、11 | 不猜城市/距离/共同兴趣；真实空态/过期/屏蔽；无城市/模型/定位也可公开规则查询 |
| 07、09 | 成功须合法 Task，POST超时/500/409/坏DTO未知不盲retry，无影子ledger或自动POST |
| 08、10 | native源/主体/Session ABA、HTTP末复核；UI A-B-A/same-key client/base退役与迟到拒绝 |
| 12 | 原Task GET只读恢复/不改updatedAt，返回详情路线退役；当前进程重启真机 NOT_RUN |
| 13、14 | 原48dp/detent/中文标准组件；320/font3原卡片实际可见48dp tap、90/104/180与20focus回归；TalkBack NOT_RUN |
| 15 | widget自动验收不当作独立真人消费任务；6真人 NOT_RUN |
| 16 | 无新日志私密payload/模型输入/Memory；原本人Task的query/conversation领域存储明确存在 |

## 交付边界

source-freeze1.json 列30现存 leased源（包括未修改的兼容文件），SHA f1b287a47c898f6004c041f22e300eefcc2a4870725d6f87d2da7b93fbfb8e99。原 AC 的本地ONLINE闭环已实现并定向核验；完整整仓/当前APK/真机/发布状态由根独立验证后判断，worker不自行DONE。

未新增 COUNTRY/INSTITUTION/COMMUNITY/线下跨城通用查询、机会参与转换、模型/Memory权限、运营供给或真实人测试；不得把这些独立能力的缺口悄然解除。Closed Pilot/Consumer Beta 保持 NO。当前native fixtures均 LOCAL_SYNTHETIC，不冒充真实用户/运营批准/现实供给。
