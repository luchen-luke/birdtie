# Functional MVP 开发 seed 验收（BT-DAT-002）

日期：2026-10-01。隔离数据库：`birdtie_audit_20261001_full`；本机 Go API：`127.0.0.1:3695`；Android 真机：`c641566b`。全部实体均为虚构本地开发资料，不代表真实 CSSA 合作或正式供给。

## 审计与修补

按文件名顺序在空库执行 27 个正向迁移，全部成功。原 `001_badminton.sql` 可重复执行，但仅有 2 地点、1 组织、2 活动、0 用户和 0 成员关系，不满足新要求。新增 `002_functional_mvp.sql`，在真实 PostgreSQL 中补入另 1 个地点、1 个明确标为“虚构本地测试，非官方”且 `unverified` 的 CSSA 样例组织、3 个未来活动、2 个测试 Person、2 个 Personal Agent、1 个 Organization Agent、3 条成员关系。虚构开发号码通过本地开发认证的哈希身份映射到这两个用户，不授予生产手机验证状态。SQL 连续执行两次成功。

完成后隔离库计数：CityContext **1**、公开 Place **3**、Organization **2**、未取消的未来公开 Activity **5**、Person **2**、active Membership **3**、Personal Agent **2**、Organization Agent **2**、verified Organization **0**。

## API 与真机

隔离 API 实测 `GET /v1/cities` 返回 1 城市，`/v1/cities/aberdeen-gb/places` 返回 3 地点，活动列表返回 5 活动；公开 CSSA 样例资料名称带“虚构本地测试，非官方”，认证状态为 `unverified`。`+999000000071` 通过仅在 loopback 启用的开发认证登录后，`/v1/me/organizations` 返回该账号可管理的组织；Agent 查询周末羽毛球返回数据库活动。验证过程未输出 Session token。

将真机临时 ADB reverse 从客户端端口 `3694` 指向隔离 API `3695`，重新打开应用后，Now 原生 Aberdeen 地图显示区域 **5**、羽毛球 **3**、社交 **2** 和聚合 Pin。截图：[真机数据库 seed 结果](evidence/2026-10-01-functional-seed-device-final.png)。此时发现新 `social` 代码直出英文，已在 Area Pulse 中文标签映射中修正，并补 Widget 测试。验收后 ADB reverse 已恢复为 `3694→3694`，指向原有本机开发 API；隔离 API 已停止。

另在同一隔离库运行 `automation/verify_vertical_slice.ps1 -ApiBase http://127.0.0.1:3695`，使用两名已 seed 的虚构测试用户：组织管理员工作区、草稿发布、学生公开发现和 Agent、详情、报名/重复保护/重读、我的活动、收藏独立性均输出 PASS。脚本最后取消生成的合成活动。该 API 流程不包含真机重启，重启证据仍引用此前单独的本地真机闭环记录。

## 工程检查

| 检查 | 结果 |
| --- | --- |
| `flutter analyze` | 通过，0 问题 |
| `flutter test` | 67 项通过，含新增中文分类测试 |
| Flutter Android debug APK | 首次失败于系统 Gradle 9.1 缓存完整性；改用仓库内隔离 Gradle 缓存后构建通过并覆盖安装真机 |
| `go test ./...` | 通过 |
| `go vet ./...` | 通过 |
| `go build ./...` | 通过 |

本验收证明开发 seed 的 PostgreSQL→API→Flutter 取数路径，不证明生产身份、真实活动、已核实 CSSA、正式 Mapbox 权限、试点运营或发布门槛。
