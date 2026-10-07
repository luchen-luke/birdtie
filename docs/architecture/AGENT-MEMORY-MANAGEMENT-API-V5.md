# Agent Memory Management API V5

本规范属于 AGE-069 本人管理 API。来源为 `docs/product/BT-V5-AGE-AGENT-ENRICHMENT-EPIC.md` 的原 AGE-069 五项要求；身份、Memory 与证据沿用 AGE-004/005/008，具体版本人工批准沿用 AGE-011/原 094。它不提供模型 Context、机器写入许可或概率校准。原 Memory/纠正写入服务、状态与 094 schema 继续为唯一真源。

## 五项接口与原领域动作

| 原要求 | 真实注册路径 | 权威动作与响应 |
| --- | --- | --- |
| GET memories | `GET /v1/me/agent-memories` | 原 `ReadOwnMemories`，保留 data envelope |
| GET memory detail | `GET /v1/me/agent-memories/{memoryID}` | 新 `CurrentHumanStore` 本人详情，data envelope |
| UPDATE memory | `PUT /v1/me/agent-memories/{memoryID}` | 原 `PutOwnMemory` 与 expectedVersion CAS，data envelope |
| DELETE memory | `DELETE /v1/me/agent-memories/{memoryID}` | 原 `DeleteOwnMemory`，原 DELETED tombstone，data envelope |
| REJECT memory | `POST /v1/me/agent-memories/{memoryID}/reject` | 适配原 094 MEMORY/REJECT 的具体人工 operation，返回原 bare Receipt |

这些路径仅支持当前有效本人个人助理身份。Bearer、当前 Personal Agent、本人 Account 和原 Session 均由原 Authenticate/native binding 得出。组织 workspace、客户端 owner/agent/confirmed 字段不能授予权限。详情 GET 必须无正文、无 query（包括单独 `?`）；拒绝正文严格只有 operationId 与 planDigest，重复 key、未知 key、错误大小写或额外 JSON 均拒绝。

## 详情投影与当前读取证明

可选 `agentmemory.CurrentHumanStore` 提供 `ReadOwnMemoryDetail` 和 `RevalidateOwnMemoryDetail`。旧 Store 没有这两个方法时，注册详情路由返回 503；不以旧 fake 或传入 owner 冒充原生当前读取。

响应 `data` 包含 schemaVersion、owner、agentId、target（原稳定 Memory ID/version/status）、memory、observedAt、expiresAt、explanation 与 modelAccess=false。ACTIVE 或 PENDING_REVIEW 仅返回本人当前原 Record；DELETED 或已自然到期的记录只返回最小 target metadata，memory=null，不返回旧正文、structuredValue 或失效来源。待审 INFERRED 仅为原保留形状，不是已确认事实；不生成 INFERRED ACTIVE 或新置信概率。

私有 proof 不进入 JSON，也不能从 JSON 重建。它同时绑定完整已编码投影、原 Session digest、原 owner/Agent/metadata authority、Memory 完整行及 xmin、完整 Evidence 行及 xmin、既有当前来源 resolver 的结果、094 suppression 控制以及原七个 trigger/function 的定义、启用状态与 catalog 行版本。新详情额外绑定完整当前 Session 行与 xmin；同 UUID 的 token/撤权字段恢复不复活旧读取证明。原 094 authority/批准账本和 Session writer 不为此修改。

读取事务复用原身份/metadata锁，Memory 与相关来源按既有顺序取得锁。它不调用会清除过期 review 的旧 beginMemoryCorrection 写入边界。捕获两次必须完整相同；所有可能的读取锁等待后，PG clock 与原 Session/当前控制末核。当前有效正文租期为两分钟、原 Memory TTL、当前 Session absolute/idle 期限、当前有效支持源最短期限中的最早者。到期不延长，不从请求抵达或客户端时间重新计算许可。过期 Session 导致原 authority 为 NULL 时明确返回权限拒绝，不当作无法扫描的服务错误。

handler 先验证闭集 DTO、路径、本人绑定和私有 seal，再完整 JSON 编码到 buffer，随后执行原生 revalidate；只在这些检查全部成功后写出原编码字节。校验失败或取消不释放私密正文，不重读另一个版本替换原材料。所有响应 no-store。GET/native revalidate 不写 Memory、Evidence、source、correction、suppression、audit 或其它账本；原 Authenticate 的会话活动更新属于既有认证逻辑，并非新详情 writer。

