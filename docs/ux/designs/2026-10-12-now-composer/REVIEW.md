# Now 输入与结果面板：第一轮设计交付记录

本记录为第一轮历史证据。原型快照已保留为 `prototype.revision-01.fragment.html`；当前方案以 `REVISION-02.md` 与第二轮检查记录为准，以下首轮判定不覆盖后续修改。

日期：2026-10-12。范围：用户已选“先完成可点击设计稿与设计说明”；未修改 Flutter、后台、旧规范或任务队列，未构建、装包或运行生产测试。

最终设计为全宽贴底单主面板、单一输入框；空态紧凑，编辑/实际换行时正文与工具分层。主流程涵盖材料、上下文引用、模拟语音填稿、短实体结果、长文阅读、可编辑动态与受众预览、加载、空和错误。地图、人物、地址、材料、来源及发送/发布均为演示。

## 设计评审

独立评审的最终处置为 `ship`，仅指本次原型。所列修正评分为 6 resolved / 0 partial / 0 unresolved：

1. 短回答的导航与来源完整可见。
2. 按实际宽度与行高判断换行，输入变成两层；大字号占位不裁切；UI 状态回灌不覆盖内存草稿。
3. 地点卡展示 `Shiprow, Aberdeen` 旧原型示例及“演示资料”，去掉实现说明。
4. 草稿地点选择进入字段和最终预览。
5. 内容与输入的内部边距统一为 16。
6. 发送、展开、收起的命中区域为 48。

首次机械检查的两个告警已经对应修正：去掉布局高度/底部动画；地图演示注记从 10 调至 11。未以机械结果代替截图评审。

## 实际检查

本地浏览器设计稿检查命令的工作目录均为 `D:/Project/birdtie`，两条均 exit 0：

```text
C:/Users/chens/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin/node.exe C:/Users/chens/.codex/visualizations/2026/10/12/01a12f37-35ca-74a2-bba1-8e9d69b940e3/check-preview.cjs
C:/Users/chens/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin/node.exe C:/Users/chens/.codex/visualizations/2026/10/12/01a12f37-35ca-74a2-bba1-8e9d69b940e3/check-final-menu.cjs
```

`review/verification.json` 记录 11 项行为断言全部通过、零页面脚本错误：菜单保稿、语音取消保稿、模拟转写不自动发送、手动提交同地点卡、返回后续读、受众和地点预览、短结果操作可见、窄屏无横向溢出、输入贴键盘示意上缘、大文字可滚。最后菜单专项检查 `issues=[]`：桌面/窄屏中文两行原稿保留，菜单位于输入正文上方。

截图在 `review/`：桌面、360px、320px 内容宽度，以及输入字号放大与深色示意。`desktop-*` 指桌面浏览器里的手机设计预览，不表示完成桌面 App 布局；键盘为原型示意，不是原生 IME。

原生系统字号/读屏、真实录音、真实服务、隐私权限、上传/模型理解、发送/发布、持久化与性能：本次 NOT_RUN。现有产品已取得的语音证据另见设计说明，不被本稿替代。

## 文件与来源保存

- 设计说明：`DESIGN-SPEC.md`。
- 第一轮原型快照：`prototype.revision-01.fragment.html`；它是 HTML 片段，需在 Codex 可视化环境或对应预览包装器中打开。
- 预览源：`C:/Users/chens/.codex/visualizations/2026/10/12/01a12f37-35ca-74a2-bba1-8e9d69b940e3/birdtie-now-composer.html`。
- 源文件 SHA-256：`F84FEB7E1EF439160E49C5405741BFCE8C94B7C4FA4C49820DE76EEE1DCAF706`。
- 仅将上述原型与两份检查脚本复制到此设计目录；未从旧 `D:/BirdTie` 移动或删除材料。用户参考 JPG 原图未复制到生产资源。

后续实现应先依据已确认设计与已有领域契约扩展现有组件。新拟议入口不可因可点击稿而标为已接通，不另建重复需求队列。
