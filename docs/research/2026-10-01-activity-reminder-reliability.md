# BT-NTF-002 提醒可靠性验收（2026-10-01）

## 实现

独立 `cmd/activity-reminders` 可重复调用。查询在插入前锁定 Activity 和 Participation 行，与改期/取消事务串行化；同一参与者、活动和提醒类型由数据库唯一键去重。改期事务删除旧提醒，新时段重新生成；取消事务删除提醒并产生取消通知。API 自带五分钟轮询作为附加触发，独立 worker 可在 API 离线时工作。worker 失败记录 `activity_reminder_run status=failed stage=...` 并返回非零，成功记录新增数与耗时。没有部署调度器和告警的证据。

## 实测

- 本地 Compose 与合成活动：`automation/verify_activity_notifications.ps1` PASS，重复运行分别新增 1/0；改期后旧提醒消失、新提醒新增 1；场地更新、收件箱目标/已读、取消后提醒清除均 PASS，脚本清理活动和通知。
- 使用 `-OfflineGatePath` 模式创建并报名合成活动，然后停止 API。独立 worker 用故意错误的本地数据库端口运行：退出码 1，日志 `status=failed stage=database_ping`；恢复正确数据库后运行两次：`inserted=1`、`inserted=0`。重启 API 记录 `source=api inserted=0`，学生 Inbox 仍有恰好一个提醒。继续改期、取消检查均 PASS，合成记录已清理。
- `go test ./...`、`go vet ./...`、`go build ./...` 随本轮最终质量检查复核。

## 仍需部署验收

运营方须指定调度器按 5 分钟间隔执行 worker，采集退出码与日志，配置“失败或超过 10 分钟没有成功运行”的告警收件人与值守人，并在真实部署环境演练停机/重试。仓库内代码和本机模拟不能证明生产可靠性；活动已开始后不发送过期的“即将开始”提示。
