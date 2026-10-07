# Agent Feature Flags — V5

2026-10-02。唯一AGE开关规范，对应BT-V5-AGE-066和源AGE-066/067。来源为[AGE原文](../product/BT-V5-AGE-AGENT-ENRICHMENT-EPIC.md) §23；[137来源映射](../../automation/v5_requirement_mapping.json)把两项合并为同一任务。本文仅记录仓库实现与隔离本地结果，不改变[认知ADR](../decisions/ADR-AGENT-COGNITIVE-ARCHITECTURE.md)、[Memory边界](AGENT-MEMORY-ARCHITECTURE.md)或正式发布Gate。没有第二份队列、Profile/Memory真源或客户端开关授权入口。

## 1. 权限与当前能力

功能开关是**服务端能力收紧条件**，不是身份、角色、来源版本、同意、内容批准或执行资格。只有开关允许、领域能力已实现且原当前身份/owner/资源/source/consent规则同时允许，才可能继续。当前flags打开不创建Memory，不产生推断、模型出网、候选成功收据、自主动作或A2A。

五个闭集开关分别为`agent_enrichment`、`agent_memory`、`agent_attention_policy`、`agent_social_policy`、`life_map`。缺省全部OFF；后三者及Memory还要求enrichment父开关ON。关闭父开关收紧所有子功能，关闭单个子开关不改其他配置值。当前版本拒绝五项同时ON，属于严格分阶段策略，不提供`all_enabled`或通配开关。

| 开关 | 本项真实消费点 | 不构成的能力 |
| --- | --- | --- |
| agent_enrichment | FeatureGatedDomains调用现CurrentDomainAdapter的本人普通Profile/私密Context窄桥前后均查当前开关 | 不是认知Profile reader、自动富集、模型上下文或其他主体读取 |
| agent_memory | MemoryReader/CandidateSubmitter外层逐次检查开关，固定内层UnavailableCognitivePorts | ON也始终Unavailable，无Memory读写/候选批准；后续人类Memory管理不接成模型port |
| agent_attention_policy | Controller当前检查、单独Disable及并发/revision验证 | Attention领域服务仍未实现，不伪造策略内容或执行结果 |
| agent_social_policy | Controller当前检查、单独Disable及并发/revision验证 | 不开启现纯social-inference合同的运行器，也不生成兴趣或共享许可 |
| life_map | Controller当前检查、单独Disable及并发/revision验证 | 不创建LifeMap、位置推断或公开地图点位 |

普通Profile、Private/逐字段隐私设置、Context及后续本人Memory查看/更正/删除等人类管理动作保留原直接领域路径，不能因关闭认知开关阻止用户隐私控制。Organization/Business/Community不从开关继承Person私人来源；窄桥继续只允许当前本人Person及active Personal Agent，不能把HasActiveAgent布尔值补成精确认知Agent ID或源版本证明。

## 2. 服务端配置

实际实现为[config.go](../../apps/api/internal/agentfeature/config.go)、[controller.go](../../apps/api/internal/agentfeature/controller.go)。启动仅从`BIRDTIE_AGENT_FEATURE_FLAGS`读取可信服务端配置；未设置使用全OFF默认，显式空值/非法值不回落默认，启动失败。该环境配置不是客户端JSON endpoint或授权管理API。

```json
{
  "schemaVersion": "agent-feature-flags-v1",
  "flags": {
    "agent_enrichment": false,
    "agent_memory": false,
    "agent_attention_policy": false,
    "agent_social_policy": false,
    "life_map": false
  },
  "pilot": {
    "memory": "basic",
    "inference": "conservative",
    "autonomousAction": false,
    "sensitiveInference": false
  }
}
```

配置上限8 KiB，要求完整且精确的顶层、五flags与四pilot字段；拒绝未知/缺失/重复（含Unicode转义同名键）、null、错误类型、尾随JSON、未知版本、full/aggressive及任一自治/敏感true。错误固定中文，不回显配置、个人值或token。Config/Ticket不能通过JSON传递或填充，未知feature不能capture/disable。

`Memory=basic`、`Inference=conservative`是**当前能力上限**；自治与敏感推断硬OFF。它们不表示已实现或启用了Memory/推断，也不构成正式Pilot配置、生产身份、获准组织或活动证据。

## 3. 当前配置与逐功能kill switch

Controller保存本进程配置及正revision，复制输入map避免调用者共享修改；内部RWMutex保护并发读取、替换和Disable。`Capture(feature)`获得不可JSON转移的Ticket，绑定同一controller、feature及当前revision；`Current(ticket)`要求这些绑定仍有效。它只说明开关状态，不签发工具grant。

