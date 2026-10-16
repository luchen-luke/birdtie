# 合成数据注入边界（DGS-06，2026-10-16）

本批把三个开发 seed 的“请勿用于生产”注释落实为数据库执行守卫，并停用本线程七个曾面向真实环境的历史 fixture 脚本。真实用户的固定验证码 `123456` 和 Civu 接续规则没有改变。本批不调用生产登录，不新增、发布或删除内容。

## 已实施的入口

| 入口 | 当前边界 |
| --- | --- |
| `apps/api/dev-seeds/001_badminton.sql`、`002_functional_mvp.sql`、`003_community_social.sql` | 每个 seed 的 `BEGIN` 后、第一条数据写入前执行相同守卫；不满足条件抛出 SQLSTATE `42501`，原 fixture 内容保留 |
| `scripts/fixture_environment_guard.py` | 无 import 副作用；验证精确数据库名称、标记和显式 opt-in；可生成受限 session SQL；可校验字面量 loopback 独立端口 |
| `automation/verify_community_migrations.ps1` | 只接受本机 Docker context，创建独立随机测试库、设置标记，再在每次 seed 所在 psql session 显式 opt-in；结束按库名、OID、标记确认后销毁该库 |
| `apps/api/internal/postgres/owned_migration_database_test.go` | 保留原独立随机数据库名称及 parent guard；创建后设置相同标记；在 seed 的同一次 pgx Exec 显式授权，完成后 RESET；后续 legacy fixture 可继续设置自己的专用标记 |
| 本线程七个历史生产 fixture 脚本 | 在原模块产生网络、文件或子进程副作用之前无条件拒绝，没有环境变量绕过；原内容和原件备份保留 |

SQL 必须同时核对：

1. `current_database()` 严格匹配 `birdtie_fixture_[a-z0-9_]{1,40}` 或已有 native helper 的 `birdtie_owned_migration_[0-9a-f]{24}`。
2. 当前数据库不是模板、可连接，数据库 owner 等于 `current_user`。
3. 数据库 comment 精确等于 `BIRDTIE_ISOLATED_FIXTURE_V1:<current_database>`。
4. 同一数据库 session 的 `birdtie.fixture_opt_in` 精确等于 `isolated-fixtures-v1`，`birdtie.fixture_database` 精确等于当前库名。

普通 `birdtie`、真实原生库名、仅有 localhost、仅开固定码登录、仅有类似测试名称均不授予 fixture 写入权限。授权数据库管理员仍能主动创建和标记隔离库；这些守卫用于阻止误将 fixture 流程指向真实数据，不声称能够阻止拥有生产数据库管理权限的人故意绕过。

HTTP origin 检查只接受字面量 loopback、HTTP、独立高位端口（排除本地 PG 55432），拒绝公网、别名、凭据、路径、查询和片段。它本身不证明某个 API 进程连接了该测试库；需要继续使用已有 runner 的进程、二进制、DB 归属检查。不能把环境变量自述当作实际连接证明。

## 开发流程

已实际适配的社区验证入口：

```powershell
pwsh -NoProfile -File automation/verify_community_migrations.ps1 -EvidenceDirectory <新证据目录>
```

该入口自行创建、核对和销毁独立库，不需要对普通开发库或真实数据库授权。现存 `ownedMigrationDatabase` 的 native 测试调用保持不变。

其他旧 migration runner 如仍向旧名称的数据库运行三份 seed，将明确拒绝。迁移方式是：保留其原本的本地运行/归属保护，改用上述允许的独立随机库名；在实际创建成功后设置精确标记；在执行 seed 的同一个 psql/pgx session 加入上述两项 SET；清理前重新核对所创建数据库的归属。不要把 `birdtie` 或任意 `birdtie_*` 加入白名单。其余 runner 未逐个改写或运行，不能称为全仓验证已通过。

以下本线程私有工作脚本已停用，备份位于原工作目录的 `work/dgs-injection-originals/`，不把含旧登录资料的原脚本复制到仓库：

