# BT-V5-AIR-023 本地权限快照审计（2026-10-06）

## 原需求与实现范围

原source `work/v5-materials/BT-V5-AIR-BACKLOG.json`：检索前 subject/tenant/purpose/resource ACL过滤、具体 consent/source/policy snapshot、执行前再验证。原依赖由根核全部DONE；旧goal无快照已陈旧，个人原Builder/Purpose真实实现复用。本轮只关闭实际组织Task入口/读取/自写/输出等待窗口，不启用任何模型或扩机器权限。

实现：新 `agentruntime/context_snapshot.go` 只 opaque server-local封装；新 `postgres/organization_task_context.go` 当前原生主体/源/Task闭包与只能收紧clock；原PG `agent_workspace.go` 只抽原同事务helper；原HTTP `agent_workspace.go` 组织 request-local facade和编码后末核。原server结构经实际读取无mutex/可变owned状态；仅复制依赖interface/scalar，不修改共享server字段。

真实个人缓存：新 `agent_context_snapshot_integration_test.go` 复用原 contextBuilderNative/contextBuilderPurposeRequest，通过原 Preview→Approve，正常idle不续期限、Policy原CAS ABA、撤原grant、跨owner、另一Service以及伪wire拒绝。原 consent_grants/Policy/Source seal全部保留，不另造epoch或账本。

## 原AC证据映射

| 要求 | 实际路径/证据 | 当前分类 |
|---|---|---|
| 当前subject/tenant/purpose/ACL，禁止同名/client伪主体 | registered Org GET/List/POST、Business actor/anonymous/forged token/crossOrg403/401；Org PUBLIC原domain SQL不借Personal invitation | native15绿，本地合成 |
| 角色移除后旧读取/dispatch不得继续 | 真City关系锁等待→原RevokeMember提交→403；原Revoke→Invite→Accept同member ABA旧receipt拒 | native1 RED→native3/13绿 |
| 缓存hit仍权限/版本复核 | 原Service seal + nativegrant/current源/Policy native_revision；不可JSON或新Service重建 | native10/13已通过 |
| 检索前版本与执行前验证 | POST前源快照，原Task同Tx锁后/audit后检查；输出先buffer/商业编码、最后同stmt native source/clock | native8/10 RED→native9/11/13绿，native15已补真实审计等待 |
| 正常会话刷新不当撤权；短界不得续签 | stable Session identity、不使用滚动xmin；每次观察短界共享private monotonic checkpoint | native6 RED→native7/13绿 |
| source撤回/ABA/自然期及等待 | 真实source/Task/Org/AgentABA；实际Session FOR SHARE等待后的sourceABA、City与Session自然期 | native15绿；不声称任意postCommit瞬时撤权 |

## 失败与HARNESS边界

每轮目录不可覆盖。native1/6/8/10包含真实业务断言失败；native12两功能叶子分别是旧Personal私密来源继承和本轮bound writer误把creator当当前member。native12 City期限的native正确拒绝类型由fixture ErrChanged订正为Org Forbidden，仍严守拒绝。native2无效SQL列、native4/5编译及空子库IN()查询、错误python cwd/path和unused import是开发/fixture诊断，不算生产权限漏洞。native10后Task ABA barrier改为真实native读取完成后才获取原Task行，消除在首次新读取前改变的合法新snapshot歧义；原旧raw保持。

旧menu pure fixture同一测试SHA9e2d0a11…：原whole095 baseline exit0，新增端口后current exit1（3组织子例503）。生产缺native proof保持503；根38pm已登记精确旧测试scope，本轮仅加明确OFFLINE_TEST_ONLY旧fixture adapter，不以fake receipt作为生产授权。原menu/权限/private marker/零业务写断言全部保留，新缺port503与真实registered native responseSafety同时通过。`menu-compat-red`保存原命令和raw。

## 执行帧与未测

