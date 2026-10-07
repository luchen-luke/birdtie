# BT-V5-AGE-024 来源审计与验收记录

2026-10-03。本轮只在 root 登记的独占新包、本文、唯一架构规范、专属证据及 work 目录写入，live 队列和共用报告由 root 核验证据后更新。保留既有未提交内容；未修改原生 RSVP、agentevent、Memory/Evidence、PostgreSQL Store、DDL、HTTP 或 main。

## 原要求与去重

源要求为 `docs/product/BT-V5-AGE-AGENT-ENRICHMENT-EPIC.md:999` 的 AGE-024：join activity 可产生 participation signal，但单次不能得出稳定兴趣。复用原 RSVP 稳定 ID 与真实动作、064 当前源版本、005 本人手动 Evidence、actorref 主体；未另造 Participation 表、总线或 Memory 账本。架构目录未发现旧独立 Participation Signal canonical，新增 [唯一契约](../architecture/AGENT-PARTICIPATION-SIGNAL-V5.md)。

| 审计项 | 开发前实际范围 | 本轮结果 |
| --- | --- | --- |
| going/cancelled 持久来源 | REAL：原生 Join/Cancel；改期/取消既有 | 保留、实际复验 |
| pending | PARTIAL：原生表保留形状；Join 当前只写 going | 独立辨认 pending；SQL 形状验证，不声称审批完成 |
| 064 活动事件 | REAL：当前 going/cancelled 元数据 producer/revalidator | 复用版本/TTL思路，不改全局事件目录或历史语义 |
| 独立参与信号 | NOT_IMPLEMENTED | REAL 本地窄只读 Reader.Collect/Revalidate |
| 005 手动 Evidence | REAL：本人 EXPLICIT MANUAL_REFERENCE | 未复制、未自动写入 |
| 参与候选 consumer / 自动记忆 | NOT_IMPLEMENTED | 仍 NOT_IMPLEMENTED；无真实用途许可/接纳/运行端口 |
| WAITLIST、到场/完成事实 | 无权威原生来源 | Unavailable / Unknown；不从结束、going、Moment、Plans推断 |
| 单次稳定兴趣 | 原文禁止 | UNKNOWN；没有打分、画像或Memory副作用 |

队列 goal 对候选消费的差距描述不能自行变成已经开放的消费者；原 source acceptance 按实际可调用信号核验。CODE_LOCAL 不等于功能运行接线、生产登录或正式试点。

## 最小完整实现

新增四个 Go 文件，生产目录 `apps/api/internal/agentparticipationsignal`：signal.go 为闭集元数据合同/结构校验，reader.go 为当前 PostgreSQL只读来源与重校验，signal_test.go、reader_integration_test.go 为纯结构及真实原生来源正负验证。

实际 Session→active Person→exact active Personal Agent→Profile metadata→本人 Participation 在每次当前 SQL 中检查。going/pending 再查当前 Activity/City/organizer/ACL/双向 block/时间，绑定真实 revision 和最小生命周期 digest；cancelled 仅本人失效控制源，不携带旧活动目标。两次来源读取保证迟到变更被拒；15分钟租期锚定实际 Participation 更新，不续期。该桥没有源正文、模型/记忆许可、消费者或外部效果。

## 实际失败与修复

1. `staging-round1`：原生 accounts 没有默认 ID，初次 fixture INSERT 缺 ID 导致 SQL23502，清理空 ID 也触发22P02。修复为每个合成账号/Agent显式 gen_random_uuid，清理仅精确合法自有 ID；旧 public 行始终完整保留。原始失败保留，未计为 PG 通过。
2. `staging-round2`：46 PASS，真实 native Join/Cancel、身份/活动边界与时区/TTL/并发通过。早期 runner 模式误叫 staging-overlay，实际独立 work module；原结果保留，随后准确改为 staging-module，生产结果使用 production-source。
3. `staging-round3`：新增负例试图删除唯一 organizer，原生约束 P0001 拒绝；测试中闭包绑定父 t 的 Fatal 引起失败。没有删除触发器或绕过 DDL。改为实际约束拒绝及原状态仍有效的独立测试，子用例使用其自身 t；原始红结果保留。
4. `staging-round4`：56 PASS；新增 host suspended、反向 block/private、相同 clock 状态变更和最终 SQL 期间过期/撤权/取消/改期/metadata删除/context取消。
5. `staging-round5`：57 PASS；真实 `UpdateSocialActivity` 改期增加实际 revision，旧 receipt 被拒；重新构造 Reader 从同原生持久源复读相同 ID/TTL。不是数据库进程重启或 AgentRun 历史验收。
6. 生产追加非法时间结构拒绝，生产round1/2各58 PASS、35实际PG+23纯检查。但自审发现 pool.Begin 会继承连接默认REPEATABLE READ，不能把这两轮默认池证明扩到任意受托连接配置，故撤回初次freeze并继续修复。
7. `staging-isolation-red`：生产目录出现后独立work module同名导致 ambiguous import 编译失败，不能称作真实PG红测试。将work module改为独立participationsignalstaging名称后，`staging-isolation-red2`实际默认REPEATABLE READ池上的7个final SQL子例失败（9个fail事件含父/包），完整public行保留；撤权、session/source到期、取消、改期、主办方失效、metadata删除确实会读旧快照。修复为显式BeginTx READ COMMITTED+READ ONLY，保留所有红结果并重新用实际生产源码验证与四SHA归档。没有沿用staging或前一个任务的全仓测试数量。

## 验证范围和保留条件

真实隔离058库运行 native CreateSocialDraft/Publish/Join/Cancel/UpdateSocialActivity。pending 只通过原生合法 SQL 状态形状核验；非本轮真实批准流程。验证未知/篡改/非本人/匿名/过期/撤销/错主体/Agent退休/metadata缺失、活动不可见取消结束、主办方失效、block、原生删除、同clock变化、重建metadata、UTC与16并发重试、稳定 ID/不续TTL/无正文及无Memory写。

最终 SQL barrier 确认两次读取间的实际数据库撤销、session过期、source过期、原生取消、改期、主办方失效、metadata删除与 context 取消不会返回旧载荷。读取后的未来撤权仍要求消费者再核验，不声称永久锁住外部世界。

无新 DDL；runner fresh001–058且比较所有 public 表完整 JSON 行及前后源 SHA，DROP 自有随机库。未改全局出网开关。GoCGO0环境未运行 race；并发测试是真实16调用，但不能称 race detector 验证。Flutter、UI、真机、辅助技术、真实 IdP、部署调度、真实活动与正式 A→H 未运行。

UX-CHECK-06：未知及当前事实分开；08/10：版本/主体与迟到边界；09：重试稳定且不续租；16：最小元数据无正文/位置。其他 UI 截图/可用性条目本轮无界面修改，不计通过。

Closed Pilot Ready：**NO**。实际候选消费者、机器用途许可与审批、模型接线、到场来源和原真实试点外部条件仍需后续独立任务；本轮代码检查不能解除这些门槛。
