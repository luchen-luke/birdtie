# 043 共同公开报名原生增量审计

仅依原 canonical §8 与 `work/v5-age038-resume/shared-activity-native-lease.json` 实施，原任务不复制。原参与真源021、原Activity组织031/032/042、现043引荐与048全局限制复用；不读取私密Moment，不把报名称到场。

## 简短计划

1. 078在原参与行附默认PRIVATE元数据与单调版本，既有状态/ID/更新时刻不因披露变化而改变。取消/重报清披露，版本触发器不锁Activity；新增独立原audit窄保护及无使用down。
2. 原生GET/preview零领域写，当前本人/Agent/Session/全部公开源最小frame/参与version/审计epoch绑定进程AEAD≤90秒。PUBLIC本人明确期限≤24h及source/ends，PRIVATE仅撤披露。
3. 当前锁序共享身份→Activity→Participation，全部写关系锁预取。源/身份/Session锁等待后与审计插入后均PGclock最终核验，失败回滚，不续token/假幂等。
4. 原引荐两次frame只加共同PUBLIC报名，双方原052/070及048限制生效，策略私密不输出；报名≠到场/成员，全部效果关闭。
5. 独占fresh078原生正负/注册HTTP、旧非空列投影+catalog/down/reapply、真实等待/版本ABA。当前客户端未授权，不修改已冻Community六Dart。初次失败与未运行范围保存。

当前新源尚未验证，不沿用旧9423整仓或72目标PASS；正式迁移/模型/消息/运营/发布均未执行，043及发布门槛保留。


## 本轮实际实现与冻结

初始计划中的“客户端未授权”是当时事实；随后根在 `shared-activity-client-lease.json` 正式追加六个普通本人 Dart 文件，并授权原引荐六文件兼容。Community 六文件及既有 1131/17 文件档案没有改写。

- 唯一报名真源为原 `activity_participations`。078只添加 PRIVATE 默认披露元数据、单调版本和独立原 audit namespace 的保护，原报名ID/活动ID/本人ID/createdAt不可重映射；取消或重新报名不会恢复公开。
- GET/options/具体预览不写领域数据。本人逐活动选择公开期限后，预览绑定原参与和活动版本、必要公开源摘要、不可逆审计 epoch、原Session稳定身份与当前期限，进程 AEAD 最长90秒；公开期限由人明确选择，≤24小时、source expiry及活动 endsAt。审批只发送原 opaque preview，不能由 boolean 或请求字段代替批准。重复批准409；重启旧进程预览失效，不能宣称持久操作回执。
- Source不公开或失效时本人只收到中性标题与必要原ID，仍可 PRIVATE撤回。来源frame不复制私有整行。Venue分支检查底层Place、所属City和当前独立审核及期限；原组织者授权不等于成员资格。
- 读列表最终在所有必要等待后同SQL核当前源与PGclock。预先取实际写关系锁，审批在审计插入等待后再核身份/源/block/期限，失败回滚；原Cancel路径不新增Activity反向锁。schema078保护缺失或disabled明确Unavailable，零披露或审计写。
- 原引荐最终两次frame同时复核双方公开有效报名、相同Activity、各自披露版本/source digest、原048展示限制、原052发现设置与私密070 SocialPolicy拒绝约束。`PUBLIC_REGISTRATIONS_ONLY`表示共同公开报名；实际到场仍UNKNOWN。旧服务 `UNAVAILABLE`不能生成共同报名依据。模型、消息、邀请、Memory、成员资格效果均关闭。
- 中文页面从原GET读取实际本人报名ID，无UUID输入。默认私密，用户先选择有限期限、检查具体预览再批准。公开后无刷新也可检查PRIVATE撤回；隐藏来源仍可PRIVATE撤回。未知提交只读核查，不重发旧preview，不将状态相等称本次成功。账号/Session/workspace变化永久退休原controller；same-key client/base/getter/listener替换销毁旧controller与原对话框，迟到响应不沿用。owned HTTP client一次关闭，borrowed client不关闭（后者有实际widget断言，owned一次是源码核查）。
- 原引荐页按当前DTO报告来源；070的共同公开报名偏好仍是原7类 CAS具体版本检查，另四类保持原值。它不发布报名，不代替原048共同信息展示设置或对方许可。

## 原始失败保留与真实验证

