# 当前20检查点18：19包编码整合完成并编译；联系人分页与Web地图重叠点已接线

依用户最新要求，本批先编码、后合并相关测试与一次编译，没有重复真机五状态、截图、录屏或语音试录。原BT-FIX-NOW-UI-001继续IN_PROGRESS，256项、183 DONE、73未完成保持。

**本次实际代码**：好友、关注、好友/消息申请接入真实SQL游标分页，首批20条并按需继续加载；分别保留列表、游标与错误，后页失败保留已有行，身份/来源变化退休旧内容与迟到响应。申请审阅及决定前核验通过当前授权单ID接口，取消旧“最近100条”造成的详情不可达。服务端继续校验本人会话、参与关系、双向屏蔽、资料可见字段以及返回前状态；旧接口保留，无新迁移。Web实际FlutterMap图层复用有限同坐标展开与真实相机投影，地点/动态保持真实经纬度和原ID，独立48dp命中及当前对象选择，来源撤回和身份变化使旧回调失效。Android18地图修正原样随19编译。

**实际编译**：Android profile19与Web profile19均退出0，完整客户端源码帧一致；Web产物41文件。全部9个Go命令程序一次构建成功，API06为其中api.exe精确副本。APK SHA256 `f4db9482364c15c3e2170116d5e58c7a30097d15006de70c1786c2241527f2f3`。已保留数据安装到已连接手机，配套本机API06。开发profile包，不是正式发布。

**必要验证**：本批10个直接相关客户端测试文件已覆盖通过（7个首次通过，3个修正脚本后定向复跑；未重跑完整套件），修改文件分析退出0；Go联系人相关测试/子测试7通过、0失败、0跳过，包含超过100条、稳定分页边界及实时权限变化的独立临时数据库案例；未迁移或重建原手机数据。完整命令/哈希/边界见[本批收据](../testing/evidence/now-v4-v41-integration-20261010/continuation20/final19/receipt.json)。19真机功能与Web浏览器验收NOT_RUN，不把旧18地图PNG改称19验证。

[剩余清单](../testing/evidence/now-v4-v41-integration-20261010/continuation20/final19/REMAINING.md)继续保留真实头像、其他材料创建、Agent图片理解、系统推送、二维码期限及同包权限/草稿/辅助技术等缺口。混合语音USER_DEFERRED，仍未通过。本轮无生产部署、无外部付费调用。

---

# 当前20检查点17：18包已编译安装；地点、动态及关联内容实际可见

用户要求先暂停混合语音验收，继续其余编译和清单、优先修复地图Pin与动态不显示。本批无录屏、无录音、无发送。原任务 **BT-FIX-NOW-UI-001 / IN_PROGRESS**，256项、183已完成、73未完成保持；没有将编译折算为完成。

**当前构建**：Android profile18 / install24，APK `faadd291542f96d646f732660976c3146652811d103ec4a6aab02d22e4aded39`，Gradle68.0秒；Web profile18退出0、41文件，两者完整客户端源码帧一致。当前地图直接测试45通过，修改位图文件分析退出0、无问题。16轮地图/数据相关129通过、17轮43通过属于各自源码帧，不冒充18全量重跑。API05及迁移121未变，本轮重新核对9个Go程序的已有成功构建与当前API源码相同，不重复编译Go。Windows/iOS/正式release门槛仍保留。

**实际修正**：原生位图按设备DPR生成，避免高密屏上缩成约2.5dp；不透明的圆形地点与方形动态保留独立ID。同坐标动态在真实SDK投影处有限展开、避开附近地点，不更改经纬度。profile/release下诊断字符串被裁剪导致不同offset误命中同一缓存的问题，已改为结构化数值键；连接线只画在图标边缘之间，中心地点不再被穿线。公开地图卡片也不再因另一私人数据源单独失败而一起隐藏，旧私有来源仍按原权限清除。

**18同包真机结果**：实际PNG中3个完整地点圆、7个独立动态方块可辨；短按动态并选择指定03后进入该条公开动态正文，关闭回到地图；Gallery地点入口可打开其公开动态预览，再关闭回空Agent输入。均为既有本机合成验收副本；不是生产内容或对全部密度场景的证明。证据：[地图显示](../testing/evidence/now-v4-v41-integration-20261010/continuation20/phone/map18-visible01.png)、[指定动态正文](../testing/evidence/now-v4-v41-integration-20261010/continuation20/phone/map18-moment03-detail01.png)、[地点与动态预览](../testing/evidence/now-v4-v41-integration-20261010/continuation20/phone/map18-gallery-preview01.png)、[本批收据](../testing/evidence/now-v4-v41-integration-20261010/continuation20/final18/receipt.json)。

**保留真实失败**：16真机虽3个地点可见但动态只有1个方形，未通过；17修正缓存后7方可见，但Gallery圆仍被连接线遮挡，18才修正。17的150ms合成press没有触发，随后短tap真实打开对象选择；没有把无响应动作写成成功。所有旧PNG/XML与失败测试记录保留。用户既有“已清空，无录音”之后，每次安装前仍核对实际空输入、mic待用，按保留数据安装，无重复索取确认。

**地点关联内容**：Gallery预览先显示7条公开分享；两次等待期间旧预览已退役，点击前没有有效按钮，因此动作未执行。预览使用原地图快照剩余期限（服务端上限2分钟，可能更早），不能从截图间隔推定固定60秒，也未延长旧DTO。随后正常关闭、从当前地图重开，在有效按钮出现后立即进入，实际显示首批5条中前两张卡及“继续查看动态”入口：[18地点动态卡](../testing/evidence/now-v4-v41-integration-20261010/continuation20/phone/map18-gallery-stories03.png)。没有把入口可见当成本轮全部分页已经跑完。中间滚动helper首版因过滤匿名scroll节点而在动作前拒绝；保留原XML，按实际完整XML中的唯一ScrollView定位后正常滚动，产品源码未因此改变。

**剩余工作**见[更新后的清单](../testing/evidence/now-v4-v41-integration-20261010/continuation20/final18/REMAINING.md)：真实头像、好友/申请/关注完整分页、部分材料/创建链、Agent看图、推送及二维码期限合同尚缺实现；五状态、双账号媒体/权限/切号、非空草稿恢复、真实AT与性能等仍缺最终同包证据。Web同坐标展开尚未实现；超过8个外围对象会复用槽位，仍通过对象列表保留可达性，不宣称114个对象能同时完全分离。混合语音按用户要求 **USER_DEFERRED，仍未通过**。未部署生产。

---

# 当前20检查点16：15包已编译安装；近期联系可读，重试裁切已真机修复

当前安装为 **profile-glass-build15 / install21**，APK `f5bcb8a143db5b4e77a3ec675db5ebe9c8afc1fa1a55e2378126595703d3ff1b`。Android profile编译退出0（Gradle57.6秒），Web profile编译退出0（41个文件），两者完整客户端源码帧一致。修改文件分析退出0、7条提示级风格项，无错误/警告；本次社交错误态及宿主直接测试83通过。API05、121迁移、base模型未改，不为Dart布局修正重复编译Go或重新迁移数据库。Web编译不是浏览器/Wasm运行验收，Windows/iOS/生产发布原门槛仍保留。

**这次已实现并验证的变化**：联系区256dp，一行姓名与一行真实消息预览，最近四位/未读标记，长名省略、最大高度及内部滚动；保留地图、小图标/48dp命中及统一输入。14的图片菜单/预览身份退休修正也随15编译，已有16个新例与相关86项通过记录。15额外把compact错误态改为自然高度与单层整体滚动，完整错误文案和至少48dp重试按钮不再被56dp状态窗裁切。新几何测试直接检查按钮和字形处于视口内，而非仅检查无异常。

**15真机实际结果**：近期列表的普通姓名/预览可辨认，通知切换仍只有一个右侧区域。再次临时移除仅QA反向端口，服务保持ready；真实界面显示连接中断/重试而非登录，PNG确认“重试”字和按钮完整可见。finally恢复原端口，明确点击重试后实际返回近期列表；Agent为空、麦克风待用，未开始录音、未发送内容。证据：[正常近期](../testing/evidence/now-v4-v41-integration-20261010/continuation20/phone/final15-contact-open01.png)、[错误态完整按钮](../testing/evidence/now-v4-v41-integration-20261010/continuation20/phone/final15-network-error01.png)、[核验收据](../testing/evidence/now-v4-v41-integration-20261010/continuation20/contact/final15-visual-review01/receipt.json)。14裁切失败原件没有删除，14受控过期/登录返回等旧结果不冒充15重跑。

用户最新直接确认“已清空，无录音”（call_ef9e2f90897c4c40931d7dae8e3aa2d6），之后14身份恢复截图再次证明Agent为空，期间未引入新稿或录音，因此15沿既有安装授权和保留数据方式安装，没有重复索要确认。手机当前安装15、测试API正常联网、联系区正常。全过程无录屏，未部署生产。

**仍未完成的代码/能力**：中英混合转写保留英文仍失败，13用户反馈和实际原文保留；没有用硬补词表伪造准确。真实头像字段/有界缩略图链尚未接齐；好友/申请/关注完整分页、视频/旅程/主题地图统一材料入口、Agent图片理解/图片独立提问、系统推送接线仍有范围缺口。当前联系人码是稳定公开UUID，无到期合同，不把坏码恢复冒充过期码处理。

**仍未完成的同包验收**：最终包完整五状态（访客零私人请求审计、确证无好友账号、过期恢复等）、双账号图片和文字/AAC、扫码申请到接受、关注/屏蔽/读取权限、切号在途内容隔离、非空草稿/材料/收件人/阅读位置恢复，以及收藏/分享/发布共用身份各入口。读屏、大字号真机、真实SDK密集/同坐标点、性能和中低端/iOS设备没有由局部截图替代。旧09/10语音、12扫码、13语音、14身份切换证据按原构建保留。

**原 BT-FIX-NOW-UI-001 继续 IN_PROGRESS**；原256项及73项未完成保留，没有重建队列或将可编译改成已完成。完整历史、失败、测试、构建与交付边界在下面各检查点可追溯。

---

# 当前20检查点15：14包已编译安装并核验联系区；错误态按钮裁切继续修正

Android profile14 与 Web profile14 编译均退出0、同源码帧；Android APK `ddcb7f9078ab3d432dd87a29f75959a033f10c7dca52689ae825c45546fc5b3d`，install20已保留数据安装。Web41文件仅为编译产物，不声明浏览器运行或Wasm通过。用户再次确认“已清空，无录音”，无录屏。受控API05/121不重复构建或迁移，仍未部署生产。

已应用图片菜单/预览身份退休修正，包括旧收件人文字与像素清除、首次显示前销毁、构建期销毁、主题变化和解除订阅。16新增例与原输入/社交宿主共86通过；右侧256dp姓名/单行预览/四近期/内部滚动直接测试37通过。三个修改的生产文件静态检查退出0，仅14条提示级历史风格项。首轮社交批次85通过3失败（两语义句柄回收时机、一大字懒列表定位），仅修测试清理与实际滚动，所有原断言保留，第二轮37通过；失败原件保留。

真机14已拍近期、全部会话、联系人、通知、网络异常和过期/登录返回。普通名称及预览可辨认、长名一行省略，地图保持可见；仅一类列表，联系人内好友/关注和添加入口可达。资料仍是合成账号和中性头像占位，不能叫真实头像交付。切换前后当前API会话页、通知页、好友、关注回包完全一致：消息未读1、通知未读0；没有恢复旧未读数字。私有API测试token最初401已保留，随后明确刷新该本机合成身份；没有把401当网络错误。

网络只移除受控QA反向端口、服务持续ready，界面显示连接中断/重试而非登录；finally恢复端口后按真实重试重新出现近期列表。截图也明确发现“重试”下半字被56dp状态窗裁切，故这一视觉状态FAIL，不能因无RenderFlex异常或节点可点击写通过。局部自然高度/单层滚动候选已准备，尚未应用于14。

两条精确本机primary有效session被受控过期，API明确401，其他账号session内容hash不变；手机自然打开单次可见登录窗口。经测试手机号重新登录回原联系区域且Agent为空，未自动执行发送或好友动作；非空草稿/完整零请求审计没有由这些截图证明。原13混合语音仍未通过，未对识别文字硬补英文或再次要求试录。

原BT-FIX-NOW-UI-001仍IN_PROGRESS。五状态尚缺本轮访客零私人请求审计与确证无好友账号的完整链；双账号图片/屏蔽/扫码申请完成链、真实头像和完整分页、AT/性能/发布等仍按实际缺口保留，不将本批编译换算成全部完成。

---

# 当前20检查点14：13包登录返回已实测；混合语音与右侧宽度仍未通过

原任务继续 IN_PROGRESS，原256个任务和73个未完成项不变。13包、API05及121迁移仍对应检查点13，未将旧包结果转记为新包。

**13登录返回实际结果**：访客展开区域只有一条登录引导及一个按钮；经过受控本机手机号登录回到原联系区域，单一Agent输入框仍为空，四个近期姓名与随后读取的当前API相符。原验证脚本在完成登录和截图后因“收起联系，有未读”比预期多后缀而断言失败；没有重复登录，另封存真实结果。登录前API回包未落盘，因此不声称未读计数前后不变。证据：`continuation20/contact/primary-ui-login13-02/receipt.json` 与 `phone/final13-primary-login-contact-return01.png`。这仅是本机合成身份，不是生产认证验收。

**13语音实际结果**：当前草稿为“帮我找一下附近安静的咖啡店并且有Wi-Fi”，用户确认说的是中文“安静的咖啡店”；另反馈说英文“quiet cafe”也未识别成该英文。简体与当前Wi-Fi可见，英文保留仍未通过；用户未明确确认本次可编辑及清稿，不填充未知证词。一次验收窗口出现三套完整数值日志，准备成功各90–99ms，JNI调用分别4411、4824、4495ms；它们含语言处理和解码，不是纯解码耗时，且没有jobID/声音/转写原文，不能把其中一组指定为最终截图。原单次helper保留“不完整或多次”的判定，额外记录三组可观察阶段而不改写原证据。未录屏、未保存声音、未宣称稳定准确率。证据位于 `work/now-v4-v41-integration-20261010/continuation20/contact/base13-phone-prep/runs/mixed01/`。

**本轮视觉与隐私缺口**：13真机联系区四行可见姓名均截成“Now20…”，仍过窄，不满足可辨认姓名与一行消息预览；正在准备局部宽度/行距修正。代码复核另确认图片菜单/预览在身份或来源异步退休后可能残留旧收件人姓名，原图片像素和动作保护不等于标题已清除；正常设置登出需先离开模态层，尚未声称真机复现该异步路径。最小候选及真实构建期通知测试正在复核，尚未应用和编译，不能写为已修复。它们将在该13验收封存后合并处理。

双账号好友图片、扫码申请完成链、屏蔽/权限/换号、同构建五状态、真实头像、完整分页、读屏及性能等仍按实际证据保留未完成；Windows/iOS/生产发布门槛未解除。

---

# 当前20检查点13：地图/玻璃、好友图片和base语音模型已整合编译安装；同包验收继续

原 BT-FIX-NOW-UI-001 仍 IN_PROGRESS，256个任务与73个未完成项保留，未新增重复队列或把编译成功改称完成。以下历史检查点按原字节保留。