`Replace(expectedRevision, config)`只用于可信服务端内部管理；CAS冲突、非法配置或全ON均原子拒绝，旧状态不变。`Disable(feature)`只收紧一个开关；重复关闭幂等，不提供一键启用。任何配置改变都会使旧Ticket失效，OFF→ON也不能复活旧响应。该revision仅是配置修订，不是UserProfile/Memory/source/consent revision、event sourceVersion、approval或effect key。

这不是持久运营控制台：没有配置表、HTTP管理员入口或热更新调度器。服务端配置重启后由环境重新加载；进程内可信调用者可持有Controller调用Disable/Replace。不能把本地并发测试称为已部署运营kill switch验收。

## 4. 真实适配与主API接入

[feature_gates.go](../../apps/api/internal/agentcognitive/feature_gates.go)构造真实FeatureGatedDomains，复用原CurrentDomainAdapter：OFF不调用Authenticate或store；ON仍执行既有当前会话、本人workspace、active Agent及源形状检查，领域读取后再次验证身份。返回前再核本次Ticket，配置改变/kill/cancel后丢弃已加载payload。核对点之后已在飞的数据不能承诺绝对撤回。

Memory两个port固定使用UnavailableCognitivePorts，不注入未来人类Memory store或伪实现；所有接受的31种flags组合均不产生认知Memory读取/写入。原DecideEligibility及社会推断许可合同保持原样，未补造Version=1、批准或授权facts。

[main.go](../../apps/api/main.go)在数据库/OIDC连接前严格加载配置，构造实际adapter及FeatureBoundary，绑定到真实HTTP handler。每请求通过私有context key携带typed服务端adapter，内部调用者可用FeatureDomainsFromContext取用；没有header/body可以选择flags/controller/store或注入server facts。**现有HTTP handler尚未作为新的enrichment消费者调用这些内部读方法**；本项安装真实配置边界并实现/实测内部适配，没有新增认知HTTP、模型运行器或用户富集界面。普通人类handler仍按原路由/资源权限执行，middleware不替代鉴权，也不封锁隐私管理。

## 5. 独立本地证据与限制

真实证据见[验证脚本](../../work/v5-age066/verify.ps1)、[round1机器结果](../../work/v5-age066/round1-result.json)、[round2机器结果](../../work/v5-age066/round2-result.json)、[首轮原始测试](../../work/v5-age066/round1-tests.jsonl)、[第二轮原始测试](../../work/v5-age066/round2-tests.jsonl)。两轮分别使用随机自有库，均由真实001–055及三个开发seed准备适配验证；本项无schema变化，不执行正式迁移或down。

| 实际检查 | 两轮均已核对的结果 |
| --- | --- |
| 五开关/Pilot/严格配置与并发CAS/逐功能kill | agentfeature 70 PASS事件 |
| 认知边界旧合同与实际gate适配 | agentcognitive 182 PASS事件，包含新真实PostgreSQL适配9事件 |
| 主API实际配置/adapter/context组合与旧main检查 | main 11 PASS事件，其中本项新组合10 |
| 隔离scope合计 | 每轮263 PASS，0 fail、0测试skip；计数含父测试和子场景 |
| 目标Go vet / 全API build | exit0；[vet](../../work/v5-age066/round1-vet.log)、[build](../../work/v5-age066/round1-build.log) |
| 当前源码编译真API启动 | 非法配置exit1，固定中文错误且无canary；[日志](../../work/v5-age066/round1-invalid-startup.log) |
| 所有public源完整行 | before/after SHA256相同，未豁免表/时间/版本或仅比数量；自有fixtures和数据库已清理 |

第二轮[vet原始日志](../../work/v5-age066/round2-vet.log)、[全API build日志](../../work/v5-age066/round2-build.log)、[真实非法配置启动日志](../../work/v5-age066/round2-invalid-startup.log)与[执行日志](../../work/v5-age066/round2-run.log)保留。每轮分别核对本轮原始完整行SHA；两轮随机fixture不同，不以跨库hash相同作为验收要求。

首轮staging无DB的目标单元检查通过但真实集成明确skip，记录在`work/v5-age066/staging-tests-round1.jsonl`，未作为集成验收；随后独立真实库scope全部执行无skip。Windows Go当前CGO_ENABLED=0且无gcc，未运行race detector；真实并发CAS、锁保护读取、途中kill与版本重放正负场景已执行，不将其冒充race instrumentation结果。

本项没有Flutter/UI变化，未运行新增中文开关界面、手机、地图/键盘或消费者验收。没有provider、Memory/敏感推断/真实自主效果或配置发布；IdP、CSSA授权/现实活动、HTTPS/生产地图、日志和值守、提醒调度及真实A→H仍缺实际发布证据。**Closed Pilot Ready：NO；Consumer Beta Ready：NO。** 最终live任务状态与全仓统一回归只由根代理记录。
