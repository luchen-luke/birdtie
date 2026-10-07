# BT-V4-TIE-003 共同关系与情境审计

状态：仓库内实现、隔离隐私矩阵和开发真机设置验证完成；2026-10-02。队列依赖 `BT-V4-TIE-001` 和 `BT-V4-CTX-002` 已完成仓库内验证。正式试点未验收。

实施前 `person_ties` 存持久双向好友，`community_memberships` 存社群成员，`activity_participations` 存报名，但没有按两名 Person 推导共同情境的 API 或 UI，也没有展示参与关系的同意字段。本人多 Context 声明继续私密，学校/城市/在线标签不用于公开社交证明。

迁移 048 的三个本人展示开关默认 false，不回填任何同意。本人可通过 `GET/PUT /v1/me/social-disclosure` 设置并产生审计记录。`GET /v1/accounts/{accountID}/shared-context` 要求公开活跃 Person 目标、无双方 Block；共同好友只返回数量，候选本人也须公开且允许计数。公开社群须双方活跃成员；公开活动须双方 `going`，未取消/过期且通过既有活动可见性规则。每类列表最多 50 条并明确截断，私密成员/报名默认不展示。见 [授权模型](../architecture/SHARED-SOCIAL-CONTEXT-V4.md)。中文设置和公开 Person 页已接入，换账号隐藏旧信息；服务失败可重试，保存失败保留原值。

验证：`pwsh -NoProfile -File automation/verify_place_context_migration.ps1 -Through 48` PASS，实际执行 001–048 与全量 Go API/DB，包括默认私密、匿名、本人归属、三方共同好友同意、双方撤回、私密社群、受邀活动、pending 成员、取消报名、非公开目标、Block 和审计；旧 ID 保持，048 有数据 down 拒绝、空表 down/reapply 及 039–047 回归 PASS。`go vet ./...`、`go build ./...` PASS；`flutter analyze` 无问题、`flutter test` 124 PASS；Android Debug 构建/安装 PASS。首轮隔离检查发现测试请求缺 Content-Type、审计 UUID/text 参数冲突，均修复；首轮 Flutter 检查发现新增行使旧设置测试需滚动到授权条目，修复后全量通过。

Android 16 `c641566b` 上预览包 `app.civu.civu_mobile.birdtiepreview` 已安装、启动并连接 Flutter 调试器。手机使用 loopback API 3694 与本地 `birdtie_phone_review_f3de825b08`；只对该开发库增量应用 046–048，未改正式服务。测试登录明确不验证手机号、不发短信。中文开关从 false → true → 重新进入仍为 true → false，数据库同步 `t|f|f` → `f|f|f`，两条审计；[截图与操作证据](../testing/evidence/shared-context-2026-10-02/README.md)。共同关系隐私矩阵由隔离 API/DB 和 Widget 测试覆盖，未以合成关系声称真实用户能力验收。Closed Pilot Ready：NO。
