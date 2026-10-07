# 媒体上传边界与低成本首期部署

日期：2026-10-08（Asia/Shanghai）  
状态：Birdtie 部署预案；Birdtie 尚无媒体上传路由或存储实现。

## 已有能力与差距

- Birdtie API 为 Go 1.26、pgx/PostgreSQL；本地 Compose 为 PostgreSQL 16，volume `birdtie_pgdata`，只绑定 `127.0.0.1:55432`。健康端点：`GET /healthz`；DB ready：`GET /readyz`。当前 `main.go` 默认 API bind `127.0.0.1:8080`，未设 API 反向代理/生产部署编排。
- Birdtie `media_assets`、`private_media_metadata`、`media_variants` 只定义数据边界；媒体上传未实现，Flutter app 也没有媒体上传界面。因此本次未改 Now 输入交互，英国视频上传还不能在 Birdtie 端验收。
- Civu REQ-488 已有服务端 OSS 短时 PUT 签名（30 分钟）、对象完成时核对大小/SHA-256、CDN 播放跳转；客户端失败时刷新签名重试两次。不开 OSS 时退回 API 5MiB 分片会话。相关 Go/Flutter 单测已在 REQ-488 记录通过；英国网络实测缺失，需求保持 `in_progress`。代码位置：只读参考 `D:\Program\Civu\Civu-server\api\internal\httpapi\media_resumable_handlers.go`、`media_handlers.go`、`internal\ossmedia\` 与 `D:\Program\Civu\Civu-client\lib\src\...`。
- Civu 配置名：`MEDIA_DIR`；`OSS_BUCKET`、`OSS_ACCESS_KEY_ID`、`OSS_ACCESS_KEY_SECRET`、`OSS_REGION_ENDPOINT`、`OSS_UPLOAD_ENDPOINT`、`OSS_CDN_BASE_URL`。当前是否启用、bucket 区域、CDN 回源、跨域 CORS 和安全组均未远程验证。
- Civu direct upload 虽不把上传主体经过 API ingress，但 complete 目前会从 OSS 下载完整对象到 API 临时媒体目录做检查/派生处理。因此需分别测客户端英国→OSS 上传和服务端 OSS→API 下载/处理时延。上传域名与播放 CDN 独立配置；不能用 API 扩 CPU 推断跨境上传可修复。

## 媒体链路的实现门

Birdtie 下一阶段需独立实现：

1. Flutter 先请求 Birdtie API 创建 owner-bound upload intent；API 检查会话、权限、内容类型、对象大小、目标可见性，生成随机不可枚举 object key，签发最短有效期的单对象/单操作授权。Flutter 不包含长期云密钥。
2. 视频按对象存储 multipart API 分片直传，上传 ID/已确认分片 ETag/校验值持久化；客户端可暂停/取消、有限重试、网络变化后刷新授权并续传。平台实际可支持的后台/进程重启能力按 Web File/移动端 URI 权限核验；不承诺 iOS/Android/Web 进程关闭后都可续传。
3. 完成接口幂等；Go 对存储端做 HEAD/list-parts 校验对象 key、版本、owner session、字节数、provider checksum 与文件签名/MIME。不得相信客户端 `complete=true`。过期上传清理由可重试 worker 回收 multipart session。
4. 状态为 `uploading → uploaded → processing → playable/failed`；Now 保持原单一输入入口，媒体状态可内联显示，不建立复杂新表单。
5. 上传完毕发布事件；异步隔离扫描、图片方向/EXIF 清除、视频 probe、720p/1080p 转码和封面生成。不可覆盖原片；派生失败可重试且幂等；转码放独立 worker/按需作业，不占 Go API CPU/RAM。
6. API 与 PostgreSQL 同一区域；首期对象存储靠近英国用户且与 API 同区域（欧洲首选，前提是 OSS/COS 与模型、TokenHub、WSA、地图和中国访问可用性/合规实测）。中国用户地图 API 走已选区域 provider；中国客户端访问欧区 API/OSS 必须真实网络测得后接受或改香港单区域。暂不跨区域数据库双写。

必须覆盖 10/50/100 MB 样本，记录 codec、容器、像素分辨率、fps、视频/音频 bitrate；断网、Wi-Fi/蜂窝切换、短签名过期、重复 complete、越权用户、伪造 complete、取消、进程重启；每个文件分别记录英中网络地点、ASN/接入、上传成功率、P50/P95 耗时与有效吞吐。没有英国设备/网络时状态是“英国网络待验证”，本地主机试验不能替代。

## 部署建议（单主区域）

- API：2 vCPU/4 GiB 是初始可试规格；Go API + 少量轻量异步控制任务。生产日志到 stdout/stderr 并轮转，配置 HTTPS 反代和上游仅 loopback/private listener。API 层健康探针 `/healthz`，readiness `/readyz`。
- PostgreSQL：独立托管实例或独立 2 vCPU/4–8 GiB 实例，与 API 同区；不要与 API 在 4GiB 同机共用，除非基准显示低内存足够且可接受故障域。Birdtie PG16 migration 需先隔离 apply、逻辑备份、restore 演练。
- 媒体：独立对象存储 bucket + 独立 CDN 域名；私有原件、quarantine、公开派生使用独立前缀/权限策略。Bucket 不设公开写。转码 worker 与 API 分开限额；早期可按作业启动短期 worker。
- TLS/Nginx 示例在 `deploy/nginx/birdtie.conf.example`；Compose 只是开发数据库，不是生产部署配置。TLS 证书自动续期、秘密放主机受限文件/秘密管理服务；仓库仅有 `.env.example` 无值模板。
- 发布顺序：备份与校验 → 隔离库逐个 migration → API 新容器版本 → `/readyz` → 只读 smoke → 灰度 → 监控 → 观察期。失败回滚到上一个 API 镜像；数据库 migration 只向前兼容，不对生产盲跑 down。恢复需先决定 DB 与对象版本点的一致性。
- 监控：API 5xx/延迟、Postgres connections/CPU/storage/backups、对象存储 4xx/5xx/请求时延/费用、CDN cache miss/回源、转码队列年龄和失败率、证书到期、备份副本校验。为每项指定值守人/告警目标后才能算试点运维就绪。

### 环境变量模板（名称，不含真实值）

Birdtie 当前：`BIRDTIE_DATABASE_URL`（secret）、`BIRDTIE_API_ADDR`、`BIRDTIE_ALLOWED_ORIGINS`、OIDC issuer/client/redirect/secret（正式认证上线前必须注入 secret store）；`BIRDTIE_DEV_PHONE_AUTH` 生产必须关闭。媒体待设计新增：`BIRDTIE_MEDIA_BUCKET`、`BIRDTIE_MEDIA_REGION_ENDPOINT`、`BIRDTIE_MEDIA_UPLOAD_ENDPOINT`、`BIRDTIE_MEDIA_CDN_BASE_URL`、受限对象签名身份、multipart part size/expiry、临时对象 lifecycle、媒体 worker queue 与转码 profile。密钥不得提交仓库。

## 月成本模型与报价限制

日期：2026-10-08；币种按供应商显示；用量假设仅用于报价输入：2 vCPU/4 GiB API 730 小时/月；独立 PG16 2 vCPU/4–8 GiB 730 小时/月；媒体 100 GiB 标准存储、每月 1 TiB 英国用户上传、1 TiB 播放下行、1 TiB 中国方向下行、每月 100 小时 1080p 转码、DB/媒体备份 100 GiB。模型/搜索不计入基础设施小计。

| 成本项 | 欧洲主区 | 香港主区 | 计费输入/状态 |
|---|---|---|---|
| ECS/API 2vCPU/4GiB | 控制台实时报价待查 | 控制台实时报价待查 | 按量 730 小时，不使用新购促销折扣 |
| 托管 PostgreSQL 2vCPU/4–8GiB | 实时报价待查 | 实时报价待查 | 单节点/备份保留天数另报价 |
| OSS/COS 100GiB | 实时报价待查 | 实时报价待查 | 标准存储，含请求费需估 |
| 公网/回源 2TiB 英国 + 1TiB 中国 | 实时报价待查 | 实时报价待查 | 按实际出网区域/阶梯价核算；CDN 下行单列 |
| CDN 下行 2TiB | 实时报价待查 | 实时报价待查 | 英国与中国流量分别计费 |
| OSS 上传加速 1TiB | 实时报价待查 | 实时报价待查 | 上传加速与下载/CDN分开，不默认启用 |
| 转码 100h 1080p | 实时报价待查 | 实时报价待查 | 独立 worker/视频处理服务报价 |
| 备份 100GiB | 实时报价待查 | 实时报价待查 | ECS 快照 + DB 备份 + 媒体副本分别核价 |
| 模型、TokenHub、WSA、地图/搜索 | 单独询价/测量 | 单独询价/测量 | 不计基础设施小计 |

截至 2026-10-08 的官方规则：OSS 按储存、请求、互联网出站、CDN 回源及可选加速分别计费；使用传输加速 endpoint 会产生加速费，下载还产生公网出站费。CDN 按客户端 IP 所在区域计费，英国在 Europe（EU），香港在 Asia Pacific 1（AP1）。见 [OSS traffic fees](https://www.alibabacloud.com/help/en/oss/traffic-fees)、[OSS transfer acceleration fees](https://www.alibabacloud.com/help/en/oss/transfer-acceleration-fees)、[CDN billing regions](https://www.alibabacloud.com/help/en/cdn/product-overview/billing-overview)、[OSS 官方价页](https://www.alibabacloud.com/en/product/oss/pricing)。这些规则不是完整价格；ECS/RDS、OSS 标准存储、请求、CDN EU/CN 下行、转码与快照仍需控制台按真实地域/规格报价。本机没有云控制台/账单访问，因此不虚构单价/月合计、不推荐尚未可买的区域。执行价格比较时保存 2026-10-08 控制台正式长期价截图/导出（含币种、税、区域、期限、带宽），排除首购优惠。

扩容触发建议：API CPU P95 >60%持续15分钟或请求P95超SLO；DB内存>75%/连接>70%/存储>70%；上传成功率低于98%或英国P95超过目标；转码队列P95年龄>5分钟。阈值需试点观测后定稿。

## 当前验收状态

- Birdtie 视频上传代码：**未实现**（已有媒体表/存储契约，无接口/Flutter流程）。
- Civu REQ-488 代码：按只读需求文件已有实现及单测历史；当前生产是否配置 OSS、是否投产未验证。
- 英国/中国媒体网络测试：**未实测**；英国网络待验证。
- 部署配置模板：文档已给出，Nginx/Compose 可部署文件仍待实作和 review。
- 供应商网络/模型/TokenHub/WSA/地图可达性、真实月费、并发容量：**未验证**。
