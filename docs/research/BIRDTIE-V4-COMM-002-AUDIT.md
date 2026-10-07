# BT-V4-COMM-002 审计与验收记录

2026-10-03；官方仓库 `D:\Project\birdtie`。依据 live 任务原 Goal/Acceptance/Verify、既有 Community 社交领域、V4/V5 协议、持续 UX 和实际原生代码；无重复队列导入。来源沿原任务的 V4 Workbook Requirements 行，既有领域原材料路径由 [原 canonical](../architecture/COMMUNITY-AND-ACTIVITY-SOCIAL-MODEL.md) 保留。

## 初始能力核对与真实缺口

| 范围 | 初始实际状态 | 本轮处理 |
| --- | --- | --- |
| 社群身份/多成员、原 16 路由、活动可见性 | 已有原生表/Store/API，公开 RSVP 不自动加入社群 | 复用，不新增 Community Agent/台账 |
| HIDDEN+OPEN 已知 ID 加入 | 实际注册 HTTP 曾返回 200 并写成员 | 当前源权限校验后 404/无写 |
| 首次 Authenticate 后等待时撤会话/过期 | 旧 actor-ID 路径实际迟到 200 | 当前 Session 同事务窄网关，真实锁等待/时钟负例 |
| 管理者等待时降权 | 旧路径实际 PATCH 200 | 当前角色/源快照重验，403/无写 |
| 原客户端管理 | 页面只有部分审批/归档，缺完整邀请/角色/移除/转让流程 | 真实原 API + 检查/明确确认/权威刷新 |
| 名单隐私 | 既有独立姓名字段规则可用 | 复用 actual 055，实际隐私 API 负例；不重造权限 |
| 角色转让与历史创建人 | 原 membership 转让已存在，creator/source 锚点保持 | 明确二者合同；不伪造更广移交 |
| 正式生产/CSSA/部署通知 | 外部条件仍缺 | 本轮未部署、未发布、未发送通知或联系合作方 |

## 实施与失败保留

- `work/v4-comm002/red2-*` 是真实注册旧路径的 HIDDEN join、Account 等待期间 revoke、Community 等待期间自然到期、等待期间降权 RED；其前 `red1` fixture SQL 类型/构造缺 capability 错误不冒认权限漏洞。
- `native1` 5 项、`native2` 8 项为较早源码/067 范围历史证据；不充当最新 068 全仓通过。
- `native3` 被并行 `social_now_integration_test.go` 未用 import 编译阻塞，保存原 build-fail/vet；并行作者修复后重跑。
- `native4` 56 PASS/1 TEST FAIL：fixture 把粗粒度 Profile visibility 当成独立姓名字段规则，不能据此声称新泄漏。后通过真实 GET/PUT field-visibility 设置 PRIVATE，再验证名单姓名隐藏，未削弱 resolver。
- `native5` fresh001–068，两轮各 57 Test PASS、0 FAIL/SKIP、vet/build0；完整旧 public 行/owned10 SHA 稳定，独立 DB 删除。全 API 并行变化明确记录为 false，不冒称共享冻结。
- `native6` 新增来源链 fixture 忘记显式 accounts.id，实际两轮均失败；不称权限 RED。显式随机 ID 后 `native7` fresh001–068，两轮各 58 Test PASS、0 FAIL/SKIP、vet/build0、旧完整 public/owned10/本轮全部 API SHA 稳定，DB DROP。新增实际 059 guard/061 source 正负只验证既有来源权限，不执行发布/通知。
- Flutter 初次 lint/48像素触达/测试 semantics 生命周期/fixture Latin1 等失败和修复保留。迟到确认 A→B→A 的旧 `finally` 清理误改新 busy 实际合成异步 RED 保留；代际 guard 后 GREEN。合成异步证明客户端逻辑，不是后端许可证明。
- 最后 `client-final1` 实际目标 analyze0/30 非 hidden Test PASS/0 Error/SKIP，20 Go/Dart/test 实际生产源 SHA 前后稳定。根独立 `work/v5-age051/root-community1/root-community1-result.json` fresh001–068 58 PASS/0 FAIL/SKIP、vet/build0、完整 public 和 owned/allAPI SHA 稳定、DB DROP；与 worker 原始证据分别保留。

最后实际 scope、完整检查、source manifest、独立复核与设备证据以 [归档 README](../testing/evidence/community-discovery-membership-2026-10-03/README.md) 为准，本文件不替 root 标 DONE。

## 未扩大边界

没有新 DDL、来源自动移交、Community Agent、推理/出网/Memory/自动通知许可。实际 059 SQL 来源选择与 061 来源元数据读取应与当前管理角色分别核验；SQL shape fixture 不称发布/通知送达。未获得实际生产身份、获准真实活动、HTTPS/地图/部署与值守条件，Closed Pilot/Consumer Beta 保持 NO。后续来源移交不是本轮自动完成的能力。
