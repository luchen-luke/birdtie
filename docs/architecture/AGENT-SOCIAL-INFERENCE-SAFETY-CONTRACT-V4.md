# Personal Agent 社交推断与动作安全合同（V4）

2026-10-02，`BT-V4-SAF-004`。依据 V4 规范、现有 SOC/PRV/AGA 权限边界和 [持续交互规则](../ux/GLOBAL-UX-INTERACTION-CONTRACT.md)。验收终态以 live 队列与本项实际证据为准。

## 当前能力和事实归属

当前 Agent 解析用户明确输入的类别、时间和区域，调用现有领域的只读活动/组织/地点查询。Relationship Context 是有时间范围的事实计数；Opportunity 是授权来源的规则理由。上述条件与计数不是推断兴趣、亲密度、出席或性格，也不是 Agent Memory。

本项收口当前响应与确认生命周期，并建立后续推断候选的默认拒绝纯策略。仓库没有真实 learner、推断 grant resolver、公开推断 API、持久 Memory、模型 provider 或自主动作 executor。纯策略正例使用合成可信事实，只证明合同可判断；不能表示偏好真实、真实同意已取得或公开分享已开放。

## 推断候选的最小合同

`internal/agentruntime/social_inference.go` 只允许非敏感 `ACTIVITY_CATEGORY` 的五个已有类别。未知字段概念、健康、民族、性格、关系强弱、个人精确位置等不在合同范围；不得借普通来源推断敏感属性。

| 来源性质 | 合同意义 | 必要证据 |
| --- | --- | --- |
| EXPLICIT | 本人明确声明 | 当前同 owner 的 USER_STATEMENT、确认、来源版本 |
| RULE_FACT | 来源中明确的规则事实 | 当前 SOCIAL_INTENT 的 EXPLICIT_RULE；不将参与次数解释成喜欢 |
| INFERRED | 待审阅候选 | 至少两个独立来源簇的 AUTHORIZED_OBSERVATION、各自明确的私人分析授权与有效版本 |

候选保存来源引用、类别、版本、期限及性质。INFERRED 的有限分数仅叫 `UNCALIBRATED_SCORE`，不是校准概率或稳定画像；来源数量不能证明真实偏好。当前没有生成或保存此候选的 runtime 路径。

`SocialInferenceFacts` 及会话、Agent、任务、候选、来源、grant 是服务器内部证据；JSON 编解码均拒绝，不能接受模型或客户端自行声明的 verified/consent 布尔值。后续 resolver 必须来自真实权威记录；缺 resolver 时使用零值并拒绝。

## 分开的许可与当前版本批准

每次决策要求当前 Person 会话、活跃 Personal Agent、同一 owner/acting user 的完成任务、任务与候选的有效 revision 和明确用途。

私人审阅与公开分享分别绑定 `REVIEW_SOCIAL_PREFERENCE` / `PUBLISH_SOCIAL_PREFERENCE`、scope、action、request/task/Agent/owner/resource、完整候选及来源版本 digest、会话和期限。公开许可不能继承 Profile 可见性、Context、发现开关、Opportunity 开关或 AGA 协作同意。来源分析授权也不能代替公开分享批准。

grant 必须是独立、由人批准具体版本的服务器事实，revision 为正且仍与当前记录一致。来源撤回/删源、分析撤权、任务或候选变更、账号/会话/Agent 变化、过期均拒绝旧批准。每次权限检查重新读取事实，未来写入事务内仍须重新核验；纯策略的 Allowed 不是缓存的永久许可证。

`HIGH_IMPACT` 及未知动作当前全部拒绝；本项没有批准消费、事务派发或效果账本。未来 AIR 的确认、幂等和结果未知恢复必须实现后，才可另行评估真实动作启用。

## 当前 Agent 响应与直接路径

`agentworkspace.WithContract` 通过 `action_safety.go` 统一投影响应：只提供完成任务、同一主体绑定的中文导航；组织创建入口必须来自当前服务端解析的 owner/admin 菜单，组织工作区还须匹配 organization account。Wire Action 无法创建服务器菜单证据。

报名、发布、邀请、发消息、举报、Block、公开推断或未知 action 不进入 Agent 自动执行链。导航只打开已有页面；直接的人类操作继续由现有领域服务鉴权及确认，不以模型文本或 entry-source 标签代替权限。

Task 和 ResultSet 只返回明确输入的合法筛选值与有效 bounds，移除未知/私密/推断字段及内部比较 ID；采用副本，不改持久化 task/resultIDs，保留查询恢复与连续追问。Agent Task 的列表/单项/POST 响应使用同一投影，恢复实体仍由服务端当前可见性重新过滤。

## 客户端确认和异步生命周期

账号 A→B（即使两者均已登录）、退出和组织切换清空私人 Agent 结果/历史/草稿上下文，递增请求序列使旧响应失效。公开且已审核的 Place 选择可保留；私人选择与确认不能继承。地图实例和 viewport 保留。

联系/组织入口确认、实体详情和 SocialIntent 草稿的请求均绑定当前授权及序列；身份变化关闭旧确认、清除私人内容，迟到响应不能写回新身份页面。生成或预览不提交；当前授权下具体的人类确认只调用现有直接动作。

适用 UX-CHECK：04（跨入口实体/动作）、05（可检查主体与后果）、06（来源/未知/过期）、08（旧批准失效）、10（多主体/迟到响应隔离）、12（退出/取消/重启恢复）、16（隐私安全记录）。14 的真机键盘范围已测，大字体/读屏范围仍待核验；15 的无提示消费者观察未执行。本项不据此宣称全产品可访问性或正式试点验收完成。

## 验收与启用门槛

验收覆盖纯策略来源/版本/期限/撤回/独立用途/敏感属性/JSON 自证拒绝，当前 action 闭集与合法筛选回归，真实隔离 PostgreSQL 匿名/有效/跨主体/撤权恢复，以及客户端迟到响应/旧确认/账号变化测试。还需本轮完整 Go、Flutter、构建、迁移回归和实际 Android 安装记录。

现有中文手动发现、详情、报名/Plans 等路径保留。开发 seed、测试验证码、Debug 安装、纯策略正例和工程 PASS 不代表生产身份、现实活动、模型批准或公开推断运营能力。Closed Pilot 保持 NO，真实 IdP、授权供给、HTTPS、地图配置、值守及 A→H 仍按原门槛验收。
