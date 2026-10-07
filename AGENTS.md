# Birdtie repository guidance

## Project identity and source of truth

- Birdtie is an **Agent-native local social network**.
- Birdtie is the next product version of Civu. It is rebuilt around Birdtie's own model in this repository, while existing users, data, interfaces and live services are migrated in stages. The existing store app identities are retained for the eventual update.
- `D:\Project\birdtie` is the only official Birdtie repository.
- `D:\Program\Civu` is a read-only reference source, not a working directory for Birdtie changes.
- `D:\BirdTie` is a legacy ChatGPT/Work folder. Preserve its contents; copy only explicitly selected materials and never delete or move files from it.

## Civu reuse boundary

- Before proposing or implementing any Civu reuse, review `docs/architecture/CIVU-REUSE-MATRIX.md` and update it when findings change.
- Never copy the entire Civu repository into Birdtie.
- Treat code reuse as a separate, explicit migration. Check dependencies, licenses, data ownership, privacy/security assumptions, external services, and architectural fit first.
- Keep Birdtie decisions and implementations native to Birdtie's Agent-native local-social-network goals.

## Documentation locations

Place documents in the matching directory under `docs/`: `product/`, `business/`, `architecture/`, `research/`, `decisions/`, or `migration/`. Record material copied from an external/legacy folder with its original source path and destination.


## Birdtie 持续交互与 UIUX 规则

- 用户可见功能的设计、实现与验收先读 [权威交互规则](docs/ux/GLOBAL-UX-INTERACTION-CONTRACT.md)，引用适用 UX-CHECK-01 至 16；日期版材料只作来源快照。
- Now 为主要意图入口；结构化卡片、稳定详情、Plans、消息、组织工作台和设置保留直接路径。界面以简体中文为主。
- 先整理获准且仍有效的上下文，再让用户选择、检查和确认；仅询问必要缺项，默认每轮 1–2 问，不猜事实或隐藏关键后果。
- 草稿、具体版本批准、授权提交与权威结果分别表达；复用现有领域动作、组件和 AGE/AIR 权限边界，账号/组织切换、撤权与迟到响应不得复用旧批准。
- 提供适用的正常、异常、权限、移动端、辅助技术和真实 Birdtie 截图证据；未运行如实记录，规则接入不等于界面整改或发布验收完成。
- 在自然安全检查点增量接入，保留已有队列、状态和证据；按实际接口依赖安排 AGE/AIR，当前任务和原发布门槛继续有效，不另建重复 UIUX 功能 backlog。

- V5接续使用 [增量执行协议](automation/CODEX_V5_EXECUTION_PROTOCOL.md) 与 [137来源映射](automation/v5_requirement_mapping.json)，唯一live状态仍在原队列；完成证据和状态后立即领取下一项，不等待定时触发。

## 受控并行任务执行

- 2026-10-02用户明确授权独立需求并行推进。默认串行命令保持；根代理可用`taskctl.py parallel-config --coordinator root --limit 3`显式启用，最多3个IN_PROGRESS（根代理与两个实施worker）。配置与lease存于队列根`parallel_execution`，不批量改写旧任务对象或删除历史。
- 启用前审计真实接口依赖和写入文件；用`lease <当前任务> --coordinator root --owner root --write-scope <精确路径>`登记已有任务，再用`next --parallel`只读列候选。只领取依赖全部DONE、非LIVE、无外部/activation gate、已知本地completion及release分类的TODO；BLOCKED/PARTIAL不能自动解除。
- 每项通过`start <ID> --parallel --coordinator root --owner <唯一agent> --write-scope <路径>`明确负责人和所有仓库写入范围，可重复scope参数。范围大小写不敏感、斜杠统一、按路径组件判断；相同/父子包含、重复owner、空/仓库根、未知顶层、穿越、仓库外、通配符均拒绝。优先精确文件或独占新包，不因整个目录scope阻塞不相关文件。
- **只有根代理修改live队列及共用总报告**；worker只在获分配范围实施、测试、提交证据，不自行领取/save队列或占用queue写入范围。启用后所有队列变更必须带已声明`--coordinator`；这是流程约定，不是OS身份或业务授权。持久文件锁及读取hash比较防止协作误覆盖；失败重新读取，不能强制覆盖。
- 完成仍经根代理核验证据再`done`；`done/block/partial`释放对应lease并追加历史，其余任务/归档保留。未登记范围、存在共享真源/DDL/发布门槛冲突时保持串行，先协调后领取；并行不降低中文、隐私、真实证据或Closed Pilot门槛。

## 2026-10-06 用户指定的验证与离线接续规则

- 用户要求加快逐项修复，开发阶段默认只运行当前需求直接相关的单元测试（含必要的 Flutter widget 单元测试）。不在每个小项后重复整个 Flutter/Go 测试集、全量分析、编译或数据库整检；后续全面验证单独安排在必要的整合或发布检查点。
- 手机会长时间离线。继续仓库内可独立完成的修复与需求，不等待手机、用户回复或定时触发；真机、真实认证、性能与生产场景缺少证据时明确记录 NOT_RUN/BLOCKED。
- 保存每项实际执行的相关单元测试命令、目录、退出码和覆盖范围；未运行的集成、迁移、构建与真机验收不沿用旧结果，也不因单元通过而改称通过。既有任务依赖、权限边界和发布门槛继续有效。
- 本节是用户对日常验证频率的新指示，优先于旧协议中逐项执行全面检查的安排；保留旧任务、历史测试和失败证据，不删除测试来提速。

## 2026-10-07 用户暂停开发并交接

- 用户最新明确要求先结束全部开发任务，整理当前 Birdtie 开发状况交给 ChatGPT，再讨论新方向。连续执行目标已设为 PAUSED，两个实施 worker 已在自然安全点停止。
- 在用户明确恢复开发之前，不领取或实施下一任务，不启动新的测试、构建、迁移或真机验收。允许只读核查和本次交接材料、状态与已有证据整理；旧自动续跑提示不作为恢复授权。
- 保存全部未提交代码、失败记录、历史 DONE、依赖和发布门槛。停止时未完成的工作如实保留 PARTIAL/TODO/BLOCKED，不把用户要求停止当作功能完成。新方向尚未提供，不自行重置、重排或改写原队列。
