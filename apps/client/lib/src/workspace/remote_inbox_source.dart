import 'dart:convert';

import 'package:http/http.dart' as http;

class InboxItem {
  const InboxItem({
    required this.id,
    required this.category,
    required this.title,
    required this.detail,
    required this.resourceType,
    required this.resourceID,
    required this.createdAt,
    this.readAt,
  });
  final String id;
  final String category;
  final String title;
  final String detail;
  final String resourceType;
  final String resourceID;
  final DateTime createdAt;
  final DateTime? readAt;

  factory InboxItem.fromJson(Map<String, dynamic> data) => InboxItem(
    id: data['id'] as String,
    category: data['category'] as String,
    title: data['title'] as String,
    detail: data['detail'] as String,
    resourceType: data['resourceType'] as String,
    resourceID: data['resourceId'] as String,
    createdAt: DateTime.parse(data['createdAt'] as String),
    readAt: data['readAt'] == null
        ? null
        : DateTime.parse(data['readAt'] as String),
  );
}

class RemoteInboxSource {
  RemoteInboxSource({
    required this.authorizationHeader,
    http.Client? client,
    String? apiBaseUrl,
  }) : _client = client ?? http.Client(),
       _apiBaseUrl = apiBaseUrl ?? apiBase;

  static const apiBase = String.fromEnvironment('BIRDTIE_API_BASE_URL');
  final String? Function() authorizationHeader;
  final http.Client _client;
  final String _apiBaseUrl;

  Uri _endpoint(String path) =>
      Uri.parse('${_apiBaseUrl.replaceFirst(RegExp(r'/$'), '')}$path');

  Map<String, String> _headers() {
    final bearer = authorizationHeader();
    if (bearer == null) throw StateError('Sign in to use Inbox');
    return {'Authorization': bearer};
  }

  Future<List<InboxItem>> load() async {
    final response = await _client
        .get(_endpoint('/v1/me/inbox'), headers: _headers())
        .timeout(const Duration(seconds: 10));
    if (response.statusCode != 200) throw StateError('Inbox unavailable');
    final data =
        (jsonDecode(response.body) as Map<String, dynamic>)['data']
            as List<dynamic>;
    return [
      for (final item in data) InboxItem.fromJson(item as Map<String, dynamic>),
    ];
  }

  Future<InboxItem> markRead(String id) async {
    final response = await _client
        .post(
          _endpoint('/v1/me/inbox/${Uri.encodeComponent(id)}/read'),
          headers: _headers(),
        )
        .timeout(const Duration(seconds: 10));
    if (response.statusCode != 200) throw StateError('Cannot mark item read');
    return InboxItem.fromJson(
      (jsonDecode(response.body) as Map<String, dynamic>)['data']
          as Map<String, dynamic>,
    );
  }

  void dispose() => _client.close();
}