**实际新构建**：Android profile13与Web profile13均退出0，完整客户端源码帧一致；Android APK `b88ea13e88dac63949a79bdcd5b6dc1cc128c03b51649eec1ca3d835e0df1cfb`，install19按保留数据方式安装。Web只证明JavaScript编译，未声明Wasm或浏览器运行通过；本轮使用本地Web运行资源。9个现有Go程序已按batch13源码重新编译通过，匹配API05 `ff986cf22c934a87f183df6ee5def2e23b8602b992bdfc13c075892b93cff4bb` 已在原loopback测试环境运行。旧101个Go测试程序编译属于12检查点，不转记为13重新运行。Windows symlink、iOS/Mac环境及生产发布门槛未解除，未重试未改变的前提。

**代码与直接测试**：地图每个获准地点/动态保持独立ID，圆地点、方动态；同坐标通过真实SDK投影选择对象，不修改坐标。地图MOMENT直接打开该动态，原Explore按地点分组列表仍保留地点预览。修复受控作者跳转误退休，同时追加外部作者/账号/来源撤回再恢复的旧回调拒绝测试。地图5套31通过；侧栏身份131通过；触摸/地图租期14通过。租期实际失败来自动画耗时跨第二个合法周期，测试按原deadline检验提前续读、跨旧期限、精确请求次数与原DTO不可延长，没有改变产品租期。

局部玻璃材质复用共享token和组件，控制/输入/侧栏分工，正文保持清晰，减少效果/高对比降级与48dp、200%字号有组件覆盖。没有全图模糊、第二地图或第二输入框。Flutter材质不等于原生Apple Liquid Glass；新13性能和实际读屏仍未验收。

好友图片实现统一输入框的本机选图/遮盖/图片单发及带字、受控重编码和缩略图、真实双方权限与屏蔽检查。发送结果未知保留原operation与hash记录，404不当取消或自动重发。补齐重启后只有图片元数据时继续明确文字/语音的恢复分支，未知图片记录仍可核实，新图片不覆盖原记录，录音期间禁止核实使停止按钮失效。最终直接图片/会话/输入测试见 [本轮测试](../testing/evidence/now-v4-v41-integration-20261010/continuation20/client-image13-recovery-tests13/receipt.json)，修改生产路径分析退出0。以前失败/挂起日志保留，未删测试来冒充一次通过。

**本机服务与数据**：真实PostgreSQL图片测试13通过、0失败、0跳过，包含121迁移/有数据拒绝down、不可变字段、跨类型幂等、并发限额和媒体互斥。随后只在既有精确owned phone DB应用121，原37条消息、1条音频、168条通知与168条inbox的数量和服务器内内容hash前后一致；新图片表为空。API05/runtime05保持model/open provider OFF，无生产迁移、外部付费调用或新凭据。13仅受控QA编译显式开启好友图片，默认开关仍关闭。

**语音仍待真实质量验收**：12混合语音错词事实继续FAIL；换为固定revision的本机base-q5_1，59,707,625字节、SHA `422f1ae452ade6f30a004d7e5c6a43195e4433bc370bf23fac9cc591f01a8898`。13实际APK审计核对唯一base模型与manifest/MIT、无tiny残留、三ABI/七JNI/16KiB LOAD。82项语音/同意测试通过只证明相应程序状态，不证明中文英文混合识别正确。新模型实际加载、准确性、耗时及内存要在13包单独记录，不借旧09/10/12结果。

用户安装前直接确认“已清空，无录音”。**不再录屏**持续有效，本批仅必要截图/轻量日志；新视频覆盖USER_CANCELLED。旧12扫码和申请窗口已确认，但新对象发送/接受未完成；来源退休拒绝、取消及未观察未读变化的UNKNOWN均保留。13同构建五状态、登录返回、双账号图片/文字/AAC、扫码申请与权限/屏蔽/换号隔离继续验收；未完成的材料创建链、真实头像、完整分页、AT/中低机/iOS/发布等不由本轮编译推定完成。

---

# 当前20检查点12：12包扫码与申请窗口实测；混合语音仍不达标，关联改动进入下一整合批次

原任务 BT-FIX-NOW-UI-001 继续 IN_PROGRESS，256项原队列保留，73项仍未完成。现有未完成代码的整批编译事实沿用检查点11：9个Go程序、101个Go测试程序（仅编译）、Android和Web profile12成功；Windows symlink前提失败、iOS环境及生产发布前提未满足。没有把编译或原型检查换算成DONE。

当前12包/APK `7c13a759c0e3ffbd8a356d6ac336a6736c3396f90ea4b7df39c5810f8ed6b4e6`、install18、API03/runtime03。12包真实扫码进入“Now20新空账号”的资料页，严格before/after只读检查关系不变；本人确认点击“添加好友”出现填写窗口，原始截图保存申请说明“好”和确认页。随后确认页显示来源已变化并禁用发送，取消回到资料页；13:01:49 UTC保存的成功读取按实际 DTO 的 otherAccountId 核对无该对象申请。当前追加读取被身份检查拒绝，未自动登录，不将旧读取冒充实时结果。**发送/接受尚未完成**，没有强行触发禁用按钮或绕过UI。详见 [12包社交检查点](../testing/evidence/now-v4-v41-integration-20261010/continuation20/contact/social12-checkpoint12/receipt.json)。初始150ms合成触摸偶有不响应，短tap取消有效；不能据此确定产品故障或触摸根因。

12包共享登录实际返回原联系区域并保留Agent草稿，四个近期联系人与API匹配；“全部会话→联系人管理→添加联系人→扫一扫”在同包实际可达。29个未读随后在未观察时间段消失；只读记录证明四个lastReadAt变动，但当时用户操作不可知，原因未确定，不能把它写成已定位或已修复的误标已读问题。

混合语音12实测有文字、无超时，本人明确主要是中英文错词；保存草稿为“帮我找附近的安静的咖啡店然后最好有外载”，虽已简体但未保留quiet cafe/Wi-Fi，**准确性验收失败**。本人此前一次提到出现wifi的反馈与保存结果都保留，不互相覆盖。SDK36 ICU五个固定样例通过只证明简繁转换，不证明ASR。停止重复tiny试录，官方固定revision的base-q5_1模型59,707,625字节、SHA `422f1ae452ade6f30a004d7e5c6a43195e4433bc370bf23fac9cc591f01a8898` 已下载验证，切换候选准备好但本检查点尚未应用/实测，不宣称更大模型必然识别准确。

一次全客户端分析实际发现0error/5warning/243info；只删除四个测试文件的未用imports/绑定并保留实际getRect断言动作后，第二次实际exit0、0error/0warning、243现有info。生产12帧未改变。好友图片后端候选通过Go overlay直接相关89项测试/子测试，1个需要DB的既有测试跳过；PostgreSQL候选测试程序编译成功，数据库事务/迁移仍未运行，客户端候选继续整合。完整原输出与失败记录保留。

已核验设计任务中用户最新继续实施与编译授权并读取 [2026-10-12交接](../ux/designs/2026-10-12-now-composer/CODEX-IMPLEMENTATION-HANDOFF.md)：真实地图逐对象保留入口，取消数字聚合及预算丢点，圆地点/方动态按ID打开正确内容；现有侧导航、右社交区、唯一输入框继续保留。共享局部材质参考Apple官方Materials，正文清晰，克制高光阴影，沿用当前局部模糊性能约束，不宣称Flutter材质等于原生Liquid Glass。原型12条只是演示证据。地图/玻璃、好友图片与base模型按关联批次验证编译。

用户取消新录屏继续有效，所有后续视频项USER_CANCELLED；原历史视频证据保留。新批次尚无APK、真机语音质量或权限全链通过结论。真实头像、材料完整链路、双账号收发/屏蔽/账号隔离、五状态同包截图、AT/中低机/iOS/生产发布等未验收项继续如实保留。

---

# 当前20检查点11：现有未完成代码已整批编译，12包安装；混合语音与双账号验收继续

仍为原 `BT-FIX-NOW-UI-001 / IN_PROGRESS`，256项队列没有重建，没有把编译成功换算为DONE。实际清点183 DONE、73未完成（47 TODO、12 PARTIAL、13 BLOCKED、1 IN_PROGRESS）。[逐项原任务及模块清单](../testing/evidence/now-v4-v41-integration-20261010/continuation20/task-compile-inventory01/INVENTORY.md)区分直接代码、基础模块与尚无实现/外部验收门槛。

**整批编译实际结果**见[同源编译覆盖](../testing/evidence/now-v4-v41-integration-20261010/continuation20/compile-coverage12-01/receipt.json)：Go全部9个命令程序实际build0；101个Go测试程序实际compile0，未执行测试；Android profile12和Web profile12实际build0，同客户端完整source dict。Windows profile12实际exit1，当前系统未启用symlink支持；MSVC另未确认。iOS需要Mac/Xcode，Android生产release的真实签名、版本与生产HTTPS前提未满足，不绕过。Web第一次真实失败的不可精确JS整数已修；release gate的正确拒绝保留，后续采用development/profile，没有用假发布配置。

新版本号校验保留原native int64-1语义，Web明确拒绝超过2^53−2，给下一版加一留下精确空间，不取整/强转不安全数。两个真实解析器与原HTTP/owner守卫27项VM测试实际通过，[4路径分析的真实tool导出](../testing/evidence/now-v4-v41-integration-20261010/continuation20/client-revision-analyze12-tool-observation01/receipt.json)exit0，无问题；原分析进程流未独立redirect，tool历史为权威记录。Chrome实测在suite加载前挂起，受控关闭独占headless进程后exit1，0断言；不能标浏览器测试通过，也不能将bootstrap失败归为产品解析断言失败。Web Wasm dry-run明确跳过。

**当前手机glass12/install18/API03/runtime03**，APK `7c13a759c0e3ffbd8a356d6ac336a6736c3396f90ea4b7df39c5810f8ed6b4e6`，客户端完整帧 `5cbdc17c796f7952b393089360dfac965df79753347901487fb4dd14de5c41ec`。本人直接确认“测试文字已清空，未在录音，没有其他草稿”后才安装，保留应用数据；[原始安装后PNG/XML](../testing/evidence/now-v4-v41-integration-20261010/continuation20/phone/final12-installed02.json)显示原地图、两社交入口、唯一空Agent输入。第一次截图因ADB daemon连接失败，重新核对同设备/安装/来源后新名捕获，未将环境失败冒充产品失败。API03和隔离DB保持，API04只编译未替换runtime。

语音实际代码从greedy改beam5，真实native语言检测后，仅检测为中文且简体产品策略允许时应用Android ICU Hant-Hans/Han filter；保留英文、符号、emoji及原取消/身份/期限/内存清理。没有针对测试句造词替换、上传或付费服务。[独立12来源审计](../testing/evidence/now-v4-v41-integration-20261010/continuation20/audit/source12-readonly02/receipt.json)1577项实际只读检查通过：12与11只差3个版本校验生产路径，1467 native11源及APK三个ABI JNI、模型/manifest/两MIT许可证逐字节相同；new JNI语言getter真实在包内。第一次审计过严比较format前候选与format后源的失败保留，第二次严格区分两个阶段，未改原回执。

[SDK36实际ICU检查](../testing/evidence/now-v4-v41-integration-20261010/continuation20/contact/offline-asr-quality01/icu-device12-01/receipt.json)5个固定公开样例全通过，包含中英、café、标点和emoji；这只证明平台转换，不证明ASR识别准确率或运行时语言判断。SDK24–28保留原文回退，检测为英文的混句也可能跳过简体转换，能力边界保留。当前mixed02已绑定同12/安装18/PID的空输入和原PNG/XML，等待本人亲自Mic→说话→Stop，未发送；旧10 mixed01真实内容/繁体失败与英语词差原字节保留，不能改判新12成功。

旧10 pair before-scan04在根显式刷新受控会话后实际PASS_READONLY_STAGE，先前01/02/03的会话/新鲜截图安全拒绝保留。这只证明旧10新pair当时关系为空，不证明扫码/申请/接受/音频ACL或新12。新12严格只读helper正在按真实包来源准备，保留固定API/DB/身份/fresh capture边界；随后继续二维码核对对象→明确申请→接受、双账号消息/屏蔽/第三人权限和账号隔离，不从代码或历史截图推验收。

仍无后续录屏，用户取消新视频要求为USER_CANCELLED。source10的127测试、访客窗口/登录返回/实际ABBA继续按原版本解释，未迁移为source12运行性能通过。好友图片、材料完整链路、真实头像/完整联系人分页、当前新包五状态及全链路、真AT、中低机/iOS/release等实际缺口继续保留，未标完成。

---

# 当前20检查点10：10包访客与登录返回、四轮性能已实测；混合语音失败，优先修准确性

原 `BT-FIX-NOW-UI-001 / IN_PROGRESS` 与256项队列保留。当前手机复用 **glass10/install17/API03/runtime03**；APK仍 `e2c071a7786cc31179c787b7cda8b9fce28a7c752667bc30938dfc9bd82e17e2`，完整客户端源码帧 `360635e20b247759164777e9c5097c1a3977ceb9c9a68f44a71c48eb76171056`。本次功能与四轮采样没有新Flutter/Gradle构建，没有录屏；只替换现成base10/glass10 APK且保留应用数据。新视频要求按本人取消，不能标PASS。

**实际10访客窗口通过限定范围核对。** [独立访客记录](../testing/evidence/now-v4-v41-integration-20261010/continuation20/performance/guest10-readonly01/receipt.json)绑定install13/PID29775的三组原PNG/XML，冷起收起0提示、联系/通知展开各恰好1提示1按钮，唯一EditText及TextureView，login触区约52×48dp。passive owned API relay完整记录9次白名单公共GET、无auth header/私人路径/领域写，1400/17833字节与结束统计吻合、零失败/截断/未知路由。仅限该QA API转发窗口，不是Mapbox/外部全网或所有未来访客动作的证明。

[实际受控主账号UI登录](../testing/evidence/now-v4-v41-integration-20261010/continuation20/contact/primary-ui-login10-03/receipt.json)从打开的通知→共用登录→测试手机号/code→回同一通知区域，未绕个人资料。随后4条近期名字与真实primary API吻合；5会话/30未读和独立通知页与历史sample06完全相同，未发送/ack/申请/屏蔽。两次helper定位失败（native手机号hint、未加载之前的unread标签）及原树保留；不是产品登录失败，也没有重做code/verify来伪造一次成功。之前未知owner的空会话不能用来假装primary验收。

[独立实际通知几何](../testing/evidence/now-v4-v41-integration-20261010/continuation20/performance/notification-phone10-readonly01/review.md)内包络高度由旧09 360dp→新10约250.15dp，减少约30.5%；标题/空提示/刷新原位置相同，主要去掉底部空白。与源码inset推外宽256dp而非显式native glass矩形。原自动firsttap未展开、后来本人确认展开分别保留；本次四轮cold过程的点击成功不抹去偶发失败，也不判原因/冷点修复。

**新10 ABBA是真实同源码比较。** [比较与完整范围](../testing/evidence/now-v4-v41-integration-20261010/continuation20/performance/abba10-01/REVIEW.md)：sample07/08/09/10依次base/glass/glass/base，4 collector真实工具exit0、4独立保存文件审计和现成比较器实际exit0；基线2959帧、玻璃3007帧，七helper、API03、source10、内容/设备条件/动作recipe相同，11动作与保守采样窗口均通过。每轮P95中位数UI2.7405→2.8025ms，raster1.9545→2.6315ms，total5.884→6.856ms；total P99中位数9.2795→11.8945ms。totalSpan>16.667ms计数10/2959→9/3007，样本量不足以据此说玻璃更稳或绝对稳定60fps。TTID475/447/399/424ms，PSS端点457533→457143、466123→466837、464415→461440、457405→458413KiB均为实际端点，非peak/leak。

