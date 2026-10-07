# 赞助机会与自然排序信任合同

任务BT-V4-ADS-001，2026-10-03。**当前状态：已实施本地原生声明、审核、消费读取与中文披露，最终验收进行中**。唯一live状态由根代理维护。来源为V4 canonical/ADR0017、原live task AC、现OPPORTUNITY-ENGINE-V1与INTENT-TO-PLACE-MATCHING-V1、GLOBAL-UX-INTERACTION-CONTRACT；本合同增量补赞助边界，不复制自然排序引擎。

## 用户结果

浏览者能区分原自然匹配与商业展示；赞助标签、实际支持方、声明来源和期限可检查。付费或声明不能改变原自然排名/理由、使无权限供给可见、覆盖硬筛选条件或伪装为Agent认为最相关。原活动/地点ID、详情/报名/收藏/导航权限继续来自对应领域，Sponsored标识本身不授动作权限。

## 真实来源

071增加`sponsored_opportunity_declarations`与专门审核许可表，商业赞助关系不从Business verified、官方链接、组织名称、HostLabel、活动正文或来源标签推断。Owner/Admin真实当前资格、已核验商家经营权、公开可运营Place/Business主办Activity只决定谁能提交，独立商业声明/出处/权利说明/观察和截止时间仍是必需事实。商业展示声明审核不证明付款、合同、营收、身份之外的外部合作；没有收费或外部广告平台调用。

审核使用专门有限`review_sponsorship`原生权限与当前City reviewer；不借070 claim/profile/venue权限，不把个人设置、Feature开关、Agent Profile、human review seal当该授权。具体provisioning为显式本地operator/测试fixture，普通人不能自我提升。Owner不得自审；权限/主体撤销恢复、源变更和自然到期不能复用旧确认或自动恢复陈旧声明。

## 分离排序和披露

1. 自然候选先通过原真实当前来源与硬权限生成。活动机会五层+时间/ID、地点Venue硬门槛+Rank/语义分/ID、Now原生查询/ResultSet/pins均不接广告分数或付款参数。
2. 独立`sponsoredOpportunities`只引用当次实际已授权、符合原条件且明确公开的Activity/Place；不同区块不挤掉或替换自然结果，不新增自然ResultSet refs/地图Pin。若同实体出现在两区，各自明确渠道，不能暗称付费提高自然相关性。
3. 赞助固定中文label“赞助”，显示当前支持方BUSINESS stableID/name和公开商业声明来源/有效期。未知/过期/撤权/未审核/错目标声明不展示，Sponsor理由不能混入白名单自然reasonCodes，模型或外部文案不能定义标签或排名。
4. 原API增量commercialTrustVersion与独立Sponsor字段，Client严格校验kind/version/ID/目标/来源/有限时间。旧没有Sponsor字段的兼容响应不被推断为存在广告；存在未知/畸形Sponsor信息不得静默作为普通条目展示。
5. Sponsor来源暂不可用时清空且明确该区状态，原自然结果保持。只有这条fallback、空数组或pure DTO测试不能证明任务已实现。

## 人类写路径与权限

本人的完整声明提交到pending → 专门独立Reviewer读取具体版本/源绑定 → 明确批准/拒绝 → 当前持久读取 → 具体版本撤销。提交、审核、撤销各自包括当前版本/源snapshot与确认；snapshot只绑定真实来源和主体，不证明真人点击、认知目的或付款批准。未知响应先查权威结果，不自动重复写。

原生事务显式RC/UTC，Business短期编辑锁、排序后的Account共享锁、声明编辑行锁、最终Session锁；角色、经营权、目标和City以真实当前SQL快照再检查，不能声称所有来源都持有行锁。所有领域与audit写后再检查PGclock、会话、authority epoch及具体源snapshot，版本CAS、operationId幂等与反ABA；last_seen刷新不成为新批准代际。实际保留来源digest/xmin不冒充递增counter。当前读取在线性化的SQL来源帧核验；不能承诺网络发送后绝对收回数据。审核记录与公开DTO分开，不输出私人rightsNote/审核员身份、Memory、聊天、个人轨迹或非公开活动资料。

## 原生接口与实际接线

| 入口 | 合同 |
| --- | --- |
| POST `/v1/businesses/{businessID}/sponsored-opportunities` | 当前Person owner/admin，明确完整声明与SourceSnapshot；pending，operationId幂等 |
| GET 同路径 | 当前本人商家管理资格；声明、当前可提交公开目标、真实源快照 |
| GET `/v1/cities/{cityID}/sponsored-opportunity-review` | 专门有限授权与当前City独立reviewer；排除自己的商家 |
| POST `/v1/sponsored-opportunities/{declarationID}/review` | expectedRevision、具体snapshot、approve/reject、note、confirmed；禁止自审 |
| POST 同前缀 `/revoke` | 当前商家管理人或独立reviewer；具体版本撤销，不能批准其它内容 |

