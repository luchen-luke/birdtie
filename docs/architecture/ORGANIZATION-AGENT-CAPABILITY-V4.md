# Organization Agent 公开问答能力包

2026-10-03；BT-V4-ORG-001。本文件为能力接线 canonical；主体和所有权继续遵循原 Agent Identity / actorref / V4 规范，FAQ、组织管理与公开活动仍由原领域服务维护。

## 已实现的调用链

`POST /v1/organizations/{organizationID}/agent/ask` → 原 FAQStore → PostgreSQL 当前公开来源 → `agentorganization.Answer` → 既有 `agentruntime.ForType(ORGANIZATION)` 的 `OrganizationContextRead` → 原 `organization.BuildAnswer`。

已发布 FAQ、组织介绍、HTTPS 官方链接与本组织公开近期活动继续使用原窄规则匹配，返回中文答案、稳定来源类型/ID；没有可引用资料时明确 unknown。不是模型生成、成员记忆或工具执行。外部模型、组织 Memory 及 A2A 用途许可仍不可用。

## 来源与主体边界

- Organization Actor 使用 `organizations.id`；Principal 使用 `organizations.account_id`；Agent 使用当前对应组织 Agent/Metadata。类型与真实 FK 关系分别核对；不额外要求不同表中的 UUID 文本必然不相同。
- 当前公开、active Organization 和 active Organization Account；双方封锁均拒绝。组织 Agent active 且 Metadata 正确绑定方可提供知识。未核验组织或失效 Agent 不引用任何答案来源。
- “核验”是数据库当前组织核验状态，不表示 FAQ 中每项现实事实被独立查证；FAQ 是该组织明确发布的声明。本轮测试核验状态、组织和活动全部是隔离库合成资料，不是 CSSA 身份/授权证据。
- FAQ 必须本组织、published；最多100项。活动必须本组织的 typed organizer、published/public、未取消、尚未开始、City公开且未失效；最多20项。组织管理员也不能将成员限定活动加入公开 Agent 知识。
- 只取回答需要的标题、时间和公开场地名称，不读私人资料/Memory/成员列表，不返回精确坐标、活动全文、候选、维护者私有资料或模型批准。
- 回答中的旧链接也按原公开资料 HTTPS/非空 hostname/无 userinfo/2048字节限制检查；不合格回执拒绝输出。

## 当前读取与会话

公开 payload 是一个有界 SQL statement。显式 READ COMMITTED、READ ONLY覆盖连接默认 RR，组织状态/核验/Agent/FAQ/活动在同一个当前帧中读取。真实 FAQ 表锁等待期间发生撤核验、组织改私密、FAQ下线或请求取消的测试已运行。被引用活动在提交前另以真实 PostgreSQL 时钟核对期限；未选中的活动不使有效 FAQ 过期。

来源 ACL 的线性化点是该公开 payload statement；最终时间检查不冒称重新检查全部来源 ACL，也不宣称网络发出后还能撤回已经交付的回答。

匿名用户可问公开问题；带凭据时只接受原 canonical bearer/digest，必须具备本接口的原生 Session 能力。初次解析使用实际 Session/Account，显式 Account SHARE → exact Session UPDATE 顺序，锁后新 statement/真实 PG clock 检查 active/revoked/绝对与闲置期限/dev-phone配置；有效才续闲置时间。最终使用相同 digest/actor、Account/Session SHARE和锁后新时钟检查，不刷新期限。缺原生能力503，失效401，不降级到 owner-ID 或匿名。

这里只收口组织问答的局部会话路径；旧全局 `Authenticate` 未修改，不能据此声称所有旧入口均已修复。身份令牌仍由既有登录流程产生，没有新登录/OIDC或 client self-confirm 绕过路径。

## Wire / UX

请求 JSON 只允许一个 `query`，拒绝重复、大小写别名、grant/owner/source字段、URL selector、workspace header和尾随 JSON。最大4096字节、有效UTF8、2–240字符；不把客户/模型输入视为授权。响应 `Cache-Control: no-store`，结构不合法或请求取消不输出来源。

复用原界面与稳定详情，无客户端改动。适用 UX-CHECK-01/02/06/08/09/10/11/12/16：中文、未知不猜、实体稳定、来源和当前身份清楚。原组织界面/辅助技术本轮未另验；不能借后端通过声称消费级 UI 或试点验收。

## 证据

见 [任务审计](../research/BIRDTIE-V4-ORG-001-AUDIT.md)、[专项证据](../testing/evidence/organization-agent-capability-2026-10-03/README.md)。真实 current066 native6：69 Test PASS/0FAIL/SKIP，vet/build0；公开完整行/428观察源保持、9 owned源固定、自有库DROP。初始/最终×绝对/idle期限、默认RR/来源等待、原人类 Profile兼容、无新增记忆/候选/Task/消息效果均实测。

根共同066三轮各7807 Test PASS/0FAIL/SKIP、vet/build0，557 API Go/SQL/module源固定、旧完整数据保留、owned DB DROP，归档1288文件manifest SHA `b6e9f78932578d936d2a614c75d98e2b376760389dce185166f5f414917eb28a`。后续067不在该历史帧。

Closed Pilot / Consumer Beta：NO。无生产身份/HTTPS/现实组织和活动授权/生产地图与运营部署/真实A→H；没有正式部署、发信或外部服务修改。
