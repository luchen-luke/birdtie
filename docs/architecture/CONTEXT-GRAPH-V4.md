# Birdtie V4 Context Graph：主体之外的情境

状态：`BT-V4-CTX-001` 实现记录，2026-10-01。依据 [V4 产品规范](../product/BIRDTIE-CANONICAL-PRODUCT-SPEC-V4.md)、[ADR 0017](../decisions/0017-v4-actor-agent-context-place-model.md)及 `033_context_graph.sql`。本文件描述已实现的领域与持久化边界，不宣称在线 Intent 或非城市发现 UI 已上线。

## 对象与 ID

`contexts.id` 是稳定 UUID，`context_type` 为 `CITY`、`COUNTRY`、`INSTITUTION`、`COMMUNITY` 或 `ONLINE`。每类只允许一种受约束的来源键：CityContext 的 `city_id`、ISO 两字母国家码、机构键、Community ID 或在线键。`CITY` 的来源关联到平台 `city_contexts`；其旧城市字符串 ID 不变。机构键和在线键只是情境标识，不证明机构认证、成员身份或在线群组运营者。

`contextType/contextId` 在 Go `contextgraph.Ref` 和 Agent Task/Results API 模型中成对使用。Context 不是 ActorRef、账号、Agent、地理坐标或权限凭据；同一个 UUID 的不同类型不可混用。Person 和 Agent 主体仍由全局账户/主体 ID 标识，不增加必填 City FK。

`person_contexts` 允许一个活跃 Person 同时声明当前、家乡、过去、目的地、机构关联或兴趣情境，默认私密。该关系不从旧城市或成员资格推断，不自动授权读取 Community/机构资料。V4 `BT-V4-CTX-002` 增加本人会话下的 `GET/POST /v1/me/contexts` 和 `DELETE /v1/me/contexts/{contextID}/{relation}`，只接收已发布 City、本人填写的 Institution/Online 情境；Community 和 Country 关系虽在底层模型中保留，但目前不开放自助声明。写入当前城市会原子替换本人旧的当前 City，过去城市/学校、目的地与线上兴趣可并存。读取仅返回本人声明，不存在公开查询接口。学校名称和在线键仅是自我描述，绝不证明学历、成员资格或运营者身份。

## 兼容与边界

迁移 033 仅把已有 Agent Task 的城市映射到 `CITY` Context，保留 `city_id`、`city_context_id`、任务 ID 和旧 API 字段。新增 CityContext 时数据库自动创建 typed 节点；旧写入缺少 Context 引用时，数据库从明确的城市补齐。新任务存储可以使用无 `city_id` 的非城市 Context，`SaveTask` 与读取模型可往返保存在线任务；当前 HTTP Agent 路由仍是城市路径，在线查询/社会 Intent 端点属于后续任务。旧 `intents.city_id NOT NULL` 保持原样，不能用虚构城市代表在线 Intent。

Community Context 只引用 Community；它不会创建 Community Agent。任何依赖私密成员关系、机构资格、个人位置或跨主体数据的推荐/Agent 工具仍须独立服务端授权。Context Graph 本身不发布内容或赋予可见性。

## 验证与回退

`automation/verify_context_graph_migration.ps1` 在一次性数据库执行 001–033、旧 Agent Task 回填、旧写入、跨城市及在线任务、Go 全量测试、033 down/reapply；`test-sql/033_context_graph.sql` 的合成数据回滚。033 down 在有非城市 Context、Person Context 或非城市任务时拒绝，以免删除新数据。当前开发主数据库未由此脚本迁移，正式环境也未迁移。
