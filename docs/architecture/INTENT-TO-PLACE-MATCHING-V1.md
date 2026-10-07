# Birdtie V4 线下意图到地点匹配 v1

日期：2026-10-01。对应 `BT-V4-PLC-003`。这是仓库内规则式地点候选，不代表实地可用性、预约成功或真实用户匹配。

`GET /v1/me/social-intents/{intentID}/place-matches` 只接受有效 Person 会话和本人的已激活、未过期 `FIND_ACTIVITY`/`FIND_COMPANION`/`ORGANIZE` 线下意图。约束沿用社交 Intent 的类别、明确 Place ID、粗区域文字、人数与可选已发布 CITY Context；没有城市也没有明确 Place ID 时返回空，不猜所在地或跨城搜索。响应标注 `source=RULE_BASED`、稳定 `PLACE` ID、中文解释、机器可读理由和 `OPEN_PLACE` 动作，不自动更改 Intent、预约或创建活动。

供给只来自仍公开、未过期的 Place 和有独立审核来源、未过期的 Venue。类别必须与 Venue 审核适配代码一致；有参加人数约束时，审核容量必须已知且足够。粗区域只做地点名称/审核地址的文字包含匹配，不表示距离。明确指定的普通 Place 可以返回，但会明示“举办条件尚未核验”；类别或人数有要求而普通 Place 无证据时返回空。没有符合条件的地点时返回空数组。按明确指定且有核验 Venue、明确指定但能力未知、其他有核验 Venue排序，同层按稳定 Place ID，不用商家付费或私有行为改变排序。最多 50 条；基础 SQL 扫描最多 200 条，超大城市需要后续分页与空间索引。

固定时钟测试覆盖稳定排序、真实审核适配/容量理由、未知容量拒绝、草稿/过期/线上/异人/异城及无供给不生成结果。隔离 PostgreSQL/API 测试用合成 Person、Intent 和已审核 Venue 验证匿名/其他 Person/组织拒绝、审核供给可读、容量不足/隐藏 Place/过期 Venue 立即移出。真实场地运营、开放时段、预约、路线距离及地点匹配消费 UI 仍需独立证据；Social Alpha Gate 不因此自动通过。

## 2026-10-03：PLC002 语义排序增量

当前规则版本为`intent-place-v2-semantic`；原v1硬门槛和层级保留，旧“同层直接按Place ID”的描述仅是历史基线。当前层内先按语义分降序，再按稳定Place ID。当前公开且来源仍有效的 [Place语义档案](PLACE-SEMANTIC-PROFILE-V4.md) 中，good_for及suitability分别精确匹配用户明确类别各加2，group_size涵盖用户明确人数区间加3。没有该声明、过期/未来观察、错Place/City或无效判断时不加分。分数不是概率、现实适合保证或商业竞价，不能覆盖Venue容量/活动适配/城市等硬门槛。

价格、氛围和无障碍未有相应明确Intent偏好时仅展示公开声明，不从私人资料补偏好。返回`semanticProfile`携来源、有效期、编辑审核判断，理由为中文；声明人数范围不等于Venue容量、实时空位或预约。capacity/reservationSupport和sourceExpiresAt仍指原Venue事实；语义档案自身期限在semanticProfile.source.expiresAt中。

真实注册路由改为当前Session窄Store能力，没有仅ownerID认证回退。原生RC事务按当前本人Intent+公开供给组装，并在最后同一PGclock检查Account/Session、有效Intent/City与profile来源集合；旧owner-ID方法仅保留内部兼容。固定排名与隔离PG注册HTTP测试证明该增量，未做消费UI/真机、真实运营场地或模型出口验收。
