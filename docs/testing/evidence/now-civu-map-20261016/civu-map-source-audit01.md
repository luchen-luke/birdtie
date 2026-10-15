# Civu 地图呈现定点依据（2026-10-16）

本轮先只读 Civu，再按根已登记的视觉范围独立适配 Birdtie。Civu HEAD `1bbf0613b513001447f326225c944c1a1fbd8e8f`；没有复制其源码、主题、资源、provider、鉴权或全页导航。未运行 Civu；下列图片是既有历史证据，不代表当前运行验收。

## 已核的真实结构

| 依据 | 可复用的行为和边界 |
| --- | --- |
| `D:/Program/Civu/Civu-client/lib/src/services/explore_map_marker_layout.dart:8` | 小型地点图案、选中略放大、48dp 命中；名称位于图案右侧，小字数量位于名称下。名称有界，不扩大整个地图标记。 |
| `.../lib/src/widgets/explore_map_marker_icon.dart:101` | AMap/Mapbox 共用绘制入口；Place 为圆形，shootingPoint 为小圆角方形；真实封面优先、没有图或加载失败回退分类图案。封面仅在显示时裁切，不改源图片。 |
| `.../lib/src/widgets/civu_explore_map_mapbox.dart:970` | 相机稳定后按有限并发升级可见封面；112px 解码、有限 LRU，选中/重要地点优先；不把整张原图留在标记中。 |
| `.../lib/src/data/repositories/remote_mappers.dart:596` | Place 图片来自服务端 `coverUrl` / `imageUrl`，不是以同名图片、本机媒体或随机素材补全。 |
| `.../lib/src/services/explore_map_marker_density.dart:80` | 当前兼容入口返回 `clusters: []`。拥挤时做标签/小点处理，保持真实坐标；不能因为存在 Cluster 类就宣称当前会显示数字聚合。 |
| `.../lib/src/screens/place_detail_screen.dart:1985` | 关联内容有加载/错误/空状态，使用真实 PlaceContent notes/journeys，动态卡点入稳定内容。`loadPlaceContent` 在 `data/repositories/domain_repositories.dart:2924`，不是扫当前缓存猜关联。 |
| `.../lib/src/navigation/civu_content_navigator.dart:38` | 当前普通 openMoment 实际 push 动态/作者内容路由；而 Place 关联卡仍可调用 panel.openMoment。Civu 存在两代导航，不能全量复制其路径后就称满足 Birdtie Now 内联。 |

## 已目视的历史截图

- `D:/Program/Civu/Civu-client/build/req499-compact-final.png`：既有拍摄点照片被画为小圆角方标记，旁边有名称；下方原底图之上有紧凑图片/地点摘要。关联 work 文档为 `docs/work/2026-09-29-REQ-499-birdship-map-media-approach.md`，文件 mtime 为 2026-09-28。未重新运行该路径。
- `D:/Program/Civu/Civu-client/docs/work/REQ-296-theme-map-camera-validation/aberdeen.png`：类别画片、地点名、`1动态`、选中轮廓和原地图下层面板；这张图不能证明某个 Place 当前具有获准照片封面。文件 mtime 为 2026-09-11。
- 图片只目视，不复制进 Birdtie 证据；未采集用户相册、消息、媒体源或新设备数据。

## Birdtie 本批最小映射

1. 已有 `MapMediaPixels.current` 是唯一显示像素资格；不引入匿名 Image.network、Civu URL、原图库或公共缓存。地点为圆形、动态为圆角方形，真实图片继续优先。
2. 把现有底部居中名字改为图案右侧；12sp、白描边、短数量信息。未知/过期数量不画为 0，也不能将近30天计数改称全部数量。48dp 点击、原坐标、现有同坐标选择器不变。
3. 没图保留当前领域图标。现 DTO 没有可信 Place categoryCode，不能解析标题或 subtitle 猜分类，也不能新抄 Civu 的97张资源图案。
4. 地点/动态实体来源、本人私有图片、关联动态和短 lease 由根及后端/数据 worker 接线。视觉不能把原私人数据变为 public；不能将 current 为 false 的像素继续交给 native provider。
5. 点击地点在 Now 原面板展示名称、合法封面及当前可见关联动态；点击动态在同面板读稳定 Moment，再编辑时回统一同框编辑器。保留地图相机和草稿；不抄 Civu 的全局导航或另开地图页。

视觉范围：`map_marker_visual.dart`、`map_marker_bitmap.dart`、新 `map_marker_widget.dart`、`native_city_map_io.dart`；`map_entities.dart` 仅 IndividualMapMarker 委派和 import，DTO 不改。测试只新增一个相关复合直接用例，定向分析；构建、部署、真实权限来源及真机呈现由根本批验证，未执行不声称通过。
