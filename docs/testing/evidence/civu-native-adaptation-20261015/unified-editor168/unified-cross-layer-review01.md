# Unified composer 168 cross-layer review

只读审查及独立 scratch 补丁，2026-10-15。依据 `work/unified-composer-wire-contract01.md`。产品实现仍在并行变化，本文件记录实际读到的代码路径，不声称全源冻结或功能验收通过。没有运行 HTTP、数据库、手机、构建或测试；没有修改仓库源代码、队列或共享报告。

## 需落实的确定接线

1. **v5/v6 复合地图 pin 的媒体当前无法读取。** `apps/api/internal/postgres/map_projections.go:163,174` 输出动态 revision、link UUID、binding hash 的复合 sourceVersion。`apps/client/lib/src/workspace/map_layers_api.dart:181,198` 将 pin UUID 拼入 mapID，但 `map_media_controller.dart:149,240,261` 仍把去掉 `moment:` 的全串视为一个 UUID，并将整个 sourceVersion 解析为整数；因此所有多地点 pin 都被 152 集合读取 gate 跳过。旁路 145 原照片分支 `map_media_controller.dart:542` 同样只接受单 UUID mapID。工作补丁 `work/map-multilocation-media01.patch` 给 MapEntity 增加独立 native Moment ID/revision，使用前重新验证完整原 mapID、pin、revision:pin:hash；保留完整 epoch/pin/source 缓存键，两个 copy 方法继续携带字段。复合 fixture 在原 `map_media_controller_test.dart` 中新增一项，未执行。补丁上下文已以 `git apply --check` 只读校验，exit 0，未应用。

2. **成功 EDIT 后的跨面板立即失效。** 读到的 `map_workspace.dart:8990` thread onPublished 回调仅刷新 own Moments 和 base layers；`map_workspace.dart:6349` 公共城市/作者 history 的 identityChanges 没有 thread EDIT 成功来源。已知本人刚移除 member、换图或改正文后，应同步 retire map pixels 并发送 history/source 变更，包含 removedMembers，再读取当前 base/author layers。原媒体单独操作已有 `_onPublicMomentMediaChange` 的 retire/requestMediaRefresh 方式可复用。这是本机即时刷新缺口；公开服务器 reader 仍有当前资格校验，不能称为匿名权限穿透。已通知 root，由宿主负责人接线。

3. **可选标题保留与可编辑性需分开。** `moment_thread_api.dart:115` 的 title 仍仅从 originalInput 读取，entry.copy 未接受 title，panel/controller 没有标题编辑操作。因此旧标题会保留、正文不会自动写成标题，但统一编辑器目前不能修改或清空旧可选标题。若落实原动态完整编辑，应给 entry 独立 title 状态，owned 初始化原值、新条目默认空，input 合并 originalInput 后显式覆盖 title，避免旧字段反盖。已通知 UI worker 和 root；未擅自扩大产品实现。

## 早期问题当前已接续修正

- `postgres/moment_thread.go:681` 公开 root 投影已按原始 `moment == root`，不是 display position 0；edited view 读取 current display rows，并允许 1 项。历史 member 仍不可替代 display order。
- `moment_thread_api.dart` 的原 root==first 和 2 项约束已经按 v1/v2 分支；旧收据仍读取原 `moment_thread_operations.receipt`（`postgres/moment_thread.go:50`），没有用当前成员重造旧结果。
- `moment_thread_panel.dart:529,550,560` 已将 singleton 文字改为普通动态语义，不显示“同一串文”标题，也不以“整串”描述单条撤回。仍应由本轮直接测试与真实 UI 验收确认最终宿主展示，旧 APK16 不能证明新编辑器通过。

## 媒体与私人可见范围

没有发现 `media: null` 把私人导入或退役素材扩大成公开素材的路径：

- `postgres/moment_thread.go:1068` 对已有条目的 null-media 要求当下 published/public；new/restore 不能以 null 自动恢复。
- `moment_media_projection.go:34` 的 captureMomentMediaCarry 从当前 public media Set 读取，只接受本人当前 parent、READY、批准 revision、parent source binding 一致的集合；逐项核 rights、retention、hash，视频另外核当前 payload。它不查询 157/159/167 的私人导入素材表。
- `moment_media_projection.go:78` 的 applyMomentMediaCarry 在同事务重新检查原 owner、新 parent revision、rights 有效期，保留原合法权利与最短期限。更换 Set 的写入会退休旧 attachments，不能借原 stage 的过期状态延长权利。
- 匿名公开内容 `moment_media_projection.go:325` 先读当前 Set，要求 exact Moment revision、Set version、media ID/version，再编码，完成前重读 compareMediaSets。旧公开 URI 不能绕过撤回或替换后的当前集合。私人图片/视频原 reader 仍走各自 owner/current gate。

## 非核心 reader 核对

- `postgres/public_moments.go:59` 详情以实际 MomentID、published/public、当前 author、`birdtie_moment_public_binding` 读取，不改写成 root ID。
- `postgres/moment_history.go:193` author/city history 使用实际 Moment 与当前 public/media binding，没有 root-first 假设。
- `map_projections.go` 每个真实可读 location link 产生自己的 pin；没有隐式连接线或虚构城市坐标。
- `moment_interaction.go:294,304` 对实际 MomentID 在效果前后重核 public binding；退休 member 若已撤回则不再可互动。
- `connections.go:1019` share 的可选标题 fallback 是卡片描述投影（title、正文短摘、通用文案），没有写回 Moment.title；同时要求当前 public binding。接收者可见性重新检查，不借发送时缓存授权。
- `now_creation_draft.dart:188` 普通动态输入只修改 body，保留独立旧 title。

未在上述非核心 reader 发现新的 root-first、退休 member 正文/地点/媒体公开权限绕过。位置与媒体依然独立失效；owner 为显式恢复读取本人 withdrawn 文本是合同允许的用途，不是匿名 reader 权限。

## 补丁审阅与必要验证

- Patch: `work/map-multilocation-media01.patch`。
- 4 文件原 SHA 与候选 SHA：`work/map-multilocation-media01.json`。
- 完整候选文件：`work/map-multilocation-media01/`，不是产品源。
- 仅新增的复合用例可由 root 应用后执行：在 `D:\Project\birdtie\apps\client` 使用现有 Flutter runtime 运行 `flutter test test/map_media_controller_test.dart --plain-name "v5 and v6 compound Moment pins read the first current 152 thumbnail while pin binding, lease and auth retire independently"`。
- 内容：实际合成 JPEG、TypedMapView v5/v6 → MapEntity → MapMediaController、同 Moment 双 pin 的独立缓存、仅首个当前 attachment、单 pin binding 变更、原 lease、退出身份、伪造 typed selector 拒绝；保留旧纯 UUID/整数 revision 的兼容断言。
- 测试、应用、构建和新统一编辑器真机验收均 **NOT_RUN**（由本轮根协调后执行）。

### 根代理后续实际回报

根代理已登记、审查、应用上述 4 文件并格式化；初次原始 SHA 对比因 Git CRLF 不一致未通过，但 normalized text 与候选逐一相等。随后根代理执行新增的唯一复合 JPEG 用例 **PASS / sourceStable**，目标 analysis **0 ERROR / 0 WARNING / 16 INFO**；其记录位于 `work/unified-map-media-client01/analysis01`。这些是根代理执行结果，本代理没有重复运行。此结果只闭合地图媒体缺陷，不代表整个新统一编辑器、构建、部署或真机验收完成。
