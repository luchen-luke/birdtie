import copy, hashlib, importlib.util, json, sys
from datetime import datetime, timezone
from pathlib import Path

R = Path(r'D:\Project\birdtie')
E = R / 'docs/testing/evidence/civu-native-adaptation-20261015'
S = R / 'docs/testing/evidence/social-remote-migration-20261015'
read = lambda p: json.loads(p.read_bytes())
sha = lambda p: hashlib.sha256(p.read_bytes()).hexdigest()
build, upgrade, video_apply, verified = sys.argv[1:]
api = read(S / build / 'receipt.json')
deployment = read(S / upgrade / 'receipt.json')
apk = read(S / 'final-apk07/receipt.json')
web = read(S / 'final-web07/receipt.json')
public = read(S / 'public-client07/receipt.json')
install = read(E / 'device-install04/receipt.json')
journey = read(E / 'native-journey-apply02/receipt.json')
video = read(E / video_apply / 'receipt.json')
real = read(E / verified / 'receipt.json')
baseline = read(Path(__file__).with_name('native159-baseline.json'))
assert all(sha(R / p) == h for p, h in baseline['migrations'].items())
assert api['source_stable'] and deployment['exit_code'] == 0
assert deployment['result']['migrations'] == 159
assert all(p['source_stable'] and p['effective_exit_code'] == 0 for p in [apk, web])
assert public['exitCode'] == 0 and install['installed']
assert journey['exitCode'] == video['exitCode'] == real['exitCode'] == 0
assert real['result']['status'] == 'PASS'
assert journey['result']['result']['counts']['journeys'] == 22
assert video['result']['result']['counts']['videos'] == 5
for proof in [api, apk, web]:
    assert all(sha(R / p) == h for p, h in proof['source_after'].items())
assert install['apkSHA256'] == apk['artifacts']['birdtie-social-core.apk']['sha256']
checkpoint = {
    'at': datetime.now(timezone.utc).isoformat(),
    'kind': 'NATIVE158_159_JOURNEY_VIDEO_ACTUAL_ADAPTATION_PARTIAL',
    'origin': 'https://47.110.154.133',
    'apiRelease': deployment['result']['release'],
    'apiSHA256': api['manifest']['bin/birdtie-api']['sha256'],
    'webRelease': public['result']['webRelease'],
    'webManifestSHA256': public['result']['newManifestSHA'],
    'apkSHA256': install['apkSHA256'],
    'installed': True, 'fullDeviceAcceptance': 'NOT_RUN', 'deviceLaunch': 'FAILED_DEVICE_DISCONNECTED_AFTER_INSTALL', 'schema': 159,
    'originalSQLMigrationsUnchanged': 157,
    'originalSQLFilesUnchanged': len(baseline['migrations']),
    'sourceWrites': 0,
    'nativeCounts': {'cities': 1067, 'places': 5245, 'moments': 83,
                     'themeMaps': 3, 'savedPlaces': 184, 'journeys': 22,
                     'journeyEntries': 110, 'privatePhotos': 135, 'privateVideos': 5},
    'journeyImport': journey['result'], 'videoImport': video['result'],
    'realOwnerRead': real['result'],
    'allRealVerificationSessionsLoggedOut': real['result']['allCreatedSessionsLoggedOut'],
    'newOwnerEnrolled': False, 'importedAgentsActivated': 0,
    'fullMigration': 'PARTIAL', 'fullNewMomentSpecification': 'PARTIAL',
    'fullSiteAccepted': False, 'normalFont': True, 'screenRecording': False,
}
path = E / 'checkpoint159.json'
assert not path.exists()
path.write_text(json.dumps(checkpoint, ensure_ascii=False, indent=2) + '\n')
(E / 'checkpoint.json').write_text(json.dumps(checkpoint, ensure_ascii=False, indent=2) + '\n')
report = R / 'docs/reports/BIRDTIE-CIVU-NATIVE-ADAPTATION-2026-10-15.md'
text = report.read_text()
text = text.replace('日期：2026-10-15（Asia/Shanghai）。', '最新实际交付：远端 schema159、22旅程／110行程项及5完整私人视频已迁入；APK07安装、Web07上线。完整新要求仍为PARTIAL，见末节。\n\n日期：2026-10-15（Asia/Shanghai）。', 1)
text = text.replace('| 旅程 | 34 份原档案保全，其中 22 份存续 | 原生行程的站点、日期及安排模型尚未适配；当前原生旅程数为 0 |',
                    '| 旅程 | 22 份存续旅程及 110 条原生行程项已迁入 | 12 份已删除旅程仅保留墓碑；原始来源与关联保留 |')
