# BT-ORG-004 组织成员生命周期验收

日期：2026-10-01。范围：本地开发 API、PostgreSQL、Flutter 与 Android 真机。**不构成真实 CSSA 管理员或生产身份验收。**

## 审计与实现

原数据库有 `active/invited/removed` 状态，但无成员邀请、接受、改角色或移除接口；唯一 active owner 索引也阻止先增加新 owner 再移交。新增迁移 `028_organization_membership_lifecycle.sql`，允许多位 owner，并扩展管理审计的资源类型、动作及旧/新角色与状态。Go 事务锁定组织行后核验当前个人账号和角色，再执行账号 ID 定向邀请、本人接受、角色变更、撤销。owner 可增加第二位 owner，再移交；最后一位 owner 不可降级或退出。admin 只可管理 member/moderator，跨组织 membership ID 无效。成员邀请出现在目标账号的站内邀请页；无需外部发送服务。Flutter 组织控制台提供中文成员管理，侧边栏提供“我的组织邀请”和本人账号 ID 复制。

## 可复现结果

| 检查 | 结果 |
| --- | --- |
| 在本机空库顺序应用迁移 `001` 至 `028` | PASS，28/28 |
| 空库对 `028` 执行 down 后重新应用，并执行开发 seed `001`、`002` | PASS；有成员审计/多 owner 的库须先人工处理才能降级 |
| `automation/verify_membership_lifecycle.py`，本机 `127.0.0.1:3694` | PASS：匿名 401、非成员 403、owner 邀请 201、重复邀请 409、目标账号接受 200、普通成员操作 403、最后 owner 降级/退出 409、admin 权限限制 403、移交及撤销 200/204、撤销后 403、跨组织操作 403 |
| PostgreSQL 查询合成组织 `428c68d5-be2d-4773-b416-890c428de1fa` 的 `admin_audit_events` | PASS，8 条成员动作，包含 targetAccountId、旧/新角色及旧/新状态；新邀请旧状态为空，撤销由 active→removed |
| `flutter analyze`、`flutter test` | PASS，0 问题、76 项（包括中文邀请与接受页） |
| `go test ./...`、`go vet ./...`、`go build ./...` | PASS |
| `flutter build apk --debug`、`adb -s c641566b install -r ...` | PASS |
| 真机 `c641566b` 的 `Birdtie device QA` 合成组织：owner 通过中文页邀请、目标账号 API 接受、列表刷新，随后点选“设为管理员” | PASS，页面显示目标为“管理员 · 已加入”；[截图](evidence/2026-10-01-member-role-device.png)。该账号是固定开发验证码身份。 |

本地脚本生成的组织和用户均为合成开发资料。正式组织成员邀请仍需真实 IdP 账号、主办方授权与运营核验；上述权限机制不能单独证明 CSSA 试点已准备好。
