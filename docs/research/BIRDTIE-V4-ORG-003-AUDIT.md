# BT-V4-ORG-003 审计与交付

2026-10-03。来源 `BirdTie_V4_Execution_Package.zip!/BirdTie_TAPD_Backlog_V4.xlsx#Requirements`；唯一 live 队列保持原任务与历史。依赖 BIZ003／ORG001 已 DONE。需求是资料核验状态真实、官方链接和公开活动可检查、无暗示推荐背书。

## 初始差距与修复

组织已存在公开资料，但异步身份／组件变更与外链异常仍有缺口；商家 070 是受控管理资料，不能直接公开。新增 072 明确公众许可、原生 supplierprofile 与三种 UI 路径；复用 canonical organizer／Activity、原生鉴权与独立审核，私密资料不回填。

独立 Go 审查发现并修复：最后 Session UPDATE 等待期间撤权窗口；纳秒与 PostgreSQL 微秒导致重试冲突；最终资料／审核授权期限复核；过期 City 活动混入；canonical 组织 ID 兼容投影缺失。真实 PostgreSQL 锁等待、同请求纳秒重试、Asia/Tokyo 连接 UTC wire/source、资料修改失效和明确许可生命周期覆盖当前实现。

独立 UI 审查发现并修复 5 项：current widget 授权 getter、目标变化刷新、组织活动归属、迟到错误隔离、launcher 异常；6 probes PASS、4 产品源 SHA 前后不变。活动详情原 BUSINESS 主办方不开放回调，已登记精确 scope 后与现有组织主办方路由统一。未借该修复改 worker 的独占预约文件。

## 原始失败保留

- native1 使用无 pgcrypto 扩展的 digest 导致 3 失败，改 PostgreSQL 内建 sha256，不安装扩展。
- native3 canonical OrganizationID 指针类型编译失败，真实修复；native4 fixture 的公开枚举错误与 nil 营业数组错误，修正 fixture，不放宽权限。
- native 手机 Session 过期 401，以已有原生开发手机号流程重新验证相同合成本人；开发 City 角色错误 23514 改成既有 contributor，不赋予 Business 自动 City 审核权限。
- UTC API 修复前手机严格 DTO 拒绝 `+08:00` 审核日期。改 Go 输出 UTC，同时添加连接时区与日期测试，未放宽客户端日期解析。
- Flutter full6 22 lint → 只修根 lease 文件；full7 563 PASS/1 失败因成功消息在新增按钮后需要滚动，保留原写入／成功断言并调整滚动。target1 大字按钮、异步旧测试和忙碌撤回 Dialog 等待分别修正真实测试顺序。
- future Activity helper 收到 native create 201 后错误期望 200；恢复仅查确切既有草稿 ID，再 publish 200，不重复创建。
- 手机是双显示设备，默认 input 发到副屏；验证当前 display 后固定输入 `-d 0`。一次多按 Back 离开 App 后 query helper 拒绝非 Birdtie 前台；重新打开实际 App 后用新标签重试，没有盲点操作。

## 最终可复现证据

- `work/v4-org003/verify-native.py --round native6`：实际原生定向 19 PASS/0 FAIL/SKIP，vet/build 0，完整 public rows/catalog 同值，自有 DB DROP。
- 同脚本 `--round whole-go072-2 --full`：fresh 001–072、原 3 dev seed 与 retained 旧数据，Go 8506 PASS/0 FAIL/SKIP/packageFail，vet/build 0，640 源未变，自有 DB DROP。
- `schema1`：非空 down exit3 且完整 rows/catalog 原子同值；空 down 与 reapply 恢复 071/072。072 双 DDL SHA 与当前源相同；该帧旧功能源不称当前 Go 回归。
- `work/v4-biz003/full-client9`：真实 analyze/test/Debug APK build 0，564 功能与 80 loading PASS/0 FAIL/SKIP，189 源稳定。APK9 真机 ADB 安装 Success。BIZ004 独立 60 PASS，共享全量已覆盖；worker-final2/3 与源由根独立校验。
- 本地 native phone DB 001–072，当前 runtime72 API3 UTC；公开许可 0→1、审核 0→1，撤回取消不变、确认 1→2 与审计 1→2；匿名公开正文清空介绍／链接，但 canonical 公开活动保留。
- 实际手机 owner 公开预览、取消／确认、PublicBusiness、未来活动卡→稳定详情、地点→预约说明→取消截图；独立 finish review 已看图无新裁切问题。APK8 的 owner 发布帧与 APK9 同 owner 文件 SHA，仅后续活动入口二文件有变化；不将 APK8 称 APK9。

## 尚未执行

TalkBack、横屏、真实目标用户无提示观察、外部官方／预约网站、真实生产登录／经营授权、正式 API／提醒部署与原真实 A→H 均未验。race 环境缺 CGO/GCC。相关运行与外部发布条件继续保留，Closed Pilot Ready=NO、Consumer Beta=NO。

详见 [canonical 公开协议](../architecture/public-supplier-profile-v4.md) 与不可变 `docs/testing/evidence/public-supplier-profile-2026-10-03/root-final1/manifest.json`，原负向帧不覆盖或删除。
