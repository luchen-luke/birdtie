# AGE038 / AGE039 通知路由实际实施审计

2026-10-03。当前任务 IN_PROGRESS，由 root 持有精确 lease；本记录是审计与实施计划，尚未完成运行验收。原只读审计来自 `work/fix-city001/http/read-only-next-notification-routing-audit.md`，原文件保持。

## 已有与新增

- 复用 Inbox、独立 starts-soon worker、原生活动/消息/联系申请/成员服务及 037 的五种 Route。037 enrichment Service 的用途授权缺口仍 Unavailable；不把 OfflineBoundary 当真实通知许可。
- 新增普通领域通知的当前来源/实际 recipient 解析、独立持久人类偏好 CAS、确定优先级和最小决定账本；接入原领域事务。普通通知不分析正文、不授权模型、Memory、自动报名/消息、组织代理或 A2A。
- sender 与 recipient 分开，来源版本只读当前原生行；固定中文文案不复制姓名、聊天正文、活动标题、任务 query 或 Profile。
- 旧五类 Inbox 展示和原 ID/API 保留，八种语义类作为附加 metadata。没有真实 Business 独立事件的类型不能冒充已有 producer。

## 并行边界与迁移顺序

009 独占 Memory 强化包与 060；010 已以真实530项本地检查记录为 PARTIAL，后继011独占模型出口/四层预算与062；本任务独占通知包、原通知 writer 和061。迁移由 root 协调冻结和应用。worker 只在分配子范围实施，live 队列和共用报告由 root 修改。

## 实施与待运行验收

1. 严格闭集 policy API：当前 Person/Session/exact PersonalAgent，独立 CAS/关闭/期限/暂停；禁止跨工作台、客户端主体选择、重复/未知/大小写 JSON。未配置或禁用分类返回普通 NORMAL，静音使用 SILENT/BLOCK，不关闭普通业务路径。
2. 原生 source → 当前权限 → 当前 policy → priority → 决定/Inbox 原事务；稳定 event/source 去重、真实改期重新提醒、source 删除与当前撤权投影关闭。
3. 在隔离随机库执行实际原生正负、重试、重启重读、并发/CAS与期限边界；完整旧 public 行及 ID/API 保持。fresh/current 迁移、非空 down 原子保护、empty down/reapply；目标与默认完整 Go、vet/build和固定 hash。
4. 八类逐类标记 actual producer 或 Unavailable；DIGEST 是待汇总状态，不宣称已摘要或 push 送达。涉及 UI 才运行新 Flutter/真机检查；本轮尚无 UI 变更或新截图。

## 当前实际结果与失败

- policy/domain/HTTP/Store 限定旧范围曾291 PASS，源窗口完整public/hash保持；该范围没有经过实际事件 resolver，不能代表所有通知入口。
- delivery-native1 实际十四来源运行暴露原生SQL `digest(bytea,unknown)` 依赖不存在：27 Test FAIL＋一个包 FAIL。原日志和失败源 hash保留；改为内置sha256，未在fixture装扩展掩盖。
- delivery-native2 实际33 Test PASS、0 fail/skip，相关vet/build0、选定29源稳定、全部旧public行保持，自有库DROP；十四kind逐项写入来源矩阵。Reminder是实际自有活动/RSVP的scoped router，部署scheduler未验证。
- migration-native1 脚本动态SQL转义错误、native2自有Inbox fixture使用无效category失败均保留；native3实际90命令PASS：001–060旧public行、061无backfill/up、policy及decision/Inbox两类非空down各exit3原子保持、owned cascade、empty down/reapply、源码hash稳定/DB清理。不是在生产执行down。
- current-061-full1 默认完整Go首轮实际6079 Test PASS、7 Test FAIL（含parent）＋2包FAIL，0测试SKIP，19无测试包另计；未到vet/build或后两轮。City四typed发布审核通知原约定“已发布”被generic中文丢失，已从实际review状态选择固定中文修复；sameTypedUUID自有fixture改写了新增通知FK引用的OrgMembership PK，已保留生产FK和全部原断言，改为仅调整未引用的自有CommunityMembership。根增精确test scope经queue锁/hash保存，全部任务与历史保持。两项需新完整回归，不能将6079报为全PASS。
- delivery-native3 正在测十四kind、exact终态重试/改版与默认RR pool及显式RR路由拒绝，最终原始结果待核；009根独立89 PASS亦待当前共同完整Go。011初稿编译通过只证明编译，062原生验证仍待结果。

Canonical 实际行为和未实现项见 [通知路由](../architecture/AGENT-NOTIFICATION-ROUTING-V5.md)。BUSINESS独立producer、DIGEST汇总、本人通知设置与新通知目标UI仍未完成；不将API/八类enum标为完整消费产品。真实 IdP/HTTPS、现实组织和活动授权、生产地图、部署日志、值守、提醒调度/告警及真实 A→H 门槛仍缺，Closed Pilot / Consumer Beta NO。
