# Current Context V5

2026-10-03（Asia/Shanghai）。实现 `BT-V5-AGE-036`，来源与实际接口差别见[审计](../research/BIRDTIE-V5-AGE-036-AUDIT.md)。遵守[认知 ADR](../decisions/ADR-AGENT-COGNITIVE-ARCHITECTURE.md)、[Memory 边界](AGENT-MEMORY-ARCHITECTURE.md)、[Context Graph V4](CONTEXT-GRAPH-V4.md)、[Context Access Policy](AGENT-CONTEXT-ACCESS-POLICY.md)与[066服务端开关](AGENT-FEATURE-FLAGS-V5.md)。本项可调用当前普通本人读取；认知/模型用途仍 Unavailable。

## 当前能力

`apps/api/internal/agentcurrentcontext` 新包提供 `NewService(real pgxpool, actual Controller, server development-session config)`、`ReadOwn(Request)` 和 `RevalidateOwn(PrivateAccess, Snapshot)`。只有当前Person本人、精确Personal Agent与真实native metadata可以读取。JSON不能提供权限事实；不注册HTTP或修改现有直接路径。

| Selection | 真源 | 输出含义 |
| --- | --- | --- |
| CURRENT_CITY_DECLARATION | 当前本人private person_contexts + 原CITY Context / City | 本人声明当前城市，**不是已核验定位/居住事实**；可带本次显式问句 |
| CURRENT_TASK | 当前本人ACTIVE AgentTask + 原typed CITY Context / City | 最近存储的用户问句、closed timePreference与任务城市；城市是任务指定情境，不代表本人实际所在 |
| SELECTED_CITY | 当前published City / active CityContext + 本次已认证本人请求 | 显式选定的搜索城市和当前问句；不创建声明/Task或推定人在该城市 |

保留原Agent / account / task / city / context ID及原服务语义；非Person不能跨入其个人声明。Business继续未激活，Organization/Community数据与个人来源不互相继承。未读取普通Profile、Private Profile、媒体、成员资格、聊天或长期Memory。

## 与长期 Memory 的区别

Snapshot.scope = CURRENT_CONTEXT、purpose = HUMAN_SELF_REVIEW，memoryPromotionAllowed=false、modelAccess=UNAVAILABLE。Source version只是当前native retained row的指纹，不是权限、信心概率、身份认证或长期偏好。

现有Declaration和AgentTask没有持久expires_at。Snapshot最多有效5分钟，并受本次request deadline（最多15分钟）、当前session期限、city原source期限及时间窗口结束共同约束；这些是本次读取的租期/限制，不能称旧Task已有expiry。每次Revalidate使用当前真实源，不延长租期。服务重建、开关换generation或原源变化时重新读取，不恢复旧缓存批准。

“今晚”的窗口使用真实city.time_zone、当地18:00到次日00:00。旧Task的相对日期锚定其真实updated_at，过去的tonight拒绝，不能每天自动解释为今晚；当前显式请求锚定本次数据库时间。today/tomorrow/weekend复用既有闭集timePreference语义。未知时区、服务器Local或未知词拒绝；DST按城市日历AddDate，不拿固定24小时猜日期。

没有Put/Delete Memory、候选接纳或自动升级入口。真实测试保存长期Memory后，短期读取与复核保持其独立version及全部原源行；客户端/模型confirmed不能改变此边界。未来保存长期Memory必须经AGE单独权限和确认流程。

## native current resolver 与撤权

原Authenticate解析会话并刷新idle期限；其余源读取采用ReadCommitted READ ONLY transaction。每次load设置transaction-local UTC，避免连接时区改变JSON时间与source摘要；不会修改全局pool或源表。

最后payload SELECT同时检查 session未撤销/未过期、active Person、exact active Personal Agent、native metadata完整绑定、资源当前所有权与状态、published且未过期城市、active CityContext及当前声明/Task。Task不能通过Person所有权读取组织历史；缺metadata不Ensure重建。未来/无穷核验时间、未来原源时间和未知形状拒绝；合法未核验city来源明确unverified，旧核验时间明确review_needed。