固定已核whole095 source+本轮获租overlay，不把移动live当整仓验收。native13 986输入/26PASS/test-vet-build及两CLI0，完整旧public rows+全部xmin/catalog、094/095 unused down/reapply保持，自有父/所有HTTP子库真实DROP且SQLabsence。native14真实97PASS/2FAIL事件（同leaf与父）已保留：300ms短idle fixture在原native读取时就自然失效，没有进入本来要测的刷新窗口，属于HARNESS时序。native15固定合法4秒短界/等待4.2秒；全部107PASS/0FAIL-SKIP/pkg，5命令0，986输入和8获租Go/live稳定，原完整public行/xmin/catalog不变。主库birdtie_air023_ad31e7a223c8及raw提取的17HTTP子库实际DROP、SQLabsence。source-freeze1.json SHA b95eab682acdf5b69a11be42484a64be411fb75eac89fec99ebe40c6001f1a46；根独立target/whole待核。

本轮无Dart/Flutter/手机/外部服务/模型/部署，race未运行。原privacy与Org私密模型出口UNAVAILABLE、provider/自动写/Vision/A2A OFF，Pilot/Beta NO。queue与共用六报告只根维护。

## 精确边界与完成待核

原本地AC已由上述真实native路径逐项覆盖；无新增权限账本、执行写权或cache。Scope版本8Go仅新增3个native测试文件/一个opaque封装/一个PG实现，并改原HTTP与PGTask入口及旧纯测试fixture。原Personal私有邀请人类读取不改，机器Builder继续PUBLIC上限；Org规则查询PUBLIC过滤发生于原领域SQL检索前。源元数据闭包只作内部本次当前性复核，最终wire无snapshot/seal/proof；原结果仍原DTO。

权限线性化点为所有实际等待及编码后的最终native SQL；锁和期限仅原本次最短边界。不能声称提交解锁后至HTTP到达之间的任意瞬时撤权可收回数据，也未运行race、真实多人、AT、模型供应商或手机本批。Session正常idle刷新阳性及原API单调revoke负例通过，任意管理员复原所有安全字节不在保证内。root95基线是获准固定输入，不以此前11126或定向107替代待运行新全仓核证。列表原≤50不声称全量。


## 2026-10-06 AIR023 当前操作者与最终全量核验（root38rl）

当前任务已自然完成其原 CODE_AND_LOCAL_VERIFICATION 范围：986冻结输入，Go全量11157 RUN/PASS、0FAIL/SKIP/pkgFail，test/vet/build/两CLI均exit0。977个原981输入字节完全保持，原实现3项与会话夹具1项修改，另5新增；初轮11154/旧11126/定向123/失败轮已通过11155的所有分支multiplicity均核对，仅随机UUID规范。当前成员B合法200审计写给creatorA的真实RED已修：UPDATE审计使用当前服务端授权成员，原task creator和CREATE审计不改；伪主体400/撤权403零副作用。先前11155PASS/2FAIL保留，旧会话夹具双volatile时间违反idle<=expires，只改同statement时间，权限断言和生产认证策略不改。初 verifier 旧输入计数及猜测audit文字错误只修核验器，不改测试。

094/095 fresh/current全140表rows/xmin/catalog、unused down/reapply保持；388个实际raw HTTP子库及父库独立SQL不存在。最终1314文件逐SHA与zip复核，证据 docs/testing/evidence/agent-context-authority-2026-10-06/final-root38rk/README.md，独立核验 work/v5-age038-resume/whole023-fixture-independent38rj/result.json。模型/自动写/Vision/A2A保持OFF、Org私密模型口仍Unavailable；CGO0无GCC raceNOT_RUN。手机原349Profile/baseline981API，当前986未真机或生产IdP验收。

原255项只AIR023状态及证据改变，其余254整对象、原DONE、两录屏lease保留；当前171DONE/2IN_PROGRESS/8PARTIAL/60TODO/14BLOCKED。录屏修复继续，S4局部50PASS及148证据文件已根核，原创建API持久operation key/source409回执关联是仓库内部差距，不能称外部条件或完成功能。Now无目录底图解耦/几何仍实施，联合whole/build/device待当前源码安全冻结。ClosedPilot/ConsumerBetaNO，不以本轮GoPASS宣称产品可用。
