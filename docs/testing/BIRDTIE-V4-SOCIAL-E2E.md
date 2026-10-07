# Friend → Chat → Share：仪器与真实人员验收

对应原 `BT-V4-E2E-001`。原 AC 保留：Two real test users connect, restart, chat, share a real Place/Activity card and reopen it successfully。依赖 `CHT-003`、`TIE-001` 已完成；本文件不改变原 AC、不升级发布门槛。

## 本地仪器路径

从仓库根运行（Python 为本机可用的 Python3）：

```powershell
python -B -m unittest discover -s automation -p test_social_e2e_local.py -v
python -B automation/verify_social_e2e_local.py --round local-check-unique-name
```

round 目录必须不存在，不覆盖旧结果。只适用 Windows 本地 Docker PostgreSQL 与已核证冻结 078 程序。默认 API 可执行文件位于 `work/v5-age038-resume/phone-chat-binding1/birdtie-api078-community-city.exe`，SHA256 为 `0e5c68bcff43810644881198c5440145c1cd3c7d2e1092d5f95d7dd69aae7f8a`；该程序源为 `native-full-activity078-3/source/apps/api`，750 份源已逐 SHA 核对。baseline001-077 和 078 up 均来自该原不可变帧，不读取变化中的 079 代码/迁移。

runner 新建随机 `birdtie_social_e2e_<16hex>` 库并记录精确 COMMENT 所有权标记，使用新 loopback 端口，拒绝手机 4173/3697 端口。仅保留必要系统环境，移除借用 OIDC、模型/云凭据及 Birdtie 功能配置；认知功能保持默认 OFF。启动与停止核验自有进程 executable path/SHA、监听端口 PID；Popen 只停止自己创建的进程。Windows terminate 是实际进程终止，exit1 有记录，不称 graceful shutdown。清理需 shared catalog 中原 marker 完全相同，才 DROP 自有库，随后验证库数量为零。

流水线均走原注册 HTTP：

1. 两次独立开发登录；各自明确 PUT 本人 public Profile，标明合成身份，不以 SQL 绕过资料权限。
2. A 发 friend 申请，B 接受；双方读取同一 Tie ID。接受只产生 Tie，不自动开聊。
3. 停止并重新启动 API，同库、同两份 Session；两方读取原 Tie，主动启动同一会话。
4. 双方各发一条普通消息；A 明确分享一个 Place 和一个 Activity 稳定引用，显式同键重放仍是原消息。
5. 再次停止并启动 API；双 Session 读取原 Tie、会话、四条消息，按卡 ID 重读当前权威详情；原 owner 按操作键读同一回执。原四条消息数量和 rowHash 相同，公开源只在重启读前后核对不变。
6. 匿名拒绝；A 明确 logout 后旧 Session 拒绝，B 仍可读。隔离 fixture Place 改 hidden 后，当前详情和新分享拒绝，原卡不返回 ID/标题，消息真源零新增。此隐藏是负向 fixture，不称真实人类发布/撤回操作。

每条业务请求留阶段、去敏路径、PID、X-Request-ID 和状态；在所属进程 API 日志中验证该 ID/方法/状态恰好一次。原令牌、连接串、账号私密正文不写证据。超时 mutation 本轮停止，不自动重发普通消息/申请；同键正常重放是独立、明确测试操作，不把 unknown 视为成功。故障/无效输出与边界拒绝有纯测试；本轮未真实注入丢响应故障，不能把纯测试称 native 网络故障验收。

## 已运行结果

- pure3：19 tests，0 failure/skip；`work/v4-e2e001-local/pure3.log`。
- native1：完整业务 flow PASS，cleanup FAIL（错用 `obj_description` 查数据库 shared COMMENT）。原失败完整保留，恢复清理前验证 shared COMMENT 对应原 issued SQL SHA；另存 cleanup-recovery，不重写原结果。
- native2：完整 flow 和 cleanup PASS。
- native3：完整 flow/cleanup PASS，3 个自有进程、2 次 API 重启、46 条 request/log 精确关联，原四条消息 count/hash 同值、所有负向场景通过，库和进程均已清理。

实际 evidence 位于 [本地仪器证据](evidence/social-e2e-local-2026-10-04/README.md)。这些是 LOCAL_SYNTHETIC_INSTRUMENTATION_ONLY，不是两名真实人员、真实供给、App 真机交互或当前 live079 验收。

## 完整任务的恢复条件

| 条件 | 当前缺项 | 真实验收须记录 |
| --- | --- | --- |
| 两名实际测试人员 | 未有两名具名同意测试者及各自操作证据 | 测试负责人、人员代号/同意时间、各自账号绑定与会话环境；不保存凭据 |
| 真实可分享目标 | 本轮只有 frozen dev seed | 一项真实 Place **或**主办方确认 Activity 的稳定 ID、出处/核验时刻、当前公开与分享许可；不强制虚构 CSSA 活动 |
| 产品内操作 | 本轮是直接 HTTP 仪器 | 两方分别申请/接受、主动聊天/发送卡片、接收方打开权威详情与返回截图及对应请求 ID |
| 实际重启持久化 | 仅 API 进程重启已测 | 明确区分 App/API/登录重启，双方重开后同 Tie/会话/消息/目标 ID，日志和测试人员确认 |

Social Alpha 原 AC 不自动变成 CSSA 发布门槛；生产 IdP/HTTPS、现实组织活动和运营条件仍在原独立 gate。Debug 与固定码不能证明身份/手机所有权；两台机器、两个脚本账号或代理两次登录都不能冒充两名真实人员。任何真实联系人消息必须由实际用户自行明确操作或获得其直接授权，当前自动执行仅合成隔离数据。

本轮代码切片完成后完整任务应 **PARTIAL**，缺项/恢复条件由根代理保留。Closed Pilot Ready=NO，Consumer Beta=NO；没有正式部署、外部联系、付费或自动 Agent 动作。
