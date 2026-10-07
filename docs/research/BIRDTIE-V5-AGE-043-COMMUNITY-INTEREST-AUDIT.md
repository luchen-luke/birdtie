# AGE043：本人社群兴趣声明增量审计

2026-10-04；同一 BT-V5-AGE-043 lease 的本地实现切片。唯一 live 状态由 root 管理，本文件不解除 PARTIAL 或发布门槛。

## 审计与计划

- 原 033 `contexts` COMMUNITY FK + `person_contexts` 的本人 / context / relation / visibility 为唯一声明真源。原 NormalizeDeclaration 拒 COMMUNITY、无公开 visibility 输入，旧 `RemoveContextDeclaration` 却可按 ID 删除 COMMUNITY，需收紧此旁路。
- 原 043 `ReadOwnIntroductionSuggestions` 已能按当前公开来源读取双方 `interest/affiliation`；不能把 schema 中合成行称为普通用户能发布。首版仅明确本人 **interest 自声明**，不承诺关联身份、成员资格或实际参与。
- 原 `audit_events` ID 可稳定排序，但原表没有不可逆保护，状态相同 / xmin / count 都不能解决 absence→publish→delete→absence ABA。077 为该资源增加窄审计保护、查询索引，不增加授权、policy 或声明账本。
- 先做 native AEAD 短期具体预览、事务批准、原删除旁路与 077 迁移；再实现中文本人 API/controller/page 和正负测试。GET/preview 不创建 context/statement/audit；批准只写原声明及原审计。
- 本轮没有重导队列、认知 source grant、会员管理、外部模型、邀请、聊天或通知。

## 实际边界

`GET /v1/me/community-interests` / `/options` 与 POST `/preview` / `/approve` 使用当前 Authenticate + Person workspace。客户端只选真实公开 Community ID 和 PUBLIC/PRIVATE/DELETE；不接受 owner/agent/workspace selector 或 confirmed boolean。初始选项为 PRIVATE。

进程随机 AES-GCM key、90 秒以内预览绑定当前 Account/PersonalAgent/metadata/原 Session 稳定身份、Community/City/context/本人 interest 原行或 absence、native source xmin 和受保护审计 epoch。正常 idle 延长不改变稳定身份，也不延长旧 preview；进程重启不能恢复旧批准。密封 payload 只包含必要源 frame，无 Community rights/contact 私密整行。DTO 不返回 Session、xmin、内部 authority。

批准前预取实际写表 ROW EXCLUSIVE 关系锁；本人 Account 串行锁防首次并发，原声明与源行锁后以 PG clock 重核，写原领域与审计后再次执行当前身份/源/期限/新 epoch SQL。隐藏来源的本人声明只显示中性名称，允许转 PRIVATE/撤回；猜中隐藏 Community ID 不产生新声明。

077 缺失或 guard disabled 时入口返回 Unavailable，零领域写；旧公开引荐只读合同保持独立。077 有保护历史时 down 原子拒绝；无新历史时 old public/catalog 严格还原。

## 本地证据（不是生产验收）

|帧|真实结果|解释|
|---|---|---|
|native1|58 PASS / 3 FAIL / 0 SKIP|期限 fixture CTE alias 语法错误引发子例与父失败；保留原日志，不放宽产品断言|
|native2|62 PASS / 0 FAIL-SKIP|目标 Go test/vet/build 0，736 源稳定，旧完整 public/catalog、空历史 down/reapply、独占 DB DROP|
|native3|60 PASS / 2 FAIL / 0 SKIP|将 published Community owner_confirmed_at 直接设 NULL 被原 CHECK 拒绝；修为 draft + NULL 合法缺确认 fixture，原约束不变|
|native4|69 PASS / 0 FAIL-SKIP|目标 Go test/vet/build 0、736 全源稳定、旧非空 public/catalog 保留、owned DB DROP；包含 101→100 截断、City/source/metadata ABA、真实等待撤权/期限、真实进程重启、审计 INSERT 后到期回滚、原删除旁路|
|中文新客户端 target2|20 PASS / analyze 第二轮 0|正常具体版本、取消零批准、未知仅读、同 key transport、workspace ABA/迟到响应、hidden 本人撤回、大字窄屏键盘、真实墙钟到期|

native4 的双人正向通过实际 Store 公开 profile / 052 consent / 065 Social policy / 原 FIND_COMPANION draft→activate / 各自 preview→approve PUBLIC interest，再调用原 Introduction resolver；共同 Community 依据确实出现，单方 PRIVATE 后该依据移除。全部均为独占数据库合成验收，非真实供给或成员核验。

原始命令、源 SHA、完整旧数据/catalog 与失败帧位于 `work/v5-age043-community-interest/native{1,2,3,4}`。源复制在命令执行前。初次错误命令 cwd 和第一轮 Dart style/deprecation 诊断亦保留；Dart fix 仅指定三项新 test 文件。

