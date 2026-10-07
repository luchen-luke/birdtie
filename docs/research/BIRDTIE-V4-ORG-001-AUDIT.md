# BT-V4-ORG-001 审计与本地验证

2026-10-03。原 goal：保留原组织问答并接入共享 Runtime/capability pack。原 acceptance：只由核验组织的公开组织资料、活动、FAQ grounding。原依赖 AGF/ORG 基础均实际DONE；本轮不修改旧队列优先级或发布门槛。

## 实际基线 → 实现

基线原 HTTP + FAQStore + `organization.BuildAnswer`真实运行，但 PG先读公开 Profile再独立查询 FAQ/活动，没有共享能力包接线；锁等待会混合旧核验与新来源。已有管理者发布/审计与规则 renderer复用，无替代 UI、服务、AgentID或 DDL。

新增 `agentorganization`三文件；PG组织桥/专项测试、原FAQ方法委托；原HTTP ask局部strict wire/current Session及两个专项测试。共9个owned Go文件。公共 sources单statement/current typed关联，原公共答案/unknown/source ID保留。组织真实核验/provider/Memory不是本项代码完成证据。

## 真实命令与历史结果

从官方repo运行 `pwsh -NoProfile -File work/v4-org001/verify.ps1 -Round <新label>`：fresh001–066+3开发seeds、真实PGStore/正式注册HTTP、test/vet/build、全public完整行与源SHA、最后DROP随机自有库。每个raw目录和当前九文件副本保留。

| 帧 | 实际结果 | 解释 |
|---|---|---|
| pure/compile1–3 | 编译及纯合同通过 | 不是native身份/数据库证据 |
| native1 | 54 TestPASS、3 TestFAIL（2子例+parent）、1pkgFAIL；vet/build0 | 到期Session夹具违反真实createdAt约束；修夹具为提前createdAt+同stamp期限，不改schema守卫 |
| native2 | 57PASS/0fail-skip | 真实公开问答、隐藏/撤核验/Agent与角色、旧admin source更新、final前四种真实会话无效化 |
| native3 | 62PASS/0fail-skip | 加实际FAQ relation等待+default RR四例；公开payload真正当前 |
| native4 | 65PASS/0fail-skip | final SHARE等待absolute/idle过期；未覆盖首次旧Authenticate idle续期 |
| native5 | 68PASS/0fail-skip | 初始及final四种锁等待，局部初始native resolver；这是旧join锁序历史帧 |
| native6 | 69PASS/0fail-skip、vet/build0 | 最终明确Account→Session锁序、Session SHARE边探针、旧人类Profile实际编辑成功且deadlocks计数不增；428观察源、9owned副本/live SHA保持；DBDROP |
| 根 shared current066-full1 | 7803/7803/7806PASS，三轮0fail-skip，但总体exit1 | 并发审查期间真实修组织Session，SOURCE_CHANGED；保留，不能作最终固定帧 |
| 根 shared current066-full2 | 三轮各7807PASS/0fail-skip、vet/build0 | 557当前/捕获API源逐SHA核对、旧完整public/up保留、DBDROP；正式固定066帧 |

工作区最初未提交内容全部保留。compile4第一次shell输出路径误写为不存在的`D:/work`，实际Go未运行；修用绝对repo路径后通过。无reset/unrelated覆盖。只读审查发现并修复首次/最终旧statement-now过期窗口、join反序潜在锁环、旧链接hostname/userinfo验证，以及跨表同UUID额外拒绝；不是用假的allowed/provider修理。

## 真实反例对照

仅work-only Go overlay，生产源未改，原生 Session/Account/权限/SQL与真值未替换：

- `verify-control.ps1 -Round legacy-session-red1 -OverlayFile .../controls/legacy-session-overlay.json -Pattern '^TestOrganizationCapabilityRegisteredHTTPNativeFinalWaitExpiry$'`：原native3 HTTP的真实初始/最终Authenticate实现，initial/idle与final/absolute、final/idle三叶子失败返回200；6 TestFAIL含parents/1pkgFAIL，1 TestPASS（initial/absolute由旧final拒绝）。当前native6这四叶子全通过401。
- 同runner `legacy-sources-red1` / `legacy-sources-overlay.json` / `'^TestOrganizationCapabilityNativeCoherentSourceWait$'`：旧immutable current065实际PG FAQ实现，撤核验/组织私密两叶子仍输出known，3 TestFAIL含parent/1pkgFAIL、2PASS（下线FAQ/取消）。当前native6四叶子全通过。

两对照均完整public保持、9实际owned源未改、自有DBDROP；故负例是旧实现可达窗口，不是fixture失败。readonly审查在 `work/v4-org001/read-only-review.md`，该审查没有执行Go/数据库或编辑产品。

## 限制 / 发布

本任务交付仅CODE_LOCAL公开规则能力。没有模型、Organization Memory、A2A、组织工具批准、生产IdP、CSSA现实资料或任何外部部署。本轮没有改UI或单独跑组织真机/辅助技术；race未运行（Windows CGO0/no GCC）。实际源权限线性化点是单公开payload，不能声称交付后的网络绝对撤回。旧全局Authenticate不在本项修复范围。

Closed Pilot / Consumer Beta仍NO；本项不解除原真实试点门禁。下一项按满足实际依赖的任务接续，不重复创建UI或授权backlog。
