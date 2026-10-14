# Birdtie 文案媒体真实测试与截图（2026-10-13）

Copy29 检查点代码冻结与构建详见 [source-manifest](evidence/now-v4-v41-integration-20261010/continuation20/copy29/final/source-manifest.json)。测试不跨运行/源码累加为一次全套PASS。原失败、启动NOT_RUN、源漂移、不可用字体截图均保留。

| 范围 | 实际结果 | 证据目录（同copy29下） |
| --- | --- | --- |
| 首批媒体Go域 | 8顶层PASS/2包 | primary-media |
| 首批媒体Flutter | 68PASS/2FAIL；只失败重跑2PASS，70 distinct闭合 | primary-media |
| 设置旧测试修复 | 19个原正常字号失败分批闭合；其中primary03有无关源漂移，不能称单hash19PASS | primary-tests |
| 二级7生产页直接测试 | 首批82PASS/7FAIL；7失败定向闭合，含真实PageStorage隔离修复；89 distinct | secondary-copy |
| 社群创建/头像 | 首批14PASS/7ERROR；后6PASS/1ERROR；最后1PASS；21 distinct | community-media |
| 社群result/Saved追加 | 新客户端6 distinct、Go域1、native1 PASS；追加公共Page校验后只域1再次通过 | community-projections / api-native-copy29-04 |
| 可达页直接回归 | 48PASS/0error，源稳定，含候选新label追加bundle | reachable-tests |
| 二维码UUID退场 | sole case1PASS，保留临时码/身份退休 | qr-copy-test |
| 授权文案/非人物关注 | 4PASS（3授权+1包含3类型）；非3额外计数 | final-reachable-tests |
| root设置前/后 | before04/after04各1PASS、同数据三组截图；before02/03失败保留 | root-settings-before04 / root-settings-after04 |
| 原生初跑 | 6顶层选中，5PASS/1FAIL、0SKIP | api-native-copy29-01 |
| 原生社群修复 | 等锁会话撤销分类修复后目标1PASS；共享begin变更后另2社群PASS、0SKIP | api-native-copy29-02 /03 |
| 新社群域 | 2PASS | api-community-domain29-01 |
| 新HTTP | HTTP01 ServeMux重叠panic后修复，HTTP02原JSONL3顶层PASS；独立命令未保存，不伪造测试前frame | community-http02 |
| 静态分析严格首跑 | exit1；0 errors/16 warnings/721 infos | final-analyze01 |
| 清理警告后静态分析 | flutter analyze --no-pub --no-fatal-infos，exit0；0 errors/0 warnings/721 infos | final-analyze02 |
| API构建 | 最终11个程序，sourceStable、exit0 | copy29-all-api-build02 |
| Android profile | exit0、sourceStable；未安装（无连接设备） | copy29-client-apk-build01 |
| Web profile | exit0、sourceStable；42文件，HTTP main.dart.js实收SHA相等 | copy29-client-web-build01 / web-runtime29 |
| 本机QA | 原库marker/OID/owner核实，135单次事务、原所有计数保持；最终API刷新不重复迁移，providers OFF | runtime29 / runtime29-refresh01 |

完整命令、cwd、时间、退出码、源码/日志SHA见各receipt；上述分批结果不能累加冒称同一当前源码全套。HTTP02独立raw未保存shell flags，明确NOT_RECORDED而非猜测。

## 未运行与不计通过

| 检查 | 状态与原因 |
| --- | --- |
| 47路径组所有匿名/个人/组织/owner/admin/member全状态端到端 | NOT_RUN；只有实际localhost匿名4表面及局部隔离widget/native |
| 真实登录账号及完整用户数据 | NOT_RUN；当前实际UI登录服务不可用，QA含既有明确合成fixture |
| 真机安装与截图/相册/相机/权限 | NOT_RUN；adb devices -l实际exit0、无设备 |
| 正常字号真实键盘/读屏/长阅读全部流程 | NOT_RUN；隔离390x844可达回归不等同OS验收 |
| 地图与媒体并行帧率/滚动、冷暖加载性能 | NOT_RUN；地图可见不能替代性能计数 |
| 生产、真人投递、A2A/模型/外部视觉视频/系统推送 | NOT_RUN；原开关保持OFF，不自动激活 |
| 全Flutter/全Go仓库suite | NOT_RUN；按实际变更运行定向检查 |
| 新真实Place/Activity/Organization封面与旧外链迁移 | NOT_RUN/NOT_IMPLEMENTED当前媒体合同；4合成插画不冒称真实照片 |

