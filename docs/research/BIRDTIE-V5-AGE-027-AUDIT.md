# BT-V5-AGE-027 原生 Place Memory 审计

2026-10-03。来源为 live queue 完整 task、`BT-V5-AGE-AGENT-ENRICHMENT-EPIC.md:1071`、认知ADR、单一 Memory／064／005 与 V4 Place/Moment canonical；前置预审记录在 `work/v5-air011/preaudit-age027.md`，不是重新导入队列。只有根代理改 live queue 与共用报告。

原文支持 visited / saved / liked / created moment at / attended activity at，五种信号须分开。原依赖005/064/V4-MOM001/V4-PLC001/INT001已DONE，不表示出席／到访事实已存在。

| 原要求 | 修改前实际范围 | 本轮实现 | 未完成范围 |
| --- | --- | --- | --- |
| saved | REAL Save/RemoveSaved、saved_items 实体 | 当前本人 Place bookmark 元数据与真实064版本投影 | 不推断喜欢或到访 |
| created moment at | REAL Create/Update/Withdraw、private/draft Place FK | 当前本人明确关联与真实 Moment revision | 不称亲历、出席或公开地点内容 |
| liked | 无原生地点 like writer | 本人 EXPLICIT PLACE 严格 typed 喜欢声明 | 不提供隐式／模型推断偏好 |
| visited | 无 check-in/visit writer | 本人严格 typed 到访自述，明确未经核验 | VERIFIED native visit 仍 UNAVAILABLE |
| attended activity at | participation going/pending/cancelled，Plans completed仅时间投影 | 明确 UNAVAILABLE，不接受 attendance 写入或来源推断 | 缺出席权威事实、当前版本、撤销与验收 |

扫描未发现已有 Place Memory canonical，新建唯一 `docs/architecture/AGENT-PLACE-MEMORY-V5.md`。仅新增 `agentplacememory` 与两个专属 Postgres 文件，无DDL；复用原 `putOwnMemoryInTx` 和 DeleteOwnMemory，不修改其源、不建第二本 Memory/Evidence，不扩014/064目录或015消费者。当前 Moment 域已有015实际hook，隔离验证基线为001–064；不继续使用前阶段062作为本轮基线。

读口是当前 human self 管理，不是 ContextBuilder 或认知 MemoryReader。会话／Person／exact Agent／metadata／当前 Place/City／来源状态版本和真实 PG clock 均重新检查。回归保留同 owner 不同声明性质、原生收藏移除／重加、Moment 编辑迁移／撤回、CAS／重试／终态、跨主体、隐藏／过期、撤权恢复、defaultRR晚更新以及真实锁跨期限。输入和输出拒绝伪造 confirmed、源许可、敏感正文／图片／坐标，默认 processing/verifiedVisit/attendance UNAVAILABLE。

首次失败完整保留：native1 两次 clock_timestamp 的微秒差导致严格读取租期边界偶发拒正常读取，已统一同一真实服务器时点；native2 fixture直接更新 Memory期限没有推进原生version，被056trigger拒绝，改为真实短期声明与自然过期；native3 fixture尝试把已发布 Activity修改到过去，被真实领域writer拒绝，改为原生短期发布／报名／等待结束。它们没有放宽真实权限或改旧DDL。

原标准未完整覆盖，建议PARTIAL：到访自述不代替已核验 visit，RSVP/活动结束/取消也不代替 attendance。恢复条件是原领域真实事实源+current resolver/版本/撤回，再实际原任务验收；私人消费UI/真机/移动恢复/辅助技术尚未实施或运行。原ClosedPilot外部身份、HTTPS/生产地图、真实授权供给、日志与值守、提醒运营和A→H未齐，保持NO。

最终命令、PASS/FAIL计数、四源SHA、完整旧public快照与自有库清理以 [归档 README](../testing/evidence/agent-place-memory-2026-10-03/README.md) 为准；纯合同测试、scope真实PG和全默认Go回归分别记录，未运行不推定PASS。