独立原图发现预热聚合mask仅有小像素偏移，所以exact native viewport相同仍未证明；primary显示端点每轮约120→60Hz，非固定刷新率。原monotonic输入未另存，四collector stdout/stderr没现场redirect；root只按真实tools返回导出[tool-observation](../testing/evidence/now-v4-v41-integration-20261010/continuation20/performance/tool-observations10-01/receipt.json)，不伪造独立原terminal streams。Flutter sample可比不含native present/首次可操作/低中机/iOS/release，也不与不同源07直接声称同源改善百分比。

**10英文链路成功但词差保留，混说确实FAIL。** 本人英文回复“已手动停止，英文正确且可编辑，未发送”，[实际英文草稿](../testing/evidence/now-v4-v41-integration-20261010/continuation20/contact/offline-asr01/phone-acceptance01/english02/human-observation10-01.json)为 `Please find a quite coffee near me.`，与requested `quiet cafe` 有词差；本人准确性报告不删，也不把整项英语准确性判通过。本人已亲自清空英文测试草稿。mixed01同glass10/install17/PID16999的[实际失败](../testing/evidence/now-v4-v41-integration-20261010/continuation20/contact/offline-asr01/phone-acceptance01/mixed01/human-failure10-01.json)原文“幫我找附近的快咖啡最好有外翻”，requested中的quiet cafe/Wi-Fi没保留且为繁体；本人明确反馈同样问题。root未注入、纠正、开始/停止录音或发送。首次notice与逐个手势未另证、运行时擦除/全网/零写/当前native耗时NOT_RUN；旧09中文仅那一次成功继续保留，不能推出三语通过。下一优先工作是本机模型/解码及简体输出准确性，仍免费本机、手动停止、不自动发送，不用测试句特制替换伪造结果。

新10严格pair只读helper已准备且34纯检查0；第一次实际before-scan01安全拒绝 `CONTROLLED_SESSION_PROOF_FAILED`，是受控fixture会话证明未成立，尚无新10pair基线或扫码/申请/接受/音频ACL通过；没有自动login或重放。后续需要root显式刷新合成会话后新名新fresh截图再核，原09成功不转记10。其他过期/离线/屏蔽/图片完整链路、头像/联系人完整分页及材料缺口继续按历史范围未完成。当前没有标DONE。

---

# 当前20检查点09：10包结构与材质修正已安装，取消后续录屏，继续同包功能与性能验收

继续原 `BT-FIX-NOW-UI-001 / IN_PROGRESS`，总256任务与其他255项保留。当前新增客户端完整源码帧 `360635e20b247759164777e9c5097c1a3977ceb9c9a68f44a71c48eb76171056`，base10/glass10实际构建0且同帧，glass10 APK `e2c071a7786cc31179c787b7cda8b9fce28a7c752667bc30938dfc9bd82e17e2` 已按install13保留数据安装。API03/runtime03及模型、三个ABI原生ASR库与09字节相同；没有以新10号声称重编原生或新增语音成功。

**依用户最新要求，未来不再录屏测试。** [直接用户约束](../testing/evidence/now-v4-v41-integration-20261010/continuation20/user-constraints/no-screen-recording01/receipt.json)保留“不要再录屏进行测试了，每次录屏测试都会导致编译时长特别长”原文，覆盖旧计划中的新屏幕视频要求。既有视频不删，新视频覆盖为USER_CANCELLED而非PASS；继续功能动作、原PNG/XML、闭合轻量日志和不录屏的同包性能样本。录屏导致编译变慢是用户反馈，未伪称已测出因果。

10仅6个生产Dart路径变动：通知宿主及嵌入列表按内容缩高、内部滚动和原高度上限保留，初始加载有“正在加载通知”语义；Now小区域blur降低为sigma1.5，顶部4个常驻控制使用现有清晰衬底而不产生BackdropFilter，输入和展开社交区域仍局部渐变材质。地图、输入/收件人/草稿、身份与服务端权限、分页/已读操作、ASR/AAC、通用BirdtieSurface等未改。组件filter个数减少不等于GPU pass数或性能实测通过。

真实通知宿主回归先RED 1PASS/2FAIL（旧空态360高），再修正；最终[127项实际通过](../testing/evidence/now-v4-v41-integration-20261010/continuation20/client-ui-content-glass-tests10-04/receipt.json)及[分析0 error/0 warning](../testing/evidence/now-v4-v41-integration-20261010/continuation20/client-analyze07/receipt.json)绑定最终冻结源码，110项既有info保留。加载future、空态→3行、全20→23条分页/内部滚动、2倍字体、48dp点击、降级/访客零私有请求/唯一输入与草稿等按相关范围检查。首次10测试清理SemanticsHandle的失败与修正也保留，不抹成全程一次通过。真实读屏、键盘/设备连贯草稿及全部账号流程不能由unit PASS替代。

**本人已确认通知能展开。** 原自动firsttap仍未展开；随后本人回复“已展开通知区域”，[实际10原始截图/层级/绑定](../testing/evidence/now-v4-v41-integration-20261010/continuation20/phone/final10-notification-human01.json)显示展开通知、单输入、地图保留，刷新及好友申请触摸区域48dp。截图观察明显比旧09短，精确外框需结合像素/源码且非XML直接panel语义；不据画面“暂无通知”推断API空数据/权限，不把后续人手展开冒充冷起第一次点击修复。新10较长150ms受控按压联系实际展开，原零时长自动点击差异继续记录，暂不判断产品/输入注入因果。

09中文的本人手动停止、语义正确可编辑未发送并清空已实际通过，仅那一次成立；原文“幫我找附近的咖啡店”、JNI1782ms证据按旧检查点08保留。本人最新明确英文“尚未试录”，因此英文NOT_RUN；混说NOT_RUN，不能从中文或10库字节一致推出通过。07优化前真实超时保留。

原07 ABBA仍只是历史材质成本结果；**新10性能尚未采样/判通过**。拟复用现成base10/glass10同条件A/B/B/A，不为每轮重编或录屏。须先核对手机真实受控身份及4条近期内容，与API同帧/同内容；当前空会话界面不能自动当primary，也不能填造联系人。冷缓存、稳定60fps、原生地图呈现/首次可操作、低中档Android、iOS、release、真实AT等未覆盖范围继续保留。

后续同10功能流程继续访客0私人GET/登录返回、真实空数据/近期与历史/通知、受控二维码确认后明确申请/接受、双账号文字/AAC与草稿/账户隔离、精确过期/离线/屏蔽ACL及图片草稿。好友图片等原缺口、创建材料完整链路/真实头像及完整联系人分页仍未完成；旧05已真实申请与旧扫码结果不会删除或冒充10链路。未标DONE。

---

# 当前20检查点08：09优化包中文已真机通过，英文与混说及完整社交验收继续

仍继续原 `BT-FIX-NOW-UI-001 / IN_PROGRESS`，256项队列与其他任务保持。当前手机为 **profile-glass-build09/install12/API03/runtime03**，APK `5fdc50cb00ce71ab8043e8c2983edce8013bd9511bdfc7b570bf992eaf0543dd`、完整客户端源码帧 `273d7252d299fda7156b290dc59e9d901504be077759577f835a64c87b52fb76`；新09实际构建成功、安装与数据保留均有回执。以下旧检查点07及历史记录按原字节保留，不能再把旧07中文FAIL当作09结果。

**09中文这一次已PHONE_PASS。** 本人先回复“可以了”，再针对“看到录音后说话、亲自停止、意思正确、可编辑、未发送并清空”的明确核对回复“已确认并清空”。[原始结果PNG/XML/JSON](../testing/evidence/now-v4-v41-integration-20261010/continuation20/contact/offline-asr01/phone-acceptance01/chinese02/result.json)显示唯一Agent输入框内实际草稿“幫我找附近的咖啡店”，语义接近请求但不是逐字复制。没有root注入文字、开始/停止录音或发送。

[同PID闭合阶段记录](../testing/evidence/now-v4-v41-integration-20261010/continuation20/contact/offline-asr01/phone-acceptance01/chinese02/numeric-timing09-02/receipt.json)有唯一完整8阶段：录音63360 PCM样本/3960ms、manual_stop、JNI边界4139→5921ms（1782ms）、worker_exit5927ms，无timeout。[独立只读接受记录](../testing/evidence/now-v4-v41-integration-20261010/continuation20/audit/offline-chinese09-success-readonly01/receipt.json)只判这一次中文；JNI区间包含copy/能量检查/识别/文本转换，不是纯whisper_full计时。日志无request ID及原始device cutoff，归属依据唯一保留trace与本人/root操作序列；首次处理说明是否再次出现及实际改字符动作UNKNOWN，网络流量、服务器零写及运行时PCM擦除未实测。英文01已准备且正在等本人结果；同句中英混说NOT_RUN，不由中文推断。

已真实修复07的原生profile编译缺失优化问题。[09实际APK及编译审核](../testing/evidence/now-v4-v41-integration-20261010/continuation20/performance/offline-asr-feasibility01/apk-audit09/glass09/compiler-binding.json)核对31/31实际编译命令最终`-O3 -DNDEBUG`，4模型/许可证assets、3JNI ABI及ARM64 16KB LOAD均通过。08是未安装的优化中间包，09再将诊断分配/Log.d纳入Exception及OOM保护，防止诊断干扰清理；[最终1467文件只读复核](../testing/evidence/now-v4-v41-integration-20261010/continuation20/audit/optimization09-final-readonly03/receipt.json)匹配冻结清单。固定模型/vendor、auto原语言、2线程、权限/身份/收件人、60秒原生调用与100/105秒总期限保留，未靠延长超时换结果。旧07真实超时及O0配置证据保留；单次09成功仍不能证明O0是旧超时唯一原因。

相关Dart生产/测试未变，[165项语音及社交宿主测试](../testing/evidence/now-v4-v41-integration-20261010/continuation20/client-voice-offline-tests01/receipt.json)及0 error/0 warning分析可按冻结的Dart文件引用；它们不替代真机三语。API03远程model/open provider OFF与打包本机Whisper区分，未引入付费服务、凭据或生产部署。

07真实ABBA保留历史：[结果与解释](../testing/evidence/now-v4-v41-integration-20261010/continuation20/performance/abba07-01/REVIEW.md)。玻璃栅格P95中位数1.920→7.4925ms、totalSpan超过16.7ms的计数比例0.2017%→1.9357%，需要降低材质成本或清晰回退。07四轮TTID443/408/413/411ms与PSS端点已实际测量，不被Flutter比较器不消费这些字段的NOT_RUN抹去。09不能自动继承07性能验收，稳定60fps、原生地图呈现/首次可操作、冷缓存、中低Android、iOS、release仍未通过。

[同09包剩余实际流程计划](../testing/evidence/now-v4-v41-integration-20261010/continuation20/contact/final09-acceptance-plan01/plan.md)继续访客私人GET为0、通知登录返回原视图、真空态/近期/历史/通知、final-qr02新受控对象扫码→明确申请→接受、新pair双向文字/AAC与草稿/账号隔离、精确过期恢复、owned tunnel离线、最后屏蔽/ACL及图片草稿。05扫码后已有pending申请是历史真实结果，不抹去或算09完整链；09暂无域流程验收通过的推断。

视频/旅程/主题地图、好友图片、新建/公开Moment图、Agent图only、真实头像与缓存、完整联系人/申请分页及短创建缺口仍明确未完成；release既有签名门槛、硬件/AT/弱网覆盖继续保留。首次冷起联系未展开的07偶发记录未被后续成功点击抹去。当前任务没有标DONE。

---

# 当前20检查点07：离线中文仍超时，已定位原生编译优化缺失；完整ABBA显示玻璃成本

继续原 `BT-FIX-NOW-UI-001 / IN_PROGRESS`，256个任务保留，未重建队列。手机安装过同源07无模糊及玻璃包，目前为玻璃07/install11；原生计算配置修复正在进行，新源码尚未构建或安装，旧07不能作为该修复的真机验证。

**中文语音仍FAIL。** 本人按新离线流程尝试后的直接回复为：“一直在显示"正在转成文字..."很长时间都没有转译成功，然后显示识别等待超时”。[真实失败及原始PNG/XML](../testing/evidence/now-v4-v41-integration-20261010/continuation20/contact/offline-asr01/phone-acceptance01/chinese01/human-failure07-01.json)绑定玻璃07、安装11及同一QA PID；输入框为空、发送禁用。闭合原生日志明确 `stage=timeout route=on_device reason=timeout`，不是仅由Dart文案推定。没有自动重录或发送。具体人工开始、停止及首次说明等未被单独证实的步骤保留unknown，不伪造完整成功确认；英文及同句混说仍NOT_RUN。

真实Gradle profile编译命令已发现whisper、GGML及JNI没有任何 `-O` 优化选项，仅 `-g`；缓存build type是自定义profile，其flags为空。之前独立Release编译启用了优化，所以独立编译成功不能证明实际APK识别性能。正在仅修复显式原生计算优化并补默认OFF、现有preview Intent开关控制的阶段计时；模型、源码pin、语言auto、2线程及60秒推理/100秒原生/105秒Dart期限保持。优化缺失是已确认配置问题，尚不能宣称它是唯一超时原因，也不能用延长等待代替修复。

[新语音、输入框及社交宿主165项测试](../testing/evidence/now-v4-v41-integration-20261010/continuation20/client-voice-offline-tests01/receipt.json)实际通过，相关Dart文件与07构建一致；[分析](../testing/evidence/now-v4-v41-integration-20261010/continuation20/client-analyze06/receipt.json)0 error、0 warning。超长转写完整留在可编辑稿，UI和IME均保护240字节Agent发送限制；模型未就绪不自动联网降级。两份07实际构建成功且完整源码帧一致，[封装核对](../testing/evidence/now-v4-v41-integration-20261010/continuation20/performance/offline-asr-feasibility01/apk-audit07/pair07/receipt.json)确认4个模型/许可证assets、3 ABI及ARM64 JNI/16KB LOAD对齐。这些均不是语音识别准确率或实机推理通过。

[真实ABBA四轮对比](../testing/evidence/now-v4-v41-integration-20261010/continuation20/performance/abba07-01/comparison.json)已完成：同一手机、SDK36、同源profile07、同一Aberdeen地图操作配方、受控5会话/30未读、相同通知正文、保留磁盘缓存及USB本机网络。03/04/05/06按无模糊/玻璃/玻璃/无模糊顺序，11项地图/社交/键盘动作均实际执行且通过保存的保守窗口qualification，原始日志、运行退出及独立只读审计保留。下表为每轮所报分位数的中位数，未从摘要伪造合并分位数。

| Flutter指标 | 无模糊 | 玻璃 |
| --- | ---: | ---: |
| UI P95 | 2.7545ms | 2.9695ms |
| 栅格 P95 | 1.9200ms | 7.4925ms |
| 栅格 P99 | 3.292ms | 12.010ms |
| totalSpan P99 | 8.921ms | 20.382ms |
| totalSpan超过16.7ms的实际计数比例 | 0.2017% | 1.9357% |