## 冻结后审查与修复（保留上述历史帧）

root 完整 Go077 第一帧为9328 PASS、test/vet/build0、736源稳定；其源尚未有 TRUNCATE statement guard，不能冒称当前最终源通过。TRUNCATE审计发现实际本地 app role 为 table owner/superuser、无仓库已证据化权限隔离。`native5-truncate-red` **1 PASS / 1 FAIL** 真实证实保护历史能被 TRUNCATE 擦除，是产品缺陷而非 fixture。

根放行后仅原 lease 四文件增量修复：同一个窄 guard 函数的 statement 分支在保护资源/purpose历史存在时拒 TRUNCATE；无新保护历史保持旧行为。原生入口必须同时核两个实际 trigger 启用、类型、函数，不完整077拒绝零写；无历史down撤两个trigger并还原完整catalog。`native6-truncate-green` **72 PASS / 0 FAIL-SKIP**、target test/vet/build0、738全源稳定、旧完整public/catalog保留、down/reapply、ownedDB DROP。禁用/缺statement guard负例、真实app-role原子拒TRUNCATE及其后合法ABA旧批准仍失效均通过。

最后 `native7-wire` 用 work 内 Go overlay 对原 registered HTTP test **仅加成功响应日志**：5 PASS、test/vet/build0、738实体源稳定、公有旧数据/catalog保留、独占DB已DROP。捕获10条实际JSON无Bearer，存于专属 evidence/native-wire.json，未替换具体字段；原生测试进程结束，其临时预览不具备任何外部效力。Dart新增实际JSON兼容与receipt期限/context/single-result检验后 **22 PASS / analyze0**。receipt旧观测时间曾被接受，单独 RED 再修复 GREEN 均保留；当前仅接受 preview.observedAt ≤ native receipt.observedAt < preview.expiresAt、同原context、单条指定target、无options/truncated。网络迟到的合法已提交结果不按收包时间误拒。

## 仍需核证

- root 完整 Go077 与 Flutter/入口回归、真实手机批准/取消/撤回/重启、本轮截图、TalkBack 尚不能由上述目标测试代替。
- TRUNCATE 已按上述当前源修复；特权DDL禁用/替换guard、数据库管理员恶意变更不属于防篡改承诺。Session API仅单向撤销/合法idle刷新；本轮不声称防止特权SQL将同Session row revoked_at清空复活，不能把正常账号/workspace/来源ABA等同所有可能数据库ABA。
- SHARED_ACTIVITY 未获得真实公开声明来源授权，完整043仍 PARTIAL。Closed Pilot Ready / Consumer Beta：NO。

## 真机消费缺陷与 PRIVATE→PUBLIC 增量修复

根代理实际手机验收发现：PRIVATE 批准返回单条权威结果后，controller 清空 options；页面仍显示可点击的“检查公开兴趣”，但 controller 只认 options，点击既不请求预览也不给反馈。旧22项目标测试和根853项 Flutter 帧没有覆盖该连续动作，不能作为此场景通过证据。手机原 PRIVATE 数据与无 POST 截图由根保留。

`private-to-public-red2.log` 真实失败：页面保存 PRIVATE→点击公开，期待 `[PRIVATE, PUBLIC]`，实际仅 `[PRIVATE]`。首轮 `private-to-public-red.log` 先在普通读取文案断言失败，亦保留。修复仅原六 Dart 范围中的 controller 与测试：本人权威结果的当前 `sourceAvailable=true` 记录可作为新预览的最小 selector；每次仍请求原 native preview，重读真实权限/版本/期限，不用旧结果代替批准。来源隐藏/期限变化的403/409会清缓存并显示刷新反馈；到期预览也明确提示。正常读取只说明当前状态，只有未知提交后的只读恢复才说明不能证明先前操作成功。

新 `client-final4.log` **26 PASS / 0 FAIL-SKIP**，`analyze-final5.log` **6项 / exit0**。新增页面实际 PRIVATE→PUBLIC 新预览/取消零额外批准、失效来源409可见反馈，controller403/409、本人 selector迟到token ABA、未知批准零重发；原 transport/workspace隔离、大字窄屏、自然到期、严格实际native JSON解析全部保留。当前测试用合成 HTTP，不冒称新手机、生产供给或新的 Go 原生实现。

`final-source-freeze2.json` 为新六 Dart SHA；旧 `worker-final1` 1131个文件逐项 bytes/SHA 全部核验，原10 Go/SQL无变化，旧freeze/失败/日志未覆盖。新完整 Flutter/build/安装和本场景真机 PUBLIC 批准/撤回须由根代理重新取得证据；TalkBack仍 NOT_RUN。后续 SharedActivity审计不改变本修复范围或原 PARTIAL/发布门槛。
