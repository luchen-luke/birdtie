# BT-V4-NOW-002 实际代码审计与实现计划

日期：2026-10-04；负责人 memory_decay；exact lease work/v5-age038-resume/now002-start32.json。原始只读发现与依赖审计保存 work/v5-air015-consumer/NEXT-NOW002-READ-ONLY-AUDIT.md，本文件为实际实施增量。

## 审计结论

- 已有：036SocialIntent/037真实目标与邀请/038原Task稳定引用、本人创建/列表/详情/旧激活取消、公开机会投影。旧Now `active_intent.dart` 只是 AgentTask 条件投影。
- 部分：既有意图可直接操作，但旧批准为 confirmed boolean，无具体版本 CAS；无 edit；expiresAt 是寻找截止，不是活动时间。
- 未覆盖：三方式/六受众/时间/地点结构化编辑、具体检查批准、同原ID修改退回草稿、取消及明确 local reset；原生 Source/Session ABA 与最终等待后权限核对；普通本人中文管理卡与详情。
- 本地可实现，无外部凭据：新 native Gateway 复用原表，无DDL；原 constraints 增加 optional startsAt/endsAt；原生真实公开 selector；短期 AEAD 具体批准。
- 继续门槛：不得把模型/候选保留/外部消息或生产试点视为本次效应。legacy boolean 不因新端口自动升级。独占卡/页与root/MAP shared入口串行协调，无共享源码越界。

## 按序实施

1. 原时间窗口 case 在旧parser跑真实RED，保存time-window-red1.jsonl；规范与归一化增量保持旧字段。
2. native复用原当前身份/row/source xmin，关系锁预取、原ID CAS、具体operation/输入/截止密封；编辑→DRAFT，原激活路由复用；最后同SQL全部源与身份核验。
3. 真实 native 隔离库矩阵：六受众×三方式、跨本人/组织、source/City/Profile/Agent metadataABA、隐藏源中性取消、Session撤回/自然到期、真实audit等待后的deadline/phantomblock、双批准一效应、实际进程新key失效。
4. 真实server注册HTTP +7原生wire；具体multi-field edit保留ID；旧confirmed拒400。
5. Chinese API/controller/page/card，具体后果、localreset、unknown原ID只读、当前运输端/账号工作区ABA、ownedDialog退休、滚动大字与DateTime本地选择。
6. 独占target+analyze冻结，root统一 whole083/Flutter/build/本地真机，并按原需求/实际证据更新queue。

## 实际故障与修复

- time-window RED：旧ParseConstraints拒startsAt/endsAt，随后真实约束增量。
- initial Dart compile：super参数未成为构造body局部变量，以及编辑Dialog括号；修显式参数与括号，不降低闭集。
- native3/4/5：五真实等待负例均完成原安全拒绝，但终态断言SQL把uuid id与text audit.resource_id共用未cast参数，42883；最终显式uuid/text cast；旧断言完整，三失败帧保存。期间PowerShell双引号插值修复未实际落更改，重复真实失败保留，没有声称绿。
- native7：兄弟WIPagent_runs测试调用不存在flags.Snapshot，使PG测试/vet编译失败；未修改兄弟scope或排除test，root修实际Disable后重新全target。
- client-target1：负例在推断Map<String,String>上插double，以及误用pageBack寻找不存在AppBar返回键；修fixture类型和真实handlePopRoute。12成功/2失败保存，不称初轮全过。
- wire extractor最初未重组Go test Output分片，真实JSON截断；改按Output拼接解析，不编辑原生wire。
- 实质解析审查：原jsonEncode直接比较会把JSONB键排序/合法RFC3339偏移不同视为不同草稿；改按具体结构/UTC时间深比较，native multi-field EDIT wire已通过，未放宽内容差异、owner、版本或期限。

## 最终本范围证据（待根合并）

work/v4-now002/freeze1.json：17源码/测试（9 Go、8 Dart）。native9/result.json：64PASS/0FAIL-SKIP/test-vet-build0、复制820源稳定；nativeSchema实际001–082，含兄弟083源码仅作为编译依赖观察，未称原生083迁移验收。完整旧public/catalog/xmin/downreapply和DROP真。client-target3.log：15功能+4loading成功事件、0失败跳过，client-analyze4.log8源0。真实wire7帧在docs/testing/evidence/active-social-intent-2026-10-04/native-wire1.json。root新whole/截图/辅助技术结果独立，不能挪旧9718为新源码证据。

freeze2补实际物理布局：原font2测试不改变实际WidgetTester.view，不称320证据；根安全点授权后已改view320×640、DPR1、font2及真实keyboardInset220。RED1仅滚动fixtureNoElement，修helper滚动后真实目标/键盘绿，无产品修改。client-target4与analyze5全绿，freeze217源/真实wireSHA保存；freeze1不覆盖。

## 整仓并发只读断言隔离修复（freeze3）

ROOT full083-joint1 的 SameIDEditActivateCancelAndNoReadsWrite 真实失败保留（GET/preview wrote domain），另整包10分钟超时不是成功。该断言比较共享测试库全部 public 行；失败区间另一 HTTP package 的 registered native fixture 同库运行，28事件重叠，包含既有 Authenticate 的正常 session 更新及独立领域操作。原失败仅存通用错误，未记录具体差异行，不能将某一行写者猜作已证明。

仅该 test 开头复用 ownedMigrationDatabase(t)，实际建立独占数据库并应用完整当前001–083及原3seed；Gateway 原完整 public 零写等式、所有生命周期/版本/期限断言仍原样，不排除 Session 或弱化安全边界。native10：64PASS/0FAIL-SKIP/test-vet-build0；复制820源稳定，runner parent001–082 原完整非空数据/7catalog/Participation xmin/unused down-reapply/清理真；该隔离 child001–083 的完整 public 零写与生命周期实际通过并 DROP。父库与子库迁移范围分列，不称 native10 默认整仓全部通过。

freeze3 只有一个 Go test 文件变化，产品9源中的8源及全部8Dart字节不变；原 freeze1/2、worker-final1 与整仓 RED 均保留。ROOT 重新默认 whole083-joint2（显式20分钟期限）独立核证。delta 审计/最小diff/真实子库83日志在 work/v4-now002/whole-no-write-fix。

## 真机匿名入口白屏修复（freeze4）

ROOT e63eed Debug APK/旧本地081 API真机点击“我的社交意图”后，Card !_data.current 返回空 SizedBox 导致空白弹窗。真实PNG own-intents-anonymous-ready1 与UIA保留，不能把初版界面称全可用。仅 Card+其test 增量：匿名中文登录说明、组织切回个人说明、永久退休身份中性重开说明；不显示旧私密数据，不新增读/写。关闭只接显式 onClose，48dp；inline 不传、不能通用Navigator.pop误退出Now。MAP唯一writer另接实际modal关闭与真实Map入口测试。

RED1 3失败；GREEN1仅旧title判断误把新增中性标题当旧Page，改为实际ActiveSocialIntentPage缺席及中性提示存在，原请求数断言保留。GREEN2原4target合计17功能+4loading PASS/0FAIL-SKIP，局部analyze2=0。原白屏、失败日志、freeze3及worker-final1/2不覆盖；Go9源保持freeze3。fixed APK真机/整包Flutter由ROOT随后执行，未提前称通过，TalkBack未运行。
