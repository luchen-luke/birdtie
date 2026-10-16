# 新关注通知与 Civu 社交增量适配

2026-10-16，父任务 BT-FIX-NOW-UI-001。完整复用差异见 `docs/audits/CIVU-SOCIAL-REUSE-MATRIX.md`；精确20个产品/测试文件的路径和 SHA256 见同目录 `freeze.json`。Civu 全只读，无生产 fixture、真实登录、部署或 Git 写入。

## 实际变化

- `apps/api/migrations/175_person_follow_notifications.sql` 新增不可变 `person_follow_events` 与 native decision 外键/封闭 kind 合同，扩展现有 current source/policy guard。仅新原生关注 writer 同事务建立 event，不在 follows 上增加 backfill trigger，不修改或重放历史数据。对应 down 在存在事件/通知时拒绝回退，空库 down/up 已验证。
- `internal/postgres/follows.go` 只在 INSERT 实际新增 person follow 时调用新 `person_follow_notifications.go createPersonFollowNotification`；相同关注重试保留原关系与事件。取消后重新明确关注是新事件。`internal/agentnotification/registry.go` 和 `notification_routing.go` 复用现有 SOCIAL preference/decision/Inbox 路由。
- Birdtie 使用单一 `person_follow`，不复制 Civu 第二份 mutual_follow 通知；互关本身不升级好友/会话。通用通知无原姓名、头像或资料快照。
- `GetInboxPersonTarget` 与 HTTP `/v1/me/inbox/{itemID}/person-target` 从当前 event、source follow、原收件人、Session、block、active account、profile ACL 解析目的地；最终响应再复核。初读失效404，最后一刻失效409。通知不授予资料权限。
- `internal/inbox/page.go`、HTTP/PG `inbox_page.go`、PG `inbox.go` 增加显式协议能力：新客户端分页头 `X-Birdtie-Inbox-Features: person-follow-v1`；游标绑定该投影。旧客户端在 count/LIMIT 前排除未知新类型，旧 nonpaged 也过滤；事件本身保存。服务端未读数与同一权限/能力投影一致。
- Flutter `remote_inbox_source.dart` 与 `inbox.dart` 复用已有 Inbox 与 `PublicPersonPage`，解码、已读与当前目标分别校验。换号/组织变化/迟到响应清退，当前 resolver 拒绝留在 Inbox。保留 root 已修未知类型通用只读占位，未知项不 markRead、不跳转。
- `connections.dart` 只修无标题 Moment 分享预览：标题、正文前120个Unicode字符、最后“分享动态”；不把正文写回标题，不取消原内容读取权限。

## 本轮直接证据

| 回执 | 结果 | 具体边界 |
|---|---|---|
| `share-before.json` | 1 PASS / 2 FAIL | 保存修复前无标题拒绝、旧身份迟到预览缺陷 |
| `flutter-targeted.json` | 8 PASS，exit0 | 分享新3 + 原历史社交5；Mock，仅选定 source frame 前后相同 |
| `go-domain.json` | 4顶层 PASS，exit0 | 历史原状态、cursor、申请与消息操作契约；无数据库 |
| `follow-native01/receipt.json` | 1 FAIL，exit1 | 测试误把 block 后已删除关注当仍可成功取消；完整175与空down/up此前已执行 |
| `follow-native02/receipt.json` | 1顶层 PASS，exit0，0 SKIP，sourceStable | schema175真实隔离PG；新关注/重复/旧协议计数/当前权限/已读/原关系撤销/禁止升级与Push |
| `follow-http01.json` | exit1 | fixture 未实现当前会话帧；产品认证未放宽 |
| `follow-http02.json` | 2顶层 PASS，exit0 | registry + HTTP；最终结果中 registry 仍适用，HTTP由下一轮覆盖 |
| `follow-http03.json` | 1顶层 PASS，exit0，sourceStable | 最终handler：成功、初读404、迟到409、匿名401、caller目标拒绝、能力与cursor |
| `follow-flutter02.json` | 6 PASS，exit0，sourceStable | 新关注3 + 原未知占位3；当前目标403/404、换号迟到、组件路由与无会话升级 |
| `legacy-history175/receipt.json` | 1顶层 PASS，exit0，0 SKIP，sourceStable | 最近受影响的既有历史通知：原读/删态、owner隔离、重复导入、当前Session与禁止model/outbox/delivery/push |

重复回归不是新增覆盖；follow-flutter01的3项包含在02的6项中，follow-http02的HTTP项包含在03中。root 的 `legacy-native/selected01` 在schema174的2项与175历史通知测试有一项重叠，单独标明，不能重复宣传总数。

两项原生执行均通过 `run_follow_native.py`：只从 Docker 配置读取内存凭据，强制 loopback 127.0.0.1:55432；父 QA name/OID/owner/comment 与保全 identity 文件精确相等，前后同一身份。测试 helper 新建随机且带所有权标记的子库，完整迁移后仅清理该子库。不会把 fixture 导入父 QA 或真实使用环境。具体 command/cwd/exitCode/source frame/日志 SHA 在各回执，凭据不输出、不落盘。

本轮源码变更并不证明已部署。真机、实际双人流程、旧安装包联网打开、断网重启、后台 Push、多设备未读、所有 Moment 入口的完整端到端均为 **NOT_RUN**，由父任务后续部署/验收记录。没有把历史报告、页面存在或编译当作这些验证通过。
