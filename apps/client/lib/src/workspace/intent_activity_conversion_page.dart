import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';
import 'package:http/http.dart' as http;
import '../auth/birdtie_auth_controller.dart';
import 'active_social_intent_api.dart';
import 'active_social_intent_controller.dart' show ActiveIntentIdentity;
import 'activity_participations.dart' show plansLocation, plansSchedule;
import 'intent_activity_conversion_api.dart';
import 'intent_activity_conversion_controller.dart';
import 'chat_entity_router.dart';
import 'agent_memory_candidate_api.dart' show candidateCategories;

class IntentActivityConversionPage extends StatefulWidget {
  const IntentActivityConversionPage({
    super.key,
    required this.auth,
    required this.intentID,
    this.client,
    this.apiBaseUrl,
    this.workspaceChanges,
    this.organizationWorkspaceID,
  });
  final BirdtieAuthController auth;
  final String intentID;
  final http.Client? client;
  final String? apiBaseUrl;
  final Listenable? workspaceChanges;
  final String? Function()? organizationWorkspaceID;
  @override
  State<IntentActivityConversionPage> createState() => _ConversionPageState();
}

class _ConversionPageState extends State<IntentActivityConversionPage> {
  IntentActivityConversionController _create() =>
      IntentActivityConversionController(
        api: IntentActivityConversionAPI(
          client: widget.client,
          apiBaseUrl: widget.apiBaseUrl,
        ),
        identity: () => ActiveIntentIdentity(
          widget.auth.authorizationHeader,
          widget.auth.accountID,
          widget.organizationWorkspaceID?.call(),
        ),
        intentID: widget.intentID,
      );
  late IntentActivityConversionController _data = _create();
  Timer? _timer;
  Route<dynamic>? _reviewRoute;
  Route<dynamic>? _detailRoute;
  NavigatorState? _navigator;
  @override
  void initState() {
    super.initState();
    widget.auth.addListener(_identity);
    widget.workspaceChanges?.addListener(_identity);
    _data.addListener(_changed);
    unawaited(_data.load());
    _timer = Timer.periodic(const Duration(seconds: 1), (_) {
      if (!mounted) return;
      _data.expire();
      if (!_data.current || _data.review == null) _closeReview();
    });
  }

