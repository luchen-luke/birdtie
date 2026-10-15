# Civu 统一动态／串文编辑器来源审计

范围：只读 `D:/Program/Civu/Civu-client` 指定源码与必要调用链，补读本机 `Civu-server` 的作者／素材事务校验，比较 Birdtie 当前适配边界。未修改 Civu、未 fetch／checkout／pull／push、未安装构建或运行验收、未读取私有账号或业务内容。网络只执行 Git 远端引用查询和 GitHub compare 只读 API。此前 `work/readback_device15_thread_video.py` 仍为 PLAN，未执行远端。

## 来源版本与 GitHub 提交证据

已先读 `D:/Program/Civu/AGENTS.md`、`Civu-client/AGENTS.md`；涉及服务端时读 `Civu-server/AGENTS.md`。本任务是用户明确授权的来源只读审计，没有据旧仓规则派生新的修改／构建／推送动作。

| 项目 | 实际只读结果 |
| --- | --- |
| Civu-client 本地分支 | `master` |
| 本地 HEAD | `1bbf0613b513001447f326225c944c1a1fbd8e8f` |
| 提交日期 | `2026-09-29T02:37:18+08:00` |
| 工作树 | `git status --porcelain=v1 --untracked-files=normal` 结果为空，tracked 修改 0／untracked 0 |
| GitHub 实时远端 | `git ls-remote --symref origin HEAD refs/heads/master`：默认 `refs/heads/master`，HEAD 与 master 都是同一 `1bbf0613…` |
| GitHub compare | `gh api repos/luchen-luke/Civu-client/compare/1bbf0613b513001447f326225c944c1a1fbd8e8f...master`：`status=identical`、ahead=0、behind=0、total_commits=0；base／merge_base 均为本地 HEAD |
| 编辑器工作文件 SHA256 | `publish_moment_screen.dart`：`5d0e4beb98c635aaa6c68b59ed627318bbb5dccfd9c792fc40982ff53261621b` |
| 草稿模型工作文件 SHA256 | `moment_publish_draft.dart`：`bb9e76ce5bef1614a8fe6e193cdd4e5c6b7017f5d1fe726aea5eb00b8c808773` |

因此这里分析的是已在 GitHub master 上的源码能力，未发现“仅本地未提交才有的统一编辑器”。该证据不证明 Civu 运行服务／已安装客户端版本与源码相同，也不证明所有旧分支已合并；本次没有运行验证。

源码永久定位：

