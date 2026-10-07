# 地图与键盘生命周期检查

日期：2026-10-01（Asia/Shanghai）  
任务：`BT-MAP-001`，真机验收已通过（见末尾复测记录）

## 已实现与已验证

- Now 持有同一个 `MapCanvas` 组件实例；地图子组件仅在城市、地图投影实体、选中项或结果实际变化时更新。输入框的逐字变化只更新输入区域；原生地图的 City Key 保持稳定。
- 原生 Mapbox 组件在闲置浏览态收到活动/地点刷新时只更新标记，不再把镜头重新移回城市初始视野。程序化镜头动画仍与用户拖图事件分开处理。
- Flutter 组件测试连续 20 次输入与失焦，验证 `MapCanvas` Widget 和 Element 身份均不变。全量 37 项 Flutter 测试及静态检查通过；Debug APK 已在手机安装。
- 真机打开键盘时，当前城市活动列表保持可见，输入框移到键盘上方；没有观察到页面红屏。

## 尚未通过的真机门槛

- Mapbox 样式可读取，但底图复合源以 HTTP 403 拒绝访问；当前手机没有真实可见的地图 Pin，因此无法判断 20 次键盘开合是否产生 Pin 闪烁，也无法验收地图控制器、选中 Pin 和拖图表现。
- ADB 返回键脚本在手机输入法中未能可靠地只关闭键盘；尝试过程中离开了 Birdtie，脚本随即停止，Birdtie 已重新打开。该脚本不计入 20 次真机通过次数。完整压测需在底图权限修复后以可见地图进行。

## 后续验收

核对 Mapbox 公共令牌对样式复合地图源/底图瓦片的访问权限，随后在真机上完成 20 次输入与键盘开合，记录地图平台视图与 Pin 的可见状态。不要用组件测试或地图失败状态替代该门槛。

2026-10-01 补充诊断：已保存的令牌与用户提供的 `lookluo` 公共令牌 ID 一致；Mapbox 令牌自检返回 `TokenValid`，Birdtie 自定义样式及 Mapbox 标准样式均返回 HTTP 200。使用同一令牌读取 Mapbox 标准公开矢量瓦片、复合源 TileJSON 和标准样式瓦片均返回 HTTP 403 `Forbidden`。这排除了自定义样式 ID 错误作为唯一原因，但仅凭公共令牌不能区分账号状态与令牌限制。Mapbox 官方 [Vector Tiles API 文档](https://docs.mapbox.com/api/maps/vector-tiles/) 把该 403 解释为账号问题或 URL 限制的可能表现；[Token management](https://docs.mapbox.com/accounts/guides/tokens/) 说明 URL 限制在缺少匹配 Referer 时也可导致 403。本仓新增 `automation/check_mapbox_access.ps1`，直接读取被 Git 忽略的本机令牌配置并只输出 HTTP 状态，不输出令牌。当前检查结果为验证 200、样式 200、公开矢量瓦片 403。需要在 Mapbox 账号页面核对账号访问状态、该令牌 URL 限制和可用地图资源；之后重新运行脚本和真机验收。

同日进一步排查：原 Civu 仓库中已获准复用的另一个公开令牌对标准矢量瓦片和同一复合源均返回 HTTP 200。现在将它用作被 Git 忽略的本地 Android build 输入，保留用户提供的令牌在单独忽略的配置文件中。`automation/check_mapbox_access.ps1` 对当前 build 输入的令牌、自定义样式和公开瓦片均返回 200。必须以新 APK 在真机看到实际底图和 Pin 后，才能解除本任务阻塞。

## 真机复测结果

使用上述有效令牌重新构建 Debug APK 并安装到 `c641566b`，手机上的 Aberdeen Mapbox 底图和活动聚合 Pin 均实际显示。首屏见 [真实底图截图](evidence/2026-10-01-map-working-first-frame.png)。在该画面上运行 `automation/verify_map_keyboard.ps1`，连续 20 次打开键盘、逐次输入、关闭键盘；每次均验证 Now 页仍在前台、输入框随键盘上移且关闭后复位。20/20 通过。第 20 次的[键盘打开](evidence/2026-10-01-map-keyboard-open.png)和[键盘关闭](evidence/2026-10-01-map-keyboard-closed.png)截图中，同一 `5` 聚合 Pin 均可见，Aberdeen 底图位置未跳动。没有观察到清空或闪烁；截图仅能证明采样时的状态，逐帧闪烁无法由静态截图排除。

组件层此前还验证了 MapCanvas Widget/Element 身份在 20 次输入与失焦时不变，并限定输入框更新范围；因此本任务的地图生命周期与可见真机门槛均完成。用户原先提供的 lookluo 令牌仍保存在被 Git 忽略的本地备份中，其瓦片 403 待 Mapbox 账号侧排查；当前本地安装使用获准复用且瓦片可读的令牌。
