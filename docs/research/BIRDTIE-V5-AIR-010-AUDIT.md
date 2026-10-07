# 2026-10-05 根代理当前 089 联合验收摘要

根代理已完成 BT-V5-AIR-010 原 CODE_AND_LOCAL_VERIFICATION 范围验收；任务状态以根代理维护的唯一 live 队列为准。本摘要不替根代理修改状态，也不代表真实供应商或发布就绪。

- 当前 schema089 默认包并发完整 Go：10,652 PASS，0 FAIL / TEST_SKIP / packageFAIL；test、vet、build、两 CLI 均退出 0。
- 根代理独立核验 921 份当前/冻结执行输入（918 API 文件 + 3 原 seed），48 行旧 public 全部行内容及 xmin、semantic catalog、089 unused down/reapply 和测试前后快照一致。
- 根代理对实际记录的 245 个 parent/子测试数据库独立 SQL 核实不存在。证据：`work/v5-age038-resume/air010-air049-current-whole-root38he.json`。
- 本 worker 原 native4 的 xmin 仅覆盖 Participation；上面的完整旧 public xmin 来自根代理当前 089 联合回归，不能倒写为 native4 原 runner 的能力。
- 历史 PARTIAL、native1/3 初败和 root whole2 的 FK / 077 审计 TRUNCATE 兼容性失败均保留；当前 whole3 通过不抹去旧日志或归档。下面全部旧文档字节原样保留为历史与阶段记录。

唯一新增 native 测试 Go SHA 仍为 `57084b2bac96d6a796d2403662d1b37d0b48e5313dd2ddbe789118909aa30b12`，本次只追加文档摘要。原 worker-final1、source-final1/2、annex2 和所有执行帧不变。真实 provider / SDK / tokenizer / 供应商地域及保留 / 生产费用 / 真机089 / 辅助技术 / 生产调度均 NOT_RUN；默认模型网络 OFF，Closed Pilot / Consumer Beta 保持 NO。

## 历史正文与阶段证据（完整保留）

# AIR010 实施审计与差距

2026-10-03。来源：队列 `BT-V5-AIR-010` 完整source及 `work/v5-materials/BT-V5-AIR-BACKLOG.json`。使用现有007/008/027及默认关闭边界，没重复导入需求或新建队列。本项由根代理实际start；worker只修改登记scope，队列与总报告由root处理。

## 审计与实现

| 条目 | 初始实际状态 | 本轮实际结果 |
| --- | --- | --- |
| ProviderError | 已有007白名单分类，队列旧goal落后 | 原分类与两字段保留，未另建错误体系 |
| Retry-After | 规范化会丢弃原错误元数据 | 安全wrapper、header/duration闭集、实际离线传递及真实1秒等待；非法hint停止 |
| 有界重试/抖动 | 未实现统一策略 | 新modelresilience实际有限循环，次数/时间/route切换上限、可取消timer；纯抖动与实际context分开验证 |
| 能力与降级 | 008有真实fake执行和合成grant资格 | 复用008真实选择/执行；起始许可上界仅收窄，secondary结果准确标识，缺路由Unavailable |
| current来源/隐私 | 原生source-purpose/模型出口resolver尚无 | 每attempt与完成后复核合成快照及真实expiry；不能称原生授权 |
| 精确版本 | 027不可变目录与Task pre-run绑定可用，但PARTIAL | 实际BindRequest/ResolveReference固定；缺/改prompt/schema/tools等零调用拒绝；027历史Run缺口仍在 |
| 全Run预算/费用 | 011共享费用与016实际Run缺失 | 本次调用attempt/time有界；没有跨Run预算、费用预留/结算或持久恢复，不伪造ledger |
| 真模型/业务自动化 | Gateway关闭，无批准adapter | Service仍关闭，OFFLINE仅显式本地合成调用；无HTTP/main/工具/Memory写接线 |

## 实际失败与修复

1. `production-round1-tests.jsonl`：两个新HTTP-date fixture用time.RFC1123格式生成UTC zone，而HTTP parser要求标准GMT。按http.TimeFormat修正合成header；未放宽生产parser，旧原始失败保留。
2. `production-clock-red-tests.jsonl`：实际延迟合成resolver返回一个已经过期的更短grant，旧代码仍释放fake成功结果。不是mock声明成功：失败记录包含实际adapter调用与错误结果。补resolver后actual clock检查、能力/expiry最后复核，不通过重写checkedAt制造许可。最终before_dispatch/before_release负例均通过。
3. `production-final1-result.json`：530 test PASS，但vet exit1，新增跨包ProviderError非keyed literals被vet拒绝。只将新modelresilience测试改为keyed literals；007旧两字段nonkeyed测试仍保留。final2/3 vet0。
4. 一次apply_patch因gofmt行合并后上下文不匹配而拒绝，随后精确读取并重试；没有覆盖旧结果。前几探索轮没有生产before/after源清单，不能宣称其源稳定；正式final2/3才有完整8SHA。

## 正负验收及证据

