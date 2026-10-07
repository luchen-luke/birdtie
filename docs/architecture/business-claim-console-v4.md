# 商家申领与管理工作台

BT-V4-BIZ-003 的实现契约。以当前原生代码和逐项证据为准，本文件不宣布任务完成或真实商家核验。

## 人类管理权限

Business 是独立商家主体，其 Principal 是 business Account。实际操作由有效会话的 Person 承担，不要求 Personal Agent 或组织管理身份，不把 legacy Organization 重新分类。普通路径为个人侧栏的“商家工作台”。组织工作区和匿名状态不展示个人管理资料，返回个人后重新读取。

当前 owner/admin 可提交资料；只有 owner 可添加/改变/撤销成员及转交所有权。独立审核使用 Business 范围、有限时间的 claim/profile/venue 专用授权，任何当前商家成员不能自审。授权来源由受信运营配置；当前隔离测试的显式 SQL 配置不表示真实运营审核已部署，也没有客户端自授角色的 API。

后端按 Business 序列锁、当前来源、按 ID 排序的 Account、成员与授权行、实际 Session 锁及等待后的 PG 时钟重查。列表最后单条来源投影复查此前读取资源，避免等待另一个资源后返回已撤销商家。最终 HTTP 材料编码后再次检查当前来源/会话。事务可能已提交而响应被拒绝，客户端将它视为未知结果，实际读取当前状态，不自动重发。

## 声明、审核与旧领域数据

- 070 是原 041/040 上的增量资料、版本、审计与授权表；保留旧 Business、Account、Agent、Place、公共 Venue、Activity 和旧 ID。
- 经营权申领先 pending，独立审核具体 CAS 版本后才 verified。修改同一商家的声明保留稳定 Business ID。经营权本身沿原模型由明确撤销失效，不能把资料新鲜度期限当作第二种身份。
- 商家名称、介绍、IANA 时区、逐日营业时间与官方链接是明确填写的资料。未填日期表示未知；休息、当日结束、次日结束分别填写。资料 finite validUntil 和编辑版本单独审核，编辑会清除旧核验印记。资料审核不证明实时营业、库存或可预约。
- 场地适用标签、预约链接和说明是商家声明。仅原公共040 Venue 及当前 candidate、Place、City、可选运营 Organization 有效时才能提交或批准。当前运营组织停用后拒绝，并在最终来源帧重查。资料状态、资料期限、原041经营关系与经营权状态分别显示；历史 verified 资料不能自动表示仍有经营权。
- 停用或删除的历史非 owner Person 可以被当前 owner 撤权；新增/恢复权限与转交仍要求有效 Person。070 的数据库例外只允许原非 owner 绑定保持不变、status 变为 removed，不允许变角色、换主体或给停用账号新权限。空 down 恢复原041函数；非空资料/审计 down 原子拒绝。
- 转交的丢失响应只允许原 owner 以当前 admin 身份读取精确已提交、审核记录匹配的结果，不能获得其他 owner 权力。

## 中文界面与具体批准

沿用既有 Material 工作台和侧栏：商家列表 → 当前权威资料 → 编辑 → 检查预览 → 核对主体和后果 → 单次提交 → 重新读取。资料编辑按任务展开，成员更改明确表示直接生效；审核预览必须展示具体来源材料、目标和版本。场地编辑保留已读场地绑定，不从当前城市推断替换目标。

采用 UX-CHECK-04/05/06/08/09/10/12/16。登录、token、组织身份的每次变动清除资料、草稿和批准，包括返回原身份的 ABA；迟到结果无效。编辑页和预览在身份变化时隐藏旧材料。取消预览不写服务器；未知结果通过真实 GET 核实，读取当前状态不证明上次请求是唯一原因。

## 证据与尚未验收部分

实际证据在 `work/v4-biz003`。native-red1/red2、停用成员 red/green 与运营组织 red/green 保存初次失败和修复。当前 `native-final2` fresh001–070 为36 Test PASS、0FAIL/SKIP、test/vet/build0、624观察 Go/SQL/mod 源稳定、全部旧 public/catalog相同且自有 DB DROP。071 在此定向帧仅观察源，不是迁移验证。

