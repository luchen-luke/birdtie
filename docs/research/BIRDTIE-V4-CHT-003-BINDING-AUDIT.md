# BT-V4-CHT-003 私信连接绑定增量审计

日期：2026-10-04；负责人：sponsored_trust；分类：CODE_LOCAL / MOCK_TRANSPORT。

## 范围与原状态

本轮恢复原 PARTIAL 的一个客户端安全切片。原任务及依赖以根代理保存的 `work/v5-age038-resume/original-cht003-binding-partial.json` 为准，live lease 明确包含两份产品文件和三份测试文件。仅修改 `connections.dart`、`chat_entity_router.dart`，新增 `human_conversation_binding_test.dart`；两份旧测试保持原字节。队列、共用 canonical、总报告、Go、数据库迁移及其他七类详情不在本轮写入范围。

遵循 AGENTS.md、V5 执行协议和 GLOBAL-UX-INTERACTION-CONTRACT，适用 UX-CHECK-04/05/06/07/08/09/10/12/13/14/16。中文退役提示提供可达返回动作；具体批准、未知结果及恢复操作键分别表达。

## 实际缺口与 RED

1. 同 key 的 HumanConversationRoute/Page 原更新检查漏掉 API base、借用 client、workspace getter、pending store 与会话对方。私信源保持旧连接，而子详情/恢复取新的 widget 参数；旧输入还可向旧主机 POST。`red2` 实际由两次读取增长到第三次请求，三个负例均失败。
2. ChatEntityDetail 原 client 在创建时捕获，dispose 却按当前 widget.client 判归属。借用 client 改为 null 后被错误关闭，实际 close 计数为 1；预期为 0。
3. 旧 activity GET 在同 key endpoint 替换后触发新连接投影；实际请求由 1 增到 3。不能因稳定 entity ID 相同而接受旧来源。
4. 最后移动端矩阵发现正常会话页在 320px、字体 3 倍、IME 260px 时固定 Column 溢出 108px，`green6` 保留真实失败。这不是历史真机错误的重新归因。

## 最小实现

Route/Page/详情在创建时捕获身份、监听器、endpoint、transport 归属。账号/工作区变化或同 key 参数替换立即永久退休；A→B→A 不恢复旧页面、正文、输入或批准。旧监听器精确移除，不把新的 getter/token 当旧操作授权。迟到 GET 不显示、不 markRead；迟到 POST 不显示成功、不重试或刷新新连接。

人类会话复用现有 NotificationDestinationBoundary 的嵌套 Navigator 和返回边界，分享审核、卡片详情与举报子路由属于同一退休子树。不会创建另一个授权模型。ConnectionSource 关闭幂等，关闭后在请求前拒绝；借用 client 不关闭，创建时自有 client 只关闭一次。

原分享 pending 的 environment/account/operationID 分区不删除、不迁移、不自动重发。原键的人工检查/具体重试在当前边界中工作；晚 201 或未知异常只保留待恢复引用，不能推断新身份发送成功。

退休正文用可滚动中文解释及至少 48dp 返回动作；正常页仅将原提示和输入区变为有界 Flexible + 可滚动内容，保留消息 ListView、字体、发送语义及领域操作，解决此次已复现的键盘溢出。

## 真实验证及失败链

命令由 `work/v4-cht003-binding/check.py` 执行，客户端目录 `D:/Project/birdtie/apps/client`。

- `red1`：实际 Flutter 三个 RED；结果解析器遇到 JSON list 失败，日志保留，不计成功。
- `red2`：0 功能 PASS、1 loading PASS、3 FAIL、0 SKIP；249 份 lib/test Dart 前后稳定。
- `green2`：两个测试 fixture 失败，POST 误用 200（实际契约为 201）、已 finalize 的 Mock 请求转发失败。修 fixture，未放松产品断言。
- `green3`：测试 fixture 编译失败；`green5`：缺 SemanticsAction import 导致 loading 失败。原日志保留。
- `green6`：53 功能及 4 loading PASS、1 功能 FAIL，实际键盘布局溢出。
- `green7`：`flutter test --machine test/human_conversation_binding_test.dart test/human_conversation_page_test.dart test/chat_entity_router_test.dart test/share_entity_to_chat_test.dart`，**54 功能 + 4 loading PASS，0 FAIL/SKIP，exit 0**。
- `analyze1`：`flutter analyze lib/src/workspace/connections.dart lib/src/workspace/chat_entity_router.dart test/human_conversation_page_test.dart test/chat_entity_router_test.dart test/human_conversation_binding_test.dart`，**No issues found，exit 0**。

