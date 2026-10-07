# City Seed 活动审核发布缺陷

2026-10-02。在 AGE003 维护者隐私来源审查中实际发现；这是独立的旧业务兼容缺陷，不是字段可见性已修复的证明，也不作外部试点数据。

## 已取得的证据

只读源码探针在自有 fresh 001–055 与三份开发 seed 的 PostgreSQL 库中调用实际 `SubmitActivity`、`ReviewActivity`，发布返回 PostgreSQL `23514`，约束为 `activity_organizer_exactly_one`。事务回滚，没有声称审核发布成功。四个维护者泄漏和这个发布失败保留在 [原始执行](../../work/v5-age003-visibility-native-maintainer-audit-red-store.jsonl) 与 [隔离清理结果](../../work/v5-age003-visibility-native-maintainer-audit-red-result.json)；前后八表统计相同，自有库已移除。

2026-10-02 复现版本的审核 writer 不提供 `host_account_id` 或 `organization_id`；031/041 初始主办方 trigger 无法映射真实 Person / Community / Organization / Business，插入全空主办方被 exactly-one 约束正确拒绝。不能删除约束、把审核员冒认为主办方，或使用人工合法 fixture 来宣称该实际入口通过。

## 旧证据的准确范围

`BT-COM-003` 原有证据证明当时九个既存活动被回填，以及合法合成 Person / Community 主办方的迁移回归；这不是本次新候选经真实审核入口发布的证据。组织工作台正常发布和开发 seed SQL 也不能替代该入口验证。

## 后续验收

在原队列增量安排独立兼容修复：明确真实主办方来源和授权，复用 typed organizer 与活动领域动作；未知主办方以领域错误拒绝并给出可恢复路径，不能产生裸数据库失败或伪造主办方。用实际 submit → review → public detail / organizer / source / persistence 正负回归验证，并保持旧 ID、主办方约束、独立 host 信息和 source 行。

AGE003 只修自动复制维护者及其历史可信来源读保护；未改变本缺陷的主办方规则。Closed Pilot / Consumer Beta 均为 NO。

## 2026-10-03 BT-FIX-CITY-001 接续

现实现见[唯一候选主办方契约](../architecture/CITY-ACTIVITY-CANDIDATE-ORGANIZER.md)。新增明确 typed selector 与不可变059 side binding；旧候选和活动行不回填，缺失主办方 publish 为中文可恢复领域错误。实际四类 submit→review→匿名/本人 public detail→新连接 Store 复读成功，来源/独立 HostLabel/真实 organizer 与主体 ID 保留，Reviewer 不变主办方，重复 review 不产生第二活动。

并行独立审查另复现过期 City 仍能 reject 写状态并返回候选；`city-expired-red1` 真实返回 rejected/nil，已补当前 City 与锁后期限保护。候选 List 的最终撤权保护也以实际 cursor barrier 验证：有意只关闭 final gate 的 Go overlay 负控返回失权候选，当前源码拒绝。早期 HostLabel 漏填、测试库清理漏依赖、非法最后 Owner 降级等 **fixture** 失败分别保留，不当作业务漏洞 RED。

fresh/current-data up/down/reapply 与最终范围/完整回归、失败原始日志、source SHA 和 owned DB 清理见[本次证据](../testing/evidence/city-activity-organizer-publish-2026-10-03/README.md)。最终状态须由根代理核证并写原队列；本条接续不将中间范围通过称全仓通过。没有新增客户端候选页面、部署、真实组织或活动授权、消费者/手机验收或正式试点证据。