玻璃有明确绘制成本，需要降低材质成本或使用清晰底色回退；`COMPARABLE_FLUTTER_SAMPLE`只证明样本可比较，不能标稳定60fps通过。Flutter totalSpan阈值比例不等于原生地图丢帧率。原生呈现、地图首次可操作时间、逐动作帧归属、精确原生viewport/冷缓存、全程固定刷新率、低中端Android、iOS和release仍未验证；原始L/T/R输入未单独保存，只能独立核对已保存的qualification输出与实际helper执行，不补造输入。此07对比也不能替代下一原生修复构建的最终证据。

首次冷起后点联系未展开、第二次才展开的07原始截图已保留；后续四轮冷进程配方中的首击成功与该偶发现象并列，原因仍未确定，未用多点重试宣布修复。既有QA触摸trace构建helper仅准备、尚NOT_RUN；锁定07旧源码，原生修复后会拒绝，不绕过其绑定。

API03、隔离本机数据库及现有地图凭据保持。服务端远程model/open provider OFF与打包本机Whisper是两件事；本机识别处理路径不引入新的付费服务或凭据，真实网络/内存擦除运行审计仍NOT_RUN。联系人资料批准上下文及录音地图竞态的29/45项实际结果沿用检查点06，仍须在最终匹配新包完成完整扫码申请、登录恢复、双账号读写/屏蔽及同构建录屏。材料/头像/完整联系人分页、release签名及硬件覆盖缺口继续保留，未标DONE。

---

# 当前20检查点06：社交与录音竞态已修，免费离线转写正在接入

仍继续原 `BT-FIX-NOW-UI-001 / IN_PROGRESS`。全部历史报告原字节保留，没有新建任务队列或改写其他任务。手机仍是05包，以下新增代码和宿主测试不能换算为新包真机通过。

联系人资料页不再因同一身份下的路由重绘重新生成批准上下文；账号、token、组织及数据来源切换会使旧上下文失效。数据来源退休显示“内容已更新，请返回联系人后重新打开”，普通同值身份通知不会错误清空资料，同账号实际token轮换保留搜索草稿且不自动重读或重放申请。[直接相关29项测试](../testing/evidence/now-v4-v41-integration-20261010/continuation20/audit/qr-request-native05-01/source-boundary-green01/receipt.json)实际通过，分析无问题；先前route/source真实失败记录保留。

录音、等待权限或整理录音时，地图预览继续保留当前好友对话。第二组真实失败进一步证明：录音时拒绝的地图选中，在取消录音后被普通历史刷新重新打开。现将该被拒绝选择消费并作废其排队预览；之后新的主动地图选择仍可用，Agent与该好友各自草稿恢复。[完整45项社交宿主测试](../testing/evidence/now-v4-v41-integration-20261010/continuation20/client-host-tests07/receipt.json)实际通过、生产与测试前后稳定。[新增竞态真实RED](../testing/evidence/now-v4-v41-integration-20261010/continuation20/audit/audio-cancel-replay-red02/receipt.json)及首轮测试夹具超时均原样保留。此缺陷与冷启动联系首击原因分开，后者仍未证明。

**现有系统Agent语音仍FAIL，中文、英文及混说尚未完成交付。** 用户增量允许更换音频处理方式，且未禁止免费离线引擎，因此正在实际接入官方MIT源码固定commit与受控multilingual tiny-q5_1模型。已扩原任务精确文件范围，不引入付费订阅、凭据或预算。计划为本机PCM录音、手动停止、离线转成可编辑文字，取消/身份退休清理内存音频，不上传录音、不自动联网降级、不自动发送；独立的首次处理说明与现有好友AAC消息隔离。依赖接线、原生构建、性能、真实识别准确性、中文英文同句混说均仍待实际验证，不能由模型存在或语言auto配置宣称通过。

本检查点尚未构建或安装最终批次；06无模糊中间包仅是历史构建。待离线实现及直接相关测试冻结后，统一新源构建并重新取得同一构建功能、录屏与ABBA性能证据。05性能单样本和旧中文失败继续按检查点05的边界解释，不用于后续版本验收。API03与隔离本机数据库保持，开放模型API提供方OFF，受控fixture登录刷新只建立会话，没有重放领域动作。其他材料/头像/分页、release签名、中低Android及iOS等未验证缺口继续保留。

---

# 当前20检查点05：真实扫码与待处理申请已观察，录音地图竞态待修复

本检查点继续原 `BT-FIX-NOW-UI-001`，仍为 `IN_PROGRESS`。下文检查点04及历史19全部按原字节保留；当前证据不能移算为后续构建通过。

05无模糊与玻璃包均已实际构建通过，共用生产源帧 `a2ed9aabf3732b5a954e6ecad48384d3f230fd227e4ca7b3c1c9dfc145dc0194`。玻璃05/install06的二维码相机由本人核对成功，直接反馈“已显示 Now20空账号的个人资料”；[原始资料截图](../testing/evidence/now-v4-v41-integration-20261010/continuation20/phone/final05-scanned-profile01.png)保留精确APK/API/源绑定。

扫码后首次读取该账号的好友及申请均为空，证明该受控窗口未因扫描自动建好友。随后[申请确认页](../testing/evidence/now-v4-v41-integration-20261010/continuation20/phone/final05-qr-friend-approval01.png)显示核对对象及“发送好友申请”；[服务器实际读取](../testing/evidence/now-v4-v41-integration-20261010/continuation20/fixtures/final05-qr-request-created01.json)已有一条相同说明的 pending 申请，创建于2026-10-12 06:48:42+08:00。先前压缩摘要将 created01/observed02误归为空，现已按真实文件纠正。受理、接受后关系、重复操作及后续构建的完整链路仍需继续验收，不能仅凭识别页交付。

05真机近期联系显示4个真实对象、长名省略、未读26/1/1/1；[全部会话](../testing/evidence/now-v4-v41-integration-20261010/continuation20/phone/final05-all-conversations01.png)可达第5会话与消息请求。冷启动首击联系曾未展开，第二击才展开，原因仍 UNPROVEN；迟到相同选择4项宿主回归通过不等于该首击缺陷已修复。

新发现独立录音缺陷：真实生产宿主使用MethodChannel模拟录音开始，父地图指针已拒绝关闭对话，但随后当前可见PLACE/MOMENT选择仍打开地点预览并隐藏对话；收件人与录音保持，未提交。这两项[真实RED](../testing/evidence/now-v4-v41-integration-20261010/continuation20/audit/native-first-contact05-01/recording-map-red01/receipt.json)实际exit1，生产源稳定。尚未修复，不将其认作冷启动首击根因。另一项扫码资料 route 重建候选正在做独立回归。

**Agent中文语音仍 FAIL。** 本人最后报告“出现错误提示”“不记得了，就是没有录音到”，原唯一输入为空、发送禁用。04闭合诊断回溯观察到系统界面约7秒返回 network 类错误，未观察到应用45秒超时或身份退休；具体厂商端点原因及实际录音时长未证明。05原生识别器字节相同，未重复请求本人试录，未注入文字或更换付费提供方。英文及混说 NOT_RUN；[失败与证据边界](../testing/evidence/now-v4-v41-integration-20261010/continuation20/contact/voice-activity01/human-failure04-01/receipt.json)完整保留。

性能05首个有效单次无模糊样本[profile-sample02](../testing/evidence/now-v4-v41-integration-20261010/continuation20/performance/device/profile-sample02/receipt.json)：60秒、1558帧，UI P95/P99 2.874/3.857ms，栅格1.942/3.862ms。此前04单行日志被Android截断，失败记录保留；05新增分块/完整校验，仅修测量传输。现无ABBA结果；实际动作检查延迟使末次键盘阶段靠近窗尾，所有阶段是否在probe窗内未证明，不能由此宣布整个流程60fps通过。主屏端点renderFrameRate由120到60，系统设置相同并不证明全窗固定刷新率。原生地图present、冷缓存、中低设备、iOS、release仍 NOT_RUN。

API03与隔离本机数据库保持，模型/开放提供方OFF、新增付费调用0、生产部署NOT_RUN。材料能力、真实头像缓存、完整联系人分页等缺口继续按上份检查点列明；未恢复旧重复入口、未新建任务队列。下一步先修复有真实RED的竞态，再冻结新源构建，重新取同一构建功能与性能证据。

---

# 当前20：结构与身份修复已接通，语音提前结束仍待实测诊断

执行检查点：2026-10-11T21:54:02.197836+00:00。本次附件已并入原 `BT-FIX-NOW-UI-001` 的39条验收子项，当前仍为 `IN_PROGRESS`；没有重建队列。以下为当前20，后面的19及更早材料按原字节保留，仅作历史。

当前手机安装 profile-glass-build04/install03，APK `f9f79b056da02f0546f7f9296540b4ee5cbbc63fcd57e774d20dd9f1f1df8287`；隔离本机 API03 为 `d11de802f34baadbf55fc710dabdaa0851c80fa3fe2655e45eb7a63d548a69ea`。同生产源 profile-base04 与玻璃04均已实际构建通过，模型与开放提供方 OFF，新增付费调用0，生产部署 NOT_RUN。

- 右上改为“联系”“通知”两个小入口，共用一个可收起区域。联系首层约四个近期对象，完整会话、联系人管理、好友/关注筛选与添加动作逐层进入；通知首层三条与真实分页，不重复真人消息。地图和唯一输入持续保留。
- 访客不读取私人列表；未登录、真实空态、权限拒绝、离线与服务错误分别处理。首次从通知登录后返回原通知视图已修复，旧账号/源退休及草稿隔离继续有效。build03受控访客窗口实际8个GET（6公共＋2认证状态），私人列表0；04同构建窗口仍待取证，不能移算。
- 新发现并修复旧地图预览回调覆盖较新社交操作的竞态：真实宿主37项通过，原33项内容保留，新增4项覆盖地点/动态×联系/通知及随后主动重开；此独立竞态不是首击问题已证实根因。
- Agent语音保留可编辑、手动发送和账号/任务/收件人守卫。04修正系统语音界面未指定调用方显示屏的问题，并提供默认关闭、仅本地QA显式开启的阶段/耗时诊断，不记录音频或转写。108项语音定向模拟检查通过；Android原生编译已通过，不能据此宣称真实识别通过。

**真实中文语音仍未通过。** 用户在03包报告“能看到录音界面，但刚开口就结束了”；稍后原唯一输入仍为空。原界面观察器拒绝越出主屏焦点范围，没有测得引擎返回时长/错误，原因未证实；04即将结合封闭时序诊断复测。没有注入转写或自动发送。英文、混说、04双账号AAC、图片、扫码、五状态截图和原生性能当前尚未完成。

玻璃复用现有token、局部裁剪和轻量回退；无模糊与玻璃实际设备ABBA尚未运行，60fps及原生地图呈现不能宣布通过。当前设备尚未证明属于目标中低性能机；iOS、冷缓存、release体验仍NOT_RUN。原release02构建在既有签名配置检查处失败，未新增凭据或绕过门槛。

材料菜单保留真正已接通的图片草稿、私人文字Moment、活动与可用地点收藏，以及旧记录编辑加图。旅程、主题地图、视频、真人图片消息、公开图片Moment与图片单独发送缺少完整合同，属于NOT_DONE，不能用入口或旧截图交付。好友/陌生请求现有列表仍有100条上限；新会话/历史/通知分页不等于所有联系人数据完整分页。真实头像源与缩略图缓存仍有缺口。

当前04 scoped Dart analyze：exit0，0error/0warning，110info保留。API03通知真实隔离数据库7项及聊天窄源8项已有通过证据，完整API帧与两者运行阶段的差异已明确记录；未重复全仓测试。全部失败记录、旧安装与旧媒体证据保留。

证据目录：[当前20](../testing/evidence/now-v4-v41-integration-20261010/continuation20/)。本检查点是进行中的实际改动与缺口记录，不代表验收完成。

---

# 当前19：统一社交空间、身份与输入草稿；真机验收仍为 PARTIAL

2026-10-12。用户本次结构要求已并入原 `BT-FIX-NOW-UI-001`，与语音、图片修改共同执行。已构建并安装 client-build10，APK `17a164b46a6f807fa5734c9579982f52ff65d81cc935af694aa10129c15822bd`；当前本机 API-build02 为 `dd547f29106579458cfdd4d375c2078f72fcbfbd0151516bcdcf7cc34f344124`。build10/install06/runtime02 源帧、产物哈希与实际装包绑定。仅在已核验的隔离本机数据库及三个人工合成账号中测试；模型与开放提供方 OFF，新增付费调用 0，生产部署 NOT_RUN。

**社交、身份和真人文字消息已取得下面的当前包证据；Agent 语音转文字仍未成功，不能标记任务完成。** 07/08/09 的实际扫码、录音、图片及失败证据分别保留原版本，不换算成 10 包实测。

## 实际实现与本轮修复

- 右上入口只展开同一个有宽度和高度上限的社交区域。默认近期消息，联系人与通知替换当前内容；好友/关注仅在联系人内筛选。消息请求和好友申请保留可达入口。访客只有一次“登录，与朋友保持联系”及登录按钮；真实空数据、权限拒绝、连接中断和服务异常分别呈现。访客零私人读取由宿主/transport 测试核对，静态截图本身不证明请求数量。
- 共用身份边界覆盖私人数据、收藏/分享/发布与当前输入。并发 401 合并恢复，403/404 不冒充登录过期；退出/换账号同步退休旧内容和在途响应。同账号过期保留对应草稿与原右侧选择，显式重新登录后恢复；不会自动发送、发布或申请。真实认证仍由服务端验证，开发手机号登录不代表真实短信/IdP 通过。
- 联系人添加菜单直接提供搜索用户、扫一扫、我的二维码；受控公开二维码只含公开对象标识。相机按需开启，识别后显示公开资料，关系操作另行核对与确认；不自动访问任意外部 URL、关注或加好友。当前 08 真机识别链已成功，10 的相机未重测。
- 真人对话复用 Now 唯一输入框，明确显示收件人，离开后恢复原 Agent 草稿。音频有真实原生录制、私有存储、授权读取和播放链；图片作为同一输入框的附件，未发送时不上传；助手读图尚未启用，界面说明真实能力。
- 本轮权限回归发现两个假状态并修复：历史未成功读取或被撤权后不能显示“还没有消息”；取消录音草稿不能清掉权限错误。随后发现黑色发送按钮仍可用，新增 `canSendText` 与提交前检查，使按钮、键盘发送和迟到按钮回调都遵循真实权限/待核实状态，保留原草稿且不调用发送。正常 200 恢复只重新启用，不自动发送。
- 修复大字号反馈被旧固定高度裁切的问题，内容按可用阅读区域增长并在必要时内部滚动。API 私有图片读取原本把任务不存在包装为 503，现映射为 404；真实存储/上下文异常仍为服务错误，身份/权限检查未放宽。旧 503 是错误分类失败，不能据此宣称另一账号读到了图片。

适用 UX-CHECK-01/04/05/06/07/08/09/10/12/13/14/16；读屏、完整可用性与性能仍按未运行记录。

## 同一 client10 / API02 的真机五状态

全部原始 PNG/XML/元数据见 [本轮手机证据](../testing/evidence/now-v4-v41-integration-20261010/continuation19/phone/)。实际 Android 设备 c641566b，1220×2656，浅色，通常 font1.0；200% 场景明确标注并已恢复 1.0。没有录屏、注入截图或模拟接口响应。

