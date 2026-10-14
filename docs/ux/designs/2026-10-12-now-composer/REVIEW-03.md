# Now 当前页面基础：第三轮检查记录

2026-10-12。本轮依据用户提供的真实当前 Now 截图修订可点击设计稿；最新规则见 [REVISION-03.md](REVISION-03.md)。仅保存原型、说明与浏览器证据，未修改生产代码、原队列，未构建、装包或发布。

## 结果与修正

保留左上菜单与城市、右上联系人与铃铛、真实地图外观和浮起输入条。最近联系人移入右侧展开区；空发送箭头保留为禁用状态；输入和录音从原条增长，回答出现后才展开阅读面板。截图署名与历史聚合显示保留，示例回答不绑定为聚合成员。

首批截图发现 320px 下旧截图的城市文字从新控件旁露出，已按原图比例对齐顶栏，同时恢复产品图标尺寸，完整矩阵确认无露字。收尾独立检查另发现单人会话阅读位置未恢复，已补保存展开状态与滚动位置；只重测这项受影响流程，避免重复全矩阵。

独立收尾结论：本轮可点击设计稿可交付，当前范围无高影响遗留缺陷。核实了窄屏修正、阅读位置恢复及发送后保持最新内容；结论依据源码、截图和保存的原型验证证据，不等同于真实设备验收。

## 实际验证

浏览器使用 Edge/Playwright，本地静态预览。下列命令均在 `D:/Project/birdtie` 下执行，exit 0：

```text
C:/Users/chens/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin/node.exe C:/Users/chens/.codex/visualizations/2026/10/12/01a12f37-35ca-74a2-bba1-8e9d69b940e3/check-revision-03.cjs
C:/Users/chens/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin/node.exe C:/Users/chens/.codex/visualizations/2026/10/12/01a12f37-35ca-74a2-bba1-8e9d69b940e3/check-revision-03-scroll.cjs
```

[verification.json](review-03/verification.json) 共 19 条定向断言通过，零页面脚本错误。包括空态透明面板、四顶栏入口、基准图片加载、禁用发送、左侧导航、展开区最近联系人和会话摘要、独立聊天、跨人/Now 草稿隔离、原框录音与取消、转写后手动发、切收件人丢弃旧回调、好友语音预览、窄屏和历史聚合说明。最终局部检查构造有溢出的私人会话：小林阅读位置 112px，切阿澈再返回后仍为 112px，展开状态也恢复。另确认恢复后发送新消息仍滚到最新内容（scrollTop/max 均为 560px），延后恢复回调只用于进入/返回会话并随下一次渲染失效。

`review-03/` 保存桌面浏览器内手机预览、360px、320px、深色、输入/材料、短答/阅读、联系人/通知/单人聊天、录音/转写/失败/语音预览以及聚合历史说明截图。`desktop-*` 是承载预览的浏览器尺寸，并非新增桌面产品页面。键盘为布局示意。局部补齐的阅读恢复截图为 `chat-reading-restored.png`。

机械检查 `impeccable detect` exit 0，0 个 anti-pattern；1 项薄边界配宽阴影 advisory。局部玻璃与边界为用户指定的材质方向，保留该选择，机械结果不代替截图判断。

## 保存与来源

- 当前预览：`C:/Users/chens/.codex/visualizations/2026/10/12/01a12f37-35ca-74a2-bba1-8e9d69b940e3/birdtie-now-current-page.html`；与 `prototype.fragment.html` 内容一致，330986 bytes，SHA-256 `32333313E0CBA3C2F7F82F9367F9A29ABCA4C8F6EAF1EB61E45CCD044271F519`。
- 第二轮另存 `prototype.revision-02.fragment.html`，SHA-256 `6A2D454CACE80468C4C498799985B7042785D0EBD284150D42365602932A188A`。第一轮及一二轮说明、证据保留。
- 原图来源为 `C:/Users/chens/Documents/xwechat_files/wxid_10l40cmwgt8y12_87f3/temp/RWTemp/2026-10/9e20f478899dc29eb19741386f9343c8/63b95b1e0a7bff8a5d0a71a30b32b8f0.jpg`。仅缩小压缩供预览，未重画地图；参考副本为 `review-03/current-page-reference.jpg`，图片同时内嵌于原型。没有删除或移动原图及旧 `D:/BirdTie` 内容。
- 两份定向检查脚本保存在 `review-03/`，与聊天专属目录中的原脚本一致。

真实地图拖拽/实时数据、权限、原生键盘、真实录音/识别/音频/发送、读屏、后台恢复、性能与原生产品视觉验收均为 **NOT_RUN**。本轮可交付的是可点击设计稿；浏览器通过不代表产品实现或发布验收。
