# ONLINE Social Opportunities V4

任务：BT-V4-NOW-005。2026-10-04。本页是该项人类主动线上发现的 canonical，不替代 V4 产品总规范、机器上下文用途许可或发布门槛。

## 用户路径与复用

用户从 Now 的独立线上发现入口选择本人当前 ACTIVE、ONLINE、未到期的原 SocialIntent。没有城市、地图结果、Place 或定位许可也可读取。页面先解释实际规则，再显示机会卡。来源原 ID 持久；不建立新的 Intent、Task、Run、消息或机会账本。

- REUSE：social_intents 与公开/好友/Community 受众合同、真正 accepted FriendTie、Community membership、已发布 ONLINE Activity、当前 Session/Person/Personal Agent、原好友聊天与活动/Community 详情。
- EXTEND：单条最终 SQL 的有限人类投影、HMAC 封装与响应前 source/version 当前复核。
- NEW：onlinesocialopportunity 闭合 DTO 与只读 API、线上发现 controller/page。它们不是模型许可或通用 Agent 能力。

## 来源与权限

本人目标必须真实归属当前 Person，ONLINE/ACTIVE 且 finite expiresAt。可选原 ONLINE Context 必须真实匹配；不造城市或 Context ID。Options 最多20个，来源最多30条，超出用 truncated 明示。

1. SOCIAL_INTENT：只读取当前 PUBLIC / FRIENDS / COMMUNITY 的其他人 ACTIVE ONLINE 来源。复用原受众 ACL，并强化真正 accepted 请求+Tie；Community 要求双方当前会员及有效已发布 Community。PUBLIC 复用真实公开 Profile。PRIVATE、仅邀请、草稿、已撤回、过期、inactive creator、任意一方屏蔽都不投影。
2. ACTIVITY：真正 ONLINE、已发布、未取消、未过期 Activity；真实 City 仍有效。请求人无需当前/目的 City。公开活动或本人当前 Community member 可见的 organizer_members 活动可读取；不借组织管理权限放大到其他私密活动。普通 PERSON、Organization、verified Business、Community organizer 当前性分别重查。
3. relation 是实际当前 accepted Tie / membership / public 读取路线；不推断共同兴趣、朋友关系或愿意私信。多个关系时优先 FRIEND，其次 COMMUNITY，再 PUBLIC。

本轮匹配限于已存 Intent 的类别或标题字面词。页面明确这是规则匹配，不称自然语言理解、全面满足人数/时间窗口/平台偏好或双方确认。结果不是参加事实、共同关系批准或自动联系。

## Wire

- GET /v1/me/online-social-opportunities/options
- GET /v1/me/online-social-opportunities/{intentID}

两路均拒绝 body、RawQuery、ForceQuery、组织 workspace header，个人当前认证；no-store。root 负责注册，Store 从原 catalog type assertion 取得，无新增 server ledger/field。

data 为 online-social-opportunities-v1：ownerId（仅本人）、intentId、observedAt、validUntil、truncated、intents、items。item 只含组合展示 ID、标题、原 sourceRef（SOCIAL_INTENT/ACTIVITY +原 ID）、当前 relation、sourceVersion、expiresAt、实际 tieId 或 communityId。无对端私人 account/profile、constraints、Memory、正文、坐标、联系方式、Token、Proof/Seal。

validUntil 不超过2分钟及本人 Intent、实际来源、Session/City/Community 适用期限。Dart 严格日期校验支持 RFC3339 Z/明确±HH:MM，并归一 UTC；非法日历、无时区、错误闭合字段、重复/非原 ID、假地图点拒绝。内部 Proof含源行 xmin/主体/关系/Session 等，seal 绑定本人当前 Session。HTTP先编码，再 native Revalidate，等待后最后 SQL 使用同一 clock_timestamp 检查所有源，不在其后再等待另一次认证资源。

## 动作、未知与生命周期

每次动作先 GET 原目标 Intent 的当前机会；旧源 ID/version/Tie/Community 不匹配则仅显示刷新结果，不执行旧动作。查看意图只显示同一最小来源 DTO；使用内层 Navigator 的 dialog，避免逃出身份边界。活动与 Community 使用原详情，朋友按钮在用户明确点击后仅调用原 StartFriendConversation；无自动消息、邀请、加入、RSVP 或授权。

捕获 auth token +本人 account ID +组织 workspace 及 transport。同 key endpoint/client/getter/auth/listener 重绑、A→B→A、跨本人/组织、迟到回包使旧页面/全部内层路线永久退休；不靠返回旧 token 复活。借用 client 不关闭。来源有限 lease 自然结束时缓存失效；正在展开的旧弹窗/路线退休，重新打开当前入口才能再次核实。普通退出/关闭不写领域数据；未知聊天 POST 不自动重发，只提示从消息核实。

## 验收与边界

适用 UX-CHECK-01/02/04/05/06/07/08/09/10/11/12/13/14/16。现有原普通组件和直接路径保留。320宽、3倍字、260键盘、长中文标题滚动、动作至少48dp已有 widget 正例；不将它们描述为 TalkBack/真人易用性证据。

实际 worker native10：32 PASS，0 FAIL/SKIP/pkgFail，test/vet/build=0，839观察源稳定，原 public 全行不变，自有库已 DROP。覆盖三独立来源路线、两城市无Place Activity、请求人零Context/City、撤权/过期/双向屏蔽/Agent+源 ABA、6 pool/table/Session 真等待、非会员公开 Community Activity 的真实期限，以及两个真实 HTTP 子进程退出重启后同一原 Intent/Activity ID 重开。native9 对 Community 到期 lease 延迟有真实 RED，SQL 已加入该公开 Community 的期限，保留失败原帧。它们使用 LOCAL_SYNTHETIC 数据；不是现实用户或正式试点。

Flutter client-target5：24功能测试 PASS；client-analyze7：6文件无问题。已保留初次失败及修复链，具体命令和 SHA 在专属 evidence/work。整仓构建、root独立核证及真机由根代理执行，本页不提前宣称通过。真实双人、真实线上组织供给、辅助技术、模型/外部部署 NOT_RUN。Closed Pilot / Consumer Beta = NO。
