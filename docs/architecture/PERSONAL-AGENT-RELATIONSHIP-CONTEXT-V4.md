# Personal Agent 关系信号合同

2026-10-02，BT-V4-SOC-001。依据 V4 canonical、Agent 身份规范、Agent Context Access Policy 和 048 共同信息展示合同。

## 授权与范围

关系信号使用权限是本人的独立明确开关，默认关闭，不迁移既有用户授权。仅当前活跃 Person 的本人活跃 Personal Agent 可调用。组织工作台、Organization/Business/Community、匿名和其他账户不能读取；不接受 client owner/principal 参数。设置允许本人在 Agent 停用时关闭。开关变化记录 audit_events，查询响应 no-store，关闭后新读取和旧任务重载立即重新检查授权。

## 事实信号

只选择双方账号活跃、当前 accepted friend request 对应的 active Tie、双向无 Block 的好友。不把聊天请求、同场活动、关注或共同社群推断为好友或 CLOSE。

读取近 30 天本人在当前仍 accepted 的双人对话中发送的人类消息数量与活跃日数；只 SELECT 元数据，不 SELECT 正文、实体卡载荷、位置、私密记忆或语义分类。3 个及以上活跃日为 RECENT_REPEATED，1–2 日为 RECENT，0 日为 NO_RECENT_DIRECT_CHAT。这是记录频次，不代表亲密度、对方意愿或现实中的协作质量。interactionCategories 只有有事实支持的 direct_chat / shared_public_activity。

共同活动需双方 048 shared_activities 均开启、对方公开资料、双方 going、活动公开/已发布/未取消/未过期、城市已发布、来源可见性与主办方 Block 检查；活动开始时间在前后 30 天内。这是共同报名，不声称真实到场。无双方授权不返回活动 ID、标题或数量。最多 50 位好友，按本人对话活跃日/数量排序；每位最多 10 个共同活动，超限明确 truncated，不能声称完整排名。

## 数据路径

051 仅增加本人开关表，不存聚合副本。GET/PUT /v1/me/agent-relationship-consent；GET /v1/me/agent-relationship-context。严格会话解析，不接受组织工作台或 owner 查询参数。

Agent Workspace 明确“我的关系信号 / 我最近常和谁联系”等请求路由 PERSONAL_RELATIONSHIP_CONTEXT 工具。实际检索返回 relationshipContext，独立于公共 people/resultSet/entities，绝不生成地图 Pin。回答说明来源与条数并提供中文“查看关系信号”动作，审阅页展示本人信号。任务只保存查询与一般说明，不缓存好友名单、聚合或私聊正文；重载按当前权限重新检索。关系检索不进行 LLM 推断，不支持查询第三方关系。

## 验收门槛

策略和隔离数据库验证默认关闭、撤销、匿名/组织/失效 Agent、无 Tie/移除/Block/停用账号、范围与共享活动授权、无正文/无 Pin、旧任务重载和持久化设置。中文设置与审阅页验证失败、空态、注销和迟到响应。迁移旧 ID/up/down/reapply、Flutter/Go 回归与本地真机场景；开发样例不能作为正式试点。
