# 杭州服务器升级 Birdtie 执行交接

2026-10-14，Asia/Shanghai。用户已续费杭州服务器，并授权 Birdtie 连接远端、适配导入旧真实数据、评估降低配置与存储。用户随后明确：直接将原 Civu 在线应用升级为 Birdtie，不保留两套在线产品，不使用 `birdtie-test.civuqiyou.com`。

目标为原公网 HTTPS 入口上的唯一 Birdtie 服务和真实迁移数据。当前原公网 `https://47.110.154.133/healthz` 已实际返回 200，TLS 校验通过，可以继续用这个入口，不必创建长测试域名。域名品牌调整另按真实域名所有权/DNS配置办理，不凭空创造 Birdtie 域名。旧库、媒体和程序作为受限迁移/回滚资产保留，不能将“停用旧在线产品”解释为删除原数据。

## 已完成的真实工作

- 使用本机既有 `C:\Users\chens\.ssh\civu-prod-hz-admin.pem`、已知主机文件 `known_hosts_civu` 和 `ecs-user` 成功登录当前杭州节点。StrictHostKeyChecking=yes，没有修改信任或输出密钥。旧 `.env.prv` 指向更早节点，不能再作为当前目标依据。
- 当前 Linux x86_64，2 CPU、3499MiB RAM；只读检查时可用内存约269MiB，8GiB swap已使用约3813MiB，load约6.2。系统盘60GB，文件系统已用约14GB；数据盘300GB，文件系统已用约41GB。
- 源库为本机 PostgreSQL 16.15，数据库 `civu` 55,811,095字节；源迁移台账151条。实际数量：20 users、5511 places、103 notes、407 media_assets、34 journeys、3 theme_maps。它们是数据库记录总数，不是已确认真实可迁移内容的数量。
- 数据盘主要目录：Pelias 32,089,522,176字节；Civu目录2,446,442,496字节，其中媒体1,640,013,824字节、PG目录242,028,544字节；另有swap。`/opt/civu/backups`约4.29GB。
- Elasticsearch容器配置内存上限6GiB，重启计数109，与当前宿主内存及交换压力冲突。Birdtie自己的PostgreSQL地点检索不依赖Pelias/Elasticsearch/libpostal。
- 云控制台已有杭州实例页面，但浏览器连接读取两次超时；尚未取得云盘类型、计费方式、续费订单、退款与变配报价，未下单/降配/释放资源。
- 旧源数据库完成只读 `pg_dump -Fc`，直接经过SSH流入本机AES-256-GCM加密备份，没有把明文dump落盘。完整数据备份3,113,325字节。
- 在本任务创建的独立Docker容器 `birdtie-civu-restore-20261014` 中实际恢复成功；容器network=none、无公开端口、PG数据在临时内存盘。恢复库 `civu_restore` 的上述六类数量及151迁移记录与源一致；pg_restore exit0、无错误。不是只跑pg_restore目录列表。
- 完整媒体目录通过只读tar经SSH流入本机AES-256-GCM备份，1,638,819,840字节；实际解密/认证、原始和密文SHA、归档读取均通过，837个归档成员。没有把私人明文媒体落盘。逐对象数据库引用/二进制可读性及跨库媒体一致性尚待ETL核验，不宣称媒体迁移完成。

### 备份文件和恢复限制

目录：`C:\Users\chens\Documents\Codex\2026-10-14\birdtie-hangzhou`。

- 数据库：`civu-database-2026-10-14T15-32-14-538Z.aesgcm`，密文SHA256 `5dbc323ae7f16df233dea5a412d30c6f563632150e1c639443c4a3997ec5838f`。
- 媒体：`civu-media-2026-10-14T15-33-45-382Z.aesgcm`，密文SHA256 `4c4b7cd1e4b45d9cdc3a7aee3cc31e136702eb44b3c7b0bc5f91de21f365bc0d`。
- 各自的 `.receipt.json`、`.verification.json` 和 `.key.dpapi` 必须一同保留。密钥由Windows DPAPI CurrentUser保护，当前账户已实际解密验证；另一设备独立恢复与外部密钥托管尚未完成，不能仅复制密文后声称任意设备可恢复。
- `backup-encrypted.mjs` 和 `verify-backup.mjs` 是本次辅助工具原件，不替代Birdtie迁移器或正式备份运维。原服务器没有新增/修改业务数据；本机隔离恢复不连接当前Birdtie QA库。

## 当前真实数据差额

隔离源schema只读结果：20 users目前active/public、21 auth_identities（phone18、email3 verified）、189 user_saved_places。5511 Place中5484无region、19 merged；需geo_areas与城市别名/坐标/来源核对，不能根据regions三条中国记录给全部地点分城。旧Place来源包含高德及Mapbox live_only，不能因数据库有记录就宣称获得新产品永久保留或封面权利。

103 notes中20已soft-deleted，83有效。有效内容34无Place、47单Place、2多Place；45标题为空、27在moment_threads，55有city destination，另28没有；不能用created_at代替发生时间、第一处Place猜城市、自动截断多地点或拆串文。媒体有21pending、45空MIME、3无owner，必须隔离而不是补造ready/owner。保留删除tombstone，私人图/EXIF不自动公开。

