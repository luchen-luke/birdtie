# Birdtie 餐厅自然语言查询修复

日期：2026-10-10（Asia/Shanghai）。用户原句：**有没有好的餐厅**。城市：Aberdeen。

本轮完成了这条问法的类别识别、实际联网餐厅查询、同轮结果卡片、地图位置与外部来源详情，并已将新 APK 安装到原手机。历史已有实现包含各个片段，但用户所期待的公开餐厅发现流程此前没有接通；不能把旧代码存在或队列 DONE 当作这条产品流程已完成。

## 原因

1. 原餐厅推荐识别只接受少量固定晚餐问法；用户原句落入地点文字匹配，留下“有没有好 餐厅”一类残词，与餐厅名称无法匹配。
2. 实际公共城市目录只返回 3 个文化地点，没有有效餐厅。数据库中此前采集的 15 个真实 OSM 餐厅已超过 24 小时有效期（2026-10-10 03:18 上海时间截止）；另有合成测试地点不能充当真实推荐。读取期限检查正常工作，不应通过修改日期让旧数据重新变成最新事实。
3. 既有本地事实／模型回答依赖特定登录 owner 与许可字段；本机运行的 licensed-place 策略只读取站内事实并调用模型，没有联网搜索。原腾讯网页搜索路径另有 SearchRetention=PENDING 边界，本轮没有擅自开启。
4. 既有网页回答支持文字引用，但没有外部餐厅候选的卡片和地图协议；即使文字回答成功，零原生实体还会被前端标为空结果。

## 实施

- 识别常见中英文餐厅推荐／查找问法，将用户原句解析为当前城市 `restaurant` 类别，清空整句文字匹配词。当前 Aberdeen 的“阿伯丁／阿布丁／Aberdeen”名称也能作为城市范围识别；其他名称与预算、菜系、时段、预订等未消费条件不被静默删除。
- 访客和登录账号均可进行一轮即时公开查询：使用真实城市目录中心和固定餐厅类别，访问现有 Overpass transport。提供方只收到城市公共坐标和类别，不收到用户原句、身份、私有偏好或手机 GPS。
- 当前覆盖城市中心周边 6 公里、最多 15 个带名称的 OSM node；明确地图范围查询再做边界过滤。没有地图范围的“附近”请求先提示选择区域，不擅自当作全城。
- 新 `onlinePlaceSearch` 协议绑定本轮 requestId、query、cityId。外部来源 ID、URL、WGS84 坐标、署名、ODbL 权利、字段一致性和新鲜度均校验。有效期最多 10 分钟，并取最早字段期限；编码后最终释放前再次检查。
- 外部卡片、地图 Pin、选中预览和来源详情使用同一候选，独立 ID 为 `external-place:osm:node:<id>`。点击原始来源打开精确 OSM node；公开结果底部提供 OSM 数据许可入口。未把候选伪造为站内已发布 UUID 地点，也未自动导入或授予收藏／发布权限。
- 候选只保留于当前客户端轮次内存。城市、账号、上下文、轮次切换或到期使其失效；历史 GET 不联网、不重建外部事实，也不重新挂接当前站内结果。持久 Task 只保存本轮成功／空／失败的一般说明，不保存供应商地点事实。
- 联网成功、真实空结果和来源不可用分别说明；不再以站内 0 个地点代表线上没有餐厅。有有效来源文字回答但无原生实体时，前端正确标为回答成功。

