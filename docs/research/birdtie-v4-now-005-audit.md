# BT-V4-NOW-005 审计、实施与验收

2026-10-04，owner sponsored_trust；仅使用原任务及 root 的精确 lease（next-parallel-plan36.json）。INT002/NOW001依赖实际 DONE，不重建需求或修改 live queue/common reports。

## 原差距与计划

原 goal：global/online social 不依赖 city map；AC 为 Online Intent 生成 friend/community/public opportunities、无需定位/map result 打开 chat/activity；verify cross-city E2E，CODE_AND_LOCAL_VERIFICATION。

原 NOW001 PUBLIC_ONLY AgentTask 只做公共 ONLINE 规则搜索，不能静默扩展为好友/Community 机器许可；原机会生成器只支持带公开 Place 的 IN_PERSON，不为 ONLINE 填假坐标。已有 social_intents、accepted Tie、Community、真正 ONLINE Activity 与原人类详情动作均可复用。新只读 ordinary-person adapter 完成原差距，保持模型/Memory/AgentTask 权限不变。

计划：最小闭合 native DTO →最终 SQL 当前权限/来源→本人只读 registered HTTP→独立中文可滚动入口→原 ID 当前动作→撤权/迟到/未知/重启实测→冻结证据交 root。未写 server/main/MapWorkspace/DDL，由 root 唯一接线；没有新 ledger。

## 实际失败与修复

- compile1：本线 unused import，保留后修。
- native1： sibling NOW006 进行中的 fixture f-shadow 编译故障；本线 pure 2 PASS，whole目标编译 FAIL，不称本线全部通过。
- native2：runner 未复制既有3 dev-seeds，18 FAIL，修独占 sourcecopy 输入，不改产品断言。
- native3：合成 Community 清理提前删除 active owner membership，18 FAIL；修顺序先删除本人 Intent 再 Community parent cascade，未放松 native guard。
- native4：21 PASS。但来源共用同一好友，无法独立证明三种路线；因此追加独立 Public/Community synthetic Person。
- native5：新增 accounts fixture 漏原 mandatory id，SQL23502，18 FAIL；修 gen_random_uuid 和真实 source creator 取消。不把 failure 改成跳过。
- native6：21 PASS独立三路线；继续补当前等待/两城/noGPS与实际进程。
- native7/native8：31 PASS；native8 子进程接受精确父 runner 已明确的 owned loopback DB，不把单个 task DB prefix 错当整个 Go 回归的必需名称。子进程只读，token 仅内存 env，非命令/日志字段。
- native9-community-deadline-red：真实产品 RED，非会员 PUBLIC Community Activity 投影 source deadline 只用了会员 joined 期限，遗漏公开 organizer Community 自身期限。追加实际 native Source.ExpiresAt/整 View.ValidUntil≤Community deadline +自然到期负例；首次还有新 Activity organizer FK 清理顺序失败，原日志均保留。只修本线 SQL least 加 activity_community.expires_at、owned fixture 先清 Activity 再 Community。native10 整目标32PASS，未放宽断言。
- client-target1/2：重复 DTO fixture List 泛型造成测试抛错，以及真实 endpoint 重绑后 AlertDialog 仍在；先修 fixture 泛型，再实际 useRootNavigator:false 收到内层 identity boundary，target3该负例已绿。
- client-target3：23 PASS/1真实 RED：有限来源到期后旧意图弹窗仍停留。修正在展开的具体 lease retirement，并保留原身份/未知结果规则。
- client-target4/5：24 PASS；analyze6 保留1条测试 braces info，修后 analyze7 六文件0 issue。
- 一次工作目录相对路径误指 apps/client/apps/api，Get/Set/gofmt 均失败，未创建产品文件；改仓库绝对路径，原工具输出保留。该失败不计测试。

## 实际可检查结果

native10 在新独占083+3devseed库执行 registered HTTP和 native truth source；32 PASS、0FAIL/SKIP/pkgFail、vet/build0、原 public全行不变、owned DROP真，839观察源前后SHA相同。6真实等待涵盖 pool-source ABA、pool-Agent ABA、table-source PRIVATE、table-membership撤回、Session撤销/自然到期。正例请求人零 person_contexts，两个不同City真正 ONLINE published Activity无Place，原 GetActivity同ID；公开 Community organizer 非会员的有限 source/View lease 及自然过期均有实际正负例。

HTTP两个独立子进程实际开启随机 loopback listener，退出后第二进程重开；双方都 GET原本人Intent opportunities及原Activity detail200，4真实requestID及稳定ID保留在tests.jsonl，非Token。不是 Store重建冒充进程重启。全部来源 LOCAL_SYNTHETIC；公共 ACL、人类本人主动读取、accepted request+Tie与会员真源均实际执行。

client-target5/analyze7覆盖闭合DTO/合法Go偏移/无定位、same-token account ABA、旧endpoint/dialog退休、源版本变化、403、匿名/组织零GET、空结果、320/font3/IME260/48dp、有限期旧弹窗清除、用户点击原Tie才一次POST且未知不重发。未在worker侧安装手机或运行外部服务。

## 剩余边界

这项不是全面多条件智能推荐：类别/标题规则匹配且UI明说，不推断对方兴趣、愿意私信、到访或出席。没有模型/机器处理许可或自动动作。原真实供给、真实跨城市真人、TalkBack、正式认证/部署均 NOT_RUN；Closed Pilot / Consumer Beta NO。任务最终状态和整仓核证由 root 决定。
