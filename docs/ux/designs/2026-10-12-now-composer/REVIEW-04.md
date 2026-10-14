# Now 第四轮逐点地图：检查与来源

2026-10-12。用户明确要求视野中每个地点与动态直接展示，密集时采用淡色单点，取消数字聚合和必须放大才拆开。本轮仅更新可点击设计稿和文档；生产地图策略、原任务队列、数据与发布状态未修改。

## 最终结果

独立收尾结论为可交付本轮设计稿，无影响用户目标的遗留缺陷。纯底图移除了第三轮截图烘焙的“9”；三个地点和六条演示动态分别有稳定 ID、类别、按钮和详情。淡点模式保留同一批九对象；每点直接打开该地点或该条动态。已有顶栏、局部玻璃输入条、左侧导航、右侧联系人和输入框内录音继续保留。

同位置的关联动态采用独立视觉位置和细引线，原锚点不变，不表示作者当前位置。这里的九对象是构造的设计演示集合，不是用户当前截图中“9”的真实成员。

## 实际检查

工作目录 `D:/Project/birdtie`，执行命令 exit 0：

```text
C:/Users/chens/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin/node.exe C:/Users/chens/.codex/visualizations/2026/10/12/01a12f37-35ca-74a2-bba1-8e9d69b940e3/check-revision-04.cjs
```

[verification.json](review-04/verification.json) 记录 12 项定向断言全部通过，零页面脚本错误：对象独立、没有聚合入口、底图/署名加载、浮起输入条、淡点保留相同 ID 集合、圆/方与44px按钮、地点/动态分别对应详情、保留输入草稿、左右展开和原框录音，以及360/320px标记操作区不重叠、不越界。浏览器为 Edge/Playwright；截图是浏览器内手机设计预览，键盘为布局示意。

首轮因检查脚本的收键盘选择器同时匹配两个按钮而中断，修正为键盘工具栏入口，未作为产品故障。完成首批矩阵后，发现最后一条同位置动态的备用位置与其他按钮相交，另有图标重绘短暂重建；同批补足位置候选，保留已有 SVG 只创建新图标。最终矩阵确认两项修正；原失败断言保留在 `verification-initial.json`，未删除或改成通过。

截图覆盖普通/淡点、点选动态/地点、侧栏/联系区/录音、360/320px与深色。机械检查 `impeccable detect` exit 0，0 anti-pattern，1个薄边界与宽阴影 advisory；用户指定玻璃材质保留，结果不代替视觉检查。

## Civu 依据与底图来源

- [REQ-283 用户修订](D:/Program/Civu/docs/requirements/REQ-283-compact-category-map-markers.md:15)：2026-09-05明确视野内匹配地点全显、不聚合。当前 `explore_map_marker_density.dart:80–114` 逐点布局且聚合集合为空；9月后续层级/预算曾隐藏部分环境点，本轮用户全显要求优先。
- [REQ-437 类型规则](D:/Program/Civu/docs/requirements/REQ-437-map-marker-type-and-freshness.md:17)：圆地点和小圆角方拍摄点。仅研究设计概念，没有复制Civu业务代码、媒体或真实动态。
- 底图沿用本仓已选 `lookluo/cmth7kwad001p01ssc9kw7kco` 样式，通过一次临时 Mapbox GL 渲染取得：390×849逻辑视口、2倍像素、中心 `[-2.089,57.155]`、zoom12.65、bearing/pitch0，样式137层、0资源错误。相机是为接近用户截图选定的预览视角，不宣称是原截图的精确相机。Static Images读取返回403，临时渲染使用既有已批准公开配置；未输出凭据或写入原型。原型只内嵌静态底图，保留本仓Mapbox标识与OpenStreetMap署名。官方资料：[静态图像及署名说明](https://docs.mapbox.com/api/maps/static-images/)。
- 地点锚点来源：[市议会海事博物馆](https://sites.aberdeencity.gov.uk/AAGM/plan-your-visit/maritime-museum)（57.1464844993，-2.0949554443359）、[Trove King's College](https://www.trove.scot/place/20194)（57.16398，-2.10117）、[Union Terrace Gardens](https://en.wikipedia.org/wiki/Union_Terrace_Gardens)（57.14694，-2.10333）。用 D3 Mercator 投影与静态底图相机一致。原型不提供实时开放信息，动态文字、作者与关联关系均为fixture。

## 保存及验收边界

当前预览源为 `C:/Users/chens/.codex/visualizations/2026/10/12/01a12f37-35ca-74a2-bba1-8e9d69b940e3/birdtie-now-individual-points.html`，与 `prototype.fragment.html` 相同，382,140 bytes，SHA-256 `33F706E8E213B46510C05F891AC5CD09E21DBA73A69081C2750721F342041017`。第三轮另存 `prototype.revision-03.fragment.html`，SHA-256 `32333313E0CBA3C2F7F82F9367F9A29ABCA4C8F6EAF1EB61E45CCD044271F519`；一二轮及全部历史记录保留。

`review-04/` 保存检查脚本、一次底图渲染脚本和 `current-style-basemap.jpg`；均复制自聊天专属目录，没有删除或移动Civu或旧 `D:/BirdTie` 材料。查看 [REVISION-04.md](REVISION-04.md) 获取正式设计规则与后续生产差异。

未运行生产测试、构建或装包。真实地图拖拽、原生点选/缩放、权限、录音/识别/音频/发送、原生键盘、辅助技术、后台恢复、性能与发布验收均为 **NOT_RUN**。全显只适用于有权查看且有合法锚点的对象，不扩大私密坐标精度或受众。
