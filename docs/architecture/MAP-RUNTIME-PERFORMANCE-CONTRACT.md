# Map Runtime Performance Contract

Status: canonical map lifecycle and performance rules. The product behavior is specified in [Now — Agent Map Workspace](../product/NOW-AGENT-MAP-WORKSPACE.md); the provider setup is in [Map Provider Configuration](MAP-PROVIDER-CONFIGURATION.md).

## Lifecycle

- Maintain one persistent native map instance per City/provider surface. Keep its widget key stable across focus, keyboard, selection, sheet, conversation and ResultSet updates; replace it only when the provider or City identity truly changes.
- Keyboard insets move overlays without shrinking/recreating the native map.
- Selection, viewport and ResultSet are separate state axes. Camera callbacks update viewport only.
- Programmatic camera effects are explicit and must not be mistaken for user map movement.

## Entities and markers

- Marker identity is a stable Entity ID (including a kind prefix where ID namespaces overlap).
- Diff previous marker IDs against the next ResultSet: remove deleted markers, create new markers and update only changed marker properties. Never clear and recreate every marker for a selection change or widget rebuild.
- Cache decoded/generated marker images by stable style key. Prefer native style layers/annotations when they avoid per-frame Flutter bitmap work.
- Preserve the selected marker while panning. Selection styling updates only the selected/previously selected marker.
- Use clustering or bounded marker visibility when result volume requires it; do not create one expensive widget per entity at unbounded scale.

## Requests and rendering

- `onCameraMove` performs no network query and no global UI rebuild. It may record the latest camera value locally; settle/debounce work is limited to deciding whether the explicit Search this area affordance should appear.
- Search this area is explicit. Keep the current ResultSet rendered until a bounds-scoped response completes, then atomically swap the ResultSet projection.
- Tag map/search requests with a request ID or cancellation token. Ignore stale completion and avoid overlapping marker mutation races.
- Camera animation and marker updates must not trigger high-frequency whole-page `setState`. Keep animation callbacks and expensive decoding outside `build()`.
- Pin, preview card and result row resolve to the same Entity ID and current entity data.

## Physical-device acceptance

On a representative Android/iOS device verify:

- opening/closing the keyboard does not recreate the map or flash its markers;
- typing changes only input/conversation/result overlays;
- camera movement sends no request until the user taps Search this area;
- a selected pin remains selected after the camera moves;
- result cards and markers share entity IDs and swap together after a successful search;
- map animation does not block composer typing or sending;
- no obvious frame stalls occur during selection, sheet expansion or keyboard transitions.

Automated tests should cover the state and stale-request guarantees. Native platform-view lifecycle and visual jank still require a physical-device run.


## 2026-10-04：MAP-001 单一图层投影

MapLayersController 是 Pin、轻卡和图层列表的单一当前源；六类原 ID 及 marker glyph 独立，选中 bitmap 也有区别。切换本地层仅更新投影，不重建 MapCanvas Element、不改 Task/选择/摄像机，不触发查询。PRIVATE Opportunity 只有本人显式打开才读原生接口。拖动地图不自动刷新本轮图层；明确「读取当前地图范围」才读取，范围未就绪时按钮禁用并说明。

同身份刷新保留旧快照至其原有效期；网络耗时从新快照 lease 中扣除。旧计时器在新请求等待时仍生效，不通过刷新续期。身份/API/client/组织/City 退休和迟到响应守卫在控件、投影与嵌套详情同时适用。详情重新读取原权限，缓存 Pin 不是访问许可。

Now 首屏本人意图卡和 Pulse 使用有界滚动容器，常驻 48dp 入口保留直接路径；不增加固定 Card 高度或缩小字号。旧 ONLINE/CITY 背景、20 次焦点循环、IME 单扣与当前 Task/result/selection 保留测试继续执行。平台 Mapbox/AMap 原生帧率、视觉闪烁和 TalkBack 仍需实际设备证据。
