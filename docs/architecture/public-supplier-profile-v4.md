# 公开组织与商家资料（BT-V4-ORG-003）

2026-10-03。这是 Birdtie 原生公开资料投影与明确批准协议，接续 070 商家资料审核、既有组织公开资料与 canonical ActivityOrganizer；不复制 Civu。

## 用户结果与入口

公众从 Now 的活动主办方或地点经营关系进入同一稳定 ID 的组织／商家资料，检查真实核验状态、当前获准公开的介绍和官方链接，再查看即将举行的公开活动。商家所有者从个人工作区 → 商家工作台 → 管理资料公开范围，预览、明确批准或撤回公开资料。组织工作身份不取得商家所有者权限。

复用既有 Material 页面、中文状态、商家工作台、ActivityDetailSheet 与 MapWorkspace 路由。EXTEND 组织页面的来源与身份复核；NEW 商家公开页面、所有者公开许可页及 supplierprofile 闭合投影。地图坐标仍由原地点／活动公开权限决定，本任务不增加组织 Pin 或推断地址。

## 三种许可分别生效

070 管理资料审核、072 面向所有公众的公开许可、Agent 数据使用许可是不同授权。已核验经营关系不自动公开 070 的介绍、营业时间、证明、审核人、联系人或预约地址，也不是服务质量、交易安全或 Birdtie 推荐背书。

公开许可绑定 business ID、许可版本、已核验 profile 版本、来源 hash、有限截止时间及当前本人所有者。来源含当前主体、经营申领、资料、提交人与独立审核人、审核授权和相关行版本，阻止 A→B→A 复活旧批准。修改资料、撤权、主体失效、审核期限届满或许可过期后不继续显示旧介绍／链接。

072 新增许可与审计，不迁移或回填旧 070 私密资料为公开。发布和撤回采用 CAS；完全相同的发布重试不追加审计。输入有效期规范为 UTC 微秒以匹配 PostgreSQL，纳秒请求的原样重试仍幂等。非空 down 拒绝并原子保留许可与审计；空 down 恢复 071，reapply 恢复 072。

## HTTP 与数据

- `GET /v1/businesses/{id}`：匿名可读当前公开商家基础身份；未批准资料返回 `profileStatus=unpublished`、空介绍／链接、资料版本 0 和空审核日期。商家不公开、无当前核验经营关系或身份失效时 404。
- `GET /v1/me/businesses/{id}/public-profile/permission`：当前原生个人 session 与本人商家所有者读取私密预览和许可版本。
- `PUT` 同一路径：闭合输入、明确 publish/revoke 动作与版本；未知／重复字段、错误目标、无授权主体、旧来源或期限无效不写。
- 组织继续使用既有公开资料路径，返回真实核验状态；未核验组织不伪装 verified。

公开业务投影限定 10 个字段。只携带 canonical 同一 BUSINESS／ORGANIZATION 主办方、明确 public、未来未取消且 Activity 与 City 均未过期的活动。canonical 组织 ID 同时投影到旧 organizationId 兼容字段，不从旧 nullable 字段猜主办方。活动详情仍使用原稳定 activity ID 和领域权限。

读取使用单一 SQL 当前来源投影，公开事务固定 UTC，避免连接默认时区改变 source hash 或 wire 日期。HTTP 在最后一次原生 Session 鉴权等待结束后重新读取完整来源，比较整份公开正文再输出。测试实际制造 Session UPDATE 锁等待并在等待期撤权，响应 404，不发送旧内容。响应 `Cache-Control: no-store`，拒绝多余 query、GET body 和非闭合目标。

## 客户端授权与动作

DTO 严格绑定请求目标、UTC 日期、公开活动主办方与 HTTPS 链接；账号／组织切换、组件更新、目标变化和迟到响应清除旧正文与批准。授权 getter 每次从当前 widget 取得，配合 identity epoch 防 A→B→A。

公开外链在点击后先重新 GET 同一来源、版本和有效期，才调用既有 launcher；打不开或异常显示中文恢复动作。所有者先检查完整预览，默认不勾选；确认前重新读当前许可和来源，PUT 后再读权威状态。写结果未知先 GET 对账，不能重发。撤回对话框绑定当前身份与目标，切身份时旧确认失效；取消不写。

## 核验与限制

适用 UX-CHECK-01/02/04–14/16：同 ID／领域动作、主体与公众受众／期限预览、撤权与迟到结果、幂等、取消和重新读取、小屏大字组件断言；无新增埋点采集私人内容。UX-CHECK-03 无澄清对话，直接检查已有资料；UX-CHECK-15 的未提示目标用户观察尚未运行。

当前根证据见 `docs/testing/evidence/public-supplier-profile-2026-10-03/root-final1/manifest.json`。原生定向 19 PASS；全 Go 8506 PASS/0 FAIL/SKIP，vet/build 成功；Flutter analyze/test/Debug build 成功，564 功能 PASS/0 FAIL/SKIP，189 client／640 API 源稳定。072 回滚与重放只按 DDL SHA 未变的 schema1 帧复用，不将其旧 Go 帧当最新功能回归。

真机 Pandora Android16，1220×2656，APK9：`d06fe8102c009f2f5f8ca6dc47b22afecf8b1ce717ce1e7f7492a50be2963551`。本地合成个人所有者检查资料并勾选时，许可／审计仍 0；确认后许可 1／审计 1。撤回取消保持 1／1；确认撤回为 2／2，匿名资料清空介绍／官方链接，现有公开活动仍由原权限保留。地点／活动主办方均进入同一商家页面，公开未来活动卡打开原 activity ID。

TalkBack、横屏、未提示真实用户观察、实际官方链接／预约服务、生产 IdP、CSSA 或真实经营授权、生产部署与 A→H 未运行。race 因 CGO=0／无 GCC 未运行。CODE_LOCAL 完成不表示 Closed Pilot 或 Consumer Beta 准备完成；两者仍 NO。
