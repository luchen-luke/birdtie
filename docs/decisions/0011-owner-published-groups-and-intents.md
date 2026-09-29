# ADR 0011: Group 与公开 Intent 由本人确认发布

日期：2026-09-30
状态：已采用；取代 ADR 0008、0010 中对 Group/Intent 独立城市人工审核的要求

## 决定

经过 Birdtie Session 验证的 Owner 可以在已发布 City 中直接发布自己的 Group 或公开 Intent，不经过 City reviewer 队列。自有 Group 无需外部链接；若提供外部来源，则必须使用 HTTPS 链接并注明来源名称，权利说明可选。公开 Intent 仍需本人明确确认、已保存为公开的 Profile、有效时间窗和粗略区域。发布和撤回由 API 记录审计事件，Agent 查询继续检查公开状态、到期时间、Account 状态、双方屏蔽关系和位置精度。发布不等于 Birdtie 核实了 Group 的来源或用户真实身份，未核实内容不得显示为“已审核”。

旧审核流程中尚处于 draft 的 Group/公开 Intent 在迁移时转为隐藏/撤回，不自动公开。Owner 可重新提交。Profile 改为 private 时撤回其公开 Intent；公开状态下编辑名称或简介不再触发撤回。原有 Place/Activity City Seed 的来源人工审核规则不变，Inbox 仍接收这些内容的审核结果；Group/Intent 不产生“审核通过”通知。

当前 Intent 界面允许选择从提交时起 1、3、7、14 或 30 天有效。服务端暂保留每位 Owner 最多三条未到期活跃 Intent 的数量限制；该上限是当前技术限制，不作为长期产品规则。未来若支持指定开始时间，应按所选 City 的时区明确转换，不以设备本地时区猜测。

## 范围

本次仅调整已有 Group/Intent 发布流、数据库约束、客户端文案和相关文档。不加入匹配算法、联系请求、消息、举报工作台或其他超出 Agent-first Shell 文档的功能。当前固定码手机号仍只用于本机开发，不代表真实身份认证；对外开放前需单独处理滥用限制与举报处置。
