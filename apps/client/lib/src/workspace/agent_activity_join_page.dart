import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';
import 'package:http/http.dart' as http;

import '../app/birdtie_app_bar.dart';
import 'agent_activity_join_api.dart';
import 'agent_activity_join_controller.dart';
import 'agent_activity_join_pending_store.dart';
import 'now_ui_tokens.dart';

export 'agent_activity_join_api.dart' show AgentActivityJoinSelection;

class AgentActivityJoinPage extends StatefulWidget {
  const AgentActivityJoinPage({
    super.key,
    required this.authorizationHeader,
    required this.ownerID,
    this.organizationWorkspaceID,
    this.sourceCurrent,
    this.identityChanges,
    this.client,
    this.apiBaseUrl,
    this.selection,
    this.pendingStore,
    this.now,
    this.elapsed,
  });
  final String? Function() authorizationHeader, ownerID;
  final String? Function()? organizationWorkspaceID;
  final bool Function()? sourceCurrent;
  final Listenable? identityChanges;
  final http.Client? client;
  final String? apiBaseUrl;
  final AgentActivityJoinSelection? selection;
  final AgentActivityJoinPendingStore? pendingStore;
  final DateTime Function()? now;
  final Duration Function()? elapsed;
  @override
  State<AgentActivityJoinPage> createState() => _AgentActivityJoinPageState();
}

