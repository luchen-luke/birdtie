> 2026-10-04 检查点27：原CHT003代码及本地验证DONE；消费/AT/发布验收仍未齐。下文各历史帧按当时证据保留。

# Chat 结构化实体卡契约（BT-V4-CHT-003）

状态：2026-10-03 已恢复 IN_PROGRESS；仓库实现与本地定向、Flutter 全量通过，等待当前冻结全 Go 核证。生产、真机更新与辅助技术未验收。

## 数据与接口

- `044_chat_entity_cards.sql` 给原 `conversation_messages` 增加可空 `entity_type`、`entity_id`；旧纯文本消息和 ID 保持不变。允许 `activity`、`place`、`person`、`community`、`organization`、`business`、`moment` 七类，必须类型与 UUID 同时存在。含卡消息仍是双方许可的一对一人工消息，不创建 Agent 发言。
- `POST /v1/me/conversations/{id}/messages` 接受 `{"entity":{"type":"place","id":"<uuid>"}}`，可附本人输入的 `body`。省略正文时只保存通用文案“分享了一张卡片”，不缓存实体标题或私人资料。原 `{"body":"..."}` 保持兼容。
- 发送时先确认对话成员及原速率限制，再分别确认发送者和接收者当前可见。不存在或无权查看统一返回 404。私人/未发布 Moment、隐藏或过期 Place、非公开社群、未核验 Organization/Business 不能附加。
- `GET /v1/me/conversations/{id}/messages` 每次按读取者当前权限解析标题。撤回、隐藏、失效或改为私密后返回 `{type,available:false}`，不返回旧标题与实体 ID。原文本消息没有 `entity` 字段。发送者自行撰写的正文仍是聊天内容，不做语义遮盖。
- 客户端实体卡显示中文类别与当前标题；不可访问时显示“已不可查看”。`chat_entity_router.dart` 仅接受上述七类型及稳定 UUID，复用对应权威详情；未知类型、任意 URL 与不可用卡不路由。嵌套 Navigator 包含详情、子详情与具体批准对话框，原账号或组织工作区变化（包括 A→B→A）永久撤除旧子树。返回动作在已安装 ModalRoute 首帧后登记。

## 当前入口与缺口

Place、Activity、Person、Community、已核验 Organization/Business 及公共 Moment 详情复用同一分享流程。选择对象来自实际获准对话和已接受 FriendTie；显示具体收件人、个人账号摘要、当前目标与只分享公开引用的后果，明确确认后才建立好友对话和提交卡片。无获准关系显示中文空态，不代发联系申请。好友路径解析出最终会话后再次读取原操作，防止旧会话落在最新列表窗口之外时生成重复操作键。

`BT-V4-MOM-002` 已实现明确预览发布和 Place Memory。当前 `GET /v1/moments/{momentID}` 提供闭合九字段 `public-moment-v1`，只包含已确认公开正文、公开地点及当前版本/发布日期，不包含私人发生日期、作者私人资料、活动关联、Context 或 Memory。只读权限检查当前发布/确认、Person 作者、地点/城市公开与期限、双向屏蔽和提供的真实会话；无效 Bearer 不降匿名，独立于摘要发现窗口。Place 历史保留原 ID/revision，打开相同公共 Moment 详情。

## 原操作恢复与副作用

- 074 只在原 `conversation_messages` 添加可空 `client_operation_id`。原消息就是权威效果和回执，未建另一套消息或效果账本。原纯文本消息/ID/旧接口保持兼容；新 UI 使用 `POST /v1/me/conversations/{conversationID}/entity-shares` 与原键 `GET .../{operationID}`。
- JSON 严格拒绝重复/未知键、伪类型与不合法 ID。发送在原 sender/conversation 许可和速率限制下序列化，在等待结束后同一最终 SQL 验证有效 Session、双方状态、关系、屏蔽及双方实体权限，再 Commit。相同键和目标返回同一原消息，不再生成消息/通知；同键换目标 409。
- 读回执按原本人、原会话、原操作键定位，不依赖最新 100 条消息。撤回或隐藏后已提交事实仍可核实，实体只返回 `{type,available:false}`，不泄漏旧 ID/标题；新的分享仍拒绝隐藏来源。带键消息的键、目标和原内容不可改；非空 074 down 原子拒绝，不丢弃原恢复键。
- 客户端沿既有安全存储保存四个必要稳定引用，按原环境/Person 分区，不保存令牌、正文、标题或批准。提交前先保存，未知结果先 GET 核实；404 不证明原请求未提交，重试必须重新核实目标/明确具体收件人并沿用原键。找不到具体原收件人时只允许核实，损坏恢复记录阻止新发，不自动丢弃或执行。
- 账号/组织切换和迟到响应不使用新身份继续旧操作，不把未知结果当成功；原恢复引用保留到权威回执验证通过。此保证适用于新结构化分享，不宣称原纯文本接口已具有相同操作键幂等语义。

