# Birdtie V4 独立 Business 主体

日期：2026-10-01。对应 `BT-V4-BIZ-001`；本文件说明仓库内的数据和服务端授权，不表示任何真实商家或经营权已经核验。

041 增量迁移新增 `accounts.account_type='business'`、独立 `businesses`、真人 `business_memberships` 和可多场地的 `business_venue_relations`。Business 不是 Person，也不是 Place/Venue；Business Account 不自动得到 Personal Agent 或 Organization Agent。旧 `organizations.organization_type='business'/'venue'` 行、Organization Agent、旧活动主办方和原有 ID 都保持原样，不做批量映射。Business Agent 由后续任务在经营权与工具边界齐备时接入。

Business 的 claim 默认 `pending`；资料可处于活跃、隐藏或关闭状态。Venue 经营关系默认 `pending`，需独立审核证据才可变为 `verified`。Business 主办活动要求活跃、已核验 Business principal；真人会话必须是该 Business 活跃 Owner/Admin。若活动指定 Place，还须有该 Place 当前有效 Venue 的已核验经营关系。数据库四类 organizer 恰好一个；Business 记录使用 Business Account 为 Activity host、真人为 creator，旧 Organization `organization_id` 继续为空。公开活动读取要求 Business 与场地关系仍有效，经营关系撤销后立即不可见。会员限定/邀请活动也使用 Business 成员及受邀人原有权限规则。

本阶段的 Business 与场地核验状态仅能通过受控数据库迁移/开发验收记录构造；**没有真实商家自助注册、claim 审核 API 或 Console**。这些属于 `BT-V4-BIZ-003`，因此不可把合成 verified 行当成真实商家声明。Venue 原有可选 Organization 经营者引用仍是兼容资料，不自动转换为 Business claim。真实商家需单独核验经营权、人员角色和撤销流程。

`BT-V4-ACTY-001` 增加只读 `GET /v1/me/activity-organizers/businesses`，仅向有效 Person 会话返回其活跃 Owner/Admin 身份下仍有效、已核验的 Business `{type,id,name}`。活动编辑页把它加入四类主办方选项；最终创建/发布仍由服务端及数据库重复鉴权，选项本身不是权限。旧服务端没有此只读路由时，客户端保留 Person/Community/Organization 编辑能力。此入口不等于商家 claim 或管理 Console。

隔离库验证旧账户、Agent、Organization、Place、Activity、Intent 和 organizer 关系前后相同；合成 Business Account 无 Person Agent、Person 不能冒充 Business principal、第二活跃 Owner 被约束拒绝。HTTP/数据库测试覆盖匿名、Business Account 直接操作、待核验 claim、待核验场地关系、普通成员、外人均不能创建；核验 Owner 可以创建并发布，公开 API 返回稳定 Business ActorRef；撤销经营关系后活动不再公开。041 有数据时 down 拒绝，空表 down/reapply 通过。未部署正式环境。
