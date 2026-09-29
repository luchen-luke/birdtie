import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;

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
       _apiBaseUrl = apiBaseUrl ?? apiBase;

  static const apiBase = String.fromEnvironment('BIRDTIE_API_BASE_URL');
  final String? Function() authorizationHeader;
  final http.Client _client;
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
    ++_serial;
    items = const [];
    loading = false;
    failed = false;
    busy.clear();
    notifyListeners();
  }

  Future<void> load() async {
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
      if (serial != _serial) return;
      items = [
        for (final row in rows) SavedItem.fromJson(row as Map<String, dynamic>),
      ];
    } catch (_) {
      if (serial != _serial) return;
      failed = true;
    } finally {
      if (serial == _serial) {
        loading = false;
        notifyListeners();
      }
    }
  }

  Future<void> toggle(String kind, String targetId) async {
    if (!configured || authorizationHeader() == null) {
      throw StateError('Sign in to save Birdtie items');
    }
    final key = _key(kind, targetId);
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
              headers: _headers(json: true),
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
              headers: _headers(),
            )
            .timeout(const Duration(seconds: 12));
        if (response.statusCode != 204) {
          throw StateError('Could not remove saved item');
        }
      }
      await load();
    } finally {
      busy.remove(key);
      notifyListeners();
    }
  }

  @override
  void dispose() {
    ++_serial;
    _client.close();
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
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(content: Text('Could not remove this saved item.')),
        );
      }
    }
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: widget.saved,
    builder: (context, _) {
      final saved = widget.saved;
      if (!saved.configured) {
        return const Center(child: Text('Saved requires the Birdtie API.'));
      }
      if (saved.authorizationHeader() == null) {
        return const Center(child: Text('Sign in to see your saved items.'));
      }
      if (saved.loading && saved.items.isEmpty) {
        return const Center(child: CircularProgressIndicator());
      }
      if (saved.failed && saved.items.isEmpty) {
        return Center(
          child: TextButton(
            onPressed: saved.load,
            child: const Text('Saved unavailable. Tap to retry.'),
          ),
        );
      }
      if (saved.items.isEmpty) {
        return const Center(
          child: Padding(
            padding: EdgeInsets.all(28),
            child: Text(
              'Places, activities and groups you save will appear here.',
              textAlign: TextAlign.center,
            ),
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
                title: Text(item.available ? item.title : 'Unavailable item'),
                subtitle: Text(
                  item.available
                      ? '${item.cityId} · ${item.summary.isEmpty ? item.kind : item.summary}'
                      : 'No longer visible. You can remove this bookmark.',
                  maxLines: 2,
                  overflow: TextOverflow.ellipsis,
                ),
                trailing: IconButton(
                  tooltip: 'Remove saved item',
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
