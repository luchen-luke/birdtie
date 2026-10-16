# Android manifest Key 接线

本批只解决已有 Android manifest 配置未接入 Dart gate 的阻断。Key 存在不代表高德鉴权已经成功；包名、签名与供应商配置仍需根任务合包和真机核验。

## 来源与实现

- 只读参考 `D:/Program/Civu/Civu-client/lib/src/widgets/civu_explore_map_amap.dart:823` 的 `AMapInitializer.init(context)`，未复制 Civu 业务代码、状态或导航。
- Birdtie 已有依赖 `apps/client/packages/amap_map/lib/src/amap_initializer.dart` 接受可空 `apiKey`；其 Android `utils/ConvertUtil.java` 对 `null` 直接返回，不调用 `MapsInitializer.setApiKey`。沿用该行为，避免空字符串覆盖已有 manifest Key。
- `apps/client/android/app/src/main/kotlin/app/civu/civu_mobile/MapConfiguration.kt`：通过当前安装包 metadata 检查 `com.amap.api.v2.apikey` 是否非空、是否仍为未展开占位符；MethodChannel 仅返回 boolean，不返回/记录 Key，不初始化 SDK。失败只返回固定错误码/文案。
- `MainActivity.kt`：注册和注销此 channel。
- `apps/client/lib/src/city/amap_native_configuration.dart`：保留显式 Android Dart define，Android 无 define 时检查 native metadata；manifest 分支 `apiKey == null`。iOS 仍依赖现有 iOS define，不探测 Android channel。
- `amap_city_map_io.dart`：异步检测时显示加载；缺少配置和读取失败分开显示，均可重试。超时、迟到回调与 dispose 有生命周期边界。通过配置检测后仍须当前会话明确服务同意，才初始化 SDK/创建平台地图。

适用交互规则为 UX-CHECK-02/06/07/09/10/12/13/16：复用已有配置、区分存在与鉴权/失败、保持重试及迟到响应边界，使用原同意入口，不写入日志中的 Key 或用户内容。

## 直接验证

在 `D:/Project/birdtie/apps/client` 执行：

`D:/DevTools/flutter/bin/flutter.bat test --no-pub test/amap_native_configuration_test.dart --reporter expanded`

最终 6 项 PASS，退出码 0，测试前后五个修改文件摘要一致。覆盖 manifest 布尔结果与空 SDK override、显式 define/iOS、不合法响应与错误、加载/缺失/重试、同意前不创建 AMapWidget、超时后迟到响应及移除后回调。结果和摘要见 `receipt.json`、`direct-tests.txt`。

首轮 3 个 widget 用例断言完成后因测试自身修改 debug platform 触发 Flutter invariant，保留 `attempt01-receipt.json` 和 `attempt01-direct-tests.txt`；改用 `TargetPlatformVariant` 后同文件通过，未将首轮失败删除或改写为通过。

Android/Kotlin 编译、APK 构建/安装、实际高德鉴权、手机同意与地图检查、iOS runtime：本 worker 均 NOT_RUN，由根任务后续统一检查。本批没有 Web、Gradle、坐标、媒体或权限逻辑修改。
