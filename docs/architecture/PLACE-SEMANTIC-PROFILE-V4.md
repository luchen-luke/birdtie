# Place 语义档案契约

2026-10-03；任务 `BT-V4-PLC-002`。这是该能力的唯一实现规范，承接 V4 总纲和既有 Place/Venue/Intent 契约。队列状态由根代理核证后更新；本规范不宣称已部署、真实场地核验、消费 UI 或 Beta 验收通过。

## 1. 实际能力与来源

`placeprofile`、迁移067和 PostgreSQL Store 实现持久的公开声明候选、独立审核、公开档案、撤回和真实注册 API。现有 `placematch` 消费仍有效的公开档案做可解释规则排序。没有从私人 Profile、Memory、Moment、收藏、RSVP、聊天、轨迹或商家付费推导公开事实。

输入必须由真实城市编辑成员明确提交来源标签、HTTPS引用、权限说明、观察时间、有效期和审核判断。服务不会抓取引用页面，也不会把文字权限说明当成现实版权、经营权或外部合作授权证据。当前验收数据全部是隔离库的明确合成资料。

## 2. 七字段与逐项证据

`schemaVersion=place-semantic-v1`，同一 Place/City 稳定 ID。七字段均显式返回；缺少事实为 `null` 或空代码列表，`claims` 对应项为 `UNKNOWN`，没有来源或置信度。每个已填字段是 `REVIEWED_DECLARATION`，继承此次候选的公开来源及判断；不同来源应另提交候选，不在读取时拼接或猜测。

| 字段 | 已实现的有界形状 | 不代表的事实 |
| --- | --- | --- |
| `vibe` | 最多16个不重复代码 | 私人偏好、实际当前氛围 |
| `good_for` | 最多16个不重复代码 | 已确认活动或自动资格 |
| `price` | ISO形状三字母币种、整数最小货币单位范围、明确计价单位 | 实时价格、免费推断、汇率比较 |
| `accessibility` | stepFree/accessibleToilet的yes/no/unknown声明 | 无障碍安全保证 |
| `group_size` | 1至1000人数区间 | Venue容量、实时空位或预约 |
| `reservation` | none/contact/external_url，外链必须HTTPS | 已预约或预约成功 |
| `suitability` | 最多16个不重复代码 | 绕过原Venue活动适配门槛 |

全未知候选、重复代码、未知/大小写变体键、重复JSON键、必需对象null、非法UTF-8、过深/过大输入、非整数金额、越界范围均拒绝。HTTP输入最多16KiB，纯规范facts最多8KiB，SQL JSONB展开另有16KiB约束。Source观察时间不得在未来，有效期最长365天。

`confidence.kind=EDITOR_ASSESSMENT_UNCALIBRATED`，level为LOW/MEDIUM/HIGH。这表示提交后经独立审核认可的未校准判断；不表示真实性概率、模型输出、自动行动、私密数据读取或模型出口许可。

公开 Source 只含label/url/observedAt/reviewedAt/expiresAt；不返回私有rightsNote、提交/审核账号、会话、坐标或正文轨迹。`checkedAt`是实际 PostgreSQL 当前性核查时刻。

## 3. 原生生命周期与当前权限

候选的事实、来源、目标、operationId、request hash和来源行代际不可修改。候选version固定1只表示这个不可变候选，不能冒称Place版本。新编辑生成新候选。公开档案version是真实持久CAS计数，首次批准为1，批准替换或撤回严格递增1。

提交者必须持有当前有效原生会话、active账号、该城市active contributor/reviewer成员权限；审核/撤回要求reviewer，提交者不能审核自己。没有城市编辑成员权限的Person/Organization/Business均拒绝；组织管理员身份、workspace header和客户端owner参数不授城市角色。普通公开地点编辑不借Personal Agent或认知授权。

