# AGE019 本人资料入口绑定接续审计

2026-10-04；唯一任务 BT-V5-AGE-019，owner sponsored_trust。原 PARTIAL 全对象保留在 `work/v5-age038-resume/original-age019-binding-partial.json` 和队列 prior_partial_records。015、068、INT001 实际全部 DONE。准确九 scope 见 `work/v5-age038-resume/age019-binding-lease.json`；worker不写Settings/Map、live队列、共用报告或Go/DDL。

## 已有能力与真实剩余项

初始分组询问、Now/Settings逐组补齐、原子SAVE、source CAS、409重审、未知结果GET核实已有实现与旧实际本地真机/重启证据，不重新建表单或后端。原自动自然资料富集缺具体分析目的、合法来源和真正消费；033的TASK_CONTEXT_READ是另一具体只读许可，007人工假设候选不授自动推断或Profile写入。完整019继续PARTIAL，Closed Pilot/Consumer Beta均NO。

独立可修复的实际缺陷是SeedSheet没有didUpdateWidget：controller固定旧client/base，其凭据getter却动态读取widget.auth，且只监听init时的auth/workspace，dispose反而从当前widget对象移除监听。同key换配置后，旧私密源继续显示，重试可将新主体token送往旧endpoint。根持有共享入口的captured epoch/nested boundary，worker仅补组件自身生命周期。

## 计划与实现

1. 同key旧503页改为新auth/client/base，点击现有重试，验证旧endpoint绝不能收到新token；另检查加载过的私密昵称即时清除。两模式加旧私密源共三实际RED。
2. init绑定真实原auth/监听/getter。配置重绑时永久退役当前State，清空草稿/正文/unknown/批准，解绑旧对象并dispose旧controller；保留普通原auth身份通知的清空与当前GET合同。
3. controller关闭幂等，关闭后identity同步没有状态重建/通知。借用transport不close，自有transport只close一次；不新增网络或授权路径。
4. 原初始/渐进Save、取消、原CITY私密后果、409/403/5xx未知逻辑回归；再补监听单项变化、A-B-A、晚GET/PUT、重新进入先GET、语义返回和窄屏大字。

## 实际检查与失败

- 首次启动目录误设在client cwd，误建空 `apps/client/work/v5-age019-binding`；log重定向不存在，因此Flutter未启动，不计测试。自动审批拒绝移除空目录操作（blocked by policy）；保留，不绕过。后续所有证据使用repo绝对路径。
- `red1.log`：testExit1，0PASS/3FAIL，真实旧endpoint/新token与旧私密昵称问题。
- `green1.log`：17PASS/exit0，修复及原流程回归。
- `green2.log`：27PASS/exit0，补单项监听/client/base/getter/mode、owned/borrowed、晚PUT、未知隔离。
- `green3.log`：31PASS/exit0，补渐进晚200/503、未知重新进入、320px/3x文字/240px keyboard/真实48dp语义返回。
- `target-final1.log`：43PASS/exit0。`analyze-final1.log`：exit1，三项test直接访问protected hasListeners警告；以test ValueNotifier子类公开真实监听检查，未改生产权限或断言。
- `analyze-final2.log`：五文件零问题/exit0。`target-final2.log`：同最终五SHA，六test文件43功能PASS/exit0；`source-freeze2.json`→`source-after2.json` 五源完全稳定。

目标命令（在apps/client执行）：

```powershell
flutter test test/agent_seed_sheet_test.dart test/agent_seed_controller_test.dart test/profile_completion_sheet_test.dart test/profile_completion_choice_test.dart test/social_preference_seed_controller_test.dart test/social_preference_seed_sheet_test.dart --reporter expanded
flutter analyze lib/src/content/agent_seed_sheet.dart lib/src/content/agent_seed_controller.dart test/agent_seed_sheet_test.dart test/agent_seed_controller_test.dart test/profile_completion_sheet_test.dart
```

实际MockClient和widget是offline合同，token均合成测试值。没有跑变化中077的全Go，没有新增native身份/授权验收；root负责同帧全Flutter/build/phone。TalkBack、真机新页面、真实IdP、自动富集、生产发布本worker均NOT_RUN。历史019旧原生/真机、9299已冻结完整Go属独立历史证据，不能代本新UI切片。
