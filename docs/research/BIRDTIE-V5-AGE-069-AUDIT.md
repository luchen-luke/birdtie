# BT-V5-AGE-069 Memory APIs 本地审计

日期：2026-10-06。原源 AGE-069 要求 GET memories/detail、UPDATE、DELETE、REJECT；原 completion_scope=CODE_AND_LOCAL_VERIFICATION。root 已逐项审计并调整其 proposed AGE-007 整项完成依赖，007仍 PARTIAL、原映射对象/发布门槛保持。本项不获取模型、概率校准、生产数据或真机操作授权。

## 去重与实现

原 GET list、PUT、DELETE 已真实存在并保留。新增本人稳定 ID GET detail 和 Memory 路径绑定 REJECT；原 Memory ID/version、056状态、Evidence、094具体人工 Preview→Confirm→Receipt 继续为真源，不新增 DDL/批准表/推断 writer。API authoritative 合同见 `docs/architecture/AGENT-MEMORY-MANAGEMENT-API-V5.md`。

| 原 AC | 实际接口/实现 | 原生验收位置 |
| --- | --- | --- |
| GET memories | 原 GET list/ReadOwnMemories | `TestMemoryAPIHTTPNativeRegisteredFiveOperationsAndRecovery` 的原注册列表 |
| GET memory detail | 新 CurrentHumanStore/GET registered，seal后编码末核 | `TestMemoryAPINativeDetailStableStatesAndZeroWrites`；`TestMemoryAPIHTTPNativeEncodedThenRealCurrentChangeZeroBody` |
| UPDATE memory | 原 PUT/PutOwnMemory expectedVersion CAS | 上述 registered五动作用同ID从version1更新到2 |
| DELETE memory | 原 DELETE/DeleteOwnMemory，DELETED清正文 | 上述 registered五动作另一个真实ID的原DELETE |
| REJECT memory | 新path-bound原094 REJECT adapter | `TestMemoryAPINativeRejectExplicitReservedExactOnce`；registered原REJECT预览→新路由version3；同operation100次不重审计 |
| 当前身份/权限/版本/来源 | 原native binding、完整private seal/后编码native复核 | `TestMemoryAPINativeDetailAuthorityABAAndNoPrivateResult`、`TestMemoryAPINativeDetailSourceChangeRetainsIndependentDeclaration`、`TestMemoryAPINativeSessionABARejectsOriginalDetail`、GuardMissing、WrongBindings |
| 时间与迟到 | PG实际specific relation waiter、原Memory/Session期限末核 | `TestMemoryAPINativeRealRelationWaitAcrossOriginalTTL`、`TestMemoryAPINativeRealRelationWaitAcrossSessionDeadline`、`TestMemoryAPINativeShortestLeaseAndExpiredRejectZeroWrites` |
| 无新激活/假概率/跨主体来源 | modelAccess=false、EXPLICIT原声明/reserved形状、optional缺失503 | pure矩阵、HTTP闭集/optional缺失、原生跨人/组织/newSession/Candidate namespace负例 |

上述名称是源码与验收定位，不自动代表每个历史帧通过。定向终态/当前root合并结果分别在证据中追加，未运行保持未运行。

## 保留的真实失败

- unit1：21 pure PASS，HTTP package未编译（新 handler局部变量与 request变量重名）；测试/ vet/build均1。修正仅新handler变量名，原source/raw保留。unit2：52 PASS，test/vet/build均0。
- native1：51 PASS/10 FAIL events/2 package failures，HTTP native unused变量导致编译未到业务；六个PG子失败（与四个父失败）发生在新fixture违反原immutable source、Memory/Profile revision、session_expiry guard时。原guard拒绝正确，修fixture为原native INSERT/合法version递增/真实idle到期，不放宽生产保护或业务断言。其已实际关系等待跨MemoryTTL用例通过。
- native2：97 PASS/2 FAIL（一个子及父），新HTTP合成key包含大写，原NormalizeInput拒绝，未到seal tamper业务断言。改合法小写key，保留失败与严格后编码断言。其真正注册五动作及全部PG用例通过。
- native3：99 PASS/1 FAIL，实际Session-bound relation waiter跨idle deadline已拒绝无正文，但ErrUnavailable与预期ErrForbidden不符。原authority在到期后为NULL，旧string Scan把权限失效归为服务错误。仅新PG捕获改nullable authority明确Forbidden，不改变原094/Session writer、截止时间或拒绝门槛。此为错误分类RED，不能称旧帧泄露了正文。
- session-aba-red4：精确单函数两个子case加父共0 PASS/3 FAIL，无编译/harness错误。同Session UUID token或revoked字段变化时旧detail已拒绝；恢复原值后旧proof返回nil，并且full public rows/xmin显示verification本身无写。仅新PG私有hash补完整Session row+xmin，原094 authority与批准合同保持。原source/raw不替换成新GREEN。

