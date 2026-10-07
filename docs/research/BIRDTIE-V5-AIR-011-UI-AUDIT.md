# AIR011 本人人审 UI 增量审计与计划

2026-10-04。接续原 PARTIAL，唯一 live 状态及共享接线由根代理负责。本文件不表示功能通过验收。

## 实际边界

原五项依赖 AIR007、AIR008、AGE066、SAF004、INT001 已 DONE。原 062 Store 与四条 HTTP 提供具体 Preview、原 ID/digest Approve、本人 Revoke、四层 Budget 读。AIR016、真实 provider、Run 和每次重试接线仍缺，不阻止本地人工 UI 切片，也不能被此切片解除。033 TASK_CONTEXT_READ 与相关性/预算投影不授予 MODEL_CONTEXT_EGRESS。

缺少可发现的既存 root/task/price 选择及持久批准状态读取。预算值不能核实批准成功。不能用 UUID 输入、开发固定 price/root、客户端 confirmed 或假 Run 补齐。

## 本轮计划

1. 新原领域 HumanOptions/HumanReceipt 只读 DTO；复用现有 session、source、config、price、expiry 和 owner 事务边界，不加 DDL 或权限账本。
2. 有界本人有效 options、最新 50 receipt metadata 和任意原 preview ID receipt。原 Session 失效、source/metadata 变化、撤回或过期时不返回正文、不准重试批准；当前合法本人仍可查旧状态及调用原撤回。
3. 三条只读 HTTP 和中文 Page/API/controller；Settings/server 注册由 root 完成。配置缺失诚实空状态。批准只表示本地具体许可，模型始终不可用。
4. 原 ID 查询未知提交、原 ID 撤回、重启找回；不自动重新 POST Preview。批准响应丢失先原 ID 核权威状态。
5. 隔离 fresh076 原生正负、等待后权限/期限、全 public/catalog 不变及 GET 零账目副作用；严格日期 offset、身份 epoch/迟到、移动/中文 Flutter 检查。保留首次失败。根最终冻结后跑完整回归。

UIUX 适用 01/02/05/06/07/08/09/10/11/12/13/14/16。复用现 Material/主题、现 page auth/workspace 注入、generation 与边界内 Navigator 的模式；不复用记忆候选的业务许可。新三个领域页面文件而非新 UI 框架。

无模型/外部服务、正式费用、真实 Run/网络出口/运营证明。完整 AIR011 保持 PARTIAL，AGE035、Closed Pilot/Consumer Beta 门槛不降低。真机、TalkBack、全仓结果按实际后续证据记录，不预填 PASS。

## 本轮已实际实现

本人可以从设置的“模型请求许可”直接进入中文页面，从原生已有且当前有效的任务、root、价格和预算中选择，不输入 UUID，也不创建默认配置。无合法配置时显示空状态。选择后读取四层预算，按原领域 POST 形成短期具体预览，检查本人、任务原句、Provider/Model/Version、region/retention、prompt/schema/price 版本、费用和 token 上界、实际期限，再决定批准或退出。

三个 GET 端口已由根代理注册：`/v1/me/model-egress/options`、`/v1/me/model-egress/previews`、`/v1/me/model-egress/previews/{previewID}`。Options 最多 20 个有效选项，收据列表为最新 50 条；列表以外的原 ID 仍可核实。已有四个写/预算端口复用 062 Store。三个新 GET 不创建或修改 Preview、批准、reserve、budget、audit；正常 HTTP 认证仍可能刷新原 Session 的 idle 时间，不能把领域账目零写称为整个认证链零写。

Receipt 从原持久记录恢复当前状态。有效正文要求原 Session 和原 source/config/price/root 的具体版本仍有效；换 Session、源变化、自然过期或撤回后仅返回当前本人可见的 metadata，不能继承批准。本人仍可调用原撤回。最终 reviewable 分支在所有可能的行/表等待之后，由同一原生 `egressFinish` 核当前原始期限、root/price 和本人 Session，随后仅 Commit；失效 metadata 分支只读核当前本人 Session，不复活原正文或批准。

中文控制器记录原 preview ID。批准/撤回响应未知时只查同一 ID，不自动重发或用另一条收据替代。Preview 创建结果未知时不盲重建；唯一匹配原 root/task/price/具体期限的当前收据才允许核实，无法唯一匹配保持未知。重复批准按钮只会发送一次原 ID/digest。页面/嵌套确认框绑定账号、Session token、工作区和 controller epoch；同 key 的 auth/client/API base/workspace 替换会退休旧运输端和确认框，迟到响应不能覆盖当前页面，借用的 client 不被关闭。