用户取消的大字号专项、录屏以及延期混合语音不作为本次验收门槛或未测阻塞。

## 前后截图与合成素材

[17对截图与SHA](evidence/now-v4-v41-integration-20261010/continuation20/copy29/final/screenshots.json)；[实际浏览器范围](evidence/now-v4-v41-integration-20261010/continuation20/copy29/final/browser-receipt.json)；[4张合成图及来源](evidence/now-v4-v41-integration-20261010/continuation20/copy29/generated-media/README.md)。CJK/图标已实际查看；样例不宣称真人/设备/获准实拍，原种子及旧证据不删除。

普通设置同fixture前后：

![整改前：隔离普通字号设置](evidence/now-v4-v41-integration-20261010/continuation20/copy29/screenshots/before04-settings-person-empty.png)

![整改后：隔离普通字号设置](evidence/now-v4-v41-integration-20261010/continuation20/copy29/screenshots/after04-settings-person-empty.png)

## Copy30 商家成员当前源码检查点

本批当前源码和构建见 [Copy30 最终回执](evidence/now-v4-v41-integration-20261010/continuation20/copy30/final/receipt.json)，不沿用 Copy29 为本批最终测试。

| 范围 | 实际执行结果 | Copy30 证据 |
| --- | --- | --- |
| Go 姓名模型/HTTP 当前结果 | 3 顶层及 17 子用例 PASS，0 FAIL/0 SKIP | business-members-backend/direct01.receipt.json；原错误 18 计数按元数据修正，旧版本保留 |
| PostgreSQL 真正原生测试 | 2 PASS/0 FAIL/0 SKIP，源码稳定；独立受控子库，父库身份前后相同 | api-native-copy30-01/receipt.json |
| Flutter 直接相关 | 50 distinct 分阶段闭合；首跑 46 PASS/3 FAIL，定向 3 PASS、后 4 PASS 含渲染、新转移返回分支 1 PASS | business-members-client/receipt.json；非同一最终源码单次 50 PASS |
| 普通字号同 fixture 前/后 | 隔离渲染各有实际 PASS 记录，2 对图片，390×844/DPR1/textScale1；明确非真人 | business-members-client；另 1 distinct 渲染用例 |
| 5 文件定向分析 | --no-pub --no-fatal-infos，exit0；0 error/0 warning/1 info | analyze01/receipt.json |
| 合批编译 | API 11 程序、Android profile、Web 42 文件均 exit0/sourceStable | copy30-all-api-build01 / copy30-client-apk-build01 / copy30-client-web-build01 |
| 本机 QA 与 HTTP 产物 | 新 API/Web 的 SHA 匹配；原库 marker/OID/owner/schema/计数核实，135 未重跑 | runtime30 / web-runtime30 |
| 最新实际浏览器 | 新 IAB 会话实际匿名首页控件与地图可见，1280×720 默认视口单张；无成员页登录 | browser-smoke/receipt.json |
| 独立复核 | 只读源/回执/SHA，无新增生产 must-fix；文档 receipt 路径已改正 | independent-review/receipt.md；复核不冒称新测试 |

APK 保留 127.0.0.1:18091 开发配置，当前 API/Web 为 18112。装包、设备端口转发、连接新运行时及真实商家身份操作均 NOT_RUN；编译包不是已安装或已联调的交付。商家后图取自 after03、早于最终转移返回编辑的角色修复；该分支由 targeted04 验证，不把图片伪称最终包真人截图。HTTP 实收摘要另见 web-runtime30/http-current-receipt.json。旧 IAB 会话失效后已使用新的同任务 IAB 核验匿名首页，不替代上表商家角色或全站验收。用户取消的放大字号/录屏不列缺口。

商家编辑同 fixture 前后（隔离渲染，非真人账号）：

![商家成员整改前](evidence/now-v4-v41-integration-20261010/continuation20/copy30/business-members-client/before-business-members-editor.png)

![商家成员整改后](evidence/now-v4-v41-integration-20261010/continuation20/copy30/business-members-client/after-business-members-editor.png)

当前本机匿名首页单张，仅为新构建运行证明，不计新增前后对数：

![当前匿名首页](evidence/now-v4-v41-integration-20261010/continuation20/copy30/browser-smoke/home-anonymous.jpg)
