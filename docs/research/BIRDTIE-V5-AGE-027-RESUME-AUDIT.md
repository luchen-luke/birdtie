# BT-V5-AGE-027 恢复审计 — 2026-10-04

## 当前需求与复用结论

原需求区分 visited、saved、liked、created moment at、attended activity at。既有canonical/native原实现真实支持当前收藏、本人private Moment明确关联、typed人工LIKED／未经核验VISITED，不支持客观visited/check-in/attendance或machine processing。已完成native来源不重复建模；原任务仍PARTIAL。

本次局部实现：真实本人PrivateAccess四HTTP入口、最小closed human DTO、原Memory CAS与目标Agent原子绑定、中文单Place人工管理页。人类无需AI、定位或认知grant，必须当前本人身份；Organization Workspace不可代人读写。公共Place History与本人声明管理是不同页面、不同投影。

审计发现两个实际缺口并补齐：过期当前来源过滤造成原声明ID/CAS不可找回，另建typed本人单Place最小control读而非第二账本；新声明 expectedVersion0可能在Agent替换后写给新Agent，正式扩共享原Place writer scope，加原锁后expectedAgent比较，共用原native writer。原五键接口不变；human六键先严格解析后复用原DecodePut。

## 页面任务

目标用户：已登录本人；当前一个稳定Place。入口根代理PlaceDetail→same captured session `/v1/me`→nested identity boundary。核心任务：查看合法当前本人来源，检查、修改、限期保存和撤回本人声明，找回原操作未知结果。当前公开目标不可读时不显示旧名称/公开资料，仍允许查看本人已有controls和撤回，续期须可读公开目标。

草稿、具体确认、提交与原生回执分离。确认展示本人、Place、性质、原版本、新/更新、范围和有效期，取消不写。AgentOnly明确仍无模型许可，VISITED明确未经核验。安全恢复只保存原ID/CAS/Agent/输入、session fingerprint；未保存不发送，未知不重建新ID或从absence推成功。切换身份/工作台、ABA和迟到确认失效；server当前self鉴权与CAS仍为唯一业务权威。

## 规则与真实验收

UX-CHECK-01/02/03/05/06/09/10/11/13/14/16：中文直接路径、少缺项、有检查/返回/取消、同一原动作、身份与版本、未知恢复、移动端和辅助语义。本次应用impeccable现有Material规则，仅独占新页；不另造UI规则backlog。

最终定向native7 200 PASS／0 FAIL-SKIP，720源码stable、公有旧rows完全不变、自有库DROP；Flutter9 32功能＋3加载PASS、target analyze0、Go targetvet/build0。真实命令、历史失败和原始输出见 [本轮证据](../testing/evidence/place-memory-human-2026-10-04/README.md) 与 `work/v5-age027-resume`。本线源码冻结14文件，根负责shared route/entry及整包Go/Flutter构建。

## 剩余与完成分类

- 客观visited/check-in、真实attendance：UNAVAILABLE；RSVP/Plans/Moment、收藏和self declaration不替代出席或到访。
- cognitivePurpose/model：UNAVAILABLE；没有private分析/模型外发、机器批准或storedPreview审批ledger。
- 原子human target guard只绑定当前Agent ID；没有具体xmin/metadata版本批准，不能用该接口冒称机器exact source permission。
- 原任务PARTIAL。真机、真实TalkBack、真实SecureStorage close/reboot设备验证、运营/生产身份/真实CSSA未运行。
- Closed Pilot／Consumer Beta：NO；原发布门槛继续有效。

不写live queue或共用报告，不解除依赖／外部门槛；根代理独立核证后记录本局部证据并保留完整原PARTIAL。


## 实际真机跨语言失败后的增量修补

历史f255真机入口失败与API两个200的差异已定位：控制DTO合法Go RFC3339 `+08:00` 被原Z-only Dart解析器拒绝。未改变服务器权限判断。本轮先增加真实字段DTO和偏移测试，RED1 exit1（10功能通过／1加载通过／2失败），再修本地日历严格核验＋偏移归一，GREEN1 exit0（35功能／3加载、0失败/跳过），target analyze exit0。原数据库、CAS、bound Agent、当前会话、来源最终复核与八个Go源byteSHA不变。

证据 `work/v5-age027-resume/offset-fix/`，冻结 `source-freeze-final2.json`；旧final1不得覆盖。新的完整Flutter、APK与设备验证待根代理，不复用f255旧成功构建为修补后证据。时区偏移只解释同一绝对时刻，不证明所在地、到访或出席。完整要求仍PARTIAL／发布NO。
