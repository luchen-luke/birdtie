# BT-V5-AGE-011 本地实施审计

## 原要求映射

| 原 AC | 实际实现与精确证明 | 当前界限 |
| --- | --- | --- |
| edit/correct/reject/delete Memory | 原 Put/Delete CAS；MEMORY REJECT→DELETED、Candidate REJECT→REJECTED；`TestMemoryCorrectionNativeLifecycleExactOnceAndMetadata` 与注册 HTTP `TestMemoryCorrectionHTTPNativeRegisteredLifecycleAndClosedRecovery` | 不增加 AGE069 detail GET；不造新 Memory 状态 |
| 降低旧 inferred、保存 correction | `TestMemoryCorrectionNativeNegateReservedInference`：原 INFERRED/PENDING_REVIEW ID/version+1 DELETED且清正文/structured；不同 ID EXPLICIT ACTIVE negative | 没有校准概率、INFERRED ACTIVE writer |
| 防止马上重新推断 | manual新ID/新source、single/multi新来源/旧Approve真实拒绝；`NegativePersistsAcrossSourceAndMemoryEdits`、`SingleProducerOldApprovalAndNewSourceBlocked`、`MultiProducerOldApprovalAndNewSourcesBlocked` | suppression未提供 reallow writer，删除纠正声明不恢复旧批准 |
| 原 Moment 删除不能 ghost | `TestMemoryCorrectionNativeMomentDeleteOnlyEvidenceNoGhost`：actual Withdraw/DELETE唯一 Evidence，旧candidate/reinforcement预览拒绝、current support清空、candidate EXPIRED且scrub，独立人类声明原行/xmin保持 | metadata marker不是后台消费；AIR018后续复用其真源 |
| 主体/来源/TTL/撤权/ABA拒绝 | `ChangedAuthorityAndTargetZeroWrite`、`CandidateRejectAndSourceChange`、`GuardDisabledZeroWrites`、`DirectFalseReceiptRollback`、`WaitAcrossOriginalExpiryAndScrub`、`ReadRelationWaitTTL`，全 public row/xmin对照 | 不通过客户端 confirmed 或模型授予权限 |
| once与UNKNOWN恢复 | 100同UUID真实确认、exact once audit/receipt/native row不动；新 Session POSTForbidden/GET metadata；客户端ID-only未知 journal | 当前COMMITTED与当前匹配分开，404保留UNKNOWN |
| 必要schema与迁移 | 094三表；fresh/nonempty old 137表+48旧行/all xmin/catalog，unused down/reapply精确还原；used down55000拒绝/history不动 | 仅隔离库，不做生产down |
| 中文客户端与验收 | 设置真实直入口、4新Dart文件、独立API/controller/store/page/Settings测试；正常/权限/恢复/ABA/320/font3/IME/48dp/深浅/semantics | 当前目标仍收敛；最终结果追加，不能拿未测真机当绿 |

## 原失败保留

- `native1`：094 guard CASE 表达式语法真实失败，Go未运行；修SQL括号后另帧，不将未运行计通过。
- `native2`：273 PASS/1旧063 roundtrip FAIL；old DROP自然移除094 Candidate trigger，严格 catalog断言捕获，原raw保持。
- `native3`：279 PASS/0 FAIL-SKIP/five0，独占73实际DB全部SQL absent；原063字面精确trigger恢复正确，随后用capture原pg_get_triggerdef/enable/function的更精确fixture再定向1PASS。
- 根独立 preview disclosure RED：缺 affected MEMORY 原Record仍被纯DTO接受；同一精确原测试体在 pure2 修复后4PASS，candidate-only正例保留。
- 根独立 old093 relation-wait TTL RED：实际关系锁等待越原valid_until后返回ACTIVE，native行/xmin不动；同一测试体 current094 1PASS，实际等待屏障不改。
- 首客户端 `client-target1`：Secure read非Format异常后旧列表仍能触发1POST（期望0），真实业务RED；后续仅修存储读阶段阻断，原断言转绿。Widget fake async等待/semantics生命周期/lazy list fixture失败分别保留，不混作产品原因。

## 当前冻结与定向证据

`backend-final-freeze1.json` 引用 `reserved5/source-before.json` 完整970当前/copied输入，20 owned backend路径全SHA固定。native3 279PASS，roundtrip4/reserved5各1PASS且 test/vet/build/2CLI均0；每轮旧全行+xmin/catalog/down/reapply/parent末快照均保持。根整仓094已经独立启动，尚未在本段把它计作完成。

