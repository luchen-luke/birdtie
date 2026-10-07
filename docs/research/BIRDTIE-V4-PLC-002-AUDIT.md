# BT-V4-PLC-002 审计与交付范围

2026-10-03。实际队列依赖只有BT-V4-PLC-001，acceptance是七个语义字段携source/confidence，verify是Fixture+ranking。队列保留来源 `BirdTie_V4_Execution_Package.zip!/BirdTie_TAPD_Backlog_V4.xlsx#Requirements`；未重新找到临时ZIP原件，不宣称重新解包原表。已读取V4总纲、AGENTS、V4/V5协议与持续UX规则。

前置只读审计在 `work/v5-age033/next-place-profile-audit.md`，本轮计划在 `work/v4-plc002/plan.md`。没有复用或修改Civu，未导入新backlog。已有Place稳定ID/公共来源和已审核Venue真实；原PLC003规则匹配真实，但七字段统一出处/判断未齐。私人PlaceMemory不能用作公共适合程度或真实到访统计。

| 范围 | 审计前 | 当前实现 |
| --- | --- | --- |
| Place稳定ID、公开生命周期、Venue审核 | REAL | REUSE，无旧数据重分类 |
| 七个语义字段、UNKNOWN、来源及confidence | PARTIAL | 新typed契约与持久067候选/档案；审核判断非概率 |
| 城市编辑/独立审核/撤回 | 未有该语义入口 | 新当前原生Session权限与CAS/API，复用真实成员表 |
| 社交适合程度排序 | 既有硬类别/容量排序 | 增量消费公开有效profile；类别和人数可解释重排 |
| 注册HTTP原生路径 | 未实现 | 根代理串行注册五路径，worker真实New路由验收 |
| 外部来源事实核验、真实场地 | 未验收 | 未核验，全部测试明确合成 |
| Flutter消费UI与真机/辅助技术 | 未实现本轮路径 | 本轮未改或验收 |
| 模型/自动分析/预约/足迹 | UNAVAILABLE | 仍UNAVAILABLE，无新许可 |

目标写入19范围按live lease保留；server.go属根代理独占，worker没有修改。正式固定基线只应用001–067；根051新068未作为本任务migration证明。并行其它源码观察变化和本任务owned17字节稳定分开记录。

最小实现不是空接口：实际Store插入/审核/持久版本、真实公开查询、注册HTTP和规则消费均可在隔离PG运行。SQL-only回退探针只证明schema保护，不是人类批准路径；原生业务验收另由真实Session/editor/API执行。

初次失败全部保留：native1会话idle时间形状及cleanup顺序/vet非keyed字面量；native3 HTTP fixture Place无默认UUID；native4 Venue amenities nil导致NOT NULL拒绝。已修fixture，未放宽生产权限。native6有两轮旧17源117PASS，但WindowsApps Python shim未执行补强编辑，新最终guard和非空down需后续label实测，不能借native6计为通过。compile命令0测试仅是编译，不能写作功能验收。

正式结果见 [唯一证据目录](../testing/evidence/place-semantic-profile-2026-10-03/README.md)。是否DONE由根独立核证和当前全仓回归决定，文档本身不更新队列。Closed Pilot Ready=NO；本任务原local acceptance和生产/Beta门槛分别记录。