最终矩阵包含正常发送、14 个 route/page 同 key 参数替换、账号/组织 ABA、迟到 GET/POST、原键未知分享恢复及晚响应、嵌套批准移除、自有/借用 transport 生命周期、七类既有详情/返回回归，以及窄屏字体/键盘/返回语义。HTTP 全为 MockClient；没有外部网络或产品测试数据写入。

## 冻结与完成边界

五文件 SHA 和 249 份观察源码清单见 `work/v4-cht003-binding/sourcefreeze1.json`。本轮证据只证明客户端本地切片。当前源码整包 analyze/test/build、最新 APK 安装和真机检查由根代理独立补充；不沿用旧 whole Flutter 920 或旧手机 125cd 作为新 CHT 通过证据。

TalkBack、真实人类未经提示消费验收、真实供给/身份、生产发布均未在本轮运行，完整 CHT-003 保持 PARTIAL；Closed Pilot / Consumer Beta 不能据此升级。

## 第二帧：正常布局真机缺陷与纠正

前段是第一帧交付时记录，已不可变归档，不能当成第二帧最终结果。根代理独立全量验证第一帧（948 功能 + 115 loading，analyze/test/Debug build 0）后，在 `work/v5-age038-resume/phone-current2/screens/chat-binding-conversation1.png` 实际发现：无键盘、默认字体的消息区约占父高度三分之一，输入区下方出现大量空白。已读取该真实截图；第一版 loose Flexible 虽然解决大字溢出，却没有把提示/输入区未使用的 flex 预算退给消息区。旧 APK 的正常分享证据不能替代最终新布局真机结果。

新增 800/820px 高、360px 宽、无 IME 的真实 widget 负例。`normal-layout-red1` 两项失败：消息视口实际 248/254.67px，分别小于 520.8/534.8px 的剩余空间验收下界。断言同时要求输入区贴近 body 安全底部、发送操作至少 48dp；未改为只验证不溢出。

第二版只在同一 leased 文件用 LayoutBuilder 确定有限高度：提示及错误区上限为父高 25%，输入区上限 50%，两者按实际内容占高并保留独立滚动；消息区的 Expanded 填满所有剩余空间，不预留 loose flex 空白。`green8` 正常视口/底部断言已通过，但发送按钮实际 40dp 小于 48dp，保留两项失败并加明确最小尺寸；不缩小字体或移除测试。

`green9` 同一四测试文件 **56 功能 + 4 loading PASS，0 FAIL/SKIP、exit 0**，原账号/组织 ABA、未知原键及 320px/font3/IME260 全部保持通过。`analyze2` 五项无问题、exit 0；两命令 249 份观察 Dart 源前后稳定。第二帧冻结 `sourcefreeze2.json`；`worker-final1`、`sourcefreeze1.json`、旧失败及旧 APK 留作历史，不覆盖。新整体 Flutter/build/手机结果仍由根代理独立补充。

原任务重新核对：goal 是发现对象发给好友，AC 为七类 stable-ID 结构卡，verify 是 entity-card contract + chat UI tests，completion_scope 为 CODE_AND_LOCAL_VERIFICATION，external_gate_ids 为空。最新实际 native-full-activity078-3 原七类 native 子测试、注册 HTTP/恢复/权限等待/幂等均 PASS；当前 UI 七类路由和绑定矩阵也已通过。第一帧“完整 PARTIAL”保守建议不意味着 TalkBack/真人消费或生产必须成为此代码子任务的新增 AC。最终状态由根代理根据第二帧当前真实产品结果及原 AC 决定；辅助技术/消费发布/生产未验仍明确保留，不能据代码 DONE 声称这些范围通过。