| 状态 | 实际结果与当前截图 |
| --- | --- |
| 访客 | [唯一短登录引导](../testing/evidence/now-v4-v41-integration-20261010/continuation19/phone/final10-api02-guest-guide.png)，仍能看公开地图 |
| 登录后无数据 | [回到原右侧消息空态](../testing/evidence/now-v4-v41-integration-20261010/continuation19/phone/final10-api02-login-return-empty-messages.png)、[单一联系人空态](../testing/evidence/now-v4-v41-integration-20261010/continuation19/phone/final10-api02-empty-contacts.png)，没有三组失败或虚假计数 |
| 联系人与未读消息 | [好友联系人](../testing/evidence/now-v4-v41-integration-20261010/continuation19/phone/final10-api02-friend-contacts.png)、[近期一行预览/未读1](../testing/evidence/now-v4-v41-integration-20261010/continuation19/phone/final10-api02-unread-recent.png)、[同区域双向历史](../testing/evidence/now-v4-v41-integration-20261010/continuation19/phone/final10-api02-two-way-history.png) |
| 登录过期 | [实际过期后的登录流程](../testing/evidence/now-v4-v41-integration-20261010/continuation19/phone/final10-api02-expired-login-flow.png)、[取消后单一引导](../testing/evidence/now-v4-v41-integration-20261010/continuation19/phone/final10-api02-expired-guide.png)、[同A重新登录恢复联系人/关注和原草稿](../testing/evidence/now-v4-v41-integration-20261010/continuation19/phone/final10-api02-expired-login-return-contacts-draft.png)，未按发送 |
| 网络异常 | 仅移除自己 QA 应用的 USB 本地隧道，API 仍 ready；[连接中断与重试](../testing/evidence/now-v4-v41-integration-20261010/continuation19/phone/final10-api02-network-error.png)，身份/Agent 草稿保持，[显式重试恢复](../testing/evidence/now-v4-v41-integration-20261010/continuation19/phone/final10-api02-network-recovered.png)；隧道已还原 |

## 双账号、关系权限和输入恢复

C 在当前包通知中打开 A 已存在的 07 申请，核对具体对象后明确接受；[查看申请](../testing/evidence/now-v4-v41-integration-20261010/continuation19/phone/final10-api02-friend-request-review.png) → [接受回执](../testing/evidence/now-v4-v41-integration-20261010/continuation19/phone/final10-api02-friend-accept-receipt.png) → 联系人。这是 10 包接受旧申请的真实流程，**不是 10 包重新发起一条申请**。C→A 明确发送 `Now10 hello`，A→C 明确回复 `Now10 reply`；原生历史和 SQL 均为 2 条。两账号真实登录切换后的内容/草稿属于各自身份，C 退出真人模式恢复 [原 Agent C 草稿](../testing/evidence/now-v4-v41-integration-20261010/continuation19/phone/final10-api02-agent-draft-restored.png)。

[31](../testing/evidence/now-v4-v41-integration-20261010/continuation19/native-acceptance/31-final10-pair-snapshot/receipt.json) → [32](../testing/evidence/now-v4-v41-integration-20261010/continuation19/native-acceptance/32-final10-pair-snapshot/receipt.json)：同一条历史，A 未读1→0；A/C 通知 `readAt` 字典完全不变，未调用通知已读接口。[39](../testing/evidence/now-v4-v41-integration-20261010/continuation19/native-acceptance/39-final10-pair-snapshot/receipt.json) 记录双向2条，C未读1。B 作为第三账号读该私人历史得到404，拒绝响应正文未读。

[33](../testing/evidence/now-v4-v41-integration-20261010/continuation19/native-acceptance/33-final10-pair-block/receipt.json) C 对 A 进行一次明确隔离验收屏蔽，双方历史404；A 在仍打开的旧对话中按一次发送被拒绝。[34当前包200%真机图](../testing/evidence/now-v4-v41-integration-20261010/continuation19/phone/final10-api02-blocked-composer-font200.png) 显示真实权限提示、旧历史已清、无假空列表、原 `Now10 blocked delivery` 草稿完整且发送按钮灰态/disabled；字体恢复1.0。[35](../testing/evidence/now-v4-v41-integration-20261010/continuation19/native-acceptance/35-final10-pair-afterblock/receipt.json) SQL仍2条，无屏蔽后投递。键盘发送/迟到回调的零提交断言属于 widget 测试，不冒充物理键盘实测。

[36过期回执](../testing/evidence/now-v4-v41-integration-20261010/continuation19/native-acceptance/36-expire-primary/receipt.json) 只更新精确 A 合成账号的一组已核验活跃测试会话，B/C 不受影响；同A手动登录后回到原联系人/关注和 `Agent A Now10 retained`，没有跳个人资料页。APIfixture显式刷新只恢复测试凭据，不重放领域动作。

## 扫码、音频、图片和语音识别的证据边界

- **扫码不是占位入口**：08/API02 [真实相机开启](../testing/evidence/now-v4-v41-integration-20261010/continuation19/phone/final08-api02-camera-active.png) → [识别到B公开主页](../testing/evidence/now-v4-v41-integration-20261010/continuation19/phone/final08-api02-camera-decoded-profile.png)，本人再次回复“已显示个人主页”。识别后 ties/follows 的独立读取仍为空；后来明确接受好友申请是另一步。搜索直接出结果与添加三动作也有09/08实机证据。10未重新扫码；拒权、无效/过期码与外链负向分支有代码/widget检查，物理矩阵 NOT_RUN。
- **好友语音真实链已于07通过**：本人录音后回复“已说完”，另一账号在原生播放后确认“听到了，声音正常”；[人工确认和读取分离回执](../testing/evidence/now-v4-v41-integration-20261010/continuation19/mobile-observations01/receipt.json) 保留原APK/API来源。实际存储音频hash、授权双方200/陌生人404、屏蔽后双方及第三人404另有回执。10音频未重录/重播，不把旧声音当10验收。
- **图片**：本人07通过底部＋选择受控公开QR，确认同一输入框附件；图片单独不能假装被助手理解。明确文字任务后实际保存私人图片。[38当前10/API02只读回执](../testing/evidence/now-v4-v41-integration-20261010/continuation19/native-acceptance/38-image-actual-readonly/receipt.json)：主人B200、A/C404，解码像素与受控原图一致，不在证据里复制私有图片bytes。原物理选择/保存仍为07；10未重新选择/提交、未物理操作遮挡预览，模型vision未开启。
- **Agent STT未通过**：08本人操作仍回复“仍然识别失败”。[闭合诊断](../testing/evidence/now-v4-v41-integration-20261010/continuation19/voice-phone08-diagnostic01/receipt.json) 为 `system / network_timeout / AndroidError1`，界面明确连接超时，没有可编辑识别草稿。系统输入法可以生成文字，但设备输入法与应用调用的RecognitionService不同，不能替代应用通过。10识别器源hash与08相同，未再次采音；具体服务超时原因仍未解决，无自动付费替代、下载或全局网络修改。

## 本轮实际检查与尚未完成

| 执行范围 | 实际结果 |
| --- | --- |
| [root-tests11](../testing/evidence/now-v4-v41-integration-20261010/continuation19/root-tests11/receipt.json)，30份直接相关客户端测试，同10源/测试帧 | 475 PASS，exit0，source/test稳定；不与旧461/470或专项数字相加 |
| [analyze06](../testing/evidence/now-v4-v41-integration-20261010/continuation19/analyze06/receipt.json)，23个整合文件 | exit1：0 error、3 warning、107 info；不是全绿分析 |
| [对话空态修复](../testing/evidence/now-v4-v41-integration-20261010/continuation19/contact/friend-thread-state02/receipt.json) | RED2→GREEN23，原失败保留 |
| [发送权限/IME修复](../testing/evidence/now-v4-v41-integration-20261010/continuation19/contact/friend-composer-permission03/receipt.json) | RED4→GREEN38，原失败保留，包含当前475中覆盖 |
| [API定向03](../testing/evidence/now-v4-v41-integration-20261010/continuation19/go-api03/receipt.json) | 同API02，59 Go测试/子测试PASS事件，0 fail/skip；非完整Go仓库通过 |
| [较宽Go02尝试](../testing/evidence/now-v4-v41-integration-20261010/continuation19/go-api02/receipt.json) | FAIL保留：3539 PASS/158 FAIL/2829 SKIP事件；多数缺明确隔离测试DB，还有独立retention gate单元断言不符，不能仅归因环境 |

尚未完成：Agent真实STT；10同包完整扫码/录音/图片重验；相机拒权/无效与过期码、TalkBack/iOS、长列表和地图运动/性能、真实短信/IdP与非合成用户联合测试；图片遮挡检查和助手读图；必要确认里“原服务”等遗留内部文案仍需精简。完整Go尝试的环境/retention旧测试差距也未闭合。未打开模型/提供方、未知发布门槛或真实对外操作；全应用验收 **NO**。

原失败和误名文件保留并排除冒充PASS：image11的A503；20/23/37的旧APIfixture401守卫失败；25大字号脚本遗漏现有“不可访问”文案而断言失败（26零重复发送只读复验）；`final08-api02-network-error-auth-retained`其实空关注、`peer-human-history`其实联系人、`final09-api02-unread-recent`其实收起地图、`final09-api02-expired-guide-draft`仍是登录sheet。最终清单逐文件hash索引，旧版报告及独立审计保持来源。

原队列仍256项，仅当前任务更新为PARTIAL；其他255任务完整对象对本轮baseline08保持不变。更早before-queue与AIR009的差异来自独立ai_loop_resume17既有工作，不回滚。没有重建队列、提交、ZIP或生产部署。原17报告以下按原字节保留，当前结论以上述19节为准。

---

# 当前17：候选无结果合同修复；输入框新规则待18批合并（2026-10-11）

用户继续注解1的真人转写与AI候选/工具闭环，并新增统一双按钮、当前图片附件、好友音频、就地登录与单空间社交视图要求。保留原任务ID和未提交代码。原15/16历史证据不改写。

- AIR009新增 `air.candidate_proposal.v2`：严格 `NO_CANDIDATE` status-only 或完整原CANDIDATE；v1原JSON字节形状保持；registry与请求schema绑定，native Activity释放仍拒绝候选。8源文件，相关两包756 Go PASS事件，0fail/skip，稳定源帧。没有模型调用、候选写入或live工具闭环完成声明。
- 初步高对比手动发送按钮：58 Flutter相关测试通过；初次测试因测试finder读取RawTooltip类型而6失败，修正finder后通过，原日志保留。窄屏320、两主题和100/130/200%字号已widget验证。随后用户改为麦克风/发送并存，单按钮版本仅保留中间证据，不再作为当前规范。
- 中间debug APK e0de94c941a7ea479baa8209a378150443de5611cd2642a70e469ccf99f1059e构建成功、源稳定，**未安装**。现手机仍15版fa8b62cd60fc84c61e506b0a45995e6a6ab9be82a9c88d21e821e4f6adc281a5；API01未更新。
- 真人本人直接确认“已通过 BirdTie 麦克风看到转写草稿”。新只读真机截图发现草稿已空，历史里出现查询以及登录失效提示；这证明当前登录/发送衔接需要修复，不能把空草稿观察当作新真人识别留存/手动发送流程PASS。是否清空/已发送的澄清尚待回复。中文/英语/混合实测与好友音频收发仍NOT_RUN。
- 真实缺口：token变化会清Now草稿与图片；通用图片入口仍分裂为独立task存档；Agent仅图片没有合法发送合同，模型vision关闭；好友消息只文字/实体且没有录音/音频存储/播放；旧＋创建旅程/主题地图无可复用页面。18批按用户最新规则实际开发，不能仅换文案或假启用。

证据：[UI测试](../testing/evidence/now-v4-v41-integration-20261010/continuation17/ui-test02/receipt.json)、[AI测试](../testing/evidence/now-v4-v41-integration-20261010/continuation17/ai-test01/receipt.json)、[当前15版真机](../testing/evidence/now-v4-v41-integration-20261010/continuation17/phone/voice-before01-100-light.png)。原PROVIDER_APPROVAL/CREDENTIALS/SPEND_LIMIT、AGE007、AIR043/044/050及发布门槛保留。新功能真机、双账号、schema与集成验收尚未运行；全应用验收NO。

以下完整保留16批报告原字节：

---

# 当前16：餐厅共享读取取消修复（本地验证）

2026-10-11。沿用 `BT-FIX-NOW-UI-001`，实际修改1个后端生产文件并新增1个测试文件；没有另建任务、覆盖其他未提交修改或部署。本补丁**尚未构建或替换正在手机使用的API01**；手机仍为15包，本节本地证据不能替代下方15版真机证据。

多个请求同时查询同一公开城市范围时，原singleflight使用首请求的context。首请求取消，其他仍等待的用户便可能得到UNAVAILABLE，并需要重新搜索。保留的原码回归实际显示“initiating caller cancellation aborted the public read”与活跃跟随者UNAVAILABLE：**RED 1 PASS／1 FAIL**，反向取消跟随者的控制组原本通过。

现在只为共享的公开缓存→提供方→缓存操作创建独立 `Background` context，**最长12秒**；提供方原**9秒**子期限、固定公开城市坐标、类别、缓存key、fresh/stale及单进程合并策略不变。它不携带任何调用者context values、账号、私有原话或Task信息。操作可在所有调用者离开后完成这次有界公开缓存填充，没有自动重试或后台无限轮询。每位调用者仍按自己的context取消；在绑定自己的snapshot前再次核验取消。私人对话提交仍在原 `r.Context()`、ExpectedTask CAS和最终身份边界内，没有脱离个人请求写记忆。

| 实际修改 | 范围 |
| --- | --- |
| [online_restaurant_discovery.go](/D:/Project/birdtie/apps/api/internal/httpapi/online_restaurant_discovery.go) | 共享公开操作有界context；启动和最终绑定取消检查 |
| [online_restaurant_cancellation_test.go](/D:/Project/birdtie/apps/api/internal/httpapi/online_restaurant_cancellation_test.go) | 无sleep的实际组合测试：source阻塞／follower加入／首请求取消；反向取消；一轮读取、一次缓存写入、后续复用和私有字段隔离 |

| 本期执行 | 结果 |
| --- | --- |
| 两个取消方向测试，修复前（完整命令见回执） | [red01](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/continuation16/red01/receipt.json)：exit1，1 PASS／1 FAIL，源码稳定；原失败保留 |
| 同命令，修复后 | [green01](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/continuation16/green01/receipt.json)：exit0，2 PASS／0 FAIL／0 SKIP，源码稳定 |
| `go test -json -count=1 -timeout=60s ./internal/httpapi -run '^TestOnlineRestaurant'` | [regression01](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/continuation16/regression01/receipt.json)：exit0，160 PASS事件／0 FAIL／0 SKIP，源码稳定 |

命令目录为 `D:/Project/birdtie/apps/api`。160为Go JSON中测试及子测试的通过事件，包含新两项，不与2或下方15版315 Flutter相加。使用合成source/cache，实际走当前discovery、singleflight、snapshot及缓存复用组合；数据库、真实提供方、真实HTTP owned取消、12秒实际超时和completion/cancellation同时就绪分支 **NOT_RUN**。build／vet／race detector／本补丁装服与手机取消场景 **NOT_RUN**；没有扩大为全仓验证。源码/测试帧、原始stdout/stderr及首失败均保留。

