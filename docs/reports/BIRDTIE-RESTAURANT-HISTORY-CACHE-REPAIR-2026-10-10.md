# Birdtie 餐厅历史、统一卡片和公共资料缓存修复

日期：2026-10-10。正式仓库：`D:\Project\birdtie`。本次接续城市启动和餐厅自然语言检索修复；旧餐厅修复报告中“十分钟临时结果、不长期保存”的行为由本次 v2 实现替代。最终版本已经保留数据安装到原手机，并通过访客强制关闭后重开验收。

## 问题原因

此前联网地点属于有时限的临时结果。界面把“资料需要更新”和“结果不能再显示”绑定起来，新一轮查询也会退役先前的结果对象；对话没有保存每一轮的完整地点资料。地点列表和地图选中预览使用不同组件，所以只有选中地点像卡片。此问题来自实现，并非本地运行方式必然导致。

增加访客设备存储后，真机验收又发现启动缺口：MapWorkspace 仅为登录用户加载最近对话；城市和身份初始化还可能在异步读取中使历史列表失效。这两项已经补齐，并用真实页面挂载和手机重开验证。

## 实现范围

- `birdtie-online-places-v2` 为每轮回答保存原始文字、完整地点快照、采集时间、来源和原消息绑定。资料新鲜度期限最多 24 小时；超过期限仍保留原回答和卡片，提示可以更新。
- 登录对话的每条助手消息在服务端 Task conversation 中保存 `externalPlaceReply`。历史 GET 恢复原轮次，保留原始文字和来源时间，不重新生成回答。
- 含 v2 餐厅快照的访客 CITY 对话写入手机 FlutterSecureStorage，按 API 环境隔离；访客历史属于该设备。启动和退出登录后均加载“最近对话”，随后选择记录可恢复原卡片。每个地点和每次恢复仍核验原城市、来源及消息绑定。
- 所有返回餐厅和地图选中预览复用 `ExternalPlaceCandidateCard`，显示名称、地址、来源和详情入口；列表卡片提供地图动作。
- PostgreSQL 迁移 116 新增 `external_public_places` 和 `external_public_place_search_cache`。前者按公开提供方地点 ID 去重保存事实，后者保存城市、类别、公共中心、半径和策略版本的整批资料。公共缓存不保存账号、会话 ID 或用户原话。
- 首次没有相同范围缓存时联网采集。重复范围优先使用数据库，包括需要更新的旧资料；用户选择“更新餐厅信息”时主动重查，更新失败则保留已有资料。读取旧资料不会重置采集时间。单 API 进程内相同请求合并联网调用，包括并发失败结果。
- 外部地点保存在公开资料库，状态 UNREVIEWED，不自动发布为 Birdtie 原生地点。资料来自 OpenStreetMap，保存 ODbL 链接、署名和原始地点来源。

## 验证结果

