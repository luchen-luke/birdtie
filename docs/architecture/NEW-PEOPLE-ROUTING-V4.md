# V4 新朋友匹配与邀请合同

2026-10-02，BT-V4-OPP-003。补充 Opportunity Engine v1，沿用 Social Intent、Friend/Tie、Profile、Block 与受众合同。

## 选择与来源

052 增加本人独立的 new-people 开关，默认关闭，不从公开资料、过去申请或 SOC-001 授权回填。开启只是允许以本人主动公开/定向开放的有效找伙伴意图参与规则式匹配，用户可以随时关闭；不使用私聊正文/频次、私密记忆、Person Context 声明、学校、身份、性别、心理属性或定位。

查询由本人明确选一个当前 ACTIVE、未过期 FIND_COMPANION。双方均需 active Person、公开 Profile、开关开启，且双方所选 Intent 在另一方读取时均满足现有 birdtie_social_intent_visible_to。排除本人、双方 Block、active Tie 和未过期 pending 申请。PRIVATE、草稿、过期/取消、无权限/未选择的来源不能参与新朋友路由。不会把公开活动参与者或旧人员 Intent 当默认候选。

## 兼容规则与最小输出

只使用双方明确填入的 category、modality、人数区间以及活动范围/平台约束。category 必须非空且规范化文字相同，模态完全一致，已填写的人数区间需有交集；未知条件不编造成已匹配。没有现成时间窗口，不声称匹配周末/空闲时间。

ONLINE 不要求城市/地点；若一方明确平台，另一方也须明确相同平台。IN_PERSON 任一方选择 Place 时双方必须明确同一个仍发布/有效的 Place，不能用区域回退忽略地点约束；双方均未选 Place 时，需明确相同且仍发布的 CITY Context/LOCAL City 与相同粗区域文字。缺少可核验范围时不按全球 city centre 等词猜地点。HYBRID 同时满足物理与线上约束。Place/City 来自 Intent 明确选择，不读取或自动创建私人 Person Context；粗区域不解释为距离/所在地。平台仅做文字匹配，不返回联系方式或会议链接。

响应只给双方来源 Intent ID、公开 accountId/displayName、类别、模态、兼容条件代码与中文理由；不返回原始标题/简介、区域/平台字符串、Person Context ID、受邀名单、坐标、私聊或好友网络。按人去重，稳定排序，最多 50 位并标明截断，不生成地图 Pin 或亲密评分。source=RULE_BASED，不宣称人格/现实协作质量或真实身份验证。

## API 与中文流程

GET/PUT /v1/me/new-people/consent：本人默认关闭开关，PUT 明确 enabled 必填，变更审计。

GET /v1/me/new-people/intents：本人 FIND_COMPANION 管理记录。POST 同路径只创建 PUBLIC 目标的 DRAFT，输入 title/category/modality/cityId/placeId/areaLabel/onlinePlatform/minParticipants/maxParticipants/expiresAt。cityId 仅为线下/混合明确选定的发布城市，服务端解析平台 CITY Context；不声明居住地。ONLINE 不携带任何物理字段。发布仍使用已有 POST /v1/me/social-intents/{id}/activate confirmed:true，独立中文预览确认，草稿不参与供给。

GET /v1/me/new-people/candidates?sourceIntentId=UUID：本人的有效来源选择；禁止 owner/principal 覆盖。

POST /v1/me/new-people/invitations：sourceIntentId/candidateIntentId/note/confirmed:true，不接受 recipient 覆盖。同一事务重新验证双方有效来源、选择、Profile、可见性、兼容、Block、Tie/待处理申请，再复用现有 friend request 事务内部创建逻辑。邀请与开关/来源取消/Block 通过一致顺序锁和事务边界串行；已关闭或过期来源不因旧结果仍可点击而获准发送。重复申请不重复写 Inbox/Tie，限流保留。接收方必须在既有好友申请流程主动接受；不会自动建立好友或聊天。

中文独立页面管理开关→填明确找伙伴草稿→预览公开并确认→主动查候选→留言/信息分享确认→发送。查看/取消弹窗不写申请。关闭、换来源、注销时立即清空候选，迟到响应不能恢复；失败可重试，真实无供给空态。候选不注入公共地图 people。关闭后停止新发现/邀请，已人工发送的申请保留原接受/拒绝/撤回语义，Block 按现有规则结束待处理申请。

## 验证门槛

纯匹配、真实 SQL/HTTP 受众/默认关闭/撤销/来源变化/组织/Block/Tie/pending/并发、邀请确认与接受不自动聊天、UI 失败/空/迟到响应/发布确认、迁移旧 ID/up/down/reapply、Flutter/Go/本地真机均记录实际证据。不得以合成用户、开发代码/Debug 包或本地运行代替生产/真实试点。
