import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';

class OrganizationAnalyticsPage extends StatefulWidget {
  const OrganizationAnalyticsPage({
    super.key,
    required this.organizationID,
    required this.authorizationHeader,
    this.apiBaseUrl,
    this.client,
  });

  final String organizationID;
  final String? Function() authorizationHeader;
  final String? apiBaseUrl;
  final http.Client? client;

  @override
  State<OrganizationAnalyticsPage> createState() =>
      _OrganizationAnalyticsPageState();
}

class _OrganizationAnalyticsPageState extends State<OrganizationAnalyticsPage> {
  static const _configuredBase = BirdtieEnvironment.apiBaseUrl;
  late final http.Client _client = widget.client ?? http.Client();
  List<Map<String, dynamic>> _items = const [];
  String? _error;
  bool _loading = true;

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    if (widget.client == null) _client.close();
    super.dispose();
  }

  Future<void> _load() async {
    final base = widget.apiBaseUrl ?? _configuredBase;
    if (base.isEmpty || widget.authorizationHeader() == null) {
      setState(() {
        _loading = false;
        _error = '请登录并连接活动服务。';
      });
      return;
    }
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final response = await _client
          .get(
            Uri.parse(
              '${base.replaceFirst(RegExp(r'/$'), '')}/v1/me/organizations/${Uri.encodeComponent(widget.organizationID)}/analytics',
            ),
            headers: {'Authorization': widget.authorizationHeader()!},
          )
          .timeout(const Duration(seconds: 12));
      if (response.statusCode != 200) throw StateError('analytics unavailable');
      final body = jsonDecode(response.body) as Map<String, dynamic>;
      final items = (body['data'] as List<dynamic>)
          .cast<Map<String, dynamic>>();
      if (mounted) setState(() => _items = items);
    } catch (_) {
      if (mounted) setState(() => _error = '统计暂不可用，请稍后重试。');
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  String _source(String value) => switch (value) {
    'now' => '附近',
    'agent' => '智能体结果',
    'map' => '地图',
    'organization' => '组织主页',
    'inbox' => '收件箱',
    'direct' => '直接打开',
    _ => '来源未记录',
  };

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: const Text('活动数据')),
    body: RefreshIndicator(
      onRefresh: _load,
      child: ListView(
        padding: const EdgeInsets.all(20),
        children: [
          const Text(
            '试点活动漏斗',
            style: TextStyle(fontSize: 22, fontWeight: FontWeight.w700),
          ),
          const SizedBox(height: 6),
          const Text('展示次数 → 详情打开 → 报名；收藏单独计数。次数不代表独立人数。'),
          const SizedBox(height: 18),
          if (_loading) const Center(child: CircularProgressIndicator()),
          if (_error != null) ...[
            Text(_error!, style: const TextStyle(color: Colors.red)),
            TextButton(onPressed: _load, child: const Text('重试')),
          ],
          if (!_loading && _error == null && _items.isEmpty)
            const Text('还没有可统计的组织活动。'),
          for (final item in _items)
            Card(
              margin: const EdgeInsets.only(bottom: 12),
              child: Padding(
                padding: const EdgeInsets.all(16),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      item['title'] as String? ?? '活动',
                      style: Theme.of(context).textTheme.titleMedium,
                    ),
                    const SizedBox(height: 8),
                    Text(
                      '展示 ${item['impressions'] ?? 0} · 详情 ${item['detailOpens'] ?? 0} · 报名 ${item['rsvps'] ?? 0} · 收藏 ${item['saves'] ?? 0}',
                    ),
                    for (final source
                        in (item['sources'] as List<dynamic>? ?? const []))
                      Text(
                        '${_source((source as Map<String, dynamic>)['source'] as String? ?? 'unknown')}：展示 ${source['impressions'] ?? 0} · 详情 ${source['detailOpens'] ?? 0} · 报名 ${source['rsvps'] ?? 0} · 收藏 ${source['saves'] ?? 0}',
                      ),
                  ],
                ),
              ),
            ),
        ],
      ),
    ),
  );
}
