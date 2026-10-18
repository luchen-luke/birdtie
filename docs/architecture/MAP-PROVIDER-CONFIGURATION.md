# Birdtie map provider configuration

## 2026-10-18 PMAP 当前接线与配置边界

本节覆盖下面的旧实现/配置快照，保留历史失败证据。普通用户仅选择目的地或查看真实内容，不选择/启用供应商。所有显式导航源汇入 `MapNavigationTarget`：大陆明确目标自动 AMap，HK/MO/TW和海外明确目标沿现有适配约定使用Mapbox；不依据语言、IP或手机所在地。自由拖动仅在相机稳定、真实地表跨度和有出处的陆地区域均确定后切换，边界/海上/世界保留当前供应商。详见 `map_provider_policy.dart`、`map_route_region.dart` 及区域资产README；坐标转换的包络不用于判定路由。

AMap真实隐私选择已改为版本化secure storage记录，仅经过当前披露和显式用户决定才调用SDK同意接口。首次需要服务时展示“地图服务隐私说明”，明确拒绝后不反复弹；设置页可查看/更改。自动路由不代表同意条款，不申请GPS、不写定位权限；撤回会退休旧SDK回调和视图。历史手动供应商偏好不覆盖地理路由。配置失败只向用户给简短重试/列表提示，具体供应商诊断留开发日志，保留logo/版权/信息入口。

Android继续使用现有已授权preview Key、包名与签名配置，Key值不入日志、Git或交付报告；本轮无需新Key。iOS构建/实机NOT_RUN（当前Windows无Xcode）。Web Key/受信AMap代理仍是外部配置，缺失时BLOCKED，用户此前“暂不部署网页”继续有效，不能静默用Mapbox代替国内服务。当前高德离线样式仍是已获准原GeoHUB导出，插件无单路名/区名opacity接口；新视觉导出缺失为PMAP10的BLOCKED。Mapbox仅对已识别次要路名/区名降权，保护主路、图标盾牌、水系、主要地名与署名；保留原zoom表达式、未知层跳过，按style实例缓存原opacity防重复降权。

本批build/API/装机和两端实际证据逐项见 [PMAP执行报告](../reports/BIRDTIE-PIN-AUTOMAP-2026-10-18.md)，不能由下文旧APK40/37的成功推定新包通过。

2026-10-18 性能增量：63版真实高德键盘/阅读窗口仍有系统慢帧，不能以Flutter UI/raster分项低耗时当总性能通过。当前vendored Android host从锁定11.2.100的MapView(GLSurfaceView)改为同SDK的TextureMapView；MapController、销毁与全部生命周期类型同步适配，Flutter使用已锁定框架的initSurfaceAndroidView（TLHC，系统不支持时HC回退），不强制Virtual Display/HCPP。原有隐私、投影转换、原生触摸/读屏和生命周期授权边界保留；64版是否改善须以新真机证据为准，63失败不能删除。

## 2026-10-16 Android37 增量更正（当前状态）

本节优先于下方较早配置快照；旧失败证据保留。用户本轮明确暂不做网页，Web34保持，不要求网页Key，也不部署174。

- 本机原本已有CIVU Android Key，旧问题包含Dart适配器未识别native manifest配置。现已接入只返回配置存在性的通道，不把Key传入诊断。
- APK36第一次真机失败为未注册AMap平台视图；离线依赖解析此前在Windows symlink步骤退出，生成注册器陈旧。已通过仅进程级关闭非Android平台的离线pub get正常生成，未手改生成文件或全局系统设置。
- APK36r2真机加载到高德底图，但原CIVU Key返回INVALID_USER_SCODE。用户随后已为预览包与实签名配置新Key；当前Key与CIVU不同，已核对配置和manifest一致，无需再次提供Key。原CIVU配置未改。
- 已复用CIVU用户GeoHUB导出的原始 `style.data` 与 `style_extra.data`，Android首次创建map前加载，两文件字节与CIVU原件一致。加载失败可重试；成功同进程复用，抑制底图POI图标与文字，保留道路标注及Birdtie业务内容。插件的基础许可证与用户自有样式来源分开记录。
- APK37已合包，SHA256 `d2a6cba403298bc605e27649c390cd71f3fd880a516e43ad690f1397eef85465`。包名 `app.civu.civu_mobile.birdtiepreview`，签名SHA1 `73:84:0A:17:D0:CA:7B:4B:F4:15:41:B4:2C:EC:B7:A2:34:D5:05:23`。Key不会提交Git。安装时ADB无设备，最新手机仍是36r2；37的新Key鉴权、自定义样式与地标对齐均NOT_RUN，外部依赖BLOCKED。
- 6项Key桥接测试、4项样式测试PASS；样式接入后Key6项复跑PASS（不是新增6项）。保留两次fixture异步同步失败及修复记录。证据分别在 `docs/testing/evidence/china-map-boundary-20261016/android-key-reuse`、`android-civu-style`、`android36-device`、`android36-r2-device`、`android37-device`。
- APK37不包含随后DGS地图稳定性和身份恢复代码。后续源码不能沿用APK37合包/真机结果，需合批新构建。