联网来源依据 [OSM 官方开放数据与署名说明](https://www.openstreetmap.org/copyright)；固定范围查询依据 [Overpass QL 文档](https://wiki.openstreetmap.org/wiki/Overpass_API/Overpass_QL)。另外核对了 [Frankie & Benny’s 官方餐厅目录](https://www.frankieandbennys.com/restaurants) 中的 Aberdeen Union Square 和 [Six By Nico Aberdeen 官方页面](https://www.sixbynico.co.uk/aberdeen/booking/)；这只能补充部分店名的存在依据，不能证明所有候选的营业状态或餐饮质量，也未把网页内容硬编码成产品评分。

## 实际验证

| 层级 | 实际结果 |
| --- | --- |
| Parser | 定向 Go 包测试 exit 0，覆盖原句／中英文／范围／保留未知约束／既有私有晚餐边界 |
| HTTP | 11 个主测试、34 个子测试通过，exit 0；含来源失败／空结果／字段与权利校验／零导入／实际 NativeStore 历史 seam／编码后到期拒绝 |
| Client | 3 个直接相关测试文件共 78 项通过，exit 0；新文件 34 项，含真实 MapWorkspace 原句、地图选中及详情 widget |
| Dart 分析 | 0 error；1 个既有 warning、20 个既有 info，实际 exit 1，没有换算成全绿 |
| 最终 API | build-02 编译成功，构建期间生产源码 hash 不变；实际 guest 原句和 signed“阿布丁有没有好的餐厅”均 HTTP 200，SUCCESS、15 家、站内地点 0 |
| 数据边界 | 真实请求前后 candidates=38、sourcePermissions=35、placeFields=69、modelRuns=10 均未变；没有导入地点或调用模型 |
| 最终历史 | signed GET 无外部候选、原生 items=0，并提示重新搜索查看最新结果 |
| APK／手机 | 一次本轮 APK 构建成功，源码 hash 不变；install -r 成功，保留数据。原手机实际中文输入原句，联网回答、餐厅卡片、地图定位、选中预览、来源详情通过；原始来源按钮打开 Chrome，已返回 Birdtie |

API SHA256：`d3306cd447111ab93205ad106993d6dee74d4225863ea0585738751cc049c02f`。

APK SHA256：`04757d06011e3e33b1f53be1b311e2a1b44089db4caaa5cd0818573911fbead8`；截图均记录同一安装包 hash、手机进程和时间。

本轮 API 先有 build-01 的真实联网检查，再经历史绑定及最终释放回归修正形成 build-02；早期失败／通过证据保留，没有覆盖成最终结果。根编译失败、客户端早期测试失败与既有分析发现均已记录。未运行全仓测试、数据库迁移或录屏。

证据：

- [HTTP 测试与保留失败记录](../testing/evidence/restaurant-discovery-2026-10-10/backend/README.md)
- [客户端测试与分析原状](../testing/evidence/restaurant-discovery-2026-10-10/client/README.md)
- [最终真实 API／数据库计数核对](../testing/evidence/restaurant-discovery-2026-10-10/api-03/receipt.json)
- [原句提交前与真实键盘](../testing/evidence/restaurant-discovery-2026-10-10/phone/original-query-committed-keyboard.png)
- [回答与地图](../testing/evidence/restaurant-discovery-2026-10-10/phone/original-query-online-answer-map.png)
- [餐厅卡片](../testing/evidence/restaurant-discovery-2026-10-10/phone/online-restaurant-cards.png)
- [地图选中后同一来源详情](../testing/evidence/restaurant-discovery-2026-10-10/phone/selected-pin-external-source-detail.png)
- [最终已返回 Birdtie 的选中地图](../testing/evidence/restaurant-discovery-2026-10-10/phone/final-restaurant-map-after-source.png)

## 能力及验收边界

本轮交付是当前城市的公开餐厅发现。建议名单按距离市中心的事实排序，界面明确尚未核验评分与当前营业状态。它不代表通用网页搜索、任意复杂自然语言、预算／菜系／个人偏好的质量排名或预订功能均已实现；这些不能计为本轮完成。

当前依旧是本地预览，API 及 USB 隧道由上一轮隐藏监督器保持运行；手机独立离开电脑需要在线后端，本轮没有发布公共服务。

适用 UX-CHECK-01/02/04/06/10/12/13 已有针对性代码与实际流程证据。UX-CHECK-14 的当前设备键盘、列表、详情和地图截图已验证，但独立读屏、多设备／额外字号及真实来源失败／空状态布局未运行；UX-CHECK-15 独立用户无讲解可用性观察未运行。UX-CHECK-16 本轮无新增聊天／位置遥测，公开供应商事实没有进入持久 Task。局部真机验证不宣称完整发布验收。

保留现有未提交工作及原队列状态，未更改旧任务 DONE、原暂停目标、生产数据库许可或发布门槛。
