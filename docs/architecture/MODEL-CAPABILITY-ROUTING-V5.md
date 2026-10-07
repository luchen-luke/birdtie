# 模型能力登记与路由资格 V5

2026-10-03（Asia/Shanghai）。对应 [BT-V5-AIR-008 原文](../product/BT-V5-AIR-REQUIREMENTS.md)。本项为 CODE_LOCAL 能力路由模型；真实推理仍 DISABLED / UNAVAILABLE。遵守[认知 ADR](../decisions/ADR-AGENT-COGNITIVE-ARCHITECTURE.md)、[Memory 边界](AGENT-MEMORY-ARCHITECTURE.md)、[007 契约](MODEL-GATEWAY-CONTRACT-V5.md)与[066 服务端开关](AGENT-FEATURE-FLAGS-V5.md)。没有新增 HTTP、Flutter、Settings、数据库、provider SDK、凭证、费用或部署。

## 实际实现

实现位于 `apps/api/internal/modelcapability`，复用现有 typed Actor / Agent 与 `agentevent.SourceVersion`，以及 007 的规范请求、fake adapter、结果与闭集本地 schema。

| 接口 | 能力与边界 |
| --- | --- |
| `NewRegistry` | 服务端构造不可变、至多 32 条能力记录；exact provider / model / version / wire contract 唯一，六类三态显式填写；复制切片与成本指针 |
| `SelectOffline` | 只验证合成授权事实并生成 OFFLINE_CONTRACT 元数据路由；当前数据许可先于能力与质量/成本 |
| `NewOfflineRunner` / `NewOfflineRunnerWithClock` / `Complete` | 显式 fake adapter 的 exact descriptor + wire 校验；公开的合成 clock 可复现 exact checkedAt；复用 007 OfflineHarness 本地 schema，输出仍提案 |
| `NewService` / `Complete` | 调用 007 默认 Gateway；服务端开关即使返回 ON 也始终关闭，因为实际 source-purpose / provider-egress / budget 解析器与获准 adapter 尚缺 |

能力记录独立列出 vision、tools、schema、streaming、state、storage 的 SUPPORTED / UNSUPPORTED / UNKNOWN；text 是 007 的基础能力。记录保存验证时间、失效时间（最长 30 天）、闭集地域、证据类别与来源链接。未来验证时间或已到期记录不参与选择。空能力、重复 key、动态 latest/default/auto、OpenAI-compatible 标签及未知证据类别拒绝。

Registry 内容摘要覆盖全部记录；记录替换或能力变化后旧摘要授权失效。摘要只证明结构一致性。Record、Registry、OfflineGrant 的 JSON 构造与序列化拒绝；客户端或模型的 Verified / ALLOWED 声明没有授权入口。

## 顺序与当前授权

1. 先复用 007 请求结构校验，并核对 exact typed Agent、请求摘要、Registry 摘要。
2. 合成 OfflineGrant 必须为 ALLOWED、用途 MODEL_CONTEXT_EGRESS，未撤销/删除；独立 consent / policy revision 非零且等于当前 revision；当前 source 与源版本完全相同。source 使用现有 updated_at digest 形状，不凭空发明原资源 revision。
3. 检查时间：checkedAt 等于本次检查时刻，expiresAt 严格晚于当前时刻且不越过请求 deadline。仅允许 grant 中明确列出的 exact destination 与地域；state/storage 还须各自批准。
4. 再过滤能力：请求所需能力 UNKNOWN 或 UNSUPPORTED 时禁用。Structured / ToolProposals 的 native schema 明确 UNSUPPORTED 时可选，但强制执行 007 的本地闭集 schema；UNKNOWN schema 不自动视为已知支持或替代策略。
5. 合格候选才按 quality ordinal 降序、已知 synthetic cost 优先、cost 升序、exact key 字典序确定结果。缺失成本保持 UNKNOWN；估算为 SYNTHETIC_ESTIMATE，不是价格、账单或支出批准。无合格候选返回 UNSUPPORTED_CAPABILITY。

OfflineGrant 只供开发者构造合成合同；字符串、source digest、revision、时间戳与形状检查均不能证明实际授权。公开 NewOfflineRunnerWithClock 只接受离线合同的本地时钟，真实 Service 没有该入口；未来 current resolver 的时钟须由受信服务端获得，不能由调用者自证。当前没有受信 current source / tenant / purpose / region-retention / provider / spend resolver；它不能传入真实 Service 开启推理。Organization 保持组织主体命名空间与角色，PERSONAL 不能继承组织许可；Business 继续 dormant。

## 六类能力与消费者边界

