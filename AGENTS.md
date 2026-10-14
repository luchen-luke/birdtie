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

## 2026-10-09 用户指定的交付对账与验证频率

- 用户要求明确核对所有已导入需求、实际改动、未完成项和 UI/UX 验收差距。历史队列 `DONE` 保留；汇报须分别说明代码实现、受影响流程验证、视觉交互验收和实际交付，不将局部测试或旧证据换算成消费产品完成率。
- 日常小项只执行直接相关单元测试（含必要 widget 测试），并验证所修改的流程。不默认重复全仓测试、全量分析、数据库整检、APK 构建、装包和完整录屏；整合与发布检查单独安排。此频率优先于旧协议和任务 `verify` 模板，保留原测试与失败记录。
- Dart/UI 调整在已有可运行调试会话支持时使用热重载；原生代码、依赖、编译期配置或需交付新包时再构建。多个关联修复合并一个构建检查点，不以每条清单为一次完整装包与取证单位。
- UI 仍须通过真实界面的布局和交互检查：批次检查正常、键盘、加载、空结果和错误等受影响状态，保存必要截图；只有手势或状态连续性需要时录制短流程。单元测试通过不能代替视觉验收，未验证的状态明确标记。
- 本次对账材料位于 `docs/reports/BIRDTIE-DELIVERY-AUDIT-2026-10-09.md`。整理报告不自动领取下一任务、改写原队列或恢复旧自动续跑；继续实施依据用户当前明确授权。

## 2026-10-09 Now P0 修复授权与取证

- 用户明确授权按 `BirdTie-Now-P0-Repair-Spec-2026-10-09.md` 实施本轮 Now 首页 P0；本轮授权覆盖旧暂停快照，保留其他工作区及已有未提交修改。
- 用户最新明确取消录屏，本轮只保存真实键盘开合和相关状态截图。规范内旧录屏条款不再执行；不得调用旧取证脚本的录屏分支。
- 按文件归属合并关联修复，直接相关单元/widget 测试通过后只做一次整合装包检查点；真实操作、静态/模拟测试与未验证状态分别报告。


## 2026-10-13 用户指定：编码与合批编译优先

- 用户明确要求继续实施剩余功能，重点是实际编码和产出编译包，避免每轮反复检测、截图及验收占用开发时间。
- 先完成一批关联功能接线，再统一执行改动所需的直接测试和编译；不重复已通过的全套真机流程、全仓测试、全量分析或数据库整检。新失败和实际风险所需的定位检查仍可执行。
- 本轮中英混合语音验收继续暂停，后续不录屏。缺少真实设备或运行证据的项目记录NOT_RUN，不能把代码或编译改称验收通过。
- 保留原任务队列、未提交代码、历史完成和失败证据；不为本轮重新建立任务列表。

## 2026-10-13 用户指定：后续按正常字号开发

- 用户明确要求“之后都不需要考虑大字号的情况”。后续 Birdtie 设计、实现与检查按正常系统字号进行，不再安排系统字号放大、1.3/2.0 倍文字或 200% 大字体的专项适配、测试、截图与验收。
- 不将大字号适配或验收列为后续待办、NOT_RUN 缺口、交付阻塞或发布门槛；此前文档、设计稿和验证模板中的对应要求由本条覆盖。既有历史测试与失败证据保留，不能将取消范围改称已经通过。
- 正常字号下的字体层级、长中文与英文换行、窄屏、键盘遮挡、可滚动内容、主要操作可达及其他适用要求继续执行。保留现有系统缩放实现；本次是取消后续大字号工作范围，不额外改代码强制锁定字号或删除历史测试。

## 2026-10-13 全站文案与媒体整改边界

- 用户明确授权按根目录 `Birdtie_UI_Copy_and_Media_Audit_2026-10-13.md` 核对18项及实际可达页面，以本机当前未提交工作树为准，已修复项验证后保留，不回滚到附件引用的旧提交。整改并入现有任务和证据，不建立替代队列。
- 第一批为Now首页、城市选择、结果卡、来源面板、已有收件箱组件、收藏和设置，随后个人智能体、记忆、初始设置、社群及其他二级页。媒体需核对接口、模型与真实渲染；原数据/测试fixture保持真实性，隔离视觉样例明确标识，缺少获准媒体不伪造封面。
- 用户补充明确：全宽、底部贴边仅适用于展开后的长内容阅读或编辑；Now首页保留10月11日确认的“联系＋通知、短头像列、局部透明材质”，不增加常驻大面板。“收件箱”指现有组件，不新增收件箱入口。
- 本轮要求逐项结果、同数据/尺寸/普通字号的前后截图和实际相关测试。仅编译不代表全站体验验收；未运行记录NOT_RUN，未检查记录NOT_INSPECTED。删除说明同时去除专用空容器、无效标题与间距，保留必要授权、真实性、操作后果和后台校验；不录屏。

## 2026-10-15 用户再次指定：减少测试，先推动开发

- 用户明确表示相关测试过多导致开发进度过慢。本轮及接续优先完成实际编码、接口与客户端接线，再合批编译交付；不把每个小项展开为测试矩阵。
- 开发批次只保留必要的直接验证；出现实际编译错误或关键缺陷时针对定位、修复，不重复已经通过的全套测试、全量分析、数据库整检、装包、截图与验收。全面验收留到必要的整合或发布检查点。
- 保留已有测试与失败证据。未运行的检查如实记录 NOT_RUN；编译通过和局部验证不等于全站体验或发布验收通过。正常字号、不录屏、中英混合语音延期及原ID、依赖、发布门槛继续有效。
