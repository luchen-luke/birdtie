# V4 七类 Agent 结果契约与原生来源

BT-V4-NOW-004；2026-10-04。本文件是该任务的增量规范，复用原 Task、ResultSet、领域详情、聊天及保存 API。完成类别为 CODE_AND_LOCAL_VERIFICATION，生产和真实试点条件不由这个契约解除。

## 用户流程

本人在 Now 明确查询 → 返回中文规则回答及结构化结果 → 选择卡片 → 打开同一原领域对象。详情、分享、保存分别调用已有领域 API，并再次检查当前权限。检索不会自动报名、邀请、加入组织、发送消息或写 Memory。

CITY 查询沿用真实 City/Task。ONLINE 沿用独立的本人情境查询；不将它转换成假 City 或地图点。新明确查询包括“找公开成员”“找社区”“找商家”“我的社交机会”。没有实现机会额外筛选时，会明确拒绝“今晚”“近一点”等条件，而不是丢弃条件后称为筛选结果。

## 单一投影

`typed-agent-results-v1` 的 `items` 驱动卡片、可选地图点、详情和分享。每个条目包含闭集 `entityRef`、标题、摘要、scope、原 `detailRef`/`shareRef` 和可选明确公开 anchor。原 ID 不因视图变更而重建。

原生读取在同一最终 SQL 中复用原 activityColumns / placeColumns 和原解码器，捕获完整获准 Activity/Place 兼容 DTO，并与 Items 一起密封。旧 wire 数组保留价格、日程、来源、受众等真实字段；typed 只从同批 Items 派生，不重复投影第二个 Ref。其他非原生兼容投影存在同 Ref 歧义时，保留原规则：删除冲突双方。旧 generic Group 不转换为 Community，也不获得新的领域动作。

| 类型 | 实际原生来源及边界 | 详情与分享 |
| --- | --- | --- |
| Person | 有效且本人确认的公开 Intent、公开个人资料及原字段 ACL、当前账号、双向屏蔽检查；不投影个人精确位置 | 原 Account ID |
| Activity | 原 `birdtie_activity_visible_to`：公开或本人当前获邀请/成员权限的已发布活动；主办方、账号、Place、商业场地关系及期限仍有效 | 原 Activity ID |
| Place | 当前已发布且未过期的地点；只有明确 wgs84 point 坐标可产生点位 | 原 Place ID |
| Community | 真 communities 原生实体，公开、已发布、活跃、本人确认，当前创建者与屏蔽条件；不猜旧 Group 来源 | 原 Community ID |
| Organization | 公开、审核通过、组织与主体活跃；本城公开活动或已批准的明确公开组织点位作为城市来源 | 原 Organization ID |
| Business | 原 041 公开名称；070 管理资料的完整当前闭包与 072 本人具体版本公开许可同时成立才采用获准名称/简介 | 原 Business ID |
| Opportunity | 本人显式读取，复用 `humanSocialSQL` 与 `opportunity.Generate`，同一个 PG clock、当前获准供给和原规则 | 私密候选 `intentID:activityID`，详情/分享仅原 Activity ID |

Business 没有当前 072 许可时只返回既有公开名称及空简介，不泄露 Console facts、核验 URL、rightsNote、人员或私密备注。Business 不猜总部坐标。商业 Activity 需要原有效 Venue、已验证关系、批准候选、当前运营组织及期限；失效初始来源不会生成旧点位。

Opportunity 为 `SELF_PRIVATE`，不公开 Intent 正文、约束、关系或其他私人来源，也不进入广告目标。普通人类 Activity 结果为 `AUTHORIZED_VIEW`，这不是机器 ContextBuilder 的用途许可；机器 PUBLIC 上限仍保持原契约。

## 当前性与原生回执

`NativeStore` 在原 Store 上实现。Read 从当前原 Task 构造闭集 query，不接受客户端提供 receipt、ACL 或 source proof。当前账号、Personal Agent/Profile、Task 内容及 xmin、Session、城市与具体来源闭包共同绑定。

回执仅在服务器内存中：Items、兼容 DTO、严格公开商业 refs、PG 观察时间、最早期限、当前源 proof、进程 HMAC seal 不作为许可序列化。兼容数组在最终响应中仍按原获准领域 wire 输出。最长两分钟且夹紧原来源/Session/商业授权最早期限；source xmin 能拒绝撤回后还原及同值更新。Session 正常 idle 刷新是允许的，不将正常认证刷新误当旧许可恢复。末条 SQL 区分真实 Session/账号失效401与仍有效 Session 下失效 Agent/Task404。

普通 POST、special intent router、本人 Task GET 都复用同一链：读取原生结果 → 原 sponsored disclosure 与 JSON 编码缓冲 → 商业核验 → 最后原生事务复验 → 返回。所有 pool、关系、账号、Task、Session 等待在最终 payload SQL 前完成；最后一条 SQL 使用同一 PG clock 重新核验当前身份、源、期限及相关商业来源。随后不再独立调用另一个 pool 中的 ValidateTask。

none、failed、组织工作区和 ONLINE 不使用此七类 proof 替代各自原权限合同。新增四类缺原生端口时 fail closed；旧非原生兼容 adapter 不作为原生生产者完成证据。

## 商业与界面

只有服务器密封的 PublicCommercialRefs 允许原公开 Activity/Place 进入原赞助 lane；`AUTHORIZED_VIEW` 本身不能证明公开。获邀私密 Activity、Opportunity/Intent/理由不会进入商业端口。相关声明、070 来源、071 审查授权、成员冲突与自然期限也参与最终复验。

对话区计数来自同一 typed Items，覆盖七类，中文显示。当前回答从本次已核验 responseMessage 投影到当前末条 assistant，旧 Task 历史与以前轮回答不重写、不自动 PUT；再次打开或重试也使用当前结果回答。小屏、大字、IME 沿用原可滚动布局，地图背景与 overlay 点击修复保持。

适用 UX-CHECK-04/06/08/09/10/11/12/13/14/16；14 的 widget 证据不代替当前真机或 TalkBack，15 的独立真人观察仍未运行。

## 验证边界

本线最终 native22：fresh owned083、80 PASS、0 FAIL/SKIP，test/vet/build 0、847 复制源稳定、完整原 public/catalog 不变、owned DB DROP。包括原 registered HTTP 兼容 Activity/Place、赞助自然结果不变、Task五场景404/401，以及兼容 DTO 篡改 seal 拒绝。11 个 Flutter 目标文件 89 PASS，四文件 analyze 0。具体命令、初失败与 SHA 见本任务 research 和 evidence。

当前整仓联合回归、当前 APK/七类真机由 root 验证；本线尚未报告其结果。所有数据为 LOCAL_SYNTHETIC。真实人员、主办方资料、生产身份、CSSA、付费赞助、生产和读屏均未以本线测试验收。Closed Pilot Ready / Consumer Beta = NO。