  void _changed() {
    if (!mounted) return;
    if (!_data.current || _data.review == null) _closeReview();
    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) setState(() {});
      });
    } else {
      setState(() {});
    }
  }

  void _identity() {
    _data.sync();
    if (!_data.current) _closeDetail();
    if (!_data.current) _closeReview();
  }

  void _closeReview({bool deferred = false}) {
    final r = _reviewRoute, nav = _navigator;
    _reviewRoute = null;
    void close() {
      if (nav?.mounted == true && r?.isActive == true) nav!.removeRoute(r!);
    }

    if (deferred ||
        SchedulerBinding.instance.schedulerPhase ==
            SchedulerPhase.persistentCallbacks) {
      WidgetsBinding.instance.addPostFrameCallback((_) => close());
    } else {
      close();
    }
  }

  @override
  void didUpdateWidget(IntentActivityConversionPage old) {
    super.didUpdateWidget(old);
    if (old.auth != widget.auth) {
      old.auth.removeListener(_identity);
      widget.auth.addListener(_identity);
    }
    if (old.workspaceChanges != widget.workspaceChanges) {
      old.workspaceChanges?.removeListener(_identity);
      widget.workspaceChanges?.addListener(_identity);
    }
    if (old.auth != widget.auth ||
        old.client != widget.client ||
        old.apiBaseUrl != widget.apiBaseUrl ||
        old.workspaceChanges != widget.workspaceChanges ||
        old.organizationWorkspaceID != widget.organizationWorkspaceID ||
        old.intentID != widget.intentID) {
      _closeReview();
      _closeDetail();
      _data.removeListener(_changed);
      _data.dispose();
      _data = _create();
      _data.addListener(_changed);
      final c = _data;
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted && identical(c, _data)) unawaited(c.load());
      });
    } else {
      _identity();
    }
  }

  Future<void> _prepare(ConversionChoice choice) async {
    final c = _data;
    final p = await c.prepare(choice);
    if (!mounted || !identical(c, _data) || p == null) return;
    final epoch = c.generation;
    if (!c.reviewCurrent(p, epoch)) return;
    final nav = Navigator.of(context);
    _navigator = nav;
    final route = DialogRoute<bool>(
      context: context,
      builder: (context) => AlertDialog(
        scrollable: true,
        title: const Text('检查这次意图关联'),
        content: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text('本人账号：${widget.auth.displayName ?? 'Birdtie 成员'}'),
              Text('原意图：${p.intent.title}'),
              Text('类型：${intentTypes[p.intent.value['type']]}'),
              Text('原受众：${intentAudiences[p.intent.value['audience']]}（保持）'),
              Text('明确约束：${_constraints(p.intent)}'),
              const SizedBox(height: 12),
              Text('原已报名活动：${p.choice.activity.title}'),
              Text(_schedule(p.choice)),
              Text(_location(p.choice)),
              const Text('报名状态：已报名；不代表实际到场'),
              const SizedBox(height: 12),
              Text(p.explanation),
              Text('具体预览截止：${_deviceTime(p.expiresAt)}'),
              const Text('批准后意图完成寻找，关联原报名；不新增报名、提醒、邀请、公开信息或模型授权。'),
            ],
          ),
        ),
        actions: [
          TextButton(
            style: TextButton.styleFrom(minimumSize: const Size(48, 48)),
            onPressed: () => Navigator.pop(context, false),
            child: const Text('取消'),
          ),
          FilledButton(
            style: FilledButton.styleFrom(minimumSize: const Size(48, 48)),
            onPressed: () => Navigator.pop(context, true),
            child: const Text('确认关联'),
          ),
        ],
      ),
    );
    _reviewRoute = route;
    final ok = await nav.push(route);
    if (identical(_reviewRoute, route)) _reviewRoute = null;
    if (!mounted || !identical(c, _data)) return;
    if (ok == true && c.reviewCurrent(p, epoch)) {
      await c.approve(p, epoch);
    } else {
      c.discard();
    }
  }

  String _schedule(ConversionChoice c) => plansSchedule(
    c.activity.startsAt,
    c.activity.endsAt,
    c.activity.timeZone,
  );
  String _location(ConversionChoice c) => plansLocation(
    c.activity.modality,
    c.activity.physicalPlaceStatus,
    c.activity.placeName,
  );
  String _deviceTime(DateTime v) {
    final t = v.toLocal();
    final offset = t.timeZoneOffset;
    return '${t.year}年${t.month}月${t.day}日 ${t.hour.toString().padLeft(2, '0')}:${t.minute.toString().padLeft(2, '0')}:${t.second.toString().padLeft(2, '0')}（你的设备时间 UTC${offset.isNegative ? '-' : '+'}${offset.inHours.abs().toString().padLeft(2, '0')}:${(offset.inMinutes.abs() % 60).toString().padLeft(2, '0')}）';
  }

  String _constraints(ConversionIntent i) {
    final m = intentMap(i.value['constraints']);
    return m.isEmpty
        ? '未额外限定类别、人数、平台或具体活动时段'
        : m.entries
              .map((e) {
                final k = {
                  'category': '类别',
                  'placeId': '已选公开地点',
                  'areaLabel': '粗区域',
                  'onlinePlatform': '线上平台',
                  'minParticipants': '人数下限',
                  'maxParticipants': '人数上限',
                  'startsAt': '开始时段',
                  'endsAt': '结束时段',
                }[e.key];
                return '$k：${e.key == 'startsAt' || e.key == 'endsAt'
                    ? _deviceTime(DateTime.parse(e.value))
                    : e.key == 'placeId'
                    ? '按原已选地点核验'
                    : e.key == 'category'
                    ? candidateCategories[e.value] ?? '已选分类（${e.value}）'
                    : e.value}';
              })
              .join('；');
  }

  void _closeDetail({bool deferred = false}) {
    final r = _detailRoute, nav = _navigator;
    _detailRoute = null;
    void close() {
      if (nav?.mounted == true && r?.isActive == true) nav!.removeRoute(r!);
    }

    if (deferred ||
        SchedulerBinding.instance.schedulerPhase ==
            SchedulerPhase.persistentCallbacks) {
      WidgetsBinding.instance.addPostFrameCallback((_) => close());
    } else {
      close();
    }
  }

  Future<void> _openActivity() async {
    final c = _data, epoch = c.generation;
    final id =
        c.receipt?.activityID ?? c.options?.intent.value['convertedActivityId'];
    if (!c.ready || id is! String) return;
    final nav = Navigator.of(context);
    _navigator = nav;
    final client = _ConversionDetailClient(
      c.api.detailClient,
      () =>
          mounted && identical(c, _data) && c.current && c.generation == epoch,
    );
    final route = MaterialPageRoute<void>(
      builder: (_) => ChatEntityDetail(
        type: 'activity',
        id: id,
        auth: widget.auth,
        client: client,
        apiBaseUrl: c.api.base,
        workspaceChanges: widget.workspaceChanges,
        workspaceID: widget.organizationWorkspaceID,
      ),
    );
    _detailRoute = route;
    try {
      await nav.push(route);
    } finally {
      if (identical(_detailRoute, route)) _detailRoute = null;
      client.close();
    }
  }

  @override
  void dispose() {
    widget.auth.removeListener(_identity);
    widget.workspaceChanges?.removeListener(_identity);
    _timer?.cancel();
    _data.removeListener(_changed);
    _data.dispose();
    _closeReview(deferred: true);
    _closeDetail(deferred: true);
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final o = _data.options, r = _data.receipt;
    return Scaffold(
      appBar: AppBar(title: const Text('关联已报名活动')),
      body: SafeArea(
        child: ListView(
          padding: const EdgeInsets.all(16),
          children: [
            const Text('仅本人明确选择已报名活动，检查当前来源后完成这条寻找活动意图。普通报名和私人提醒仍可独立使用。'),
            const SizedBox(height: 12),
            if (!_data.current)
              const Text('请在当前个人账号重新打开；组织工作区不批准本人意图。')
            else ...[
              if (_data.busy) const LinearProgressIndicator(),
              if (_data.message != null)
                Text(_data.message!, semanticsLabel: _data.message),
              if (r != null) ...[
                Text('已完成：${r.intent.title}'),
                const Text('已关联之前报名的活动，不会重复报名或扩大公开范围。'),
              ],
              if (o != null) ...[
                Text(
                  o.intent.title,
                  style: Theme.of(context).textTheme.titleLarge,
                ),
                Text('当前：${intentStates[o.intent.status]}'),
                Text('受众：${intentAudiences[o.intent.value['audience']]}（不扩大）'),
                if (o.intent.status == 'CONVERTED')
                  Text(
                    o.intent.linked
                        ? '这条意图已有原活动关联，可在计划查看原报名。'
                        : '旧记录仅有完成状态，没有可核验关联；不补造活动。',
                  )
                else if (!['ACTIVE', 'MATCHED'].contains(o.intent.status) ||
                    o.intent.value['type'] != 'FIND_ACTIVITY')
                  const Text('当前类型或状态不支持关联，请回到原意图管理。')
                else ...[
                  Text(o.explanation),
                  if (o.choices.isEmpty)
                    const Text('没有满足明确约束的当前已报名活动；人数、平台或粗区域无法核实时不会猜测。'),
                  for (final choice in o.choices)
                    Padding(
                      padding: const EdgeInsets.symmetric(vertical: 8),
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            choice.activity.title,
                            style: Theme.of(context).textTheme.titleMedium,
                          ),
                          Text(_schedule(choice)),
                          Text(_location(choice)),
                          const Text('已报名；是否到场未知'),
                          OutlinedButton(
                            style: OutlinedButton.styleFrom(
                              minimumSize: const Size(48, 48),
                            ),
                            onPressed: _data.ready
                                ? () => _prepare(choice)
                                : null,
                            child: const Text('检查关联此活动'),
                          ),
                        ],
                      ),
                    ),
                  if (o.truncated) const Text('只显示当前最多 100 条已报名来源，并非完整活动列表。'),
                ],
              ],
              if (_data.completedChoice != null) ...[
                Text(_data.completedChoice!.activity.title),
                Text(_schedule(_data.completedChoice!)),
                Text(_location(_data.completedChoice!)),
              ],
              if (r != null || o?.intent.linked == true)
                OutlinedButton(
                  style: OutlinedButton.styleFrom(
                    minimumSize: const Size(48, 48),
                  ),
                  onPressed: _data.ready ? _openActivity : null,
                  child: const Text("查看原活动详情"),
                ),
              const SizedBox(height: 12),
              TextButton(
                style: TextButton.styleFrom(minimumSize: const Size(48, 48)),
                onPressed: _data.current && !_data.busy ? _data.load : null,
                child: Text(_data.unknown ? '读取原意图核对' : '刷新当前状态'),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

// Bound nested detail transport never closes the borrowed HTTP client. It
// buffers the response before releasing it so an old page/transport frame
// cannot restore private data after an A -> B -> A retirement.
class _ConversionDetailClient extends http.BaseClient {
  _ConversionDetailClient(this.base, this.valid);
  final http.Client base;
  final bool Function() valid;
  bool _closed = false;
  @override
  Future<http.StreamedResponse> send(http.BaseRequest request) async {
    if (_closed || !valid()) throw StateError('当前入口已失效');
    final response = await base.send(request);
    final bytes = await response.stream.toBytes();
    if (_closed || !valid()) throw StateError('当前入口已失效');
    return http.StreamedResponse(
      Stream.value(bytes),
      response.statusCode,
      headers: response.headers,
      contentLength: bytes.length,
      request: response.request,
      isRedirect: response.isRedirect,
      persistentConnection: response.persistentConnection,
      reasonPhrase: response.reasonPhrase,
    );
  }

  @override
  void close() {
    _closed = true;
  }
}
