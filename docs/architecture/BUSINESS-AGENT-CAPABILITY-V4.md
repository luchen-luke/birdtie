# Business Agent 能力基础：本人已核验资料规则

2026-10-03；BT-V4-BIZ-002。本轮仅人类管理范围 foundation，整项原 verified AI interaction layer **PARTIAL**。Business Agent、统一 Business Profile、模型及公开资料用途桥仍 Unavailable；不解除053 dormant约束，不修改原Runtime/身份/Console/DDL。

## 真实可调用路径

根在原 server 注册 `POST /v1/me/businesses/{businessID}/knowledge/ask` → `askOwnBusinessKnowledge` → `agentbusiness.Service.Answer` → `postgres.ReadOwnBusinessKnowledge` → 有界中文规则。body精确为 `{"query":"营业时间","placeId":""}`；场地问题用明确canonical placeId。重复/未知字段、null、尾随JSON、身份workspace selector、超过4096字节、无效UTF8/UUID均拒绝。只有真人当前该商家Owner/Admin；匿名、Business/Organization登录、member、独立reviewer、外人不能调用。

人类管理员无PersonalAgent/PrivateProfile也可以读取。这里复用原businessconsole.Access与native身份，没有第二身份/权限账本。Actor是businesses.id，Principal是businesses.account_id；不把其中任何ID当Person。Business不继承OrgAgent、Person Memory、到访/关系许可或通知开关。

## 来源与规则

- 原041 active Business、独立active Business Account、verified claim及有限非未来review时间；当前actor是active Person、Owner/Admin与同token真实Session。
- 070 profile/venue Facts必须真实verified/current、finite validUntil、finite非未来review时间。管理员改为pending、拒绝/撤销、到期后不引用。审核员/来源URL/rightsNote/名单/私有备注不进入回答。
- 场地还要求当前verified经营关系、approved原Venue candidate及精确Place/City/reviewer绑定、公开Place/City、有效Venue/Place/City期限，兼容原Venue的可选active运营Organization资格。运营者只是该Venue source资格，不授组织身份或Memory。
- 原verified claim/经营关系直到显式撤销，不把新Facts有效期改成身份永久许可或用旧身份核验替代Facts freshness。
- 闭集问题：`商家介绍`、`商家简介`、`营业时间`、`官方网站`、`场地适用场景`、`场地预约链接`。资料不足/其它问题为unknown、sources空。多场地不猜目标；用户明确选择placeId。
- 营业时间保留真实来源IANA时区及每个明确日期，未列日期未知。不推断当前营业、有空位、付款或预约成功。场地适配/预约链接仅说明当前资料，没有工具副作用。

返回 mode=`HUMAN_VERIFIED_RULES`，known/unknown、稳定来源ID/实际Facts版本与validUntil、opaque sourceVersion。`agentStatus=modelStatus=unavailable`、`tools=[]`。共享 `agentruntime.ForType(BUSINESS)` 仍 Available=false；`Service.Invoke` 无论配置/输入都硬Unavailable，没有模型fallback。规则渲染与离线shape合同不是实际Agent调用。

## 当前性与会话

native读取显式READ COMMITTED、本事务UTC；domain只有SELECT/LOCK。PostgreSQL row SHARE锁要求普通事务，**不是SQL READ ONLY**；initialAuthenticate会合法刷新现Session idle，这也不能称整个HTTP全部SQL只读。领域资料不写。

关系表等待先于Session锁；Business SHARE → 按ID排序Account SHARE → exact本人membership SHARE → exactSession SHARE，锁后新PGclock/session/source核对。Business SHARE兼容已有公开活动/old outbox SHARE，串行同商家Console写。来源payload来自最后一条有界SQL、最多100场地（101拒绝），不包装旧跨事务Console结果。

时间戳显式归一UTC，不以Go time.Location指针判断同一native UTC时刻。validUntil与被选Venue/Place/City最早期限在本事务最终PGclock再核对；到期或时钟无法核实拒绝。sourceVersion绑定实际当前Business/Account/member行与被允许Facts、依赖Venue/Place/City/operator的opaque xmin；它是短期快照token，不是业务revision、概率、长期授权或可提交grant。

Service.Render前后及HTTP JSON materialization后通过真实Store重新读取当前帧、比较sourceVersion，同事务锁后Session/期限校验非刷新。没有缺能力时退回ownerID/匿名/旧Authenticate；没有在持Session锁后调用旧独立事务读包装。来源ACL线性化点是每次最终payload SQL；最终时间核对只补期限和会话。提交或网络发送后的新变化不承诺“永久最新”，不能撤回已经交付的数据。

## 原验收剩余

