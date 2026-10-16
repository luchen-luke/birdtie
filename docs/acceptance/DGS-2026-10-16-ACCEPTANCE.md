# DGS 当前验收 · APK40 / API31 / schema175

| 检查点 | 状态 | 证据及剩余边界 |
| --- | --- | --- |
| DGS-01 | DONE | 本地及GitHub起点cef1a11、当前APK40/API31/schema175逐项绑定；原附件旧基线不回退。 |
| DGS-02 | DONE | 单封面故障全清、组织切换不恢复已确定性复现；逐项解码与链路计数留证。现场未登录是另一个已见原因，不声称唯一根因。 |
| DGS-03 | DONE | 账号、token、组织监听后统一恢复3项通过；旧响应仍隔离；真实登录与40会话恢复已执行。 |
| DGS-04 | PARTIAL | 网络/资源/过期边界直接测试通过；真实后台超过30秒恢复、阅读后地图常驻通过；手机断网矩阵及30次循环NOT_RUN。 |
| DGS-05 | PARTIAL | v2无POI坐标点、空标题解码及无标题分享直接测试通过；本批所有发布/读取入口全链路NOT_RUN。 |
| DGS-06 | DONE | 来源清单、3 seed双重环境守卫、7旧注入脚本封停；14实际阻断尝试、隔离PG验证通过，真人固定码保留。 |
| DGS-07 | PARTIAL | 备份实际恢复、22表103行精确清理、幂等重跑及保护摘要通过；6测试账号隔离；手机长期旧缓存/搜索全入口矩阵NOT_RUN。 |
| DGS-08 | PARTIAL | 部署后83 Civu动态/39旧消息及稳定映射保护；真实owner登录和原图片读取通过；全部媒体与关系逐项真人验收NOT_RUN。 |
| DGS-09 | PARTIAL | 中英文Mapbox真实调用200、手机巴黎选择及原生高德上海成功；provider失败仅直接测试，真机外部故障NOT_RUN。 |
| DGS-10 | PARTIAL | 真实Now搜索/国家消歧/巴黎定位与上海城市尺度通过；全球语言和错误态真机矩阵NOT_RUN。 |
| DGS-11 | PARTIAL | 无city目的地状态和bounds-only查询直接测试、巴黎手机定位通过；跨日期变更线真实设备NOT_RUN。 |
| DGS-12 | PARTIAL | 好友/关注/屏蔽差异矩阵、新关注通知及当前权限适配；隔离双账号PG通过，真实双人设备依赖BLOCKED、E2E NOT_RUN。 |
| DGS-13 | PARTIAL | 原消息/历史通知留存、禁止历史Push、未知单项降级、原生新关注通知部署；双设备未读/后台Push NOT_RUN；逐条撤回writer仍缺。 |
| DGS-14 | PARTIAL | 无标题分享、当前目标resolver、迟到身份拒绝通过；真实地图→图片已执行；全入口同内容真人E2E NOT_RUN。 |
| DGS-15 | PARTIAL | APK40已装/API31已部署，地图/搜索/图片/前后台和性能抽样完成；其余明确列NOT_RUN/BLOCKED，整体不标完成。 |

完整实际证据、性能限制和剩余工作见 [交付记录](../reports/BIRDTIE-DGS-EXECUTION-2026-10-16.md)。外部双设备依赖BLOCKED，其余未运行NOT_RUN；整体PARTIAL，原父任务IN_PROGRESS。

---

## 历史检查点：APK38时刻（以下不代表当前状态）

# DGS 2026-10-16 执行与验收

当前整体 **IN_PROGRESS**，状态真源为 `automation/codex_task_queue.json` 原父任务下的 `dgs_execution_20261016`。适用 UX-CHECK 的身份、可恢复读取、现有领域动作、Now统一面板、简体中文与真实证据约束继续有效；正常字号，不录屏。

