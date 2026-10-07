# AIR015 Phase B 实施审计与证据

原同一任务IN_PROGRESS，根独占live queue/总报告/共享server/main。scope依原21路径+cleanup两CLI路径，receipts在work/v5-age038-resume/air015-consumer-phase-b-lease1.json和air015-cleanup-cli-lease1.json。

079原Analysis→080独立Retention→082实际同Tx 063 candidate/064 effect/inbox/checkpoint实现。原Civu未复用，没新授权真源/Run/Provider、没有修改旧063 writer。原79/80不可变归档原字节保留；旧079 schemaGate只按已授权顺序down80→down79→up79→up80兼容测试保留原断言。

## 真实结果（不代替根全量）

| 帧 | 结果 | 说明 |
|---|---|---|
| native1 | runner失败；owned DB DROP | 新082 down文件名替换错误，无Native测试PASS |
| native2 | 18PASS /11FAIL | 新真实effect FK使旧fixture清理顺序失败；测试专属cleanup先清自己effect；不改生产FK |
| native3 | 30PASS /0FAIL-SKIP | 早期业务/fault/真实registeredHTTP历史帧 |
| native4 | 54PASS /3FAIL | 账户列display_name不存在、link city/确认必填fixture错误（含父case） |
| native5 | 128PASS /0FAIL-SKIP | 首完整故障矩阵/实际子进程CLI等历史帧 |
| native6 | 127PASS /1FAIL | 清理正向600ms首次提交Unavailable，无底层SQL细因；清理正向具体TTL2秒后过期断言未减，原350/450ms真实期限负例原样 |
| native7 | 131PASS /0FAIL-SKIP | 新checkpoint/metadata/intent闭集历史帧 |
| native8 | 132PASS /0FAIL-SKIP | 最终实际HTTP先Commit后断连→原ID恢复与0effect增量重试 |

final native8所有test/vet/build0，794独占源稳定、20current owned SHA逐一相同。old非空全public/Participation xmin/7catalog/up-down-reapply/native后全目录保持、history down原子拒绝、自有库DROP true。

首纯编译错误记录：初native测试写base.personID误位/PersonalRole不是真实enum；pure fixture SessionDigest类型必须[32]byte且feature config必含真实pilot闭集；HTTP New须原16参数+variadic Option。均按真实接口修复，未放松权限断言。工具显示原始diagnostic输出，native2..8原始json日志完整留存，不将无DB compile/fixture SKIP当Native PASS。

完整契约见 architecture/AGENT-CANDIDATE-ATOMIC-CONSUMER-V5.md；源冻结work/v5-air015-consumer/freeze1.json；原目标命令每帧commands.json。根全082待独立执行，唯一队列只能根核证后评估；本worker没有DONE、没有领取新项。

## 明确范围

真实native词法/授权/候选/同Tx效果已可受可信Memory+Enrichment控制调用；默认OFF，旧无许可consumer仍Unavailable0effects；对真实模型/候选自动提升没有权限。没有新客户端本批验收、没有phone/TalkBack/CGO race/production scheduler/provider；本地合成不作试点证据。Closed Pilot/Consumer Beta NO。
