# BirdTie Now 2026 年 10 月 8 日整合执行包

本包汇总当天四组详细反馈及最终合并请求，共 30 项待核对需求。它包含需求、修改设计、逐项验收、依赖和可复制给 Codex 的执行指令。

## 使用方式

1. 下载并解压整个 ZIP，将文件放到当前 BirdTie 仓库的 docs/requirements/2026-10-08-now/ 或当前项目约定的文档目录。此处是建议位置；本包生成不代表已经写入 Windows 项目。
2. 在正在开发 BirdTie 的原 Codex 会话中提供文件路径，并粘贴 CODEX-MASTER-PROMPT.md 的内容。不要开第二个执行器同时修改同一工作树。
3. 要求在自然检查点接入。Codex 先核对现有 42 项 UI Repair、22 项 Discovery 等任务，合并差异后执行；不能新建重复队列或重置已完成记录。

## 文件

- BirdTie-Now-Consolidated-Requirements-and-Changes-2026-10-08.md：完整规范和 30 项需求
- BT-NOW-20261008-BACKLOG.json：带依赖、来源、验收和状态的机器可读清单
- BT-NOW-20261008-BACKLOG.csv：同一清单的 UTF-8 BOM 版本
- CODEX-MASTER-PROMPT.md：直接交给 Codex 的执行指令
- VALIDATION.json：跨文件 ID、依赖、状态和内容完整性检查

所有需求初始 RECONCILE_REQUIRED、测试 NOT_RUN；实际执行后由 Codex 依据证据更新。该包没有修改仓库、调用付费服务或执行部署。PDF、Word 不是运行本包的前提；Markdown 可直接进入工程规范。