实际本人规则回答覆盖部分grounding，但它不是原完整BusinessAgent AI层。完整恢复需要：根核证BIZ003/070；真实Business Agent/Principal/metadata及统一Profile适配；明确资料受众/处理用途和approved工具的当前授权桥；真正provider/current source-purpose-egress-budget。070审核grant仅授权claim/profile/venue审核，资料verified/HTTPS/rightsNote/admin确认不自授公开或AI用途。若将来启用Agent，必须根协调独立增量迁移及功能范围，不能改旧053或SQL造active。

未配置模型保持Unavailable；不开放Business Memory或Person/Org Memory。无新Client/手机/TalkBack/restart证明。本backend中文/稳定来源/unknown适用UX-CHECK-01/02/06/08/09/10/11/12/16，但不称消费UI完成。Closed Pilot / Consumer Beta NO；无现实经营核验、生产身份/部署、外部发信/付费/模型访问。

## 证据

专属 `work/v4-biz002/verify.ps1 -Round <fresh-name> -Baseline 70` 使用owned随机库001–070+原dev-seeds，只跑 `-run TestBusinessKnowledge` 三包；071 ADS不在验证范围。根统一whole-Go另验。本文件最终计数由证据README记录，首次编译/fixture/原生失败完整保留，不把无测试compile或测试目标通过但vet中断称完整PASS。


## 2026-10-07 前瞻增量：商家暂停身份的真实工作台入口

本增量属于原 BT-V4-BIZ-002 与既有 AGE054 来源映射的身份前置；整项仍 PARTIAL。上文“统一 Business Profile 不可用”现须区分：当前工作台可明确建立/查看真实 suspended Business Agent 及原053统一 Profile **元数据**；原公共 GetAgentProfile、Business Runtime、Invoke、模型、工具、资料用途与公开使用仍未启用，不将人类规则回答改称 Agent。

- 工作台真实已选商家 →“商家智能体身份”→ GET `/v1/me/businesses/{businessID}/agent-identity` →当前商家名称、经营权核验和暂停状态→检查当前具体经营权版本→明确 POST 同路径，仅 `expectedClaimVersion`。不会创建空商家，不以 Person/Organization/Place/城市 ID 代替 Business principal。
- GET/POST 都使用原 native Person Session、该商家 active Owner/Admin、独立 active Business Account。POST 还需原041与070当前 verified 经营权及有限非未来审核时间；无当前权限先拒绝，再判断版本冲突。审核资料不授予 AI 用途、读取私有资料或工具执行权限。
- Business advisory/资源锁 →经营权 source →按ID排序 Account →精确成员→Agent/Profile→Session最后。唯一身份由原019 `(agent_type, principal_account_id)` 保证；创建明确写 `suspended`，原053触发器在同事务初始化统一 Profile。重复操作只读复用现身份；retired 不复活，已存在 Profile 不重置版本。
- 本事务新建 Agent 才写原商家 audit 的 `agent_provision`。104只扩原070动作闭集，保留15原动作；已有新审计时 down 拒绝破坏性回退。未执行104或缺原schema时真实失败，不声称已部署。没有新授权账本、激活开关、数据回填或 Memory 写入。
- 审计等待后单一 materialized PG clock 同时核当前 Business/经营权/账户/成员/Agent/Profile 的实际 xmin、当前 Session、审核时间及30秒短读期限，随后 commit/context 检查。HTTP编码后再真实读取同绑定来源，比较内部 sourceVersion；该绑定不导出给客户端。只含身份与 Profile 基础元数据，没有已核验知识或私有内容。
- 客户端借用当前 Console transport，账号、token、组织、选中商家、父来源/API对象或来源 epoch 变化永久退役旧页及具体版本检查。409须新GET、新检查、新确认；失联或无法确认写结果只GET核实，不自动重发。GET“当前存在”不是此前操作的因果回执；已经发出的写不声称撤回。身份仍暂停，用户页面不展示 UUID/opaque binding 表单。
- 适用 UX-CHECK-01/02/04/06/08/09/10/11/12/16；中文、现有导航及48dp真实主要动作保持。本批仅新的相关 Go/domain/registered HTTP/事务 SQL spy 与 Flutter/API/controller/实际 Console widget 单元；具体版本命令和失败见 `docs/testing/evidence/business-agent-identity-lifecycle-2026-10-07/README.md`。SQL spy/静态迁移检查不是实际 PG/锁并发/迁移验收。

104 migration、实际 PostgreSQL、schema/seed、全量回归、analysis/build、真机/AT/真实性能、真实 IdP/现实商户核验均 NOT_RUN；模型与 Business Invoke保持OFF/Unavailable。Business Pilot、Closed Pilot、Consumer Beta仍NO。
