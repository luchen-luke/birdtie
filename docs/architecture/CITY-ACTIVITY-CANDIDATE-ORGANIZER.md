# 城市活动线索的明确主办方

2026-10-03，BT-FIX-CITY-001。仓库实现与隔离开发验证；未部署，不是现实主办方核验或试点授权。历史缺陷及最新证据见[审计记录](../research/CITY-SEED-ACTIVITY-PUBLISH-REGRESSION-2026-10-02.md)。

## 沿用权威领域

复用[社群与活动主体模型](COMMUNITY-AND-ACTIVITY-SOCIAL-MODEL.md)、[地点模型](ACTIVITY-LOCATION-MODEL-V4.md)、原 actorref、Activity 与成员表。Community 不变为 Agent，Organization 资源 ID 不等于其账号主体 ID，Business Agent 仍不启用。没有复制 Civu、替换 Activity 表或移除主办方 exactly-one/FK。

原城市线索 submit/review writer 只携带外部 hostLabel，发布时无法填入原 typed organizer。补充可选 `organizer:{type,id}`；四类选择必须由当前 Person 提交者明确提供，选择器不授予管理权，也不证明现实活动授权。

| 选择 | 提交及发布时的领域条件 | 写入 Activity |
| --- | --- | --- |
| PERSON | 当前活跃 Person 明确选自己 | 原 Person 主体 |
| COMMUNITY | 公开、已发布、活跃、有效社群；原创建者活跃；提交者当前 Owner/Admin | 原 Community ID；保留旧 host 兼容字段 |
| ORGANIZATION | 公开活跃组织及其活跃组织账号；提交者当前 Owner/Admin | 组织资源 ID 与真实组织账号分别保留 |
| BUSINESS | 原已审核、活跃商家及其活跃商家账号；提交者当前 Owner/Admin | 真实商家账号及原 Business organizer；指定 Place 时须原有效审核场地关系 |

普通成员、外人、失权者、未知或其他类型主体、私密组织/社群、跨资源/账号命名空间均不能由城市公开审核路径发布。HostLabel、来源 URL、权利说明独立保留，不从私人资料或审核员姓名生成主办方标签；公开维护者文字为“城市维护者”。

## 候选与授权的持久化

059 新增唯一 side table `city_activity_candidate_organizers`。原候选的列、ID、内容和历史状态不回填、不推断：selected_by 必须原提交者，每个绑定仅一个原生目标，类型分别有真实 FK；selected_at 为有限服务端时间。绑定不可原地换人、换对象或单独删除；修改须原提交者重新提交，父候选删除才可 cascade。该数据库保护不等于会话、用途或模型许可。

旧 nil/null 候选仍可提交、查看、拒绝。发布无绑定候选返回 recoverable `organizer_required`（409），中文说明要求原提交者明确选择有权管理的主办方重新提交；Reviewer 不替旧候选补填，也不把 submitted_by/外部标签推断为现实主办方。无效 selector 为 400，目标失效/撤权为 409。字段闭集、大小写、重复键和未授权 workspace/query actor 由局部 HTTP 解码及现有身份校验拒绝。

Submit 在当前 City editor、Person 与目标授权检查后同一事务保存候选、绑定和原审计。Review 仍须不同的当前 Reviewer；锁定原候选，再复核原提交者当前 City editor/目标代表权，而不是继承提交时授权。所有相关当前源锁保持到事务结束；使用明确 READ COMMITTED，锁等待后取 PostgreSQL clock_timestamp 校验 City、Community、Place/Business Venue、候选与结束时间。Review 的 reject 同样检查 City 当前权限与期限；候选自己过期仍可由有效 Reviewer 拒绝。

有效 Publish 同一事务写真实 Activity/organizer、公开 source/seed 链、原候选状态、审计与中文 Inbox；published_at、reviewed_at、verified_at 用实际 PG 时钟。Community organizer 在原活动兼容创建后同一事务切到 Community。重复审核已完成候选返回冲突，不产生第二活动。业务源锁后当前时钟检查是相应写入的核验点，不承诺网络返回后追溯回收，或把该普通领域权限升级成 AGE/AIR 模型用途授权。

私密候选列表在读取前、payload SQL 以及 cursor 完成后核验当前 City/活跃 Person/editor；最终撤权或取消丢弃已读取 payload。返回前重验有真实 PostgreSQL query barrier 测试；通过 Go overlay 只关掉该 final gate 的负控会实际返回失权候选，负控不修改生产源码、不作接受结果。

## 迁移与验收边界

fresh001–059、原058完整 public 行至059 up、非空绑定 down 拒绝且全行/guard/结构原子保持、只清理自有候选后的 empty down/reapply 均在隔离库验证。非空 down 拒绝为预期 exit3；不能把它写成失败发布或在生产执行 down。新 binding 表为空时允许回退，不改旧活动 ID 或其源链。

适用 UX-CHECK-06/07/08/09/10/11/12/16：明确来源与未知、候选/发布状态、当前主体和撤权、重复审核、普通领域路径、重建 Store/连接持久性、最小公开标签；证据为真实 Store/HTTP/PG 开发验收。当前 Flutter 没有候选 submit/list/review 页面，本任务未新增 UI、未重装手机、未取得辅助技术/消费者观察截图。新增页面仍须完整执行中文与其适用 UX 门禁。

最终范围测试、独立复核、完整 Go 回归、版本 SHA、失败和清理记录见[证据](../testing/evidence/city-activity-organizer-publish-2026-10-03/README.md)。最新源码应对应最终证据，不能用旧055/058报告或中间 Green 代替。开发 Person/组织/商家、固定认证与活动均为合成开发资料；Closed Pilot / Consumer Beta：NO。
