> 最新CODE_LOCAL状态以末尾2026-10-04根最终验收及live队列为准，旧阶段边界保留。

# 商家经营权审核结果通知

2026-10-04。BT-V5-AGE-038 的 BUSINESS 语义来源；复用原 `businessconsole` 与 `native_notification_decisions`，不是新商家审核、经营权授予或 Business Agent。

## 当前实际能力

原 `ReviewBusinessClaim` 的明确审核版本、原审核员当前 Session 与 `business_review_grants` 权限保持。pending→verified/rejected、verified→revoked 的原 Claim CAS、原审核 audit、AttentionPolicy 决策、即时/普通 Inbox 写入在同一 Read Committed 事务提交。相同明确版本和 Note 重试返回原 Claim，不能重发通知。

事件 `business_claim_review` 固定分类 BUSINESS、中文标题“商家经营权审核状态已更新”。仅原 `submitted_by` 现在仍为该商家的活跃 owner/admin 时可作为收件人。普通成员、公众、经营权已丢失者、不同 Person 或 Business principal 不继承收件权限。没有向全体成员或关注者广播。审核员、原提交人、Business/principal、成员关系、有限审核许可、当前审核 audit 及双向屏蔽均从数据库解析，模型或请求 JSON 不选择收件人或分类。

## 真实来源与当前可见性

081 新增 `native_notification_decisions.business_claim_id`，外键引用原 `business_claim_controls.business_id`；`source_id` 仍是稳定 Business ID。事件版本为原 `claim:<version>`，指纹含 Claim、Business、principals、membership、reviewer、review grant 和 audit 的 xmin。修改后恢复同值不能复用旧通知可见性。审核人员许可过期或撤回也使旧通知不可见；这不会撤销原权威 Claim 的状态，当前工作台仍按原权限独立读取。

新数据库 source 函数仅处理这一闭集事件，其他来源原样委托原 079 前函数。未知类别、无当前来源、错误 recipient/分类/事件版本/policy/priority 仍被原 guard 拒绝。081 没有修改历史迁移、关闭触发器、改写旧领域 ID 或回填旧审核。

| Attention route | Priority | 实际结果 |
| --- | ---: | --- |
| IMMEDIATE | 100 | 同事务决策及 Inbox |
| NORMAL | 50 | 同事务决策及 Inbox |
| DIGEST | 10 | 持久待汇总决策，没有送达 Inbox |
| SILENT | 0 | 仅决策记录，没有 Inbox |
| BLOCK | 0 | 仅拒绝决策，没有 Inbox |

DIGEST 汇总是下游 AGE040，不能把本来源的 pending 决策说成汇总、推送或已送达。`business_update` 和 Business Agent 原 Unavailable 保持；原人类审核是此来源唯一生产者。

## 中文客户端与直接路径

收件箱 wire 的 `resourceId` 是决策 ID，必须另有 typed `targetBusinessId`，严格限 BUSINESS/`business_claim_review`。点击前先执行原当前 Inbox read；通过后读取原 `GET /v1/me/businesses/{BusinessID}/console`，没有以通知缓存代替当前审核结果。页面告诉用户当前状态可能已变化、经营权核验不代表交易背书。资料与成员管理保留设置→商家工作台路径。

只读目的页不提交审核、报名、外部消息或 Memory。旧账号/API/授权 getter/主体/凭据或观察到的 workspace ABA 使原页面永久退役，迟到响应不能显示或复用原权限。承接 Inbox 的原 `NotificationDestinationBoundary`，借用同 transport，不关闭调用方 client。

适用 UX-CHECK-03/04/06/07/08/09/10/11/13/14/16：通知提示与当前权威对象分开；有限权限与异常明确；旧批准和迟到结果失效；原领域动作和中文组件复用；通知内容不复制原审核材料、来源 URL、经营权声明或审核 Note。

## 迁移与实际证据

081 fresh/up 在非空 001–080 隔离库保持全部旧列/数据和 Participation xmin；unused down/reapply 恢复完整语义 functions/triggers/constraints/columns/indexes/tables/policies。已有 BUSINESS 决策或相应 Inbox 时 down 原子拒绝 SQLSTATE55000，不能删除保留历史来回滚。删列前先恢复旧约束，以免 PostgreSQL 自动删约束导致回滚失败。

最新 `work/v5-age038-resume/native-business081-5/result.json`：358 Test PASS、0 FAIL/SKIP、test/vet/build0，复制执行源字节全部核证、root owned SHA 稳定、旧 public/catalog/Participation xmin/迁移往返/自有库 DROP 通过。包含五策略×三原审核状态、100 次顺序/100 次并发同版本重试、当前关系撤销/主体失效/屏蔽/审核 grant ABA、registered HTTP 审核/本人 Inbox/跨 Person read404/原私有 Console、真实 Inbox 表等待跨过 reviewer grant 到期后整个 Claim/audit/decision/Inbox 回滚。

本轮 live API 源在其他 worker 修改时发生变化，明确不是整个 live 源稳定验收；实际执行的是不可变复制帧。首次 down 失败、runner compose cwd/清理失败及原唯一 owned 库恢复 DROP、测试参数/清理 fixture 错误均保留在 native-business081-1/2/3，不改写失败结果。

`business-client-target4/result.json`：26 功能检查 PASS、analyze/test0，6 owned 源稳定；正常/权限拒绝/workspace ABA 的原 Inbox→目标页、current GET/刷新、same-key API/凭据 ABA、迟到结果与 320px/3倍字体定向检查。当前全 Flutter、联合全 Go081、新 APK 真机、TalkBack 和真实目标用户验收尚未完成，后续证据另记。合成商家不能作为现实经营权、生产身份、CSSA 或试点证据。

Closed Pilot Ready=NO；Consumer Beta=NO。真实 IdP、获准供给、HTTPS/地图/部署/日志/运营、真实 A→H 等原门槛保持。


## 2026-10-04 根全量与实际真机结果

最终current082全Go9718PASS/0FAIL-SKIP/test-vet-build0/794stable+3seed/旧public-catalog-xmin/updownreapply/drop，currentFlutter991功能+120loading/analyze-test-Debugbuild0/259stable。44a2506 Debug实际安装c641566b，沿用safe081自有本地API/DB。显式合成新Business申请原v1→独立有限claim-only本地合成reviewer原REJECT v2同Tx生成唯一BUSINESS NORMAL decision/Inbox，旧所有行值/xmin保留；点击通知实际打开原Business当前私有Console，显示未通过/版本2，refresh一致，经营权核验不代表交易背书。App/API同binary真正重启保存全部public/xmin；附手机PNG/XML和原request ID。合成主体/开发Session不是真实审核人或已核验身份/经营权；AT/TalkBack/生产部署未运行。证据work/v5-age038-resume/phone-now-business081-1/business-fixture-execution1.json及screens/business-notification-detail1.png、business-current-refresh1.png，根three-task-acceptance32.json。Closed Pilot/Beta NO。
