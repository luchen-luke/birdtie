# Now 非城市查询：公开线上意图

2026-10-04；原任务 `BT-V4-NOW-001` 的增量实现。队列和最终状态由根代理核证；本文不替代发布门槛。

## 能力与现有来源

Now 的「选择查询情境」提供当前城市与本人明确声明的 ONLINE 情境。没有城市时仍可选择线上情境；没有声明时复用「我的情境」管理入口。`contexts.id` 是真实 UUID，`person_contexts` 仅表达本人私密的 interest/current/affiliation 声明，不证明群组成员身份、机构资格、经营权或机器读取许可。

线上查询是用户明确请求后的公开标题规则匹配。复用 `social_intents`、原 `agent_tasks` 与 native AGENT_TASK_COMPLETED 通知，不创建第二套任务账本、AgentRun、Memory、联系人或模型请求。当前只读同一真实 ONLINE Context 下 PUBLIC、ACTIVE、未到期且有限期限的 ONLINE 意图；作者须为活跃 Person，当前双向屏蔽均排除。PRIVATE、FRIENDS、DRAFT、CANCELLED、过期内容不会成为结果。最多 20 条，截断明示；标题字面匹配不是共同兴趣、距离排序、验证供给或运营背书。

该切片没有新增 DDL；复用已有 Context、声明、Intent 和 Task 的领域来源。原城市查询继续走已有 CITY 路径。COUNTRY/INSTITUTION/COMMUNITY 以及线下跨城市查询没有被本轮新增为通用路由；不从线上情境猜测城市或位置。

## 真实 HTTP 合同

仅有效本人 Person 会话；拒绝 Organization/Business 工作区，包括空的工作区头。所有 GET 拒绝 body/query，包括末尾裸 `?`。不存在 native port 时返回 503，不回退开发代理。

| 方法与路径 | 内容 |
| --- | --- |
| GET `/v1/me/now/online-contexts` | 本人当前声明的 ONLINE 节点，最多 20 个；仅 id/label/type |
| POST `/v1/me/now/online/tasks` | 严格四键 contextId/query/taskId/expectedTaskUpdatedAt |
| GET `/v1/me/now/online/tasks/{taskID}` | 本人原 Task 恢复；读取当前公开结果，不写 Task |
| GET `/v1/me/now/online/intents/{intentID}` | 原稳定 Intent ID 的当前公开详情；不创建 Task |

首次 POST 的 taskId/expectedTaskUpdatedAt 显式为空字符串。追问使用原 Task ID 和具体 updatedAt CAS，保留原首轮 query 与有界 conversation，filters.currentQuery 保存本轮文字。Task 为 person/本人 owner 与 actingUser、ONLINE/真实 Context ID，city_id 和 city_context_id 均 NULL；不伪造城市 Agent。

响应 schema 为 `now-public-online-query-v1`：context/query/answer/task（查询与恢复）/items/observedAt/modelAccess/promotion/truncated。Item 仅含 id/title/modality/contextId/sourceUpdatedAt/expiresAt；不含作者私密档案、constraints、平台联系方式、地点坐标、source seal 或 session 信息。`modelAccess=UNAVAILABLE`、`promotion=false`。公共详情同一最小投影，不将私人正文伪装成公开资料。

## 当前性与权限

native 先锁当前 Account、Personal Agent、具体 Context/声明、Intent/Task 来源；资源等待结束后检查当前 Session。响应正文与 native 来源 frame 经进程内 HMAC 封闭，HTTP 在序列化后再次原生复核完整来源，客户端不能制造读取回执。Account/Agent/Context/声明/Intent 的 xmin 和 Session 的 ID/创建时间绑定原读取；普通 idle 更新时间不会误撤销有效 Session。修改、撤回、替换、有限期限到期和双向屏蔽变化会拒绝旧来源或重新返回真空结果。

ONLINE 选项列表也有独立 sealed native 回执，不能仅复查 Session：组装后撤回声明、声明 ABA、Context 名称 ABA 或 Agent ABA 时 HTTP 拒绝旧私密情境标签。native final revalidation 不等于跨后续请求的永久授权；详情重新打开重新读取当前来源。

POST 超时、500/409 或成功 HTTP 却缺失合法 Task/主体/DTO 时表达「提交结果尚未确认」。客户端不自动重发，界面不提供盲重试按钮；用户从原最近对话读取权威 Task。没有收到 Task ID 时不能以 absence 猜提交失败，也没有影子任务账本。已知 401/403/404 与读取失败分别中文解释。

## Now 与地图

- 线上情境、活跃意图、中文规则回答和原 ID 卡片可见；先回答，再展示卡片，详情为明确的只读动作。
- ONLINE Result 的地图实体集合为空、MapEffects 为 null；MapCanvas Element、原 CITY 地图投影/Pin/选中卡片和相机 context key 保持，线上结果不清空地图或造新点位。账号切换仍清除旧账号任务投影。
- Task 恢复和追问使用原 ID/CAS。切换查询情境明确开始新任务；线上不执行 Search this area。
- 账号/组织变更、同 key client/base 重绑与 A→B→A 永久退休旧路线，监听 epoch 关闭嵌套详情；旧 getter 不能将原操作指向新身份。
- 结果模式为一个 CustomScrollView 的头部和有界内容，原 ListView shrinkWrap 且禁内滚，避免 320 宽/font3 长头部使 NestedScrollView body 无法稳定到达。48dp handle、原 detent、CITY 内容与对话模式保留。
- 对话也显示实际线上意图数量与「无地图点位」。详情是刚读取的公开快照，到期移除正文；不能将已读取快照理解为持续监控来源或邀请授权。

## 检查证据与未测范围

本轮独占 `work/v4-now001-online/native6/result.json`：fresh079 隔离库，定向 34 PASS/0 FAIL-SKIP/pkgFail0，test/vet/build0，783 全源稳定，原完整 public 行相同，owned DB 已 DROP。原 options-old-red2 四类真实 OLD HTTP200 泄漏标签（四子失败+父失败）和 new native5/native6 拒绝证据保留。

`work/v4-now001-online/client-final5/result.json`：定向 90 功能 + 11 loading PASS，analyze0、test0，257 Dart 前后 SHA 相同；包含真实 ONLINE 选择→回答→稳定详情→身份退休、同 key endpoint 重绑、未知提交不重发、旧 CITY/20 次焦点/布局回归。源冻结 SHA 见 source-freeze1.json。没有以先前整仓或手机 APK 代替当前切片结果。

UX-CHECK-01/02/04/05/06/07/08/09/10/11/12/13/14 的适用仓库检查在专属审计逐项映射。TalkBack、6 项独立真人消费测试、当前切片真机、App/API 真正进程重启及新整仓/build 由根代理另行核验，本文写入时 NOT_RUN。UX-CHECK-15 不由 widget 测试推为真人完成；没有新内容埋点（16），query/conversation 仍为原本人 Task 领域持久记录，不加入日志或 telemetry。

Closed Pilot Ready = NO；Consumer Beta = NO。真实线上供给/合作方授权/生产身份、HTTPS、地图与运营门槛不由 synthetic fixtures、公开规则查询或 Debug APK解除。原 NOW-004/活动参与转换与模型认知用途有独立依赖，不据本切片宣称已完成。
