# Android CIVU 底图样式复用

本批按用户 2026-10-16 明确授权，仅将原用户 GeoHUB Android 导出的 `style.data`、`style_extra.data` 原样接入 Birdtie。两文件来源、字节数和 SHA-256 见 `asset-provenance.json`，仓内来源说明见 `apps/client/assets/map_styles/civu_android/README.md`。原 Civu 项目保持只读，没有复制导航、状态、iOS/Web 样式或其他业务代码；当前 Android Key 没有被读取、覆盖或写入证据。

只读参考 `Civu-client/lib/src/services/amap_offline_style.dart` 和 `widgets/civu_explore_map_amap.dart:423/817/944`：借鉴两个本地文件先就绪、初次地图携带 `customStyleOptions` 的接线方式。Birdtie 自有 loader 额外验证原始文件摘要，共享成功的加载结果，失败可重新加载；界面明确显示失败和重试，不使用默认图作为静默回退。首次平台地图创建需 Key 检测、完整样式和已有明确服务同意同时满足。地图重建 key、读取/媒体授权、marker、坐标、相机逻辑均沿用原实现。

原样式 README 声明 GeoHUB 导出关闭 POI 图标和文字。客户端继续 `labelsEnabled: true` 保留道路文字，继续 `touchPoiEnabled: false`，地图归属组件未改。实际当前 SDK 下是否准确隐藏各类底图 POI，须由根任务 APK37 真机验证，不能从这份样式说明直接认定通过。

在 `D:/Project/birdtie/apps/client` 仅执行 `flutter test --no-pub test/amap_offline_style_test.dart --reporter expanded`。最终 4 项 PASS、exit 0、7 个本批修改文件的前后摘要一致。覆盖实际 bundled assets 摘要、共享并发/成功缓存、ByteData offset、损坏文件对拒绝和重试，以及真实 AMapWidget 在 mock 平台工厂下携带完整样式、同意后创建、null Key override、道路文字和同 widget state 保持。

首轮 widget 断言全部完成后，测试 cleanup 恢复上游默认 platform 实例触发其 token 校验失败。原失败保存在 `attempt01-receipt.json` / `direct-tests01.txt`；去掉该单文件隔离测试内无效的替身恢复操作后，`direct-tests02.txt` 为完整通过结果。没有删除首轮证据，没有以 mock 平台替代真机渲染结论。

适用 UX-CHECK-02/06/07/09/10/12/13/16：复用获准资料、区分加载与失败、重试/迟到响应安全、不重建已有地图、不记录 Key 和用户内容。APK 构建安装、当前 Key 鉴权、真实国内地图 POI/道路文字/业务点显示均由根任务统一验证，本 worker NOT_RUN；iOS/Web 不属本次改动范围。