## 接续实施顺序

1. **原开发根代理先冻结当前本机合包检查点。**核对最新源码、API/APK/Web/schema及worker状态，保留全部未提交改动和旧证据。在同一原队列中接续远端/迁移增量，不另建替代功能队列；协调地图、媒体真源与迁移写范围，不能并发改相同DTO/DDL。
2. **补可重复的Linux部署、迁移台账与诊断。**部署独立Birdtie数据库/角色/目录及可回退Linux x86_64 API，原Civu库仅作只读迁移源。当前main启动会写提醒/退休媒体，不能启动Birdtie去“试连”旧Civu库。修复示例Compose expose与主机Nginx127.0.0.1代理不通、图片route 1m限制不匹配，以及readyz只有Ping的问题；本服务器现有Caddy优先复用，避免无必要再加Nginx。
3. **实现源抽取、staging/dry-run、稳定映射和幂等apply。**稳定键为source_system/entity/source_id，保留源版本/哈希、目标ID、原始可见性、批次与未映射原因。先City词典和20个永久owner映射，再获准Place、189收藏及集合；Moment先进入本人私人恢复草稿或只读旧档案，保留多地点/串文/未知时间，20删除记录不复活。直接灌旧SQL或按同名/同邮箱合并禁止。
4. **补旧用户登录续接。**读取旧auth/hash格式与停用状态，实现合法旧身份验证→唯一Birdtie Account/Personal Agent绑定；旧session/refresh token不能直接作为新授权。远端关闭开发固定码，不为绕过loopback/模型启动边界打开devPhone。当前真实Now文本启动仍有本机限制，生产接线需实际补齐。
5. **逐对象迁媒体。**原件、私有/公开派生、文件/语音与数据库关联对账，SHA/大小/MIME/owner/来源权利复核；缺失、pending与未知owner隔离，保留原备份。不把历史公开或照片默认扩大到新的受众或地点封面。
6. **同一当前包连接真实远端。**以原HTTPS公网入口为最终API origin，发布实际Linux服务与对应APK/Web；取消对本机18113及adb reverse的依赖。先在受控内部路径/通道核验目标，之后短维护窗口停旧写入，做最终增量并切原入口至Birdtie。核验认证、公开地点/地图、本人内容/收藏、媒体及必要旧链接；旧服务只作为回滚资产，不继续运行第二套在线产品。
7. **切流后退役重型搜索。**停止旧Civu程序及其后台写入，停止并禁止Pelias API/placeholder/Elasticsearch/libpostal自动重启；保留PostgreSQL、Caddy与证书续期。保留索引来源/可重建清单，不把停服务与删除源资产混成一个动作。
8. **最后执行成本变更。**当前暂留2核/4GB和60GB系统盘；目标可为新增约50GB数据盘，仅迁业务库、媒体及必要TLS状态。需云控制台核对盘型、现计费、实际新盘/快照费用与回滚时间，再给用户具体确认。300GB数据盘不能原地缩，需新盘迁入核对后，旧盘先保留回滚窗口，再经明确确认释放；没有用户确认不得下单或不可恢复删除。未来增加用户再按实际负载扩容。

Cloud规则来源：

- ECS变配： https://help.aliyun.com/zh/ecs/user-guide/change-instance-types/
- ECS云盘不能原地缩： https://help.aliyun.com/zh/ecs/user-guide/shrink-a-disk
- RDS PostgreSQL部分云盘支持缩容： https://help.aliyun.com/zh/rds/apsaradb-rds-for-postgresql/reduce-the-storage-capacity-of-an-apsaradb-rds-for-postgresql-instance-that-uses-essds/

本节点实查为自管PG与300GB挂载盘，不能套用RDS缩容步骤。删除文件只减少已用空间，不自动降低配置容量或账单。

## 代码与原资料

正式仓仅 `D:\Project\birdtie`；Civu代码 `D:\Program\Civu`只读。原执行根代理登记精确scope后，将本交接复制到 `docs/migration/HANGZHOU-BIRDTIE-EXECUTION-2026-10-14.md`，记录原始路径，并增量更新CIVU-REUSE-MATRIX、现迁移/部署文档中的过时事实。保留本次真实备份/恢复receipt，不与旧合成数据检查合并。

参考 `docs/migration/CIVU-DATA-PRESERVATION-AND-MIGRATION.md`、`CIVU-TO-BIRDTIE-CUTOVER.md`、`CIVU-SELECTED-COLLECTION-PREPARATION.md`、`docs/architecture/CIVU-REUSE-MATRIX.md`。现prepare_civu_collections.py只生成本地候选，没有DB extractor/apply，不可称真实迁移工具。现backup脚本的DATABASE_URL未被libpq调用采用，不能直接照旧运行；用明确PGSERVICE/PG*和源身份校验修复。

执行以相关直接单元/必要数据库迁移验证、合批编译和真实远端流程为准；不重复全仓验证，不录屏，正常字号。分别报告代码完成、数据迁移对账、实际公网/手机运行与未验证项。当前用户授权受控远端接入/升级，不把内部远端测试当成Closed Pilot或Consumer Beta发布验收。