新增源文件：`amap_native_configuration.dart`、`amap_offline_style.dart`、Android `MapConfiguration.kt`；适配 `MainActivity.kt`、`amap_city_map_io.dart` 与pubspec；样式来源 `D:\Program\Civu\Civu-client\assets\map_styles\civu_android` → `apps/client/assets/map_styles/civu_android`。CIVU只读，未整体迁移旧导航。


Status: 2026-09-30. Overseas City provider: Mapbox; mainland China provider: AMap. Civu is a read-only configuration reference. This page records the exact values to create and where they belong; it does not include any key value.

## Reuse inventory

| Civu item | Read-only source | Birdtie use |
|---|---|---|
| Mapbox custom style | `D:\Program\Civu\Civu-client\lib\src\widgets\civu_explore_map_mapbox.dart`, `mapbox://styles/lookluo/cmth7kwad001p01ssc9kw7kco` | Selected as Birdtie's overseas style. The Web Static Tiles URL and the Birdtie iOS/Android Mapbox SDK adapter use the same style ID. |
| Mapbox public token | `D:\Program\Civu\Civu-client\.env.local`, `MAPBOX_PUBLIC_TOKEN` | Reusable for Styles API and potentially native SDK where account restrictions permit. A direct Static Tiles request returned 403, `This API requires a token with styles:tiles scope.` Create a public Birdtie token in the same Mapbox account with `styles:tiles` before using the Web map. |
| AMap Web Service Key | `D:\Program\Civu\Civu-server\api\.env.local`, `AMAP_WEB_SERVICE_KEY` | Candidate for future server-side geocoding/Place lookup only. It is not a browser JS API or native SDK key. No Birdtie geocoding route currently consumes it. Confirm service scope, billing and quotas before use. |
| AMap Android Key | `D:\Program\Civu\Civu-client\android\amap.properties`, `android.key` | Reusable for the final `app.civu.civu_mobile` app only with Civu's registered release signing SHA1. Copied to ignored `apps/client/android/amap.properties`; Birdtie's side-by-side debug package has suffix `.birdtiepreview` and cannot use this registration. |
| AMap iOS Key | `D:\Program\Civu\Civu-client\ios\Runner\AMapKeys.xcconfig.example` | The actual `AMapKeys.xcconfig` is absent in the current Civu checkout. Birdtie retains the existing `app.civu.civuMobile` bundle ID; retrieve the existing Key from the AMap account or register one for that same ID. |
| AMap Web JS Key | No matching key/security configuration found in the reviewed Civu client/server files | Create a Web JS API Key and security key for Birdtie. |

The reviewed Civu local environment files did not contain configured Mapbox Directions, Google Routes or Pelias keys, although the Civu server has optional configuration fields for them. They are not part of Birdtie's current map display path.

The Civu Mapbox public token was copied to ignored `apps/client/.env.maps.mobile.local.json` for local native configuration. The Web token file is absent until the new token is created. The Civu AMap Web Service Key was copied to ignored `apps/api/.env.maps.local` for a future server adapter; no current route reads it. The Civu Android AMap Key was copied to ignored `apps/client/android/amap.properties`, but its registered package/signature cannot authenticate the debug preview. No key value, signing material, map widget or customer data is committed to Birdtie. Account owner should confirm that the `lookluo` Mapbox style is published and permitted for Birdtie distribution. AMap Key registration and Mapbox scopes are platform constraints, not code substitutions.

