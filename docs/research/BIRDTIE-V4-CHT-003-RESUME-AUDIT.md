# BT-V4-CHT-003 增量恢复审计

2026-10-03，IN_PROGRESS。原 PARTIAL 完整对象保存在 `work/v4-cht003-resume/original-partial-task.json`，既有 044 状态／历史未清理。MOM002 实际已 DONE，旧契约的“公共发布未实现”不再代表当前仓库；恢复审计时公共稳定 Moment 详情、七类聊天卡跳转及无现成对话的已接受好友分享均有缺口。以下计划现已实际实施，当前冻结回归与真机限制见追加核证记录。

## 当前边界与计划

1. 复用原明确人类 Moment 发布源，新增闭合只读公共单 Moment DTO／API；不复用含私人元数据的 content.Moment，不输出 occurredAt、Context、Memory、活动关联或作者私人 Profile。稳定详情不受发现摘要的 30 天／5 条窗口限制。只读验证 active Person author、本人明确公开、实际 Place／City 当前公开与期限、双向 block、提供的 Session 当前有效；错误会话不能降匿名。
2. 七类型稳定 ID 路由复用 Activity／Place／Person／Community／Organization／Business 详情；补公共 Moment 详情与分享入口，PlaceHistory 保留既有 id/revision 不丢弃。相同 ID、目的页面重新读取权限与当前来源。
3. 好友选择复用已接受 FriendTie 与 startFriendConversation，选择前不发消息；显示具体收件人与目标，账号／组织／目标变化后旧批准失效。
4. 原 sendEntity 返回 void、无操作 ID，按 entity/title/时间查最近 100 条无法确定本次是否提交。074 仅在原 conversation_messages 加可空操作键、原行恢复与不可变已键回执；不造影子 effect ledger。新分享路线带当前 Session digest，等待 sender/conversation/通知后最后复核本人、双方和来源；同键重试不重复消息／通知，不同键同目标允许再次明确分享。未知结果先按原键读，重试仍同键。
5. 本地 pending metadata 只在既有安全存储保存必要稳定引用，用于本人恢复／核实，不把未提交操作自动执行；换账号／组织和已变目标不能复用旧批准。辅助技术、大字／小屏／迟到及真实截图分别核验。

根拥有共享 server／Map 与消息源、精确新增 074；另一独立 AGE053 worker 拥有 073 和组织证据源，不竞争文件。两线冻结后再跑当前全 Go/Flutter 与 fresh/current-data migrations。前批 Go8506／Flutter564 是 ORG003／BIZ004 冻结证据，不覆盖本轮新源。

## 已发现的失败与修复

初次 PublicMoment build 使用不存在的 identity.AccountTypePerson 常量，以及工作目录已为 apps/api 却给 gofmt 传 apps/api 前缀；均为真实编译／命令失败。核查实际 identity/identity.go 后使用原 `AccountType == person`，正确相对路径 gofmt 与 go build ./... exit0。此编译不等于原生功能测试或验收完成。

旧聊天 Moment ACL 只检查 published/public/活跃作者，缺确认、Person、Place／City 与双向 block，已纳入真实修复和负向回归。旧 native sender 只传 actorID、等待后无 Session 复核，旧 API 并无未知结果幂等证据。独立只读审查给出以上缺口；未靠文档／fixture 硬标完成。

## 发布限制

只仓库与隔离本地／合成真机验收，不发送给真实用户或开放 Agent 自动消息。生产 IdP、真实组织／活动、HTTPS／生产地图、部署日志与值守、可靠调度运营和真实 A→H 缺失，Closed Pilot Ready=NO、Consumer Beta=NO。TalkBack、真实未提示用户观察、App 重启恢复与当前 native 功能测试尚未运行，继续如实记录。

## 2026-10-03 连续并行核证（root snapshot 13）

唯一 live 队列253项：DONE136、IN_PROGRESS2、PARTIAL13、TODO89、BLOCKED13，尚未完成117。P0 147项：DONE88/IN_PROGRESS1/PARTIAL11/TODO38/BLOCKED9。两项仍在根核证；未用定向或历史构建直接标DONE。任务与既有证据未重置；当前读 next --parallel 没有满足全部依赖的第三候选。