正式生产final2/final3各530 PASS（007221/008130/027纯37/010142）、0FAIL/0SKIP，vet/build/dependency0。8生产Go源SHA前后稳定；仓库其它文件并行写入若有变化只能记录观察，scope不声称全Go/全部PG验证。完整原始日志、命令及实际源快照在[证据目录](../testing/evidence/model-resilience-2026-10-03/README.md)。复现脚本 `work/v5-air010/scope.ps1 -EvidenceLabel <独立新标签>` 不创建数据库、DDL或网络调用。

已覆盖RATE_LIMIT/TEMPORARY、Retry-After实际等待、指数/抖动/溢出、超大或非法hint、attempt/全截止/context/等待取消、合作式adapter timeout和迟到内容、原始permit新增destination/region/expiry/source/consent/policy变化、结果后的撤权及下一次前撤权、secondary真实descriptor、schema/工具仅提案/未知工具结果、固定版本、输入切片隔离、16并发独立本地计数、默认gate true仍关闭与grant不可JSON。

## 建议状态和恢复条件

**建议PARTIAL。** 未完成原实际Run剩余预算、原生授权的跨provider出口、真正硬网络timeout和持久恢复。011必须为每attempt建立真实预留、当前出口与结算/共享root额度；016必须创建实际Run/Step与remaining/deadline/retry_wait并引用027精确配置；009必须获批adapter后提供符合context的网络超时证据。这些接口建立后复用本内核，实际撤权、重启、并发/预算和授权降级复验，才重评整体任务。不要把本次counter或RunReference作为上述完成证据。

本项没有用户可见界面；没有Flutter/真机/辅助技术截图或真实模型/付费请求，race未运行（CGO0）。Closed Pilot / Consumer Beta仍NO。原生provider目的/预算服务缺失不是停止独立仓库工作理由；根代理可记录PARTIAL并继续其它READY任务。

## 2026-10-05 原PARTIAL的受控恢复与原生证据

根显式恢复原任务，完整原对象/来源/依赖/gates保留；获准唯一新测试文件与本canonical/audit/专属证据/work五范围。上文缺口和PARTIAL建议是旧日期历史，不能当当前原生实现事实。实际062/066/088已存在，直接复用原真实本人批准、四预算、单轮ticket及Run/Step，不新增模型传输、许可、DDL、HTTP或UI。新代码不是另一套韧性内核。

| 轮次 | 实际结果 | 准确归因 |
| --- | --- | --- |
| compile1 | `go test ./internal/postgres -run ^$`退出0 | 编译诊断，非业务验收；首gofmt cwd路径错误保存为HARNESS_PATH |
| native1 | 29 PASS、3 FAIL事件（两leaf与父）、test1其余四命令0 | 100ms包括native准备，adapter未被调用却期待调用后迟到；fault-injection fixture未到预期点，不是已证生产授权缺陷 |
| native2 | 32 PASS、零FAIL/SKIP、五命令0 | adapter确已收到ctx，再等真实截止后忽略取消返回；规范refusal的finish_reason由误写refused修为refusal并加强必须返回规范终态 |
| native3 | 146其他PASS、PG编译失败/vet1、build及两CLI0 | 新hook port只嵌LocalModelRunPort，漏原LocalRetryPort；PG新矩阵未执行业务断言 |
| native4 | 376 PASS、零FAIL/SKIP/package FAIL、五命令0 | 完整原062/088/配置/原OS/新41事件的稳定当前915输入帧 |
| pure-current1 | 四包530 PASS、零FAIL/SKIP | 同native4不可变源的旧纯合同兼容，不冒充native授权/真实网络 |

新native矩阵闭环：批准A/B版本及具体目的地不互借；140ms RetryAfter下界实际被等待，有界jitter算术与实际native额外耗时区分；拒答/未知/取消/迟到/无合格授权零备用调用；UNKNOWN费用NULL、占原上界与四层requests保留；新Run仍同root耗尽；未用备用metadata零Reserve。actual backoff hook只在原handle真正failure revalidation成功后改变真实源，保持原授权端口而非提供fake permission；hook及等待有界，未到hook明确FAIL。

原Run OS联合证据有四实际kill/restart/原PID与test exeSHA，恢复calls=0且账目原行/xmin不变；原Session、Task/Agent ABA、原批准/价格/配置/version/gate及pool/audit wait矩阵仍原测试断言。完整fixture账户/数据清理由原helpers完成，native4完整public行与semantic catalog前后相同。**本runner仅有Participation xmin，未采完整public xmin**；根后续joint当前schema另核，不补造旧帧。新trace不含私人query/answer；输出仅自有合成Run/Task/root/binding稳定ID和控制状态。

证据：`work/v5-air010-native/native1..4`原source/raw/result全部保留；最终Go冻结见source-freeze1.json，新test SHA57084b2bac96d6a796d2403662d1b37d0b48e5313dd2ddbe789118909aa30b12。本地scope是否满足原AC由根独立核证，未自行done/改live队列或共用报告。已配置真实供应商、收费、网络硬超时、手机/读屏及部署均未验；当本地scope完成也不解除LIVE/Pilot/Beta门槛。
