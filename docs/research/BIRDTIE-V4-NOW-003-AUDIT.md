# BT-V4-NOW-003 来源审计与修复

原 goal：Expose useful friend/community signals without building an infinite feed。
原 acceptance：Shows bounded actionable signals: friend interest, shared opportunities, relevant community activity; no endless engagement feed required。

本次接受的具体范围是 current accepted Ties、好友明确可见意图和原规则活动机会；不把私密长期兴趣、RSVP/attendance、现实亲密关系或认知推断作为事实。既有 Now/Settings 入口与旧 SocialIntent/Opportunity API 真实存在，无需 DDL 或新 Feed 服务。

## 已发现并保留的真实失败

- `compile1.log`：新测试引用不存在的 DigestString；`compile3.log`：注册 New 路由后 net/http 未使用。已修，保留原失败。
- `native-red1`：runner 生成数据库名大小写与 SQL identifier folding 不一致，没有完成 native 权限测试。
- `native-red2`：HTTP 好友请求 fixture 缺少接收人公开 Profile，前置条件失败；不得称会话权限 RED。
- `native-red3`：真实 accepted request 源改为 withdrawn、derived Tie 保持 active，旧 ListTies 仍返回好友。HTTP 空 IN_PERSON constraints 前置失败。修复仅追加原 ListTies accepted/exact-endpoints predicate，原 Profile 字段逻辑保留。
- `native-red4`：实际注册 New HTTP 路由，经独占来源表锁等待和原 Session revoke 后，ties、visible intents、opportunities 仍返回 200 与真实 fixture payload。3 PASS/9 FAIL events 包含父/包事件；不把父包事件算独立场景。
- `native-red5`：原取消 SocialIntent/RemoveTie 在机会来源等待期间先提交，旧多语句机会读取仍给取消意图生成机会、仍保留 TIE_ORGANIZER。5 PASS/7 FAIL events；非 owned 来源并行变化真实记录为 false。
- `native-green2`：原生功能 11 PASS，但补 offline companion 同时改变 owned test source，整体帧 FAIL，不能作为稳定交付。
- `native-green3`：固定 8 Go 源实际 14 PASS/0 FAIL-SKIP、vet/build 0、完整 public/schema/461 来源稳定和 DROP。随后新增 post-JSON final session 时钟核验，因此此帧属于历史，最终源需重新证明。

Source/Tie adversarial SQL 改动均只在随机独占 fixture 库，明确不提供新用户撤销/重激活 API。HTTP Session revoke、Intent cancel 和 Tie remove 用真实领域/Session 源；所有 DB 标本是 synthetic fixture，没有真实用户试点或现实参与证明。

## 已实现及待最终核验

新 client controller/page 从三条现有接口只读投影有限信号。7 Dart `client-freeze1.json` 是客户端冻结帧，定向 13 PASS/analyze 0，包括原 018/019 入口回归；不等于全量客户端或真实手机证明。widget/MockClient/原 transport companion 标为 OfflineContract，不给 fake Verified 或测试时钟任何原生授权作用。

登录的当前本人读使用实际 PostgreSQL human gateway 和最后单条原生 payload SQL；原匿名公开意图继续旧原域。该桥只服务 ordinary human read，当前内容分析用途、模型调用、PrivateProfile/Memory/Agent 关系授权均未由此实现或开启。业务 revision、source opaque tokens 与数据库 clock 不混作概率。

在来源等待结束后捕获同一 SQL snapshot/数据库 clock，再进行当前会话复核。关系 ACCESS SHARE 与 Account/Session 行锁等待分别记录；未提交的撤权没有被虚称已经提交，最后 source snapshot 之后的变化也没有被虚称永久反映在旧 payload。

最终 worker native/source/clock/serializer 证据与源码 SHA已见下方固定帧，根独立复验另追加独立收据。根代理独占状态与发布判定，本文件不自行修改 live queue。

## 当前时钟与兼容源审查实证

- `native-final-auth-red1`：postJSON Session fixture 独立 clock 写入造成 idle≤absolute CHECK 失败，不能算授权 RED；改用同一 MATERIALIZED DBclock。
- `native-final-auth-red2`：实际 JSON materialization 后旧 Authenticate 等 Session UPDATE 行锁，绝对期限自然过期后仍 200 payload。原始失败保留。
- `native-review-red1`：真实055 API把 displayName 设 PUBLIC、粗 Profile private仍明确允许；新通用“好友”却丢合法姓名。PRIVATE 拒绝没有原 generic shape。真实 Community expiry 后可见 Intent/Joined signal仍存在；合法 LOCAL 双城同类别+同 areaLabel，目标 CityID 缺失、跨城/冲突 Context/过期 City仍产生供给。13 PASS/13 FAIL events 包含父包；初稿 idle final fixture因首次合法刷新未达到短期 deadline，单列为屏障失败。
- `native-idle-red2`：在首次合法刷新和 JSON materialization之后，先把自有 Session idle deadline明确设短期，再真实 Session UPDATE 等待，DB clock已过 idle而绝对期限仍有效，旧Authenticate复活 idle并200 payload。13 PASS/14 FAIL events；初始 Account idle屏障未达到deadline，不把该场景称权限RED。
- `native-current-green4`：上述真实源/时钟修复后25 PASS/0 FAIL-SKIP（PG14/HTTP11），vet/build0、全部461 Go/owned8/schema/完整public行稳定，owned DB DROP。包含新的严格初始 Account绝对/idle期限等待及最终 Session absolute/idle行锁等待。随后补原受众兼容/锁序核验，因此25帧保留为历史。