class _AgentActivityJoinPageState extends State<AgentActivityJoinPage>
    with WidgetsBindingObserver {
  late final _changes = widget.identityChanges;
  late final _data = AgentActivityJoinController(
    authorizationHeader: widget.authorizationHeader,
    ownerID: widget.ownerID,
    organizationWorkspaceID: widget.organizationWorkspaceID,
    sourceCurrent: widget.sourceCurrent,
    api: AgentActivityJoinAPI(
      client: widget.client,
      apiBaseUrl: widget.apiBaseUrl,
    ),
    selection: widget.selection,
    pendingStore: widget.pendingStore,
    now: widget.now,
    elapsed: widget.elapsed,
  );
  Timer? _timer;
  bool _queued = false;
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    _changes?.addListener(_identity);
    _data.addListener(_changed);
    unawaited(_load());
    _timer = Timer.periodic(const Duration(seconds: 1), (_) {
      if (!mounted) return;
      _data.identityChanged();
      _changed();
    });
  }

  Future<void> _load() async {
    await _data.initialize();
    if (mounted && _data.canPrepare) await _data.prepare();
  }

  void _identity() => _data.identityChanged();
  void _changed() {
    if (!mounted) return;
    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      if (!_queued) {
        _queued = true;
        WidgetsBinding.instance.addPostFrameCallback((_) {
          _queued = false;
          if (mounted) setState(() {});
        });
      }
    } else {
      setState(() {});
    }
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state != AppLifecycleState.resumed) {
      _data.retireApproval('应用已离开前台，本次批准已停用；原操作仍可核实。');
    } else {
      _data.identityChanged();
    }
  }

  @override
  void didUpdateWidget(covariant AgentActivityJoinPage oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (!identical(oldWidget.authorizationHeader, widget.authorizationHeader) ||
        !identical(oldWidget.ownerID, widget.ownerID) ||
        !identical(
          oldWidget.organizationWorkspaceID,
          widget.organizationWorkspaceID,
        ) ||
        !identical(oldWidget.sourceCurrent, widget.sourceCurrent) ||
        !identical(oldWidget.identityChanges, widget.identityChanges) ||
        !identical(oldWidget.client, widget.client) ||
        oldWidget.apiBaseUrl != widget.apiBaseUrl ||
        oldWidget.selection != widget.selection ||
        !identical(oldWidget.pendingStore, widget.pendingStore) ||
        !identical(oldWidget.now, widget.now) ||
        !identical(oldWidget.elapsed, widget.elapsed)) {
      _data.retireIdentity();
    } else {
      _data.identityChanged();
    }
  }

  @override
  void dispose() {
    _timer?.cancel();
    WidgetsBinding.instance.removeObserver(this);
    _changes?.removeListener(_identity);
    _data.removeListener(_changed);
    _data.dispose();
    super.dispose();
  }

  Widget _button(String label, VoidCallback? action, {bool primary = false}) {
    final child = Padding(
      padding: const EdgeInsets.symmetric(vertical: 10),
      child: Text(label, textAlign: TextAlign.center),
    );
    return Padding(
      padding: const EdgeInsets.only(top: 12),
      child: ConstrainedBox(
        constraints: const BoxConstraints(
          minHeight: 48,
          minWidth: double.infinity,
        ),
        child: primary
            ? FilledButton(onPressed: action, child: child)
            : OutlinedButton(onPressed: action, child: child),
      ),
    );
  }

  String _time(DateTime at) {
    // This timezone is explicit. Do not label a device-local conversion with
    // the Activity's timezone, or invent an unconfigured venue time conversion.
    final t = at.toUtc();
    return '${t.year}年${t.month}月${t.day}日 ${t.hour.toString().padLeft(2, '0')}:${t.minute.toString().padLeft(2, '0')} UTC';
  }

  Widget _fact(String label, String value) => Padding(
    padding: const EdgeInsets.only(top: 8),
    child: Text('$label：$value', style: NowUiTokens.of(context).body),
  );
  List<Widget> _review(AgentActivityJoinReview r) {
    final role = switch (r.organizerType) {
      'ORGANIZATION' => '组织',
      'BUSINESS' => '商家',
      'COMMUNITY' => '社群',
      _ => '个人主办方',
    };
    final modality = switch (r.modality) {
      'in_person' => '线下',
      'online' => '线上',
      'hybrid' => '线上与线下',
      _ => '形式未注明',
    };
    final location =
        r.locationLabel ??
        switch (r.raw['physicalPlaceStatus']) {
          'tbd' => '实体地点待定',
          'not_applicable' => '无需实体地点',
          _ => '原活动未注明地点',
        };
    // Original Activity prices use hundredths. Unknown/native NULL is never
    // converted to zero or shown as free. This action does not make a payment.
    final price = r.priceMinor == null
        ? '原活动未明确费用'
        : '${r.currency} ${(r.priceMinor! / 100).toStringAsFixed(2)}';
    return [
      Text(r.title, style: NowUiTokens.of(context).pageTitle),
      if (r.summary.isNotEmpty)
        Padding(
          padding: const EdgeInsets.only(top: 12),
          child: Text(r.summary, style: NowUiTokens.of(context).body),
        ),
      if (r.description.isNotEmpty)
        Padding(
          padding: const EdgeInsets.only(top: 12),
          child: Text(r.description, style: NowUiTokens.of(context).body),
        ),
      const SizedBox(height: 12),
      _fact('主办方', r.organizerName ?? r.hostLabel ?? '$role（主办方名称未提供）'),
      _fact('开始', _time(r.startsAt)),
      _fact('结束', _time(r.endsAt)),
      _fact('活动注明时区', r.timeZone),
      _fact('形式', modality),
      _fact('地点', location),
      _fact('费用', price),
      _fact('报名条件', r.eligibility ?? '原活动未注明额外条件'),
      _fact(
        '名额',
        r.capacity == null
            ? '原活动未注明人数上限'
            : '人数上限 ${r.capacity}；${r.participantCount == null ? '当前人数未提供' : '当前 ${r.participantCount} 人'}',
      ),
      const SizedBox(height: 20),
      Text(
        '确认后，智能体将以你本人身份向这个活动提交一次报名，写入原活动报名记录。报名会受当前名额和条件检查；这一步不会付款。',
        style: NowUiTokens.of(context).body,
      ),
      _fact('报名记录', '按原活动报名规则可由活动管理方查看；不代表已到场或参加完活动'),
      const SizedBox(height: 8),
      Semantics(
        liveRegion: true,
        child: Text(
          '本次预览剩余 ${_data.reviewRemaining.inSeconds} 秒；返回、后台或来源变化后，本次批准停用。',
          style: NowUiTokens.of(context).metadata,
        ),
      ),
      _button(
        '确认并让智能体报名',
        _data.canConfirm ? () => unawaited(_data.confirmAndExecute()) : null,
        primary: true,
      ),
      _button('结束本次批准', _data.canStop ? () => unawaited(_data.stop()) : null),
    ];
  }

  @override
  Widget build(BuildContext context) {
    final review = _data.preview?.review;
    return Scaffold(
      appBar: birdtieAppBar(context, title: const Text('智能体报名')),
      body: SafeArea(
        child: Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 680),
            child: SingleChildScrollView(
              padding: const EdgeInsets.all(16),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  Text(
                    '本人报名 · 一次具体活动',
                    style: NowUiTokens.of(context).metadata,
                  ),
                  if (_data.busy)
                    const Padding(
                      padding: EdgeInsets.symmetric(vertical: 20),
                      child: LinearProgressIndicator(),
                    ),
                  if (_data.message != null)
                    Padding(
                      padding: const EdgeInsets.symmetric(vertical: 12),
                      child: Semantics(
                        liveRegion: true,
                        child: Text(
                          _data.message!,
                          style: NowUiTokens.of(context).body,
                        ),
                      ),
                    ),
                  if (review != null &&
                      !_data.writeRetired &&
                      !_data.identityRetired)
                    ..._review(review)
                  else if (_data.hasPending) ...[
                    Text('核实原报名', style: NowUiTokens.of(context).sheetTitle),
                    const SizedBox(height: 8),
                    Text(
                      '只核实这次原批准的历史结果。停止批准不会取消已经产生的报名；已有提交将继续核实原回执。',
                      style: NowUiTokens.of(context).body,
                    ),
                    _button(
                      '核实原报名操作',
                      _data.canRecover
                          ? () => unawaited(_data.reconcile())
                          : null,
                      primary: true,
                    ),
                    _button(
                      '结束并核实原批准',
                      _data.canStop ? () => unawaited(_data.stop()) : null,
                    ),
                  ] else if (_data.canPrepare) ...[
                    _button(
                      '重新读取报名预览',
                      () => unawaited(_data.prepare()),
                      primary: true,
                    ),
                  ] else if (_data.unknown &&
                      !_data.identityRetired &&
                      !_data.busy) ...[
                    _button(
                      '重试读取本机核实记录',
                      () => unawaited(_data.reloadJournal()),
                    ),
                  ],
                  const SizedBox(height: 16),
                  _button('返回', () => Navigator.of(context).maybePop()),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}
