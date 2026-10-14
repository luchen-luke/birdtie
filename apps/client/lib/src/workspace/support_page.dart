import 'dart:async';
import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import 'package:uuid/uuid.dart';
import '../app/birdtie_app_bar.dart';
import '../config/birdtie_environment.dart';
import 'human_causal_control.dart';
import 'human_causal_pending_store.dart';
import 'human_causal_operation_controller.dart';

Widget humanSupportDestination({
  required String? Function() authorizationHeader,
  String? Function()? ownerID,
  Listenable? identityChanges,
  bool Function()? sourceCurrent,
  required Object? Function() sourceKey,
  String targetType = 'general',
  String? targetID,
  String? apiBaseUrl,
  http.Client? client,
}) => HumanCausalEntryBoundary(
  ownerID: ownerID ?? () => null,
  authorizationHeader: authorizationHeader,
  sourceKey: sourceKey,
  identityChanges: identityChanges,
  sourceCurrent: sourceCurrent,
  builder: (_, live) => SupportPage(
    authorizationHeader: authorizationHeader,
    ownerID: ownerID,
    identityChanges: identityChanges,
    sourceCurrent: live,
    targetType: targetType,
    targetID: targetID,
    apiBaseUrl: apiBaseUrl,
    client: client,
  ),
);

class SupportPage extends StatefulWidget {
  const SupportPage({
    super.key,
    required this.authorizationHeader,
    this.ownerID,
    this.sourceCurrent,
    this.identityChanges,
    this.targetType = 'general',
    this.targetID,
    this.apiBaseUrl,
    this.client,
    this.pendingStore,
  });
  final String? Function() authorizationHeader;
  final String? Function()? ownerID;
  final bool Function()? sourceCurrent;
  final Listenable? identityChanges;
  final String targetType;
  final String? targetID, apiBaseUrl;
  final http.Client? client;
  final HumanCausalPendingStore? pendingStore;
  @override
  State<SupportPage> createState() => _SupportPageState();
}

