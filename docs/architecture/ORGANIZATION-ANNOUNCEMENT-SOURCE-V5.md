# Organization Announcement 原生来源契约

2026-10-03；仅补原 AGE-053 第五类来源。沿用同任务四来源实现、认知 ADR、068 当前管理员权限与 [交互规则](../ux/GLOBAL-UX-INTERACTION-CONTRACT.md)。此前181PASS和71文件worker-final1不修改；本轮以新source frame验收。

## 审计与最小边界

真实 `rg announcement/公告` 仅找到 AGE原文、068 Memory类别/注释和053 Unavailable；不存在公告资源表、native公告model/store、发布撤回API或版本来源。025 FAQ是question/answer/published，不改名为公告；068 ANNOUNCEMENT仅人工私密注释，不作公告发布源。原需求没有要求广播、通知、社群公告、AI发布或新增UI，均不在本轮范围。

实现独立 OrganizationAnnouncement 领域；不新建重复任务或复用FAQ/Memory正文作为发布资源。075为本轮新迁移，073保持原文，075仅增第五Evidence shape并创建公告资源/预览/审计。root独占server路由注册和live queue/shared reports。

## 实体与授权

- 稳定source UUID；组织entity ID与真实Organization account principal分别存储且原生约束一致。`revision` 真实从1起逐次CAS递增，创建者/修改者是实际Person，title/body有界UTF-8文本，公开受众固定PUBLIC，明确validUntil。
- 人工DRAFT、PUBLISHED、WITHDRAWN分别表达。新建默认DRAFT；更新任何已发布内容都进版并退回DRAFT，不自动沿用公开批准。WITHDRAWN终态且不自动复活；新公告使用新ID。
- 管理动作复用真实Bearer session与068 Org-first权限事务/当前owner-admin；普通成员、moderator、非成员、组织/商家token无权管理。JSON中Org/Actor/role/verified/confirmed不能授权。
- 具体发布预览绑定当前实际Person、session ID、membership及updated_at、当前Account/Org/Agent/metadata快照、公告id/revision/内容/受众/期限。使用实际源snapshot，不发明权限epoch。任何改版、换主体、换session、撤权/角色改变、短预览期到期，都拒绝旧预览执行。

## 人工动作

1. PUT draft：expectedRevision/title/body/validUntil，仅保存私密草稿。相同ID+相同当前内容的精确重试幂等，不同源版本冲突。每组织bounded128条资源；withdrawn历史保留，不回滚擦除。
2. POST publication-preview：仅expectedRevision。当前管理员读取完整内容、组织身份、PUBLIC受众、具体版本、有效期和“公开后可由访客查看”后果；服务器生成previewId与短deadline（至多10分钟且不晚于资源期限），持久存仅binding/digest/期限。previewId不授权限，也不是confirmed字段。
3. POST publish：expectedRevision/previewId，明确发布请求。当前授权、实际源revision/snapshot与预览绑定全部重解一致才提交；state进入PUBLISHED，真实revision进1，publishedAt服务器PG时刻。使用相同已消费preview的精确重试只可返回同一当前发布版本，不再次进版或审计。
4. POST withdraw：expectedRevision，单次进版，立即撤去公共可读与来源资格；精确重试幂等。编辑、改期和撤回均有原生审计，不记录原文到审计或Evidence。
5. GET管理列表/详情：当前owner-admin可找回草稿、发布状态、过期投影和撤回历史；错误中文、no-store、现有requestId。未知提交结果先读权威状态。
6. GET公开详情：匿名只能读精确该组织已明确发布、当前public/verified/active组织及account/Agent、当前publication context、有效公告版本。私密/未批准/草稿/撤回/过期/其他组织返回不可见，不输出管理人、session、preview、audit或private source字段。source/public context变化后不自动复用旧公开批准。

## 版本化与第五Evidence来源

`ORGANIZATION_ANNOUNCEMENT` 指向独立原生公告UUID和实际revision。当前source resolver与公开详情复用同一原始source可读条件：精确组织entity/account、明确PUBLISHED、PUBLIC、publication context仍匹配、PG clock有效、原始ACL/blocks。不复制公告正文到MemoryEvidence。

角色授权只允许人工管理和公告公开；不授组织认知purpose、模型分析、自动学习、egress或广播。公告内容属于管理员发布声明，不因此称为独立核验事实、CSSA授权或真实运营通知。改版/撤回/过期使旧Evidence失效，并继续复用073同ledger版本绑定、scrub/tombstone与人工读清理。

## 数据和回滚

资源revision和stable绑定在DB guard中检查；preview、消费回执与audit同事务。外部动作与后台调度为零。原057 tombstone会删除sourceType/sourceId，不能在down时从REMOVED行猜原来源；075保留最小announcement evidence control，仅evidence ID→本来源类别（无公告地址或正文），由人工ANNOUNCEMENT evidence INSERT建立并随真实父ledger删除级联。不是第二份Evidence内容账本。

