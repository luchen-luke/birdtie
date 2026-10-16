# 会话恢复检查点（2026-10-16）

当前交付代码：API32/schema176已部署；APK42已替换安装，应用保持关闭等待真实idle期限。最终真机状态和Git提交见根报告及最终回执，本文件不作为完成声明。

- 根因：APK40真实原账号19:48:33 idle超时；20:46截图空图，102条动态/83条Civu映射/39消息仍在。
- API：backend-build32/build01与deploy01。只增加176，原175摘要与既有业务行保护，未登记旧token。
- 认证：auth04最终11 PASS；auth05旧相关45 PASS。auth01、auth04-before失败保留。
- 地图：area/targeted05 5 PASS对应41；area/login-tap03 4 PASS对应42，其中3项重叠。41原生点击失败与隔离测试改前PASS并存，不能混称复现。
- APK41：替换安装保数据，原侧栏固定码登录后不拖动即见7个位置，mapCreations=1。新地图CTA点按失败保留。
- APK42：候选SHA af7f66f543f8050e790440aa9d3e9a8f77fe197e3843d0efa1e0e441e3b00d66；宿主提示覆盖与忙碌守卫修订；安装后仍无进程。
- 实际超时窗口：应用21:26:59停止，服务器idle截止21:56:49；未改变到期时间，没有合成数据或屏幕录像。

证据中的worker NOT_RUN仅说明其职责范围，由根实际设备/部署回执独立覆盖；未执行项目保留NOT_RUN。所有截图位于用户本机outputs，Git不保存手机截图或凭据。

## 21:57自然过期与设备阻塞

服务器原会话已实际idleExpired，family仍有效。最终启动时ADB列表为空，cold-start命令退出1。自动刷新、42登录入口和区域结果真机NOT_RUN，设备依赖BLOCKED。42已安装及API32已部署不因此失效，但不能记为全部真机通过。完整回执见 android42-device/verification.json；用户重新连接后继续当前已过期会话。
