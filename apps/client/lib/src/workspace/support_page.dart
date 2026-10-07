import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';

class SupportPage extends StatefulWidget {
  const SupportPage({
    super.key,
    required this.authorizationHeader,
    this.targetType = 'general',
    this.targetID,
    this.apiBaseUrl,
    this.client,
  });

  final String? Function() authorizationHeader;
  final String targetType;
  final String? targetID;
  final String? apiBaseUrl;
  final http.Client? client;

  @override
  State<SupportPage> createState() => _SupportPageState();
}

class _SupportPageState extends State<SupportPage> {
  static const _configuredBase = BirdtieEnvironment.apiBaseUrl;
  late final http.Client _client = widget.client ?? http.Client();
  final TextEditingController _details = TextEditingController();
  String _reason = 'safety';
  bool _busy = false;
  bool _loading = true;
  String? _error;
  String? _receipt;
  List<Map<String, dynamic>> _reports = const [];

  String get _base => widget.apiBaseUrl ?? _configuredBase;
  Uri get _endpoint =>
      Uri.parse('${_base.replaceFirst(RegExp(r'/$'), '')}/v1/me/reports');

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _details.dispose();
    if (widget.client == null) _client.close();
    super.dispose();
  }

  Future<void> _load() async {
    final token = widget.authorizationHeader();
    if (_base.isEmpty || token == null) {
      if (mounted) setState(() => _loading = false);
      return;
    }
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final response = await _client
          .get(_endpoint, headers: {'Authorization': token})
          .timeout(const Duration(seconds: 12));
      if (response.statusCode != 200) throw StateError('reports unavailable');
      final rows =
          (jsonDecode(response.body) as Map<String, dynamic>)['data']
              as List<dynamic>;
      if (mounted) setState(() => _reports = rows.cast<Map<String, dynamic>>());
    } catch (_) {
      if (mounted) setState(() => _error = '提交记录暂不可用，请稍后重试。');
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  Future<void> _submit() async {
    final token = widget.authorizationHeader();
    if (_busy || token == null || _details.text.trim().runes.length < 10) {
      setState(
        () => _error = token == null ? '请先登录后提交。' : '请至少填写 10 个字，说明发生了什么。',
      );
      return;
    }
    setState(() {
      _busy = true;
      _error = null;
      _receipt = null;
    });
    try {
      final response = await _client
          .post(
            _endpoint,
            headers: {
              'Authorization': token,
              'Content-Type': 'application/json',
            },
            body: jsonEncode({
              'targetType': widget.targetType,
              'targetId': widget.targetID ?? '',
              'reason': _reason,
              'details': _details.text.trim(),
            }),
          )
          .timeout(const Duration(seconds: 12));
      if (response.statusCode != 201) {
        final code =
            ((jsonDecode(response.body) as Map<String, dynamic>)['error']
                as Map<String, dynamic>?)?['code'];
        throw StateError(
          code == 'report_rate_limited'
              ? '24 小时内最多提交 5 次，请稍后再试。'
              : '提交失败，请稍后重试。',
        );
      }
      final item =
          (jsonDecode(response.body) as Map<String, dynamic>)['data']
              as Map<String, dynamic>;
      if (!mounted) return;
      _details.clear();
      setState(() => _receipt = item['id'] as String?);
      await _load();
    } catch (error) {
      if (mounted) {
        setState(
          () => _error = error is StateError ? error.message : '提交失败，请稍后重试。',
        );
      }
    } finally {
      if (mounted) setState(() => _busy = false);
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
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: const Text('帮助与举报')),
    body: ListView(
      padding: const EdgeInsets.all(20),
      children: [
        const Text(
          '遇到问题？告诉我们',
          style: TextStyle(fontSize: 22, fontWeight: FontWeight.w700),
        ),
        const SizedBox(height: 8),
        const Text('提交后会生成编号。请勿填写密码、完整住址或其他不必要的私人资料。'),
        if (widget.targetType != 'general') ...[
          const SizedBox(height: 12),
          Text(switch (widget.targetType) {
            'activity' => '举报此活动',
            'organization' => '举报此组织',
            'account' => '举报此账号',
            'message' => '举报此消息',
            'community' => '举报此社群',
            'business' => '举报此商家',
            _ => '举报此内容',
          }),
        ],
        const SizedBox(height: 16),
        if (_base.isEmpty)
          const Text('帮助服务暂不可用。')
        else if (widget.authorizationHeader() == null)
          const Text('请先在个人资料页登录后提交。')
        else ...[
          DropdownButtonFormField<String>(
            initialValue: _reason,
            decoration: const InputDecoration(labelText: '问题类型'),
            items: [
              for (final reason in [
                'safety',
                'harassment',
                'incorrect_information',
                'technical',
                'other',
              ])
                DropdownMenuItem(
                  value: reason,
                  child: Text(_reasonLabel(reason)),
                ),
            ],
            onChanged: _busy
                ? null
                : (value) {
                    if (value != null) setState(() => _reason = value);
                  },
          ),
          const SizedBox(height: 12),
          TextField(
            controller: _details,
            maxLength: 1000,
            maxLines: 5,
            decoration: const InputDecoration(
              border: OutlineInputBorder(),
              labelText: '请描述发生了什么',
              hintText: '例如：活动时间与现场信息不一致',
            ),
          ),
          FilledButton(
            onPressed: _busy ? null : _submit,
            child: Text(_busy ? '正在提交…' : '提交问题'),
          ),
          if (_receipt != null) Text('已提交，编号：$_receipt'),
          if (_error != null)
            Text(_error!, style: const TextStyle(color: Colors.red)),
          const SizedBox(height: 28),
          Text('我的提交记录', style: Theme.of(context).textTheme.titleMedium),
          if (_loading) const LinearProgressIndicator(),
          if (!_loading && _reports.isEmpty) const Text('还没有提交记录。'),
          for (final report in _reports)
            ListTile(
              title: Text(_reasonLabel(report['reason'] as String? ?? 'other')),
              subtitle: Text('编号：${report['id']}'),
              trailing: Text(_statusLabel(report['status'] as String? ?? '')),
            ),
        ],
      ],
    ),
  );
}
