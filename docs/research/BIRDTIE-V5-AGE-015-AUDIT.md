# AGE015 原生渐进 Seed 审计

2026-10-03，owner seed_onboarding。原 source AGE015/016/017 与唯一 live queue 由根代理管理；本文件不改队列。

## 原要求及可复用能力

- 昵称使用真实 user_profiles/Profile 方法，不新增另一个 displayName。
- 浏览城市默认值是界面目录状态，不是本人当前城市。原 person_contexts + contexts 的 CITY/current 可保存本人声明，但原 owner-only context endpoint 没有当前 session/CAS；新本人 Seed bridge 在同事务和 final gate 下复用表。
- 语言、兴趣复用原私密九字段，不通过 AgentNotes 或 query 代填。旧 full replacement 外部方法保留；只抽同事务 helper。
- 既有公共 SocialIntent 不满足私密基本意图。066 是独立 UserIntent，来源必须人工明确选择，含稍后进度和独立版本。
- 登录/恢复入口和 Settings 直接入口实际接入现有客户端；旧历史 Moment/Now/Map 源保持原功能，未复制 Civu。

## 具体风险及处理

| 风险 | 实际处理/证明 |
|---|---|
| 本人ID或 JSON Verified 自证 | trusted actor +真实session digest；未知键拒绝，Org/Biz fail closed |
| 初次认证后等待撤权/过期 | 资源等待后锁Session，最终DB clock；原生 barrier/HTTP请求正负测试 |
| 全资料替换覆盖其他字段 | 原 helper 同事务 CAS，仅 language/SET personal preferences 更新；其他私密字段/bio/visibility保持 |
| 新登录复用旧具体批准 | digest 绑定 source snapshot，新session409 |
| 旧源删除重建/同版本恢复 | actual xmin+row source comparison；独立UserIntent重建负例 |
| City目录无关变化误冲突 | 本人源与选定 City 的独立snapshot；无关目录增量不冲突 |
| 目录过期/撤下冒称本人声明可用 | 原published/active/finite expiry核查，当前城市未知或409，最终时钟复核 |
| 浏览首城冒称当前城市 | unknown不预填，实际widget与native无声明正例 |
| 原声明public却界面说private | 明确预览「保存为私人城市声明」，原表同事务限定本人行改private |
| 旧PrivateProfile和Seed账户锁不兼容 | Account SHARE；真实旧入口+Seed+Revoke等待检查及明确错误结果检查 |
| 网络丢回复盲重发 | resultUnknown阻止PUT，GET权威当前值核实；与具体请求提交成功区分 |
| 新schema破坏并行完整测试 | down/up只在owned runner独占SQL阶段，普通Go测试不drop共享表 |

## 首次失败与真实修正

work/v5-age015 保留原始日志：domain 发现孤立 Unicode surrogate 被旧 normalization 接受，新增 wire escaped-pair 验证；初始 HTTP spy 构造参数数量修正；初始 Flutter 分析10info和后续1info均修正；入口测试错误复用不存在的 City client 参数、缺必需 Moment auth callback、SemanticsHandle 晚释放均为测试夹具错误并保留原日志。

native-authority-red1 原始73 PASS/4 fail events来自真实 schema 夹具错误：Agent status 未允许 paused，cities无summary列；按真实 suspended/name字段修正后 native-authority-green1 实际76 PASS/0 fail/skip、vet/build0、全部 public相等、source428稳定、owned DB DROP。

native-red1、native-http-red1、native-city-schema-red1 的功能成功与整体source帧失败分别记录：并行033等非owned源变动使sourceUnchanged=false，不能用它们冒称最终全仓固定帧。最终同SHA证据另归档。

## 验收边界

可在 CODE_LOCAL 实际验收人工渐进填写、私密独立Intent、现有字段保存、服务端CAS/撤权回滚、恢复和直接编辑入口。seed非自动内容授权、无模型/自动推断/Memory消费，也未激活组织或Business。真实生产身份、外部授权、现实用户样本、部署/值守和 Closed Pilot 门槛仍未由本地fixtures完成。

worker只跑已授权 targetGo/Flutter/analyze；完整Go及Flutter/APK/真机由根代理协调固定源窗口。最终计数、sha、raw、完整public和cleanup见专属 evidence manifest；历史日志不覆盖。
