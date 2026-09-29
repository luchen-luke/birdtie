# ADR 0012: Owner-only Saved City objects

日期：2026-09-30  
状态：已采用，首批支持 Place、Activity、Group

## 决定

Saved 是登录用户对现有公开 City Graph 对象的私人书签，不复制目标内容，也不产生公开关系或参与动作。首批目标为 Place、Activity、Group；People、私人 Moment、Intent 与 Journey 尚不开放收藏。每条书签只引用一个权威目标，Owner 从 Session 推导；重复收藏保持同一条书签。

创建和读取都重新检查目标 City、发布状态、有效期，以及 Activity/Group 与当前用户之间的屏蔽关系。目标变为不可见时，Saved 列表只显示不可用占位，不返回旧标题、摘要或城市；Owner 仍可删除该书签。恢复可见后重新展示当前目标内容。撤销收藏不改变原对象。

客户端在有 API 且已登录时，从 Agent 结果收藏或取消收藏，并通过 Sidebar 的 Saved 管理。无 API 的本地演示实体不可收藏。Saved 不授予 Agent 私人资料读取权，也不隐含联系、报名或发布许可。该实现不迁移 Civu 的收藏代码或数据。
