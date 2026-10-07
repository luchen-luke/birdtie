# BT-V4-SAF-004 审计、计划与实现核验

2026-10-02，当前 DONE；live 队列为唯一状态源。依赖 SOC001 与 AGA001 已有实际证据，保留已有未提交内容。规范：[社交推断与动作安全合同](../architecture/AGENT-SOCIAL-INFERENCE-SAFETY-CONTRACT-V4.md)。

## 实际审计发现

- 无真实 learner/推断 Memory/公开分享 grant/自主 executor；明确 query 条件与有期限事实计数不能改称推断。
- Agent 响应原有任意 action 字符串和过滤字段投影，缺中心闭集保护；同一持久 task 需保留内部 resultIDs 而只过滤输出。
- 客户端旧登录布尔值比较不能识别 A→B；已打开的确认、私人草稿与迟到请求缺身份绑定。组织刷新须同样清除私人上下文，同时保持已审核公开 Place 与地图实例。
- 现有人类直接流程已有后端权限和确认，应复用领域服务；不为此新增第二套 Agent 写入路径。

## 简短计划与范围

1. 最小来源/性质/有限未校准分数合同与独立当前版本许可，敏感/未知/撤权/无来源默认拒绝；不启动 learner 或公开路由。
2. 当前响应只保留绑定主体任务的安全中文导航，组织入口取 live owner/admin 菜单；过滤副本不修改数据库条件。
3. 私人结果、草稿、详情和确认绑定授权与请求序列；账户或组织变化、旧响应与旧批准失效，保留直接路径。
4. 纯策略/动作闭集、真实 PostgreSQL 与客户端异步负例→完整回归/迁移/构建→Android 安装与场景→证据→队列。

## 本轮实现路径

- `apps/api/internal/agentruntime/social_inference.go` 与对应测试：仅 server-only 纯候选策略，未来 AIR 的批准/效果账本另行实现。
- `apps/api/internal/agentworkspace/action_safety.go`、`response.go`、`model.go` 及 HTTP Agent handlers：绑定导航和统一安全投影。
- `apps/api/internal/httpapi/agent_action_safety_test.go`：真实自有 PG fixture 验证当前读取与恢复，没有隐式业务副作用。
- `apps/client/lib/src/workspace/map_workspace.dart`、`social_intent_drafts.dart`、`connections.dart`：授权监听、草稿/确认/详情生命周期保护。

## 规则与证据边界

适用 UX-CHECK-04/05/06/08/10/12/16；14 只完成键盘范围，大字体/读屏待核验，15 无提示消费者观察未执行。消费级评估原文已找到（文件名带 `(1)`），用于权限、在途失效与真实消费旅程核验，不创建新 backlog。本项测试不能替代真实 IdP、CSSA、线上投递、模型批准和正式发布验收；Closed Pilot NO。

## 本轮实际验收

203 项纯策略 PASS、168 条动作/真实 PG 专项 PASS；fresh001–052/三个 seed 默认并发 full Go 三轮各 1112 PASS，均无 FAIL/被跳过测试；vet/build、迁移与旧 ID/拒绝破坏性 down/空 down-reapply PASS。Flutter analyze、198 测试及本轮 Debug APK PASS。ADB 真机安装成功，当前 API binary hash 与完整回归一致；同 task 连续追问保留条件，退出撤销 session 并清空结果/历史，第二 Person 登录和 App 重启不会恢复第一 Person 历史，九类业务计数未变。见 [完整证据、命令、限制与复现](../testing/evidence/agent-social-safety-2026-10-02/README.md)。

替换旧 Windows API 的终止操作被自动审批拒绝后，使用空闲本地3697端口与ADB设备3694映射完成验证，保留原进程，没有重复尝试被拒绝动作。现有 Kotlin 插件迁移提示不影响本轮 Debug APK；未声称 Release/真实 provider/推断公开接口或自主效果通过。本项 DONE 限于其安全合同及当前实现保护；V5 Memory、AIR批准/效果账本与生产门槛各自继续未完成。
