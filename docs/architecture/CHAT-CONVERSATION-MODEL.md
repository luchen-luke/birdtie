# Birdtie V4 一对一会话与已读位置

日期：2026-10-01。对应 `BT-V4-CHT-001`；一次性开发数据库验证，尚未部署。

现有 `conversations.id`、`conversation_messages.id`、`created_at` 和两位成员账户 ID 是稳定的会话与消息主键。会话由用户明确同意的 `scope=conversation` 联系申请创建；接受好友申请只创建 Tie，不自动开私信。活跃好友可由任一方主动点击“发消息”，`POST /v1/me/ties/{tieID}/conversation` 复用已有双方会话或创建一条新会话。当前只支持两位真人成员，消息 `speaker_kind=human`。会话不是 Agent 身份或私密记忆入口。

迁移 `035_conversation_read_state.sql` 为每位会话成员各建一条 `conversation_member_states`。`last_read_message_id` 必须引用同一会话的真实消息，`last_read_at` 仅在本人调用已读接口后写入。新会话由触发器创建两条成员状态；既有会话回填两条状态，以迁移时刻为未读计算基线。旧消息此前是否读过无从证明，因此既不写“已读”，也不显示为新未读。两个成员各自的游标独立，服务端只向本人返回自己的 `unreadCount` 和 `lastReadAt`，不提供对方已读回执。

`GET /v1/me/conversations` 返回本人可见会话及未读数；`GET /v1/me/conversations/{id}/messages` 返回最近一百条；客户端在实际显示消息后调用 `POST /v1/me/conversations/{id}/read`，传入 `throughMessageId`。服务端核验 Session、双方活跃、会话成员、Block 和消息所属会话，再向前推进游标。重复或较旧的标记不会倒退游标。Block 期间读取、发送、标记已读均不可用；显式取消 Block 后旧会话可再次使用，但 Tie 不恢复。

好友发起的会话在 Tie 移除后不可继续收发，重新建立 Tie 后可恢复原双方会话。旧 `scope=conversation` 一次性私信同意独立有效；移除好友不会撤销旧私信同意，Block 则在生效期间阻止所有私信读取/发送。服务端沿用原消息持久化与速率限制，失败时客户端显示错误且不虚构本地消息。

`automation/verify_conversation_read_migration.ps1` 对一次性数据库验证 001–035、历史保留/回填、HTTP 与 PostgreSQL 集成、游标权限和重连持久性，以及有真实游标时受保护的 down。生产迁移须单独备份、演练和审核。实体附件、群聊、推送和对方已读回执不在此任务范围。
