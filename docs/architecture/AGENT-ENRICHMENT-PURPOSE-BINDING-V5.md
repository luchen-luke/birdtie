# 当前 Moment 本地分析用途许可

AIR015 原任务内增量。MOMENT_LOCAL_ANALYSIS 仅当前本人、原Session、exact PersonalAgent、ACTIVE Task 和明确选择的本人 private draft Moment title/body。人工预览内容供本人检查，不是机器控制；原 consent_grants 是唯一状态/revision/expiry/revoked 真源。079 preview 的有限显示期限与 immutable selected-source frame 不构成第二许可；binding 只有 grant_id/preview_id。

预览与原ID批准需要当前native源。分析解析再次验证同原Session稳定身份、账号/Agent/metadata native版本、Task版本/query摘要、Moment revision/xmin及选择正文摘要。正常Authenticate idle延长不改变稳定身份，但原显示有效上界不延长。选择仅title/body，不读媒体、位置、关系或历史扫描。输出不是已核实偏好，不能用于模型出口、候选保留、Memory提升或消息。

GET receipt 不返正文。当前本人新Session可核实/撤回原许可，却不能消费原Session的分析批准。明确撤回单调 revision+1，禁止恢复/续期/改用途；重复批准回原grant且不续命。已撤销/过期不能再批准。unknown先原previewID/GrantID读取，不新建grant盲重发。

同事务取得实际读写关系锁与行锁后获取PGclock；写许可/绑定/审计后再次原生capture，所有期限/身份/源/许可必须当前有效才commit。未迁移或guard缺失/禁用Unavailable零许可写。无使用down/reapply保留旧完整行/catalog；任何批准或撤销binding历史阻止down，旧purpose语义保持。

这是当前许可第一片，CandidateSubmitter与064 effect writer仍不可用。真正候选保留需独立明确用途与同事务writer/checkpoint后才可能解除完整AIR015 PARTIAL。Closed Pilot与Consumer Beta仍NO。

## 当前实现与验证边界

用途许可接口在 agentenrichmentpurpose/model.go，原生Postgres实现agent_enrichment_purpose.go，严格HTTP agent_enrichment_purpose.go，schema079。创建预览端口仅CURRENT_REVIEW；批准后原previewID GET提供RECEIPT_ONLY，不回正文，新Session不能由receipt继承分析许可。

原generic consents只处理profile_view/read并拒额外purpose字段，不是本用途批准路径。稳定Session摘要记录原Session ID、createdAt、authmethod、absoluteExpiry与tokenDigest，正常idle刷新不误用Session xmin；当前有效期每次同PGclock查。直接SQL同row撤销再清除无合法原生产API，未声称可抵御任意特权恢复或DDL。原grant用途单调不可恢复/续期由079guard限制。

34原生/纯/registered HTTP测试通过，旧public/七类catalog/unused down-reapply/used down原子拒绝与ownedDROP真实保存于work/v5-air015-purpose/native3。raw字段/body只用于明确本人human review和已批准server-only本地读取；没有实际模型分析、候选保留或Memory写入能力。后续AIR015必须继续原任务内具体保留用途与真实atomic业务效果，不能将本文授权第一片称完整DONE。


## AGE049 清单与撤回消费入口（2026-10-07，原任务增量）

原 079 有 durable 本人许可，但旧 Multi 页面重开不保存已完成许可 ID，也没有本人清单。新增 `InventoryStore` 与实际 `GET /v1/me/agent-enrichment-purpose/grants`，从原许可/binding/preview 同体查询本人、当前 Personal Agent 的 `MOMENT_LOCAL_ANALYSIS` 元数据；授权真源仍仅原 consent_grants。原 by-ID API、Grant decoder、批准/分析/保留/撤回写者字节保持。

- 同 SQL statement PG 读取钟、最终原 owner/Agent/Session 与 clock 再验；最多 30 秒读取期限不延长原许可。原生 SQL/并发尚未执行，现证据是直接 helper pgx spies 与注册 handler 合成端口。
- 未撤回且在读取钟尚未到期的许可优先，再组内 created/id，最多 50 与明确 truncated；历史不能永久遮住更早的有效许可。排序仅元数据状态，不导出 canAnalyze，不代表来源、Task、Session仍可分析；实际分析保持原 native 最终收口。
- 设置中文直接入口复用原 personal route/source epoch/current、现有 Theme/控件。具体版本检查与原 expectedRevision 撤回相连；不把清单放回分析批准缓存或恢复/续期旧许可。
- 写前使用原 bodyless recovery journal。换身份、transport/base/widget/入口寿命、ABA、迟到响应关闭旧批准/晚 UI；已发请求不能被撤回或宣称回滚。UNKNOWN/409 仅原 ID GET，当前同值、截断或缺项不证明 NO_EFFECT。读到当前已撤回时可结束该 revocation 本机引用，但明确不是因果回执。
- 再次允许走原当前来源选择/新预览/明确批准；不添加默认 AI/学习总开关或新的 consent authority。撤回不删除已消费结果或独立记忆。

UX-CHECK-01/05/06/07/08/09/10/12/13/14 在本切片做相关单位覆盖；大字号仅 widget 证据，真实辅助技术/手机/OS 存储/跨进程/PG/生产认证/模型/视觉/完整发布全部 NOT_RUN。当前许可清单/撤回可达代码不等于 AGE049 五族整体完成，Personalization 总门禁和字段受众 UI 仍是原任务缺口。轻量冻结、原字节逆证、实际命令及失败在 `docs/testing/evidence/enrichment-privacy-control-2026-10-07/freeze11/`。


## AGE049 分项控制说明的前瞻纠正（2026-10-07）

上一节“Personalization 总门禁和字段受众 UI 仍是原任务缺口”保留为当时的记录，不作为当前状态。原 AGE049 明确要求 Public profile、Agent private profile、Memory、Personalization、Agent learning 的相关控制，没有要求新增统一的个性化/学习 Boolean。

当前设置已提供各项资料可见范围（十一字段、五种受众）、记忆管理与待确认记忆、本地分析许可，以及本次登录会话的任务资料许可入口。智能体资料页准确指向这些分项控制；修改社交等领域偏好不授予模型读取、长期保留或自动执行权限，使用资料仍需当前用途的具体批准。资料受众不是模型用途授权，许可元数据清单也不证明来源仍可读取。本次未新增总开关、授权创建流程或跨会话权限，不宣称模型训练或自动学习已启用。

本次只修正智能体资料页一条用户说明并追加此纠正。一条真实页面 widget 用例在原说明下失败，替换后通过；320×640 已绘制说明、48dp 主要动作可命中、5 GET/0 写。生产身份、原生许可事务、真机与辅助技术、模型和发布未运行；AGE049 整体保持 PARTIAL。相关原始结果与原字节保留证明在 `docs/testing/evidence/agent-profile-privacy-copy-2026-10-07/`。