批准仅是本地具体版本许可。页面明确显示模型未开放、价格为本地验证资料，不预留费用、不调用模型、不产生真实付费或 Memory/RSVP。033 上下文权限未继承为出口权限。本轮没有 DDL、配置维护 API 或新授权账本。

## 实际检查与首次失败

- 隔离 `native4`：fresh001–076，原有非空全 public 行与 catalog 在迁移 up/down/reapply、unconsumed-preview down/reapply 和原生测试后保持；owned DB 最终删除。实际 `^TestModelEgress` 在三个包中 125 PASS events、0 FAIL/SKIP/package failure，test/vet/build exit 0，727 API 源和本 worker 7 源稳定。只代表目标套件，不是全仓测试。
- 原生覆盖有效选择、具体收据/批准/撤回、旧 Session replacement、账号隔离、当前 Task/Agent metadata 变化、自然期限、真实 advisory/table lock 后期限、最新 50 条及原 ID 读取、缺价格无配置、GET 零领域账目写和已注册 HTTP 路由。
- Flutter 定向 `flutter-tests8.log`：18 PASS、0 failure；6 个源/测试文件 analyze `flutter-analyze-final3.log` exit 0。包含严格 RFC3339 Z/合法正负 offset 与日历边界，具体 DTO/cost/scope/status、未知批准/撤回与 Preview、重复点击、身份 A→B→A/迟到/销毁、同 key 运输端替换、360×720 且文字缩放 1.6 的检查/取消，以及工作区切换后的嵌套确认退休。
- 初次 Go compile 是测试将 PrivateAccess 误用为另一原生 Access，日志保留；native1 是 root 注册路由前的真实 404 和源漂移，119 PASS/2 FAIL，不作最终验收；初次 runner 错拷 AGE007 owned 列表，明确只作历史诊断。native2 121 PASS；native3 125 PASS 仍是最终 clock 修复前旧帧，不覆盖 native4。
- 根代码审查发现并修复两项真实集成缺陷：同 key 运输端替换仍保留旧 API/dialog（`flutter-transport-red.log` 真正 RED），以及 readReceipt 的第二次 owner-only Finish 在有效正文分支不再包含原预览期限。前者修后 widget PASS；后者修后 native4 PASS，未把真实缺陷归为 fixture 错误。
- Flutter 早期失败包含预算 fixture Map 推断、void reset await、lazy ListView 查找/scrollable、退出提示尚未滚回视区、最后重复点击测试在 MockClient 接收流前过早计数。全部日志保留；最后用实际 entered Completer 等运输端收到请求，仍断言只一个 POST。没有放宽权限或跳过断言。

## UX 约束接入及验收范围

01/02/05/06：先从当前真实可用任务整理选项，空配置诚实说明，具体版本与关键费用/目的地/期限可见。07/08/09：设置直接路径、原领域动作、检查/批准/权威结果分开。10：未知原 ID 核实与单次提交。11：中文与既有 Material/主题、可滚动具体预览。12/13/14/16：移动大字 widget 检查、异步/身份/工作区退休、scope/source/current-clock 测试均有本地记录；真实设备截图与辅助技术验收尚未运行，不称规则全部通过。

## 尚缺 / 不扩大结论

完整 AIR011 仍 PARTIAL：真实 provider/model/secret、模型 Run 及每一次重试的出口/预算消费接线、真实价格和 paid quota、真实端到端效果、当前 APK 的手机截图/重启/TalkBack 等未完成。AGE035、Closed Pilot、Consumer Beta 均未解除。根最终全仓 Go/Flutter/build/真机在各自新帧核证，不沿用历史批次证据。

原 062 的任意 SQL 对同一 Session row `revoked_at` 置值再清空未由本轮新增 generation/DDL 约束；此审计发现尚未做 RED。当前生产 `Authenticate` 只合法刷新当前 Session idle，`Revoke` 单向撤回，没有合法 clear-revoked API。原生测试证明的是实际不同 Session 替换与期限/元数据撤权，不声称已证明所有任意 SQL Session ABA，也不声称现生产 API 可利用该 SQL 恢复。原 Task digest/updated_at 合同同样未扩大为任意 SQL 全字段复位的证明。

当前 13 源 SHA/bytes 在 `work/v5-air011-ui/worker-freeze-receipt.json`；原生命令/数据/源帧在 `work/v5-air011-ui/native4`。完成不可变 worker 档案后交根代理核证，worker 不修改唯一 live 队列。
