# BT-V4-OPP-004 可解释机会消费审计

2026-10-02，审计时 IN_PROGRESS，随后已完成实现与实际验收。OPP-003 完成后立即由 live next/start 领取；一次一项，无两小时等待。保留原未提交内容。

## 实际基线

Go `opportunity/engine.go` 已生成八种静态中文事实理由与代码，Store 使用本人有效找活动来源和 Activity 既有可见性/Block；但 Flutter 没有消费 /v1/me/opportunities，已有 Agent 结果没有每条机会说明，新朋友页直接使用 API reasons。机会引用没有 title/placeName，不适合把 UUID 作为消费者标题。来源 PRIVATE 找活动草稿可以从已有 Now 真实 FIND_ACTIVITY 任务保存，但现草稿页没有激活 UI。

TIE_ORGANIZER 的真实意义是好友主办，无兴趣/报名模型；现 Store 活跃 Tie 查询缺双方活跃账户和已接受 friend 的额外核验。组织原因可依据 activity_organizers 的明确 typed ORGANIZATION（不是 HostLabel），不能推断 CSSA 身份核验或混为 Business。基础合同应保留，只增加最小当前授权名称及允许列表中文投影。

## 本项计划

合并既有 OPPORTUNITY-ENGINE-V1 canonical；补 title/placeName 与明确组织主办理由、当前好友依据及 owner-only/no-store 请求边界。实现独立中文消费和既有 PRIVATE 来源预览确认激活，不公开私人意图、不注入地图、不自动 RSVP/邀请/聊天。详情复用服务端授权读取；unknown code/未经确认/错误/注销/身份切换/迟到响应安全处理。新朋友使用隔离的 v1 code 投影。

随后合同/SQL/HTTP/UI、迁移后全 Go/Flutter 回归、Debug build 与本地真机证据，实际通过才 DONE。这一段记录实施前计划；后续结果如下。V5 AGE 保持 QUEUED；Closed Pilot NO。

## 完成结果

当前授权名称与组织理由、严格本人接口、完整好友依据、中文来源隔离投影、Now/设置入口、PRIVATE 草稿预览确认及授权详情均已实现。001–052/三个 seed/全 Go SQL+HTTP/旧 ID/down-reapply、Go vet/build、Flutter analyze/189 tests/APK 均 PASS。Android 16 实际 Now 保存私人来源、预览返回不写入、确认后 3 条授权活动/理由、详情、取消及 App/API 重启持久化 PASS；参与/申请/会话/Tie/Plans 保持基线，9 张截图、来源 UUID 与请求 ID 已保存。见 [可复现证据](../testing/evidence/opportunity-reasons-2026-10-02/README.md)。本地合成不作为正式身份、真实 CSSA 供给或部署条件，Closed Pilot 仍 NO。
