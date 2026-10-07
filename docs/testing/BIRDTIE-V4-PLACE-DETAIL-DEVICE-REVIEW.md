# V4 地点详情真机复核

任务：`BT-V4-PLC-005`。状态：PARTIAL。2026-10-02 已在连接的 Android 真机完成隔离本地开发环境复核；正式授权数据、已核验 Business 经营关系和试点环境仍待验收。仅使用经授权的测试账号和真实可见数据做正式复核；下列开发 seed 截图不能作为正式试点证据。

1. 从地图公开 Place Pin 的轻量卡进入详情，确认标题、来源、审核地址、关联活动使用同一 Place ID；关闭详情后地图选择不异常。
2. 从 Agent 地点结果打开同一详情，尤其检查无公开坐标的地点能进入详情，但不出现 Pin 或导航。
3. 对已审核 Venue 查看容量、适配、预约方式、公开组织与已核验 Business；过期、未审核、撤销或隐藏时不显示旧经营关系。
4. 登录本人后查看与地点关联的私人 Moment；退出登录或换账号后确认不出现，分享文本也不得包含私人内容。公开 Place Memory 需后续 `BT-V4-MOM-002` 单独验收。
5. 测试导航、分享、无坐标、空活动、弱网/失败与重试，确认只展示真实可用动作，不凭空显示预约能力。

验收记录需填：设备型号与系统、App/API 版本、测试环境、测试账号角色、Place/Activity/Venue ID、截图或录屏路径、上述各项结果和失败请求 ID。真机证据齐全后再将 `BT-V4-PLC-005` 从 PARTIAL 评估为 DONE。

## 2026-10-02 隔离开发环境真机记录

- 设备：25098PN5AC，Android 16，ADB 序列号 `c641566b`。Flutter App `0.1.0+1` Debug、API 工作树 HEAD `d6e86d3` 加未提交改动；非发布包。API `127.0.0.1:3697` 仅经 `adb reverse tcp:3697 tcp:3697` 连接一次性 PostgreSQL `birdtie_phone_review_f3de825b08`；43 个迁移及 3 组明确标注为虚构的开发 seed。Mapbox 公共令牌由本地忽略配置文件注入，未写入报告。
- 身份：先匿名，再用本地开发手机号映射的合成 Person 管理员 `b1700000-0000-4000-8000-000000000010` 登录，最后退出。此验证码不验证手机号所有权，不能代表生产身份。
- 公开 Place `b1700000-0000-4000-8000-000000000004`、其活动 `...0005` 和 `...0017`：地图 Pin/轻卡与 Agent `find sports venues` 均进入同一详情。详情显示来源、公开坐标导航、关联活动及无预约承诺的举办条件。向隔离库插入合成的审核 Venue 后，页面显示容量 20 人、羽毛球适配和联系咨询；见 [地图轻卡](evidence/place-detail-2026-10-02/03-place-peek.png)、[Venue 详情](evidence/place-detail-2026-10-02/16-reviewed-venue-detail.png)。
- 私人 Moment `7e1f29b8-54a5-4fdc-93cb-72f91c22aed1` 只在合成作者登录后出现；退出后同一 Place 详情不出现。系统分享面板的地点预览不包含私人标题或正文；见 [本人视图](evidence/place-detail-2026-10-02/28-private-moment-owner.png)、[分享预览](evidence/place-detail-2026-10-02/30-private-share-sheet.png)、[退出后视图](evidence/place-detail-2026-10-02/33-private-moment-logged-out.png)。Moment 为本地 SQL 测试行，未由真实用户创作。
- 无坐标 Place `b1700000-0000-4000-8000-000000000088` 由隔离库合成，API 返回 `location.precision=none`。Agent 返回 1 条稳定 ID 结果且 `mapEffects.pinEntityIds=[]`；真机结果可进入详情，详情无导航按钮，空活动如实显示；见 [Agent 结果](evidence/place-detail-2026-10-02/41-new-no-map-query.png)、[详情](evidence/place-detail-2026-10-02/42-no-coordinate-detail.png)。
- 移除 ADB 反向端口后，详情显示加载失败与“重试”，恢复端口后同一页面成功加载，未显示旧资料；见 [断连](evidence/place-detail-2026-10-02/38-offline-place.png)、[恢复](evidence/place-detail-2026-10-02/39-retry-restored.png)。本次无 HTTP 请求 ID，因为断连时请求未到达 API。
- 发现 `sports_venue` 原样露出英文内部代码，已在 `place_detail_sheet.dart` 转成“体育场馆”；无坐标 fixture 的 `library` 转成“图书馆”。Flutter 定向 analyze/test 3 项 PASS，带持久 Mapbox 公共令牌和本地 API 配置的 Debug APK 已重新构建并安装。

仍需真实资料来源和授权测试账号，对正式已审核 Venue、Business 经营关系、隐藏/过期/撤销后的页面、跨账号私密状态及导航外部应用完成复核。正式身份、HTTPS API 和真实组织活动缺失，Closed Pilot Ready 仍为 NO。