| 能力 | 008 元数据合同 | 当前实际离线执行 |
| --- | --- | --- |
| vision | 所需时仅 SUPPORTED 可合格 | 007 没有 image bytes / URL DTO；008 显式拒绝 vision，零 adapter 调用；不能删图后按 text 执行 |
| tools | ToolProposals 需要 SUPPORTED | 仅允许 007 已知只读提案；未知工具/权限字段拒绝；无执行器 |
| schema | SUPPORTED 或明确 UNSUPPORTED + 强制本地校验 | 未知/重复/缺失字段、模式不一致拒绝；拒答/截断不释放部分内容 |
| streaming | 所需时仅 SUPPORTED 可合格 | 007 无 stream 消费合同；明确拒绝，零 adapter 调用 |
| state | 能力与独立 destination AllowState 同时需要 | 007 无 provider state 消费合同；明确拒绝，零 adapter 调用 |
| storage | 能力与独立 destination AllowStorage 同时需要 | 007 无 provider storage 消费合同；明确拒绝，零 adapter 调用 |

fake 响应后再次检查授权与能力记录失效时间、exact adapter descriptor / wire、取消与 deadline；失效内容丢弃。OfflineGrant 是合成快照，不能模拟中途真实源撤权的原子判定；真实服务维持 Unavailable。007 原始 Agent / run / context / policy / budget selectors 不进入 fake provider payload。此包不写日志中的模型输入，也不保存输出、个人事实或 Memory。

## 官方资料核查与能力矩阵

查阅日期为 2026-10-03；这是 DOCUMENTATION_CHECKED，不是 LIVE_VERIFIED，不自动生成可执行 Registry 或获准租户配置。

| 文档候选 | vision | tools | schema | streaming | state | storage | 登记/路由状态 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| OpenAI / gpt-4o / gpt-4o-2024-08-06 | 官方模型页列 image 输入 | 官方页列 function calling | 官方页列 Structured Outputs | 官方页列 streaming | UNKNOWN：须逐 endpoint / 配置核验 | UNKNOWN：须逐地域 / 配置核验 | 未登记 wire contract、地域许可或 live adapter；不可路由 |
| DeepSeek Responses 来源链接 | UNKNOWN | UNKNOWN | UNKNOWN | UNKNOWN | UNKNOWN | UNKNOWN | 打开失败，UNVERIFIED，不猜其兼容能力 |
| fake-fixture / fake-text / fixture-v1 / text-wire.v1 | 合成三态矩阵 | 合成三态矩阵 | 合成本地严格校验 | 合成三态矩阵 | 合成三态矩阵 | 合成三态矩阵 | 仅 OFFLINE_CONTRACT，非真实供应商能力 |

模型页列出固定 snapshot 与能力；本表仅抄取文档支持范围，未验证账号可调用性。[OpenAI GPT-4o 官方模型页](https://developers.openai.com/api/docs/models/gpt-4o)

Structured Outputs 与只保证 JSON 形状的 JSON mode 有差异；schema 子集、拒答与不完整结果仍需应用处理。008 的本地校验始终执行。[Structured Outputs](https://developers.openai.com/api/docs/guides/structured-outputs)

图片支持依赖具体模型；图片输入具有成本，008 没有图片调用路径。[Images and vision](https://developers.openai.com/api/docs/guides/images-vision)。Responses streaming 使用单独的 SSE 消费语义，不能从普通 Complete 推断。[Streaming responses](https://developers.openai.com/api/docs/guides/streaming-responses)

Conversation 状态 API 与客户端手工上下文不同；storage/retention 与项目地域、endpoint、模型配置有关。本项保守地将未核验逐组合记录置 UNKNOWN，不能继承兼容标签、模型 state 或长期许可。[Conversation state](https://developers.openai.com/api/docs/guides/conversation-state)、[Data controls](https://developers.openai.com/api/docs/guides/your-data)

## 验证与未测范围

最终公开 clock 入口的 staging 与 production 新包各 130 个 test PASS、0 fail、0 skip；目标 vet/build exit 0，5 Go 源哈希与本轮冻结前后相同。早期129是不同源码的历史结果，已保留快照。实际日志、初次失败与修复、可复现命令、源码快照、007 未改 hash 均见[正式证据](../testing/evidence/model-capabilities-2026-10-03/README.md)。仅为本包 CODE_LOCAL；根代理另行记录全 API 回归。

没有 live canary、视觉/stream/state/storage 消费者、持久租户配置、UI、移动端/辅助技术截图或 race instrumentation。AIR009 获准 adapter 与 AIR011 出口/预算仍独立；不把 fake 当发布验收。Closed Pilot / Consumer Beta 保持 NO。

按[全局交互规则](../ux/GLOBAL-UX-INTERACTION-CONTRACT.md)适用 UX-CHECK-06/08/10 的未知、过期、撤权、主体与迟到边界，以及 11 的模型不可用和 16 的输入留存限制；本项只提供领域负例，未证明普通 UI 路径或全局 UI 整改完成。
