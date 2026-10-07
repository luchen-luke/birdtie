# V4 社群对话

每个 Community 最多一个持续对话。Community 继续没有账号或 Agent；公开社群发现独立于聊天，不要求读消息才能发现社群。加入 Community 不自动加入对话，不产生好友，也不使活动报名与社群成员混同。

活跃 Person 拥有当前 `active` CommunityMembership 才可明确加入、读取和发送；pending/invited/left/rejected、匿名、停用真人均拒绝。加入前解释：名称与消息对其他已加入的有效成员可见，当前 owner/admin 可移除消息。每次操作重新检查成员与角色；成员退出/撤销后对话成员转 left，重新获得成员资格仍需明确加入。退出交流不改变 CommunityMembership。社群归档/不再发布后不提供聊天读取或写入，数据保留，公开发现同时按既有生命周期规则执行。

成员本人可撤回，当前 owner/admin 可管理消息；撤回清空正文并记录审计，普通成员不能移除他人消息。双向 Person Block 过滤对应发言，不把屏蔽一个人推导成屏蔽整个社群。当前已加入且有资格的成员才可举报非本人、未移除且未被 Block 隐藏的消息，目标为 `community_message`。运营处置仍需另行证据。

路径 `/v1/communities/{communityID}/conversation` GET/POST（confirmed=true）/DELETE；`/messages` GET 最近 100 条及 before 翻页/POST；`/messages/{messageID}` DELETE。消息使用客户端 UUID 幂等重试、2000 字上限，每名真人每分钟 60 条社群消息（跨社群计数）。早期客户端手动刷新，不宣称实时推送、已读或群消息通知已完成。050 是增量迁移并保护已有对话/举报的回退；不改 Activity/Community/Tie 旧 ID。
