# BT-V4-E2E-001 本地仪器编排审计

2026-10-04，owner sponsored_trust。原任务 `work/v5-age038-resume/original-e2e001-todo.json`、六范围 lease `e2e001-local-lease.json` 由根代理登记；worker 不修改队列、共用报告、Go/Dart/DDL、手机 API/库或现有用户内容。

## 去重与缺口

原 FriendTie HTTP 测试已有两份 test Session、申请/接受/双向 Tie，旧 custom mux；postgres FriendChat 已用 fresh pool 重读普通消息，不是进程重启。CHT-003 registeredHTTP、七类原键、双方 ACL、未知恢复及真机单会话分享已完成。原 `verify_inbox_conversation.ps1` 用 SQL 建 accepted request/conversation，不能代替自愿 FriendTie 申请链。

本轮新增的是同两份独立会话跨真实 API 生命周期的完整编排，复用原登录/Profile/FriendTie/Chat/Share/详情/会话撤销路径；没有新产品接口或授权账本。原 AC 里的两名真实测试人员及真实 Place/Activity 未获得，故不以编排通过标全任务 DONE。

## 实施与原生核验

`automation/verify_social_e2e_local.py`：固定可信 binary/SHA、750 frozen078 源验证、明确自有 DB marker、新端口/同 PID readyz、两 Session 明确公开资料/朋友申请接受、两次真实进程重启、普通消息和结构卡、原键回执、当前源开同 ID、撤销/隐藏拒绝、零重复/原行 hash、不携带模型/OIDC配置及去敏仪器记录。

`automation/test_social_e2e_local.py`：19 个纯测试覆盖非 loopback/手机端口/任意路径拒绝、准确 DB/UUID、环境白名单、body/token去敏、普通 POST/PUT/DELETE unknown 停止不重发、读不可用/错trace/status/redirect/bounded响应/deadline、binarySHA/SQL目标、停止时 path/listener mismatch拒绝、DROP marker拒绝、deadline后的仅清理、原生trace方法状态/PID恰好一次。

真实 first native1 业务链已过但 cleanupFail，不计整轮通过。数据库 COMMENT 在 shared catalog，原 `obj_description` 得空；改 `shobj_description` 并补纯回归，未放松所有权相等要求。恢复清理使用 shared COMMENT 重建原 issued COMMENT SQL，并与旧 marker.json inputSHA完全一致，确认所有自有 PID已停后 DROP，另存证明；原 native1/result.json不改。

native2 flow/cleanup 通过；新增环境最小白名单及逐 request API日志关联后 native3 通过：46 条请求，3 个已核 executable path/SHA/监听 PID 的真实 API进程，2次进程重启，双 Session原Tie/会话/四消息/两卡/原操作键一致，消息 rowHash重启前后同值；匿名、logout旧Session、hidden来源/新分享拒绝、旧卡遮蔽无ID标题、零新消息。所有进程均已停止、每个自有数据库已 DROP，未读手机token/DB或借用其PID。

## 冻结、证据和界面规则

本地命令和真实 ID/日志见 `work/v4-e2e001-local`，源码冻结 `sourcefreeze1.json`。root提供冻结API binary `0e5c68bc…aae7f8a`，只验078；live079并行变动不作为此次稳定帧。没有重跑变化中的全Go/Flutter，也没有称旧构建覆盖本轮新脚本。

适用 UX-CHECK-04/06/07/08/09/10/12/14/16：复用人类普通动作，具体确认和权威结果区分，身份/原键/撤权处理、模型不可用普通路径、去敏来源和当前权限。仪器记录不是移动端、TalkBack或目标用户体验验收。普通消息 unknown native 网络故障/真实 App重启/真人/真实供给均未在本轮执行，如实保留；此前 CHT具体证据不伪称本E2E两真人通过。

完整恢复条件和逐步产品验收见 `docs/testing/BIRDTIE-V4-SOCIAL-E2E.md`。建议最终 PARTIAL，真实人员和资料缺口保留，不自动降依赖/发布 gate；Closed Pilot / Consumer Beta 均 NO。
