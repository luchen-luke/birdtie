import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';
import 'entity_action_contract.dart';

class SavedItem {
  const SavedItem({
    required this.id,
    required this.kind,
    required this.targetId,
    required this.title,
    required this.summary,
    required this.cityId,
    required this.available,
  });

  final String id;
  final String kind;
  final String targetId;
  final String title;
  final String summary;
  final String cityId;
  final bool available;

  factory SavedItem.fromJson(Map<String, dynamic> json) => SavedItem(
    id: json['id'] as String,
    kind: json['kind'] as String,
    targetId: json['targetId'] as String,
    title: json['title'] as String? ?? '',
    summary: json['summary'] as String? ?? '',
    cityId: json['cityId'] as String? ?? '',
    available: json['available'] as bool? ?? false,
  );
}

class SavedController extends ChangeNotifier {
  SavedController({
    required this.authorizationHeader,
    http.Client? client,
    String? apiBaseUrl,
  }) : _client = client ?? http.Client(),
       _ownsClient = client == null,
       _apiBaseUrl = apiBaseUrl ?? apiBase;

  static const apiBase = BirdtieEnvironment.apiBaseUrl;
  final String? Function() authorizationHeader;
  final http.Client _client;
  final bool _ownsClient;
  bool _disposed = false;
  int _generation = 0;
  http.Client get followClient => _client;
  String get followApiBaseUrl => _apiBaseUrl;
  final String _apiBaseUrl;
  bool get configured => _apiBaseUrl.isNotEmpty;
  List<SavedItem> items = const [];
  bool loading = false;
  bool failed = false;
  final Set<String> busy = {};
  int _serial = 0;

  String _key(String kind, String targetId) => '$kind:$targetId';
  bool contains(String kind, String targetId) =>
      items.any((item) => item.kind == kind && item.targetId == targetId);
  bool isBusy(String kind, String targetId) =>
      busy.contains(_key(kind, targetId));

  Uri _endpoint(String path) =>
      Uri.parse('${_apiBaseUrl.replaceFirst(RegExp(r'/$'), '')}$path');
  Map<String, String> _headers({bool json = false}) {
    final token = authorizationHeader();
    final headers = <String, String>{};
    if (json) headers['Content-Type'] = 'application/json';
    if (token != null) headers['Authorization'] = token;
    return headers;
  }

  void clear() {
    if (_disposed) return;
    ++_generation;
    ++_serial;
    items = const [];
    loading = false;
    failed = false;
    busy.clear();
    notifyListeners();
  }

  Future<void> load() async {
    if (_disposed) return;
    final capturedToken = authorizationHeader();
    if (!configured || authorizationHeader() == null) {
      clear();
      return;
    }
    final serial = ++_serial;
    loading = true;
    failed = false;
    notifyListeners();
    try {
      final response = await _client
          .get(_endpoint('/v1/me/saved'), headers: _headers())
          .timeout(const Duration(seconds: 12));
      if (response.statusCode != 200) throw StateError('Saved unavailable');
      final rows =
          (jsonDecode(response.body) as Map<String, dynamic>)['data']
              as List<dynamic>;
      if (_disposed ||
          serial != _serial ||
          authorizationHeader() != capturedToken) {
        return;
      }
      items = [
        for (final row in rows) SavedItem.fromJson(row as Map<String, dynamic>),
      ];
    } catch (_) {
      if (_disposed ||
          serial != _serial ||
          authorizationHeader() != capturedToken) {
        return;
      }
      failed = true;
    } finally {
      if (!_disposed &&
          serial == _serial &&
          authorizationHeader() == capturedToken) {
        loading = false;
        notifyListeners();
      }
    }
  }

