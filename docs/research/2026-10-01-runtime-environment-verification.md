# BT-RUN-003 运行环境验收记录

日期：2026-10-01。仓库：`D:\Project\birdtie`。

## 审计与实现

原客户端的 22 处 API 地址读取和地图配置分别读取 Dart 编译常量，正式构建缺少拒绝本机地址、明文 HTTP 和缺失配置的入口。现集中到 `apps/client/lib/src/config/birdtie_environment.dart`，在应用启动前校验。正式构建须经 `apps/client/tool/build_release.ps1` 读取本地配置文件，检查环境、非本机 HTTPS API、目标平台 Mapbox 公共令牌和可选的高德 Web 配置，再写入编译检查标记。绕开脚本的普通 release 编译在 Dart 常量断言处失败。正式 Android manifest 现明确具有网络权限。调试仍可通过 ADB reverse 访问本机 API。

`apps/client/release-config.example.json` 只列字段，不含凭据。该检查仅验证配置结构，不证明远端服务、令牌权限或部署可用。真实 staging/production 配置尚未取得；因此未生成可用正式 APK，也未完成生产连接验收。

## 可复现检查

以下命令均在 `apps/client` 执行，除特别说明外：

| 检查 | 结果 |
| --- | --- |
| `flutter analyze` | PASS，0 问题 |
| `flutter test test/birdtie_environment_test.dart` | PASS，5 项：调试、本机/HTTP、缺失配置、地图令牌和高德配置边界 |
| `flutter test` | PASS，72 项 |
| `flutter build web --release`（不经配置脚本） | 预期拒绝：`_ReleaseGate` 常量断言，编译失败 |
| `./tool/build_release.ps1 -ConfigFile ./release-config.example.json -Target web -CheckOnly` | 预期拒绝：缺少 API 地址 |
| `./tool/build_release.ps1 -ConfigFile ./test/fixtures/release_localhost.json -Target apk -CheckOnly` | 预期拒绝：本机 API |
| `./tool/build_release.ps1 -ConfigFile ./test/fixtures/release_http.json -Target apk -CheckOnly` | 预期拒绝：明文 HTTP |
| `flutter build apk --debug --dart-define-from-file=.env.maps.mobile.local.json --dart-define=BIRDTIE_ENVIRONMENT=development --dart-define=BIRDTIE_API_BASE_URL=http://127.0.0.1:3694` | PASS，使用隔离 Gradle 缓存；Debug APK |
| `adb -s c641566b install -r build/app/outputs/flutter-apk/app-debug.apk` | PASS，真机安装 |
| `./tool/run_device_debug.ps1 -CheckOnly`，API 运行于 `127.0.0.1:3694` | PASS，主机和手机经 ADB reverse 均可请求 `/readyz` |
| 真机启动 `app.civu.civu_mobile.birdtiepreview` | PASS，本机 Go API 记录来自应用的多个 HTTP 200 GET |

本机 Debug、开发地图令牌与模拟数据不是正式环境证据。正式配置、远端 API、地图服务及发布渠道由后续试点门槛核验。