根代理已在既有Server注册五路由；没有新Server字段、费用或自授权接口。三个消费入口为`GET /v1/me/opportunities`、`GET /v1/me/social-intents/{intentID}/place-matches`、现Now任务创建/恢复。Now恢复使用既有`GET /v1/me/agent-tasks/{taskID}`，不是另造URL。组织/Business工作身份不继承Person赞助数据；其合法原检索路径保留，商业区明确Unavailable。

公开封闭DTO：`commercialTrustVersion=sponsored-opportunity-v1`、`sponsoredStatus=available|unavailable`、最多5条独立`sponsoredOpportunities`。每条包含stable declaration ID/revision、固定`SPONSORED/赞助`、BUSINESS ID/name、原ACTIVITY或PLACE ID/title、声明HTTPS出处和观察/审核/截止时间、服务器当前checkedAt。Client仅接收当次自然结果中已有且标题一致的typed目标，不向ResultSet、地图或原数组插入赞助Entity。

两个实际UI为活动机会页与Now结果sheet。固定“赞助展示/赞助”及支持方、来源、期限，单独位于原自然条目后，详情仍走原稳定Activity/Place回调；没有回调的控件禁用。place-matches目前仅API，不虚构独立消费UI。原自然硬门槛和动作权限继续由原领域决定。

## 适用UX证据

| 检查 | 实际对应证据与限制 |
| --- | --- |
| 01、04、05、06 | 机会页与Now原直接入口，原稳定详情回调；固定赞助标签/支持方/出处/期限及付款未核验说明；严格拒绝无标签/过期/错自然target |
| 07、08、09 | Native pending/approve/reject/revoke、具体source/authority snapshot与版本、op冲突/重复审核仅一个audit；API实际状态不称管理UI |
| 10 | 机会页真实auth.signOut与迟到响应清空；原Now request/工作身份路径保留，Org/Business没有Person赞助数据；不声称两个组织间真机已测 |
| 11 | 纯规则自然匹配和五条人类领域API，不需要模型或定位；没有获准推理/付款出口 |
| 12 | 实际Store/HTTP重新构造后持久读取，撤回后商业区消失；App/API进程重启与前后台真机NOT_RUN |
| 13、14 | EXTEND现OpportunityPage/AgentResultsSheet，REUSE原详情、theme、OutlinedButton；NEW仅封闭Sponsor DTO与单独披露区；360px/1.6文本与不可用详情控件测试，TalkBack/键盘/真机NOT_RUN |
| 16 | 商业管理audit只存actor/action/resource/decision/purpose；公开DTO不带rightsNote/reviewerID/private正文，不加聊天/轨迹埋点 |
| 02、03、15 | 本项消费为只读披露，不新增澄清问答，02/03不另造表单；未经指导的独立用户可用性观察NOT_RUN |

UI数据解析与模拟传输测试证明消费接线，不冒充Native权限或现实商业事实。

## 验收门槛

- 真实注册HTTP与独立fresh071库，实际提交/独立审批/持久读/撤销；没有rawSQL approved Sponsor正例。
- 批准Sponsor前后对三路径原自然数组、排序、理由、稳定ID、ResultSet/pins逐结构相等；Sponsor不能突破类别、人数、城市、受众或来源期限。
- Owner/Admin/member/外人/Org/Biz/self-review/缺专门grant的City reviewer、source更改/隐藏/改期/取消、claim/经营权/成员撤权恢复、锁等待后Session过期/撤销、最后写后deadline、CAS与operation冲突均有实际负例。
- 新071 SQL形状/immutable/CAS/FK、旧非空全部public完整行/up、非空down原子拒绝与空reapply，最终旧catalog对象保持。
- 现活动Page和Now结果sheet中文Sponsor披露/固定Target回调/未知metadata拒绝，自然order/Pin保持，账号/工作区迟到与大字。place-matches目前只有API，不虚构已完成独立消费UI。
- 本轮原始失败、最终source SHA、目标/完整Go与Flutter、真机分别记录；未跑写NOT_RUN。

## 当前限制

现实现提供本地可调用、持久化的商业声明能力；本轮全部数据和审核许可为明确本地合成验收，不能称实际合同、付款或商家授权已获运营核验。管理路径是五条真实API，本轮没有另建商家/审核人管理UI、付费系统、广告投放平台或生产开关。模型、隐私源、费用、外部推送与原发布许可不因赞助声明而开放。

