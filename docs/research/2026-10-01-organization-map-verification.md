# BT-MAP-004 组织地图点位验收（2026-10-01）

## 数据与权限

- Migration `029_organization_map_locations.sql` 建立组织自己提交的独立 WGS84 点位、公开意向、审核状态、版本和审计表。没有从成员、个人位置或活动场地导入坐标。
- 组织 owner/admin 可提交或隐藏；每次修改都撤销原审核结果。独立、有效的城市 reviewer 才能审核，同一提交人不可自审。
- 公开接口 `GET /v1/cities/{cityID}/organizations/map` 只返回 `location.visibility=public`、`review_status=approved`、组织 `status=active`、`visibility=public`、`verification_status=verified` 的 ID、名称和精确坐标。私有管理接口要求 owner/admin。无坐标时返回空数组。
- Flutter 只消费此公开接口，使用 `organization:<UUID>` 稳定 ID。点选 Pin 显示轻量卡片，查看按钮打开组织公开资料。组织管理 UI 可以提交/隐藏点位；审核由现有城市 reviewer 身份通过受保护 API 操作。

## 实际执行

- 本地 Compose 数据库：029 forward 成功；独立 `birdtie_mig028_verify` 数据库：029 forward/down/forward 均成功。
- `C:\Users\chens\.cache\codex-runtimes\codex-primary-runtime\dependencies\python\python.exe automation/verify_organization_map.py`：PASS。实际 HTTP 检查：匿名管理 401、非成员读取 404/写入 403、非法坐标 400、未提交/待审/未核验组织/私密组织/改点待复审/拒绝/隐藏均无公开坐标；独立 reviewer 审核 204；审核公开后精确点仅出现一次；审计记录 `submit,approve,submit,reject,submit,approve,hide`。
- `flutter analyze` 0 issue；`flutter test` 最终 78 项全通过（含新增的 2 项点位解析和稳定 ID 点击测试）；`go test ./...`、`go vet ./...`、`go build ./...` 通过。
- 真机 `c641566b`：debug APK 构建并 `adb install -r` 成功。仅在本地开发数据库临时建立名称明确标记“非真实组织”的合成点位，完成提交/审核路径并临时置为 verified，以验证 Android 地图。集群列表显示该组织，选择后出现轻量卡片，点“查看”进入组织资料。截图：[列表](evidence/2026-10-01-organization-map-list-device.png)、[卡片](evidence/2026-10-01-organization-map-selected-device.png)、[资料](evidence/2026-10-01-organization-map-detail-device.png)。集群初次打开暴露长列表溢出，已改为可滚动列表并在重装后的真机截图确认消失。
- 真机验证后已将合成组织 `011bd983-3960-4bd9-ace6-3797029d604f` 恢复为 `unverified/private`，并停止应用。截图中的“已认证组织”只对应测试时的临时本地数据库状态，不是现实组织身份核验或试点证据。

## 运营边界

没有真实组织授权坐标、独立审核人员值守或部署环境证据；本任务仅证明仓库实现与本地/真机开发验收。生产地图不能使用上述合成位置。
