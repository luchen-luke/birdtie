> 2026-10-16 20:46后续：用户再次报告空图，现已确认原账号30分钟idle超时、旧客户端没有持久接续。此前32秒后台验收不能覆盖此路径。新修复与当前版本以 [会话接续修复记录](../reports/BIRDTIE-SESSION-RECOVERY-2026-10-16.md) 为准；以下保留APK40批次历史。

# DGS 当前交付基线

2026-10-16：APK40已替换安装；API31/schema175已部署，主进程SHA已验证。新Key、原Civu样式、真实上海/巴黎搜索及原Civu图片已进行限定真机检查。手机连接阻塞已解除，双人多设备依赖仍BLOCKED。

精确版本/哈希和每条测试边界见 [执行记录](../reports/BIRDTIE-DGS-EXECUTION-2026-10-16.md) 与 `docs/testing/evidence/dgs-20261016/android40-device/verification.json`。

---

## 启动及中间快照（保留历史，不代表当前版本）

# DGS 2026-10-16 实际基线

执行主体为原队列 `BT-FIX-NOW-UI-001`；DGS-01 至 15 是其增量检查点，不替换历史任务、依赖或发布门槛。原任务仍 IN_PROGRESS。用户已恢复开发并授权限定真实数据迁入/测试数据清理；Civu 源码与旧服务保持只读。网页延期，正常字号，不录屏。

## 版本与现场

| 对象 | 本次核对结果 |
| --- | --- |
| Birdtie 本地与已推送基线 | `codex/birdtie-import` / `cef1a11ada611247021c4e81593c50437f336a66`，本轮继续叠加未提交修改，未 reset/clean |
| 附件阅读基线 | `4637cfb6e7758fc4fa5dd548fad623a8484c4cfb` 是当前祖先；不回退 |
| 实际 API | `/opt/birdtie/releases/social173-20261016-build30`，binary SHA256 `4cc70262b66cffa0061a79fa91fdc7f622de23aedd5552a713b5e1070a7b9900`，active/running |
| 独立数据库 | BIRDTIE_NATIVE_HZ / OID 54087，migration max/count 173；174 未上线 |
| 手机已安装 | APK36r2，SHA256 `a91fe1c8fb491c063b9375e2488846626e40f826ffce73bc908895315ff49a5f` |
| 新 Android 候选 | APK38，SHA256 `c504e54211d75b7e18efa5675210b8d28d49d281942698aca10cc53e41710e3b`；包含新 Key、CIVU样式、DGS地图稳定性与全球目的地；尚未安装。随后通知/分享修复不属于38 |
| Web | 仍为 Web34，不在本轮实施/部署范围 |
| 真机外部依赖 | ADB 当前无设备，安装与新 Key/自定义样式验收 BLOCKED；未运行检查为 NOT_RUN |
| 旧服务 | 只读观察 `civu-api.service` failed/failed；未启动、停止或修改它 |

上述远端核对为 2026-10-16T08:43:04Z 的只读事务，不是新的发布。备份 source SHA256 `ba6b9b5d2d404ada806a858cbc0931628a5958c4b2abdbd8b7d50b62b4e6f40d` 与 manifest `78c390f8b3f714981b7e98f6e938065bd65de60a81dec226e0a77318627241f1` 已校验绑定，权限0600。备份含20用户、103旧动态、407媒体等29表；这不是当前迁入行数，也不是新清理批次的可恢复备份。

## 已复现的地图失败路径

1. **OwnedMap 单资源故障扩大为全图撤权。** 两个正常点中一个封面返回404/not_found或409/moment_changed，无关点也被退休。修复前2 FAIL；修复后该资源失效、旧像素/详情授权退休，其他点保留，按身份每资源只做一次重新鉴权。401、人控代次变化、无可靠局部合同的403/410和未知拒绝仍全局失效。
2. **组织切换的 typed 图层仅退休、不重新加载。** 使用真实 MapWorkspace、Sidebar 控制器、解码器和隔离 HTTP 的回归：个人→组织→个人后公开层为空，未拖动相机就不恢复。修复前该项 FAIL，而同批 token刷新/账号切换2项PASS。修复后统一安排到监听结束后读取当前身份与视口，3项PASS。没有将推测的登录竞态写成已复现事实。
3. **逐条解码缺少隔离。** 两种地图解码器现在丢弃完整坏条目并计数；全批坏数据显式报错；全局身份、范围、期限等无效时仍拒绝整批。短暂503/网络失败保留原有效期内的内容，重试有界，不延长授权。

详见 `docs/testing/evidence/dgs-20261016/map-stability/REPORT.md` 与 `identity-host/before-fix-settled.json`、`after-fix.json`。这些证明代码路径缺陷及修复，不证明用户现场只有这一个原因；真机复现、30次冷启动和性能对照 NOT_RUN。

## 真实数据审计边界

只读审计显示，本地3份 dev-seed 的精确 UUID 和 dev-seed:// 来源在线上均为0，不能指称它们已污染线上。当前110条动态中83有Civu moment映射；8条与既有脚本执行回执精确匹配，均已 withdrawn/private；18条虽属脚本账号但逐条证据不足，另1条其他来源，暂不删除。此为删除前审计快照。随后限定清理已执行：8动态、1会话、3消息、2请求及精确依赖共22表103行；全量备份恢复验证及隔离重演通过，生产二次执行ALREADY_CLEAN。保留83条Civu动态、19条未知动态、39条旧消息与29账号；14类保护摘要不变，外键与12个命名保护触发器恢复，增加8条内容失效事件。原Civu零写。详见清理报告与 data-cleanup/receipt.json。

固定验证码 `BIRDTIE_DEV_PHONE_AUTH=true` 是用户明确要求的身份接续配置，不因合成清理关闭。`BACKGROUND_MAINTENANCE=false` 不能单独证明全部注入入口已禁用。现阶段不执行任何真实环境合成创建脚本；清理须先具备该批精确行、依赖闭包、备份恢复验证和保护对象摘要。

## 未完成

全球目的地服务/Now选择器/无内部city视口链路已接线，26项provider/controller/map测试及7项原生桥/Now真实组件/身份测试通过；巴黎、Paris实际Mapbox调用均200。Android原生高德搜索使用已包含SDK及现Manifest Key，真实鉴权和手机选择定位NOT_RUN。社交差异矩阵、默认拒绝合成注入、通知未知类型和无标题分享适配继续实施，DGS15整体验收未完成。已有类、页面、迁移计数和编译均不作为完成证据。
