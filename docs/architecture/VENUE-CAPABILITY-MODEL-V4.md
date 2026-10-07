# Birdtie V4 Venue 举办能力

日期：2026-10-01。对应 `BT-V4-VEN-001`。本文件记录仓库内实现与隔离数据库验证，不表示真实场地、商家经营权或正式环境已核验。

`venues.place_id` 直接引用已有 Place 稳定 ID；Venue 是该 Place 可举办活动的可选资料，不是 Place 的默认类型、账户或 Agent。公园、街道等公开 Place 可以完全没有 Venue 记录。040 迁移不更改旧 Place、Activity 或 Intent ID。

City Seed 的活跃 contributor/reviewer 可以对已发布、未过期 Place 提交有 HTTPS 来源和有效期的 Venue 候选。候选可描述容量、预约方式（未知、不提供、联系、外部 HTTPS 链接）、适配项目和设施。未知容量保持空值；至少一项能力必须有来源。只允许同城独立 reviewer 审核，投稿者不能自审；通过后才写入公开 Venue。候选与审核写审计，重复待审候选被拒绝。数据库在有候选或公开 Venue 时拒绝 040 回退以防丢失来源。

`GET /v1/places/{placeID}/venue` 只返回已审核、未过期 Venue，且 Place/城市仍为公开、Place 未过期。无记录、待审、隐藏、过期均返回 404。可选 `operatorOrganizationId` 只是审核来源中的当前 Organization 引用；只有活跃组织可写入/读取。它不构成独立 Business 主体、经营权 claim、预约可用性承诺或商业赞助证明。后续 Business 任务需单独验证经营权和撤销，并明确新旧关系迁移。

隔离库使用合成 editor、Place 和来源验证投稿、双人审核、匿名公开读取、权限拒绝、重复候选、撤销/过期不可见、审计及旧 ID 不变；Go 全量测试与迁移 down/reapply 通过。没有真实场地来源、经营者确认或正式发布证据，Venue 消费 UI 也未在本任务实现。
