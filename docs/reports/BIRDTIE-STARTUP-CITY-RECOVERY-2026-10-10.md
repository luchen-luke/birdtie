# Birdtie 手机启动城市读取修复

日期：2026-10-10，Asia/Shanghai。授权范围：修复用户打开手机 Birdtie 时出现“无法读取城市，请稍后重试。”与“重新读取城市”的问题。正式仓库为 `D:\Project\birdtie`；原有未提交改动与任务队列保留。

## 原因与现场恢复

手机安装的是本地调试版，API 地址编译为 `http://127.0.0.1:18091`，依靠明确手机 `c641566b` 的 ADB reverse 转发到电脑 `127.0.0.1:18109`。最初检查时，手机在线、转发仍存在、Birdtie PostgreSQL 容器健康，但 API34 已无进程及监听。手机 GET `/v1/cities` 返回 `curl: (52) Empty reply from server`，真实应用界面显示用户报告的错误及“未选城市”。

因此本次直接原因是本地后端未运行，城市目录读取失败，无法建立探索城市。原 API 最初为何退出，现有证据不能确定。未把它归因于城市数据损坏、用户未选城或登录失败。

复用原 API34 可执行文件及现有 trial 数据库恢复服务后，手机城市读取为 200；原 APK 点击重新读取即恢复 Aberdeen 地图。没有重新编译后端、运行迁移、重置数据库或修改服务授权。

## 本轮变更

- `apps/client/lib/src/city/city_catalog_recovery.dart` 与 `birdtie_app.dart`：首次读取失败且目录为空时，在前台按 2、5、10 秒最多重试三次；后台取消等待，返回前台立即重读失败目录；成功后停止，保留已选城市、手动重试及正常空目录。首次缺少 API 配置仍由原 controller 显示明确配置错误。仅重试公开读取。
- `apps/client/tool/local_preview_runtime.py`：隐藏常驻监督器，单例运行，修复指定手机丢失的转发，自动恢复自己启动的 API；复用确切已有 API，保护未知端口占用与冲突映射。配置路径和内容 fingerprint 不匹配时明确失败。Stop 只终止自己持有的 API 进程句柄，保留 adopted 服务；状态不打印密钥或控制 token。
- `apps/client/tool/start_local_preview.ps1`：提供 Start、Status、Stop。仅外层监督器请求 Windows Job breakaway；受其管理的 API 使用普通隐藏子进程。实测发现重复请求 breakaway 会导致 `PREVIEW_DURABLE_PROCESS_UNAVAILABLE`，已修正并保留失败证据。
- `apps/client/tool/run_device_debug.ps1`：默认读取本机已有 preview 配置并恢复运行器；分别使用手机端口与主机端口、统一配置的 ADB 驱动；有界等待后端就绪，并检查主机与手机城市接口。既有正确转发保持幂等，冲突转发保留，无转发时使用 `--no-rebind`。
- 本机 ignored 配置 `apps/client/.env.preview.local.json` 指向原 API34、原数据库与明确手机。配置不包含密钥。

## 验证结果

| 范围 | 实际结果 |
| --- | --- |
| 新客户端恢复测试 | 13 PASS；三文件定向 Dart analyze 无问题 |
| 本地运行器测试 | 最终 26 PASS，API/ADB/docker 操作使用 mock，Windows单例锁在临时目录真实验证 |
| 旧城市探索测试 | 17 PASS |
| 旧 Now 城市恢复测试 | 3 PASS、5 FAIL；旧文案或控件 finder 与现有 V2 界面不符，未改写失败断言；测试直接构造 MapWorkspace，不进入本轮 shell/helper。没有还原整个工作区另跑历史基线，因此不称全部测试通过 |
| PowerShell 与有限 diff 检查 | 通过 |
| 装包检查点 | 首次新包成功；review 修正缺配置回归后最终 build02 成功、源快照未变，`adb install -r` 成功。最终 APK SHA256：`04ec457ed559c544edd638b4bb8dec52530bed6a4bb24461de40271530536449` |
| 真机正常启动与前台恢复 | 手机实际截图与 accessibility tree 确认 Aberdeen 城市、地图显示，错误及重试按钮消失。最终包还真实验证前台启动遇到转发缺失，随后自动恢复；同一 app PID，全程零手动重试、零切后台、零失败后重启，约 15.656 秒完成截图确认 |
| 转发丢失恢复 | 实际移除本轮指定映射后，监督器自动恢复；最终检查约 6.469 秒 |
| 冲突映射保护 | 调试入口明确失败并保留注入的测试映射；检查结束恢复原映射 |
| API 退出恢复 | 实际终止本轮管理的确切 API 进程后，最终监督器自动启动同一二进制，手机城市接口通过；从退出到确认约 21.359 秒。前两次运行器实现失败保留，不沿用失败版本作为通过 |

客户端证据：`docs/testing/evidence/startup-city-recovery-2026-10-10/client/README.md`。运行器测试：同目录 `runtime/verification.json`。现场结果：`runtime-root-checks.json`、`foreground-root-checks.json`；最终前台恢复配对截图：`final-foreground-missing-tunnel.png`、`final-foreground-auto-recovered.png`，对应 JSON 记录最终安装包 SHA256 和同一 app PID。历史真实故障失败：`runtime-root-checks-failed-43628.json` 与 `work/local-preview-runtime/events.jsonl`。最终构建：`work/startup-city-recovery-2026-10-10/client-build-02/receipt.json`。

适用 UX-CHECK-09（安全重试）、10（异步隔离）、12（前后台恢复）。本轮未运行全仓测试、迁移、正式发布或供应商测试请求；截图不代表其它产品流程已验收。

## 本机启动方式与使用边界

从 `D:\Project\birdtie` 启动或核验本地服务：

```powershell
& .\apps\client\tool\start_local_preview.ps1 -Action Start
& .\apps\client\tool\start_local_preview.ps1 -Action Status
& .\apps\client\tool\run_device_debug.ps1 -CheckOnly
```

本地配置与原 API/数据库文件必须仍在；启动需要原受保护环境中的服务凭据。停止命令为 `-Action Stop`。本轮没有注册系统登录自启；电脑重启后需要运行 Start，调试入口也会调用它。

此修复提高本地调试版的可靠性，不能让手机在脱离电脑后访问电脑回环地址。当前仍需要电脑、数据库与手机调试连接；短时断开会恢复，电脑关机或未连接时仍无法取得在线数据。日常手机独立使用需要一个持续在线、手机可访问的后端以及对应安装包配置。本轮没有开放本地开发认证服务到公网。
