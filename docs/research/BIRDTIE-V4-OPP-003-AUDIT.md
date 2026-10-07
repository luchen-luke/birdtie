# BT-V4-OPP-003 新朋友隐私路由审计

2026-10-02，审计时 IN_PROGRESS，后续实际实现与验证已 DONE。SOC-001 实际验证并 DONE 后立即由 live next 领取/start，不等待定时触发。依赖 OPP-001/PRV-002 已 DONE。

## 实际基线

`internal/opportunity/model.go` 和 `internal/postgres/opportunities.go` 只为本人 active FIND_ACTIVITY 读取已授权活动/Place，按本人好友主办、Community 与公开供给排序；不是新朋友匹配。`social_intents` 已有独立类型/约束/受众/激活/到期/Block 可见性合同。现有公开 Person 资料和人员意图搜索本身不是可信新朋友路由。Friend 请求→接受→Tie、主动好友对话的真实持久路径已经存在，不需再造绕过同意的聊天接口。

SOC-001 的本人私密关系统计不能用于向陌生人披露私聊、联系人名单、亲密度、位置或学校/身份推断。Person Context 私密声明不能作为对外匹配资料；已有 Profile public 也不代表默认同意被匹配。没有真实供给时返回空，禁止补虚构用户。

## 本项计划与门槛

先形成新朋友路由 canonical：明确独立默认关闭/可撤销的被匹配选择，真实活跃 Person、当前公开资料及本人主动发布且当前阅读者有权查看的有效伴侣 Intent 作为来源。只依据允许公开使用的明确类别/模态/约束匹配，排除本人、已连接/Block/非活跃/私密/未选择/失效来源，结果解释事实兼容条件，不声称心理或私人记忆推断。线上不强填城市，线下未知范围不猜距离/坐标。

随后最小完整 SQL/HTTP 与中文消费/邀请入口，沿既有持久好友申请和接受边界；查看候选不自动邀请、建 Tie、入聊天或通知。再次操作必须重新核验来源/授权，关闭/Block/过期后停止读取。需要实际策略、匹配/数据库、UI 空/错/邀请确认、迁移回归/构建和本地场景证据。此段记录实施前计划，后续结果见下文。Closed Pilot NO；V5 AGE 保持 QUEUED。

## 完成证据

`BT-V4-OPP-003` 已实现独立默认关闭的被匹配选择、双方有效且互相可见的明确找伙伴意图、有限规则/中文解释、最小候选输出、事务内重新核验的确认邀请，以及中文草稿→公开确认→主动查询→留言确认流程。公开资料或 SOC-001 授权不自动开启；线上不要求城市，线下不猜定位，候选不进入地图。已有好友/Block/pending 排除，取消/撤销立即拒绝旧结果，接受方主动确认才产生 Tie，无自动聊天。001–052/三个 seed/full Go SQL+HTTP/旧 ID/down-reapply、Go vet/build、Flutter analyze/147 tests/APK 以及 Android 16 本地合成全流程/重启/调试 PASS；确定性并发抓出的真实 40P01 已修并重复 60 次 PASS。见 [可核对证据](../testing/evidence/new-people-2026-10-02/README.md)。不使用私人记忆/聊天推断人格、好友兴趣、协作质量、时间或身份；本地验收不解除真实 IdP/供给/部署/运营与 Closed Pilot NO。
