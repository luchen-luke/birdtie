# 本人意图关联原报名活动

日期：2026-10-05。原任务 BT-V4-PLN-001；唯一领域真源为原 social_intents、activities、activity_participations 和当前原生身份/来源。适用 UX-CHECK-01/04/05/06/07/08/09/10/11/12/14/16。

## 消费路径与边界

“我的社交意图”→一条本人 FIND_ACTIVITY ACTIVE/MATCHED→“从已报名活动完成这条意图”→当前本人 going 列表（最多100，明确 truncated）→“检查关联此活动”→具体本人/意图/原受众/原约束/活动起止/地点或线上/报名非到场/截止/后果→“确认关联”。取消或系统返回不提交。完成后显示原活动信息，保留“查看原活动详情”及原“计划”路径。页面不用内部 UUID 输入或状态下拉伪造转换。

原活动创建时间来自 activities.created_at，不把计划时间、报名时间或零值当活动事实。时间使用设备时区转换并标设备UTC偏移，附活动IANA时区原值“未换算”，不把BST/GMT固定为UTC+0。ONLINE无假地点；TBD显示地点待定；仅当前公开有效 Place 可提供名称。当前无已核验人数、粗区域映射、平台时拒绝猜测匹配，不支持 FIND_COMPANION 等其他意图。

## API 与权威

- GET `/v1/me/social-intents/{id}/activity-conversion`：只读当前本人、意图、原going候选和具体版本。
- POST `.../preview`：仅 ActivityID + ExpectedVersion；当前源重新核验，返回进程密钥AEAD预览。无领域写。
- POST `.../approve`：仅原 PreviewID；不能 confirmed/status/关联字段/未知query扩大效果。

PERSON 当前Session、活跃PersonalAgent与原Profile、本人Intent及原受众权益构成边界；组织工作区不可使用。普通RSVP和私人提醒不因此新增Agent前置。期限 min(90秒预览上限、原Plans30秒边界、原Session/Intent/City/Activity/Place等有效期)，批准不能随Authenticate idle延长而续命。只转换证明排除会话正常idle更新的xmin，其余稳定Session身份/绝对期限/token摘要/撤销与当前idle均核；原Plans读证明不修改。源xmin/ACL变化、ABA、自然到期、最终Session等待后撤权都拒绝旧批准。

活动已报名必须复用原 going Participation，Intent→Activity→Participation的NOWAIT锁失败返回冲突，避免与原报名锁顺序死锁等待；不重插报名。具体来源/原受众证明与绑定版本检查后同Tx按原状态机写ACTIVE→MATCHED→CONVERTED、原关联及审计。编码后另一次当前原生复核覆盖所有返回候选及本人Session/来源，不把GET当效果许可。

提交结果未知（含可能提交后编码复核的403/409）只用原Intent GET核对，不能盲重发新预览或新报名。相同具体预览重试只有原关联/当前许可仍成立才能读原回执，不延长期限。原API进程重启仍读原Intent/Activity/Participation IDs，旧进程预览失效。

## 数据兼容与隐私

085仅增加原social_intents四个可空字段：converted_activity_id、converted_participation_id、converted_at、conversion_preview_digest。没有第二台账/授权账本。关联须原已going报名和本人FIND_ACTIVITY MATCHED→CONVERTED路径；新未关联CONVERTED写入拒绝，已有历史CONVERTED全NULL保留未知，不补FK。有关联历史时down在实际关系锁内原子拒绝，unused down/reapply保留所有旧列行、IDs、xmin和可见semantic catalog。

本人旧generic own DTO带三个关联值；旧active human DTO仍显式最小字段，不因omitempty假设其带关联。中文原页面通过新GET查看真实关联。旧ACTIVE公开发现排除CONVERTED，不公开原参与ID；终态历史不可用不读取私密fallback。

## 本地验证与已知范围

最终定向 native8：61 PASS /0 FAIL-SKIP，schema085，877 Go/SQL/module源+3原seed稳定，test/vet/build及2原CLI构建0；完整旧非空public/semantic catalog、Participation xmin、unused down/reapply和自有库清理通过。真实registeredHTTP记录五类当前身份、普通idle延长/原idle截止、绝对到期、新Session、源变/隐藏/屏蔽/City expiry、具体约束、未知输入、编码后闭包以及同Tx原RSVP不变。两个实际OS API子进程验证原关联持久和旧预览失效。

native2 registered正常登录链失败为真实Session idle/xmin绑定缺陷，已最小修复；native3为子进程请求缺JSON Content-Type的415 harness失败。native4 LIST为decoded struct Data诊断，不称原HTTP字节；native7/8 LIST/PREVIEW/RECEIPT均直接Body.String记录。native6/7曾输出零Activity createdAt，客户端严格wire RED揭示，native8改为真实来源并逐项核数据库值。全部旧帧保留。

客户端最终target12：45 PASS /0 FAIL-SKIP，14项 analyze0；覆盖原native8 raw HTTP解析、完整旧约束闭集、UNKNOWN仅原GET、同key transport/workspace ABA、迟到详情退休、借用client0close、取消0approve、320px/font3/IME200预览滚动及48dp操作。初次108px overflow真实RED与修复帧均保留。TalkBack、该切片真机、生产身份/部署/真实人试点尚未运行；根代理联合whole/Debug构建另行记录。模型/自动匹配/创建活动/扩大公开范围不存在暗含出口。


## 联合回归增量（2026-10-05）

根首轮whole085发现原Place matching loader手工12个Scan目标与已扩展15列socialIntentColumns不兼容，三个原地点/语义/赞助HTTP测试503。经根精确扩租，仅place_matches.go改为复用原scanSocialIntent；原SQL、owner/session/来源ACL/expiry和旧测试期待保持。该公共scanner原本有host EffectiveStatus，native loader仍额外使用PGclock做原过期/状态限制，不以host clock放行。本轮只修扫描列兼容，未扩大其他时钟行为。原whole失败日志保留；独立Run after_commit故障由根另审，不借此声称全仓绿。

小屏后续安全点只参数化原Page test增加IME260，不改产品源码。IME200/260均320×640/font3实际8个Page目标通过，确认和取消≥48dp、确认按钮位于键盘上方可触达；7文件组合目标46 PASS，test文件analyze0。原45帧、首轮1234 whole客户端/313源以及旧归档均保持历史，新whole/真机由根另核。

此兼容修复 native9 定向64 PASS/0FAIL-SKIP，包含原61与三个旧registeredHTTP；test/vet/build及2CLI0、877+3稳定、完整旧public/catalog/xmin/down/reapply与DROP通过。新25GoSQL frozen2和Dart14 frozen2存独占work；原whole失败/首批归档保留，根后续whole独立。
