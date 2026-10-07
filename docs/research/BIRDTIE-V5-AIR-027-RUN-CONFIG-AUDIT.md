# AIR027 实际 ModelRun 配置恢复审计

2026-10-05；本轮任务为原 BT-V5-AIR-027 的受控恢复，原058阶段证据保持不变。

## 实际能力与计划

088 的 `model_request_runs.binding_id` 通过058的固定绑定和配置 version/fingerprint 外键定位中央不可变 prompt、schema、policy、tools 与 capabilities。真实创建已复用批准预览及预算来源核验。历史定位可组合现有 `ReadOwnLocalModelRun`、`ReadPinnedModelTaskConfiguration(false)`、`ReadModelConfiguration`，无需新增领域表或恢复执行接口。

本轮增加原生集成测试：真实预览、批准、创建 R1/v1；激活v2后创建R2；两个独立OS子进程只读取配置元数据；回退v1再创建R3。核对所有配置字段、中央制品摘要及Run与binding不同ID。另验证未知/失效版本、不可变SQL约束、主体/Session、Task变化后历史读取与当前执行权限分离，以及真实等待后失效。

测试输入为隔离数据库内合成主体、任务和本地价格，模型调用不发生。083确定性候选Run不被改称模型Run。默认模型端口、正式发布和真实试点门槛保持。

## 证据状态

本轮命令与结果尚待实际执行；不引用旧058或AIR011测试数量替代本轮验收。真机、TalkBack、真实provider与费用、生产身份、现实授权供给均不在本轮运行范围。

## 初轮真实结果与修正

compile1实际失败：Control字段应为ModelRunID，原private fixture跨主体access应取peer；初命令cwd导致rg/gofmt路径不成立。订正实际接口后编译通过；不改变生产代码。

native1：12PASS/7FAIL（含父事件）/0SKIP。版本测试错误比较包含后来新根的全owner账目；改为精确原Run/Steps/原root/task完整行+xmin，原账户计数仍另核。正文检查误匹配合法 air.answer.v1；改为检查JSON正文字段而不是schema子串。闲置到期fixture违反createdAt边界；单一PG stamp使createdAt确实早于expired idle。等待fixture在reader已持Session SHARE时再更新同Session而阻塞；自然expiry在起读前设定，撤销持同Session UPDATE锁后观察真实reader/阻塞者再commit。所有原始失败/timeout保留，未放宽生产约束或负向零副作用断言。

native2：19PASS/0FAIL-SKIP，test/vet/build/两CLI全部exit0；911 API源与3seed冻结，原完整public行、目录、原行xmin保持，独占父库DROP且独立不存在。native3将用最后OS metadata日志与独立子库absence增证运行完整ModelConfiguration/ModelRequestRun/ModelEgress组合，尚未以native2充当该组合的验收。

UX-CHECK-09/10/11适用的权限/具体版本/未知结果边界延续；本轮不改UI，314Flutter源、phone/TalkBack/provider均不声称新运行。

## 本轮最终专项核验

native3完整 `^TestModel(RequestRun|Configuration|Egress)`：372PASS/0FAIL-SKIP/pkgFAIL0，test/vet/build/两CLI五命令exit0。新测试19事件包含真正Run/Step及两个OS metadata读取；其余原相关配置、预算、dispatch、结算、故障与恢复用例保留。914输入=911 API与3seed，全部live/冻结执行字节一致。

全部旧public行及xmin在088升级/down/reapply和测试前后保持；目录核验为函数、触发器、约束、列、索引、表与策略的可见语义，不声称物理PG目录完全相同。留存合成旧行数量：48；参与xmin条目：0（空集合不作为真实参与正行证明）。10个新测试独占子库均经postgres查询独立不存在，父库实际DROP并独立不存在。两OS child均exit0且PID不同，原Run/Binding/Task字段与全部静态配置及prompt/policy摘要保持。

这是CODE_LOCAL专项组合验收，根全仓尚待运行，任务状态由根核证；无provider调用/支付、API服务重启、手机、TalkBack或真实试点证据。
