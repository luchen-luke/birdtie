# Now 地图六类来源（BT-V4-MAP-001）

2026-10-04 增量合同。依赖原 NOW-001 / PLC-001 / MOM-001；不建立新的实体、任务、许可或数据库表。全局 UX-CHECK-01 至 16 继续有效，本次重点为中文、当前来源、身份切换、稳定详情、移动端滚动和可操作错误。

## 来源和公开边界

| 类型 | 当前原生来源 | 点位 | 原详情 |
|---|---|---|---|
| PLACE | published 且未过期的原 Place 和 City | 明确 wgs84 / point | 原 Place ID |
| ACTIVITY | published、public、未取消/未结束、当前主办方 ACL 的原 Activity | in_person/hybrid 且 confirmed 的公开 Place | 原 Activity ID |
| MOMENT | 本人确认时间不晚于 publishedAt 的 public/published 原 Moment；作者 active，无双向 Block | 明确公开 Place 情境；不是作者位置或到访证明 | 原 Moment ID |
| ORGANIZATION | active/public/verified 原组织及 active Organization principal | 组织明确提交、独立审核 approved 的 point | 原 Organization ID |
| BUSINESS | active/verified 原商家、active principal、当前 verified 经营关系、approved Venue 来源 | 当前公开 Place；多场地时按原 Place ID 选当前范围内一个明确 anchor，绝非总部/平均坐标 | 原 Business ID |
| OPPORTUNITY | 本人有效 Session、原本人激活且未到期 FIND_ACTIVITY 和原规则机会 | 仅仍 PUBLIC 的 Activity 真实 Place | 原 Candidate ID `intentId:activityId` → 原 Activity ID |

前五类进入公共来源；第六类是本人显式开启的私人叠层，默认关闭，组织工作身份与匿名无法读取。它不公开 Intent 标题、作者、规则理由或私人关系；详情重读原 Activity。候选只表示规则结果，不自动报名、邀请、发消息或创建任务。私人 Moment、未审核组织坐标、私人/非 point Place、ONLINE Intent 和精确 Person 位置不会成为 Pin。

## HTTP 和当前性

- `GET /v1/cities/{cityID}/map-layers?west=…&south=…&east=…&north=…`：匿名公开读取；带会话时必须是当前 Person，不会将坏会话降为匿名。
- `GET /v1/me/cities/{cityID}/map-opportunities`：同四个范围参数，严格当前本人。
- 无 body、未知/重复/非法编码参数、组织工作 header 均拒绝。返回闭合 `typed-map-layers-v1`，仅 7 个顶层字段和 7 个 Item 字段；Entity/Detail/Anchor 的固定字段不含私人原文、来源 URL、rightsNote、内部 xmin/Seal/Proof/Session。
- 每种公共类型最多 30；原本人机会最多 50 且必须与同一快照 PUBLIC Activity/Place 相交。`truncated` 提示缩小范围，不用假坐标补足。
- 明确 Account-before-Session 锁序；所有关系/Session 等待后单条 payload SQL 捕获当前源，编码后再次原生捕获核验具体行版本、权限和最短有效期。进程内 HMAC receipt 只保护只读投影，不是机器用途或人类批准。
- 有效期最多两分钟，并取当前 City/Place/Activity/Venue/Intent 的较短期限。撤回、隐藏、过期、Block、账号失效及同值行版本替换在下次原生读取/末复核时拒绝。投影没有永久新鲜承诺。

## 客户端

一个 MapLayersController 同时投影 Pin、轻卡和图层列表，`kind:originalID` 保持稳定。切换图层仅改本地可见性，不改 Task、选择或摄像机；隐藏层不显示轻卡，恢复层仍用原 ID。明确“读取当前地图范围”才刷新；拖动地图无新增请求。