`native12`在fresh001–071实际两轮各152 Test PASS/0FAIL/0SKIP，含原自然排序测试、四类非空down与五个最终audit写等待后权限屏障；vet/build0、完整旧public行/八种旧非空数据升级保持、空down/reapply及所有独占DB清理通过。14个专属Go/SQL源码稳定；并行根代理改变自己的测试，所以allAPI观察hash变化如实记录，不能将此scope称整体源冻结。`native10`历史146两轮及`native11`计数查询类型错误原始记录保留。两UI目标测试`client-target6`通过；后续四条Dart braces lint已修，新的全Flutter结果由根复验。整体当前Go、全Flutter/build、真机与辅助技术结果只以最终证据索引为准，尚未执行的不得从target PASS推断。

不开费用、生产广告、外部服务或正式发布；真实商家/CSSA/IdP/HTTPS/地图/值守/真实A→H仍受原发布门槛，Closed Pilot/Consumer Beta = NO。

详细精确范围/路由提案与审计见`work/v4-ads001/audit-plan.md`和`docs/research/BIRDTIE-V4-ADS-001-AUDIT.md`。

## UTC DTO 校验补充（2026-10-03）

四个服务器时间字段只接受封闭的 UTC RFC3339 格式：四位公历年、真实年月日、24 小时时分秒、`T`/`Z`，以及可选 1–9 位秒小数。客户端逐项回查年月日时分秒，拒绝 Dart `DateTime` 对 13 月、32 日、非闰年 2 月 29 日、25 时等输入的归一化；UTC 偏移、紧凑格式、逗号小数、换行和超过九位的小数不属于此 wire 合同。Go RFC3339Nano 的秒小数在 Dart 中保留到微秒（多余纳秒截断），这不是新的权限或服务器期限真源，原生最终鉴权和 PG 时钟校验继续有效。

`client-utc-red1` 保留旧实现接受非法 `checkedAt=2026-10-03T25:06:00Z` 的真实失败；`client-utc-green2` 为两份赞助消费测试的最终定向成功，七个 ADS Dart 文件 `client-utc-analyze2` 无问题。测试包含四字段非法日期/格式、世纪闰年、有效闰日以及 1–9 位小数。不可变 `worker-final1` 保留原帧，`worker-final2` 仅追加本次时间校验差分和结果；14 个 Go/SQL 文件未变。全 Flutter/build 与真机结果仍由根代理单列核验，定向测试不替代设备、TalkBack 或生产商业证据。

## 根代理当前最终复验与手机披露（2026-10-03）

上述 native/target 和等待共同回归的描述保留为分阶段历史。当前最终证据来自 `work/v4-biz003/whole-go071-3`、`full-client5` 与 `phone`；worker重新读取实际结果、命令、源 SHA 和手机节点/截图核对，不将旧 APK 或失败帧充作新成功。

- fresh001–071 的完整默认并发 `go test ./... -count=1 -json`：8487 Test PASS、0FAIL/0SKIP；全 `go vet ./...` / `go build ./...` exit 0，631 个观察源前后稳定，旧全部 public 完整行、catalog 保持，自有测试库已 DROP。CGO0 无 gcc，race 未运行。
- 完整 Flutter analyze/test/build：exit 均为 0；497 功能 PASS + 75 hidden loading PASS、0FAIL/0SKIP，179 个 Client 源前后稳定。此构建是本地 development Debug APK，不是 production release。APK SHA256 `1119c68621eafb5a5e3f4ca1a9c6f10d9687f9b512ce3582cddc9f52d7074175`，实际 ADB Streamed Install `Success` / exit 0。
- 手机截图 `sponsored-disclosure1.png`（原始 1220×2656）实际显示独立“赞助展示/赞助”、支持方、HTTPS 声明出处、审核/有效日期、未核验付款/合同说明及“查看赞助活动详情”。实际点击后的 `sponsored-detail1.png` 为同名 Business 合成活动的既有权威详情，继续保留原报名/分享/导航动作；这不等于已完成这些动作的本轮 E2E。
- `phone/organic-before.json` 与 `organic-after.json` 实际 `data` 完整逐结构相等（均3条），独立 `sponsoredOpportunities` 从0变1，目标为原自然结果中的 ACTIVITY；当前 native152 已覆盖原自然排序、Now typed refs/ResultSet/Pin 不变。这里手机观察针对活动机会页；不把它冒充所有 Now/PLACE 场景的手机证据。

所有账号、Business、活动、声明与有限审核许可都是 **LOCAL_SYNTHETIC / CODE_LOCAL**。TalkBack、独立用户观察、真实支付/合同/CSSA 授权、生产部署及 App/API 正式进程重启未验收；原生重建 reader 与本地手机安装不替代这些条件。Closed Pilot / Consumer Beta 仍为 **NO**。只读核验及精选实际证据追加到不可变 `worker-final3`，final1/2 不覆盖。
