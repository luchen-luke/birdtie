# CIVU Android 自定义底图

来源：`D:/Program/Civu/Civu-client/assets/map_styles/civu_android/`，原项目只读。
用户于 2026-10-16 明确授权 Birdtie Android 复用这两个样式文件；按字节原样复制。
原来源 README 记载：用户于 2026-09-05 提供 GeoHUB 导出 `mystyle_sdk_1788538778_0100.zip`，
面向 AMap Android 11.2.000 及以后版本，当时客户端使用 11.2.100。

导出样式关闭底图 POI 图标和文字；客户端仍保留道路文字、地图归属标识和受权限控制的 Birdtie 业务标记。
仅用于 Android，不将该导出当作 iOS 或 Web 样式，也不修改原 Civu 项目。

| 文件 | SHA-256 |
| --- | --- |
| `style.data` | `2e58328b9a676e23705f8a11ded22f9eb5d5482d1c2d1d00b9d67494ec25c8d1` |
| `style_extra.data` | `a2055f0e095badfbbb402eb5e61b49ec5a7ff71795be6600a6a55a92bb9e034b` |

运行时先校验并加载完整文件对，再创建高德地图。样式接线与静态摘要不能替代当前 SDK/Key 下的真实底图显示验收。
