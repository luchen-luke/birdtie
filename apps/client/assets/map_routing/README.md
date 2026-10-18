# 地图服务区域判定数据

本文件仅供 Birdtie 判断当前浏览目标所适用的地图服务，不绘制行政边界，不建立城市/地点，不作主权或地理事实声明。

- 官方数据：Natural Earth **Admin 0 – Map units 5.1.1**，298 个 map units。
- 官方版本页：https://www.naturalearthdata.com/downloads/10m-cultural-vectors/10m-admin-0-details/
- 实际原始下载：https://naciscdn.org/naturalearth/10m/cultural/ne_10m_admin_0_map_units.zip
- ZIP SHA256：`45bebe2aaf8bf42b9daf4428594925bcb11eabd32fe9dc6e0acf681438053eb5`，5,003,719 bytes。
- 许可：public domain，官方说明 https://www.naturalearthdata.com/about/terms-of-use/ 。
- 原始投影：归档 `.prj` 的 GCS_WGS_1984；版本取同归档 `.VERSION.txt`，不是依网页日期推断。
- 派生版本：`natural-earth-map-units-5.1.1-wgs84-q1e6-v1`。
- 派生 JSON SHA256：`37858edb3ca811e4b2de7a85b44803b753da7b9e74113cfec66ac1934ff4cb55`，6,682,709 bytes。

转换方法：按 SHP/DBF record 顺序保留全部 polygon parts、洞和原始 552,828 顶点；仅将 WGS84 四舍五入为 1e-6 度并去除量化后相邻重复点，每个 ring 用有符号整数差分编码。不做 Douglas–Peucker 简化、不按名称猜区域，也不使用坐标转换函数的矩形包络判定归属。`code` 保留 `GU_A3`；`iso`/`iso3` 保留去填充后的源字段（源 TW map unit 的 ISO_A2 为 CN-TW），不能把它们写回业务数据。

路由约定：CHN map unit 为高德；HKG、MAC、TWN 及其他明确陆地 map units 为 Mapbox。这是应用服务适配约定。判定必须通过实际 ring，bounds 只作加速；洞、海上、离岸/边界 0.5 km 留白、重叠歧义及无法可靠展开的极区环返回 unknown，保留最近明确目标/可用地图。0.5 km 是产品保守留白，**不是**本源数据精度保证。1:10m 指比例尺 1:10,000,000，不是 10 米精度；局部边界以明确、获准的结构化导航目标为优先。

世界级视口依据真实可见经纬跨度判断，不能将不同 SDK 的同一 zoom 当作相同覆盖范围。明确的目录/地点目标不必等自由拖动的防抖。原始 Civu 坐标、业务记录和供应商 Temporary 搜索结果都不因这份路由数据被覆写或持久化。

完整原始归档、PRJ、可复现的 Python 标准库派生脚本及回执保存在本轮忽略的 `work/pin-automap-20261018/routing/`；不将归档作为应用资产打包。