## 数据、源帧与发布边界

所有native帧使用原 ownedMigrationDatabase/v4PrivacyHTTPDatabase，随机本人合成记录、自有数据库；整套137旧表、48非空旧行/xmin、094旧catalog、unused down/reapply和native后父140表rows/xmin/catalog均保持。SQLabsence从实际raw所有库名核，不能用空ownedHTTPChildren字段漏数；原runner旧label数组为空时，allRawEmittedChildDatabaseSQLAbsence仍真实记录新MEMORY_API两HTTP库。

基线是 root-whole094-fixed38ni/source 的完整970文件原字节；manifest SHA256 `34b20553c48c3891070f2f0a307f1dc2b591bc4d1417532a68578ff80f745af6`。每轮本任务八Go overlay中server已在原970，七新文件使输入977。并行018新095与其测试、旧candidate fixture变化列入 live-source-observation，明确excluded，不能将本目标称current095或whole。

无客户端改动，无新增schema，故本项不把旧Flutter或Widget截图作为新增UI绿。phone由root统一，本worker无ADB/设备操作；真机功能、OSSecure、读屏、性能与生产运营证据依实际独立验收记录，API定向绿不覆盖它们。race在CGO0/no GCC环境未运行。模型、自动写、vision/A2A OFF，ClosedPilotReady/ConsumerBeta NO。队列与六共用报告仍由root独立核证更新。

## 2026-10-06 定向终态（非 current095 whole）

`work/v5-age069-memory-api/native5` 实际103功能 PASS、0 FAIL/SKIP/package failure，test/vet/build/两CLI均0。完整977 before/copied-after/current own八Go一致，source-before.json SHA256 `ed68dee2570875bb62339833030f20ec8f2a6d682b8866dccdf412a15b0bb240`。native runner SHA256 `9bd0e1f0e39658d168212409cd275045108f00ff95f1b09dce01bf2aa24594fd`。10 migrationchildren+2 registeredHTTP children+parent共13个实际自有DB，全由独立SQL核不存在。

两条实际Session ABA、原期限等待错误分类、原HTTPseal负例均在原断言下通过。当前原Memory/Session两个specific relation waiter都实际观察到阻塞并等待PG clock越过原期限；不是sleep后猜测等待，也不改变native row/xmin。真正httpapi.New路由在编码之后由测试port执行实际Memory/metadata/Session变更，然后调用真实PG revalidate，拒绝响应无私密正文且全public rows/xmin保持；sealLeaseTamper是受控畸形证明，明确不当作自然TTL首因。

原unit1/native1/native2/native3/session-aba-red4证据全保留。此worker目标完成只意味着原069定向合成native合同及本地构建通过；root须另帧使用当前018/095与合并源码全量回归，才可更新原任务状态。八Go交付冻结后不继续改产品源码。专属归档、原AC矩阵、命令与完整raw/源帧索引见本项evidence目录；未把011旧Flutter或已重连手机称为本API的真机验收。


## root current095联合终态（2026-10-06 root38pb）

原本地AC已DONE。当前981输入Go全仓11126 PASS/0FAIL-SKIP/packageFail，五命令0；原11003/72/103分支及multiplicity保留（仅随机fixture UUID归一，首次24 raw名字差异的harness失败保持）。094原137/48与095全140 rows/xmin/完整语义catalog保护、unused down/reapply、末parent精确同，369实际RAW owned库根SQL不存在。root不可变1431文件manifest 33b1d43ef35a96b8fbab9e638e0a425c8f42f431ff13e0c28661d34ed61c97be，根证明work/v5-age038-resume/whole095-joint-root38ox.json、状态证据local018-and069-closure-root38pb.json。原定向/RED/archive资料完整保留；非current095手机/生产调度器/真实IdP/CSSA证据。原007校准/INFERRED ACTIVE及attendance门槛未改，model/真实自动写/Vision/A2A OFF，Pilot/Beta NO。
