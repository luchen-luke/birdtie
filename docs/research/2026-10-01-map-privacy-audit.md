# 地图公开位置隐私审计

日期：2026-10-01（Asia/Shanghai）  
任务：`BT-SAF-001`

## 边界核对

- 当前 Android Manifest 不申请设备定位权限；Flutter Now 地图使用所选城市地图视图，不读取设备 GPS。公开 API 路由中没有普通用户实时定位写入或公开轨迹查询接口。
- Activity、Group 只有在关联的已发布 Place 具有 WGS84 公开点坐标时才得到地图点；Place 自身也要求已发布和公开点精度。未满足条件的实体可在获准列表中出现，但不上图。
- People 只来自本人确认、公开 Profile、公开 Intent、有效时间窗口与双方 Block 过滤后的查询。本人默认不上图；只有主动选择 `city_centre/north/south/east/west` 时，API 才从**城市地图中心**生成固定示意锚点。此锚点与本人 GPS、住址、活动地点无关。参见 [ADR 0015](../decisions/0015-opt-in-public-person-area-markers.md)。
- 服务端 `WithContract` 新增最终响应防护：没有合法自选粗略区域、缺失或越界坐标的 People 即使被 Store 错误返回坐标，也会在序列化前清除坐标并排除地图 Pin。Flutter 解析层同样拒绝为未选区域的人员生成 Pin。

## 验证

- Go 新增表驱动测试，覆盖未选择区域、非法区域、缺失坐标、越界坐标与有效粗略区域，并检查 JSON 中是否泄露坐标及 MapEffects 是否生成 Pin。`go test ./...` 通过。
- Flutter 新增 API 响应测试：即便响应错误地给未选区域的人员附坐标，客户端也只为已选择粗略区域的人员建 Pin。`flutter analyze` 无问题，55 项 `flutter test` 通过。

该边界限制当前 MVP 的公开地图。未来若要接入更细的区域目录或设备定位，必须单独评估并重新定义明确的授权与公开精度，不能把现有示意锚点当作真实位置。
