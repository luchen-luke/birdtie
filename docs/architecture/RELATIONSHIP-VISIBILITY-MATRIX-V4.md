# 真人资料与 Intent 五档可见范围

2026-10-10，原任务 `BT-V4-SAF-002`。这是普通真人资源的服务端权限合同；本地测试证据位于 `../testing/evidence/unfinished-completion-2026-10-10/visibility/`。上线、真机与整体消费者验收由各自证据决定。

## 五档含义

| 受众 | 普通真人 Profile | 真人 Social Intent |
| --- | --- | --- |
| public / PUBLIC | 匿名可读，仍检查活跃状态、Block 和原字段规则 | 沿用公开 Profile、ACTIVE、有效期和 Block 限制 |
| followers / FOLLOWERS | 阅读者当前单向关注所有者 | 当前关注创建者 |
| connections / CONNECTIONS | 当前有效好友 Tie，且对应当前 accepted friend request | 同一真实好友条件；旧 FRIENDS 继续有效并加强为相同条件 |
| close / CLOSE | 所有者明确选定的当前好友，方向不能颠倒 | 创建者明确选定的当前好友 |
| private / PRIVATE | 本人，或原有效的精确 profile_view grant | 原外部详情继续不可读，本人通过原本人接口管理 |

Profile 本人读取和原精确 `profile_view` 授权保留。整体 Profile 权限与已有 Agent 字段可见性同时满足，整体可读不等于每个字段可读。组织、商家 Profile 保持 public/private，不继承真人关系受众。公开地图、公开搜索和公开发现仍要求 public；不能把受限资料分享到公开面板。

Intent 的受众独立判定：关系受众可能允许读取私密 Profile 创建者的 Intent，却不授予其 Profile 字段。创建者必须是当前活跃 Person。旧 COMMUNITY、LOCAL、INVITE_ONLY 及原私密草稿、预览、确认与发布流程保留，受众改变走原 EDIT 流程。

## Close 是本人明确选择

118 迁移新增方向性 `person_close_connections`，绑定 owner/person、确切 Tie 和当前 accepted request。重建好友时 Tie ID 可以复用，request ID 会变化，因此旧选择必须退休，不能复活。Block、好友失效、请求变更、账号停用或主体类型改变都会写持久退休标记；解除屏蔽、恢复账号或重新加好友不恢复 Close。独立删除退休记录被拒绝；父实体删除仍可执行原外键级联。

接口仅供本人人类会话：

- `GET /v1/me/close-connections`：当前真实好友列表，最多100项，明确 truncated。无选择时 revision 为0。
- `PUT /v1/me/close-connections/{personId}`：要求当前 Tie、request、sourceVersion、validUntil 和 confirmed=true。
- `DELETE /v1/me/close-connections/{personId}`：要求当前 Tie、request、expectedRevision 和 confirmed=true；旧 revision 不删除新选择。

列表不是永久授权凭据。原生服务端给出不超过30秒及当前会话、相关授权期限的有效窗口，响应证明绑定原 Store、主体、会话 generation、来源和截止时间。响应编码后再次核验；来源撤回、会话换代或到期时拒绝返回旧正文。写入后的未知响应不能证明没有写入。客户端只 GET 核对，不自动重复 PUT/DELETE；新的人工确认才可提交新的当前操作。

资料读取采用相同的原生响应边界：SQL 先过滤字段，编码后再次检查原来源和原截止时间；grant、Block、Follow、Friend、Close 和会话的 A→B→A 变化使旧证明失效。已经发送到网络的数据不能追溯撤回，也不宣称数据库检查能做到这一点。

## 数据库和迁移

119 统一 Profile/Intent 受众谓词，保持原字段 ACL 与旧 grant。Profile 受众修改先锁定所有者类型，所有者类型改变也检查 Profile，防止并发生成组织 followers/close 资料。关系事实来自数据库，不接受模型文本或客户端布尔值。

119 down 遇到新档位数据会拒绝；空数据可恢复原037/047函数定义并保留118选择。118 down 遇到119依赖或已使用/退休的 Close 记录会拒绝。迁移只在新建、精确命名和记录 OID/owner 的本地隔离库验证；本轮没有迁移手机或共享库。

当前 Close Store 使用关系表级 `SHARE ROW EXCLUSIVE` 锁，读取也参与这一保守同步。它保证本轮撤权边界，但吞吐量、延迟和消费者规模性能尚未验收；不得用局部正确性测试推断可扩展性。

## Agent 边界

这些受众是普通真人 Profile/Intent 的权限，不新增 Agent Context 作用域、Memory、私密聊天、模型读取或 A2A 授权。Agent 对 CONNECTION/CLOSE 资料仍需要原用途和具体资源的独立授权；当前未实现的读取入口仍默认拒绝。旧 AGENT_ONLY、PUBLIC、COMMUNITIES、CONNECTIONS 等字段策略枚举保留。

客户端继续使用已确认的字体、语义颜色、TextScaler 和至少48逻辑像素的主要操作区域。原 Now 单地图、唯一输入框、键盘管理和面板边界保持原合同。组件截图是实际生产 Flutter widget 在测试传输和字体下的证据，不能代替安装构建或真机验收。
