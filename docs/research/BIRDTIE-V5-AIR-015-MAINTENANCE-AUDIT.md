# AIR015 单次控制维护接续审计

2026-10-04。根显式恢复原 PARTIAL 任务，原对象与 prior_partial_records 保留；AIR014 和 INT001 实际 DONE。owner sponsored_trust，精确范围见 `work/v5-age038-resume/air015-maintenance-lease.json`。只新增独占维护包/命令及单一原生测试文件，复用064真源；不写DDL、server、live队列或共用报告。

## 真实缺口与原验收边界

原 `postgres/agent_outbox.go` 已有可信内部 `ClaimAgentOutboxControlForSubject` 和 `ConsumeAgentOutboxControl`，moment三writer事务capture、持久inbox/fence/lease/过期与恢复及100次重复均有实际原生验证。但库外没有维护命令/服务调用这些方法；这次补一个可实际运行的、有界本地维护入口，不新增候选消费者/授权，也不把它称自动富集。

原064只允许MOMENT来源和三闭集事件；effect ledger INSERT/UPDATE保留SQLSTATE55000。扩大参与/收藏等hook会涉及共享DDL，不在当前077并行窗口进行。AIR010/AIR027分别仍缺真正Run预算/逐次原生准入和Run历史，不能为推进它们造影子Run或绕依赖。

## 实施计划与原冻结记录

1. 单次维护包严格校验Person selector、worker、handler、batch/deadline；复用原Store，claim和receipt不可从JSON导入。
2. CLI只接受明确隔离本地开发操作，loopback primary/fallback地址；原数据库环境值不进入argv/log。没有daemon、HTTP、scheduler、跨网模型或机器grant。
3. Claim非确切无工作ErrNotFound错误即停止，空ErrUnavailable不是成功。Consume只有结构与原lease逐字段匹配的白名单终态receipt及原预期ErrUnavailable可计控制结果；commit未知/空/foreign/矛盾返回停止，不重发。
4. aggregate输出只含闭集阶段/结果/计数，输入或底层rawerror不出日志；不输出ID、claim、正文、DSN、token。
5. 专属fresh077原生夹具用原Moment writer真实capture，验证100次独立运行/两worker/领取退出重开pool/旧fence/当前源变化/过期/失败和零effect；实际命令子进程接线与私密canary检查，全目录vet/build和冻结SHA，根核证全量。

初次审计时根077全量正在冻结，只有计划和专属work staged文件；根实际全量9328 PASS结束后已明确放行登记范围，现已落五个Go源。旧冻结说明属于历史，不表示当前仍未实施。没有修改原outbox SQL、DDL或授权真源。

## 实际实现与首轮检查

- 新 `internal/agentoutboxmaintenance/runner.go` 负责闭集校验、bounded循环和确认回执计数；结构核验不替代原Store当前权限/源复核。
- 新 `cmd/agent-outbox-control/main.go` 实际连接本地PG，并调用原Claim/Consume，不是接口壳子。数据库地址与fallback/dial均拒远程，flag/rawerror不打印。
- runner与command各一unit测试文件；新 `postgres/agent_outbox_runner_integration_test.go` 复用原fixture和writer，没有改旧helpers或原授权。
- `compile1`/`compile2` 实际exit0仅算编译；首轮pure1实际exit0。
- fresh077 `native1` 实际91 Test PASS / 0 FAIL / 0 SKIP / 0 packageFail，CLI build0，741源SHA前后相同、所有public完整行相同、自有库已DROP。这是新增命令目标检查，不是全仓/生产/效果证明。
- native1实测100次独立CLI（首次1，后续99次0）、两CLI唯一4控制回执、真实子进程领取后退出→自然30秒lease到期→新CLI恢复及旧fence拒绝、源编辑后空Unavailable停止、提交后故障注入丢回执不重试、自然source到期及pool等待截止。辅助子进程测试无模式正常返回仅是测试驱动，不作为能力证明；故障丢回执是明确模拟，不冒称真实网络故障。
- 首轮没有产品测试失败。早期read错误采用不存在的docker-compose.yml及Windows rg wildcard产生查找错误，已按真实compose.yaml/目录glob读取；这类未执行测试不计通过。
- 根review要求补回执更新时间必须严格小于lease/source截止；实际已补equal/future负例、历史createdAt正例及收到确认后取消。又补原生Claim后修改源→Consume当前复核INVALIDATED及v1/v2重复不新增回执。

五Go已冻结于 `work/v5-air015-maintenance/source-freeze1.json`。fresh077 native2实际139 PASS/0 FAIL-SKIP/pkgFail，command/test/vet/build0，741全源稳定、public完整行与catalog相同、ownedDB已DROP。其准确范围是maintenance/command/postgres三包的 `^TestAgentOutbox`，没有原agentoutbox纯领域包；不能将本轮139表述为原230纯领域全部已复验。native2的scope标签沿用旧模板带HTTP，实际无HTTP能力，原始记录保留。正在native3纳入原领域包，同一冻结源码复验，最终结果另追加。

最终 `native3` 已真实结束：fresh077四包含原领域230，369 Test PASS /0FAIL-SKIP/pkgFail，command/test/vet/build0；741全API源稳定、公有完整行和catalog保持、自有库DROP。实际命令二进制SHA `9a3cedf1c7ff5c6032e49ffc7b151e6ba74ff7ccc6e70086ff04a55b975a6022`，原五Go未改。新增三轮均无产品测试失败，初期查找错误和compile-only如上保留。worker只提交局部冻结证据，原PARTIAL与根整仓核证保持分离。

UX-CHECK-06/08/09/10/16适用边界：不编造许可/源当前性、借原权威领域控制、不重试未知提交、按typed本人/exactAgent/fence分隔、输出不携私密正文。无新增用户UI，移动/AT/真机不适用本CLI切片且本批NOT_RUN，不能抵扣原产品门槛。

## 完成分类

完整AIR015继续PARTIAL：真正分析目的/CandidateSubmitter、原子非零候选/效果、全部来源hook、部署消费者/调度器仍缺。控制receipt可恢复不等于一个业务效果，也不宣称网络exactly-once。没有新界面，中文产品路径不变；运营/生产、真机、TalkBack与自动认知不因本切片获得验收。Closed Pilot/Consumer Beta NO。
