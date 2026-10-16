# 高德直辖市目的地缩放修复

2026-10-16。父任务 APK39 真机反馈：Shanghai 返回上海市但选择后是省级视图（defaultZoom=7）。本 worker 源码核对确认 `DestinationSearch.kt` 原将 `level=省` 一律映射为 region。本 worker 未重复操作手机，未将该反馈写成自身真机执行。

现抽取可直接执行的纯 Kotlin `DestinationAdministrativeType.kt`；仅当 SDK level=省 且 adcode 精确等于 110000/120000/310000/500000，映射为 place，使用省字段中的直辖市名称，避免显示“市辖区”。名称缺失才回退 city/formatAddress。其他省保留 region，区县保持 district，不按行政代码前缀把整市辖区提升为城市，也不创建内部 City、POI 或公开位置。`DestinationSearch.kt` 实际使用该 mapper；没有改地图引擎、底图样式、坐标转换、搜索同意/取消或任何社交/API文件。

直接验证：

- `native02.json`：编译实际生产 mapper + `DestinationAdministrativeTypeTest.kt` 后运行 JVM 断言，12 个 case PASS、编译/执行退出码0、sourceStable=true。覆盖四直辖市、名称回退、普通省/市、上海区县、前缀不猜测、非目的地、provider边界及空名称。
- `flutter02.json`：既有 `amap_destination_search_test.dart` 3 项 PASS、退出码0、sourceStable=true。增加上海 place→zoom11 和普通省 region→zoom7 断言；保留显式会话同意、GCJ只转换一次、temporary/无internalCityID和取消迟到回包验证。仅 Mock MethodChannel。
- `native.json/native-compile.log` 保留首轮命令行失败：PowerShell 字符串相加未加括号导致参数合并，Java输出用法后exit1，未开始编译或测试；修正调用参数后才得到上述PASS。不是产品FAIL或测试通过。
- `flutter.json` 是首次3项通过，随后补省级zoom断言才得到flutter02；不是额外3项独立覆盖。

精确4项修改/新增路径与最终 SHA 见 `freeze.json`。本 worker 本修复真机/完整 APK 编译为 **NOT_RUN**；由父任务 APK40 合批后验证实际相机尺度。高德自定义样式是否生效不属于这次映射测试结论。