[独立源码核对和真实边界](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/continuation16/independent-review16.json)、[当前16验收回执](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/continuation16/acceptance16.json)、[最终封存](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/continuation16/final-checkpoint01/seal.json)。旧15原完整报告字节保存在 [before-report15.md](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/continuation16/before-report15.md)，原封存不改。

进一步核对AI未完成项：真实适配路径 [tencent_wire.go](/D:/Project/birdtie/apps/api/internal/modelgateway/tencent_wire.go:110) 与 [model_request_live_sources.go](/D:/Project/birdtie/apps/api/internal/postgres/model_request_live_sources.go:833) 仍限定TEXT，尚无结构化CandidateProposal生产提交消费者；[schema.go](/D:/Project/birdtie/apps/api/internal/modelgateway/schema.go:154) 的“无候选”成功协议也未实现。AIR009仍PARTIAL，AIR043/044/050原依赖未满足，不能因本轮餐厅缓存通过而宣称AI候选→记忆→工具闭环完成。徒步human-candidate词汇与模型公共五类推断的分离是原有刻意边界，本轮未擅自扩展。

旧餐厅逐轮v2资料、统一卡片、账号Task快照、本机游客历史和公共数据库缓存已有实现，本轮没有重复开发或宣称这些旧手机结果在16版重新实测。模型／公开提供方本期仍OFF，新付费调用0。原队列256项、73项未闭合（P0为28），其他255任务对象与依赖／发布门槛保留，当前UI任务PARTIAL。全应用消费者验收NO，无ZIP，无生产部署。

---

以下15及更早报告按原字节保留；设备交付仍按各自实际构建判断。

# BirdTie：当前15版地图续读与原生语音

2026-10-11。已实际修改、构建并用 `adb install -r` 安装 **client-build15**，APK SHA256 `fa8b62cd60fc84c61e506b0a45995e6a6ab9be82a9c88d21e821e4f6adc281a5`。沿用原 `BT-FIX-NOW-UI-001`；未清应用数据、未覆盖其他未提交修改、未另建队列或部署。下面14版及更早报告按原字节保留，其中“语音未接通／自动地图续读未修正”是当时版本的事实，由本节更新。

地图问题的直接原因是原自动刷新把聚合弹层当成 Now 不可见；一次到点读取被跳过后未再排队，源到期时就只能手动重查。当前只有原 Now 或它拥有且当前可见的城市／图层／聚合弹层可续读；这些弹层被另页覆盖时暂停读取，返回时补当前范围。重读绑定 API、身份、城市、范围、作者与请求代次，合并相同的在途请求；短有效源也有至少2秒间隔的有界触发，HTTP409/5xx／transport最多一次延迟恢复，401/403／无效DTO不循环。原源仍按绝对期限和请求耗时保守到期，没有延长旧DTO的许可。

**真机地图跨期核对已通过。** 当前源原 `observedAt=2026-10-11T02:15:11.134345Z`、`validUntil=2026-10-11T02:17:11.134345Z`；在保持同一原聚合列表打开时，新的成功读取 `receivedAt=2026-10-11T02:17:01.178120Z` 早于旧期限，新 `observedAt=2026-10-11T02:17:01.038800Z`、`validUntil=2026-10-11T02:19:01.038800Z`。同epoch `4`，serial `6→7`，底层PUBLIC对象数10。跨旧期限后列表仍显示原组 **2个地点＋7条公开记录**，返回地图仍见9聚合与另一地点；没有手动刷新、重启或数据库写入发生在成功区间。这里10是当前底层来源对象数，9是已核对的原聚合成员数，不把它们当同一个统计。

| 当前15实测 | 原生截图 |
| --- | --- |
| 聚合列表、原期限前 | ![期限前](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/phone/continuation15-map02-members-before-deadline.png) |
| 同一列表、跨过原期限 | ![期限后](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/phone/continuation15-map02-members-after-original-deadline.png) |
| 返回地图，仍有原点位 | ![返回地图](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/phone/continuation15-map02-return-after-original-deadline.png) |

[原安全字段时序与同包回执](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/continuation15/map-phone02/result01.json)。只保存限定的日期、epoch/serial、count与布尔，不保存原始logcat、令牌或坐标。首次100秒新鲜度采集guard拒绝较早加载的源；随后记录了**测试前**冷启动、不清数据。首次stationary swipe未打开原生标注，原map-phone/result01失败仍保留；actual input tap打开后才开始独立map-phone02成功区间，不改写失败为通过。当前聚合刷新针对**打开时原memberIDs集合**的授权内容、标题及撤回；新加入ID需重新打开聚合，不称自动纳入全部新增成员。地点预览自己的旧许可不会被地图新源顺延。

