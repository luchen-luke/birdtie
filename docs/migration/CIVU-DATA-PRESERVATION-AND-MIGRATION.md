# Civu 数据保全与 Birdtie 选择性迁移

日期：2026-10-08（Asia/Shanghai）  
状态：本地审计与安全脚本准备完成；生产数据保全、隔离恢复与迁移演练未执行。

## 执行边界

- Birdtie 身份真源为 [Agent 身份与归属模型](../architecture/AGENT-IDENTITY-AND-OWNERSHIP-MODEL.md)：真人账号与 Personal Agent 严格一对一；组织是独立 principal，经 Organization Membership 授权，并有独立 Organization Agent。城市只有平台管理的 CityContext。
- Birdtie 当前目标 schema 为 PostgreSQL 15+，本地 Compose 使用 PostgreSQL 16；本地数据库名 `birdtie`、卷 `birdtie_pgdata`，只绑定 loopback。生产参数不能从开发 Compose 推断。
- Civu 当前 checkout（只读参考 `D:\Program\Civu\Civu-server`）使用 Go + pgx/PostgreSQL；迁移记录由 `civu_schema_migrations` 管理，当前源码包含 141 个 migration 文件。`DATABASE_URL` 默认值只代表本地缺省值，不证明生产实例版本或名称。
- Civu 源配置项名称：`DATABASE_URL`、`MEDIA_DIR`、`MEDIA_PUBLIC_BASE_URL`、`OSS_BUCKET`、`OSS_ACCESS_KEY_ID`、`OSS_ACCESS_KEY_SECRET`、`OSS_REGION_ENDPOINT`、`OSS_UPLOAD_ENDPOINT`、`OSS_CDN_BASE_URL`。本地只发现受限的 `api/.env.prv` 路径（未读取其值）；模板中的 SSH 地址/端口仅是历史配置，不证明实例仍存在、可登录、数据库可达或值仍有效。不得从模板复原真实凭证。
- 当前主机未找到 `ssh`、`pg_dump`、`pg_restore`、`psql`、`docker` 或阿里云 CLI；到历史模板 SSH 主机 22 端口探测失败。ECS、云盘、释放期限、容器/卷、数据库名/版本、媒体数据路径、OSS/COS bucket 与对象数量均 **未核实**。无快照的背景截图不是数据保留证据。
- 不重装、释放、删除实例/磁盘/备份，不清库。真实备份完成且隔离恢复核验通过前，旧资源必须继续保留。

## 现在需要在阿里云控制台完成的最少步骤

1. 进入 ECS 实例列表与回收站/到期资源页面，核实实例 ID、地域、状态、到期/释放时间及是否已经进入释放流程。若即将释放，先按控制台可选的最短续费/保留期限恢复保留；购买前记录当前官方报价、资源规格和期限。本次没有控制台账单访问，不能给出费用或声称资源可续。
2. 在实例详情核对系统盘和每块数据盘的 ID、容量、挂载点、自动释放属性、状态与最近快照。对两盘分别创建崩溃一致性快照；数据库运行期间此项只作底层兜底，不能替代 PostgreSQL 逻辑备份或一致性快照组。确认快照可见后，再执行数据库/媒体备份。
3. 查实例关联的安全组、SSH 密钥/登录方式、快照策略、容器清单与挂载卷；经控制台批准临时恢复 SSH 或用既有运维通道。不要把密码、密钥贴进聊天、命令行参数、脚本、日志或仓库。
4. 登录后只读核验 `docker ps -a`、`docker inspect`（只抽取 image/tag、mount source/destination、volume 名，先过滤环境变量）、`df -h`、`findmnt`、`psql -X -Atc 'select version(), current_database()'`，并从受限环境清点 `MEDIA_DIR`/OSS 配置项是否存在及其脱敏 endpoint。切勿输出 `docker inspect .Config.Env`、完整 DSN 或任何记录内容。
5. 使用下方 `scripts/backup_civu_postgres.sh`，凭证通过 `PGSERVICE`/受限 `.pgpass` 或 `DATABASE_URL` 环境变量从权限受限 shell 注入；不要在命令参数提供 URL。备份写到加密本地盘后再复制到 ECS 外的加密目的地。另行对 `MEDIA_DIR` 做文件级清单、SHA-256 与加密归档。若对象存储已启用，先启用阿里云 OSS Inventory（推荐每日 CSV/Parquet、加密、输出桶独立），或使用具备只读 list/head 权限的 `ossutil` 生成对象清单；只在加密受限目录保留完整 key 清单，给 Birdtie 报告仅写 prefix、数量、字节数和脱敏 key 哈希。按版本/对象数、大小、供应商支持的 checksum/ETag 核对后，用 server-side copy/跨区域复制写入独立 bucket/账号并抽样读回。不得把 ETag 一律当 MD5（分片对象不成立）。SQL 备份不包含媒体。
6. 对隔离 PostgreSQL 恢复到全新临时库，核对核心表数量、用户到内容关系、迁移台账和随机媒体可读；不得直接恢复到生产库。保存仅统计与脱敏标识的报告。

