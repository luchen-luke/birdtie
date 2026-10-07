# Local Pulse 与 Active Intent 验收

日期：2026-10-01（Asia/Shanghai）  
任务：`BT-NOW-002`

- Local Pulse 使用独立的 `NowPulse` 范围快照，只在空闲、无选中且镜头不移动时出现在左上控制区下方。默认展示“当前区域”总数和最多两个实际存在的分类，共不超过三个紧凑事实；拖图时收起，稳定后需显式搜索新范围。
- Active Intent 使用独立 `ActiveIntentSummary`，从任务操作、活动类别和时间筛选生成简短中文标题。原始完整提问保留在对话中；等待状态不把长句放回收起栏。
- Flutter 静态检查无问题，63 项测试通过。新增测试覆盖结构化意图、未知请求不显示长句、Pulse 分类上限；既有 Shell 测试覆盖选中、移动和区域层显隐。
- 最新 Debug APK 已安装到真机 `c641566b`。首屏 [Local Pulse 截图](evidence/2026-10-01-local-pulse.png) 显示“这片区域有什么”和两项真实计数；输入 `badminton this weekend` 后，[Active Intent 截图](evidence/2026-10-01-active-intent.png) 显示“羽毛球活动 · 周末”，原始英文输入未占据标题。

当前数据仍为本地测试活动；计数真实来自本地 API/数据库，不代表 Aberdeen 试点已经有真实供给。