2026-10-01 local Android update: the newly user-provided public token remains in ignored `.env.maps.mobile.provided.local.json`; it validates and reads styles but returns 403 for standard vector tiles. The already approved Civu public token is now the ignored `.env.maps.mobile.local.json` native build input because direct requests for the same standard and composite vector tiles return 200. `automation/check_mapbox_access.ps1` reads the current mobile build input without printing it. This is a local verification choice, not confirmation that the credential is approved for production distribution or that Web Static Tiles works.

## Create in provider consoles

1. **Mapbox**: In the existing `lookluo` account, create a **public** token named `Birdtie web map`. Enable `styles:read`, `styles:tiles` and `fonts:read`. For a URL-restricted Web token, allow each exact Birdtie production/staging origin and `http://localhost:7357` for local development. Mapbox URL restrictions do not accept IP addresses; `127.0.0.1` is unsuitable. Copy `apps/client/map-config.web.example.json` to ignored `apps/client/.env.maps.web.local.json` and set `BIRDTIE_MAPBOX_PUBLIC_TOKEN`. The native SDK reads the existing Civu unrestricted public token from a separate mobile file; Mapbox URL-restricted tokens do not support mobile Maps SDK requests. The style is already selected in the shared `apps/client/lib/src/city/mapbox_style.dart`. Existing Civu `MAPBOX_PUBLIC_TOKEN` reads the style but lacks `styles:tiles`; Birdtie's Android preview has loaded the native custom style on a real device.
2. **AMap Web**: In the existing AMap account, create an application `Birdtie`, then a Key named `Birdtie Web`, service platform **Web端 (JS API)**. Record both Key and 安全密钥. Configure a trusted HTTPS reverse proxy with `serviceHost` ending in `/_AMapService`; it forwards only the required AMap paths and appends the 安全密钥 server-side. Set public `BIRDTIE_AMAP_WEB_PUBLIC_KEY` and the proxy URL `BIRDTIE_AMAP_SERVICE_HOST` in the Web build configuration. Do not put 安全密钥 in Flutter Web source or `--dart-define`. If the console offers a domain whitelist, include the exact Birdtie domains and development host.
3. **AMap Android**: The final app retains `applicationId` **`app.civu.civu_mobile`**. Use the existing Civu Key and registered release SHA1 only after the original release signing credential is securely available and verified. The current ignored `apps/client/android/amap.properties` contains that Key, and the build injects it into the manifest. For side-by-side debug testing, register a separate **Android平台 SDK** Key for package **`app.civu.civu_mobile.birdtiepreview`** and this workstation's debug SHA1 **`73:84:0A:17:D0:CA:7B:4B:F4:15:41:B4:2C:EC:B7:A2:34:D5:05:23`**, then temporarily set `android.key` to that debug Key. Restore the release Key for release builds. A different workstation has a different debug SHA1. The AMap renderer is not implemented yet, so Key registration alone cannot validate its map.
4. **AMap iOS**: The final Xcode bundle identifier is **`app.civu.civuMobile`**. Retrieve the existing iOS Key from the AMap account, or create one for this same bundle ID. Copy `apps/client/ios/Runner/AMapKeys.xcconfig.example` to ignored `AMapKeys.xcconfig` and set `AMAP_IOS_KEY`; the iOS build configuration loads it into `Info.plist`. The actual Civu Key file is absent in the read-only checkout. Build and validate on a Mac/iOS device before release.
5. **AMap Web Service**: Reuse the Civu Key only in Birdtie server configuration after confirming account/service authorization and request quota. It must stay server-side. No Birdtie route needs it today.

