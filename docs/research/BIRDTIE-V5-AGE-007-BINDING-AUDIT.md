# AGE-007 人工候选页连接绑定接续审计

日期：2026-10-04。范围为原 PARTIAL 任务的 CODE_LOCAL 修复，非自动富集能力。

## 原需求与去重

复用 `AGENT-MEMORY-CANDIDATE-V5.md` 中已实现的本人原生候选、sealed 人工预览、原候选 ID 对账与版本化接受。原任务 depends_on 为 BT-V5-AGE-004/005/008、BT-V4-SAF-004、BT-V5-INT-001，根代理检查均 DONE；原任务对象与发布门槛保留于 `work/v5-age038-resume/original-age007-binding-partial.json`。没有新建任务、用途许可、候选 ledger、Memory writer 或 Settings 入口。

接续前检查 AIR010/011/027：实际 Run/每次外发目的/Provider 等依赖尚缺，不能添加重复预算、人审或离线接口并称闭环。Activity 临时用途062需原 recipient、Activity 权限与 consent 真源协调，未在本并行切片造 UI/授予。已存在候选页面的真实绑定缺陷才是可独立修复项。

## 实际缺陷与短计划

旧 State 只创建一次 transport，认证 getter 却读更新后的 widget。相同 key 更换 auth/API endpoint 后，token B 被发送至旧 host A；同身份只更换 endpoint，旧具体批准仍显示。`red1.log` 的两个实际 MockClient 测试均 FAIL；没有向真实服务发送请求。

计划并执行：捕获创建时 auth、client、base、workspace listener/getter；同 key 任何配置替换永久退役旧 State，移除原 listener，清空 controller 私密草稿/预览并关闭其自有 transport。普通当前 auth/workspace 通知沿用既有 generation/serial 失效与读取逻辑。重新从入口创建新 State 才读取新连接，不继承旧批准。补正常动作、A→B→A、晚响应、transport ownership 和小屏语义测试，再分析与冻结。

## 实施与权限边界

- 修改 Page 与 Controller；新增 binding widget 测试，并扩展原 controller 测试。原 API/Go/SQL/Settings 未改。
- 旧页面只表达“本页旧预览不能继续提交”；不冒称后台许可已撤销或已提交动作已回滚。未知结果需核实原候选，不自动补发。
- Controller dispose 幂等并清空私密记录、来源选择、批准、当前结果和错误。迟到完成在原 `_current` 的 closed/generation/serial 检查被丢弃。
- 借用 client 不关闭；自有 IO client 通过真实 `http.Client()` factory + `HttpOverrides` 计数验证 dispose 两次仅 close 一次。
- 正常初始 GET→具体 preview→原 opaque token accept 保持；无变更 rebuild 不退役当前批准。换 key 新 State 只发当前 GET，不自动 POST。

## 验证与失败保留

工作目录 `work/v5-age007-binding`：

| 帧 | 实际结果 | 含义 |
| --- | --- | --- |
| red1 | 2 FAIL，exit1 | 产品修复前，旧 host 收新 token、旧预览未清除 |
| green1 | 2 PASS，exit0 | 初始修复的两项回归 |
| green2 | 11 PASS/1 FAIL，exit1 | 测试 semantics handle 在测试结束前未释放；补显式 dispose，未改产品 |
| target1 | 40 PASS，exit0 | 原 API/controller/page 加新 binding 正负路径 |
| analyze1 | 4 info，exit1 | 测试 if 缺花括号，保留原输出后修样式 |
| target2 | 测试 raw 日志保留；runner 解析失败 | 首行 JSON list 被误当 object，不能计最终验收 |
| analyze2 | 6 items，无问题，exit0 | 同最终产品源与修好后的测试 |
| target3 | 40 功能 PASS + 4 loading，0 FAIL/SKIP，exit0 | machine 结果与前后5个获分配文件 SHA 稳定，freeze1 |

target3 覆盖正常原 token/host/opaque 接受、base A→B→A、新 State GET、client/listenable/getter 同 key 替换、listener 移除、borrowed close0、owned close1、晚 GET/preview/confirmed accept/unknown accept 无恢复或重发、controller 双 dispose 与状态清空、320px/font2 滚动返回及语义、原 workspace/权限/过期/取消/未知原 ID 对账回归。

复现命令（client 目录）：

```text
flutter test test/agent_memory_candidate_api_test.dart test/agent_memory_candidate_controller_test.dart test/agent_memory_candidate_page_test.dart test/agent_memory_candidate_binding_test.dart --machine
flutter analyze lib/src/workspace/agent_memory_candidate_page.dart lib/src/workspace/agent_memory_candidate_controller.dart test/agent_memory_candidate_api_test.dart test/agent_memory_candidate_page_test.dart test/agent_memory_candidate_controller_test.dart test/agent_memory_candidate_binding_test.dart
```

可执行验证与哈希脚本：`work/v5-age007-binding/verify-client.py`。原 whole Go9423/fresh077 是根已核证历史帧；本切片没有 Go 修改，不把它写成新测试。新全 Flutter/build/手机由根在双方冻结后统一核验。

## UX 与剩余条件

UX-CHECK-05/07/08/09/10：当前具体预览、退役、迟到、原 ID 对账与零自动重发由测试覆盖。12：widget 中新 State 重开/返回已测，真实 App 前后台/重启未测。13：复用 Scaffold/ListView/Button 与原领域动作。14：320/font2 返回>=48dp、scroll/semantics 已测，实际 TalkBack 未测。01/15：入口继续原 Settings，独立真人可用性与截图待根实际记录。16：未添加埋点或输出私密候选正文到生产日志。

全 AGE-007 仍 PARTIAL：自动 analysis purpose/current native CandidateSubmitter 仍 UNAVAILABLE；INFERRED 激活/模型出口/真实供给/正式发布条件不在本切片。Widget 合成验收不等于真机、运营或 Consumer Beta/Closed Pilot。源冻结后只追加文档证据，live 队列与共用报告由根更新。
