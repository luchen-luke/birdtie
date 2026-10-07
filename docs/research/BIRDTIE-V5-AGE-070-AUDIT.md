# BT-V5-AGE-070 Policy APIs 审计

2026-10-03。根代理已领取唯一 live 070/P0/CODE_AND_LOCAL_VERIFICATION，依赖 037/041/044/INT001 为 DONE。依据实际代码与 AGE 原文2144、137映射、V4/V5受控并行协议和 UX canonical，不根据前任务 DONE 推断新API已实现。根维护队列与共用报告，本 worker 仅登记范围开发。

## 来源与去重

只读预审来源为 `D:/Project/birdtie/work/v5-age068/preaudit-age070.md`；保留其当时未领取状态原文，不修改或重复导入。正式原文 `docs/product/BT-V5-AGE-AGENT-ENRICHMENT-EPIC.md:2144` 的四项是 GET policies / UPDATE attention / UPDATE social / UPDATE autonomy level。当前 canonical 扫描没有独立070合同，建立唯一 [AGENT-POLICY-APIS-V5.md](../architecture/AGENT-POLICY-APIS-V5.md)，其他原模型 canonical 继续各自负责权限/运行门禁。

| 审计对象 | 实施前实际状态 | 本轮增量与限制 |
| --- | --- | --- |
| Attention037 | 真实闭集模型/进程Store/Unavailable Service；持久本人API缺失 | 复用窄纯校验、实际新表和本人GET/PUT；不接处理服务 |
| Social041 | 七类偏好/进程独立版本/当前用途resolver缺失 | 全七类持久设置、DISABLED保守缺项；不制造大学/关系/双向consent |
| Autonomy044 | 四级定义/进程Store/准备限制，Level3硬关闭 | 0/1/2本人设置持久；3仍拒绝，没有自动动作 |
| Feature066 | 原生服务器进程开关全OFF、非同意 | 不写或返回其控制对象；设置信息不授任何开关/机器权限 |
| Notification061 | 实际八category真实表/API/独立version | 完全保留；不能重命名为13 eventType规则 |
| Profile/Private/visibility/Memory/grants | 原生各自来源与版本存在 | 正式非空 retained 原始值全行比较；设置只借身份 metadata，不写这些领域 |
| Flutter/消费界面 | 未找到三类设置消费API | 本轮接口任务无客户端改动；移动端/辅助技术/真机未运行 |

## 最小实施与验证计划

唯一新 065 表 `(exactAgent,family)`，三类 native CAS 独立；窄 Store/HTTP 四正式 routes，当前 native PERSON Session/Account/exactAgent/metadata/最终 PG clock。GET 不 backfill/restore/续期，EXPIRED 保留真实版本。新 pure exported validator 只调用原模型形状函数，不松开原控制对象 Marshal 拒绝。单Agent短事务 advisory 协调 full bundle 的行锁，不能把版本汇总或进程policy1假造为数据库版本。

按审计→计划→实现→纯域和 transport spy→fresh001–065 实库→原字段全行保持→迁移非空down拒绝/空down-reapply→源码 SHA 与原始证据交付根复核。必须 owner-positive、Peer自己的未配置数据、Org/admin/dormantBiz-negative；真实 Account 等待后 expiry/revoke/RR晚停用/cancel、插入后 session expiry 回滚；同类并发一胜/跨类不耗版本、旧版本拒绝、断开旧pool/新server重读。

## 实际过程记录

- 初期 compile-only 不作为功能验收。`native-compile2.log` 未使用time import失败修复后compile3成功；pure1未定义 DefaultHardLimits符号，改用实际 Limits及原自治字符串后 pure2 PASS；原产物065本身采用现真实字符串枚举。
- http-spy1 被并行023新测试未使用import影响编译，原JSONL保留，没有越界改别人文件；对方修复后 http-spy2 PASS。
- native-compile5 自己新增测试未使用net/http import失败，修除后compile6成功。没有把compile success说成实库完成。
- native1 真实 fresh065：207 Test PASS、两具体过期fixture违反原session创建/idle约束（父test与package合计四fail events）；修为原约束合法的真实已过期Session fixture，未修改sessions业务代码。该轮不是 PASS。14 owned源稳定、全部public原始行稳定、旧表非空/up不改值、非空down exit3及原子保留、空down-reapply、vet/build0、自有DBDROP实际通过。全API hash false来自其他并行任务，准确保存，不能以该轮称全API源码冻结。
- native2 最终 fresh001–065：两轮各210 Test PASS，0fail/skip，原037/041/044/066纯域回归502 Test PASS，vet/build0；14生产源码逐SHA当前一致，全部API源码该观测期间也稳定（非共享冻结承诺）。八类旧非空表及全部public完整行保持；up、非空down exit3/原子保留对象与行、空down/reapply、自有随机DBDROP实际通过。根独立与默认并发全Go另作新源码帧，不借7063历史。原始证据以 [本任务归档](../testing/evidence/agent-policy-apis-2026-10-03/README.md) 为准。没有新 consumer/授权resolver，不用本地设置替代生产证据。
- `final-guard-negative1`保持14生产源码冻结，仅work Go overlay故意禁用终端Session拒绝，真实同一PostgreSQL AFTER INSERT+过期测试返回200/Test FAIL，反证当前最终clock检查有效；原生产native2返回403并原子回滚。overlay/raw/sourceSHA单独保留，不能借负控修改授权或宣称正式服务已生产验收。

## 完成边界

四个本人持久设置 API 是本任务实际验收范围；Current machine purposes、模型出口、自动动作、真实场景授权、消费界面以及发布各自仍缺真实条件。保存后不称 runtime 接线完成。Closed Pilot / Consumer Beta NO，不更改原门槛、不启外部服务。
