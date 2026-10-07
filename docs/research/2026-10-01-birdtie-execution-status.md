# Birdtie 执行状态报告

日期：2026-10-01。官方仓库：`D:\Project\birdtie`。

## 执行结论

任务队列共 39 项：**35 DONE、1 BLOCKED、3 TODO**。当前没有依赖已满足的待办。唯一阻塞项是 `BT-REL-001` 封闭试点发布门槛；CSSA 资料与单活动试点、试点复盘和 P2 视觉收尾均依赖该门槛，不能提前宣称完成。

## 已落地并验证

- [Agent 身份与归属规范](../architecture/AGENT-IDENTITY-AND-OWNERSHIP-MODEL.md)明确 Person/Personal Agent、Organization/Organization Agent、平台管理的 CityContext；旧的 City Agent 身份和 Organization Account 设计已被取代。
- [中文优先规则](../ux/GLOBAL-UX-INTERACTION-CONTRACT.md)与 [Now 规范](../product/NOW-AGENT-MAP-WORKSPACE.md)已经成为默认交互约束。真机 Now 首屏、查询、状态文案以简体中文为主，保留原始实体名称与用户输入。
- Now 地图、聚合成员选择、轻量单点卡片、区域信息、显式“搜索此区域”、Agent 回复与结果面板、上下文追问及旧响应防覆盖已完成本地验证。单项证据见 [地图与键盘](2026-10-01-map-keyboard-lifecycle-audit.md)、[Pin 选择](2026-10-01-map-marker-selection-verification.md)、[区域搜索](2026-10-01-search-this-area-verification.md)、[过期响应](2026-10-01-stale-response-verification.md)、[区域信息](2026-10-01-local-pulse-active-intent-verification.md)、[面板分层](2026-10-01-entity-peek-results-sheet-verification.md)。
- [公开地图隐私检查](2026-10-01-map-privacy-audit.md)确认 People 标记仅用用户主动公开的粗略区域；没有将设备精确位置公开给 Now。
- [性能记录](2026-10-01-now-performance-profile.md)包含 Flutter profile 真机帧数据、镜头热路径优化、更新后 debug 真机复验以及 profile APK 更新被手机拒绝的量化限制。
- Android 设备 `c641566b` 已安装并打开最新版 debug 预览包 `app.civu.civu_mobile.birdtiepreview`；调试服务和 DevTools 已附加。API 本地端口 3694 通过 ADB reverse 连接。地图所用本地令牌配置已持久化于被忽略的本地文件；用户新提供的公开令牌读取矢量瓦片返回 403，生产使用仍待处理。

## 最终工程检查

| 检查 | 结果 |
| --- | --- |
| Flutter `analyze` | 通过，0 问题 |
| Flutter `test` | 66 项通过 |
| Flutter Debug APK | 构建并覆盖安装成功 |
| Go `test ./...` | 通过 |
| Go `vet ./...` | 通过 |
| Go `build ./...` | 通过 |

## 尚未达到发布条件

[封闭试点发布门槛记录](2026-10-01-closed-pilot-release-gate.md)和[值守与回滚手册](../business/CLOSED-PILOT-OPERATIONS-RUNBOOK.md)已经列出验收步骤。本地公开的 3 条未来活动仍是测试数据，组织未核验；缺少 CSSA 确认的真实活动与组织资料、明确的 Birdtie/CSSA 值守人与备用渠道，以及生产 Mapbox/API 部署验收。需要这些外部条件到位后，用真实活动重新执行发布→发现→报名→重启→通知闭环，才可复审 `BT-REL-001`，再执行 `BT-PIL-001`、`BT-PIL-002` 和 `BT-POL-001`。

当前状态是**本地可验证的 MVP 预览**，不是已获准上线的 CSSA 封闭试点。未向 CSSA 或其他外部对象发送材料。