**若无法恢复远程访问：**本地继续准备，但不能称真实数据已备份。通过控制台恢复到受控的短期 SSH 或可下载备份通道，最低获取：系统盘/数据盘快照 ID、PostgreSQL `version()`/数据库名、`pg_database_size`、`civu_schema_migrations` 最大版本、媒体目录文件总数/总字节、OSS 配置项存在性及 bucket 区域/对象数量。然后补做真实备份与隔离恢复。旧服务器数据包含用户个人信息和媒体，备份需加密、最小访问、记录保留期限；恢复验证前禁止释放。

## 备份脚本

`scripts/backup_civu_postgres.sh` 与 `.ps1` 仅支持自愿执行的 PostgreSQL custom-format 逻辑备份，不连接生产、不自动读取 `.env`、不把密码放命令参数、不改数据库。它会检查 PostgreSQL 客户端、数据库版本/数据库大小、目标可用空间、拒绝覆写已有目标，检查 `pg_dump`/`pg_restore --list` 退出码，输出带哈希的 manifest。Linux 文件权限限制为仅 owner；Windows 文件 ACL 需由操作者提前限定为备份服务身份。运行前仍需操作者确认已授权目标 DB、加密卷、备份目录位于 ECS 外；脚本本身不加密，目的地必须为已加密卷或先经组织认可的加密层保护。凭证优先走 `PGSERVICE` 配合权限受限 `.pgpass`，或由受限会话交互提示；不要把数据库 URL 放脚本参数或命令历史。

```sh
umask 077
export PGSERVICE=civu-production-backup  # service/password files must be owner-only
export CIVU_BACKUP_DIR=/encrypted/offsite/civu-$(date -u +%Y%m%dT%H%M%SZ)
scripts/backup_civu_postgres.sh
```

支持 PostgreSQL 的 schema、业务数据、视图、扩展定义、函数/触发器、序列、ACL 与 ownership 元数据；不会做恢复，也不会包含外置媒体/OSS 对象。隔离恢复命令需使用单独空目标库及限权角色，并由 DBA 按线上 `server_version_num` 选择兼容的 `pg_restore`；脚本不会提供可能连错生产的恢复快捷方式。

## Civu → Birdtie 映射（按实际 schema）

来源定位：`D:\Program\Civu\Civu-server\api\internal\store\migrations\001_init.sql`、`002_notes.sql`、`003_journey_teams.sql`、`004_follows_media.sql`、`005_regions_places.sql`、`007_notes_extend.sql`、`032_creator_theme_maps.sql`；Birdtie 目标见 `apps/api/migrations/001_foundation.sql`、`003_sessions_and_profile_consent.sql`、`006_city_graph_content.sql`、`015_saved_items.sql`、`019_agent_identity_organizations.sql`。此表是基于仓库 schema 的初步映射，不代表真实数据已探查或已迁移。

