# Activity 地点与 Venue 关系（V4）

状态：仓库实现，未部署。对应 `BT-V4-PLC-004`、迁移 `042_activity_place_venue.sql`。

Activity 保留独立 ID、城市、主办方与可选 `placeId`。Place 是位置实体；Venue 是该 Place 经审核、仍有效的举办能力。只有明确选择 Venue 时才记录 `venuePlaceId`，它必须等于 `placeId`。普通 Place 不自动变成 Venue。

| `modality` | `physicalPlaceStatus` | `placeId` | `venuePlaceId` |
| --- | --- | --- | --- |
| `in_person` / `hybrid` | `confirmed` | 当前城市有效且公开的 Place | 可选，同一 Place 的有效审核 Venue |
| `in_person` / `hybrid` | `tbd` | 无 | 无 |
| `online` | `not_applicable` | 无 | 无 |
| `unspecified` | `unknown` | 无 | 无 |

最后一行用于没有地点的旧记录，不能推断其形式。迁移只将原本已经有 Place 的记录回填为线下已确认，保留所有旧主键与 `placeId`。旧客户端不传新字段时，服务端按是否提供 Place 得到上述兼容值；新的中文创建界面要求用户明确选择。线上与地点待定活动不产生物理导航或地图 Pin。公开 Activity 读取仍遵守原有活动与 Place 可见性、有效期及 Block 规则。

数据库 CHECK、外键与写入触发器防止混合状态、跨城市/未公开/过期 Place、非审核或过期 Venue；API 在写入前返回可解释错误。Venue 之后失效时公开投影不继续宣称其有效，运营须重新确认场地。此模型不自动提供线上参与链接、预约或商家经营权；相关能力需要后续独立验收。

`BT-V4-ACTY-002` 将 `hybrid` 的地点已确认/待定两种状态接入中文活动编辑器和详情，编辑时保持原模态。可见范围由服务端活动策略执行：`public` 可公开发现，`organizer_members` 仅活跃主办方成员，`invite_only` 仅有效受邀者；邀请写入只能由有权管理活动的人执行。客户端发布确认和状态文字按真实可见范围显示。上述能力已在隔离数据库及 Debug 客户端验证，线上参与链接与正式活动运营仍需另行实现和验收。
