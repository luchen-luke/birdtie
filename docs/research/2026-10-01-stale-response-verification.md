# 快速查询与旧响应验收

日期：2026-10-01（Asia/Shanghai）  
任务：`BT-PER-001`

- Agent 工作区以本地递增 turn 序号决定当前界面所有者；旧请求返回时不能覆盖新任务、结果、对话或选中状态。登录任务仍以 `taskId` 关联，恢复时校验任务身份。区域 Pulse 有独立请求序号；切回已经显示的范围也会使其他未完成请求失效。
- 请求处理中，Composer 保留发送按钮并同时显示小型加载圈，用户可以继续提交新问题。HTTP 旧请求使用 ignore 语义，不强行取消底层网络；边界已写入 [Agent API 契约](../architecture/AGENT-DATA-API-CONTRACTS.md)。
- 自动测试令 A、B、C 逆序完成，最终界面仍是 C；另测区域缓存 A→B 待返回→切回 A，以及请求处理中再次发送。`flutter analyze` 无问题，60 项测试通过。
- 最新 Debug APK 已安装于真机 `c641566b`。连续发送 `badminton`、`football`、`tennis` 后，对话按顺序保留三轮，最后显示 `tennis` 与其真实服务端回复。见[真机截图](evidence/2026-10-01-rapid-query-final.png)。

此项验收保护当前设备的活动界面；同一已保存任务的跨设备并发写入尚无专用版本冲突协议，不能声称跨设备最终持久化顺序已经解决。