  Future<void> toggle(
    String kind,
    String targetId, {
    String entrySource = 'direct',
    EntityActionDescriptor? approved,
  }) async {
    if (_disposed) return;
    if (!configured || authorizationHeader() == null) {
      throw StateError('Sign in to save Birdtie items');
    }
    final key = _key(kind, targetId);
    final currentToken = authorizationHeader();
    final generation = _generation;
    if (approved != null) {
      final expectedType = kind == 'group' ? 'community' : kind;
      if (approved.target != EntityActionRef(expectedType, targetId) ||
          approved.operation !=
              (contains(kind, targetId) ? 'UNSAVE' : 'SAVE')) {
        throw StateError('收藏状态已变化，请重新检查。');
      }
    }
    if (busy.contains(key)) return;
    busy.add(key);
    notifyListeners();
    try {
      final existing = items.where(
        (item) => item.kind == kind && item.targetId == targetId,
      );
      if (existing.isEmpty) {
        final response = await _client
            .post(
              _endpoint('/v1/me/saved'),
              headers: {
                ..._headers(json: true),
                'X-Birdtie-Entry-Source': entrySource,
                if (approved != null) ...approved.conditionHeaders,
              },
              body: jsonEncode({'kind': kind, 'targetId': targetId}),
            )
            .timeout(const Duration(seconds: 12));
        if (response.statusCode != 201) throw StateError('Could not save item');
      } else {
        final response = await _client
            .delete(
              _endpoint(
                '/v1/me/saved/${Uri.encodeComponent(existing.first.id)}',
              ),
              headers: {
                ..._headers(),
                if (approved != null) ...approved.conditionHeaders,
              },
            )
            .timeout(const Duration(seconds: 12));
        if (response.statusCode != 204) {
          throw StateError('Could not remove saved item');
        }
      }
      if (_disposed ||
          generation != _generation ||
          authorizationHeader() != currentToken) {
        return;
      }
      await load();
    } finally {
      if (!_disposed && generation == _generation) {
        busy.remove(key);
        notifyListeners();
      }
    }
  }

  @override
  void dispose() {
    if (_disposed) return;
    _disposed = true;
    ++_generation;
    ++_serial;
    if (_ownsClient) _client.close();
    super.dispose();
  }
}

class SavedPage extends StatefulWidget {
  const SavedPage({super.key, required this.saved});
  final SavedController saved;

  @override
  State<SavedPage> createState() => _SavedPageState();
}

class _SavedPageState extends State<SavedPage> {
  @override
  void initState() {
    super.initState();
    unawaited(widget.saved.load());
  }

  Future<void> _remove(SavedItem item) async {
    try {
      await widget.saved.toggle(item.kind, item.targetId);
    } catch (_) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(const SnackBar(content: Text('取消收藏失败，请重试。')));
      }
    }
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: widget.saved,
    builder: (context, _) {
      final saved = widget.saved;
      if (!saved.configured) {
        return const Center(child: Text('请连接 Birdtie API 后查看收藏。'));
      }
      if (saved.authorizationHeader() == null) {
        return const Center(child: Text('登录后即可查看收藏内容。'));
      }
      if (saved.loading && saved.items.isEmpty) {
        return const Center(child: CircularProgressIndicator());
      }
      if (saved.failed && saved.items.isEmpty) {
        return Center(
          child: TextButton(
            onPressed: saved.load,
            child: const Text('收藏内容暂不可用，点击重试。'),
          ),
        );
      }
      if (saved.items.isEmpty) {
        return const Center(
          child: Padding(
            padding: EdgeInsets.all(28),
            child: Text('你收藏的地点、活动和社群会显示在这里。', textAlign: TextAlign.center),
          ),
        );
      }
      return RefreshIndicator(
        onRefresh: saved.load,
        child: ListView(
          padding: const EdgeInsets.all(16),
          children: [
            for (final item in saved.items)
              ListTile(
                leading: Icon(switch (item.kind) {
                  'place' => Icons.place_outlined,
                  'activity' => Icons.event_outlined,
                  _ => Icons.group_outlined,
                }),
                title: Text(item.available ? item.title : '内容暂不可用'),
                subtitle: Text(
                  item.available
                      ? '${item.cityId} · ${item.summary.isEmpty ? _savedKind(item.kind) : item.summary}'
                      : '该内容已不可见，你可以取消收藏。',
                  maxLines: 2,
                  overflow: TextOverflow.ellipsis,
                ),
                trailing: IconButton(
                  tooltip: '取消收藏',
                  onPressed: saved.isBusy(item.kind, item.targetId)
                      ? null
                      : () => _remove(item),
                  icon: const Icon(Icons.bookmark_remove_outlined),
                ),
              ),
          ],
        ),
      );
    },
  );
}

String _savedKind(String kind) => switch (kind) {
  'place' => '地点',
  'activity' => '活动',
  'group' => '社群',
  _ => kind,
};
