# BT-V5-AGE-068 能力审计与实施记录

2026-10-03；正式任务由根领取，worker owner=profile_apis，只在登记路径实施，根负责唯一队列与共用报告。依据 AGE epic2106、现 canonical、原生迁移/实现、现客户端和实际运行结果；保留此前未提交内容。

## 审计发现

| 需求 | 修改前实际分类 | 本项处理 |
| --- | --- | --- |
| GET Agent Profile | REAL：本人 Private route 已含完整原生 metadata | 复用并运行横向真实 API 验收，不新增 combined endpoint。 |
| UPDATE Private Agent Profile | REAL：已有九字段、aggregate CAS、当前本人/exactAgent/metadata | 复用现领域，实现自己的真实双资源/并发/清空/重连证据。 |
| UPDATE Public Profile | PARTIAL：原 user_profiles / 三字段客户端已实现；正式 handler 只把初次 Actor ID 交 writer，无事务 Session current check | 先实际 RED 再新窄人类 gateway 与唯一 helper；保留原字段/意图撤回/内部接口兼容。 |
| Public wire | PARTIAL：共享 DisallowUnknownFields 不拒 duplicate/case/null | Public 独立严格 decoder；不影响 cityseed 或 Private 解析。 |
| Private/policy UI、辅助技术和真机消费验收 | 本轮未改／未运行 | 不把三个 API 完成等同 UI 或试点就绪。 |
| 模型/自动推理/来源许可 | 不由资料编辑 API 提供 | 不新增或借已存在 Profile/Memory 授权。 |

现客户端 `apps/client/lib/src/content/public_intent_section.dart:140` 只发送 displayName/bio/visibility，保持原合法三字段。组织实体 Profile 工作台另有原组织鉴权；新本人 Public route 仅原账号自身 user_profiles row，不创建角色，现 Organization/Business 普通 account 自身资料兼容实测，Private 仍403。

## 真实失败与修复

- `public-session-red1` 使用 Windows PowerShell5，psql NOTICE 被 runner Stop 当异常，测试未开始；自有 DB 清理。保留原日志，不称功能失败或 PASS。
- `public-session-red2` fresh001–064 原 native route：expiry / revoke 在初次 Authenticate 后等待 Account，均实际 200 且 profile_unchanged=false。2子测试失败（JSONL 4failEvents 含父和包），0PASS/测试SKIP；原源码 SHA、完整旧行稳定与 DROP 均保留。
- `new-gateway-compile.jsonl` 初次编辑多余 strings import 导致编译失败；删去后 compile2 成功。缺 disposable env 的仅编译／跳过不算真实运行。
- `public-session-green1` 新当前 Session gateway 3PASS/0FAIL/测试SKIP，真实两边界都401且无 source 变化。
- native1：116PASS；native2 两轮各117PASS/1实际测试失败（包另计），失败为新增 policy fixture 的 nil communityIds 未按原 NormalizeFieldRules 归一化，现 strict policy wire 正确400拒绝。只修测试输入，不放宽生产规则。全部 raw 保留。
- native3：两轮120PASS；加根审查的 nil ctx/Store/pool 防护、响应 exactself/输入绑定后，native4 最终两轮134PASS（42native/API、92transport/pure），0FAIL/测试SKIP。scope vet/build0；fresh64完整旧 public 行、8生产SHA稳定，自有 DB DROP。

## 最小实现与验收边界

唯一新增 canonical [PROFILE-APIS-V5](../architecture/PROFILE-APIS-V5.md)。不新增路由、schema、客户端、Permission/Memory 影子真源。原 helper被旧内部 writer与新 session-aware gateway复用，原AccessStore签名仍在；新secure接口缺失503，无旧 ownerID fallback。

真实 Account→Session→Profile 锁、显式RC、源/audit写后 clock、no-op final check；失败事务回滚。输出绑定检查防错接 port，但不替 native ACL。Profile/Private/policy/Memory/普通grant 完整非空记录、跨人、Org/Biz、缺源、期限、撤销、ctx取消、两路 CAS 与重连皆有实际测试。当前 Public `updated_at` 与 metadata版本独立，不伪造CAS数字。

验证命令与哈希见 [正式证据](../testing/evidence/profile-apis-2026-10-03/README.md)。根须独立核证与当前全 Go 回归后决定状态；本 worker不写 live queue，不拿 scoped证明代替全仓或正式部署。无新DDL，new up/down/reapply 不适用；fresh001–064 与 retained old rows仍实际执行。无客户端变更，Flutter／真机／截图未运行；Closed Pilot继续NO。

## 根完成核证（2026-10-03）

根独立134PASS/0fail/skip、8源/fullpublic/fresh64/drop与vet/build0；当前全Go7063 PASS/0fail/testskip/fullvet-build0、504API/18owned冻结，578 worker档与88 root档逐SHA核验，原真实public session expiry/revoke失败与修复可复现。见[根receipt](../testing/evidence/profile-apis-2026-10-03/root-independent-final.json)。三既有registered APIs本地DONE；无新客户端/手机API部署/真实IdP/生产证据。Private Profile、Public Profile和metadata真实来源独立，不镜像。Closed Pilot/Consumer Beta NO。
