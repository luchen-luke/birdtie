# BT-V5-AGE-033 仓库审计与实现记录

2026-10-03。读取 AGENTS、V4/V5 执行协议、live task 全文与 AGE-033 来源、认知ADR、Context Access Policy、Current Context、Private Memory、Policy APIs 和全局 UX 合同。在当前安全检查点增量实施，不重写队列或旧任务证据。

## 去重与真实差距

| 能力 | 原始审计 | 本项结果 |
|---|---|---|
| 规则活动/地点检索与当前 Task query | REAL，多个 handler 各自装配 | 复用原域；统一 Builder/current-native read 并消费当前公开内容 |
| exact Person Session/Agent/metadata | REAL，其他本人领域已有边界 | 本 Builder 真实 RC/UTC 窄只读桥，当前事务与最终 PG clock |
| 本人 Private Profile、明确 Memory、三类 Policy | REAL，人类本人入口 | 有界选定 human review，不能借作 machine permission |
| 统一最小 Context Builder | NOT_IMPLEMENTED | 新独占包 + PG bridge + 原 registered Runtime hook |
| 六类私密机器上下文 | NOT_IMPLEMENTED / resolver不齐 | PARTIAL：当前端口拒绝，没有伪造授予 |
| Relationship 认知装配 | 缺实际资源目的授权 | UNAVAILABLE；未从聊天/报名/关系推断许可 |
| 模型出口、自动 Memory 写、执行 ledger | 不属于现规则检索能力 | 未开启；此项不替代 AIR/后续任务 |

本项总体建议 PARTIAL，不能以 CODE_LOCAL 抹掉原六源 Runtime验收。需要后续真实精确认知用途 resolver、撤权/源版本绑定与实际消费证明后恢复。

## 最小计划与真实实现

1. 唯一包定义闭集模式、选择、数量/文本/期限/source shape、服务端控制对象及本进程 seal。
2. 独占 PG 文件复用既有固定 SQL/scan，不新建 schema/账本/许可，不改 native 业务 writer。
3. Runtime 从既有 s.catalog 窄能力读取，保留匿名/组织路径与合法非公开领域结果；公开Bundle只含本轮公开最小源。
4. 实际 pure / native SQL / registered HTTP 正负，锁等待/最终时间/撤销/source版本；无外部 provider。
5. 原始结果、来源SHA/完整旧 public 行和自有DB清理归档，交根核证；只有根写queue和总报告。

## 失败记录

- pure1：测试 Family slice 类型编译失败，修成真实闭集family；pure2实际52通过。
- compile5：新 PG test 两个未用 import；删除后compile6三包可编译。compile7：测试将Store错当pinger，改测试构造参数nil。compile9/10：新human变化测试误用旧方法/Bundle字段名，改为实际Read/ReplacePrivate和Autonomy记录。全部原始日志保留。
- legacy-transport-red1：新生产窄能力缺失，两个原offline高影响只读测试503。仅在新owned test文件补明确offline companion adapter，green1通过，生产没有 fallback。
- native1：真实105通过，city_contexts负例误填completed违反实际active/paused枚举；改为paused。
- native2/3：新HTTP合成fixture坐标缺point精度违反现有约束，早期fixture cleanup未注册；修正确保point并先注册owned cleanup。残留/失败及 owned DB DROP 原记录保留。
- native4：真实114通过，Block返回安全503；原scanActivity把pgx.ErrNoRows转foundation.ErrNotFound，新桥据真实domainNotFound映射403。原失败保留。
- native5：真实126通过，新human expiry/future负例违反既有 Memory时间/Profile版本guard；调整为真实未来短expiry后等待，Private新writtenVersion再未来时间。不会削弱原源约束。

最终结果只以对应最新源码帧/原始证据记载。没有运行的全回归/移动端验收不计通过。
