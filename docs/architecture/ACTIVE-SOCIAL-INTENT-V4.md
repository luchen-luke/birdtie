# Active Social Intent — V4

状态：BT-V4-NOW-002 已实现，并通过本范围原生与客户端定向验收；根代理整仓、构建及新真机核证另记。此文是原 SocialIntent 生命周期的增量合同，不创建另一意图账本。

## 目标与入口

普通用户以当前个人账号，查看已有真实意图、修改条件、检查具体版本、开启或取消同一意图。Now 原生卡片与直接 `ActiveSocialIntentPage` 复用同一 controller/API。地图入口由 MAP001 唯一 writer 集成；卡片自身不改变地图、对话或 AgentTask。组织工作区不允许代本人批准。

适用 GLOBAL-UX-INTERACTION-CONTRACT 的 UX-CHECK-01–16：简体中文、现有领域 ID、预填实际资料、可选字段不猜事实、明确草稿/批准/提交/权威结果、当前身份与迟到响应、返回取消、移动滚动和文本放大；辅助技术、真机证据按实际执行另记，不以规则引用宣称全部通过。

## 唯一真源与兼容

复用 `social_intents`（036）、原 audience targets/invitations（037）及 source_agent_task_id（038）。新增原生 Gateway 无新表/迁移，不重建 Intent。edit/cancel/activate 全部保留 ID、creator、created_at、原 Task 引用。旧 constraints 增加可选 RFC3339 startsAt/endsAt（必须成对，结束晚于开始，最多90天窗口）；expiresAt 始终是寻找截止，不能当计划活动时间。

既有 unversioned/confirmed boolean HTTP 路线仍是旧兼容合同；它们不会因为此 Gateway 存在获得新的具体版本批准。新增 approve 严格拒 `confirmed:true`，仅接受进程密封 previewId。本轮不声称所有旧表单/批准路径均已升级。

## 当前原生来源

原 user/active personal Agent/agent_profiles/current Session 是必要身份；捕获原 Session 的 id/created/auth method/absolute expiry/token digest 的摘要，正常 Authenticate idle 延长不伪造新身份，实际有效期限在锁后用 PG clock 再核。原 Profile visibility 仅约束 PUBLIC/LOCAL 开启，不能授认知或模型许可。

所选 Place 必须当前 published、未到期，底层 City 也必须当前 published/未到期；LOCAL 与已有 CITY context 的城市匹配。无 Place 可用本人明确粗区域，不推断位置。COMMUNITY 须真实当前 active membership/current Community。INVITE_ONLY 检查当前 active person、非本人、无任一方向 block；UI 从真实好友选择，不输入 UUID。实际 source/xmin、targets/invitations xmin、原 row xmin 参与版本；A→B→A 也使旧批准失效。selector 每类最多100项，读101显示 truncated，选择不是授权。

## 审批与真实效应

GET /v1/me/active-social-intents、GET /options、GET /{intentID}；POST /{intentID}/preview 输入 operation/expectedVersion/可选edit；POST /{intentID}/approve 仅 previewId。

- EDIT：展示受众、方式、地点/平台、可选计划时间、寻找截止与参与人数；保存同ID并退回 DRAFT，原 ACTIVE/MATCHED 停止发现。之后单独具体批准 ACTIVATE。
- ACTIVATE：只接受当前 DRAFT，并再核实际来源与公开资格；复用原机会 owner routing，不发送新邀请，不产生报名、活动、Memory 或模型动作。
- CANCEL：只取消当前 DRAFT/ACTIVE/MATCHED 原意图。已隐藏地点可在本人中性显示后取消；不复制隐藏外部名称。终态不重新开启。
- RESET：仅清本地选定意图/待审批版本，文案明确“不取消服务器意图、不清对话和任务”。

AEAD随机进程key、90秒以内短预览，绑定当前 owner/Agent/Session 摘要、原 row/source版本、具体 edit/operation 和期限。GET/preview 无领域写；HTTP Authenticate 正常闲置刷新仍是原身份合同，不把它描述为数据库完全零写。未知提交仅用原ID GET核对当前事实，状态相等不能证明本次已成功；不盲重发、不生成新ID、不自动续预览。重启旧key作废，有实际子进程测试。

