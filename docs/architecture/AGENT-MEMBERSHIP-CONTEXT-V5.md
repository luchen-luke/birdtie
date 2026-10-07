# Membership Context V5

2026-10-03。BT-V5-AGE-031同时覆盖原AGE031 CommunityMembershipSignal与AGE032 OrganizationMembership，[实际审计](../research/BIRDTIE-V5-AGE-031-AUDIT.md)。遵守认知ADR、Memory边界、原Context Access和066服务端开关。

## 可调用能力

apps/api/internal/agentmembershipsignal提供NewService(real pgxpool, actual Controller, server development-session config)、ReadOwnMembershipContext(Request)、RevalidateOwn(PrivateAccess, Snapshot)。真实native source读取实际调用assembleContext，返回Person本人Personal Agent当前的成员Context Evidence；不是fake合同或只注册事件。

| Kind | 当前真源 | 可输出角色 | 明确含义 |
| --- | --- | --- | --- |
| COMMUNITY_MEMBERSHIP | 本人community_memberships + 当前Community/owner/原creator/必要City | member/admin/owner | 当前已加入关系，不推断摄影兴趣；Community不是Agent |
| ORGANIZATION_MEMBERSHIP | 本人organization_memberships + 当前Organization/account/owner | member/moderator/admin/owner | 当前机构角色；resourceId与principalAccountId分别保留，不能混为组织Agent/private用途授权 |

每次精确membership ID读取一项Context Evidence。无列表入口或暗中遍历他人成员；同UUID在不同kind分属各真表/版本domain。pending/invited、left/rejected/removed、未知角色不能作当前positive。private社群/组织仍可供本人有效成员review，不允许公开或跨人继承。

Snapshot.schemaVersion=agent-membership-context-v1、scope=PERSONAL_MEMBERSHIP_CONTEXT、purpose=HUMAN_SELF_REVIEW。Evidence包含membership/resource ID、机构account principal、原label/角色/status、source版本与native updatedAt；interestInferred / identityVerified / friendEstablished / memoryPromotionAllowed全部false，modelAccess=UNAVAILABLE。不是持久005 Memory Evidence或统一033 Builder，不保存Memory、不自动维护兴趣/敏感身份或好友关系。

## 实时权限和有效期

Request仅服务端PrivateAccess/typed PersonalAgent/kind/ID/deadline；JSON不能还原Request或Snapshot。实际Authenticate、activePerson、exact active PersonalAgent、053metadata owner/version、资源状态/所有权/Block/真实期限必须当前有效。Organization/Business/Community主体不能读取Person来源；Business保持未激活。原成员权限/工作台/普通Profile授权不能升级成认知、匹配、跨Agent或模型出口许可。

Community检查published/active非hidden、当前唯一active membership owner和原creator active及双方Block、源expiry与finite非未来核验日期；关联City的published/expiry与CityContext有效。原Transfer只改membership role，创建者ID保持，本项分别绑定两者。Organization检查active机构/其真实account、当前本人active成员、活跃owner与相关Block，保持原四角色；没有自造Organization expiry或验证身份。

两个原membership都没有持久expiry/业务revision。本次Snapshot最多5分钟，受本次request deadline（最多15分钟）、实际session和源expiry约束。相对租期不提升原事实为永久许可。Revalidate读取当前真源且不延长期限；退出后重新加入、撤销再接纳必须重新获取。

每次load为ReadCommitted READ ONLY transaction、transaction-local UTC；最后payload SELECT是当前授权/source线性化点。初读与final源及authority必须一致；metadata/Agent/账号、角色/源/owner、撤权、取消/deadline或066 generation变化丢弃payload。Authenticate会刷新原session idle；“只读”限定源领域，不声称所有SQL无写入。没有网络后追溯回收承诺。

真实row/created_at/updated_at/PG xmin组成opaque source token。Community复用agentevent.SnapshotVersion；Organization使用本包明确typed domain与现SourceVersion形状，不冒充Community event或扩旧registry。token不等于CAS、revision、历史加入行为、身份或grant。服务实例随机HMAC绑定selector、当前session/exactAgent/native metadata/源/066generation，跨session或实例、篡改内容/期限/model字段、OFF→ON均拒绝旧Snapshot。

## 开关、模型及验证

066 Enrichment默认OFF，ON只允许本项普通本人读取，仍须全部当前SQL检查。ReadForCognition始终agentcognitive.ErrUnavailable；没有actual cognitive/current-purpose/model egress授权resolver，不能用本人事实、内部字段或ON flag伪造模型许可。普通Member不是现实Friend/Tie/身份/长期偏好，同组织不代表允许读取他人资料或自动匹配。

实际七角色、两类Context组装、旧Memory/原源不变、撤权/改角色/Block/时间/重建、16真实并发屏障、连接时区等证据见[归档](../testing/evidence/agent-membership-signal-2026-10-03/README.md)，以最终production source manifest为准；历史RED及144结果保留。无新DDL/旧pkg/API修改；root独立核验及全Go另记录。

最终production154 PASS/0fail/0skip，目标vet/build exit0、五source SHA稳定、旧public完整行相同、ownedDB已DROP；与最终staging154五源及正式快照一致。默认OFF、同UUIDtyped域、source lease、重加入/重新接纳、owner时间和同内容变化均实际验证。

后续共同059 round2暴露本包跨墙钟复核ErrExpired；上述154为历史。当前服务统一以私有真实pgx数据库clock_timestamp检查deadline/ObservedAt未来/ExpiresAt到期，final返回前再次核时和ctx/ticket，不比较PG观察时间与host time.Now。严格future和expiry边界无容忍，未安装caller/fake clock或改全局时钟。新增真实200次复核与明确模拟host落后的负控；当前五源及166结果见正式source-manifest-clockfix.json，原154源码/manifest保留。共同root全API必须针对最新hash重跑，不把第一旧轮5472或后续3fail当新通过。

UX-CHECK-06/08/10/11/16为领域核验。无新UI/Settings/路由/普通路径端到端/真机/辅助技术截图或真实CSSA试点；无race detector结果、部署/运营、认知/model/provider/自动写/视觉/A2A。Closed Pilot / Consumer Beta NO。