| 检查点 | 当前结果 | 证据/缺口 |
| --- | --- | --- |
| DGS-01 | PARTIAL | 当前树/API30/schema173/安装36r2/候选38已分开核对；新包真机待连接 |
| DGS-02 | PARTIAL | 单媒体两条全清空路径及组织切换公开图层不恢复已确定性复现；现场/30次冷启动NOT_RUN |
| DGS-03 | PARTIAL | 同一监听周期后统一恢复；token、账号、组织3项PASS；真机NOT_RUN |
| DGS-04 | PARTIAL | 局部资源撤权、租期保留、一次重验、短暂失败恢复直接测试PASS；真机前后台/长停留NOT_RUN |
| DGS-05 | PARTIAL | 当前v2支持无POI坐标点；空标题anchor回归PASS；完整发布/分享一致性待后续 |
| DGS-06 | IN_PROGRESS | 精确来源审计已完成；3个seed和历史脚本默认拒绝守卫实施中；固定码真实用户保留 |
| DGS-07 | PARTIAL | 备份恢复、精确22表103行清理、二次ALREADY_CLEAN、真实数据对账PASS；手机旧缓存清除效果NOT_RUN |
| DGS-08 | PARTIAL | Civu稳定映射只读核对；完整逐域幂等与媒体抽样待完成 |
| DGS-09 | PARTIAL | Civu来源映射完成，Mapbox巴黎/Paris真实200；Android高德桥编译PASS，真实鉴权NOT_RUN |
| DGS-10 | PARTIAL | Now搜索式选择器接线，IME/防抖/取消/失败保留/空城市目录直接测试PASS；真机NOT_RUN |
| DGS-11 | PARTIAL | 独立browse destination到既有地图及bounds-only API测试PASS，不建假city/POI、不改草稿；真实跨城市手机NOT_RUN |
| DGS-12 | IN_PROGRESS | 精确Civu/Birdtie行为与迁移矩阵核对中，已有规则不等于本轮E2E通过 |
| DGS-13 | IN_PROGRESS | 未知通知只读占位及目标隔离已实现，20项直接测试PASS；完整双设备真实链路NOT_RUN |
| DGS-14 | IN_PROGRESS | 无标题动态聊天分享及迟到身份退休适配中；各入口完整真实链路NOT_RUN |
| DGS-15 | TODO | 新包、真实数据、性能及总回归未完成 |

实际执行：地图读取边界49项PASS，日志与9文件冻结SHA在 `docs/testing/evidence/dgs-20261016/map-stability/final-targeted.json`；host身份恢复3项PASS，命令、cwd、exit0、2文件SHA在 `docs/testing/evidence/dgs-20261016/identity-host/after-fix.json`。保留修复前失败。隔离fixture不写实际使用环境。

Android37只验证了合包、manifest新Key匹配、CIVU两份样式字节及签名。此前36r2的INVALID_USER_SCODE是真实旧Key结果；不能将旧默认底图截图称为新Key或自定义样式PASS。手机未连接属于BLOCKED；未运行的具体测试均为NOT_RUN。

## 本批补充证据

- `destination/targeted-composite.json`：26 PASS；源映射、真实provider脱敏结果见同目录REPORT.md。
- `root-integration/tests.json`：7 PASS、sourceStable=true；3项原生搜索桥、1项实际Now目的地选择、3项身份恢复。包含的身份3项与此前host重复，不重复计为新增。
- `notifications/targeted03.json`：20 PASS，含3项新增未知通知/当前界面测试与17项既有直接边界。前两次widget失败因借用旧登录fixture未绑定API origin，未发网络请求；保留日志并改为明确隔离的已登录fixture，不放宽产品认证。
- `content-map-refinement-20261016/candidate-apk38/receipt.json`：Android profile构建exit0、560文件sourceStable；`china-map-boundary-20261016/android38-device/artifact-identity.json`核对Key存在且与Civu不同、签名及原始两份样式入包。APK38不含随后通知/分享修改。
- `data-cleanup/receipt.json`：22表103行精确清理、备份恢复及二次执行通过。真实数据/未知记录保留，未物理删除媒体bucket或账号。

手机仍离线，合包成功不等于新Key、Civu样式、国内坐标对齐或DGS真机通过。网页按用户要求延期。
