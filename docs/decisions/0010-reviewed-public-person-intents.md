# ADR 0010: 经独立审核的公开人员意图

日期：2026-09-30
状态：Intent 人工审核决定已由 ADR 0011 取代；Profile 可见性、本人确认和隐私边界仍有效

## 决定

登录用户可编辑自己的 Profile，并明确选择 `private` 或 `public`。公开 Profile 会对匿名访问者显示名称与简介。用户仅在 Profile 已保存为公开时，才能为已发布 City 提交未来 31 天内、带有效时间窗和粗略区域的公开 Intent。提交表示本人确认，但 Intent 保持 `draft`，不进入 Agent 搜索。

City reviewer 必须是另一名 Account，且拥有该 City 的有效 reviewer membership。审核通过前复查 Owner Account、Profile 公开状态和有效期；通过后 Intent 才成为 `active`。审核拒绝或 Owner 撤回都使它退出公开发现。审核结果通过现有 Inbox `updates` 通知 Owner。每个 Owner 同时最多三个未到期、待审或活跃 Intent；没有公开坐标。

Owner 改动 Profile 的名称、简介或可见性时，事务内撤回该 Owner 所有待审和活跃的公开 Intent。这样更改后的资料必须重新提交审核；切换为 private 也立即退出人员发现。Agent 查询继续按 Account block、Profile 公开性、本人确认、有效时间窗和过期时间过滤。用户不得借 Profile consent 扩大公开查询。

## 当时边界（现行发布规则见 ADR 0011）

界面提供 Profile 编辑、七天 Intent 提交、状态查看与撤回。审核由受限 API 完成；当前没有审核工作台或运营身份配置。没有联系请求或消息，Agent 不替用户联系他人。开发手机号固定码不验证号码所有权，因此本机测试资料不能视作真实身份。对外开放前还需真人审核运营、限流、举报与冒充处置。
