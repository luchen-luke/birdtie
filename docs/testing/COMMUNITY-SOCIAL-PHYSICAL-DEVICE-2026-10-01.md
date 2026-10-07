# Community Social Layer 真机验收（本地合成数据）

日期：2026-10-01。设备：Android `c641566b`（25098PN5AC），Flutter debug 包 `app.civu.civu_mobile.birdtiepreview`。API 是本机开发实例，经 `adb reverse tcp:3696 tcp:3696` 连接本地 PostgreSQL。三个手机号和社群、组织、活动均为合成测试资料；此记录不构成生产身份、CSSA 合作或 Closed Pilot 证据。

测试主体：A `4f710971-487f-4b30-8f85-4ed72006e5eb`（社群 owner），B `90cf49e0-9cba-4b36-b144-89215a384a94`（获批成员），C `17c80ae8-6090-49c5-b471-804340475d7c`（非成员）。社群 `bf427e93-934a-4c68-8fe1-c7f45703917c`。公开活动 `37d7ceac-f86f-4998-acdd-1eae15d7d21d`，仅成员活动 `3b6fea3b-be2c-4466-b50c-4a9773c31bce`，组织活动 `cfd739ba-345a-4aef-94cf-2217b7b3da15`。

| # | 场景 | 结果与证据 |
| --- | --- | --- |
| 1 | A 创建 Community | PASS；`work/community-created.png`；数据库 owner membership 为 active。 |
| 2 | B 申请加入 | PASS；`work/community-request.png`。 |
| 3 | A 审批 | PASS；`work/community-approved.png`；B membership 为 `member/active`。 |
| 4 | A 以 Community 身份创建公开 Activity | PASS；`work/community-activity-detail.png`，活动 `37d7...` 已发布。 |
| 5 | 非成员 C 查看公开活动 | PASS；`work/birdtie-com-c-detail-latest.png`、`work/com-c-public-detail.png`。 |
| 6 | C 报名公开活动 | PASS；`work/com-c-rsvp.png` 显示“你已报名”；数据库 `activity_participations.status=going`。 |
| 7 | 报名后不自动入社群 | PASS；C 的 `community_memberships` 计数为 0。 |
| 8 | 成员 B 查看仅成员活动 | PASS；`work/com-b-member-detail.png` 同时显示公开和仅成员活动；数据库 `birdtie_activity_visible_to(activity,B)=true`。 |
| 9 | 非成员 C 无法查看仅成员活动 | PASS；`work/birdtie-com-c-detail-latest.png` 仅显示公开活动；数据库 `birdtie_activity_visible_to(activity,C)=false`。接口拒绝路径由 `automation/verify_community_social.ps1` 校验。 |
| 10 | Organization Activity 仍正常 | PASS；`work/org-activity-created.png` 是真机组织活动管理页；数据库活动 `cfd739ba-345a-4aef-94cf-2217b7b3da15` 保持 `published`，`organization_id=1626e8b5-e461-4644-a46a-c3ca6ce422cf`。 |

验收期间修复了 Community Detail 创建活动后活动列表未立即刷新的问题：创建后刷新 `PublicCityController` 状态并重建页面，进入详情时也加载当前用户可见活动。`flutter analyze` 和 `flutter test test/community_page_test.dart` 均通过，修复后的应用已重新安装真机。旧截图 `work/birdtie-com-c-detail.png` 拍摄于修复前，不作为第 5/9 项证据。

边界：第 10 项真机截图拍摄于本轮新迁移及最新客户端构建后；后续数据库查询确认活动仍已发布。以上只覆盖本地开发设备和合成数据，正式部署、生产登录与真实活动 A→H 仍未验收。
