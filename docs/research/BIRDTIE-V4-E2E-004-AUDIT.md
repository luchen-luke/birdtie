# BT-V4-E2E-004 跨城线上原生端到端验收

日期：2026-10-04；唯一仓库 `D:\Project\birdtie`。适用 UX-CHECK-02/05/06/07/08/09/10/12/16。原 NOW005 / CTX001 依赖已 DONE；root 登记 worker scope 并在真实 RED 后扩租唯一 `postgres/activity_plans.go`。状态及共用报告由 root 核证。本轮没有 UI、DDL、模型、外部服务或正式发布操作。

## 接口与验收路径

使用真实 PostgreSQL、001–083 迁移及三个原始开发 seed，两个独占合成人员、稳定全局 Person / Personal Agent / Session。真实注册 HTTP 创建各自不同 CITY 的 PRIVATE current 自声明和同一个 ONLINE interest；城市没有测试所需的地图视口、Place 或个人定位。CITY 自声明不证明居住事实。

`httpapi/v4_cross_city_online_e2e_integration_test.go:378` 两人分别通过原 `/v1/me/social-intents` 创建 PUBLIC ONLINE DRAFT，再从原 HumanActiveIntent 具体版本 preview 独立 approve ACTIVATE；原 Intent ID 不变，City / Place 为空。各自在 `/v1/me/now/online/tasks` 明确查询，真实 Task 使用 ONLINE Context UUID、空 City 和当前本人主体，原机会读取相互返回对端 Intent 原 ID。没有 GPS/map/location 输入、自动发消息或创建 Tie。

主办方通过原本人 Activity 路径明确创建并发布 invite_only ONLINE 活动，无 Place，保留主办方合成 City 发布元数据。另一城市的陌生用户在邀请前详情 404；主办方明确邀请后，双方独立查看、各自 RSVP going（原键重复 200）、加入本人 Plans。Activity 发布仍复用原领域 API，不声称它已升级为新的具体预览权限合同。RSVP 是报名，不是到场。全过程 accounts / agents / public Profile / Agent metadata / Session 稳定身份（不含合法 idle 更新）/ 当前声明 / person_ties 完整等式不变。

真实子进程 PID **38956**、**41712** 各自创建新的 HTTP listener、native Store 和 pool，重开双方原 Context、Intent、Task、Participation、Plan / 空 Tie 列表；两个过程实际退出后再次启动。主线日志含 `e2e004_0001…` 和 `e2e004_restart_0_0…1_5` 真实请求 ID、合成主体及原领域 ID。没有记录 bearer token。重启前后所有 public 领域表完整摘要相同，唯一明确排除 `sessions` 表中 Authenticate 的合法 idle 刷新；Session 稳定身份另行严格核验。并非只重建内存对象冒充 OS 重启。

## 实际缺陷与修复

`native1-red` 真实 peer GET invite_only Activity 404，但 `e2e004_0004` POST Plans **201**：原 PlanActivity 缺 `birdtie_activity_visible_to`；ListActivityPlans 同样缺 ACL。root 扩租后在原 INSERT SELECT 与 List LEFT JOIN 中复用该唯一可见性函数及当前 City / Activity PG 时间期限。未邀请、撤邀、组织成员 removed、任一方向 Block 或源过期后，新增 Plan 404；旧本人稳定 Plan 保留为 unavailable，title / City 为空、时间不返回。授权仍有效的 cancelled / past 保留原显示语义。没有复制授权台账、改 RSVP 或删除本人计划。

真实负向案例见新测试 :196（撤邀、双向 Block、City / Activity 过期、cancelled），:241（普通组织成员 → removed），:260（PG clock 200ms City 期限自然跨越及 past），:323（未受邀零写）。普通组织成员数据为隔离权限 fixture，不表示现实会员或真人组织。

## 命令与证据

最终 `work/v4-e2e004-cross-city/native8-final/commands.json` 原样保存：

- `go test ./internal/httpapi -run '^(TestV4CrossCityOnline)' -count=1 -json`：exit 0，6 test PASS / 0 FAIL / 0 SKIP / 0 package FAIL。其中 5 个实际验收 case 和 1 个 child 启动 guard helper；两个真实 child 执行包含在主线输出，不能把无 child 环境的 helper 返回当独立功能覆盖。
- `go vet ./internal/httpapi` / `go build ./internal/httpapi`：均 exit 0。
- copied native frame 841 个 Go / SQL / module 源全部稳定，两个 owned 文件在前后与当前 hash 相同。这是捕获帧定向验证，**不是随后 live 全仓验证**；NOW004 其他 worker 仍可在其独占源实施。
- parent 完整非空旧 public 及七类 semantic catalog 前后完全相同；每个 owned child 在清理后所有原 public 行（包括 sessions）严格等于创建 fixture 之前。没有忽略 audit 表。所有 owned fixture residue=0、子库及 parent DROP 成功。

最终 source SHA：

- `httpapi/v4_cross_city_online_e2e_integration_test.go`：`90730ee214e0525f98015f0d8354eada5f5de6d30f4aec5e008654f0b0639c10`
- `postgres/activity_plans.go`：`8bf9da6f2c3a2809e3bf57947ef56180e54f5ed5c6787822e6b2905972e4f994`

## 保留失败及范围

初编译的相对 gofmt 路径 / unused import 诊断保留。native1 有真实 Plans RED，同时有误用 cleanup owner_id/source_key 列的 fixture 错；native2 修 cleanup 后主线发现误用 ties 表名，改用 person_ties；native4 membership revoked 不合法枚举改为真实 removed。native6/7 新增严格 child 完整旧行等式后，实际定位 `admin_audit_events` 未清理；只补清当前随机 owned actor 的审计，不排除审计或弱化等式。此前每轮原始结果 / 日志和实际旧 source 保留，native3/5 中间 GREEN 不代替 native8。全量测试由 root 在共同安全点另行执行。

完成分类建议：**CODE_AND_LOCAL_VERIFICATION**。真实两位用户、生产 IdP / 活动 / 地图配置 / 外部部署、手机与 TalkBack 本轮 **NOT_RUN**；Closed Pilot Ready / Consumer Beta **NO**。本轮证明仓库线上跨城能力与本地持久化，不构成真人合作方或发布证据。