## 验证边界

2026-10-03 当前 `chat075-2`：33 测试 PASS，0 FAIL/SKIP，test/vet/build exit0、671源稳定、完整 public rows/catalog 未变、隔离库 DROP。覆盖七类原键、117条历史最新100窗外原键恢复、真实注册HTTP与撤回原事实、真实账号锁等待后的撤销/自然到期/关系撤销/屏蔽/地点隐藏零新消息。`schema074-1` 另验旧ID/全数据保留、非空down原子拒绝、带键消息不可改键/目标、空down全catalog恢复和reapply等值。当前全Go075回归单独核证，上述定向不能代替全量。

Flutter `whole-client1`：analyze/test/Debug APK build exit0，623功能及86loading PASS、零失败/跳过、199源稳定；APK SHA256 `35a632fe637447d5141fbedc4b45d4399f2e0f2d5a216337ffe1509525042b25`。覆盖七类路由/返回、具体收件人批准/取消、持久引用与未知原操作核实、原键重试、损坏缓存阻新发送、身份ABA/迟到、嵌套批准撤除、小屏200%字体/键盘/8条恢复滚动。ADB当前无设备，新APK未安装，新流程真机/实际手机App重启/新截图/TalkBack未运行。旧2026-10-02截图仅作为以下历史实现证据，不能替代新流程验收。适用 UX-CHECK-01 至16仍按正常/错误/权限/移动端/辅助技术分别记录，未运行范围不算通过。

一次性 PostgreSQL 001–044 与 Go API/DB 测试验证七类卡、私人 Moment/未核验主体拒绝、外人对话拒绝、资料私密化与撤回后的读取遮蔽；044 有卡片时禁止回滚。Android 16 真机连接隔离本地 API/DB，从地图 [Place 详情](../testing/evidence/chat-entity-2026-10-02/01-place-share.png)选择[虚构开发对话](../testing/evidence/chat-entity-2026-10-02/02-recipient.png)，服务端持久化 `place` 与稳定 UUID，[聊天显示中文卡片](../testing/evidence/chat-entity-2026-10-02/03-chat-card.png)。上述均为开发数据和 Debug 应用，不是正式身份或合作方试点证据。

## 2026-10-04 当前真机核验（本地合成，不是生产）

根已明确恢复原PARTIAL并保留原对象。Android16设备c641566b实际连接隔离076本地API/DB，原72 API和数据库未改。七类卡→同ID权威详情→原聊天返回实际截图对应e52 APK；初始7张由本地API创建，不能说UI发送7类全部通过。具体收件人批准/真实UI发送/未知提交恢复单独验证。

e52 APK：代理只丢真实201提交后的一个响应；原键55a85574-55f5-4440-92e1-ab6e2c6bda91，消息06f0eb76-8aa0-46c9-b6f8-8df7e90cfa27。强制停止并重启后安全存储原引用仍在，UI GET请求363e65705b9efe6d8a740ffd230f8776恢复相同消息，SQL该原键1条，当时总8条。另一次明确新批准正常分享成为9条；初次过期准备SQL语法失败，不能把其后正常分享当过期拒绝证据。正确准备后401且无第10条。原owner未知记录在另一账号聊天未出现。

实际发现旧成功提示与新未知结果并存：保留widget RED，具体批准新提交/核实开始清除旧result/error。client076-device3 analyze/test/Debug构建0、624功能+86loading/0FAIL-SKIP、199源冻结；第一次device2全局Gradle缓存失败保存，使用已有隔离缓存成功。新APK d1b7360ed8e171f3edd92c723513b59b89bd597cc9e7656ea2c588f2eaafc709已ADB install Success；实际UI新确认201请求9225afe9beb55cfb661897a8b67a0dac成为第10条。正确独占合成会话到期后再次明确批准得到401请求44fbbd50e0a5b4285250bc4ffdfe356e，数据库仍10条，截图确认只显示新待核实、没有旧成功。

字体200%可达有实际截图。TalkBack尝试被首用教程抢前台，不能计Birdtie读屏通过；音频听验NOT_RUN。原secure服务空字符串/accessibility_enabled0/font1.0精确恢复。没有安装输入法/清除安全存储。设备已重新通过原本地测试登录恢复有效本人会话，reverse3697→6623、force-stop再启动，并实际Flutter attach会话35429；DevTools地址来自真实输出，任务面板打开请求已queued。服务/调试留供已授权继续验收。

