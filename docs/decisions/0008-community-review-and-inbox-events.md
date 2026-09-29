# ADR 0008: Community 独立审核与 Inbox 事件

日期：2026-09-30
状态：已采用，首个真实内容供给与 Inbox 事件切片

## 决定

经过 Birdtie Session 验证的 Owner 可为已发布 City 提交 Community。提交同时记录 Owner 确认、HTTPS 来源与权利说明，但状态保持 `draft`；提交本身不使 Group 进入公开搜索。City reviewer 角色由已有 `city_editor_memberships` 授予。另一个 Account 的 reviewer 核验来源和权利后，才能发布或拒绝。数据库约束禁止 Owner 自审发布。发布时再次检查到期时间与关联 Place 的可用性；Owner 可随时撤回 draft 或 published Group，使其立即退出公开查询。

Place、Activity 与 Community 的审核决定在同一事务中写入属于提交者的 Inbox `updates` 项。Inbox 的 Account 来自数据库中提交者身份，客户端不能代他人生成消息。列表与已读操作都用 Session 中的 Account 限定，其他 Account 的条目返回 404。标题和摘要仅包含审核结果，不把审核注释、私有权利证据或账户标识放入通知。没有人工审核身份或来源验证时，不发布真实 Group 数据。

## 当前限制

Inbox 的首个真实 producer 只有内容审核决定。`messages`、`requests`、`agent_updates` 与一般系统通知仍无 producer；客户端在 API 模式下显示真实空状态，不显示演示通知。Group 暂不支持编辑或再次提交，Owner 可撤回并重新提交新记录。公开 Intent 人员供给仍缺少用户可控的 Profile 与 Intent 发布流程。OIDC、编辑角色与实际审核运营尚未配置，当前仓库无公开内容种子。部署前还需限流、滥用举报、审核工作台和审计复核。
