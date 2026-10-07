# AGE043 人类引荐 UI 增量审计

2026-10-04。接续原 PARTIAL，不新增队列。根代理拥有共享入口、canonical 和唯一 live 队列；worker 仅六个新 Dart 与专属证据范围。

## 真实接口及计划

1. 复用 GET 本人 new-people consent/intents、070 agent-policies、043 agent-introductions。仅本人当前 ACTIVE/PUBLIC/FIND_COMPANION 可选；现 NewPeoplePage 的非 PRIVATE 筛选不能直接当严格 PUBLIC 引荐选择器。
2. 065 是迁移号，对应 AGE070 原生 Policy APIs；当前客户端没有 Social 编辑。新页复用当前七类完整 SOCIAL 设置，只明确修改 UNKNOWN_PERSON / SHARED_COMMUNITY，原其余值保持；具体版本和期限预览、原 CAS 更新、取消零 PUT。403/空结果不解释对方私密设置。
3. 候选仅当前公开声明的 human review：精确 Person / Intent IDs、公开名称、中文依据、当前观察及期限。SourceBinding 是只读 receipt，不授予消息、模型或 Memory 权限。输出三个 effect/model 布尔必须 false。
4. 刷新/主体/工作区/同 key auth-client-base 变化退休旧响应、具体确认框和旧候选；未知保存先 GET 原策略，不自动重发。实际期限到达清除候选与批准。
5. 新页仅精确 callback 打开 Person 详情，或回原找新朋友 source ID 再查候选和独立批准。不给新页快捷邀请/聊天写接口。原 invite 未核043双方Social，所以不能称原 confirmed 为043具体版本批准。
6. 严格 DTO/日期时区、正常/空/拒绝/CAS/未知/迟到/ABA/运输端替换、小屏大字/键盘、定向 Flutter，再根冻结全回归/手机；未运行明确记录。

## 完整需求缺口保持

共同 Community PUBLIC person_contexts 只具条件 resolver；DeclarationInput 无 visibility，NormalizeDeclaration 不接受 COMMUNITY，原 Store 固定 private。SHARED_ACTIVITY 来源明确 UNAVAILABLE，RSVP/private Moment links 未获公开关联批准。本页不修这些领域，不推断成员身份、所在地/距离、共同到场、长期兴趣。AGE042/AIR037 四态机器消息仍未完成，不通过本页开启。

UX-CHECK-01/02/05/06/07/08/09/10/11/12/13/14/16 适用；主要用户结果为“本人自主查看当前公开意图的共同依据，并决定下一步”，设置/找新朋友为直接路径，中文与既有 Material 样式。完整 AGE043、Closed Pilot、Consumer Beta 保持未完成；原 AIR011 十三源与不可变档案不改。

## 已实施与真实证据

六个独占 Dart 文件已冻结。当前 client-final3 为 24 PASS、analyze exit 0、六源 SHA 稳定；client-final1 原 23 PASS 档案保留。新增测试直接读取 native-wire2 注册 HTTP 原始 JSON，不以手写 DTO 代替：UNCONFIGURED 省略时间字段、ACTIVE 七类策略、真实 creatorAccountId 和原生枚举、+08:00 时间、当前候选真实 Person/Intent IDs 与全部 effect=false 都通过 Dart API 消费。

native1 为只读现有原生依赖回归：五包 308 PASS / 0 FAIL-SKIP、vet/build exit 0、727 API 源稳定、独占 fresh001–076 旧非空 public/catalog/down/reapply 保留、库 DROP。它不单独证明 Flutter 或真机。native-wire2 另以隔离数据库真实 Store+httpapi.New 注册 HTTP，创建两位明确本地合成账号、原生 opt-in/草稿/激活/本人 SOCIAL CAS 后捕获十二响应，未调用模型、未联系或发消息；727源稳定、库DROP。这些账号与公开声明不是真实运营用户/活动。

## 失败保留与修复

早期测试 Response(string) Latin1 编码造成中文读取错误，改为 UTF8 Response.bytes；曾错误使用不存在的 encoding 参数，原失败日志保留。原生枚举首次生产实现使用错误名单，native-enums-red 真实失败后改为现有 native CLOSED set，ACTIVE/PUBLIC 资格不放宽。wire1 403 为本地探针漏注入 intentStore，不是产品 ACL 绕过；修探针并使用原激活 confirmed 语义后 wire2 通过。client-final2 的新增测试 getter 名误写引起编译失败，按真实 intentID 名修测试后 client-final3 通过，产品三源未变。

## 本轮未验收与门槛

worker 未运行新真机安装/截图、TalkBack、平台键盘、race、生产服务或整仓 Flutter build。小屏大字取消路径仅 widget 验证，无自由文本字段。根代理负责共同冻结后完整 Flutter、手机与共享入口。完整 AGE043 仍 PARTIAL：合法 SHARED_ACTIVITY 来源和 Community PUBLIC 人类写入口未实现，当前 policy/callback 不是双侧机器引荐/消息许可。Closed Pilot/Consumer Beta 仍 NO；没有外部身份、现实授权活动或生产证据被本轮代替。
