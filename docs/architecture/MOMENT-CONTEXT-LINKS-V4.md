# Birdtie V4 私人 Moment 情境关联

日期：2026-10-02。对应 `BT-V4-MOM-001`。本阶段只让本人私人草稿稳定引用已有真实实体；未建立公共动态、Place Memory、社群动态或自动社交推断。

## 数据与授权

- Moment 保留 006 的稳定 ID、作者、城市、可选 Place ID、`draft/private` 默认值和撤回状态。`moments.place_id` 继续由 `(place_id, city_id)` 外键保护；不会从位置或文字推断地点。
- Activity 继续使用 006 已有的 `moment_activity_links`，保留原有多活动链接。043 增量添加 `moment_community_links`、`moment_organization_links`，各有实体外键及作者确认时间，分别允许当前一条社群和组织关联；不重建 Moment 或旧链接表。
- 创建或编辑私密草稿时，服务端要求 Activity 在同城、已发布且当前作者可读；Community 要求作者为活跃成员、社群有效且同城或无城市；Organization 要求作者为活跃成员且组织有效。无权限、错误城市或无效 ID 回滚整个写入和版本变化。
- 新字段省略表示旧客户端保留现有关联；显式空字符串表示清除该类关联。只有本人鉴权接口可读取、编辑或撤回草稿。草稿关联不能从 Place、Activity、Community 或 Organization 的公开接口反向检索。

## 中文客户端

“我的 Birdtie”私人草稿编辑器可从当前城市的公开 Place/Activity 和本人已加入的 Community/Organization 名单明确选择关联。记录列表只显示关联类型，不在公共地图、资料或详情中公开草稿。登录令牌变化后，本地列表立即隐藏上一会话资料。活动或成员资格已变化时，服务端仍在写入时重新校验；旧的私人引用可以继续在本人草稿中查看。

## 边界

当前编辑器一次选择一条 Activity；旧表允许多条，旧关联不会被迁移或自动删改，只有本人显式更换 Activity 时才替换该草稿的 Activity 链接。公开发布、媒体、可见范围细分、朋友可见历史、Place Memory 聚合由后续任务单独定义和验收。043 与客户端尚未正式部署，开发 seed 和 Debug APK 不作为试点证据。