CHT003正常、未知提交、App重启、身份切换与本轮修复取得实际证据；辅助技术和未提示真人消费观察仍未验，完整任务不可仅凭安装标DONE。适用UX-CHECK-01–16按范围留证。开发手机号/固定测试码不证明手机号所有权；组织/商家verified仅隔离库合成状态夹具，无现实核验。Closed Pilot NO / Consumer Beta NO，模型和自动动作关闭。

### 检查点27：CHT003原结构卡验收完成；AIR015具体分析许可接续

唯一253项：{'DONE': 142, 'BLOCKED': 13, 'TODO': 86, 'PARTIAL': 10, 'IN_PROGRESS': 2}；P0：{'DONE': 92, 'BLOCKED': 9, 'PARTIAL': 8, 'TODO': 36, 'IN_PROGRESS': 2}。CHT003按原 `CODE_AND_LOCAL_VERIFICATION` 的七类稳定ID结构卡、原生领域权限与Chat UI验收标DONE，保留原PARTIAL对象与旧失败，释放lease后立即 `next --parallel`，原E2E001依赖满足，先审计实际测试人员/源资料及仪器检查。根038与AIR015继续独立推进，没有改变原依赖或原发布门槛。

最新Flutter冻结251源：**950功能 +115loading PASS，0FAIL/SKIP，analyze/test/DebugAPK build exit0**。原同key transport/借用client/迟到投影真实三RED保留；首个新APK948帧真机发现正常列表只有1/3且输入下巨大空白，未把安装当验收。追加两个几何RED，保留发送触点40dp的实际负例，修LayoutBuilder有界提示/输入自然高度，消息填满剩余、发送48dp；56目标/整包950及新cf0 APK实际截图确认正常贴底、IME和返回。

当前手机 `c641566b` Android16 / APK `cf0f010175e56751910f5bc644b5300568905c7800c0cf46e73fb656ddb122b2`，实际ADB install-r-t Success，保留App数据。owned本地DBschema078、API47120来自冻结Go9483，readyz与监听PID、升级前后原全部public行/xmin核证同值；未重复迁移。取消具体发送10条消息原全行hash不变，旧未知原键GET404明确“不等于未发送”不盲重发；旧APK与新最终APK分别实际明确预览并确认本地收件人，一共两条新原键消息，最终12条。新最终APK真实201请求 `09c306f39a6eb91c63b6aaf9004270fc`，Message `4aef1271-d0b1-40d9-89d5-1bb146516f9b`、operation `ad57d229-268f-48da-bd25-6a34c769bab5`、原Activity `ba40a427-293a-4f2e-95f0-a28ca0eedc91`；实际App强停/重启12rows+xminhash不变。原报名cancelled/PRIVATE v5与3审计全行hash保留。正常/大字/键盘与原权限负例分别保留真实范围，不声称手机UI七类发送全部运行或TalkBack/真人未提示观察通过。当前Flutter attach83213连接实际VM/DevTools2755，未Hot reload；面板打开请求的可见结果单独以工具回执为准。

Go **9483PASS/0FAIL-SKIP/test-vet-build0/750源稳定**、原public/catalog/down/reapply/ownedDROP属于冻结078帧，包含真实七类型 stable ID/native registeredHTTP/双方ACL/等待后撤销/并发原键去重/100窗外回执。现在AIR015079与五新路由在独立实施，**9483不覆盖079新增源**。AIR015直接依赖AIR014/INT001均DONE，formal graph无循环；未要求先完成依赖它的AIR016再恢复。原015第一切片复用唯一consent_grants，绑定具体Person/Session/Agent/ACTIVE Task/Moment字段和源版本，用于本地分析；079scope已登记，当前未验收。它不授候选持久保留、Memory、effect、模型出口，原064拒写/consumer Unavailable保留，原015故障矩阵+100次真实单一候选效果仍待后续。

47 worker归档逐bytes/SHA独立核证，manifestaa0edff8e98c0c4a6dffb5bf03a5e596bd2e84a2b1a9abe5721f288e67eacbd2；本检查点root-code-final9 431文件，manifest `e072001e0f8c0697f6f6e626865482a61f4c801d0d3944591edb32c3ac054ed6`。保留此次侧边栏系统返回退出App的两次foreground断言失败（未计PASS）；非当前分享域修改，回到确切Birdtie Activity后继续。

**Closed Pilot Ready：NO；Consumer Beta：NO。** 本地合成、固定开发认证、Debug和已安装Mapbox public token不能证明现实身份、组织授权/活动确认或生产地图许可。现实测试人员/授权供给、生产IdP、HTTPS生产API、实际调度/失败观测/日志访问、值守/备用联系及真实A→H未齐。模型provider出口地区/保留/计费等未批准；无外部服务变更、联系或发布。TalkBack/音频/真人消费验收/race仍NOT_RUN；不以单项CODE_LOCAL完成替代消费级或发布门槛。