| 帧 | 实际结果 | 原因或覆盖 |
| --- | --- | --- |
| compile1 | 三包编译通过 | 仅编译，不等于原生验收 |
| compile2/native1 | 编译/目标初次失败 | 测试unused import和workspace拒绝fixture预期失配，保留原输出 |
| native2 | 81 PASS / 5 FAIL（包含父测试） | PUBLICapprove的time.Location指针相等是实际实现缺陷；统一原生扫描UTC。另等待fixture错误选择已锁Agent行，改为真实最早Account等待，不放宽当前权限断言 |
| native3 | 88 PASS / 0 FAIL-SKIP | 原Join→具体披露→原引荐真实闭环与权限/ABA/expiry/进程重启 |
| native4 | 96 PASS / 2 FAIL（包含父测试） | host account fixture用了非法 retired enum；只改合法 suspended，断言保持 |
| native5 | 99 PASS / 0 FAIL-SKIP | test/vet/build全部0，750源稳定，真实post-audit等待期间phantom block回滚，Cancel/down等待、原数据/xmin/cat保存 |
| native6-catalog | 1 registered HTTP PASS | test/vet/build0，750源稳定；原非空全列投影/xmin、unused down/reapply与七类可见语义catalog恢复、ownedDB实际DROP |
| 根 whole078-1 | 9470 PASS / 5 FAIL | 旧TTL fixture改写不可变createdAt被正确078 guard拒，根在原两测试scope修fixture，仅保留原updatedAt过去16分钟与相同到期断言；未放宽产品 |
| 根 whole078-2 | 9477 PASS / 0 FAIL-SKIP | 已独立读取根result：全go默认并发test/vet/build0，750源稳定、原数据和七类catalog保存、ownedDB DROP。此帧两旧fixture与worker native6帧不同，13 owned产品/测试源相同 |
| client2 | 34 PASS / 3 FAIL | nativewire实际2preview/5view的计数fixture错、hidden option local guard的真实客户端缺口、测试pageBack错误（改实际系统back）。完整保留 |
| client3/4/5 | 37 / 40 / 42 PASS | 逐次增加正负与真实7wire/2approve绑定验证，不替代最终帧 |
| build-notification-red1 | No tests，exit79 | 工作路径错误；不冒称权限RED |
| build-notification-red2 | 1 PASS | 父build内workspace通知未复现Navigator错误；实际绿色负例，不冒称修复 |
| client6 | 46 PASS / 4 FAIL | 新Intro fixture缺指定base和最终取消按钮应为原“返回”；根entry 3tests tile中心在屏外，根补ensureVisible。不改生产状态/权限 |
| client7 | 50 PASS / 0 FAIL-SKIP | 六worker目标文件+根3entry；具体期限与预览自然到期、未知仅GET、公开→PRIVATE无刷新、same-key/ABA/迟到/dispose、360×640大字1.8与键盘、≥48px批准触区 |
| analyze1 | 30 lint infos / exit1 | 本批新增单行条件缺花括号，未忽略规则；修改为有界代码格式，修后分析0 |
| analyze3 | 12 items / 0 issues | 冻结后的12 Dart文件，exit0 |

实际命令与每轮JSON/日志保留于 `work/v5-age043-community-interest/activity-native*/`。Go目标为新domain+原引荐+PG+HTTP，regex `^(TestParticipationDisclosure|TestIntroduction)`，最终catalog帧为已注册HTTP生命周期；隔离owned fresh078，不Skip。源码先snapshot再执行。Flutter最终命令：

```powershell
flutter test test/activity_participation_disclosure_api_test.dart test/activity_participation_disclosure_controller_test.dart test/activity_participation_disclosure_page_test.dart test/agent_introduction_api_test.dart test/agent_introduction_controller_test.dart test/agent_introduction_page_test.dart test/activity_disclosure_entry_test.dart --reporter expanded
flutter analyze <对应12个worker产品及测试文件>
```

Go13 receipt：`activity-native-source-freeze1.json`；Dart12 receipt：`activity-client-source-freeze1.json`。注册HTTP真实合成响应原样归档为 evidence `native-wire.json`（7 entries/无Bearer），Dart测试不是另造fixture来代替这些wire。

## 门槛及未运行范围

本worker未运行新的ActivityDisclosure真机、TalkBack、完整Flutter build或正式HTTPS/IdP/模型/provider/生产migration；全客户端与真机由根完成后才能加证据。原seed与合成测试组织/活动不是运营资料。Native Venue/Person host有本轮真实矩阵；全部组织者类型并非都新建了独立fixture矩阵。可见catalog七类语义检查不代表PG物理内部OID完全相等。无CGO race证据，不将网络返回之后的撤回称持续授权。合法产品Session没有clear revoked路径，但不声称可以抵抗任意管理员SQL/DDL破坏。实际参加仍UNKNOWN，完整043状态与Closed Pilot/Consumer Beta由根按原需求和真实证据评估，本报告不得自行解除门槛。

本轮遵循 GLOBAL-UX 的具体预览/批准/权威结果区分、中文、必要已获准来源、明确未知、取消零提交、身份切换/迟到响应、移动与辅助技术未测实述（UX-CHECK-01至16适用部分）。impeccable context已执行，仓库无PRODUCT/DESIGN文件；按明确领域流程继承现有Material设置界面，不改repo外scope或另造产品规范。代码/移动widget检查已运行，截图/辅助技术仍需根真机证据。


### 8.1具体预览收尾与freeze2

根已保存250源/920功能+114loading全Flutter analyze/test/Debugbuild0的freeze1帧与ac43 APK，随后在自然安全点批准仅Page+test两文件收尾。具体预览现在实际显示native活动开始/结束、本人报名状态及受众；不会在隐藏来源PRIVATE撤回显示失效活动时间。client8仍50PASS/0FAIL-SKIP，analyze4两delta文件0，其他10Dart hash保持原analyze3帧。新 `activity-client-source-freeze2.json` SHA dddd9385c26d06ae6997c6dd8cc195fc41c7ae3c90a4e16b76be18dbbe4a633e。920旧整包不当作这两delta的新验收；根新全包/新手机仍待其证据。

运行诊断补记：首次client1输出相对路径多一级导致OutFile失败（未执行目标test，不当作PASS）；后续本地花括号机械处理曾导致三处多行语句format语法错误，已准确包围整语句，最终真实test/analyze重新执行。Intro widget第二次仍失败因具体确认取消按钮实际是原“返回”，只改fixture按钮名称，未改变原领域授权/生产动作。

原完整043三缺口（普通引荐UI、Community PUBLIC writer、SharedActivity授权公开来源）现已在代码与各局部证据补齐，不能沿用旧缺口称仍不存在。仍需根按原全项保存新Activity普通流程真机/两个普通账号共同依据实际消费与辅助技术范围；本worker没有这些新证据。更广Agent消费不是本次人工只读机器授权，模型/effect默认OFF是原约束，不另造旁路。Attendance仍UNKNOWN，完整发布/生产条件未解除。
