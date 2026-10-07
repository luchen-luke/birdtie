# BT-V4-ADS-001 审计

2026-10-03，正式官方仓库。live task由根领取，20生产写范围与071唯一迁移已登记；worker只改专属源码/测试/本报告，root唯一改live队列与共用报告。最初审计/精确范围见 `work/v4-ads001/audit-plan.md`。

真实基线：OPP当前原生Human源/五层固定自然排序、PlaceMatch的Venue硬边界/067语义分、Now按原查询及ResultSet/Pin稳定投影均已存在；Flutter活动机会页和Now结果sheet已有可消费路径。尚无商业赞助记录、授权审批、Sponsor API字段、赞助UI。Business verified不是赞助事实。place-matches暂无独立消费UI，不把Now地点页称为它的既有页面。

本项计划增加本地真实有限公开商业展示声明，Owner/Admin明确提交、独立专门权限Reviewer审批、native持久化当前读，三个API把Sponsored区块与原自然数组分开，两个现有UI明确中文披露。不接收费/生产投放。071须独立schema约束/不可变/CAS/up/down/完整旧数据保护；缺该真实source链不能凭pure metadata或无广告空数组称完成。

## 实施核对

| 能力 | 当前实际范围 |
| --- | --- |
| 独立赞助事实 | REAL_LOCAL：071实际表、专门有限review_sponsorship授权；五条registered HTTP与当前Person/商家/City reviewer链（City为真实类型，非人位置） |
| 声明/审批/撤回 | REAL_LOCAL：pending→独立approve/reject→当前持久读→revoke；完整确认、CAS/op幂等、source/authority反ABA |
| 自然排序 | 复用原Opportunity/PlaceMatch/Now，源和排序算法不修改；真实注册前后比较原data/reasons/Entity refs/ResultSet/mapEffects |
| 三API消费 | REAL_LOCAL：活动机会、地点匹配、Now创建/恢复，只有原自然供给中明确公开target可进入独立lane |
| 两UI披露 | 实际Dart接线、严格封闭DTO解析、中文badge/支持方/来源/期限、固定详情路径；target测试通过，全Flutter/真机单列 |
| 管理UI/付费/生产投放 | NOT_IMPLEMENTED/NOT_ENABLED，本任务不把API称完整商家运营工作台或现实合作核验 |

## 原始失败及修复

所有`work/v4-ads001`原labels保留，不覆盖或把夹具错误当权限RED：初scope PowerShell parse未执行；native2迁移调用未安装pgcrypto函数，改为PG内置SHA256；native3 Activity原夹具缺HostLabel，改真实CreateSocialDraft/Publish；native4参数`$3`类型未知，补显式text；native5过期Session测试违反现有idle≤expiry约束，改实际有界到期等待；native6 spy token格式和新City缺CityContext，补合法wire令牌和真实描述Context；native7 Venue suitability缺项和未支持query词；native8恢复路径错误，使用原/me/agent-tasks。native9非空down全表比较被另包并发写夹具干扰，复用既有ownedMigrationDatabase隔离，不减断言/不改-p并发。

`native10`真实fresh071两轮146 Test PASS、0FAIL/SKIP。`native11`新增审计INSERT等待后的屏障，实际全部拒绝/完整行保持，但结尾计数SQL复用text/uuid参数导致PG42883，失败两轮保留；补显式cast后`native12`两轮各152 Test PASS/0FAIL/SKIP。target vet/build0、旧非空/全部public完整行升级与测试后保持、四种非空down、空down/reapply、RR/Honolulu pool+正常会话刷新与隐藏来源失效、audit写等待后五个晚期边界通过。14own源稳定，根并行测试变化造成allAPIHashObservedStable=false如实记录。

首全Flutter analyze发现worker四处braces info，已修并format，根重新全Client复验；此新Dart帧不冒用历史target6的40功能测试作本轮全量证据。最终源码SHA/不可变归档/后续共同回归见证据目录。

