# BT-PER-002 — Now 真机性能记录

日期：2026-10-01。设备：Android `c641566b`，1220×2656。测量使用 Flutter profile APK 的 VM timeline；每组为连续 8 次拖图、4 次键盘开合或 10 次面板切换。原始摘要位于 `evidence/2026-10-01-profile-*.json`。

| 场景 | Flutter UI 帧 p95 / p99 | Raster 帧 p95 / p99 | UI 超过 16.7 ms |
| --- | --- | --- | --- |
| 连续拖图 | 0.75 / 28.02 ms | 1.92 / 2.37 ms | 7 / 353 |
| 键盘开合与输入 | 14.50 / 17.49 ms | 3.19 / 3.43 ms | 7 / 312 |
| 仅切换结果与对话面板 | 3.17 / 34.12 ms | 2.35 / 6.83 ms | 10 / 296 |

面板切换的慢帧集中在 Flutter 布局阶段（p99 约 27.61 ms），不是 Raster 阶段。手机上没有出现持续卡顿、地图重建或标记闪烁；该样本说明仍存在少量单帧布局尖峰，不应称为零卡顿。

代码审查发现镜头每帧取消并创建一次 400 ms 定时器，而且纯平移后仍会同步地图标记。已改为每次移动周期只发一次运动通知、一个按静止时间复用的定时器；仅当聚合所依赖的 zoom 变化时才在镜头停稳后同步标记。异步获取视口期间若又发生镜头移动，会忽略过期视口。镜头回调没有 Birdtie API 请求；标记位图按样式键缓存，输入和覆盖层更新不会重建原生地图实例。

更新后 `flutter analyze` 无问题、66 项 Flutter 测试通过、debug APK 构建并安装。真机追加 8 次拖图的 Android `gfxinfo` 为 311 帧、0 个 janky frame、0 次慢位图上传；另有第 21–24 次键盘开合检查通过。Flutter 调试服务已重新附加并打开 DevTools。

更新后的 profile APK 被手机以 `INSTALL_FAILED_USER_RESTRICTED` 拒绝两次；因此上述 Flutter timeline 是优化前的基线，不是优化后的量化对比。已恢复并打开更新后的 debug APK，当前真机交互验收通过。Android `gfxinfo` 与 Flutter timeline 口径不同，不能直接相减比较。
