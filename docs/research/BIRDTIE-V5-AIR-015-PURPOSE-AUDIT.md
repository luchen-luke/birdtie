# AIR015 当前 Moment 本地分析许可审计与计划

2026-10-04。唯一任务 BT-V5-AIR-015；root 显式恢复原 PARTIAL，lease 位于 work/v5-age038-resume/air015-purpose-lease.json。原 outbox/control/候选人工服务、发布门槛与不可变档案保留。

## 实际差距

001 consent_grants 是许可唯一 revision/expiry/revoked 真源，但无 Moment 字段/版本/Task/Session 绑定。076 TASK_CONTEXT_READ 只读当前批准 Task 上下文，不批准分析或保留；062 MODEL_CONTEXT_EGRESS 只 SELF_TASK_QUERY。CandidateSubmitter、SubmitInferredCandidate、ActualConsumer 仍不可用，064 effect writer 被原生拒绝。没有正式队列依赖循环；015 deps AIR014/INT001 均 DONE，016 单向依赖015。不得用 fake Run、事件metadata、公开可见、本人管理或 OfflineGrant 补权限。

## 此片计划与边界

1. 实施 Person-only MOMENT_LOCAL_ANALYSIS 的闭集 title/body 选择，原当前 Account→Session→PersonalAgent/metadata→ACTIVE native Task→本人 private draft Moment 原 revision/xmin/选定正文digest。具体预览展示本人当前可读内容与源/Task版本，正文不持久；human 预览不是机器分析许可。
2. 079 新 immutable preview 与 1对1 consent binding，无第二 state/revision/expiry。许可实际写原 consent_grants；批准精确 preview ID，一次消费/同键未知核实，期限不超过原显示预览与当前源/Session，旧 purpose 永远拒绝。
3. 当前 server-only resolver 同事务锁定来源后重查批准及 PG clock，返回最小已批准正文，JSON拒绝。新Session不能继承旧机器批准；当前本人可读无正文 receipt并撤回，不把旧批准复活。
4. 严格本人 HTTP 与中文 consequence，拒workspace/任意主体/重复未知null/错path字段。root 注册共享 server route，worker不写共享文件。
5. 正负纯Go+真实 registered/native：撤权/过期/源或身份ABA/等待后当前性、同键批准/撤回、无未批准机器正文、无候选/effect/model副作用、fresh/current-data/up/down/reapply/旧全部 public/catalog 与独占DB DROP。失败帧与源SHA保存，最终源freeze后root独立whole。

不启模型、候选保留/自动写、Organization/Business/A2A、Run或调度；原015完整需求仍 PARTIAL。UX-CHECK-06/08/09/10/16 适用于来源、具体批准、未知恢复与最小私密内容；本片没有Flutter界面，移动/辅助技术不可称已验。

## 本片实际验证（2026-10-04）

`work/v5-air015-purpose/verify-purpose-native.py --round native3`：三包 `go test -run ^TestEnrichmentPurpose -count=1 -json` **34 PASS /0 FAIL /0 SKIP /0 packageFail**，定向 vet/build exit0。源执行前快照759 Go/SQL/mod文件，执行后全部同SHA，9个worker产品/测试/迁移源均同SHA。

独占 fresh079：先001–078与既有开发seed/保留旧Person/Organization证据数据，再079 up→未使用down→reapply。所有旧非空public行完整保持，Participation原xmin保持；down后七类public可见semantic catalog与旧完全一致；native前后全部public/catalog相同，runner及子迁移库实际DROP。开发合成库只作仓库验收，非真实试点资料。

实际原生场景包括具体body-only内容、人类title不泄漏、原preview未知重试/消费receipt、单调撤回、source/Task/Account/Agent/metadata版本或ABA拒绝、新Session本人receipt/revoke与消费隔离；pg_stat_activity确认真实关系锁阻塞后撤许可、Moment/Task/City合法ABA、截止及Session自然idle到期均空正文拒绝。缺迁移/禁用原生guard拒绝且无许可写；原显示preview上界不因真实Authenticate闲置刷新延长；当前消费lease不晚于当前Session idle。既有Memory/candidate/outbox/inbox/effect/session行在普通native批准生命周期保持。

五条新路由经真实原server注册调用，401/400/403/409/503边界与返回闭集检查通过。原 `/v1/me/consents` 只接受Recipient+expiry且固定profile_view/read；extra purpose/resource/actions输入400，合法原profile grant不能分析Moment。人类JSON与server-only resolver严格分离，Resolution不能JSON构造/输出。

native1：26 PASS /2 FAIL（原flat DTO被测试错误当envelope解析；fixture新Session用了默认关闭dev_phone）。native2：29 PASS /1 FAIL（原撤回批准返回403与明确409合同不一致）；修复实现精确返回原已撤回ErrExpired，旧409断言保留。所有旧帧/失败日志保留，不描述初轮全绿。其余初次format/compile失败见 `FIRST-COMPILE-FAILURES.md`。

直接特权SQL同row撤Session后再clear未作为合法产品恢复路径；production Authenticate只刷新当前未撤Session，Revoke是单向，旧Session元数据A→B→A完全重建不被本片声称全面防篡改。未运行race（CGO0）、Flutter、真机、TalkBack、外部模型/部署。全仓079待root独立回归，不用本片目标代替全仓。

完整AIR015仍缺真实CandidateSubmitter、独立候选保留用途批准、同事务实际writer/checkpoint及故障/重试矩阵，维持PARTIAL；本片不启任何这些端口。