- `remote_friend_chat_flow.py`、`remote_friend_chat_flow02.py`
- `remote_media_thread_flow.py`
- `remote_multilocation166.py`、`remote_crosscity166.py`
- `verify_remote_social.py`、`verify_remote_social02.py`

精确路径、修改前后 SHA 和插入位置见 `docs/testing/evidence/dgs-20261016/injection-guards/historical-script-guards01.json`。清理脚本、个人 Agent 偏好 seed 和经过授权的城市候选导入不属于本批 fixture 注入入口，没有按名称中的 seed 一概禁用。

## 实际验证和边界

`docs/testing/evidence/dgs-20261016/injection-guards/direct01/` 保存第一批实际执行的命令、退出码、stdout/stderr、前后源码摘要及三个真实 SQL 拒绝日志：

- 5 个 Python 直接测试通过。
- 七个已加守卫的历史脚本分别按 import、`__main__` 执行，共 14 次均在原网络、目录写入、子进程副作用前拒绝。
- 本机 PG16 的社区 runner 通过未标记库、缺少 opt-in、session 指向错误库三个拒绝检查，且拒绝后库内没有创建公共表。
- 同一 runner 完成迁移 030–032 forward/down/reapply、原 SQL 不变量、三份 seed 和重复 seed 检查；5 个既有测试活动及组织者保持，重复社区 seed 仍为3项。临时库已经按其身份销毁。

随后为了保留既有 native fixture helper，补入严格 `birdtie_owned_migration_<24hex>` 路径，并再次定向运行上述5项 Python 测试通过；此批源码摘要和实际检查记录见 `final-source-unit02.json`。第一批旧 source frame 保留，不冒称其覆盖后续源码。根任务随后实际执行两项直接 native 集成验证：`TestLegacySocialNativeAtomicHistoryCurrentImageAndOriginalRetry` 与 `TestLegacySocialHistoryNativePrivateOwnerInertInboxAndRepeat`，2 PASS、0 FAIL、0 SKIP；回执为 `docs/testing/evidence/dgs-20261016/legacy-native/selected01/receipt.json`，包含当前三份 seed 和 native helper 的源码摘要。

未进行全仓测试、APK/Web 重构建或真机验收。seed/runner 验证阶段没有生产写入；随后经恢复重演及核对，生产只执行下面列明的六个账号状态隔离。

## 已证实测试身份的生产隔离

仅拦截本机脚本不能阻止旧身份直接登录。因此另作了真实库只读核对，计划见 `quarantine-prepare02.json`；随后已完成恢复重演、限定生产隔离及重复执行检查。

- 六个精确已证实 fixture 账号各保留唯一固定码 identity，均无 Civu 用户映射、旧 phone/email binding 或有效 session。
- 现有固定码及 OIDC 对已存在 identity 的非 active 账号拒绝发 session；现有认证和最终写入校验要求 active。唯一 Civu 初次接续激活路径还要求原始 binding、没有 native identity、未接续和原始账号版本一致，不适用于这六个账号。
- 已按该清单仅将这六个账号由 `active` 可恢复隔离为 `suspended`，保留 identity，防止旧手机号被当成首次用户重建。没有关闭真实用户固定码，没有删除账号或内容，也没有泛化阻止未知新用户。
- 必须明确可见性影响：保留的19条来源未明动态中18条属于这些 fixture 账号。账号隔离会使这些18条按既有权限规则不可访问；行仍保留。另1条未知动态及83条 Civu 动态不归属于这些账号，不在此隔离范围。
- 对另1条非 fixture 未知动态又单独做了只读元数据核对：它原本为 `withdrawn/private`，作者账号仍 active，public binding 不可用；公开和本人私有地图候选条件均不成立。虽保留1条 active 地点关系，`apps/api/internal/postgres/map_content.go` 的 `mapContentMomentsSQL` 明确排除 withdrawn 动态，地点关系不能独立将其上图。证据 `unknown-other-boundary01.json` 仅记录动态ID摘要和状态，没有读取正文、媒体或坐标。因此本轮是“18条随fixture账号隔离，另1条原本已撤下且私有、保持不动”，不能写成19条均由本轮隔离。
- 执行前须再次核对精确账号行摘要、身份映射、依赖、保护摘要、生产 DB OID/release/source guard；漂移拒绝。只按 post-apply 精确行摘要恢复原状态，不自动恢复或删除 identity。

