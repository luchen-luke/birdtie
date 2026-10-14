# BirdTie 统一开发交付包

把整个文件夹放入当前 BirdTie 项目的 docs/handoffs/2026-10-08-unified-now/，在正在开发的同一个 Codex 会话中粘贴 CODEX-EXECUTION-PROMPT.md 的内容。历史项目位置为 D:\Project\birdtie，先确认实际路径。本包没有自动写入本机或发给 Codex。

推荐读取顺序：
1. CODEX-EXECUTION-PROMPT.md：直接交给 Codex 的执行指令
2. BirdTie-Codex-Unified-Development-Plan.md：完整产品、交互、状态、数据权限与阶段方案
3. REQUIREMENTS-TRACEABILITY.json：原Now30与PG26共56个ID的映射输入，不是56个新任务
4. ACCEPTANCE-SCENARIOS.md：33个跨模块验收场景，全部待当前实现运行
5. design/BirdTie-Now-Prototype-Overview.png 与 screen-01.png 至 screen-10.png：离线高保真参考
6. sources/：三份已交付原文，便于核对范围和历史，不作为最新完成状态

在线可编辑原型：https://www.figma.com/design/kHXRfYNZaSrCp6MjW9tQVb?node-id=4-2

本轮优先完成站内真实地图动态浏览，不等待SearchPro许可、完整语音或所有Agent能力。原型中的示意地图/人物/数量不能作为生产数据；十屏是同一个Now地图的状态。先交付一个真实可用链路并核对截图，再扩展。

原包ID、DONE历史和正在执行任务保留。18:47的用户转述进展须核验，不能批量标已完成或重置为未做。新细则附在既有需求下；本包只重整执行顺序和补交互契约。

VALIDATION-REPORT.md只证明此交付包结构，不证明BirdTie测试通过。SHA256SUMS.json用于核对本包文件字节。Sources内历史优先级和依赖如与最新主方案明确拆分冲突，以主方案第14节为准，其余范围和验收保留。