## 事务边界

预先取实际读/写表关系锁，再 Account→Agent/metadata→原Intent；写用原ID FOR UPDATE NOWAIT，Session按现有顺序最终 FOR SHARE。Session真实等待之后复捕版本；SET CONSTRAINTS ALL IMMEDIATE、最后源读取之后，一条 RC SQL 同时核所有 selected sources/current身份/ACL/PGclock/截止与 block phantom。编辑/状态、原 target/invite 与 audit 同tx；失败回滚，重复双批准仅一次原效应。

版本保证是当前 guard 时刻的原生权威检查；不是对网络返回后、任意特权DDL/SQL恢复或未来源撤回的永久承诺。没有持久一次操作回执账本，不用现状相同替代那次提交证明。

## 页面生命周期与验收

API/Controller 绑定 auth/account/workspace/client/base/listener/getter；same-key transport 替换重建 transport，老controller/dialog永久退休。workspace/token A→B→A 不能继承旧预览；during-build 同步废除操作能力， owned route 移除延后避免 Navigator 祖先修改。borrowed client 不关闭，owned API 只关闭一次。未知失败、动态期限和dispose迟到均不得审批。

本范围 native9：64PASS/0FAIL-SKIP，target test/vet/build=0，复制820源稳定；原001–082隔离库、完整旧public/catalog/xmin与unused082 down/reapply保留、ownedDB DROP。native registered HTTP 多字段 EDIT/CANCEL 七帧在 native-wire1.json；客户端实际解析 JSONB键排序、Z与+08:00、同ID receipt。client-target3：15功能+4loading全部通过，8源 analyze=0。root统一全083/Flutter/build/真机另记录。TalkBack、生产 IdP/地图/试点部署、真正合作方资料均不由这些本地合成证据替代，Closed Pilot 与 ConsumerBeta 保持 NO。
## UX 核验定位补充（freeze2）

| 检查项 | 本范围证据与边界 |
| --- | --- |
| 01/02/03 | 当前本人管理已有意图，复用旧值；显式完整编辑器为例外，不把可选时间当必填，不从字段反推搜索事实 |
| 04/05 | 原同ID/native Gateway＋Now唯一MAP入口；真实字段/后果断言已跑，根新截图待核 |
| 06/07/08/09 | SourceAvailable/到期/中性隐藏名/草稿与未知独立；native真实期限ABA、双批准，unknown只读原ID |
| 10 | directPage/Card 同key运输端、真实parentbuild workspace A→B→A永久退休，跨owner/组织native拒 |
| 11 | 普通页面仅本地原生domain操作，不调用模型，不依赖定位；线上单独约束 |
| 12 | 取消/系统返回零approve、实际OS child新key拒旧批准；持久重启真机结果由根独立记录 |
| 13 | REUSE现Material/字体/按钮、原social_intents；EXTEND原constraints两时间字段；NEW窄native批准Gateway和本人卡/页 |
| 14 | freeze2真实WidgetTester.view=320×640、DPR1、font2、keyboardInset220；输入可取消，取消按钮bottom≤640；TalkBack未运行，目标下未发现布局异常不代表全部辅助技术通过 |
| 15 | 定向脚本不是未经讲解用户可用性观察；根本地真机任务观察另记 |
| 16 | 不新增分析埋点/聊天位置正文；audit仅actor/op/resource/decision/purpose，sealed process preview非持久正文账本 |

freeze1/target3仅font2＋逻辑MediaQuery，不能称真实320物理布局证据。client-320-red1实际失败是lazyListView尚未滚动导致finder NoElement；精确test helper滚动修正后client-320-target2和真实键盘3绿，无产品布局修改。最新client-target4 15功能＋4loading/0failSkip、8源client-analyze5=0，freeze217source与wireSHA当前。未覆盖辅助技术/无指导消费者观察/生产环境保持待核，不能由全部引用替代证据。