语音已接入Android原生识别链路：优先真实本机识别能力，并只把已安装的所选语言模型视作本机就绪；否则显示本次系统服务实际标签并说明可能联网处理音频，每次由本人确认，再请求麦克风权限。当前小米手机实际显示 **“系统语音引擎”**，不能据此声称本机离线模型可用。识别语言为明确选择的中文或英语；本地不可用的二次系统确认沿用原选定语言，避免显示中文而实际按英语请求。使用[Android SpeechRecognizer接口](https://developer.android.com/reference/android/speech/SpeechRecognizer)，不把 PREFER_OFFLINE 当作离线承诺。

真实原生mic现可用。已实际打开说明窗并取消，麦克风权限before/after均false，原唯一空输入保留、mic恢复。取消、改草稿、身份／城市变化、同意弹窗被其他页面覆盖、页面退出及迟到回包都退休本次结果，最终文字只加入原可编辑草稿，不自动发送；输入预算超限保留原草稿并提示，不静默截短。原生可能收到系统回调缓冲，但立即丢弃，不保留、不主动上传，只向Flutter传最终文字；不承诺系统识别服务自己的存储政策。

真人试说已向用户发出文字提问，当前尚无已确认的物理语音结果，标记 **NOT_RUN**。未替用户选择系统音频接收者／麦克风权限，未注入转写或自动发送。

| 当前语音入口 | 实际15图 |
| --- | --- |
| 原唯一输入、mic可用 | ![入口](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/phone/continuation15-voice-ready-100-light.png) |
| 当前真实服务与语言同意窗 | ![同意窗](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/phone/continuation15-voice-consent-100-light.png) |
| 取消后原草稿与权限保持 | ![取消后](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/phone/continuation15-voice-consent-cancelled-100-light.png) |
| 实际键盘打开、原输入在键盘上方 | ![键盘](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/phone/continuation15-ime-100-light.png) |
| 原生Back后输入恢复 | ![键盘返回](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/phone/continuation15-ime-return-100-light.png) |

[实际语音入口／取消回执](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/continuation15/voice-phone/consent01.json)。默认入口不采音。新包键盘打开／Back实际检查使用 `dumpsys input_method`，原输入始终唯一、取消后mic可用；[原生键盘回执](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/continuation15/keyboard-phone01.json)。权限接受／拒绝、停止录音、语音时切后台／换身份、英语／混排质量与系统服务变更的**手机**矩阵，本批仍 **NOT_RUN**；相应控制器／真实mounted测试另有通过证据，不能替代物理识别。iOS、浏览器语音、TalkBack、profile/release性能、地图闪烁与录屏 **NOT_RUN**。本期这些新实图仅100%浅色，不能借用14包130%/200%图片称15语音手机矩阵通过。

本次实际修改客户端13文件及原GLOBAL-UX-INTERACTION-CONTRACT追加规则。没有替换字体、引入字体服务或改V4原地图、唯一输入与主页面入口。保持已有回答15/w400、标题角色18/w600与16/w500、输入16/w400、辅助13；语音同意正文复用现有语义body，语言控件在原窗内自然换行，不叠额外语言PopupRoute。

| 客户端文件 | 范围 |
| --- | --- |
| [AndroidManifest.xml](/D:/Project/birdtie/apps/client/android/app/src/main/AndroidManifest.xml) | 修改 |
| [MainActivity.kt](/D:/Project/birdtie/apps/client/android/app/src/main/kotlin/app/civu/civu_mobile/MainActivity.kt) | 修改 |
| [VoiceInputRecognizer.kt](/D:/Project/birdtie/apps/client/android/app/src/main/kotlin/app/civu/civu_mobile/VoiceInputRecognizer.kt) | 新增 |
| [map_cluster_members_sheet.dart](/D:/Project/birdtie/apps/client/lib/src/city/map_cluster_members_sheet.dart) | 新增 |
| [native_city_map_io.dart](/D:/Project/birdtie/apps/client/lib/src/city/native_city_map_io.dart) | 修改 |
| [native_city_map_stub.dart](/D:/Project/birdtie/apps/client/lib/src/city/native_city_map_stub.dart) | 修改 |
| [public_city_map.dart](/D:/Project/birdtie/apps/client/lib/src/city/public_city_map.dart) | 修改 |
| [agent_composer.dart](/D:/Project/birdtie/apps/client/lib/src/workspace/agent_composer.dart) | 修改 |
| [map_canvas.dart](/D:/Project/birdtie/apps/client/lib/src/workspace/map_canvas.dart) | 修改 |
| [map_layers_api.dart](/D:/Project/birdtie/apps/client/lib/src/workspace/map_layers_api.dart) | 修改 |
| [map_layers_controller.dart](/D:/Project/birdtie/apps/client/lib/src/workspace/map_layers_controller.dart) | 修改 |
| [map_workspace.dart](/D:/Project/birdtie/apps/client/lib/src/workspace/map_workspace.dart) | 修改 |
| [voice_input.dart](/D:/Project/birdtie/apps/client/lib/src/workspace/voice_input.dart) | 新增 |

契约：[GLOBAL-UX-INTERACTION-CONTRACT.md](/D:/Project/birdtie/docs/ux/GLOBAL-UX-INTERACTION-CONTRACT.md)。map层API另统一严格bodyBytes UTF8读取与typed HTTP错误。精确验证表明 `Content-Type:application/json` 无charset的原读取本来就正确；乱码来自完全无Content-Type的旧Host测试响应。该修正是缺header防御，**不能称正式Go接口已证实存在无charset中文bug**。

**最终合并24相关文件：315 PASS，exit0，源码／测试帧稳定并与当前APK一致。** 计数是expanded runner最终统计，不声称日志逐标签已独立列出完整unique leaves。地图worker210、语音worker52与根315相互覆盖，不累加。root-test01在中间帧315 PASS；局部公开Notifier入口与新告警修正后，最终root-test02再315 PASS。历史RED／fixture失败／超时及各源帧完整保留。新增测试验证被覆盖弹层无读取、返回补一次、短期限、UTC已过期DTO拒收、singleflight、撤回／身份ABA、永不回复platform future的卸载与迟到、语言回退、唯一输入/IME、V4社交与对比度。不是全应用全套测试。

[最终测试回执](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/continuation15/root-test02/receipt.json)、[原始日志](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/continuation15/root-test02/raw.log)。10生产文件定向分析exit `1`：**0 error／1既有warning／23info，非clean**；既有warning为unused `_openSeedRoute`。[分析回执](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/continuation15/root-analyze01/receipt.json)。debug Gradle构建成功，原Mapbox KGP迁移提示仍留在构建日志，不把debug结果当release验收。API源码／执行文件沿用01、本期Go/API全套 **NOT_RUN**；模型/provider **OFF**、没有付费Agent调用、生产部署 **NOT_RUN**。因此不能用本轮地图GET验证宣称餐厅联网推荐、数据库缓存成本、全部在线结果长期记忆或AI模型效果已完成。

[独立最终源码审核](/D:/Project/birdtie/work/now-v4-v41-integration-20261010/continuation15/independent-audit/final-source03.md)、[本轮实际手机审核](/D:/Project/birdtie/work/now-v4-v41-integration-20261010/continuation15/independent-audit/phone-review04.md)、[当前验收边界](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/continuation15/acceptance15.json)。根队列仍256项、73项未闭合（P0为28），其他255任务对象保留，原UI任务仍PARTIAL；本期完成两个限定范围，不声称全应用消费者验收通过。[最终封存](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/continuation15/final-checkpoint01/seal.json)。无生产部署，无ZIP。

---

以下14及更早检查点原字节保留；以本节15版结论为当前依据。

# BirdTie：当前14版V4社交布局修正

2026-10-11。已实际修改并安装 **client-build14**，APK SHA256 `a2db0c384a1488e153042cb1421d17e4f79a8cc05984835c3a3a146871f6078b`。本节替代下方12版的冲突界面结论，原12及更早报告字节保留。沿用 `BT-FIX-NOW-UI-001`，没有另建队列、清除应用数据或部署。

当前实现把右侧从248/280dp的大文字列收回 **64dp**：36dp头像、13/w400/1.5单行昵称，长名省略且实际点击能看到全文；保留至少48dp主要触控。成功读取但为空的关注/对话栏目不常驻，已有查找和消息操作改为同侧纵排，原导航、身份/source绑定及退休守卫保留。分组与操作13/w500/1.5。真实操作栏底边+4dp锚定不变。

右侧仅绘制 **96dp** 局部浅色渐变，复用语义paper `#FAFBF8`，横向透明至95%，上缘24dp、下缘32dp柔化；没有BackdropFilter或全地图模糊。13版第一轮实图暴露的硬上缘已在14修正。深色应用中这部分仍匹配实际浅色地图，并使用地图深色文字。输入占位15/w400/1.5、复用metadata颜色降低强调；正文输入沿用原16/w400；系统TextScaler、唯一地图/输入、键盘及面板规则保留。

**语音未接通。** 检索确认无应用录音/转写服务或权限绑定。空输入显示明确禁用的mic，非空显示原发送按钮；没有让假按钮执行键盘动作冒充语音。真实录音/转写与手机非空发送全链路是 **NOT_RUN**，后者只有本轮组件测试实际调用原发送回调的证明。

| 本次修改文件 | 作用 |
| --- | --- |
| [now_social_edge.dart](/D:/Project/birdtie/apps/client/lib/src/workspace/now_social_edge.dart) | 紧凑昵称、完整名Tooltip、隐藏成功空组、同侧操作、局部软渐变。原4344字节State绑定区不变，不是整个文件前缀不变。 |
| [agent_composer.dart](/D:/Project/birdtie/apps/client/lib/src/workspace/agent_composer.dart) | 占位语义样式；空mic禁用／有文字原send。 |
| [now_ui_tokens.dart](/D:/Project/birdtie/apps/client/lib/src/workspace/now_ui_tokens.dart) | 64/96/32dp语义尺寸、地图paper与composerHint。 |
| [map_workspace.dart](/D:/Project/birdtie/apps/client/lib/src/workspace/map_workspace.dart) | 右侧锚点对齐64dp列，保留原顶部+4dp。 |

更新测试：`now_social_edge_test.dart`、`agent_composer_test.dart`、`now_map_overlay_contrast_test.dart`；在既有GLOBAL-UX-INTERACTION-CONTRACT末尾追加本次明确覆盖规则，没有替换全应用字体或字体服务。

[原V4与当前三张真机图并排对照](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/social-v4-correction14/V4-actual14-comparison.md)。三张主图：[收起](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/phone/social14-collapsed-100-light.png)、[展开](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/phone/social14-expanded-100-light.png)、[键盘](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/phone/social14-ime-100-light.png)。同一长中英文名的补图：[全文](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/phone/social14-full-name-100-light.png)、[130%](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/phone/social14-expanded-130-light.png)、[200%约320dp窄屏](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/phone/social14-expanded-200-narrow-light.png)、[深色](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/phone/social14-expanded-100-dark.png)。原V4没有已确认的键盘图，明确NOT_AVAILABLE。

实际核对显示：名称不撑到地图中部，两操作在同一侧；当前200%窄屏的“找朋友”自然换行，消息和输入可达；键盘展开后唯一输入位于键盘上方。phone03四帧后400ms回退断言失败保留，phone04补三帧error=null；不能把7张合并图片称一次全部通过的矩阵。[同包7图原生几何回执](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/social-v4-correction14/acceptance14.json)；[独立实际图审核](/D:/Project/birdtie/work/now-v4-v41-integration-20261010/social-v4-correction14/actual-audit14/actual-audit14.md)。静态截图不证明地图闪烁、所有触控或AT。

**“9”口径已实际查明。** 打开原聚合并滚动取齐成员，去重并集是 Gallery、Provost Skene's House 两PLACE和七条明确公开的合成MOMENT；类型/ID逐项对上原只读数据库快照。9是聚合可见内容对象数，2/7是同组类型分解，不是9地点、7作者或全城总数；全城冻结来源另有Maritime Museum，不在此组。原权限过滤、ID去重及Mapbox署名保留。[成员初始](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/phone/social14-count-members-initial.png)、[底部](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/phone/social14-count-members-bottom.png)、[独立ID核验](/D:/Project/birdtie/work/now-v4-v41-integration-20261010/social-v4-correction14/count-native-audit14/membership-audit.md)。原helper把ASCII单引号误写成弯引号导致的FAIL亦保留，不是地点或APP数据错。

核对期间来源到期：[返回图](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/phone/social14-count-return.png)实际已显示到期且标记退役，不能称返回后仍保留9。之后仅点击既有“更新地图点位”，[真实重载图](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/phone/social14-count-reloaded.png)恢复9和另一地点，无新入口、DB写入或联网推荐。**自动源续期未修正／NOT_PROVEN**；这一条与长期对话记忆是不同范围，仍需后续处理，未用延长TTL绕过权限退役。

实际运行8个相关Flutter文件，最终 **test05：110 PASS／exit0**；覆盖64dp列、三种宽/字号条件、全文点击、48dp、空组、原身份/ABA退休守卫、唯一地图/输入、发送与聚合类型去重。源码帧与当前14 APK一致。前三次为95/15、107/3、107/3（PASS/FAIL），修正新旧断言后13源码110 PASS；柔化上缘后的14再跑110 PASS，旧失败日志保留，不累计成全应用数量。

四生产文件 `analyze05` **exit1，24 issues＝1 warning＋23 info、0 error，非clean**；warning为原`_openSeedRoute`未引用。机械detect输出`[]`，只作静态补充，不能替代Flutter视觉验收。局部合成背景对比度测试覆盖选定瓦片与0.88 paper合成，不宣称所有地图位置／渐变边缘均已测。debug构建和`adb install -r`成功且不清数据；API源码未改、模型/provider OFF，无新增付费调用，Go/API全套本轮 **NOT_RUN**，不冒用历史测试为当前全应用验收。

真实找朋友／消息完整导航、非空关注／对话和长多联系人名单、TalkBack/AT、高对比度／减少动画手机开关、130%／200%真实IME、200%深色、profile/release性能／地图闪烁、语音录音转写、全应用E2E均 **NOT_RUN**。本轮完成当前布局修正与有限真机核对，整体消费者验收仍 **NO**。沿用原任务记PARTIAL，其他255任务对象保留，原队列256项、73项未闭合（P0为28）；[当前封存](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/social-v4-correction14/final-checkpoint01/seal.json)。无生产部署、无ZIP。

---

以下12及更早检查点原文按原字节保留；其中“当前／最终”只代表当时版本，不能覆盖上面的14版结论。

# BirdTie：当前12版顶部与右侧展开修正

2026-10-11，北京时间。本节是最新检查点；后面的07版原报告属于历史记录。手机已实际安装 **client-build12**，APK SHA256 `edb10276c66995f55933965a64a841770a6739123fb82e2697d26df08e6a703a`；API仍为原01。沿用 `BT-FIX-NOW-UI-001`，保留原未提交修改、账号/应用数据、旧证据和队列历史，不新建任务或部署。

## 原因与修改

旧右栏错误复用了地图内容区起点，把左侧48dp控件预留及相关间隔也算进来。07原生XML显示：顶部入口底边到首个“找朋友”操作空了 **72dp**；64dp窄列使长名字变成多行竖排。顶部只有一次SafeArea，实际问题是额外8dp上padding和背景过渡结束位置，不能把系统安全区直接去掉。

现在顶部操作上移 **8dp**，保留状态栏/开孔安全区；右栏从真实操作栏底边 **4dp** 开始，右边距8dp。顶部背景增加20dp绘制尾部，不把尾部算进任何控件或列表高度。高对比度、减少动画及accessibleNavigation的opaque回退也排除这段尾部，避免深色背景压到列表标题。

列表按“好友／关注／对话”分组，头像和名称横排，标题/数量/空状态与人名列对齐；分组相隔16dp，查找与消息入口并排。普通列宽248dp，大字号优先280dp，同时不超过屏宽减80dp；头像视觉36dp、头像区48dp、人名间隔12dp、整行触控至少56dp。名称15/w400/1.5，分组/操作14/w500/1.5，数量/辅助13/w400/1.5，复用此前确认的字体、语义色及系统TextScaler。长中文/English自然换行，长内容允许滚动。

08、09第一轮实图仍有首组标题与原生Mapbox信息按钮相交，已明确判定 **NEEDS_FIX**，不能算完成。12把标题与空状态缩进到人名列，原生XML在100%浅/深、130%、200%窄屏的5种社交截图中均无这两者相交。只修这个实际冲突，不靠隐藏Mapbox信息入口或缩小文字解决。右栏仍全透明，没有左右渐进模糊或整列白底。

10、11的首次矩阵各0帧/失败：列表已经真实展开，但顶部社交按钮的原生语义节点消失。原 helper 事件名“首次无效”只是旧标签观测，不能断言第一次点击没执行。显式按钮 label/button/onTap 保留原触屏回调和Tooltip；真实宿主测试进一步证明，accessibleNavigation切换会更换顶部容器并重新挂载祖先。12使顶部始终使用同一容器，只改变背景绘制及尾部，保留原地图/输入实例和3个读取。当前手机5个社交状态的“收起好友与关注”及IME状态的“打开好友与关注”均恢复为唯一可点击原生节点。这个结果支持修正后的实际行为，不把Android/SDK内部原因写成已完全查明，也不代替TalkBack测试。

| 本次生产文件 | 修改内容 |
| --- | --- |
| `apps/client/lib/src/workspace/top_controls.dart` | 去掉8dp额外顶部padding；20dp背景尾部不计真实操作栏高度；独立稳定按钮语义，保留原Tooltip、48dp和触屏回调。 |
| `apps/client/lib/src/workspace/map_workspace.dart` | 右栏锚定真实操作栏底边＋4dp；顶部opaque背景单独处理尾部。 |
| `apps/client/lib/src/workspace/now_social_edge.dart` | 响应式透明分组、头像名称横排、文字对齐、空状态与并排操作。原身份/source/读取及ABA守卫所属State区块（4344字节）与修改前逐字节一致。 |
| `apps/client/lib/src/workspace/now_map_edges.dart` | 默认行为不变；顶部显式提供opaqueTailInset时保持同一Stack宿主，仅opaque绘制收回尾部；不更换操作栏祖先或子组件大小/触控。 |

对应测试改动：`now_p0_chrome_test.dart`、`now_social_edge_test.dart`、`now_social_edge_host_test.dart`。保留唯一地图、原输入框、草稿、面板边界、键盘和卡片点击行为；不增加字体服务或网络请求。

## 当前12版真机证据

| 截图 | 实际结果与边界 |
| --- | --- |
| [修改前07，100%浅色](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/phone/header-social-before-100-light.png) → [修改后12，100%浅色](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/phone/header-social12-social-100-light.png) | 同手机、主题、字号和测试账号；多余72dp空档变4dp，操作栏上移8dp。相机可能不同，不称逐像素A/B。 |
| [12深色100%](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/phone/header-social12-social-100-dark.png) | 操作栏落在顶部背景内；列表在实际浅色底图上使用原深色地图语义文字。 |
| [12字号130%](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/phone/header-social12-social-130-light.png) | 长中文/English名称自然换行。 |
| [12字号200%窄屏](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/phone/header-social12-social-200-narrow-light.png) / [滑动后](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/phone/header-social12-social-200-narrow-scroll-light.png) | 约320dp宽，姓名完整、查找/消息入口在输入框上方；当前仅1个合成好友加空分组，初始已经可见全部内容，滑动事件不证明长非空名单滚动位移。 |
| [12键盘100%](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/phone/header-social12-ime-100-light.png) | 实际dumpsys input_method确认键盘展开，唯一Now输入在键盘上方，收起后恢复地图。 |
| [12城市100%](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/phone/header-social12-city-100-light.png) | 当前城市、搜索、关闭可见；原城市modal罩层生效，underlay可见不代表社交source已退休。 |

同12包矩阵7帧、error=null，每图保留实际PNG/XML/JSON及构建条件，末尾恢复100%、浅色、physical density520。[原生几何与相交回执](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/header-social-layout13/layout-metrics04.json) / [12矩阵回执](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/header-social-layout13/phone-matrix05/receipt.json)。图片不证明全地图、所有触控区域或AT完整通过。

## 实际测试和边界

最终 `green08` 为 **87 PASS / exit0**，6个直接相关Flutter文件，源码帧与当前12构建一致。覆盖身份绑定、真实宿主、48dp操作、大字号/窄屏、唯一地图/输入和IME；包含3个opaque辅助组合、3个分组/名称对齐、2个Tooltip开关下的语义叶正反动作和1个真实宿主辅助状态切换用例。

原布局Red01为56 PASS/22 FAIL；Green01为72 PASS/6 FAIL，原因是原测试从移动的key寻找InkWell，随后把key放回外层语义节点，保留原退休回调断言，Green02为78 PASS。opaque组合原0 PASS/3 FAIL→Green03共81 PASS；实际08/09原生相交发现后，对齐用例旧源0 PASS/3 FAIL→对齐修复84 PASS；新增独立按钮语义后语义修复86 PASS；真实宿主辅助切换修复后最终87 PASS。所有失败日志和09版报告快照保留，数量不相加成全应用总数。

语义新增测试初版误从外层SizedBox取祖先节点，Green05为84 PASS/2 FAIL；改为遍历可达语义叶后，Green06为84 PASS/2 FAIL，剩余失败来自语义handle未在测试结束验证前释放，随后用finally释放。修正测试在exact10源码上 `semanticsRed03` 为0 PASS/2 FAIL，候选11字节在finally精确恢复，再Green07为86 PASS。该RED证明显式label和Tooltip独立性，不冒充Android桥内部复现。两次旧源重建SHA防护失败均发生在任何生产写入之前；记录保留。真实宿主 `headerTransitionRed01` 为0 PASS/1 FAIL，实际祖先从DecoratedBox换到Stack；12修正后87 PASS，原权限/退休/源码断言未削弱。

`analyze07`检查4个生产文件，**exit1：23 issues＝1 warning＋22 info，0 error，非clean**。debug构建和 `adb install -r` 成功且不清数据；API源码未改，先前102叶Go回归和07版231项Flutter检查仍是各自历史范围，不能替代12版全应用测试。

真机高对比度/TalkBack/系统减少动画开关、200%深色全矩阵、长非空社交名单、全应用E2E、profile/release掉帧及地图闪烁均 **NOT_RUN**；辅助标志组合只在挂载组件测试中通过。模型/provider继续OFF，无新增付费或联网推荐调用。整个产品仍未达到完整消费者验收。

本次沿用原任务记 **PARTIAL**；其他255项与原证据/历史保留，队列仍256项、73项未闭合，其中P0为28项。[本次队列回执](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/header-social-layout13/final-checkpoint01/queue-receipt.json) / [当前源、测试和图片封存](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/header-social-layout13/final-checkpoint01/seal.json)。原07报告字节备份和seal保留，原录屏仍是07，未改名成12证据。

---

以下是原07检查点报告，原文保留；其中“当前”“最终”均指当时07版，不能覆盖上面的12版结论。

# BirdTie Now V4/V4.1：本轮修改与实际验收

2026-10-11，北京时间。沿用原 `BT-FIX-NOW-UI-001`，正式仓库 `D:/Project/birdtie`。保留本轮开始前的未提交修改，没有新建重复队列、生成 ZIP、提交或部署生产。

本轮实际完成左右无渐进模糊、指南针移到左侧、右侧透明且从顶部向下展开；修复关闭地点预览后恢复旧大白面板的问题。地点公开动态已接通真实后端分页，并在手机完成首批5条、续读2条、打开排序第6条原动态的流程。**这些是局部开发与本地验收结果；完整消费者验收未完成。**

## 构建与数据归属

最终客户端为本轮 `client-build07`，debug APK SHA256 `692b4090a32b9732d98d5791341bfd2c23847cb4d1540e425a8a53145b359f24`。API为 `api-build01`，SHA256 `443155614cf38a2001e3d1efed08369b826773bba5e7b5505a0e4434ffbec04e`。构建输入帧稳定，客户端与最后231项测试的生产源码帧一致；实际 `adb install -r` 成功，没有清除手机应用数据。06为颜色修正前的真实检查点，历史证据保留。

手机 `c641566b`，原始分辨率1220×2656、density520；窄屏检查使用density610，约320逻辑像素宽。访问独立的owned本地QA数据库，模型和外部provider关闭，无新付费调用。验收动态明确标记为合成文字，经原草稿→发布预览→本人确认公开接口创建；没有伪造图片、头像、到访或营业信息。这不是生产内容、真实IdP或两个真实用户验收。

构建/安装与原始证据位于 [本轮证据目录](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010)。每张图片配有原始PNG、XML与JSON，记录实际APK/API、字号、主题及设备条件，不能只用文件名判定通过。

## 修改文件与行为

客户端文件均在 `apps/client/lib/src/`：

| 文件 | 实际修改 |
| --- | --- |
| `workspace/map_workspace.dart` | V4入口和面板状态整合；当前城市来源恢复；社交入口绑定当前connection；关闭或退休地点轻预览时不恢复旧EntityPeek大白面板。原Agent任务、回答、结果集、阅读位置、草稿和Activity面板保留。 |
| `workspace/top_controls.dart`、`workspace/now_map_edges.dart` | 平面轻主控；去除左右渐变和模糊。保留原顶部/底部纵向过渡及高对比度、减少动画回退。 |
| `city/public_city_map.dart`、`city/native_city_map_io.dart`、`city/native_city_map_stub.dart`、`city/now_map_compass.dart` | 使用原生地图bearing与朝北操作；指南针位于左侧，48dp触控，保持唯一地图实例。 |
| `workspace/now_social_edge.dart`、`workspace/now_social_edge_source.dart` | 整列透明，从右上入口向下展开；好友/关注/实际会话与未读来自现有接口。保留当前身份、client、API/source及ABA退休守卫；长名字换行，超长内容滚动。原私人照片不作为公开头像。 |
| `workspace/map_canvas.dart`、`workspace/map_marker_bitmap.dart` | 原生投影坐标及类型化地点/动态/公开作者标记，使用实际cluster计数；不创建虚假坐标或作者。 |
| `workspace/now_place_preview.dart`、`workspace/now_public_place_panel.dart` | 附着地图的地点轻预览；横向动态按空间呈现1–3张卡片。失败、撤回或版本变化后可在原预览重新读取当前公开数据。 |
| `workspace/place_history_controller.dart`、`workspace/place_history_pagination.dart` | 接通公开动态分页、严格closed DTO及游标；后页不能延长初次授权期限，旧身份/source迟到回包不可复活。点击后页动态仍重新读取该稳定ID的当前权限。 |
| `workspace/now_ui_tokens.dart` | 提取原浅色语义色常量，复用于产品当前固定浅色地图上的社交文字和指南针；地图小字使用原正文色而非低对比度的浅次要色。普通深色面板和既定文字参数不变。 |

后端文件位于 `apps/api/internal/`：`placehistory/model.go`、`placehistory/page.go`、`httpapi/place_history.go`、`httpapi/place_history_page.go`、`httpapi/server.go`、`postgres/place_history.go`、`postgres/place_history_page.go`。新增正式 `GET /v1/places/{placeID}/social-history/moments`：先按原ACL过滤再统计，固定30天窗口，按 `(publishedAt,id)` 倒序，单页5条和第6条lookahead，canonical opaque cursor。

最后原生PG语句再次核实原Session ID/generation、来源和原截止时间，撤回、Block或资料私密切换后不返回已经编码的旧内容。正常Session idle续期不延长旧公开预览许可。公开资料只有确实opt-in配对才返回作者，私有owner资料不推广为公开身份。

`now_ui_tokens.dart`复用此前用户确认的字号、字重和行高，并新增实际地图背景的独立语义色。本批没有替换字体、引入字体服务或全局乘缩放系数。产品和交互契约已追加至 `docs/product/NOW-AGENT-MAP-WORKSPACE.md`、`docs/ux/GLOBAL-UX-INTERACTION-CONTRACT.md`，原文保留。

字体来源检查：`app/birdtie_surfaces.dart`的ThemeData未显式设置fontFamily/fontFamilyFallback；TextTheme由NowUiTokens映射，继承Flutter平台字体配置，pubspec未加入新的字体服务。地图位图中的字体显式为Roboto；中文最终字形由平台/引擎回退提供。逐字形最终命中的手机字体文件本批 **NOT_RUN**，不猜测为某个中文字体。

## 文字与布局参数

| 语义角色 | 字号 / 字重 / 行高 |
| --- | --- |
| 页面主标题 | 20 / w600 / 1.4 |
| 面板标题 | 18 / w600 / 1.5 |
| 地点、动态卡片标题 | 16 / w500 / 1.5 |
| 正文、Agent回答、用户消息、地址 | 15 / w400 / 1.5 |
| 输入文字 | 16 / w400 / 1.5 |
| 按钮 | 14 / w500 / 1.5 |
| 菜单、轻主控 | 15 / w500 / 1.5 |
| 时间、来源、辅助说明 | 13 / w400 / 1.5 |

浅色面板语义颜色：标题 `#20242A`、正文 `#374151`、次要文字 `#566273`；动作使用原 `colorScheme.primary`。深色面板使用原onSurface/onSurfaceVariant。当前产品Mapbox底图固定浅色，与系统深色模式独立，因此地图叠加标题为 `#20242A`、小字和指南针为 `#374151`，字形halo为白色；没有新增背景条。代表浅地图底色最低约6.97:1，普通次要色在海蓝上的约4.19:1不足，因此不用于地图小字。保留系统TextScaler和原字体；复杂底图标注重叠与全地图背景验收仍有边界。

主要操作48dp；输入外层至少56dp，原始XML的inner EditText高40dp，不能称每个语义节点均≥48dp。社交列宽64dp；轻预览折叠上限250dp、展开390dp；一次最多3张可见卡片，数据单页5条和继续读取是独立规则。

## 实际运行的检查

| 检查 | 实际结果与边界 |
| --- | --- |
| `finalIntegration04`，24个直接相关Flutter文件 | **231 PASS，exit0，源码帧稳定**。包括地图宿主、社交身份绑定、预览与分页、城市恢复、IME、字体卡片、原生HTTP原字节解码、实际浅底地图在两种应用主题下的叠加对比度。组件Mock流程不冒充真机网络。 |
| 关闭预览原失败范围 | 原生产源码 `closeSurfaceRed01` 为1 PASS/5 FAIL；修复后Green01为5 PASS/1 FAIL，最后正控夹具对齐且真实滚动可见后Green02 **6 PASS**。失败日志保留；不削弱阅读位置、实例、原结果与Activity正控。最终231中已包含这6项，不重复相加。 |
| 透明社交 | 实际新规范Red02为5 PASS/8 FAIL→修复后相关49 PASS；已纳入231。旧名含Red的Red01实际10 PASS，是原opaque规范基线，不写成失败。 |
| 动态失败恢复 | 原0 PASS/5 FAIL→5 PASS，纳入231。覆盖重新获取新页、版本变化、撤回及原预览期限不延长；HTTP使用MockClient。 |
| 地图叠加暗色失败回归 | 06真机白字/指南针在浅地图上不清楚；原生产源码Red01为20 PASS/6 FAIL→颜色修正后Green01 **26 PASS**，其中4个新增叶纳入231。检查实际挂载字色、代表地图背景、原面板独立配色、3个GET不变、48dp/20图标及原朝北操作。 |
| 原生后端 `page-regression02` | **102不同Go叶PASS：37原生PG/注册HTTP＋65 domain/SQL shape/单位契约**；当前全API源帧与构建01一致，前后稳定，owned测试库身份及数据库名单不变。不是102个原生场景，也不是全仓套件；旧01保留，不重复相加。 |
| 原生读取后撤权barrier | 原6 FAIL/3 PASS→9 PASS。公开撤回、双向Block、ABA及资料private→public在编码之后也不能返回旧许可；原正常idle续期、Session撤销、自然到期行为保持。 |
| 原生分页wire | 实际HTTP首5＋续2、终页cursor=null；客户端读取这两份原字节。另有手机实际连续流程，见下文。 |
| `analyze06`，13个生产文件 | **exit1：37 issues＝1 warning＋36 info，0 error；不是clean/PASS**。包含既有未使用`_openSeedRoute`和import/括号建议，原输出完整保留。 |
| debug客户端/API构建及手机安装 | 两端exit0，源帧稳定；最终07实际安装Success，数据保留。profile/release性能不由debug构建证明。 |

所有测试数量按其独立检查点记录，范围互有重叠，不相加成全产品通过数。日志、命令、SHA与源帧见 `verification/` 和native receipts。

## 真机前后与06连续流程证据

旧整条白底的 [build04社交截图](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/phone/checkpoint04-social-100-light.png) 与当前 [build06透明好友展开](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/phone/checkpoint06-social-native-friend-100-light.png) 对照。均为同设备100%浅色；旧图为访客，新图包含本地合成好友，内容不同，**不是逐像素同内容字体A/B**。

旧 [build05关闭预览后大白面板失败图](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/phone/checkpoint05-place-camera-refreshed-100-light.png) 与当前 [build06关闭详情返回地图](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/phone/checkpoint06-close-reader-map-100-light.png) 记录实际前后行为；地图视角可能不同，不从图推断选择ID被清空。

当前06真实连续分页：[首5条中的1–2卡](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/phone/checkpoint06-native-stories-first5-100-light.png) → [已读7条中的5–6卡](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/phone/checkpoint06-native-stories-later6-100-light.png) → [排序第6条原动态详情](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/phone/checkpoint06-native-later-moment-reader-100-light.png)。流程真实exit0，receipt位于 `phone-story-workflow06/receipt.json`；它验证原生续页与具体对象打开，不证明完整正文已经滚读或真实用户供给质量。

当前 [06键盘100%](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/phone/checkpoint06-ime-100-light.png) 附真实input_method proof，唯一Now输入框在键盘上方。后续同包130%、200%、约320dp窄屏及深色矩阵的准确图名与视觉结论在本报告最终封存附录，不由旧03字号矩阵替代。

## 剩余需求与NOT_RUN

原队列256项，本轮检查点前183 DONE、47 TODO、13 BLOCKED、12 PARTIAL、1 IN_PROGRESS，**73项未闭合，其中P0为28项**。原Now30＋PG26映射149个去重任务，53项未闭合；新16节对应151项，额外两项已DONE，因此仍53。**53是73的子集，不相加；73是工作项，不能解释成73个已知bug或消费者完成率。** 本批局部修复仍记录在原任务，最终状态与队列SHA见封存附录。

仍未完成的主要领域：地图内人类聊天和按会话草稿、双真实账号Friend→Chat→Share→回复；公开媒体/头像/封面完整wire、唯一作者数、正式内容供给；Journey/ThemeMap真实domain接通；完整语音/STT、材料/大文件/视频和恢复；真实模型推荐、规划、工具效果、多模型运营、长期学习及纠正/忘记传播；Civu正式ETL迁移、IdP/HTTPS及Pilot/Beta门禁。

本批真实付费模型/provider成功、跨24小时对话留存及新餐厅联网质量、全应用E2E、全部73项闭合、真实IdP/两个真实账号、跨城市GPS失败完整矩阵、AT、弱网/大数据容量、profile/release掉帧和地图闪烁、iOS与生产部署均 **NOT_RUN/未完成**。静态截图不能代替这些检查。已实现的缓存与历史也不等于永久复用过期访问许可。

最终结论：**最新透明展开、左侧指南针、无左右模糊及地点分页已实际开发并在当前手机核验；完整消费者验收NO。**

## 最后07同包视觉检查点

06深色模式实际失败图保留：应用白字与浅色地图混用，社交文字发虚、左侧朝北难辨。07使用上述地图语义色后，在 [07深色实图](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/phone/checkpoint07-social-native-friend-100-dark.png) 中文字和指南针清晰；深色顶底与输入面板仍是深色。06/07镜头不同，不作同背景逐像素A/B。

最终手机图库固定15帧，同APK07/API01；独立视觉审计为 [07-04](/D:/Project/birdtie/work/now-v4-v41-integration-20261010/phone-visual-audit07/visual-audit07-04.md)，其86文件manifest SHA `2097bda439739b6a77c9424a8f28c8899527a9d2310de85a7d97f2fd75e81034`。root也实际查看了主要前后图、当前暗色、130%、200%窄屏尾部、IME与餐厅卡片；审计保持PARTIAL，不以截图采集exit0代替视觉通过。

| 当前07真实截图 | 实际结论 |
| --- | --- |
| [透明好友100%浅色](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/phone/checkpoint07-social-native-friend-100-light.png) | 无整列白底、无左右渐进模糊；长中文/English名字自然换行，唯一Now输入。 |
| [130%好友](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/phone/checkpoint07-social-native-friend-130-light.png) | 名字、英文自然跨行；列中空状态保留，不禁用系统缩放。 |
| [200%窄屏实际滚动尾部](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/phone/checkpoint07-social-native-friend-200-narrow-scroll-end-light.png) | 约320dp宽实际滚到“暂无关注／对话／暂无对话”；这是滚尾可达证据。固定入口可收起；文字变大不会缩小触控区。 |
| [城市100%](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/phone/checkpoint07-city-100-light.png) / [城市200%](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/phone/checkpoint07-city-200-light.png) | 标题、搜索、当前Aberdeen和关闭入口可见、可操作，没有城市读取报错。当前本地只覆盖这一城市内容，不冒充跨城市GPS完整矩阵。 |
| [IME100%](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/phone/checkpoint07-ime-100-light.png) / [IME200%窄屏](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/phone/checkpoint07-ime-200-narrow-light.png) | 四组100/130/200/200窄屏真实input_method证明，唯一输入在键盘上方；200%提示自然换行，地图保留，无旧大空白。 |
| [缓存餐厅回答](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/phone/checkpoint07-agent-cached-answer-100-light.png) / [统一地点卡片](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/phone/checkpoint07-agent-cached-cards-100-light.png) | root在手机唯一输入提交ASCII问句“Are there good restaurants?”。原Agent显示复用15家已保存资料，提供中文回答、范围和采集时间，并展示同形式地点卡片；评分与当前营业状态明确未核实。首卡完整、第二卡底部处于自然滚动视口，不声称15张均已滚看。 |

同包矩阵实际 `phone-matrix09` 命令exit0，10次capture、真实窄屏滚动事件和恢复状态完整；首次08原ADB展开没有生效，NOT_PROVEN尝试保留。09仅在实际观察仍关闭时重按原按钮，不盲目切换；没有以改产品权限或延长租期解决自动化等待。字号最终恢复1.0、浅色、physical density520。

这组15帧不证明200%深色完整矩阵、所有15卡操作触达、非空草稿恢复、正文全部滚读、中文实输入解析、实时联网或新的模型质量，仍为 **NOT_RUN**。公开动态06的原生续页证据不改名成07；最后3文件颜色变化的直接回归与07布局另有准确源帧。

本地QA运行环境保留供手机继续调试：原owned API及当前USB helper只维护本应用18091→18112映射；模型/provider关闭。它不是生产服务或Windows开机服务；其他数据库、映射、账号及原应用数据未清除。

## 当前07原操作录屏

[手机原操作录屏](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/phone-recording06/now-native-workflow.mp4) 为当前APK07/API01的实际Android screenrecord，610×1328、61.57秒，SHA256 `edace7333427af1387e0f3fef87cbae5a8e2c887c417c9c9d168a5a6d6c3e5c2`。同一计时进程依次点击原好友展开/收起、城市打开/关闭、原唯一输入框唤起/关闭键盘、缓存地点卡片展开/收起；8个事件、95次前台/解锁/构建守卫、exit0、actionError=null。root实际查看首帧、城市、29秒IME和43秒地点卡片解码图，确认录下了这些原操作。

录屏前后均100%、浅色、density520，原地图在上述操作中持续可见；内部地图实例保留另由组件测试验证。没有延长公开预览许可。录屏不替代profile/release掉帧、地图闪烁或完整Beta验收，这些仍 **NOT_RUN**。前一次 `phone-recording05` 仅录到静态缓存回答，保留为部分证据，不写成操作全流程。

## 队列与源证据封存

本轮沿用 `BT-FIX-NOW-UI-001`，局部修复按 **PARTIAL** 封存，完整任务不记DONE。封存后的队列仍256项：183 DONE、47 TODO、13 BLOCKED、13 PARTIAL、0 IN_PROGRESS；73项未闭合/P0为28项。只释放本轮root租约，其他255个任务对象、旧证据与原历史保留。实际提交状态及队列SHA以 [检查点回执](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/final-checkpoint01/queue-receipt.json) 为准。

[最终源和证据封存](/D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/final-checkpoint01/seal.json) 绑定当前源码、最后测试、APK/API、15帧真机证据、录屏及本报告。运行中的API/USB日志不作为不可变文件封存，只记录明确截止时的健康、身份和OFF状态。

首次封存比对发现旧Go回归01之后，原生HTTP测试夹具增加了可选原wire导出；生产代码及原业务断言未变，后续 `page-wire01` 已覆盖该当前测试帧。仍补跑当前全帧 `page-regression02`，实际102叶PASS、exit0、全1451项API源帧前后一致。初次过宽源帧断言失败保存于 `final-checkpoint01/prepare-attempt01-failure.json`，发生在任何队列变更之前；没有覆盖旧记录或伪造构建一致。