只读准备曾因遇到复合 account 外键而保守中止，失败记录保留于 `quarantine-prepare-attempt01-failure.json`；第二次按外键对应 `accounts.id` 的准确列核对成功。账号隔离执行及真实拒绝验收不由这份只读计划证明。

隔离工具为 `scripts/quarantine_birdtie_fixture_accounts.py`，默认 dry-run；生产 apply 必须提供与上述固定 manifest 完全对应、已保存于服务器的恢复重演回执。`quarantine-dryrun01.json` 为真实生产只读预检通过记录。`quarantine-unit02.json` 和 stdout/stderr 记录最终脚本的5项直接测试通过及稳定源码摘要。

`quarantine-restore03.json` 已实际通过既有完整备份的隔离恢复重演：先仅在恢复库重演原先获准的103行清理，以对齐当前数据，再执行 `QUARANTINED → ALREADY_QUARANTINED → STATUS_RESTORED → QUARANTINED`。每次事务对276张表除六个准确账号外的全部行做前后摘要比较，全部一致；446个账号相关触发器的代码摘要和启用状态保持，未关闭触发器。当前六账号没有 active close，因此原118账号状态触发器没有关系退休派生行；若执行前该事实改变，工具拒绝。恢复临时库已按精确OID和标记销毁，原完整备份未改变。回执摘要为 `1715a096d9be66604e37f43d9d313996e545b5d51bbcab34ff79269888b12454`。

恢复01/02失败也保留。两次都停在真实schema/触发器对比阶段，生产0写；02确认数据库结构和数据摘要一致，仅两个长名称FK的比较顺序不同。原因是 PostgreSQL `name` 类型截断了附加事件类型的排序键。最终只把此元数据表达式显式转为 text，并稳定排序；恢复生成的内部trigger名字只规范化该生成部分，每项约束身份、事件、函数代码摘要仍保留校验，没有跳过触发器。

真实公网 challenge 申请仍遵循原固定码通道；账号状态拦截发生在既有 identity 的 verify/session 及权限检查。上述重演和生产执行没有申请 challenge、登录测试账号或新建会话。没有将任意新手机号分类为 fixture，也没有声称任意拥有数据库管理员权限的写入已经被禁止。

生产执行回执 `quarantine-apply01.json` 为 `QUARANTINED`；同一 manifest 和恢复证明再次执行的 `quarantine-apply02.json` 为 `ALREADY_QUARANTINED`，没有新增修改。生产事务全部保留前置环境、行摘要、identity、无有效 session、触发器和其余行保护；只有六个精确 `accounts.status` 从 active 改为 suspended，updated_at、identity、session、内容和关系行不变。

`quarantine-post01.json` 是随后实际只读核对：账号总数29，六个 fixture 均 suspended、六个固定码 identity 全部保留、有效 fixture session 为0；动态总数102，其中83条Civu、18条fixture名下未知、1条其他未知全部保留；账号相关触发器全部启用，没有遗留隔离恢复库；当时 API active/running、schema173。后续API/schema部署由根任务记录，本回执不冒称覆盖随后版本。

真实公网拒绝登录、真实用户登录和手机可见性验收为 `NOT_RUN`；本批依据现有认证源码、真实账号状态和隔离库往返验证确认限定隔离，没有通过在生产再次登录测试账号来制造证据。最终文件摘要与各阶段覆盖见 `final-receipt02.json`；补查非fixture未知内容之前的 `final-receipt.json` 保留为前一检查点，原清理103行的独立报告和回执保持未改。
