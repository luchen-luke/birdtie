# 国内 Web 地图接线与坐标边界

本批只修改 Birdtie Web 适配与公共城市分流。Civu 项目只读，未导入其页面、路由、账户状态或旧导航。JS SDK 真服务、国内真机、地标对齐和端到端性能均为 **NOT_RUN**；本批 14 项 fake SDK、9 项 Dart/widget 检查不能替代这些验收。`receipt.json` 固定源码摘要与检查日志。

| 已核对来源 | Birdtie 落点 | 复用方式与边界 |
| --- | --- | --- |
| `Civu-client/lib/src/services/map_provider_policy.dart` | `apps/client/lib/src/city/map_provider_policy.dart`、`public_city_controller.dart`、`public_city_map.dart` | 复用明确国家选择提供方的产品规则，独立实现 CN/CHN → AMap、其他明确国家 → Mapbox；未知国家保持原配置，不靠相机坐标猜国家。 |
| `Civu-client/lib/src/services/amap_coordinate_boundary.dart`、`widgets/civu_explore_map_amap.dart` 的屏幕角点采样 | `amap_city_map_stub.dart::canonicalAMapViewport`、`amap_city_map_web.dart` | 复用“SDK 坐标只在边界转回标准查询坐标”的设计；实际公式由根任务提供的独立 MIT `MapCoordinateBoundary` 实现，未复制 Civu helper。 |
| Birdtie 既有 AMap Web SDK 2.0 transport | `apps/client/web/birdtie_amap.js` | 保留官方 `convertFrom(..., 'gps')` 显示转换、SDK 像素投影和同一地图实例。补实际四角与中心、缩放值、滚轮/旋转/拖动统一安静期、显式点选回传。 |
| Birdtie 既有 `MapDisplayPolicy`、受控 owned-map media/controller | `amap_city_map_stub.dart`、`amap_city_map_web.dart`、`birdtie_amap.js` | 使用同一权限结果、媒体预算与有界任务聚合；独立动态、内容聚合与同位置条目送回当前 Now 面板。封面复制需真实期限并在 JS 到期清除；有效像素不依赖旧 `momentPhoto!`。 |
| Civu 服务端 `places_handlers.go` 的 provider 导入转换与 `legacymap/build.go` 来源证明 | `coordinate-source-readonly.json` | 只核对 Web34 实际访问过的两个精确公共地点。二者均有 GCJ02 原坐标、WGS84 存储声明；原转换重放误差小于 1e-6 度，Birdtie 迁入坐标与原值相同。未批量移动内容坐标。 |

173 的公共城市 camera 坐标和内容位置是两个不同来源：camera 使用带 CRS84/WGS84 声明的公共 Natural Earth 城市点，仅影响浏览视图；内容使用原授权地点/动态的来源事实。两条已核验地点不支持“迁入漏转或重复转换”的判断，用户所见偏差仍须有效高德服务上的地标对齐验收。174 的国内 provider 修正由后端任务负责，本批不应用数据库迁移。

节点更新保留当前授权且坐标未变的 DOM；撤回项同步移除，旧 revision 点击始终失效。成功坐标转换缓存上限 320，每批上限 40、两并发，刷新只有一个执行槽和一个最新待执行槽。Dart gate 继续校验完整权限/来源 scope；JS 接受的是完整的新授权集合，账号清空时清除坐标缓存。已有 scope 撤回/ABA、变更标题、封面过期、任务聚合成员和真实 SDK 模拟像素均有定向断言。

初次运行中原测试把任何 source revision 都应清空节点作为期望，与此次增量更新要求冲突；已保留其跨 scope 撤回检查，并新增同 scope 节点/转换复用检查。首次运行结果为 8 PASS / 1 FAIL，最终 14 PASS；没有以旧断言失败冒称产品通过。

官方坐标输入说明：[AMap JS API 2.0 坐标转换](https://lbs.amap.com/api/javascript-api-v2/guide/transform/convertfrom)。该服务支持将 GPS 坐标转换为高德显示坐标；本批仍使用该正式调用。性能对比仅验证未变节点和转换次数复用，真实网络/帧耗时前后对比 **NOT_RUN**。