| Civu 源 | Birdtie 目标/处置 | 保留与风险 |
|---|---|---|
| `users`、`travel_profiles`、`auth_identities` | `accounts(account_type=person)`、`user_profiles`、经人工验证后建立 `account_auth_identities`，并各建一个 `agents(agent_type=personal)` | 保存旧 user UUID → 新 account UUID、旧 auth identity → 新 verified issuer/subject 的隔离映射；不导入 `password_hash`，哈希算法/参数未核验，不因相同邮箱合并。没有重新验证身份时建待认领/冻结记录，不签发登录 session。 |
| `places`、`regions`、`place_provider_refs` | `cities`/`places`/`place_external_refs`，仅在来源权利、城市、坐标系、来源与新 schema 合格后选择性导入 | 原 Place ID 写迁移映射表；地点目录是平台/供应商资产，不是个人内容，默认排除批量搬迁；坐标需确认 WGS84/GCJ 与精度。 |
| `user_saved_places`、`note_saves`、`theme_map_saves` | 经 owner 映射后可映射至 `saved_items(place_id)`；Moment/Journey/ThemeMap 无一一等价目标则保留旧库只读或等待产品语义决策 | 只迁移主动用户确属个人收藏，记录原 ID/时间；不可据“已收藏”授予目标对象读取权限。 |
| `journeys`、`journey_members`、`journey_arrangements`、`journey_notes`、`journey_media`、`journey_publications` | `journeys`/`journey_stops` 仅能在目的地、时间、成员授权及位置可见性映射规则确定后迁移；行程清单/私密备注尚无 Birdtie 等价表，留只读档案或未来经用户确认转换 | 映射旅程和成员旧 ID，保留成员角色/状态、时间、顺序、归属。私人旅程转 `visibility=private,status=draft`；公开发布需 owner 重新确认，不能自动发布。 |
| `theme_maps`、`theme_map_entries` | 没有当前同构 Birdtie 表；保留旧内容只读导出，不伪装成 `journeys` 或公共 Place 目录 | 保留 owner、entry 顺序、保存、发布/审核状态与旧 ID 对照；未来转换需用户确认。 |
| `notes`（Civu Moment/Post）、`note_places`、journey/activity links、likes/comments | `moments` 与地点/活动/旅程 link 只有在语义、城市、owner、可见范围与作者确认重建后迁移；互动关系当前没有完整等价映射则保留旧库 | 私人/旅程团队内容一律 private/draft；历史 public 也不直接 `published`，要求 owner 确认、审核和新可见性规则。评论/点赞不伪造为新 Connections 或消息。 |
| `media_assets`、`note_media`、`journey_media` + 本地文件/OSS 对象 | `media_assets` quarantine/private 状态和新对象键；先复制/校验二进制，再写关联 | 保存 Civu media ID → Birdtie media ID 和 object key 映射、SHA-256/尺寸/MIME；从私有原件重建派生版本前先扫描/脱敏 EXIF；存储失败时不写“ready”。媒体需单独备份与迁移，SQL 不含文件。 |
| `follows`、followers、sessions/refresh tokens | 默认不导入 Birdtie ties、consent 或 sessions；旧认证在过渡期走独立兼容桥接 | 原授权与 Birdtie Connection/Tie 的语义不同；用户重新验证并显式同意前不建立关系。 |
| `note_likes`、计数、浏览与推荐信号 | 不迁移到权限/Agent memory；统计是否需只读归档需单独决定 | 不作为公开性、排序权利或 Agent 事实的来源。 |
| `orgs`/journey teams | 不自动创建 Birdtie organization principal、Membership 或 Organization Agent | 仅组织主体权属、法人/社团授权、真人管理员身份和成员角色逐项确认后，另行创建 Organization + Membership OWNER/ADMIN/MODERATOR/MEMBER + 唯一 Organization Agent。 |

### 转换程序约束与尚缺项

- 实际源、目标均为 PostgreSQL；无需 MySQL→PostgreSQL SQL 转译。应使用 pgx 以只读 Civu 事务分页抽取 → Birdtie staging 表 → 校验报告 → 经授权后幂等 apply；不 `psql -f` 导入旧 SQL，不复制旧 migration。
- 稳定键为 `(source_system='civu', source_entity, source_id)`，Birdtie 侧隔离迁移映射表保存目标 ID、源 revision/更新时间、数据哈希、执行批次与状态。按稳定 source ID upsert，外键依赖拓扑先 accounts/places/media 后 contents/links；每批可事务回滚，批次失败报告不含 PII。
- 每种实体先产出 dry-run 计数与脱敏错误分类；实际 schema 可读、哈希兼容、媒体读取、隐私映射、重复执行和隔离恢复未验证前不启用 apply。
- 登录安全：必须盘点 `password_hash` 使用的算法、成本参数、盐格式、pepper/服务器密钥依赖及近期重置策略；不把哈希直接移植。第三方 `provider/subject` 只有原 provider 证明/用户重新认证后才能映射；同一邮箱不等于同一人。
- 生产切换前停写仅在明确窗口中执行：冻结 Civu 写入/上传，做最终增量导出，核对 source high-water marks、各表数量、外键/孤儿、哈希和抽样媒体；以用户/作者授权检查通过为切流条件。回滚条件包括账户认领失败超过阈值、权限泄漏、内容/媒体核对差异、错误率或延迟越限；回滚时恢复旧 API 单写和 DNS/路由，保留 Birdtie 失败期审计与映射，不双写、不清除任何一侧数据。具体阈值与停写时长须凭真实数据量测试后由负责人批准。

## 当前验证结论

- schema 映射：**PARTIAL**（依据本地迁移文件初绘；无生产 schema introspection/数据量，未生成 ETL apply 程序）。
- 真实数据备份：**BLOCKED**（无 SSH/云 API、客户端工具/连接；未备份任何真实数据）。
- 隔离恢复：**BLOCKED/NOT RUN**。
- 选择性迁移：**仅方案**，没有执行 dry-run、导入或隔离样本验证；没有迁移用户/媒体。
- 实际实例状态/释放期限/费用：**BLOCKED**，需云控制台核验。