首次 current authentication与最终 non-refresh validation只复用实际Session/account原生方法，不追加Organization/Agent/模型purpose条件。Offline opportunity transport companion保留旧wire测试，并明确不是原生权限证明。全局Tie姓名保持旧055独立字段语义；未获准不可经handle回退。

`native-owner-red3`：新增测试假设“本人可通过visible读取自己的FRIENDS意图”，真实24 PASS/3 FAIL events，全部owned稳定/public相等/DROP。随后实际读取037及原GetVisibleSocialIntent证实原受众规则也不提供self-Tie；这是测试背景假设错误，不能当产品权限RED。本人管理实际使用ListOwnSocialIntents独立路径。`native-current-green5`仍25 PASS/3 FAIL events保留，失败为该测试错误预期；真实Profile writer/validator并发锁序子测试已PASS。最终将测试改为旧/new visible一致拒绝、原ListOwn仍可管理本人意图，并撤回多余self豁免，未扩原visible私密ACL。

最终validator独立Account→Session显式两语句，不复用Org final JOIN的planner锁序假设；使用真实UpdateHumanProfile/自有Profile行锁屏障与当前ValidateHumanSocialResponse并发：观察writer持Account并等Profile，validator只等该Account，释放后二者正常完成且deadlocks不增。首次AuthenticateHumanSocial仍复用现有严格Account→Session→fresh clock认证primitive。

最终Tie payload保留原LEFT JOIN user_profiles，不要求caller有Agent或peer存在Profile；获准字段原name/handle/generic形状不变，拒绝仍generic，055 PRIVATE负例同时保留真实known-handle canary。

## 最终固定源码本地验证

- 两个独立fresh库 `native-final-green6` / `native-final-green7`（001–068）：每轮28 PASS、0 FAIL、0 SKIP；Postgres17、HTTP11，包含父测试但不含package PASS计数。两轮 test/vet/build均exit0。
- 每轮实际全461 Go hash/owned8 SHA前后同帧、迁移源hash相同、完整public所有表行前后相等、owned数据库DROP成功；严格保留原cleanup日志。
- 原生正负覆盖accepted exact request、Block、noAgent、noProfile仍Tie、原owner/visible受众一致、真实055 PUBLIC/PRIVATE knownhandle、expiredCommunity、两LOCAL城市/冲突Context/expiredCity、真实source等待撤Session/Intent/Tie、初始Account等待与最终Session等待absolute/idle expiry、实际HumanProfile写与validator锁序/两条完成/deadlocks不增。
- 明确不声称Account native历史generation或用户行为概率；实际当前ID/digest/typed Person/Account状态/Session绑定与期限，旧digest不能借客户端actor自证。源payload statement是当前来源线性化点，最终Session检查不重授全部source永久有效。
- root管理 fullGo default三轮、独立scope及同EEA7 APK手机；worker没有运行全仓Go/Gradle/race/外部IdP或生产部署。根此前175Client源完整Flutter analyze0/test278/Debugbuild0由根证据记录，client-final1目标13只作widget/transport合同，不能替手机。

可复现：仓库根运行 `powershell -NoProfile -File work/v4-now003/verify.ps1 -Round <fresh-unique-label>`，要求本地Docker数据库可用；runner只创建已验证prefix随机owned库，001–068、3devseeds、真实registered HTTP/Store tests、vet/build、完整public与SHA、最后DROP。Go命令为 `go test -json -count=1 ./internal/postgres ./internal/httpapi -run TestSocialNow`，包位于apps/api；该命令若无BIRDTIE_DATABASE_URL/BIRDTIE_DISPOSABLE_DB会SKIP，所以最终证明只接受runner 0skip。

正式worker-final1保存所有初始/历史raw日志与hash/public、历史owned源码快照、两轮完整461Go快照和当前7Dart来源；manifest给文件逐SHA。root future receipts另目录，不重写原manifest。推荐原bounded明确来源功能按CODE_LOCAL核证，真实Pilot/认知/模型/Memory许可仍由原边界约束，root唯一决定queue状态。
