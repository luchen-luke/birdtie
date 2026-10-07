# 商家预约资料与外部预约入口 V4

2026-10-03 · BT-V4-BIZ-004 · 本地实现及合成验收。

## 权威来源与公开边界

商家管理员提供预约 URL 的能力复用 070 Business Console：`VenueFacts.BookingURL`、owner/admin 鉴权、CAS 版本、独立审核、sourceUrl 和 validUntil。其管理资料不因填写 URL 而成为公开资料。不得将 note、rightsNote、reviewer 或私有 URL 从管理端直接带入匿名地点详情。

消费入口复用 040 已明确审核公开的 `GET /v1/places/{placeID}/venue`。`postgres/venues.go:GetPublicVenue` 用 PostgreSQL 当前时钟检查期限、批准 candidate、reviewer 绑定、公开地点和城市、有效关联组织，再读取当前有效商家经营关系。本轮不新增表、路由或预约授权，不将 070 与 040 两条独立来源自动合并。没有公开批准的商家资料不能以此路径露出。

返回的公开预约材料为 placeId、reservationSupport、reservationUrl（可选）、sourceUrl、reviewedAt、expiresAt。没有预约联系方式或实时空位事实时，界面明确显示未提供，不编造电话、邮箱、价格或可订状态。商家填写 URL 与公共 Venue 预约 URL 的发布归属保持各自审核流程；本功能不宣称已完成两者自动同步。

## 打开外部页面的流程

1. 地点详情显示有效预约资料的来源、审核时间和期限；HTTPS、精确地点 ID、闭合 support、合法日期均须满足。
2. 用户点击查看说明，控制器重新读取公共 Venue，形成不可从 JSON 伪造的本地具体材料预览。
3. 原生 Material 对话框展示完整 URL、来源、时效和外跳后果，用户可以取消或确认。
4. 确认时再次读取公共 Venue。URL、来源、审核时间、期限或支持方式任一变化，必须重新预览和确认；取消、404、无效或过期材料不启动链接。
5. 旧批准绑定账号、组织工作区、地点和本地 identity epoch；切换、往返 ABA、刷新、销毁或迟到响应不复用旧批准。批准消费一次；不自动重试外跳。
6. 外部 launcher 返回 false 或抛异常时显示中文失败。失效后提供“刷新预约资料”，新读取仍要具体确认，不直接打开旧链接。

这是导航到外部预约页面，不是原生预约、付费、库存检查或预约成功的凭证。外站已经启动后无法由本地账号变化撤回浏览器；再次操作仍须重新核验。客户端时钟仅保守拒绝已过期资料，不能代替服务端 PostgreSQL 授权和发布审核。浏览器打开与最后读取之间外站内容或服务端撤权可能继续变化，外部页面不是 Birdtie 的事务提交。

## 界面与接口协调

`VerifiedBookingController` 与 `VerifiedBookingSection` 接入 `PlaceDetailSheet`；单个地点详情拥有控制器及共享 HTTP client 的生命周期，不关闭外部传入的 client。账号/workspace listener 和 epoch 与现有地点读取共用。

`PlaceDetailSheet.onOpenBusiness` 是可选 `ValueChanged<String>`。已核验经营关系仅在严格非零 UUID 且提供回调时显示“商家资料”，传递原业务 ID。根代理拥有公共 Business 页及 map_workspace 路由；入口不导入管理工作台，不把管理页当作公开详情。动作使用 Wrap，避免经营关系一行多个按钮在手机挤压。

适用 UX-CHECK-05/06/07/08/09/10/11/12/14：身份、具体批准、当前材料、空/异常、直接路径、移动端和辅助技术。组件合成测试覆盖 360px 和 200% 字体，提供 liveRegion 错误文字与可滚动确认框；这些结果不能代替当前真机截图、TalkBack 或正式试点。

## 验证范围

最终本地帧：6 个 Dart 文件 analyze 通过；3 个测试文件 60 PASS、0 FAIL；源字节 SHA 前后相同。覆盖两次读取、材料变化、404、401/403/500、自然期限、身份/workspace/epoch 和迟到、取消、launcher false/异常、未知联系/空位、销毁、失效恢复及商家入口 ID/回调。额外修复已预览后销毁控制器的取消通知，以及启动浏览器过程中主体变化不得返回旧成功回执；不能撤回已启动的浏览器。58帧保留在 worker-final1，当前60帧另存 worker-final2，源文件与命令、输出见专属 evidence 档案。

040/070 原生事实仅复用既有实现与历史测试，不宣称本轮新增原生能力。完整 Flutter/Go/build 由根统一冻结回归；本任务未运行真实外站访问、真实预订、真实 IdP、CSSA A→H、TalkBack、App/API 重启或正式发布。Closed Pilot 仍为 NO。

