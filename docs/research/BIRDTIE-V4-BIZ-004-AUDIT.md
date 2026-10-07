# BT-V4-BIZ-004 审计

2026-10-03。原要求：商家可提供预约URL/联系方式、可选availability与来源/时效。070 BookingURL owner/admin写入及独立审核已实现；无明确public permission，管理note/rights/reviewer/links均不从本功能公开。040公共Venue已有预约URL、sourceUrl/reviewedAt/expiresAt及当前经营关系，Place详情已打开外部预约说明，但未复读当前资料或解释期限。

本轮复用原040 GET /places/{id}/venue，实施具体版本外跳确认与时效显示；不称native reservation、实时可订空位、真实联系资料或外站可用性。无新数据库/HTTP/公共Business profile。独占实现/测试见work/v4-biz004/plan.md，原规则 UX-CHECK-05/06/07/08/09/10/11/12/14适用；真实mobile截图、TalkBack、当前App重启未验。

根ORG003唯一拥有公共Business profile/批准；本任务仅在PlaceDetail提供可选onOpenBusiness callback，同一业务ID由根路由消费。

## 实现与最终本地验证

新增 VerifiedBookingController / VerifiedBookingSection，原 PlaceDetail 预约 URL 直跳已替换为当前公开 Venue 读取→材料预览→用户批准→再次读取→匹配后外跳。资料变化、404、自然期限、账号/workspace/epoch ABA、迟到、取消和设备打开失败均不描述预约成功。错误提供“刷新预约资料”，新资料仍要确认；来源和时效中文展示，实时 availability、未提供联系方法保持未知。商家资料按钮可选 callback+严格非零UUID，同原 ID 回调，经营关系动作改 Wrap。

最终 `work/v4-biz004/verify.py` 实际执行 analyze 6 项 exit 0、3 个文件 tests 60 PASS/0 FAIL exit 0；6 文件 before/after SHA 相同。首次58帧不可变档案保留 worker-final1；随后修复预览后控制器销毁取消通知及 launcher 等待过程身份变化成功回执，并新增2负例，另存 worker-final2，不改旧档案。此前57用例帧只在对话运行输出，没有独立持久档案；不复用被覆盖日志声称旧帧完整。

070管理端 URL/source/validUntil 继续私有；040公共预约独立审核。没有联系方式的条目如实显示未提供，可选空位元数据未增加，也未宣称实时空位。070→040自动发布桥不属于本次实现，若产品要求以具体商家提供资料直接匿名外跳，须由公共批准领域证明发布许可后再接，不能绕过边界。

历史可检查来源：`docs/testing/evidence/business-claim-console-2026-10-03/root-final1/manifest.json`（BIZ003原生36/UI34/wholeGo8487/wholeFlutter497，历史帧，不是本轮58）；`work/v4-biz003/native-venue-green1` 与 `native-venue-lifecycle1`（040旧原生回归，本轮不重述为新增）。未运行当前原生、正式外站、真机截图/TalkBack、生产IdP或真实Pilot。根将独立验证档案，并决定仓库完成状态；worker未改 live queue。
