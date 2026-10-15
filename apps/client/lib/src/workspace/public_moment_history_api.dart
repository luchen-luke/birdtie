import 'dart:convert';
import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';
import 'human_causal_control.dart';
import 'public_moment_api.dart';

class PublicMomentHistoryQuery {
  const PublicMomentHistoryQuery({
    required this.scope,
    this.cityID = '',
    this.authorID = '',
    this.from,
    this.to,
  });
  final String scope, cityID, authorID;
  final DateTime? from, to;
  Map<String, String> get parameters => {
    'scope': scope,
    if (cityID.isNotEmpty) 'cityId': cityID,
    if (authorID.isNotEmpty) 'authorId': authorID,
    if (from != null)
      'from': from!.toUtc().toIso8601String().replaceFirst('.000Z', 'Z'),
    if (to != null)
      'to': to!.toUtc().toIso8601String().replaceFirst('.000Z', 'Z'),
  };
  String get binding => jsonEncode(parameters);
}

class MomentHistoryFailure implements Exception {
  const MomentHistoryFailure(this.status, this.message);
  final int status;
  final String message;
  @override
  String toString() => message;
}

class PublicMomentHistoryPageData {
  const PublicMomentHistoryPageData({
    required this.items,
    required this.snapshotAt,
    required this.observedAt,
    required this.validUntil,
    required this.refreshCursor,
    this.nextCursor,
  });
  final List<PublicMoment> items;
  final DateTime snapshotAt, observedAt, validUntil;
  final String? nextCursor;
  final String refreshCursor;
  factory PublicMomentHistoryPageData.decode(
    Object? raw,
    PublicMomentHistoryQuery query,
  ) {
    const keys = {
      'schemaVersion',
      'scope',
      'cityId',
      'authorId',
      'from',
      'to',
      'items',
      'snapshotAt',
      'observedAt',
      'validUntil',
      'nextCursor',
      'refreshCursor',
    };
    if (raw is! Map<String, dynamic> ||
        raw.length != keys.length ||
        raw.keys.toSet().difference(keys).isNotEmpty ||
        raw['schemaVersion'] != 'public-moment-history-v1' ||
        raw['scope'] != query.scope ||
        raw['cityId'] != query.cityID ||
        raw['authorId'] != query.authorID ||
        raw['from'] != (query.parameters['from'] ?? '') ||
        raw['to'] != (query.parameters['to'] ?? '') ||
        raw['items'] is! List) {
      throw const FormatException('动态列表格式不符');
    }
    final observed = humanControlTime(raw['observedAt']),
        snapshot = humanControlTime(raw['snapshotAt']),
        until = humanControlTime(raw['validUntil']);
    if (snapshot.isAfter(observed) ||
        observed.isAfter(
          DateTime.now().toUtc().add(const Duration(seconds: 2)),
        ) ||
        !until.isAfter(observed) ||
        until.difference(observed) > const Duration(minutes: 1) ||
        !until.isAfter(DateTime.now().toUtc()))
      throw const FormatException('动态列表已过期');
    final cursor = raw['nextCursor'];
    final refresh = raw['refreshCursor'];
    if (refresh is! String || refresh.isEmpty || refresh.length > 4096)
      throw const FormatException('动态刷新来源不符');
    if (cursor != null &&
        (cursor is! String || cursor.isEmpty || cursor.length > 4096))
      throw const FormatException('动态列表分页格式不符');
    final items = <PublicMoment>[], ids = <String>{};
    for (final row in raw['items'] as List) {
      if (row is! Map<String, dynamic> || row['id'] is! String)
        throw const FormatException('动态列表条目不符');
      final item = PublicMoment.fromJson(
        row,
        row['id'] as String,
        includePublicAuthor: true,
      );
      if (!ids.add(item.id) ||
          item.publishedAt.isAfter(snapshot) ||
          item.validUntil == null ||
          item.validUntil!.isBefore(until))
        throw const FormatException('动态列表来源不符');
      if (items.isNotEmpty &&
          (item.publishedAt.isAfter(items.last.publishedAt) ||
              item.publishedAt == items.last.publishedAt &&
                  item.id.compareTo(items.last.id) >= 0))
        throw const FormatException('动态列表顺序不符');
      items.add(item);
    }
    if (items.length > 20 || cursor != null && items.length != 20)
      throw const FormatException('动态列表分页不符');
    return PublicMomentHistoryPageData(
      items: List.unmodifiable(items),
      snapshotAt: snapshot,
      observedAt: observed,
      validUntil: until,
      refreshCursor: refresh,
      nextCursor: cursor as String?,
    );
  }
}

class PublicMomentHistoryApi {
  PublicMomentHistoryApi({
    required this.authorizationHeader,
    http.Client? client,
    String? apiBaseUrl,
  }) : client = client ?? http.Client(),
       ownsClient = client == null,
       baseUrl = apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl;
  final String? Function() authorizationHeader;
  final http.Client client;
  final bool ownsClient;
  final String baseUrl;
  Future<PublicMomentHistoryPageData> read(
    PublicMomentHistoryQuery query, {
    String? cursor,
  }) async {
    final token = authorizationHeader();
    if (baseUrl.isEmpty) throw const MomentHistoryFailure(0, '动态服务尚不可用。');
    final response = await client
        .get(
          Uri.parse(
            '${baseUrl.replaceFirst(RegExp(r'/$'), '')}/v1/moments/history',
          ).replace(
            queryParameters: {
              ...query.parameters,
              if (cursor != null) 'cursor': cursor,
            },
          ),
          headers: {'Authorization': ?token},
        )
        .timeout(const Duration(seconds: 12));
    if (authorizationHeader() != token)
      throw const MomentHistoryFailure(401, '登录身份已变化，请重新读取。');
    if (response.statusCode != 200)
      throw MomentHistoryFailure(
        response.statusCode,
        switch (response.statusCode) {
          401 => '请登录后查看当前可访问的动态。',
          404 => '此城市或作者当前不可查看。',
          409 => '动态或关系已更新，请刷新列表。',
          _ => '动态暂不可读取，请重试。',
        },
      );
    final envelope = jsonDecode(utf8.decode(response.bodyBytes));
    if (envelope is! Map<String, dynamic> ||
        envelope.length != 1 ||
        !envelope.containsKey('data'))
      throw const FormatException('动态列表响应不符');
    return PublicMomentHistoryPageData.decode(envelope['data'], query);
  }

  void dispose() {
    if (ownsClient) client.close();
  }
}
