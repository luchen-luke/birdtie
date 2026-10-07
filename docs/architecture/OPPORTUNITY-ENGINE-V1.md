# Birdtie V4 机会候选引擎 v1

日期：2026-10-01。对应 `BT-V4-OPP-001`。这是仓库内的**规则式候选**，不代表通用 AI 推理、真人匹配、活动席位或真实试点上线。

`GET /v1/me/opportunities` 只接受有效 Person 会话，从其本人未到期、已激活的 `FIND_ACTIVITY` 社交 Intent 开始。当前只匹配 `IN_PERSON`，因为现有 Activity 没有经核验的线上参与模态；`ONLINE`/`HYBRID` 返回空，不给城市或会议链接造假。Intent 的类别、明确 Place ID、粗区域文字和可选 CITY Context 均为硬约束；粗区域只在审核 Place 名称/地址中作文字匹配，不能解释为距离或定位。没有明确约束的全球查询不生成候选。

供给只来自已发布、未过期、未取消且未结束的真实 Activity，必须关联仍公开有效的 Place。读取复用 Activity 原有受众、Block 和主办方权限过滤；新查询不直接读取私密 Moment、私信正文或参与者名单。显式 Person Context 只作为本人选择的城市信号，不证明所在地。`BT-V4-OPP-002` 对有权查看的候选按四级排序：**活跃且未屏蔽的本人好友主办** → **本人当前已加入的活跃 Community 主办** → **其他公开活动** → **其他有权查看的活动**。同级按开始时间和稳定 ID。离开 Community、移除/屏蔽 Tie 后，相关优先级及理由立即消失。Community 关系只描述本人会员资格，不揭示其他成员；“朋友的朋友”、亲密度和共同出席均没有已授权模型，当前不计算。

响应每条候选含确定性 `intentId:activityId`、`ACTIVITY`/`PLACE` 稳定实体引用、中文简短理由与机器可读 `reasonCodes`、`OPEN_ACTIVITY` 详情动作、开始时间和 `ruleVersion=activity-place-v2`、`routeTier`。API 标记 `source=RULE_BASED`。它不自动 RSVP、邀请、发消息、改 Intent 状态或宣称转化。最多返回 50 条。没有符合条件的已授权供给时返回空数组。Intent 的 `audience` 控制**谁可读取该 Intent**，不赋予别人查看 Activity 或本人关系的权限；本 API 始终只返回本人结果。

验证使用固定时钟纯函数 fixture 和一次性 PostgreSQL 001–039：真实 seed Activity/Place、本人私有激活 Intent 能生成引用；匿名/组织账号不能读取，邀请制 Activity、隐藏 Place 不成为候选；真实申请/接受的 Tie 与主动加入 Community 可作为排序输入，Block/退出后撤销；旧 Place/Activity/Intent ID 保持。修复了原社交 Intent 约束对有效 Place UUID 少匹配一段的问题，并补有效/截断 UUID 回归。尚无 Flutter 消费页、生产数据或真实用户 A→H 证据，Social Alpha Gate 仍为 NO。

## 2026-10-02：解释消费合同（BT-V4-OPP-004）

上述“尚无 Flutter 消费页”是 OPP-001 时的历史状态。本项在同一 canonical 增量接入本人机会消费：GET /v1/me/opportunities 响应仍为 data 数组、source=RULE_BASED、ruleVersion=activity-place-v2；每条保留旧字段，新增当前有权查看的 Activity `title` 与 Place `placeName`，不新增原始私人画像/情境、成员名单、联系方式或坐标。列表与详情动作不会报名、邀请、发送消息或改变地图；详情必须通过现有 Activity 授权接口重新读取。

理由 UI 仅将明确允许的 reasonCodes 投影为简短中文，不按 routeTier、主办者名字或原始 reason 文本猜理由。INTENT_CATEGORY / INTENT_PLACE / PLACE_TEXT_AREA / INTENT_CITY_CONTEXT / DECLARED_CITY_CONTEXT 分别解释本人明确类别/地点、审核地点文字与选择的城市范围，不能说定位/距离/居住地。TIE_ORGANIZER 只能表示当前有效好友主办，必须双方活跃、有效已接受 friend 请求/active Tie 且无 Block；没有好友兴趣或报名/参加证据，不写“好友也喜欢/会来”。JOINED_COMMUNITY / FOLLOWED_ORGANIZER 仅解释本人有效成员/关注关系，不透露其他成员。明确 typed ORGANIZATION Organizer 可以产生 ORGANIZATION_ACTIVITY（组织主办的活动），不能从 HostLabel/CSSA 名称猜身份，也不表示组织已核验；Business/Community 保持各自语义。未知代码跳过、重复去重、有限显示；完全无已知代码显示中性“依据当前有权查看的信息进行基础规则匹配”。

独立中文“为你找到的活动”页默认只读实时本人机会；设置与 Now 提供入口，名单不进入公共地图。加载/空/错误可重试；未知 source/version/错误实体动作不能当成功或触发自动操作。刷新、关闭来源、身份切换与注销均清空旧候选，迟到响应不能恢复。`new_people_page` 复用同一允许列表投影，以既有 v1 来源边界为准；不能把活动的好友/关注理由挪用于新朋友候选。

本人找活动来源沿既有 PRIVATE FIND_ACTIVITY 草稿与确认激活接口。页面允许选择自己 PRIVATE、未过期 IN_PERSON 的 DRAFT，明确预览“仅用于你找活动，不会公开”，再 confirmed:true 激活；PUBLIC 新朋友的独立公开确认不受此动作影响。草稿由既有 Now 活动查询“保存意图草稿”创建，页面给出实际路径；不把查询自动发布、不将 PRIVATE 改 PUBLIC。首次无符合供给显示真空态，提供明确来源管理；暂无已核验线上活动供给，ONLINE/HYBRID 不伪造推荐。

本项验证 canonical/实际字段投影、未知/恶意原始理由不显示、身份/好友/社群/关注撤销、组织与Business区别、PRIVATE 激活确认及取消无副作用、中文页面与详情/迟到响应、整库 Go/Flutter/构建和本地真机。2026-10-02 已实际通过 001–052/三个 seed/full Go/旧 ID/down-reapply、Go vet/build、Flutter analyze/189 tests/APK 与 Android 16 私人草稿→确认启用→3 条机会/理由→授权详情→取消→App/API 重启；详见 [消费验收证据](../testing/evidence/opportunity-reasons-2026-10-02/README.md)。live 队列以此证据记状态。本地合成不等于真实试点；新朋友路由见 [新朋友合同](NEW-PEOPLE-ROUTING-V4.md)，正式试点 Gate 仍为 NO。


## 2026-10-04：MAP-001 本人 PRIVATE 叠层

原名单「不进入公共地图」继续有效。本轮仅增加本人显式开启、默认关闭的 PRIVATE 地图叠层：原本人规则候选与同一快照仍 PUBLIC 的 Activity/Place 相交后展示原 Candidate ID，点击重新读取原 Activity 详情。公共 feed 从不包含 Opportunity；匿名、组织工作身份无法读取；不公开 Intent 标题、原因、本人关系、私人内容或精确位置。ONLINE/无明确公开 point 不补 Pin，不自动 RSVP/邀请/发消息。这里是 human 本人只读能力，不扩大机器用途或模型出口许可。
