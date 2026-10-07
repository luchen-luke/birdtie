# BT-V4-SAF-001 原生社交投影隐私核验与修复

日期：2026-10-04。唯一仓库 D:\Project\birdtie。原任务依赖 MAP001/OPP003 已 DONE；worker 仅登记范围执行，状态与整仓回归由 root 核证。适用 UX-CHECK-02/05/06/07/08/09/10/12/13/16。没有 Civu 迁移、DDL、位置采集、权限台账或模型出口。

## 实际隐私边界

- `postgres/agent_workspace.go` 的公开 Person 来自当前公开 UserProfile 与本人明确公开有效 Intent。仅 `public_map_zone` 使其拥有根据已发布 City 中心计算的 **area** anchor；空 zone 的陌生人和 accepted 好友均没有坐标。朋友关系不增加精度；双方任一 Block、私密 Profile、撤回/过期来源不提供投影。`agentworkspace.WithContract` 将同一来源用于 result/card/pin，拒绝无 zone 的 Person 坐标。
- `postgres/new_people.go` 读取当前 `person_new_people_consent` 与 PUBLIC/FIND_COMPANION 的有效 ACTIVE 声明；目标范围取 Intent，不读取个人 City History。线上无 GPS 也能匹配。公开 Place 是声明的会面地点，不是本人当前位置。候选仅含声明引用、账号/允许的显示名、category/modality 和可解释 reasonCodes/reasons；不含原 title/platform/area/context、精确位置、会员清单或私密 Profile 值。
- `postgres/map_projections.go` 的公开图层只从明确批准的 Place/Activity/Moment/Organization/Business 公共来源取点。没有 Person GPS 图层。本人 Opportunity 图层只 SELF_PRIVATE，并使用原 Activity 的公开 Place。`human_social_now.go` 的匹配理由与 `inbox.go`/原通知投影保持最小引用，既有参与/位置声明不变成用户精确位置。
- 本轮自动化证据中的 City/Place、账号、声明和活动全部是独占可删除数据库的合成领域数据，不是实际主办方或真实用户。canary 只用于证明原 Profile/raw constraints/details 未进入最小结果。OS 实际定位许可拒绝、手机、TalkBack 和正式试点均 NOT_RUN；这里证明无 GPS 输入的数据路径仍正常以及未知 GPS 字段拒绝。

## 真实缺陷与实现

原 registered `GET /v1/me/new-people/candidates` 在 Authenticate 与 `FindNewPeople` 后直接响应。通过实际 native Store 生成候选，再在响应前实际撤销 Session、peer opt-out、双向 Block 或取消 peer Intent，五个负例全部返回 200 和旧候选。`work/v4-saf001-privacy/native1-red` 保存完整首 RED（0 PASS/6 FAIL，五 case + parent）；这是真实实现缺口，不是 fixture 或生产配置问题。

经 root 扩 lease 后增加 `newpeople.HumanStore` 与不可 JSON 序列化的 server-only `HumanReceipt`；保留旧 Store/FindNewPeople/invitation API。`postgres/new_people.go:239` 复用原匹配 SQL，同一 statement 投影原来源与 peer，增加最小 ID/xmin hash、source deadline 与当前 native owner/Session/PG clock。`postgres/human_new_people.go:60` 包含实际 Account/Profile/opt-in/Intent/target/City/Place、Agent field visibility 与双方关系/Block/邀请/Community current tokens；原 audit ID/xmin 捕获真实 Block→Unblock 的合法 ABA，未复制审计正文。

`humanNewPeopleRead` 先获得所需表 ACCESS SHARE 锁，再按原 Account→Session 顺序持行锁，最后一条来源 SQL同时重读 native Session、所有来源 ACL/version 和有效期；仅 Commit 位于其后，无尾部 Authenticate 或 Session-only 再等待。HMAC 绑定 actor/digest/具体 Response/proof/deadline；`httpapi/new_people.go:122` 先编码，再原生 revalidate 完整 receipt，拒绝 stale/forged/cross-owner，才输出已编码数据。实际 Session 被刷新或更改导致 row token 改变时，此短期读 receipt 可保守 409，重新 GET 获得当前读；不是授权续命。没有 invitation、关系、通知或领域写入。

原非 native fixture 仍可使用旧 domain Store；它没有当前原生 receipt 保证，也没有被包装成生产授权证据。生产 postgres Store 实际实现并自动走 native 分支。

## 精确测试覆盖

- `v4_projection_privacy_integration_test.go`（PG）：8 类公开 coarse zone/好友/Block/隐藏矩阵、9 类 opt-in/无 GPS/明确 Place/好友排除/双向 Block/过期/私密源；实际 published Activity→PRIVATE Intent→native Opportunity→非空真实路由 Inbox，以及公有地点与本人私有图层分离、source xmin ABA/隐藏 Place。所有 read/revalidate 前后 **完整 public 行相等**。
- `human_new_people_integration_test.go`：不可序列化/HMAC DTO伪造/跨本人、原 source+Session 版本、真实 Block→Unblock/opt-in/Profile ABA、好友与 pending 排除；8 类 `pg_stat_activity` 已观察真实 Session row lock 等待后撤权/自然到期/来源变更；公开会面 Place/City 当前、ABA、隐藏和自然期限。Session同 row revoke→clear 是显式特权 SQL 攻击测试；不声称存在正常产品恢复 API。
- HTTP 专属测试：真正 `httpapi.New` 路由与 Store，不是直接 handler/模拟权限；正常候选、peer opt-out、借用 source 404、GPS unknown fields 400；原五真实迟到撤权 now Session=401、peer source/ACL=409，拒绝错误响应泄露 peer 引用。旧 NewPeople/Map 所有同名目标测试同时回归。

## 失败保留与证据范围

- `native1-red`: 0 PASS/6 FAIL，5 实际 stale privacy 缺陷；vet/build=0，旧 public/catalog 保留，owned DB DROP。
- `native2`: 0 PASS/20 FAIL，真实 City fixture 未提供 schema008 视口 source_ref 违反 CHECK23514；按真实完整视口合同补 local:SAF001，未放宽 CHECK。
- `compile2` 与 `compile5`：原测试 helper 签名/unused import 与错误 AcceptFriendRequest 方法名导致编译失败；改用实际 `enrichmentAllPublic(t,pool,ctx)`/`DecideRequest(...,accept)`，原日志保留。一次错误 cwd 的脚本未执行变更，不是 PASS。
- `native3`27、`native4`49、`native5`130 Test PASS /0 FAIL-SKIP，test/vet/build=0，完整非空 parent public+catalog 保留、owned DB DROP。每轮 source 都是实际只读 API frozen copy，不声称根正在开发的全 current global 已通过。
- 最后 strict 状态断言后的 `native6` 实际130 Test PASS/0 FAIL-SKIP/pkg0、test/vet/build=0、copied840源稳定、完整非空public/catalog相等且owned DB DROP。当前七源SHA与该帧逐一相同，是待 root 独立核证的最终七源帧；命令、逐源 SHA、原始 JSONL、owned DB 清理及完整 catalog 在同目录。最终收据和不可变 archive 另存，不覆写早期失败或已完成 NOW006 档案。

## 完成分类

该任务可按代码与本地可复现自动化隐私门槛核证；最终队列 DONE 由 root 在全量回归与源核对后决定。原 IdP/CSSA/HTTPS/上线资料与独立发布门槛不在本任务完成证据中；Closed Pilot / Consumer Beta 仍 NO。没有把普通公开报名/会面声明当到场、会员资格或实时位置。