schema1 因测试 fixture 未显式给 Account UUID 失败；schema2 实际 up、非空 down exit3且原子保持、空 down恢复069完整 catalog、reapply保持旧行且自有 DB DROP。原生注册 HTTP 覆盖匿名/普通Person/组织及Business账户、申领与独立审核、资料具体版本、成员权限、转交重试及撤销；材料编码后会话/角色变化拒绝，来源变化返回503且不泄漏已编码旧资料。场地 approve/edit/revoke、过期来源、经营权撤销、审核范围和运营组织停用有真实隔离库正负验证。

Client `ui-target10` 实际34功能PASS、0FAIL/SKIP、analyze/test0、9源稳定，包括中文侧栏、360×640字体1/1.6、取消无写、单次提交重新读取、签出与同身份权限/版本变化使批准失效、具体审核材料和跨城市场地绑定。初侧栏溢出及新增fixture选择器失败均保留。`full-client2/3` 各全量391功能PASS、analyze/test0、179源稳定，但 Debug APK 构建因实际Gradle transform缓存损坏失败；旧APK不能充当当前源码构建证据。任务独立缓存已准备，下一完整构建单独记录。

`whole-go071-1` 中途停止，无最终测试输出/退出状态，不计通过；确认没有测试进程及数据库连接、核对自有库保留资料后仅清理对应自有测试库，记录在 `interruption.json`。重新验证记录在 `whole-go071-2`，其结果以实际终端收据为准。新runner即时记录数据库所有权、PID和流式测试日志，避免中断后丢失原始输出。

当前全 Flutter/Go 最终回归、当前APK、真机管理闭环及辅助技术仍待终端证据。此前ADB为空；最新 `adb-device-return-20261003.txt` 记录设备已返回，不表示已安装或验收。独立 Business Agent、公开问答/模型资料用途和赞助授权不由此工作台授予。真实经营材料、运营审核员、生产身份和正式试点仍缺，Closed Pilot/Consumer Beta 保持 NO。

## 2026-10-03 根代理最终本地核证

上述失败与待验收段落保留为历史帧，本段记录当前结果。修复真机发现的“返回修改”只关闭预览问题：返回实际编辑器、保留具体版本草稿、取消旧批准，再预览须重新确认。编辑器绑定进入时的会话 generation；撤权或切换主体后不恢复旧草稿。真实 RED2 后 ui-target11 为34功能PASS/4加载、0FAIL/SKIP、analyze/test0，360×640字体1/1.6覆盖。

当前 whole-go071-3 默认并行 go test ./... -count=1 -json 为8487 TestPASS/0FAIL/SKIP；vet/build0，001–071与保留数据验证后631源稳定、全部public行及catalog不变、自有测试库DROP。native-final2 仍是070专项36；schema2包含空/非空down与reapply，不把071观察冒充该帧迁移运行。

full-client5 全量 analyze/test/Debug APK build0，497功能PASS及75加载事件、0FAIL/SKIP、179源稳定。APK SHA256 `1119c68621eafb5a5e3f4ca1a9c6f10d9687f9b512ce3582cddc9f52d7074175`，Android16真机ADB install -r返回Success。full-client2/3 Gradle transform缓存失败保留；最终使用任务独立Gradle缓存，没有修改产品依赖以绕过失败。

当前真机通过普通中文入口：个人侧栏→商家工作台→资料编辑→具体版本预览→返回实际编辑器→草稿保留→重新确认提交→读取同一Business。隔离合成库取消前后整个profile相同，version1/audit5；提交后version2/pending/audit6，不继承审核。权限拒绝/未知结果/会话撤销由原生与组件测试覆盖，不能描述成全部真机场景通过。

本轮是CODE_AND_LOCAL_SYNTHETIC_VERIFICATION。真实商家经营材料、受信运营审核人员、生产身份与正式试点未提供；独立Business Agent/模型资料用途不在此工作台内启用。TalkBack、景观方向、真机撤权和App/API重启未运行；真实CSSA A→H未运行。Closed Pilot Ready=NO，Consumer Beta=NO。最终来源与收据见 `docs/testing/evidence/business-claim-console-2026-10-03/root-final1/manifest.json`，其中原始工作目录保留完整失败与测试日志。