075 down存在任何公告/preview/audit或announcement evidence control（含REMOVED历史）时原子拒绝；仅空新增域、空本来源历史时恢复073精确shape并删除075新增schema。不修改既有PERSON、四来源或073归档。公开projection当前Organization publication context需与明确publish时的真实native snapshot相同，组织资料/公开状态变化后要重新人工草稿/预览/发布，不静默恢复旧公开批准。

## 验收计划

UX-CHECK-02/03/05/08/09/10/11/13/16。测试draft→具体revision预览→明确publish→匿名详情→第五source→编辑退回draft/撤回/到期；owner/admin和其他角色、跨Org/Person/session、旧preview、原生当前源/版本/期限、重复请求/并发/重启、真实PG等待自然过期、审计不复制正文；migration fresh075/current-data up/down/reapply与旧PERSON+四来源数据原样保留、组织历史拒绝down。Go定向后root默认全Go075与现有Flutter回归另核。无公告UI/真机/AT，未运行如实记录；production/真实合作方/模型/通知运营/Closed Pilot原门槛不改变。

## 实际命令与冻结帧

`python work/v5-age053/verify-announcements.py --round announcement-native4`：四目标包 `agentorganizationevidence / organizationannouncement / postgres / httpapi` 的 `go test -run '^(TestOrganizationEvidence|TestOrganizationAnnouncement)' -count=1 -json`、`go vet`、`go build`均exit0；230 TestPASS、0FAIL/SKIP。18 owned Go/SQL与670观察源stable；isolated fresh075、pre075旧存储样本up/down/reapply完整行/catalog保留与ownedDROP成功。前三次native/失败帧保留，最终manifest另归档，不修改旧worker-final1。root对当前075独立全Go验收另记，不以定向代替全仓。

七条真实路由：管理GET collection/detail、PUT detail、POST detail/publication-preview、POST detail/publish、POST detail/withdraw；公共GET `/v1/organizations/{organizationID}/announcements/{announcementID}`。管理路径统一 `/v1/me/organizations/{organizationID}/announcements`，strict JSON与no-store，具体ID来自原生UUID。PUBLIC audience指公开内容许可；本轮没有公开搜索/feed/广播/通知/新UI或模型权限入口。

## 根复核后的公共读取修补（当前覆盖旧final2产品帧）

原230帧并未覆盖非UTC池连接和“第二次认证已完成、最终公共SELECT还在等连接”的边界。root只读独立复核发现两处缺陷，本轮在同一scope修复，不改旧final2或已批准公告context内容。公开context的非时间键仍精确JSONB匹配，旧updatedAt字符串解析为timestamptz与实际组织updated_at比较同一个瞬间；读取连接时区不同不改变批准的含义，不重新写或静默失效旧UTC批准。

Bearer公共读取携实际SessionDigest。首次公共SELECT返回内部不可序列化session snapshot，只包含真实session ID/创建时刻/absolute expiry/auth method和实际account ID/type/status/updated_at，时间字段按UTC瞬间规范；不含令牌或每次认证会延长的idle expiry。最终公开payload SELECT同一SQL/PG stamp重新核sessions digest、当前active Person、revoked/absolute-idle expiry、既有dev-auth边界，并与首读snapshot一致；anonymous仅零viewer/digest/snapshot。第二次Authenticate仍核同一个actor和digest，但不会在最终payload查询后又加一次可能等待的认证。没有新认知purpose或业务授权。

`announcement-native5` 定向native242PASS/0FAIL-SKIP；18owned源稳定、migration旧行/catalog保持、ownedDROP。真实三非UTC连接保持publication_context字节原样；真实registered HTTP最终reader pool MaxConns1被占用、第二次Authenticate完成后，revoke/absolute和idle自然expiry/session-ID ABA/account-updatedAt ABA拒绝正文，healthy200。此帧并行期间global source drift仅root两条CHT测试，不能当最终全API稳定帧；待共同freeze后的新native6与root全Go另记。新证据必须另建final3，不覆盖final1/final2。

**共同冻结后的最终native6**：同一242 TestPASS/0FAIL-SKIP/0packageFail，四目标test/vet/build0；671全部API源及18owned源在前后snapshot一致。完整public/catalog不变，旧PERSON2行与pre075四ORG8行/ID在up/down/reapply保留、catalog精确恢复、ownedDB DROP。命令为 `python work/v5-age053/verify-announcements.py --round announcement-native6`；实际process记录与源码hash在新worker-final3。根默认全Go075及发布门槛另核，worker不自行改DONE。最终pool等待已运行；未另执行最终SELECT的table-lock专属场景，不把它称已测。