class _SupportPageState extends State<SupportPage> {
  late http.Client _client;
  late bool _ownsClient;
  late HumanCausalOperationController _data;
  final _details = TextEditingController();
  String _reason = 'safety';
  int _read = 0;
  List<Map<String, dynamic>> _reports = const [];
  bool _reportsLoading = false;
  String? _reportsError;
  HumanCausalReceipt? _lastReceipt;
  void _bind() {
    _client = widget.client ?? http.Client();
    _ownsClient = widget.client == null;
    _data = HumanCausalOperationController(
      api: HumanCausalApi(
        client: _client,
        baseUrl: widget.apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl,
        authorizationHeader: () => widget.authorizationHeader(),
      ),
      family: HumanCausalFamily.report,
      targetID: widget.targetID ?? '',
      reportTargetType: widget.targetType,
      ownerID: widget.ownerID == null ? null : () => widget.ownerID!(),
      identityChanges: widget.identityChanges,
      sourceCurrent: () => mounted && (widget.sourceCurrent?.call() ?? true),
      targetKey: () => (
        widget.targetType,
        widget.targetID,
        widget.client,
        widget.apiBaseUrl,
      ),
      pendingStore: widget.pendingStore,
    );
    _data.addListener(_changed);
    final c = _data;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted && identical(_data, c)) unawaited(_load());
    });
  }

  void _changed() {
    if (!mounted) return;
    if (!_data.current) {
      _details.clear();
      _reports = const [];
      ++_read;
    } else {
      final r = _data.receipt;
      if (r != null && !identical(r, _lastReceipt)) {
        _lastReceipt = r;
        if (r.status == 'SUCCEEDED' &&
            HumanReportInput(
                  operationID: r.selector.operationID,
                  targetType: widget.targetType,
                  targetID: widget.targetID ?? '',
                  reason: _reason,
                  details: _details.text.trim(),
                ).digest ==
                r.selector.payloadDigest)
          _details.clear();
      }
    }
    setState(() {});
  }

  @override
  void initState() {
    super.initState();
    _bind();
  }

  @override
  void didUpdateWidget(SupportPage old) {
    super.didUpdateWidget(old);
    if (old.targetType != widget.targetType ||
        old.targetID != widget.targetID ||
        old.client != widget.client ||
        old.apiBaseUrl != widget.apiBaseUrl ||
        old.identityChanges != widget.identityChanges ||
        old.pendingStore != widget.pendingStore ||
        (old.ownerID == null) != (widget.ownerID == null)) {
      _data.removeListener(_changed);
      _data.dispose();
      if (_ownsClient) _client.close();
      _details.clear();
      _reports = const [];
      ++_read;
      _bind();
    }
  }

  @override
  void dispose() {
    ++_read;
    _data.removeListener(_changed);
    _data.dispose();
    _details.dispose();
    if (_ownsClient) _client.close();
    super.dispose();
  }

  Future<void> _load() async {
    await _data.initialize();
    if (!mounted || !_data.current || !humanControlID(_data.owner)) return;
    await _loadReports();
  }

  Future<void> _loadReports() async {
    final c = _data, guard = _data.capture(), read = ++_read;
    if (!guard() || !humanControlID(c.owner)) return;
    setState(() {
      _reportsLoading = true;
      _reportsError = null;
    });
    try {
      final raw = await c.api.json('GET', '/v1/me/reports', current: guard);
      if (!mounted || !identical(_data, c) || !guard() || read != _read) return;
      if (raw is! List) throw const FormatException();
      final rows = <Map<String, dynamic>>[];
      for (final v in raw) {
        if (v is! Map<String, dynamic> ||
            !humanControlID(v['id']) ||
            v['reason'] is! String ||
            v['status'] is! String)
          throw const FormatException();
        rows.add(Map.unmodifiable(v));
      }
      setState(() => _reports = List.unmodifiable(rows));
    } catch (_) {
      if (mounted && identical(_data, c) && guard() && read == _read)
        setState(() {
          _reports = const [];
          _reportsError = '提交记录暂不可读，请稍后读取。';
        });
    } finally {
      if (mounted && identical(_data, c) && guard() && read == _read)
        setState(() => _reportsLoading = false);
    }
  }

  Future<void> _submit() async {
    final c = _data;
    if (!c.current || !c.canStart) return;
    final input = HumanReportInput(
      operationID: const Uuid().v4(),
      targetType: widget.targetType,
      targetID: widget.targetID ?? '',
      reason: _reason,
      details: _details.text.trim(),
    );
    final r = await c.checkAndConfirm(context, input);
    if (!mounted || !identical(_data, c) || !c.current) return;
    if (r?.status == 'SUCCEEDED') {
      _details.clear();
      await _loadReports();
    }
  }

  String _reasonLabel(String value) => switch (value) {
    'safety' => '安全问题',
    'harassment' => '骚扰或冒充',
    'incorrect_information' => '信息有误',
    'technical' => '功能故障',
    _ => '其他问题',
  };
  String _statusLabel(String value) => switch (value) {
    'open' => '待处理',
    'reviewing' => '处理中',
    'resolved' => '已处理',
    _ => '状态待核对',
  };
  @override
  Widget build(BuildContext context) {
    final c = _data,
        current = c.current && humanControlID(c.owner),
        e = c.epoch,
        s = c.snapshot;
    bool frame() => mounted && identical(_data, c) && c.current && c.epoch == e;
    return Scaffold(
      appBar: birdtieAppBar(context, title: const Text('帮助与举报')),
      body: ListView(
        padding: const EdgeInsets.all(20),
        children: [
          if (!c.current)
            Text(c.error ?? '当前个人身份或来源已变化。')
          else ...[
            HumanCausalOperationPanel(controller: c),
            if (current) ...[
              Text(
                '遇到问题？告诉我们',
                style: Theme.of(context).textTheme.headlineSmall,
              ),
              const SizedBox(height: 8),
              const Text('提交后会生成编号。请勿填写密码、完整住址或其他不必要的私人资料。'),
              if (widget.targetType != 'general')
                Padding(
                  padding: const EdgeInsets.only(top: 12),
                  child: Text(switch (widget.targetType) {
                    'activity' => '举报此活动',
                    'organization' => '举报此组织',
                    'account' => '举报此账号',
                    'message' => '举报此消息',
                    'community' => '举报此社群',
                    'business' => '举报此商家',
                    'community_message' => '举报此社群消息',
                    'activity_message' => '举报此活动消息',
                    _ => '举报此内容',
                  }),
                ),
              if (s is HumanReportSnapshot)
                Padding(
                  padding: const EdgeInsets.symmetric(vertical: 12),
                  child: Text('当前24小时额度还可提交 ${s.remainingQuota} 次。'),
                ),
              DropdownButtonFormField<String>(
                initialValue: _reason,
                decoration: const InputDecoration(labelText: '问题类型'),
                items: [
                  for (final reason in humanReportReasons)
                    DropdownMenuItem(
                      value: reason,
                      child: Text(_reasonLabel(reason)),
                    ),
                ],
                onChanged: c.busy || c.pending != null
                    ? null
                    : (value) {
                        if (frame() && value != null) {
                          c.invalidatePreview();
                          setState(() => _reason = value);
                        }
                      },
              ),
              const SizedBox(height: 12),
              TextField(
                key: const Key('support-details'),
                controller: _details,
                enabled: !c.busy && c.pending == null,
                maxLength: 1000,
                maxLines: 5,
                decoration: const InputDecoration(
                  border: OutlineInputBorder(),
                  labelText: '请描述发生了什么',
                  hintText: '例如：活动时间与现场信息不一致',
                ),
                onChanged: (_) => c.invalidatePreview(),
              ),
              FilledButton(
                key: const Key('support-submit'),
                onPressed: c.canStart
                    ? () {
                        if (frame()) unawaited(_submit());
                      }
                    : null,
                child: const Text('检查提交问题'),
              ),
              if (c.receipt?.status == 'SUCCEEDED')
                Text('原提交已确证，编号：${c.receipt!.resultID}'),
              const SizedBox(height: 24),
              Text('我的提交记录', style: Theme.of(context).textTheme.titleMedium),
              if (_reportsLoading) const LinearProgressIndicator(),
              if (_reportsError != null)
                Text(_reportsError!)
              else if (!_reportsLoading && _reports.isEmpty)
                const Text('还没有提交记录。'),
              for (final r in _reports)
                ListTile(
                  title: Text(_reasonLabel(r['reason'] as String)),
                  subtitle: Text('编号：${r['id']}'),
                  trailing: Text(_statusLabel(r['status'] as String)),
                ),
              TextButton(
                onPressed: !c.busy && !_reportsLoading
                    ? () {
                        if (frame()) unawaited(_loadReports());
                      }
                    : null,
                child: const Text('重新读取提交记录'),
              ),
            ] else if (!c.busy)
              const Text('登录并核对当前个人身份后提交问题。'),
          ],
        ],
      ),
    );
  }
}