## 边界与后续证据

局部纯/传输fixture不充当前Native会话或现实商家事实；全部原生数据、商业声明、专门授权均标LOCAL_SYNTHETIC。Store重新构造的持久化实测不称App/API进程重启。全默认Go和全Flutter由根协调共同源冻结；真机、TalkBack、外部商业合同/付款、IdP/CSSA/正式部署未测不自动PASS。Closed Pilot/Consumer Beta继续NO。

根BIZ独占server/Sidebar/Map；其五条窄路由注册作为共享源观察，不归worker自行修改。原自然源/算法无写入；只有消费桥的独立商业DTO接线变化，保留旧实体和领域动作。

## UTC DTO 最后补齐与不变源码帧

旧 `_stamp` 用 `DateTime.tryParse` 会将无效日历字段归一化；实际 `client-utc-red1` 以非法 25 时的 `checkedAt` 证实消费解析缺陷（exit 1）。这不代表原生授权失败。修复为封闭 UTC RFC3339/RFC3339Nano 语法与公历字段回查，保留 Go 1–9 位小数在 Dart 的微秒表示；四字段分别有非法年月日时分秒、格式及世纪非闰年负例，有 0004/2000/2024 闰年和所有 1–9 位小数正例。

`client-utc-green1` 两测试文件成功，但首次定向 analyze 有四处测试字符串插值 braces info，实际失败日志保留；修复后 `client-utc-green2` 两测试文件 exit 0、`client-utc-analyze2` 七文件 exit 0/no issues。精确计数与当前 SHA 在 `work/v4-ads001/client-utc-results.json`。本次只修改两份 Dart 源/测试；实际完整生产帧是 **14 个 Go/SQL + 7 个 Dart = 21 文件**，不是 23 文件；所有 14 个原生文件与其余五份 Dart 与历史 final1 相等。final1 不覆盖，final2 单独封存差分。当前全 Flutter、Debug 构建、手机和辅助技术仍不得从本定向结果推定完成；live 队列与共用报告由根代理核证更新。

## 根代理最终当前帧结果与范围

worker重新读取 `work/v4-biz003/whole-go071-3/result.json` 和命令：完整默认 Go 8487 Test PASS、0FAIL/SKIP，test/vet/build0，631源稳定、旧完整public行/catalog不变、测试库已DROP；`full-client5/result.json` 与命令：全analyze/test/Debug build0，497功能+75隐藏加载PASS、0FAIL/SKIP，179源稳定。最终APK SHA `1119c68621eafb5a5e3f4ca1a9c6f10d9687f9b512ce3582cddc9f52d7074175`，`phone/install-full-client5.log` 实际 Streamed Install Success，exit0。

实际读取并查看手机 `sponsored-disclosure1` 节点/截图：中文赞助独立区、支持方、来源、期限、合同付款未核验说明清楚，原1220×2656截图来自实际构建。`sponsored-detail1` 展示同名合成Business活动原详情；原动作保留不代表本次都已执行。实际比较 `organic-before/after.json`：natural data三条完整相等，Sponsor独立lane从0到1，typed ACTIVITY与原自然ref一致。phone只证明该本地活动机会页披露与详情，不把Place/API或Now目标测试扩张成手机全场景成功。14GoSQL/7Dart与根当前绿帧精确源核验结果在专属 `root-final-evidence-review.json`，immutable final3只追加精选证据；final1/2不变。

原AC的本地ranking/UI disclosure已有当前源码、原生/全量/实际手机证据，最终队列判定仍由root作出。所有source为LOCAL_SYNTHETIC，设备安装是development Debug：TalkBack、正式App/API重启、真实用户独立观察、真实商业授权/CSSA/支付/生产试点未通过或未测，不提升Closed Pilot/Consumer Beta（均NO）。历史失败原labels继续保留，不改成PASS。
