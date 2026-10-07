# BT-V4-BIZ-002 本人管理资料回答 UI 增量

2026-10-04；owner sponsored_trust；仅 CODE_LOCAL slice。完整 BIZ002 保持 PARTIAL，Closed Pilot / Consumer Beta NO。

## 前置 AGE062 审计

原 AGE062 依赖已 DONE，但现有 agentpurpose 对 TemporaryActivity/Unknown 仍拒绝。076 consent_grants 是本人 TASK_CONTEXT_READ 的具体版本来源许可，不能转换为某 Activity 的 Account/Agent recipient 许可；旧 profile_view 也不是认知许可。Coordination 的纯 Actor/Grant 类型无 live source resolver。没有正向 Activity 用途授权、人类入口、受限消费者和下游副本清理真源。需根代理协调共享 grant/DDL 后串行建立原生能力；不在当前并行批创建 preview/grant 或 UI 壳子，不从 RSVP 推断许可。

## 实际复用与差距

- 原 `agentbusiness/knowledge.go` 六字符串闭集已真实实现；商家介绍/简介是同义规则。原 registered HTTP `/v1/me/businesses/{businessID}/knowledge/ask` 当前 Session、owner/admin、独立核验资料及最终来源复查已存在。
- 原 BusinessApi 无 typed 问答 transport；Console 无直接问答入口，也无同 key API/controller rebind。新增页面只借用它，不关闭 parent transport。
- 新 UI 的用户结果：本人管理员选择当前商家的一项规则问题；场地问题再选择明确的已绑定场地；得到现有已核验资料或真实未知，返回工作台。
- 非公开资料不进入公众路由。仅 native Answer 的正文、实际来源类型/版本/期限可呈现，不投影 raw Console、sourceUrl、rightsNote、私密 note 或审核员。该界面不是商家 Agent、provider、工具执行或 Memory。

## 简短计划与验收

1. 复用 BusinessApi，typed 闭集答复、已知/未知及精确来源，严格 RFC3339 日历和显式 Z/offset 归一 UTC，禁止过期答复与越界内容。
2. 查询 POST 明确只读，不继承 uncertain mutation 提示；每次先 read 当前角色/场地，后 native 问答。身份/组织/商家/source frame 或 transport/controller 换绑后拒绝迟到响应，ABA 不复活旧页。
3. Console 直接入口，中文逐步选择，独立滚动与 48dp 控件；前后台及自然到期清除当前回答。取消无领域写，返回仍有工作台。
4. 定向 Flutter 正负 API/controller/widget 回归与旧 Business API/Console 回归；隔离真实 PG native foundation 复验另记录。最终 source freeze 由根代理独立全量/build/真机验。

适用 UX-CHECK-01/02/03/04/05/06/07/08/09/10/11/12/13/14/16。未运行的真机/TalkBack/现实商家及 provider 不计完成；普通规则只读不需要额外假批准仪式。

## 验证记录

实际命令与旧失败均保留在 `work/v4-biz002-ui/`。

- `flutter test --reporter expanded` 指定新问答 API/controller/page、原 Business API/Console/controller、原 public permission page/supplier API 共8文件：最终 target11 **74 功能 PASS，0 FAIL/SKIP**，命令实际终端 exit0。
- `flutter analyze` 10源文件：analyze4 exit0、No issues found。source-freeze1 → source-after1 **10 文件 SHA 完全一致**。business_api_test 原文未改。
- `python work/v4-biz002-ui/verify-native.py --round native1`：owned fresh076 原三包 `^TestBusinessKnowledge` **51 PASS，0 FAIL/SKIP/pkgFail**；727 Go/SQL/mod/sum source stable、全 public 行前后相等、owned DB 实际 DROP。runner 由原 `work/v5-age027-resume/verify.py` 精确复制，改任务目录/owned fixture/三包 pattern，不修改 native 实现/DDL。
- Go vet/build 三包实际 exit0，native-tools1.json；不称全 Go 或运行部署。native1 没有独立 catalog 快照，不能写 catalog PASS。

### 真实 RED 与修复

- target1 44 PASS / 2 FAIL：新语义 fixture 漏已有 focus action，未知回答在 ListView 屏外未 build；修正精确语义与真实滚动，保留旧日志。
- target3 56 PASS / 3 FAIL：真实 Console 换绑通知旧 Navigator sibling，造成 during-build setState；修复同步退休捕获对象、post-frame通知。取消 fixture 返回工作台时原 scroll offset 保留，加入真实向上滚动，不重置状态。
- target6 60 PASS / 1 FAIL：fixture 直接 paused 后期望新 Flutter frame。改为真实 inactive → paused → resumed 序列；inactive 清除显示、resumed 重读角色，撤权后无自动 POST。
- target9 73 PASS / 1 FAIL：恢复借用 external controller 时原 load 同步通知导致旧问答页 during-build setState。真实修复：新问答 controller 先同步清状态/拒操作，再合并 post-frame UI 通知；Console 永久 binding epoch 防原 API/controller 对象 A→B→A 复活旧草稿、批准或回答。target10/11 均74绿。
- analyze1 10 issues 均已修复；analyze2/3/4 exit0。初次失败未覆盖。

### 当前真实实现

Console 同 key auth/org/API/controller 改绑捕获旧 ownership 后只 dispose 原自有对象；borrowed transport/controller 不关闭。新知识 controller 还验证 API 实际 token 等于本人捕获 token，每次查询重新读取当前 membership 与明确 venue，再让 native Answer 最终核源。监听源帧变化后永久退休旧页，迟到或 ABA 响应不呈现。资料到期或退入后台清答复，返回前台重新 native 读，不自动 query。未知项不造来源；仅 rule Answer 最小 DTO、零 provider/外发/Memory/预约。

旧‘管理资料公开范围’入口原动态 widget getter 已在 Console 范围改为 captured auth/org/selectedBusiness/API/sourceFrame 与永久 epoch；退休后 getter 返回 null，恢复旧对象不复活。原 SupplierProfileApi 的401-before-HTTP与零 PUT 已定向验证，公共 API/page 源码没有改动。原编辑与确认 dialog 也校验捕获 binding epoch，不把旧批准提交给新 controller。

360px/font3 滚动完整回答、明确场地、48dp 主按钮及真实 semantics 的 widget 证据已过；不等于 TalkBack/真机验收。新真机、重启、现实商家、生产身份/部署、模型/工具均 **NOT_RUN/UNAVAILABLE**，不得使用上一批 Now/Place APK 当本批 UI 验收。由根代理统一 full Flutter/build/phone 后独立追加证据。完整 BIZ002 仍 PARTIAL，外部门槛和 dormant Agent 未解除。