事务显式READ COMMITTED和UTC，Account→Session→城市成员→City→Place→地点编辑advisory锁→候选/档案；空档案也序列化真实CAS。最后的同一PG语句核查当前会话、账号/成员/城市、目标与相关来源代际及期限，随后没有新的DB来源读取。候选来源采用实际行xmin，账号、城市、Place或编辑成员改变并恢复后，旧候选仍失效，必须重新提交/独立审核。该代际不是虚构版本或永久授权token。

同operationId且完整输入相同返回原回执，不增加候选；改变输入409。重试已批准候选返回 `APPROVED_CANDIDATE_RECEIPT`，它只确认曾审核该候选，不能保证目前仍公开；公开GET的当前结果才是权威资料。撤回后保留档案版本与审计，公开读取404；重建Store也不能恢复旧发布。所有拒绝/晚会话失效必须回滚候选、档案和业务审计。

公开GET要求published档案、对应approved候选、当前公开未过期Place/City、当前有效提交者/城市编辑来源代际以及来源期限。独立审核记录是已发生审核的历史事实；审核者今天的角色不作为消费者的当前权限。公开内容不因普通登录读取而创建Agent许可。

## 4. 注册接口

五条路径由真实 `Server.Routes()` 注册，Store通过已注入catalog的窄能力取得；缺接口返回503，没有按ownerID回退。

| 方法/路径 | 用途 |
| --- | --- |
| GET `/v1/places/{placeID}/profile` | 匿名可读取当前公开档案与七项claims |
| POST `/v1/cities/{cityID}/places/{placeID}/profile-candidates` | 当前城市编辑提交具体候选 |
| GET `/v1/cities/{cityID}/place-profile-candidates` | 当前reviewer读取最多100个仍有效待审核候选 |
| POST `/v1/place-profile-candidates/{candidateID}/review` | 独立approve/reject具体候选与档案版本 |
| POST `/v1/places/{placeID}/profile/withdraw` | 当前reviewer按CAS撤回公开档案 |

响应no-store，安全错误以中文显示。禁止查询参数替代主体或隐式组织workspace。匿名写401，当前权限失效403，版本冲突409，无有效公开档案404，能力/存储异常503；不透出内部SQL或私人错误。

## 5. 排序复用与运行边界

真实已注册 `/v1/me/social-intents/{intentID}/place-matches` 复用当前Person会话与本人原生Intent，显式RC；当前Account/Session/selfIntent/City和有效profile来源集按同一最终PGclock核验。旧ownerID方法只留内部兼容，注册路由没有回退到它。组织与其他Person不能继承本人Intent或私密数据。

`intent-place-v2-semantic`保留原Venue类别/容量、城市、区域文字和明确Place约束及原rank层级。层内good_for、suitability各匹配明确类别加2，声明group_size包含明确人数范围加3，然后按稳定Place ID。语义分不能绕过Venue硬条件。用户未声明价格、氛围、无障碍偏好时这三项仅描述，不用私密Memory补偏好。不计算距离，不称实时可用/已预约，返回中文理由和原OPEN_PLACE动作。详见 [既有匹配规范增量](INTENT-TO-PLACE-MATCHING-V1.md)。

没有模型、自动审批、预约外部请求、推断到访或Agent执行出口；`modelAuthorization=UNAVAILABLE`。已审核公开声明不等于事实绝对保证。

## 6. 验收与未测范围

命令、失败、固定源码SHA和隔离PG证据见 [任务证据](../testing/evidence/place-semantic-profile-2026-10-03/README.md)。适用UX-CHECK-04/06/07/08/09/10/11/12/16：稳定实体、未知/期限、候选与发布区分、当前权限/版本、幂等、主体隔离、无模型普通路径、持久重启、无私密来源聚合都有API/代码证据。

UX-CHECK-01/02/03/05/13/14/15涉及完整消费编辑路径、截图、小屏/键盘/读屏和无讲解可用性，本轮未做Flutter界面或真机操作，不能标通过。旧全仓066结果不能代替当前067/068回归。Closed Pilot与Consumer Beta保持NO，真实IdP、现实授权活动/场地、HTTPS/地图、运营值守和真实A→H仍按原发布门槛处理。
