# Birdtie V4 Place 情境节点

日期：2026-10-01。对应 `BT-V4-PLC-001`。此文档记录仓库内已实现的 Place 基础；Venue 独立能力层见 [Venue 审核模型](VENUE-CAPABILITY-MODEL-V4.md)，私人 Moment 情境链接见 [Moment 链接模型](MOMENT-CONTEXT-LINKS-V4.md)，公开 Place Memory 尚未上线。

## 身份与来源

`places.id` 是稳定 UUID。地图实体 `place:{id}`、Agent 地点结果、活动 `activities.place_id`、私人 Moment `moments.place_id`、社群/收藏及公开地点 API 继续引用它；039 迁移不改旧 Place、Activity、Moment 或 Intent ID。Place 是地点情境，不是账号、Agent 或经营权。已有 City FK 与跨表 `(place_id, city_id)` 外键阻止把地点和活动/Moment 误接到不同城市。

Place 的类别、摘要、WGS84 精度/坐标和来源来自现有 City Seed 候选及独立审核。039 为候选和 Place 增加可选 `address_label`；现有记录保持未知，不反向地理编码或从个人/Moment 推断地址。候选仅在明确提供 point 级地点时可提交地址，独立审核 `publish` 后才进入公开 Place；`link_existing` 只给原本无地址的公开 point Place 补充审核地址，不覆盖已有地址或改变稳定 ID。带地址数据时 down 拒绝丢失。

## 公开读取与界面

`GET /v1/cities/{cityID}/places` 与 `GET /v1/places/{placeID}` 只返回已发布城市内、已发布且未过期的 Place；非 point 级地点不返回精确坐标。新 `GET /v1/places/{placeID}/activities` 再次核验 Place 可公开读取，并通过原 Activity 可见性函数和 Block 过滤返回该 Place 的近期公开或当前阅读者有权访问的活动。隐藏、过期、取消、私密或无权活动不可从地点关联绕过原规则。私人 Moment 仍只在作者 API 中存在，不给公共地点详情提供 Moment 列表或计数。

Flutter 地图 Pin 点击保持轻量 Entity Card；点击“查看”后按 Place ID 重新读取地点资料和可见活动，显示来源、审核地址及可点击活动。Agent 地点结果也可直接打开同一详情，含无公开坐标的 Place；无 point 坐标时不生成 Pin 或导航。地点详情独立读取经审核 Venue，显示有来源的容量/适配/预约方式及仍有效的公开组织、已核验 Business 经营关系；不把普通 Place 当默认 Venue。已登录者仅可从本人接口看到最近与该 Place 关联的私人 Moment，明确标注只给本人看且不进入分享文本。活动详情继续用原 Activity ID。子资源失败时保留客观地点资料并给出分区错误，不展示旧缓存或伪造内容。

## 已核验边界与后续

一次性 PostgreSQL 001–042 迁移验证审核地址、现有 ID 保留、隐藏/过期 Place、私密 Activity/Moment 不泄露及数据保护 down/reapply；Flutter 分别测试地点详情、Venue/Business 关系、私人 Moment、导航分享与失败状态。2026-10-02 已在 Android 真机连接隔离开发库完成地图/Agent 入口、合成 Venue、无坐标/空活动、私人记录和断连重试；真实授权地点与商家资料尚待试点复核，见 [设备记录](../testing/BIRDTIE-V4-PLACE-DETAIL-DEVICE-REVIEW.md)。正式环境尚未部署。公开 Place Memory、Business claim、自助管理、可分享 Moment 与 Chat 实体卡和跨城市 Place 发现由各自队列任务完成，不在本任务中假装具备。