初读与最后payload的source及authority指纹必须相同；并发撤权、源改删/重建、账号/Agent/metadata变化、取消、deadline到期或066票据变化都丢弃payload。线性化点是最后SQL statement snapshot；没有额外dispatch承诺，也不承诺已在网络的数据能被收回。

Task版本调用现有QueryVersion；Declaration/City使用SourceVersion的CreatedAtDigest / UpdatedAtDigest形状与本包明确domain，不扩大旧agentevent闭集。actual created_at / updated_at、原行与PG xmin共同进入opaque snapshot token。xmin不称单调业务revision/CAS/权限，也不证明历史动作；同ID同timestamp重建仍失效。metadata revision仅绑定当前控制状态，不能冒充原Context源版本。

Snapshot以本服务随机HMAC key绑定公开内容、selector、native source/authority摘要及066配置generation。修改问句、到期时间、source/Agent或modelAccess均不能恢复；另一个同人session、另一个服务实例、OFF→ON旧generation拒绝。JSON仅可输出普通本人view，不能构造可恢复Request/Snapshot；无Bearer、原authority、key或Memory正文输出。

## 默认关闭与用途

实际066 Enrichment默认OFF。ON只允许本项普通本人review且仍须全部native SQL检查；不授权认知处理或模型出口。`ReadForCognition` 无条件ErrUnavailable，不接受人类Snapshot作为grant。没有model/provider、视觉、A2A、真实写、预算或模型授权resolver；future AGE033装配和AIR用途边界保持独立。

## 实际证据

最终production源码105基础场景加2个Local时区拒绝场景，共107 PASS，0失败/测试skip，目标vet/build exit0；五源前后SHA稳定。实际自有001–057/3seed库、完整public旧行比较、长期Memory对照、7真实并发屏障、强制不同连接时区及到期/重建证据、历史95/105结果见[正式归档](../testing/evidence/agent-current-context-2026-10-03/README.md)。本项无新DDL，不执行down/reapply；现有迁移仅用于准备隔离验证库，库已清理。全API回归由根代理另记录。

适用[全局UX规则](../ux/GLOBAL-UX-INTERACTION-CONTRACT.md)的06/08/10/11/16，当前仅领域模型/真实native读取证明。未有新Flutter/UI、移动/辅助技术/真实截图或普通路径端到端验收。Windows CGO0无gcc，race detector未运行，不将并发SQL用例称race instrumentation。Closed Pilot / Consumer Beta：NO。

## 2026-10-03 共享065回归后的时钟域修正

根连续执行023/028/070时，current065-full1第一轮7437 Test PASS，第二轮7436 PASS、TestNativeCurrentContextConnectionTimeZoneCanonicalization一个Test FAIL及一个package FAIL。527源码SHA、完整旧public行、vet/build和自有库清理通过。原raw未记录各clock，不声称测得具体主机偏差或确认该次命中的分支。

发现数据库签发的ObservedAt直接与host时钟比较。仅work内控制RevalidateOwn的host clock−1秒、真实native session/source/SQL/HMAC保持的overlay三次拒绝。将ObservedAt未来检查移至当前native SQL加载后，与同数据库clock比较后，同一控制三次通过。Host到期/deadline、PG到期、source未来事实/版本、撤权、HMAC/generation、ctx与原租期不续期均保留，没有epsilon放宽未来用户事实。新十个纯clock case和实际native snapshot/current SQL的不同clock域检查均在完整回归中通过。Overlay不是生产源码或真实权限签发。

修复后，命令 pwsh -NoProfile -File work/v5-age028/root-shared.ps1 -Label current065-full2 -Rounds 3 每轮 **7449 Test PASS、0FAIL/SKIP/packageFAIL**，18无测试包另计；Go vet/build0、528全部API Go/SQL/mod及归档副本一致、fresh001–065/三个开发seed/非空旧up保持、每轮全public完整原行相等、自有DB DROP。完整原始失败、控制、源帧与收据见[根共享065归档](../testing/evidence/agent-city-history-2026-10-03/root-current065-final/root-current065-receipt.json)。本段不增加任务或认知/模型许可，后续015/033/062须新冻结帧。

Closed Pilot / Consumer Beta **NO**；无正式身份、部署、真实授权活动、race instrumentation或生产构建证据。