严格 DTO 检查六类引用、公开 point、范围、重复 ID、有限期限、原版本和合法 RFC3339 时区。客户端以请求开始的 Stopwatch 减掉网络耗时，Timer 到期清空源；旧账号/组织/API/client/City frame 及迟到响应不能恢复。借用 client 不关闭。详情复用原 typed router，并在捕获身份/transport/source epoch 的嵌套边界内重读，不把缓存标题或坐标当权限。

该来源不会替代原 Task 或模型运行；ONLINE 查询保留旧 CITY 地图、结果和选择。普通产品文案不显示 opaque UUID、内部证明或精确用户位置。

## 证据范围

独占本地 PostgreSQL 001–082 + 原三个开发 seed 的真实原生/registeredHTTP 目标测试、Flutter DTO/生命周期/移动布局/marker 测试见 `docs/testing/evidence/typed-map-layers-2026-10-04/README.md`。seed/合成账号/Debug 不是真人供给或正式试点。根代理最终整仓 build 和真机须另记录当前冻结帧，不能沿用 NOW-001 历史通过。

Closed Pilot / Consumer Beta：NO。真实供给、真人完整验收、TalkBack、模型、正式部署均不由该代码合同推定完成。


### 本轮当前客户端验收

同身份刷新先保留仍有效源，原期限到期立即清空，即使新读取仍等待；新结果必须完整合法才替换。实际 RED→GREEN 覆盖该边界。独立本人意图入口复用 NOW-002 Card 的当前 selector；常驻 48dp「我的社交意图」与无 Task 首屏 Card/Pulse 有界滚动并存。Card 绑定稳定 listener/getter 和捕获的 auth/client/base/epoch，旧端点 A→B→A 不复活、取消不写入。未引入重复意图 API、账本或 compact 模式。

冻结帧 `work/v4-map001-layers/source-freeze1.json` 含 23 Go/Dart 源。native6 实际22 PASS/0 FAIL-SKIP，client-target8 实际41 PASS，analyze5=0；整仓/构建/真机待根代理独立核证，不以旧 NOW-001 帧替代。

### 同会话账号 ID 替换的补充边界

冻结后只读复核发现：MapWorkspace 仅 token 改变时退休图层，若账号 ID 单独变化而 token 相同，旧本人 PRIVATE 来源仍可显示。实际 Mock RED 为切换后5公开+1私人仍在；同步按当前(token,accountID,org)三元组变化退休完整投影/seed边界后 GREEN。旧缓存与第二次读取的挂起回包同场景覆盖 A→B→A，不复活、不自动重读本人 overlay、默认关闭机会层，保留原选择和Map Element。它是客户端 fail-closed 边界，不伪造会话身份、不扩大原生授权字段。

最终 source-freeze2 有23源，仅MapWorkspace/auth test变化；client-target9实际42PASS、analyze6=0，7Go仍逐字节匹配native6。freeze1/worker-final1是历史帧，当前整仓/手机须使用freeze2。
### 2026-10-04 匿名意图入口集成补充

本人意图 Card 在匿名、组织身份和退休帧显示中文说明；Now 的弹出 Card 显式传 `onClose`，显示至少 48dp 的「关闭」，内联 Card 不传该回调，不调用 Navigator 退出 Now。MapWorkspace 320px 实际入口回归确认关闭后保留同 MapCanvas Element；没有以隐藏按钮或自动登录替代提示。

当前 MAP 源为 `work/v4-map001-layers/source-freeze3.json`：仅 MapWorkspace 与 shell test 相对 freeze2 变更，7Go 未变。`anonymous-modal-red1` 复现缺关闭按钮；`anonymous-modal-green1` 通过；`client-target11` 全8文件43PASS、`analyze7` 16items0。`client-target10` 保留旧文本 finder 与新增中性标题重名的1FAIL，修正为原 SocialIntentDraftPage 子树中的标题定位，未删除原入口/地图/动作断言。根整仓与最新真机验收另行记录，不能将此 widget 结果称为手机或生产验收。