五态 Candidate 接口已存在并有 native/whole证据，007剩余校准任务保持PARTIAL。根只修 proposed依赖并保留原来源/AC/门槛/history，而非重建 backlog绕过。最终完成状态只由根核证原 CODE_AND_LOCAL AC后修改唯一live队列。

## 发布与真实验收

本地合成 owned fixtures不是生产数据、真实到场、真实Agent或CSSA试点；模型/自动写/vision/A2A OFF，Pilot/Beta NO。未操作手机、未运行真实OS Secure/TalkBack/VoiceOver。截图标 widget/mock与实际中文字体，不冒称真机。原部署、外部门槛与 AGE069专用接口/AIR018消费范围不由本任务自动完成。


## 定向客户端收尾（2026-10-06 本地）

最终 `client-target12` 为78功能+5loading PASS、test/analyze0，342源当前/copied/after全同；16 Widget/mock PNG保留。原Secure read实际1POST、UTC归一年0、旧expiry材料、future server迟到短lease、clock回拨与font3本次完成后结果868偏移的真实RED均保存后按原否定断言GREEN。Stopwatch只保守缩短本机review可用期限，不改变server deadline、Memory历史TTL或批准权；关闭重开仍只GET原引用。明确已核实本次结果才移到可见区域，不重置正在编辑的异步草稿。

根初次whole094虽然11003Go PASS/five0，父库保留断言为FALSE（旧MomentContext来源修改附带metadata0→5）；该原失败保持，等待精确fixture隔离与新帧。最终whole Flutter由root独立进行；本段不提前声明整仓或任务DONE。证据README与immutable `target-delivery1/manifest.json`列完整raw/source/DB和限制。


## 旧 fixture 隔离与中文收尾（2026-10-06 第二冻结帧）

根在相同970冻结源和完整父库保护下独立定位：原两个 MomentContext 测试自身 PASS，但把本应只在隔离库追加的 source invalidation metadata 留入父库，0→5。现仅两获准旧测试在原 disposable guard 后复用 `ownedMigrationDatabase(t)` / `v4PrivacyHTTPDatabase(t)`；HTTP pool 成功后只日志实际隔离库名。原 assertions、权限、ID 和 cleanup 全保留，不删除 marker，不修改094 guard或放宽完整性断言。

实际 `context-isolation6` 原 same2 pattern `^TestMomentContext(HTTP|Links)Integration$` 为2 PASS/0 FAIL-SKIP，test/vet/build/两CLI均0；原137表48行/all xmin/catalog、094全部140表末 rows/xmin/catalog、unused down/reapply全部精确保持。RAW发出 migration与HTTP两自有child，其SQL absence及parent DROP后独立absence均为0。完整970源 only两旧fixture差异；原20 Go/SQL freeze1字节不动。`backend-final-freeze2.json` SHA `ddcafc824614806ec9a458bc913aa58b5212b2f151b0b858b50c09a2c9a0f149`；完整970manifest SHA `34b20553c48c3891070f2f0a307f1dc2b591bc4d1417532a68578ff80f745af6`。

根初次全Flutter为1603功能PASS/5旧入口FAIL/165 loading PASS，analyze0/test1、build未运行，原342输入稳定。五次tap原文字已mounted但在默认800×600视口外；三个原entry fixture在既有真实scroll/ensureVisible后pump并要求 hitTestable，再执行原tap。所有原声明、权限和A-B-A断言保留，不扩大viewport、不屏蔽miss警告。page生产代码仅两中文短语改“更换会话”“仅个人助理范围”，机器标识不变。

`client-target13`与缩进收尾后的 `client-target14` 均83功能+8 loading PASS，test/analyze0；342当前/copied/before/after一致。相对target12仅三个旧entry test和page两短语变化，另九owned Dart字节保持；13 Dart最终冻结。`client-final-freeze2.json` SHA `165edb367a7ccafb4a00f87aa6d842554f010d7245417590c81149949801dc02`；342manifest SHA `5c113880f3245c63289c8a32d2b3024b3cbd4a1bae4728de6f797a7c379ad912`。16新PNG仍为Widget/mock，正常字体/大字与IME流程通过实际scroll确认动作与完成结果可见；手机、真实OS Secure、TalkBack/VoiceOver NOT_RUN。