- CHT003 七类稳定ID详情/返回、真正公开单Moment九字段、已接受好友具体中文确认、持久必要引用与原键未知结果恢复已实现。原消息/ID与纯文本兼容；044+074原消息是真源，未造影子账本。真实 chat075-2：33PASS/0FAIL-SKIP，test/vet/build0、671源稳定、完整public/catalog未变、ownedDROP true。包含七类原键、117历史中原卡落在最新100窗外仍可恢复、actual registeredHTTP、不重复消息/通知、撤回已提交事实、真实锁等待后撤销/到期/移除关系/屏蔽/地点隐藏零新效果。schema074-1：fresh001–073+old068/个人及四类组织历史保留；074up旧ID/全数据保留、非空down原子拒绝、带键原消息不可改键/目标、空down全catalog恢复、reapply等值，自有DB清理。
- Flutter全量 whole-client1：analyze/test/Debug APK build0；623功能+86loading PASS/0FAIL-SKIP、199源冻结。APK SHA256 35a632fe637447d5141fbedc4b45d4399f2e0f2d5a216337ffe1509525042b25。已修真正首帧ModalRoute生命周期错误、身份ABA残留批准/旧收件人AppBar、隐藏旧会话绕原恢复、未知原收件人禁重试、损坏恢复缓存阻新发送、键盘大字8条恢复滚动；当前32回执/预约与35路由/分享/Human负向定向测试通过，均被上述全量覆盖。实际首轮失败/生产修复保留，未删除断言；错误 root cwd Flutter analyze扫描大量归档属错误命令，不作为客户端通过，实际apps/client分析无问题。
- AGE053五来源实际实现：profile/activity/admin input/public FAQ/075原生版本化ANN，沿057 Evidence账本StrictOrg边界。073/075/预览/发布/撤回、当前源权限和过期重新核对，REMOVED控制保留保护075down，模型/客户端confirmed不授予业务权限。worker native6 242PASS/0FAIL-SKIP、4目标test/vet/build0、671全部源stable、18ownedstable、完整public/catalog和旧PERSON2及四ORG8行保留、down/reapply全catalog复原、ownedDROP true。此前worker-final1/2保留，新final3独立71文件证据948bf9efa37bb21cefa38ed5ea8ff71fa03f4855dde7ced9beb1770d7b1187cf。独立复核实际发现非UTC和最终Session等待缺口后修复：旧批准JSON字节保留，timestamp按同瞬间/其余字段精确比较；最终payload同SQL检查原SessionDigest/snapshot，真实pool等待后撤销、absolute/idle自然到期、SessionID/AccountupdatedAt ABA不给正文。公告UI/广播运营/模型/最终table-lock专例未验，不称这些能力完成。
- 当前全Go075-1初轮8753PASS/1FAIL-SKIP0，失败为原063 CurrentDataMigrationRoundtrip 45秒context timeout，vet/build0，670源稳定与全public/catalog不变、自有DB已删。不放宽旧断言；root独立新ANN242与全Go075-2正在顺序核证，避免同时迁移runner争用。早期074全Go8705通过属于历史冻结帧，不能替代当前075回归。root ANN独立1只是核验runner引用fixture路径错、ownedDB已删；修正确来源路径后独立2实际230PASS/迁移往返全部绿，其为公共修补前帧，不冒充最终242。

ADB devices当前没有设备：本轮新APK未安装、新聊天流程/手机App重启/真实截图未验；旧APK9真机证据只对应旧ORG003/BIZ004。TalkBack、真实未提示消费用户、生产运行与CGO race无GCC均NOT_RUN。Closed Pilot Ready=NO，Consumer Beta=NO；真实IdP、已核验组织及主办方授权活动、HTTPS/生产地图、部署日志/独立提醒调度运营/值守/备用联系和真实部署A→H仍缺。未部署、发布、联系合作方、改外部服务或使用真实用户。

## 2026-10-04 当前真机核验（本地合成，不是生产）

根已明确恢复原PARTIAL并保留原对象。Android16设备c641566b实际连接隔离076本地API/DB，原72 API和数据库未改。七类卡→同ID权威详情→原聊天返回实际截图对应e52 APK；初始7张由本地API创建，不能说UI发送7类全部通过。具体收件人批准/真实UI发送/未知提交恢复单独验证。

e52 APK：代理只丢真实201提交后的一个响应；原键55a85574-55f5-4440-92e1-ab6e2c6bda91，消息06f0eb76-8aa0-46c9-b6f8-8df7e90cfa27。强制停止并重启后安全存储原引用仍在，UI GET请求363e65705b9efe6d8a740ffd230f8776恢复相同消息，SQL该原键1条，当时总8条。另一次明确新批准正常分享成为9条；初次过期准备SQL语法失败，不能把其后正常分享当过期拒绝证据。正确准备后401且无第10条。原owner未知记录在另一账号聊天未出现。

实际发现旧成功提示与新未知结果并存：保留widget RED，具体批准新提交/核实开始清除旧result/error。client076-device3 analyze/test/Debug构建0、624功能+86loading/0FAIL-SKIP、199源冻结；第一次device2全局Gradle缓存失败保存，使用已有隔离缓存成功。新APK d1b7360ed8e171f3edd92c723513b59b89bd597cc9e7656ea2c588f2eaafc709已ADB install Success；实际UI新确认201请求9225afe9beb55cfb661897a8b67a0dac成为第10条。正确独占合成会话到期后再次明确批准得到401请求44fbbd50e0a5b4285250bc4ffdfe356e，数据库仍10条，截图确认只显示新待核实、没有旧成功。

字体200%可达有实际截图。TalkBack尝试被首用教程抢前台，不能计Birdtie读屏通过；音频听验NOT_RUN。原secure服务空字符串/accessibility_enabled0/font1.0精确恢复。没有安装输入法/清除安全存储。设备已重新通过原本地测试登录恢复有效本人会话，reverse3697→6623、force-stop再启动，并实际Flutter attach会话35429；DevTools地址来自真实输出，任务面板打开请求已queued。服务/调试留供已授权继续验收。

CHT003正常、未知提交、App重启、身份切换与本轮修复取得实际证据；辅助技术和未提示真人消费观察仍未验，完整任务不可仅凭安装标DONE。适用UX-CHECK-01–16按范围留证。开发手机号/固定测试码不证明手机号所有权；组织/商家verified仅隔离库合成状态夹具，无现实核验。Closed Pilot NO / Consumer Beta NO，模型和自动动作关闭。