来源变更会使旧支持证明失效。本人独立 EXPLICIT 声明不会仅因原 Moment 或支持过期而删除；新详情只读取其当前原声明，不返回已失效来源 ID/正文。当前 provenance 与领域证据仍沿原接口，不另设一套来源推断器。

## 路径绑定的 Memory 拒绝

先用原 `POST /v1/me/agent-memory-corrections/previews` 建立并展示具体版本的人审 REJECT，随后显式提交新路径适配器。新请求不建立预览，也不接受 summary/category/targetKind/confirmed 等批准材料。它严格核对原 operation 的 owner、Agent、原 Session、immutable MEMORY target、路径 ID、Action=REJECT 和 planDigest。错路径、CANDIDATE target、其它 action/digest 均在调用原 Confirm 之前拒绝，零确认副作用。

PENDING 必须仍在原截止时间内；只调用既有 `ConfirmOwnMemoryCorrection`，由其原完整版本、来源、身份、权限、deadline 与事务末核执行。初始检查事务释放后，原 094 trigger 保证 operation target/action/digest 不可更改；原 Confirm 再执行自己的全部检查，适配读取本身不是新的批准。

本人 EXPLICIT 与 reserved INFERRED/PENDING_REVIEW 的 MEMORY REJECT 都明确丢弃该 Memory，复用原 DELETED、version+1、summary=''、structuredValue={} 和证据清除语义；原 Moment 不改，原 source nature 不升级。它与 CANDIDATE REJECT 的 REJECTED 状态不同，Candidate UUID 不能作为 Memory 的别名。新接口不新增 Memory 状态、第二 writer 或批准表。

同一已 COMMITTED operation 的重复调用只读原 Receipt，不再 Confirm、删除或写第二条 audit。新 Session 不继承旧具体批准，即使原操作已经提交；本人新 Session 可通过原 GET correction receipt 核实历史 metadata。结果 UNKNOWN 只查原 operation ID，不能自动 POST、新建预览或把 404 当作未执行的证明。Receipt 的历史提交与 CurrentResultMatches 分开表达。

## 接口与验收边界

`agentmemory` 不反向导入已依赖它的 `agentmemorycorrection`。详情 optional port 位于新 api.go；HTTP 的本地拒绝 optional interface 与 PostgreSQL 适配方法直接复用原 correction Receipt。server.go 只增两个注册路径，原 list/PUT/DELETE/correction 路由保持。

没有新 DDL、客户端组件、provider 或模型入口。适用 UX-CHECK-06/08/09/10/11/16：真实事实与普通管理、同领域动作、具体版本批准、未知原 ID 核实、中文说明、身份及隐私边界。AGE-007 概率校准与 INFERRED ACTIVE 仍未实现，不是本人工 API 的依赖替代。模型/自动写/vision/A2A OFF，Closed Pilot/Beta NO；真机/辅助技术/生产运行不由这些 API 定向合成测试证明。

本地结果和失败历史见 `docs/research/BIRDTIE-V5-AGE-069-AUDIT.md` 与 `docs/testing/evidence/agent-memory-api-2026-10-06`。定向源使用原 root 已验证 schema094 完整970不可变副本＋本任务七个新 Go 文件及 server 增量；并行018的095仅记录移动 live 差异，当前合并整仓与真实迁移验收由 root 另帧执行，不能混称同一冻结帧。


## root current095联合终态（2026-10-06 root38pb）

原本地AC已DONE。当前981输入Go全仓11126 PASS/0FAIL-SKIP/packageFail，五命令0；原11003/72/103分支及multiplicity保留（仅随机fixture UUID归一，首次24 raw名字差异的harness失败保持）。094原137/48与095全140 rows/xmin/完整语义catalog保护、unused down/reapply、末parent精确同，369实际RAW owned库根SQL不存在。root不可变1431文件manifest 33b1d43ef35a96b8fbab9e638e0a425c8f42f431ff13e0c28661d34ed61c97be，根证明work/v5-age038-resume/whole095-joint-root38ox.json、状态证据local018-and069-closure-root38pb.json。原定向/RED/archive资料完整保留；非current095手机/生产调度器/真实IdP/CSSA证据。原007校准/INFERRED ACTIVE及attendance门槛未改，model/真实自动写/Vision/A2A OFF，Pilot/Beta NO。