新不可变附录 `docs/testing/evidence/agent-memory-correction-2026-10-05/fixture-isolation-annex2/manifest.json` 保存174文件/13,215,730字节，SHA `d17e02e4e4351cf2e1804cdac9b31f42502e12081348f9d6b5d15b1ea3c59c29`，含当前35源码、精确commands/runnerbytes、完整父库row/xmin/catalog、实际SQL absence、target13/14 raw与16PNG、root两旧RED证明。旧1007文件target-delivery1及manifest不动。首preparer在LF/CRLF guard假设处于源写入前assert，Go未运行；该work-only失败脚本也保留，不计业务失败或通过。

本段只记录定向GREEN和第二冻结帧。根 whole094 frame2与全Flutter frame2已实际启动，完成结果需根独立proof另追加。旧整仓父库FALSE、旧五入口FAIL都保留，不以定向结果替代原整仓。默认模型/自动写/Vision/A2A OFF，Closed Pilot/Beta NO，未安装APK。


## 根独立整仓收尾与手机重连接续（2026-10-06，root38ny）

上述 RUNNING、未安装及用户离线记录是各原帧的历史事实。本轮根随后完成当前 frozen094 整仓：`go test ./... -count=1 -timeout=30m -json` 11003 PASS/0 FAIL-SKIP/packageFail，`go vet ./...`、`go build ./...` 及两个 CLI build 均0；970输入 before/copied/after/live 逐SHA一致，旧10922和上轮11003通过名字及multiplicity保持。原137表48行/xmin/完整semantic catalog、unused094 down/reapply、末全140表parent精确保持；341个RAW实际随机库由根独立SQL检查全不存在。原首次 parent marker0→5 FALSE及同源same2再现保留，未删除metadata或放宽断言；只隔离两个旧测试自有DB后的新whole真正通过。根证明 `work/v5-age038-resume/whole094-fixed-root38ng.json`，不可变1217文件归档 `docs/testing/evidence/root-joint094-fixed-code-local-2026-10-06/manifest.json`。

Flutter当前完整342输入 before/copied/target/after/live逐SHA一致；`flutter analyze`、`flutter test --machine`、Debug APK build 均0，1608功能+165 loading PASS/0 FAIL-SKIP，旧1534通过名字与当前83目标保留。五个旧Settings tap miss原RED保留，只三个旧entry fixture补真实scroll/pump/hitTestable，未扩大视口或消除原断言。真实registered native五JSON与最终client4互验另PASS，不重复计入1608。根证明 `work/v5-age038-resume/whole-client011-root38nh.json`；不可变370文件归档 `docs/testing/evidence/root-client011-code-local-2026-10-06/manifest.json`，16图为Widget/mock。

BT-V5-AGE-011按原 CODE_AND_LOCAL_VERIFICATION 已由根记DONE，其他252任务对象/source/门槛保持；闭合证明 `work/v5-age038-resume/age011-local-closure-root38no.json`。这不把007尚缺概率校准/INFERRED ACTIVE提升为完成，不把094追加最小metadata称已部署传播consumer或真实vector/provider删除。

用户明确重新连接手机后，根实际ADB安装并pull核对同一APK SHA `4611f432efd67ee5f911462eebb1b3a564bdd9449d2e26e3f0ceec20a7a0b9cf`，打开独立preview包并连接Flutter VM/DevTools；应用数据未清、原store Civu不动、已保存Mapbox配置复用。手机c641566b/Android16，独占新本地094合成API，不使用旧手机库。当前首屏PNG已实际捕获，`work/v5-age038-resume/phone094-first38nw/result.json`。初两次截图probe因Android16 window字段与多物理display前置warning停止，原输出保留；明确主物理display重试成功，不称app失败。此阶段只有安装/启动/attach/首屏与匿名设置登录限制真实观察；记忆修改/重启安全存储、完整Now六项、TalkBack/VoiceOver及Profile性能比较尚未验收。实际后续结果独立追加，不能从首屏或mock反推绿。

完成后018撤权传播与069普通Memory详情/reject API立即受控并行启动；复用本任务真源，不重复backlog。模型/provider/真实自动写/vision/A2A保持默认OFF；生产IdP、真实组织授权活动、HTTPS与生产地图、部署日志/值守、提醒运营和真实A→H仍缺，Closed Pilot Ready=NO，Consumer Beta=NO。
