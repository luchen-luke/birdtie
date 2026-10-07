# AGE031 / AGE032 Membership Context 实际审计

2026-10-03，官方仓库D:\Project\birdtie。来源[AGE原文](../product/BT-V5-AGE-AGENT-ENRICHMENT-EPIC.md)1172/1202，唯一队列031合并Community与Organization Membership；实现范围CODE_AND_LOCAL_VERIFICATION。没有Civu复用或队列写入。

## 原接口与边界

| 原接口 | 实际能力 | 本项复用 |
| --- | --- | --- |
| 030 community_memberships、community/social.go、postgres/community_social.go | 原UUID、Person、Community、三角色owner/admin/member、active/pending/invited/left/rejected、created/updated；原加入、退出、批准、角色、转交 | 只有实际active可进入Context；pending/invited非已加入；未给Community虚构moderator或Agent |
| 019/028 organization_memberships、postgres/organization_memberships.go | member/moderator/admin/owner；active/invited/removed；原邀请、接纳、角色、撤销/owner保护/audit | 保留四角色、真实原UUID；组织资源ID≠组织account principal。原workspace授权不是Person私密资料许可 |
| 原Authenticate / actorref / Personal Agent / 053 metadata | 实际本人会话、active Person、精确PersonalAgent与native metadata | 不用客户端Verified/confirmed/Agent字符串自证；缺失metadata不创建；Authenticate会刷新session idle，仅源领域只读 |
| 066 Controller | Enrichment默认OFF、generation ticket | 只作刹车，不是数据/模型用途许可；OFF→ON旧Snapshot无效 |
| agentevent CommunityJoined/Left、SnapshotVersion | 真实当前retained active/left metadata ingress及native更新摘要 | Community复用SourceVersion/版本helper，但当前源复核在本包；LEFT不证明曾参加/谁移除 |
| 005 agentmemory/evidence.go、postgres/agent_memory_evidence.go、057 | 原闭集只有Moment/Participation/SavedPlace，explicit人工引用与墓碑 | 本项**不是005落库Evidence**，不修改闭集、Memory或旧migration |

原Community TransferSocialOwner只改变两条membership角色，没有改communities.owner_account_id；后者保留原创建者。本包检查原创建者和真实当前单一owner的active/Block并把两者源token绑定，防止误把创建者当当前owner导致合法转交不可读取。未知多owner形状保守拒绝；原领域不改。

两个membership表无自身持久expiry或单调revision。真实created_at/updated_at和保留行/xmin组成opaque token，识别同timestamp修改、同ID重建、退出再加入、撤销再接纳；不是历史转移、业务CAS、身份认证或持久用途grant。City/Community有实际expiry时只约束读取，Organization没有expiry不发明该字段。

## 当前真实组装路径

新agentmembershipsignal.NewService(real pgxpool, actual Controller, server dev config)创建具体native resolver；没有外部fake/resolver注册或客户端时钟端口。ReadOwnMembershipContext执行Authenticate、初读、assembleContext，再最后payload SELECT当前权限/源核对并重新组装。输出Scope PERSONAL_MEMBERSHIP_CONTEXT、Purpose HUMAN_SELF_REVIEW的一条真实Evidence，可由普通本人领域服务调用；并非只event/fixture/Unavailable壳子。

Community要求本人active原membership、published active非hidden社群、创建者/当前owner active与Block、原expiry/verification时间有效、关联City published非过期/active CityContext。Organization要求本人active原membership、当前active organization与organization account、active owner、当前Block；private原组织/社群允许其本人成员view，不能向第三方或public转存。没有读取他人Profile/roster/聊天/Private Memory。

每次load以ReadCommitted READ ONLY transaction和SET LOCAL TIME ZONE UTC获取canonical native source，同一最终SQL statement snapshot为线性化点。HMAC绑定当前session、exactAgent/account/metadata、source、selector及真实066 generation；Revalidate不续租，不给旧事实长期缓存批准。未知、过期、撤权/改角色/删除重建、账号切换/迟到取消无payload。源租期最多5分钟且受本次deadline/当前session/实际source期限约束；request deadline不是源已有expiry。

事实不自动等于摄影兴趣、敏感身份、学校认证、Friend/Close/Tie、matching目的许可或Agent动作。Community不变Agent；Business仍dormant；机构管理者和Organization Agent不能借此读Person私密内容。ReadForCognition始终Unavailable，即使真实本人证据且Enrichment ON。

## 实际证据与限制

最初两轮真实RED及失败源码/owned DB清理保留；修复fixture、SQL拼接和连接释放后第三隔离库144 PASS/0fail/0skip、vet/build0、旧public完整行相同、五源稳定。补充后最终源码结果、正负/并发原始日志、source SHA、脚本和复现命令以[正式证据](../testing/evidence/agent-membership-signal-2026-10-03/README.md)为准。不把历史144当后续新源码证明。

最终第四staging154与一次copy后的production154都0fail/0skip、vet/build0、旧public完整行相同、ownedDrop；production/staging/archive/manifest五源SHA一致并冻结。增加真实defaultOFF、同UUID两个typed域、source lease、退出再加入/撤销再接纳、未知多owner、owner未来/Infinity时间及同内容mutation用例。root独立目标包与全API验证由root核证，不预称全域完成。

共同059 root round2时区Organization Revalidate在0.12秒用例中实际ErrExpired、形成3fail事件。代码把PG ObservedAt与host time.Now比较，混用数据库和Windows墙钟；原日志没有瞬时偏移数值。旧代码200次连续read/revalidate的fresh诊断157 PASS未再现，不能虚称诊断RED。保留旧154及157源码/log后改为实际native DB clock统一租期/未来判断，仍拒未来1ns和真实到期，不添加容忍或caller clock。明确模拟host落后负控与真实200次快速复核加入当前166；最新manifest/完整原始结果在clock-fix归档，旧root全域通过或失败不能替代新源码回归。009只读审计因此暂停，未实现009或改队列。

没有新DDL/原源backfill/HTTP/main/server或UI；没有统一033 ContextBuilder、005持久Memory Evidence、认知current-purpose/模型出口resolver、自动Memory/inference/外部写/A2A或真实CSSA数据。真实普通本人Context组装完成可按031/032 CODE_LOCAL核验；若另要求持久005/统一033消费，不得把本包说成该接入。适用UX-CHECK-06/08/10/11/16仅领域边界；Flutter/真机/辅助技术/截图未运行。CGO0无race instrumentation；root全API证据另外核验。Closed Pilot / Consumer Beta：NO。