## 2026-10-05 增量：BT-V4-BKG-001 外跳报告

本节为本地代码验收增量，保留上面的 BIZ-004 历史范围。仍只使用040已审核公开Venue，不读取070私有管理预约URL。新084表 `booking_external_events` 与原Activity统计分开，Place不能写入具有Activity外键的统计。新接口为 `POST /v1/places/{placeID}/booking-events`。

### 当前来源与具体期限

公共Venue改用同一原生事务捕获公开地点/城市、批准candidate、reviewer绑定、Venue及有效公开经营关系。PostgreSQL当前时钟、实际行版本和最早来源期限参与封闭回执；HTTP先编码，再原生复查当前来源和身份，最后返回。撤回再恢复的行版本变化、隐藏地点、已到期城市/资料和无公开预约入口均拒绝。内部seal/proof不返回；客户端只得到opaque `bookingSourceRevision`、签定原期限的 `bookingSourceVersion` 及 observedAt/validUntil。这些值是并发条件，不是授权。

无Authorization时仍可匿名读取公开入口；带失效Authorization不能降级匿名。本人会话的正常idle刷新不当作撤权；原RevokeSession的撤销、自然期限及新Session绑定受到核验。原API没有恢复已撤销Session的功能，不能声称可识别管理员直接恢复所有原安全字节的任意SQL操作。会话读锁和最终来源复查给出本地操作的线性化边界，不能撤回已打开的外站。

确认仍展示具体URL、来源、时效和后果。客户端的第二次读取必须与预览来源revision一致，不能用新读取续签原批准期限；账号/工作区/epoch变化、来源变化、取消或过期不打开链接。短期限自然到期会停用旧确认。打开失败或异常不报告事件；打开后身份或期限已失效也不提交旧统计。窄屏确认使用可滚动标题和正文，操作目标至少48dp，不缩小文字。

### 统计含义、隐私与保留

事件仅固定为 `EXTERNAL_BOOKING_CLICK` / `CLIENT_REPORTED_EXTERNAL_OPEN`：客户端报告外跳，服务端不能证明浏览器实际成功打开，更不能证明供应商接受预约。UUID去重和当前来源校验不会把报告变成交易凭证。返回 `confirmedCapability=UNAVAILABLE`、`confirmedBooking=UNKNOWN`，不显示“0笔已验证预订”。

表只保存eventId、placeId、固定事件/结果和服务端recordedAt，不保存账号、会话、token、URL、query、坐标、私密正文或proof；不新增公开统计或管理权限接口。用途是内部评估公开预约入口。保留政策为30天，原生内部清理每次最多1000行；本轮只验证内部SQL清理函数，尚无部署调度器或运营保留验收。内部SQL统计应称“客户端报告次数”。取消、打不开、迟到、自然到期、源撤权和失败事务零事件；未知提交不盲重试。统计失败不倒推外站没有打开，界面明确外跳统计未确认且预约结果须向对方核实。

### 本轮证据与限制

定向schema084原生 `native7`：35 PASS、0 FAIL/SKIP，test/vet/build均0，868复制源稳定；完整public/catalog前后相同，自有测试库DROP返回0。定向Flutter四文件87 PASS，七项analyze无问题。实际旧City到期仍泄露预约资料的原生RED、无Venue port panic RED及320px/font3/IME260确认框overflow RED均保留，修复后的原测试与负向回归通过。真PostgreSQL测试覆盖唯一/FK或来源表等待后的撤权、来源ABA、自然来源/会话到期、ctx取消、0残留事件、原期限不可延长、时区和私有管理表未读取。

原native7 runner未保存随机自有库精确名称，空create/drop日志不能支持按名称独立核证；不得补造该名称。其DROP仅有实际返回码证据。冻结回执/命令/原始失败见 `docs/testing/evidence/booking-analytics-2026-10-05/` 及 `work/v4-bkg001-20261005/`。server注册在冻结后移交PLN，历史SHA只表示该schema084帧；joint085及完整Flutter/build由根代理另行验收，不沿用旧绿替代。

外部URL访问、真实预订/供应商receipt、真实合作方、生产身份、TalkBack、正式部署及运营均NOT_RUN。没有原生预订或已确认交易能力。Closed Pilot / Consumer Beta均NO。

归档安全检查点补验：`native8` 从已经冻结的 `native7-frame` 原字节复制执行，没有从移动中的live读取。schema084再次35 PASS、0 FAIL/SKIP，test/vet/build0、868源稳定/public/catalog保持。创建前保存精确ownership、CREATE与DROP实际argv/SQL/returncode；自有库为 `birdtie_bkg001_native_4ac0dfc118d6`，DROP返回0，可由根代理独立查不存在。此补验不修改产品，不改变native7随机库名称缺失的历史记录，也不代表尚未运行的joint085。