Official setup: [Mapbox token guidance](https://docs.mapbox.com/accounts/guides/tokens/), [Static Tiles API](https://docs.mapbox.com/api/maps/static-tiles/), [AMap Web setup](https://lbs.amap.com/api/javascript-api-v2/prerequisites), [AMap JS security proxy](https://lbs.amap.com/api/javascript-api-v2/guide/abc/jscode), [AMap Android Key](https://lbs.amap.com/api/maps-sdk-for-android/guide/create-project/get-key), [AMap iOS Key](https://lbs.amap.com/api/ios-sdk/guide/create-project/get-key).

## Local Web run

After filling ignored `apps/client/.env.maps.web.local.json` with the newly scoped Web token, run from `apps/client`:

```powershell
flutter run -d chrome --web-hostname=localhost --web-port=7357 --dart-define-from-file=.env.maps.web.local.json --dart-define=BIRDTIE_API_BASE_URL=http://127.0.0.1:8080
```

Build **debug** native targets with `--dart-define-from-file=.env.maps.mobile.local.json`. Keep the Web and mobile files separate so the Web build does not carry the unrestricted mobile token. For staging/production release builds, fill a locally ignored copy of `apps/client/release-config.example.json` and use `apps/client/tool/build_release.ps1`; it rejects missing, HTTP and loopback API settings and requires the target platform's public Mapbox token. This validates configuration structure only; check live provider access separately. URL restrictions and scoped permissions protect the Web token; a mobile SDK token cannot be URL-restricted. Set the API's `BIRDTIE_ALLOWED_ORIGINS` and OIDC client redirect to the `localhost:7357` origin for this development command. Do not put an AMap security key, Web Service Key, Mapbox secret token or mobile signing secret in a client Dart define.

## Next implementation boundary

Birdtie's Android/iOS Flutter targets retain the Civu release application IDs. Android debug uses `.birdtiepreview` as an application ID suffix, so both apps can coexist on one device. The Birdtie-native Mapbox SDK adapter uses Birdtie's public City/Place state and the Civu custom style URI. On 2026-09-30 the Android preview installed and launched on a Xiaomi Android 16 device, loaded Aberdeen from the local Birdtie API through `adb reverse`, and displayed the custom Mapbox style. No published Place marker was available in the local seed data. iOS requires a Mac/Xcode build and device review. AMap Web JS 2.0 rendering is now wired for City view and public markers, with display conversion from WGS84 through `AMap.convertFrom`; the browser receives only the public Web Key and trusted proxy URL. No AMap Web Key or proxy is configured in this repository, so live tiles are unverified. Native Android/iOS AMap renderers remain unimplemented and show the list fallback. The City API may select `amap` before a platform renderer is configured.

Local verification: Flutter analysis and the Web build completed earlier. The Android debug build completed using an isolated Gradle home, then `adb install --no-streaming -r` installed `app.civu.civu_mobile.birdtiepreview` without replacing the existing `app.civu.civu_mobile`. Android native Mapbox loading was visually confirmed on that device. iOS compilation and AMap rendering remain unverified. The final release requires the original Android signing identity and a versionCode greater than the current production release.


## 2026-10-16 当前国内地图接线与配置状态

前文“Native尚未实现”为历史快照。本轮已实现Native/Web内容地图适配，明确CN/CHN用AMap、其他已知国家用Mapbox；候选APK/Web35编译通过但未部署安装。WGS仅在显示/查询边界转换，174仅修上海/汕头provider，尚未生产执行。

Native adapter现在读取BIRDTIE_AMAP_ANDROID_KEY/BIRDTIE_AMAP_IOS_KEY编译配置；Android清单android.key需与同目标身份保持一致。预览包仍是app.civu.civu_mobile.birdtiepreview，已核APK34 SHA1仍为73:84:0A:17:D0:CA:7B:4B:F4:15:41:B4:2C:EC:B7:A2:34:D5:05:23。本机旧清单Key与Civu正式包一致，未证明可用于预览包；当前编译JSON未设置AMap Key。Web仍缺BIRDTIE_AMAP_WEB_PUBLIC_KEY和受信BIRDTIE_AMAP_SERVICE_HOST。安全密钥只应在代理服务端。

已用两个公开城市点核对旧Web Service Key，返回USER_KEY_RECYCLED/10013；这是另一平台，不能替代Android或JS Key，也不能据此判断Android Key失效。缺配置已向用户询问本机路径/控制台，未要求在聊天发密钥。真实国内地图对齐、原生SDK、Web SDK及性能仍NOT_RUN，源码/构建不能代替服务验收。

## 后续实际交付更新：APK40 / API31 / schema175

此前候选包/ADB离线记录为历史快照。当前40已替换安装，新高德Key和原Civu两份样式已在上海真机验证；直辖市城市尺度修复已入包。真实Civu owner会话、Aberdeen内容/图片/作者原时间、超过30秒后台后的地图恢复均已执行。巴黎外部目的地定位为APK39实际证据，40未重复。地图平移封面复用与性能限制、数据清理/隔离及社交缺口统一见 `docs/reports/BIRDTIE-DGS-EXECUTION-2026-10-16.md`；完整验收尚未完成，不能把旧NOT_RUN统称已通过。网页仍延期。
