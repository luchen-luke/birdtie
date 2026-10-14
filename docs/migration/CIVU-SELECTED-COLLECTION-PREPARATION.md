# Civu 集合选择性迁移：本地准备合同

2026-10-14 杭州接续：用户已明确授权原远端真实数据适配及唯一Birdtie入口。最新真实保全/恢复见 [杭州执行交接](HANGZHOU-BIRDTIE-EXECUTION-2026-10-14.md)。下面prepare工具仍仅为无网络候选准备，不因已有真实源而冒称extractor/apply。接下来独立补实际源schema抽取、稳定source_system/entity/source_id映射、未映射原因和幂等apply，保原deleted、私人备注、媒体及多地点/串文档案；没有源城市/精度/权利时不猜填。原本机合成检查保留，不与真实备份结果相加。

2026-10-14 批 F 的准备增量，来源 NOW027/PG25、BT-V4-MIG-001 的历史映射。原 MIG 历史 DONE、AIR005 独立依赖、真实备份与发布门槛保留。本工具不领取或执行生产迁移，未访问真实数据库或媒体。

`scripts/prepare_civu_collections.py` 接收明确选择、规范化后的 JSON 文件，仅生成新的本地候选文件。没有数据库连接、HTTP、SQL、apply、资源扫描、Session 导入或关系写入。它不会解析旧仓的整个备份，也不会把旧公开状态、allow_copy 或审核状态当作 Birdtie 发布许可。旧备份应在既有保全链中保存；该工具不代替备份。

当前 Birdtie 原个人集合 API 支持 Journey/ThemeMap，ThemeMap 的实际表由 `124_personal_collections.sql` 建立。候选沿当前 Create 输入闭集：`operationId, kind, cityId, title, summary, entries`，entry 为 `placeId, note, locationVisibility`。每项 note 为空、精度 hidden；不搬作者/协作者私密备注、媒体 URL、位置坐标、公开版本或成员权限。源标题/说明只进入本地待确认候选，未发送给服务、模型或他人。目标新集合 ID 尚未生成，不能把候选操作 ID 当作目标实体 ID。

输入顶层闭集为 schemaVersion（`birdtie-civu-selected-collections-v1`）、sourceInstanceTag、sourceBackupSha256、sourceSchemaSha256、accountMappings、placeMappings、collections。两个 SHA 字段是调用者提供的保全标识，不是工具已核验真实备份的证明；输出仍记录 sourcePreservation NOT_RUN。sourceInstanceTag 是不含地址/凭据的稳定资源标签。

accountMappings 是原用户 UUID→已明确选择的目标个人 UUID，不能按邮箱自动合并；placeMappings 是原地点 UUID→`{id, cityId}`。这些映射只是准备输入，当前身份、地点读取、来源权利仍需真实阶段核验。缺 owner/place 映射、跨城、已移除/归档、合并为同一目标地点均明确 deferred，不生成假 ready 数据。

collection 的闭集为 kind（journey/theme_map）、id、ownerId、revision、status（active/removed/archived）、visibility（private/draft/public/team）、cityId、title、summary、entries。entry 只含 placeId、position，排序按原 position，不要求原位置连续。原 Journey 的自由 city 文本、时间、成员、清单、Section、creator_note 和媒体不能默默压入该合同，应保留原档案，另经明确语义/权利确认再转换。

最多 1000 个明确选择的集合；每集合 1–50 个地点，沿当前原领域长度限制。输入最多 2 MiB；重复 JSON key、重复来源/位置/地点、未知字段、非 UTF-8/无效 Unicode、错误版本或类型均拒绝。稳定原操作提案由 sourceInstanceTag/kind/sourceID 生成；源版本或文字变动改变输入 SHA，保持原操作提案，不伪造新操作恢复。真正授权执行时必须按当前具体版本重新预览，不允许用准备文件直接重放旧提案。

运行时显式传 --selected 和 --output。输出必须不存在；同名旧产物和输入文件均保留。标准输出仅给计数、固定错误分类和文件 SHA，不含原用户/实体 ID 或正文。详细候选文件含所选的私人材料，沿原受限保全目录保存，不当作公开报告。实际 owner 权利、真实源保全、当前 target reader、导入和媒体迁移均 NOT_RUN。

合成隔离检查使用 `scripts/test_prepare_civu_collections.py`；真实资源未提供时，合成 SHA 不得表述为真实备份通过。下一真实步骤仍为：保全源 DB/媒体并核验→新隔离库恢复→当前 owner/Place 与所选语义核验→具体版本授权→原 writer/最小迁移 writer 一次效果及原回执→新 ID 映射和回滚对账。真实生产备份、导入、签名覆盖升级、切流不由本工具启动。