| 验证 | 结果和证据 |
| --- | --- |
| 数据库迁移 | 在本地预览实际使用的 `birdtie_ui_live_20261008_b160550993` 应用 additive 116，退出码 0；[database-01](../testing/evidence/restaurant-history-cache-2026-10-10/database-01/receipt.json)。 |
| 首次实际联网和复用 | Aberdeen 原句“有没有好的餐厅”返回 15 家，首次 LIVE；随后游客、登录两轮和历史 GET 均复用同一采集时间。公开资料从 0 增至 15 条、缓存批次 0 增至 1；[api-01](../testing/evidence/restaurant-history-cache-2026-10-10/api-01/receipt.json)。 |
| 后端重启后复用 | 重启本地 API 后缓存仍为版本 1，仍返回同一 15 家；[api-02](../testing/evidence/restaurant-history-cache-2026-10-10/api-02/receipt.json)。 |
| 最终 API 实际协议 | guest、登录、第二轮均 CACHE，历史为 HISTORY；登录对话 4 条消息带 2 份完整快照，未登录读账号历史返回 401。模型调用累计 10→10，原生地点 22→22；[api-03](../testing/evidence/restaurant-history-cache-2026-10-10/api-03/receipt.json)。 |
| 后端窄测试 | 60 个通过事件，退出码 0，仅运行餐厅、缓存和原消息保存相关测试。生产源码与 API build02 一致；[backend-03](../testing/evidence/restaurant-history-cache-2026-10-10/backend-03/receipt.json)。 |
| 客户端窄测试 | 已执行 99 个独立测试通过，含 100 天后的资料仍可读、两轮 6 张相同卡片、真实 Go 协议、51 份本机历史无自动清理、存储写入失败保留旧内容、A→B→A 城市恢复和迟到响应不能写入；[client](../testing/evidence/restaurant-history-cache-2026-10-10/client/README.md)。 |
| 真实页面启动测试 | 3 项 MapWorkspace 测试覆盖游客启动、城市 null→selected 时仍在等待设备存储、签出时身份 listener 次序；均从侧边栏恢复 15 张卡片，餐厅历史恢复不发 HTTP。 |
| 真机保留数据更新和恢复 | APK03 `install -r` 后，访客侧边栏出现原记录。打开后能读两轮原回答和卡片、原采集时间 06:23；地图选中恢复为相同卡片。Android 强制停止并重启后，再次从原记录恢复成功；[phone](../testing/evidence/restaurant-history-cache-2026-10-10/phone/README.md)。 |
| 最终源代码与运行版本 | APK、API 构建均成功，构建期间源码未变化，最终源码再次匹配构建清单，手机截图 APK 哈希一致。真机验收前后缓存版本 1、地点 15、模型调用 10 均未变化，API 和手机隧道均 ready；[最终验收](../testing/evidence/restaurant-history-cache-2026-10-10/final/receipt.json)。 |

最终 APK SHA-256：`fb7ad25620d34c84556f86f33b942a611f31e33d2fe4a684e844a94dc7acdf7c`。当前 API SHA-256：`d5991eaa7f8b8aaccf05a802b751bfc11117318ceca16c9c8a6c62655f330bad`。完整构建清单在 [APK03](../testing/evidence/restaurant-history-cache-2026-10-10/final/client-build-03.json) 和 [API02](../testing/evidence/restaurant-history-cache-2026-10-10/final/api-build-02.json)。

此前 APK01/02 为候选验证记录。APK02 的访客列表未加载失败已保留，不能将它当作成功验收；最终关闭依据是 APK03。迁移和缓存写入为真实数据库操作；后端锁和权限边界的定向测试使用 mock，不宣称完整真实并发 SQL 验收。

## 范围与限制

本次持久化覆盖已接通的公开餐厅检索链路，并非全部联网领域、全部游客聊天或 ONLINE/组织会话的通用历史。登录餐厅对话在账号对应的服务端 Task 保存；游客记录仅属于本机和同一 API 环境，换设备、卸载或清除应用数据不能据此承诺恢复。

资料覆盖当前城市公共中心周边 6 公里、最多 15 家，按距离整理；尚未核验评分或当前营业状态，不能宣称已评选“最佳餐厅”。数据库复用和模型调用计数已验证，未量化实际账单节省。

旧 v1 回答没有保存完整地点事实，无法补回此前已丢失的列表；重新查询后使用 v2。游客存储没有按时间或数量自动删除，8 MiB 或单会话 1000 条消息安全上限触发时，新写入明确提示失败并保留旧数据。登录“最近对话”现有列表最多 50 条，更早 Task 仍在数据库，可以按 ID 读取；本轮未增加无限历史分页。

24 小时以上的新鲜度变化通过可控时间的窄测试验证，没有让真机等待 24 小时。真实手机未强制联网更新或模拟提供方故障；这两项由定向 mock 测试覆盖。并发合并限于单个 API 进程。未运行仓库全量测试；选择性分析零 error，既有 MapWorkspace 未使用方法 warning 和 info 仍存在。本次仅采集原始截图和界面树，没有录屏。

