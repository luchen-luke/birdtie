# Now 情境查看选择合同（V4 / NOW006）

2026-10-04。来源：原 BT-V4-NOW-006、CONTEXT-GRAPH-V4.md、NOW-NON-CITY-QUERY-V4.md、GLOBAL-UX-INTERACTION-CONTRACT.md。本合同描述本人查看选择，不改变原 ContextGraph 声明、身份、Tie 或领域许可。

## 唯一源与语义

- contexts / person_contexts 原生声明继续为唯一声明真源。仅读取当前本人行，使用原 Context ID、relation、visibility 和 native xmin/createdAt。
- 发布且仍有效的 City 与 active CityContext 可以选择为目的地浏览。未声明选项是 DESTINATION，不代表当前位置、GPS、居住或到访。
- 本人 City 的 current/destination/home/past 明确分别标记；自己的 ONLINE、INSTITUTION、COUNTRY 不借名称改成 City。机构与社群声明不证明学历或成员资格。
- Community 首版仅本人 interest；必须当前 PUBLIC/published/active、ownerConfirmed、未到期、关联 City（存在时）已发布有效、owner active、双方无 Block。不显示外国私密声明。无 City 的真实社群可以查看，不推断地图位置。
- CITY 复用原 City 查询；ONLINE 仅原 Now001 可接受的本人 private interest/current/affiliation。其他声明仅 UNAVAILABLE 查看，不能偷偷读取或创建查询权限。

## 两个只读端口

- GET `/v1/me/now/context-selection/options`：无 body/query，只在个人工作区返回 Options。
- POST `/v1/me/now/context-selection/resolve`：只接收 `optionsToken` 与 `optionId`；对原本轮具体 Option 返回当前 Selection。
- Envelope 闭集 `now-context-selection-v1`，owner PERSON/原 ID、PersonalAgent、native observedAt/expiresAt、viewOnly=true、modelAccess=false、sendAllowed=false。limit100/truncated 明确为有界列表。
- Options 的 AES-GCM 随机进程 key/nonce 密封了当前稳定 Session、Account/Agent/metadata 与最小来源版本 frame；Options 期限至多90秒并受当前源和 Session 实际期限收紧。Resolve 不续期，进程重启旧 token 失效。
- 这是可重复核验的只读选择，不是一次效应批准、机器 purpose grant 或持久回执；不新增 DDL/授权账本/后台写入。

## 原生最终边界

`postgres/now_context_selection.go` 的 capture 使用一条 SQL 和 MATERIALIZED PG clock 同时捕获当前身份、source/ACL、Block、expiry、最小投影。先取所需关系 ACCESS SHARE，最后当前 Session FOR SHARE 的真实等待结束后再次同 SQL 捕获并比较完整 bounded source frame。HTTP 先编码闭集响应，再真实 Revalidate，最后才输出200；末检查之后不再次认证或等待源表。

Session 稳定绑定排除正常 idle 滑动刷新；仍在每次当前观察核实际 idle/absolute expiry、revocation、原认证方法。source xmin、声明删除重建、原 metadata 版本及 Agent 变化令原 token 失效。当前 Block 被最终 SQL检查；不宣称存在不可逆 Block 历史 epoch，也不宣称防御特权 SQL 任意恢复 Session。

当前有效仅指最终 native 观察时点；不把返回内容变成网络传播期间永恒许可。全 bounded frame 保守变化会要求刷新，即使变化不在所选选项中。

## 消费组件与宿主契约

- NowContextSelectionAPI.options/resolve：严格本轮 owner/Agent/Option.same 与日期，接受真实 RFC3339 Z/±offset，GET/POST各12秒有界等待，无自动 POST 重试。借用 client 不关闭，自有 client 只关闭一次。
- Controller 的账号、token、工作区 A→B→A 永久退休旧 options/pending，source到期及时清除；未知 Resolve 只显示中文刷新重选，不执行领域动作或盲重发。
- Page 的同 key auth/client/base/listener/getter替换退休旧控制器；父组件 build 中身份变化延后 UI setState但同步失效数据。ListView/SafeArea 与48dp操作支持320×640、font3、IME220及语义。
- Page.onSelect 仅交付 NowContextChoice；宿主单独保存 viewChoice，不清 Task/Intent/ResultSet/Pin/Camera/身份/Ties。下一次明确输入提交必须 fresh options→原 Option.same→resolve，再调用原 CITY/ONLINE查询；失败保持原工作区与输入。UNAVAILABLE不得查询。
- 可选 onOpenOnlineOpportunities 仅明确点击“查找线上活动与伙伴”后调用宿主原 NOW005 页，不要求 City/GPS、不自动启用任何来源。

## UX 检查与实际证据

适用 UX-CHECK-01 至16：中文明确范围/来源、原生有界选择与缺项、不猜所在地/成员、只读具体核验和真实结果、完整身份/迟到响应失效、未知可刷新、异常/空/组织/匿名状态、直接路径、移动/语义检查、保持原 Intent/地图、增量复用及证据门槛。相关系统/消息/发布动作不在这个只读选择页新增。

`work/v4-now006-context-switch/freeze1.json` 列13当前源 SHA；native4为34 TestPASS/0FAIL-SKIP，目标 test/vet/build0，复制836源稳定、完整非空 public/catalog 保留、自有数据库 DROP；client-target3为15功能+3loading PASS/0failSkip、六源analyze0。原注册HTTP实际 JSON 在本 evidence/native-wire1.json，被真实 Dart 严格解析。

全仓与宿主 Map 集成/Flutter Build/真机由根代理共同冻结后独立验收；本 worker 尚未跑这些检查。TalkBack/真实生产 IdP/外部部署/CSSA 试点 NOT_RUN；Closed Pilot Ready/Consumer Beta均NO。局部目标通过不替代整仓门槛。