- [编辑器，固定提交](https://github.com/luchen-luke/Civu-client/blob/1bbf0613b513001447f326225c944c1a1fbd8e8f/lib/src/screens/publish_moment_screen.dart#L448)
- [草稿模型，固定提交](https://github.com/luchen-luke/Civu-client/blob/1bbf0613b513001447f326225c944c1a1fbd8e8f/lib/src/models/moment_publish_draft.dart#L4)

## 真实能力来源与调用链

以下 client 路径均相对于 `D:/Program/Civu/Civu-client/`；server 路径相对于 `D:/Program/Civu/Civu-server/`。行号为本次读取的本机文件位置。

| 能力 | 精确来源 | 实际行为 |
| --- | --- | --- |
| 单条／串文统一入口 | `lib/src/screens/publish_moment_screen.dart:448`、`:562`、`:1016` | 一个 PublishMomentScreen、一个 `_entries` 数组。初始化先有一条，增加条目后成为多条；不是单条和串文各建一个编辑器。 |
| 每条独立编辑状态 | 同文件 `:491` | `_MomentComposerEntry` 自有 title/body Controller、FocusNode、media List、LinkedHashMap places、destination、themeMap、mapAnchor、转场及路线锚点；编辑时还有原 `noteId`。active getter 只把当前条目映射到共享操作。 |
| 添加／删除 | 同文件 `:840`、`:859` | `_addEntryAfter(index)` 插到任意条目后，最多 20，聚焦新正文；`_removeEntry` 最少留一条，dispose 被删条目 controller 并修正 activeIndex。 |
| 排序／稳定组件身份 | 同文件 `:873`、`:1842`、`:2019` | ReorderableListView 移动同一 entry 对象，记录当前 active 对象并找回其新 index；`ObjectKey(entry)` 避免拖动时字段串条。 |
| 每条媒体独立排序 | 同文件 `:2689`、`:2708`、`:4224` | 删除和 reorder 当前条目的 media，不改变其它条目材料。 |
| 编辑入口传入原作品 | `lib/src/widgets/civu_discover_media_post.dart:445`；`lib/src/screens/content_detail_screen.dart:610`；`lib/src/app_router.dart:219` | 菜单带 `edit=<原note ID>`，extra 为 threadSiblings 或当前单条；router 把 edit 与 `initialThreadItems` 交给同一 PublishMomentScreen。 |
| 恢复整串与原 ID／位置 | 编辑器 `:478`、`:625`、`:658`、`:1041` | extras 与本地 discoveries 先按 ID 合并，再按原 threadId 查成员，按 threadPosition 排序。`_applyMomentToEntry` 写回原 noteId、title/body、media 与地点；保存用原 ID。这里没有重新创建已发布子项。 |
| 媒体与路线元数据补读 | 编辑器 `:764`；`lib/src/data/repositories/domain_repositories.dart:916`、`:945` | 编辑前 GET 原 map-context／photo metadata，依原 noteId 恢复每条素材地点与相邻 leg；未加载完则阻止保存，避免空字段覆盖。 |
| 新草稿兼容旧单条格式 | `lib/src/models/moment_publish_draft.dart:4`、`:65`、`:152` | Draft 同时保留旧顶层字段和 entries；entries 空时 `effectiveEntries` 把旧单条变为一条 entry。entries 序列本身表示顺序，每条有文字／媒体／地点及转场。 |
| 草稿保存／恢复 | 编辑器 `:1234`、`:1243`、`:1090`；`lib/src/services/moment_publish_draft_store.dart:11` | 新建模式 700ms debounce，按 user + journey/global scope 保存，恢复每条；空内容删除。Store 是 SharedPreferences JSON，原生另复制媒体到 applicationSupport 的草稿目录。 |
| 新建发布 | 编辑器 `:3630` → repository `:1553`、`:1660`、`:1826` → `lib/src/api/api_client.dart:428` | 先准备各条素材并分组 media IDs，再一次 POST `/v1/note-threads`，返回有序 items。不是线上循环单条发布冒充串文。1 条同样走这一新建入口。 |
| 线上新建事务 | server `api/internal/httpapi/router.go:285`、`notes_handlers.go:244`；`api/internal/store/notes_thread_postgres.go:11`、`:70` | withAuth＋withIdempotency；同一 Postgres tx 建 thread 与各 note、media/places/legs，带 thread_id/thread_position。归属来自当前鉴权作者。 |
| 原作品更新 | 编辑器 `:3545` → repository `:1962`、`:2114` → API `:446` | 遍历 entry，用 `entry.noteId` PATCH `/v1/notes/{id}`，再额外 PUT thread legs；保留原 note IDs。成功后写回 discovery cache，并发 `CivuMomentChangeBus.updated`。 |
| 媒体上传／已有素材复用 | repository `:4868`、`:4930` 后；API `:483`、`:487`、`:511`、`:582` | `_existingRemoteMedia` 命中时复用已有 media；新图片 prepare 去元数据／受限处理，视频准备、checkpoint 和必要上传；上传端点 `/v1/media/uploads`、`/v1/media/{id}/content` 及分块接口。 |
| 作者和素材权限 | server `api/internal/httpapi/notes_handlers.go:310`、`:334`；`api/internal/store/notes_postgres.go:291`、`:342`、`:1087` | authorID 来自鉴权，UPDATE 锁原 note 后核 author；已存在 media IDs 再按 owner_id 读取，非本人无法借 URL 授权。If-Match 在 handler 中解析；这不等于客户端总是提供强制原版本。 |

## 不能扩大成“Civu 原先已具备”的能力

1. **编辑时增加／删除／排序条目没有开放。** 编辑器 `:1802` 明确 editing 用普通 ListView；仅新建 `:1842` 使用 Reorderable。`:2083` 的 drag/remove 和 `:2236` 的 add 都受 `if (!_editing)` 限制。函数存在不代表已发布编辑入口可调用。
2. **编辑不是整串原子保存。** `:3545` 逐条 updateMoment，`:3602` 最后独立 updateMomentThreadLegs。前一条成功、后一条失败会有部分成功，不能照搬为 Birdtie 的整串原子 UPDATE。
3. **已发布编辑草稿不会保存。** `:1235`、`:1244` 在 `_editing` 时直接 return。旧 DraftEntry 没有原 Moment ID／expectedRevision／thread version／原 current-source proof，也没有跨重启稳定的本地 entry UUID；它只适合旧新建草稿语义。
4. **Civu 仍强制整串至少一个媒体。** 编辑器 `:3462` 和 repository `:1575` 拒绝全纯文字；单条最多 18、整串媒体总数 18，视频需封面。这些限制与 Birdtie 正文 OR 合法媒体、九图 OR 单视频的合同不同。
5. **旧幂等键不能代替 Birdtie 原操作恢复。** API `:433` 每次 createNoteThread 生成时间型 Idempotency-Key；它没有 Birdtie 本地持久 opID＋GET 原事实＋STOP 的同等恢复链。旧发布失败可重试文案不能替代已未知写的恢复。
6. **旧草稿媒体保存不能直接迁用。** Native `moment_draft_media_persistence_native.dart:26` 会复制 raw／data URI 素材到草稿目录；Birdtie 既有禁止 raw／批准进入 journal 的边界应保持。该 copier `:65` 构造新 MomentMediaDraft 时没有保全 capturedDate／photoAssignment；store `moment_publish_draft_store.dart:49` 重建 DraftEntry 时也没传 inboundOrigin／selection／mode 等已有字段。模型支持这些键不代表实际持久化逐字段保真。
7. **extras／本机缓存不是权限证明。** `_ownedEditMoments` 用 extras 和 store 恢复视觉状态，最终写权限仍依赖 server。Birdtie 应从 owned reader 取得当前整组、各 ID/revision、source epoch，再编辑；不能用导入 archive、公开缓存或旧 URL 作为写入许可。

## Birdtie 复用适配与缺失表

这里“复用”指状态组织和交互语义，不复制 Civu 业务接口／许可／数据代码。Birdtie 行号按审计时的当前源码，正在实施的新需求不在本审计中预先算完成。

| 目标 | 已有可复用设施 | 仍需落实的增量 |
| --- | --- | --- |
| 一个 editor，1..20 entries | Civu Entry 对象＋active proxy＋稳定 key；Birdtie `apps/client/lib/src/workspace/moment_thread_api.dart:10` 有稳定 localID，Now 原输入框已有接线 | Birdtie 当前 NowCreationDraft 与 MomentThreadController 分立；统一为一个 entry-list 状态，单条也是 entries[0]，不得切模式重建／丢字。新建 1 条可分派既有单条合同，2..20 分派原子串文；UI 与材料状态不能因此分叉。 |
| 每条独立 title/body/media/locations | Civu Entry 自有全部字段；Birdtie localID＋素材 scope、160/166 locations 已有 | 当前 Birdtie thread entry 的 title 仅取 `originalInput` getter，缺新条目可编辑独立 title；新增统一模型需保存每条全部字段，0..N 位置保留显式清空语义，不能强取第一地点为主城市。 |
| 本地增删排序不串料 | Civu `ObjectKey(entry)`，Birdtie localID 与 per-entry media controller | 以 localID 绑定文字、媒体、地点、stage/localScope；移动改顺序，不改身份；删除只退休该 entry 的 RAM／prepare 选择与批准，不动其它条目。最少可剩 1 条，当前 Birdtie `moment_thread_controller.dart:349` 最少 2，需改统一语义。 |
| 新建／原文编辑同一面板 | Birdtie `map_workspace.dart:274` `_openOwnedMomentInNow` 和 owned thread reader | 原 ID、原 version、原素材、private/current owner reader 接进同一 entry-list。不要创建新 Moment 来假装编辑已发表作品。 |
| 已发表串文结构编辑 | Civu 没有可直接继承的完整实现 | Birdtie 当前 `moment_thread_controller.dart:330`／`:349`／`:367` 也禁止 existingThread 增删排序；后端 `content/moment_thread.go:140`、`:177` 只有 CREATE_PUBLISH／PUBLISH／WITHDRAW。需要明确新增原子编辑／单条转串／原 ID 保留／撤回删除的合同及授权，不能循环 UPDATE_PUBLIC 充当事务。 |
| 发布／更新带媒体 | Birdtie `CaptureMomentMediaMaterialsInTx`（postgres/moment_media.go:333）与 `AttachMomentMediaInTx`（:457）及 155 真转码 | 继续当前 owned stage、逐项 rights、新集合／current source、整体预览封印、single MP4 OR up to9 JPEG 与真实 NOT_RUN/UNKNOWN；已发布更新要明确旧集合退休及新集合保留选择，不能借旧 private import 权利自动公开。 |
| 本机恢复 | Birdtie TypedLocalDraft／MomentThreadPendingStore 已按 origin/owner、scope 保存材料状态 | 分开可编辑 material draft 与不可重发提交事实；恢复原 ID、entry 顺序、文本／title／locations；任何 source/auth 变化使批准退休，普通 token 续期不丢本人素材。不把保存的批准／raw bytes当可重发草稿。 |
| 原操作与部分成功 | Birdtie preview→一次 commit→GET immutable receipt／STOP 已有 | 新编辑合同继续封印整组 ID/order/version/materials/rights，未知时只 GET 原操作，不自动新建 op 重试；同事务失败必须全回滚，旧操作事实保留不随 UI 改写。 |

## 下一步最小实施建议

先冻结统一的 entry 模型与 lifecycle：每条 stable localID、可选 native Moment ID／expectedRevision、独立 title/body/ordered media/locations；整个 editor 持有原 threadID/version 与 owner/source scope。保留 Now 同一输入框，通过 active entry 切换填写，页面中的条目顺序和材料顺序可调整。Civu 可以证明这种“一个模型支持单条和多条”的产品方向有真实来源。

随后按新需求的精确编辑范围补 Birdtie server 合同；尤其已发布结构变更不是旧 Civu／旧 Birdtie 已有的原子操作。实现与必要验证应围绕原 ID 保留、顺序／数量变化、媒体授权、全部 source recheck、原操作恢复和事务回滚，不以搬回旧整页发布 screen 或复用旧 API URL 代替适配。本审计不宣称这些增量已实现或已真机通过。

结束时再次只读检查 Civu-client：工作树仍干净，指定两文件 SHA256 与开头一致。未改变原项目。