text = text.replace('| 视频 | 5 份已认证备份中的存续动态视频已识别 | 尚未导入原生私人视频；不截断以迁就现有限额 |',
                    '| 视频 | 5 段完整私人视频已迁入，原字节及兼容播放副本均保留 | 未伪造扫描或公开授权；真实主人界面播放另行验收 |')
text = text.replace('旧旅程详细安排、5 个私人视频、旧关系／消息／组织、邮箱身份与停止旧 writer 后的最终增量对账也仍有缺口。',
                    '旅程与视频的实际迁入见下方159接续；旧关系／消息／组织、邮箱身份与停止旧 writer 后的最终增量对账仍有缺口。')
text += f'''
## 158–159 旅程和完整视频实际交付

本节是上文157历史回执之后的最新接续。远端当前 schema159，原1–157的289份SQL文件摘要均未改动；API运行于 `{checkpoint['apiRelease']}`。APK07已安装，Web07已切流并通过默认证书的HTTPS与实际资源摘要校验。

22份存续旅程已成为原主人普通私人Journey，110条行程项完整包含24站点、85安排、1备注；日期、顺序、站点连线、交通、地址、来源文字和未决字段保留。新旧Journey共用原本人列表、详情及147编辑事务；具体版本批准覆盖完整行程。迁入没有用扁平地点列表替代详细日程，没有复活12份删除墓碑。客户端可改日期、安排、备注及排序；已有城市／地点稳定引用保留，直接更换这些引用的选择器尚未补齐。

5段MP4已实际提交到当前独立库。来源从原加密备份完整GCM认证后精确抽取，原文件合计88,823,138字节；原字节内部完整保留，播放使用H264兼容副本和AAC原音轨。3段超过30秒，全部按原时长迁移；未截短、缩放、裁切或降低帧数。逐文件核对完整解码、帧数、尺寸与方向、时长，以及全部AAC包数量和顺序摘要。兼容副本是有损编码，不宣称逐像素无损。病毒扫描仍NOT_RUN、隐私分类UNKNOWN，私人归属不变，不自动公开。

158和159均实际检查原回执，并重复调用确认返回同一不可变事实。重新执行不会覆盖后续编辑、复活删除的旅程／视频或重新填回已清除字节。所有原视频、播放副本和封面落库摘要及owner／来源关系已在远端聚合核验；原库没有写入，验证会话均已退出。

真实已接续账号读取：{real['result'].get('selectedOwnerNativeJourneys', 0)}份旅程、{real['result'].get('selectedOwnerItineraryEntries', 0)}条行程项；私人视频HTTP正向读取状态 `{real['result'].get('positiveOriginalVideoHTTP')}`。未借其他原账号身份做验收，没有新增原账号接续或激活Agent。数据库字节核对、隔离库测试和实际主人界面验收分别记录。

本批还修复串文两处残留门槛：选Place而未选City时仍按真实地点处理；纯媒体条目按完整媒体版本及shared localScope校验，不再被空正文误拦。沿原Now输入框和原子串文操作，不生成必填标题。

直接证据：`native08-journey-video`中的Journey PASS；`native10-video`中的视频导入、本人读取、回滚、跨owner拒绝、删除／退休与不复活PASS；`client-journey-video02`4项PASS；`client-thread-optional01`6项PASS；本地真实5视频`legacy-private-video/verify-blobs10` PASS，以及本次远端codec、实际导入和主人读取回执。APK07安装成功后设备断连，启动尝试失败已记录device-launch04；完整界面流程／设备视频播放和iOS仍NOT_RUN，不将编译安装当作全站验收。

失败记录保留：本地视频01–09暴露时间基准、色彩格式与输出上限兼容问题；没有接受部分视频。native08/09视频用例中全库active假设及两次取时问题已修正为原库基线和同一时点，未降低生产约束。旅程apply01缺运行库路径配置，停在校验且未写库；apply02成功。视频远端apply01/02停于服务器codec验证；Linux最初虚拟地址空间不足及退出时VmRSS缺失的监控误判均保留证据。实际采样峰值约132MiB；已改用固定statm常驻页口径，并通过128次Linux快速退出的针对性回归，512MiB监控阈值保持。后续实际结果以 `{video_apply}` 为准。

- API SHA-256：`{checkpoint['apiSHA256']}`。
- Web manifest SHA-256：`{checkpoint['webManifestSHA256']}`。
- APK SHA-256：`{checkpoint['apkSHA256']}`。
- 最新实际回执：`docs/testing/evidence/civu-native-adaptation-20261015/checkpoint159.json`。

整体仍未完成：Moment 0..N跨城地点／多地图点、失效位置独立退休而正文保持可读、外部地点稳定引用、剩余缺失媒体／未映射收藏、旧串文元数据、关系／消息／组织、2个仅邮箱身份及最终增量对账继续保留原任务。没有关闭原发布门槛，没有把本面板或本批导入称为全部交付。
'''
report.write_text(text)
matrix = R / 'docs/architecture/CIVU-REUSE-MATRIX.md'
matrix.write_text(matrix.read_text() + '''

2026-10-15 原生158/159继续：在用户既有迁入和部署授权内，Birdtie自有legacyjourney/legacyvideo已将22旅程及110条日程、5完整私人MP4迁入同一独立Birdtie库。仅转换原备份数据语义，没有复制Civu业务实现或旧schema。原owner／来源永久映射、12旅程墓碑、原回执保持不可变；视频原字节保留，兼容播放副本按完整帧／时长／AAC包回验，AV仍NOT_RUN。两阶段重复执行不会覆盖编辑或复活删除。日期／站点／安排／交通及原文沿普通Journey模型读取编辑，不另建运行时旧库。具体实际数量、原账号读取和缺口以原生适配报告158–159节及checkpoint159为准；缺失媒体、未映射数据、旧关系消息组织和最终增量仍未完成。
''')
spec = importlib.util.spec_from_file_location('taskctl', R / 'automation/taskctl.py')
t = importlib.util.module_from_spec(spec)
spec.loader.exec_module(t)
q = t.load(t.QUEUE)
u = copy.deepcopy(q)
parent = next(v for v in u['tasks'] if v['id'] == 'BT-FIX-NOW-UI-001')
assert parent['status'] == 'IN_PROGRESS'
event = {'at': checkpoint['at'], 'event': 'NATIVE158_159_JOURNEYS_FULL_PRIVATE_VIDEOS_ACTUALLY_IMPORTED',
         'owner': 'root', 'checkpoint': path.relative_to(R).as_posix(), 'checkpointSHA256': sha(path),
         'report': report.relative_to(R).as_posix(), 'nativeCounts': checkpoint['nativeCounts'],
         'apiRelease': checkpoint['apiRelease'], 'webRelease': checkpoint['webRelease'],
         'apkInstalled': True, 'fullMigration': 'PARTIAL', 'fullMomentRevision': 'PARTIAL',
         'fullSiteAccepted': False, 'newTasksCreated': 0, 'historicalStatusesChanged': 0}
parent.setdefault('history', []).append(event)
parent.setdefault('verification_history', []).append(dict(event, result='PARTIAL_ACTUAL_DEPLOYED'))
parent['remote_migration_execution_20261015']['last_actual_checkpoint'] = {
    k: event[k] for k in ['at', 'checkpoint', 'checkpointSHA256', 'report']}
parent['remote_migration_execution_20261015']['full_business_adaptation'] = 'PARTIAL'
t.commit(t.QUEUE, q, u, coordinator='root')
print(json.dumps({'status': 'CHECKPOINT_AND_REPORT_SAVED', 'apiRelease': checkpoint['apiRelease'],
                  'journeys': 22, 'privateVideos': 5, 'parentStatus': parent['status']}))
