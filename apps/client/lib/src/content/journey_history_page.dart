import 'dart:async';
import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;

bool _id(Object? v) =>
    v is String &&
    RegExp(
      r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
    ).hasMatch(v) &&
    v != '00000000-0000-0000-0000-000000000000';
DateTime _time(Object? v) {
  if (v is! String || !RegExp(r'(Z|[+-]\d{2}:\d{2})$').hasMatch(v))
    throw const FormatException('原记录时间无效');
  return DateTime.parse(v).toUtc();
}

class JourneyHistoryRecord {
  JourneyHistoryRecord(
    this.id,
    this.journeyID,
    this.contextKind,
    this.summary,
    this.createdAt,
    this.updatedAt,
  );
  final String id, journeyID, contextKind, summary;
  final DateTime createdAt, updatedAt;
  factory JourneyHistoryRecord.read(Object? raw) {
    final v = raw as Map<String, dynamic>;
    if (!_id(v['id']) ||
        v['journeyId'] is! String ||
        (v['journeyId'] != '' && !_id(v['journeyId'])) ||
        !const [
          'JOURNEY',
          'DRAFT_CONTEXT',
          'JOURNEY_UNAVAILABLE',
        ].contains(v['contextKind']) ||
        v['summary'] is! String ||
        v['sourceStatus'] != 'active' ||
        v['sourceContextRevision'] is! int)
      throw const FormatException('原对话身份无效');
    return JourneyHistoryRecord(
      v['id'],
      v['journeyId'],
      v['contextKind'],
      v['summary'],
      _time(v['createdAt']),
      _time(v['updatedAt']),
    );
  }
}

class JourneyHistoricalMessage {
  JourneyHistoricalMessage(this.id, this.role, this.body, this.createdAt);
  final String id, role, body;
  final DateTime createdAt;
  factory JourneyHistoricalMessage.read(Object? raw) {
    final v = raw as Map<String, dynamic>;
    if (!_id(v['id']) ||
        !const ['USER', 'ASSISTANT'].contains(v['role']) ||
        v['body'] is! String ||
        utf8.encode(v['body']).length > 32768)
      throw const FormatException('原对话内容无效');
    return JourneyHistoricalMessage(
      v['id'],
      v['role'],
      v['body'],
      _time(v['createdAt']),
    );
  }
}

class JourneyHistoryData {
  JourneyHistoryData(
    this.threads,
    this.messages,
    this.preferences,
    this.observedAt,
    this.validUntil,
    this.nextCursor,
  );
  final List<JourneyHistoryRecord> threads;
  final List<JourneyHistoricalMessage> messages;
  final Map<String, dynamic>? preferences;
  final DateTime observedAt, validUntil;
  final String? nextCursor;
  bool get current => validUntil.isAfter(DateTime.now().toUtc());
  factory JourneyHistoryData.read(Object? raw) {
    final v = raw as Map<String, dynamic>;
    if (v['schemaVersion'] != 'owned-journey-history-v1' ||
        v['threads'] is! List ||
        v['messages'] is! List ||
        v['sourceVersion'] is! String ||
        !RegExp(r'^[0-9a-f]{64}$').hasMatch(v['sourceVersion']))
      throw const FormatException('原记录来源无效');
    final observed = _time(v['observedAt']), until = _time(v['validUntil']);
    if (!until.isAfter(observed) ||
        until.difference(observed) > const Duration(seconds: 30) ||
        !until.isAfter(DateTime.now().toUtc()))
      throw const FormatException('原记录读取已过期');
    final threads = (v['threads'] as List)
            .map(JourneyHistoryRecord.read)
            .toList(growable: false),
        messages = (v['messages'] as List)
            .map(JourneyHistoricalMessage.read)
            .toList(growable: false);
    if (threads.length > 20 ||
        messages.length > 1000 ||
        threads.map((e) => e.id).toSet().length != threads.length ||
        messages.map((e) => e.id).toSet().length != messages.length)
      throw const FormatException('原记录顺序无效');
    final next = v['nextCursor'];
    if (next != null && (next is! String || next.isEmpty || next.length > 4096))
      throw const FormatException('原记录分页无效');
    Map<String, dynamic>? preferences;
    if (v['preferences'] != null) {
      preferences = v['preferences'] as Map<String, dynamic>;
      final settings = preferences['settings'];
      if (settings is! Map<String, dynamic> ||
          settings.length != 8 ||
          !const [
            'chatPush',
            'requestPush',
            'commentPush',
            'followPush',
            'praiseSavePush',
            'journeyPush',
            'systemPush',
            'previewEnabled',
          ].every((k) => settings[k] is bool) ||
          preferences['sourceVersion'] is! int ||
          preferences['timeZone'] is! String ||
          preferences['quietStart'] is! String ||
          preferences['quietEnd'] is! String ||
          preferences['collaborationPush'] != null &&
              preferences['collaborationPush'] is! bool)
        throw const FormatException('原偏好无效');
      _time(preferences['updatedAt']);
    }
    return JourneyHistoryData(
      List.unmodifiable(threads),
      List.unmodifiable(messages),
      preferences,
      observed,
      until,
      next as String?,
    );
  }
}

class JourneyHistoryPage extends StatefulWidget {
  const JourneyHistoryPage({
    super.key,
    required this.client,
    required this.apiBaseUrl,
    required this.authorizationHeader,
    required this.ownerID,
    required this.current,
    this.identityChanges,
    this.journeyID,
    this.preferences = false,
  });
  final http.Client client;
  final String apiBaseUrl;
  final String? Function() authorizationHeader, ownerID;
  final bool Function() current;
  final Listenable? identityChanges;
  final String? journeyID;
  final bool preferences;
  @override
  State<JourneyHistoryPage> createState() => _JourneyHistoryPageState();
}

class _JourneyHistoryPageState extends State<JourneyHistoryPage>
    with WidgetsBindingObserver {
  JourneyHistoryData? _data;
  List<JourneyHistoryRecord> _threads = [];
  String? _threadID, _next, _error;
  bool _loading = false, _retired = false;
  int _serial = 0;
  Timer? _lease;
  late final _source = (
    widget.client,
    widget.apiBaseUrl,
    widget.authorizationHeader,
    widget.ownerID,
    widget.current,
    widget.journeyID,
    widget.preferences,
  );
  late final _identity = (widget.authorizationHeader(), widget.ownerID());
  bool get _current =>
      mounted &&
      !_retired &&
      widget.current() &&
      _identity == (widget.authorizationHeader(), widget.ownerID()) &&
      _identity.$1 != null &&
      _identity.$2 != null &&
      _source ==
          (
            widget.client,
            widget.apiBaseUrl,
            widget.authorizationHeader,
            widget.ownerID,
            widget.current,
            widget.journeyID,
            widget.preferences,
          );
  void _changed() {
    if (!_current) {
      _retired = true;
      _clear('账号或入口已变化，请返回后重新打开。');
    }
  }

  void _clear(String error) {
    _serial++;
    _lease?.cancel();
    if (mounted)
      setState(() {
        _data = null;
        _threads = [];
        _next = null;
        _loading = false;
        _error = error;
      });
  }

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    widget.identityChanges?.addListener(_changed);
    unawaited(_load());
  }

  @override
  void didUpdateWidget(covariant JourneyHistoryPage old) {
    super.didUpdateWidget(old);
    if (old.identityChanges != widget.identityChanges) {
      old.identityChanges?.removeListener(_changed);
      widget.identityChanges?.addListener(_changed);
    }
    _changed();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state != AppLifecycleState.resumed) {
      _clear('返回后请重新读取原记录。');
    }
  }

  @override
  void dispose() {
    _serial++;
    _lease?.cancel();
    widget.identityChanges?.removeListener(_changed);
    WidgetsBinding.instance.removeObserver(this);
    super.dispose();
  }

  Future<void> _load({bool more = false}) async {
    if (!_current || _loading) return;
    final serial = ++_serial, thread = _threadID, cursor = more ? _next : null;
    if (more && cursor == null) return;
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final path = widget.preferences
          ? '/v1/me/notification-preferences/original'
          : thread == null
          ? '/v1/me/journey-history'
          : '/v1/me/journey-history/$thread';
      final uri =
          Uri.parse(
            '${widget.apiBaseUrl.replaceFirst(RegExp(r'/$'), '')}$path',
          ).replace(
            queryParameters: {
              if (!widget.preferences &&
                  thread == null &&
                  widget.journeyID != null)
                'journeyId': widget.journeyID!,
              if (cursor != null) 'cursor': cursor,
            },
          );
      final response = await widget.client
          .get(uri, headers: {'Authorization': _identity.$1!})
          .timeout(const Duration(seconds: 10));
      if (!_current || serial != _serial) return;
      if (response.statusCode != 200)
        throw StateError('原记录暂不可读（${response.statusCode}），请重新读取。');
      if (response.bodyBytes.length > 3 * 1024 * 1024)
        throw const FormatException('原记录超出读取预算');
      final data = JourneyHistoryData.read(
        (jsonDecode(utf8.decode(response.bodyBytes))
            as Map<String, dynamic>)['data'],
      );
      if (thread != null &&
          (data.threads.length != 1 || data.threads.single.id != thread))
        throw const FormatException('原对话不匹配');
      if (more && data.nextCursor == cursor)
        throw const FormatException('原记录分页重复');
      if (!_current || serial != _serial) return;
      final rows = more ? [..._threads, ...data.threads] : data.threads;
      if (rows.map((e) => e.id).toSet().length != rows.length)
        throw const FormatException('原记录重复');
      _lease?.cancel();
      setState(() {
        _data = data;
        _threads = rows;
        _next = data.nextCursor;
      });
      _lease = Timer(
        data.validUntil.difference(DateTime.now().toUtc()),
        () => _clear('原记录读取已过期，请重新读取。'),
      );
    } catch (e) {
      if (_current && serial == _serial)
        setState(() {
          _data = null;
          _threads = [];
          _next = null;
          _error = e is FormatException ? e.message : '原记录暂不可读，请重新读取。';
        });
    } finally {
      if (_current && serial == _serial) setState(() => _loading = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final data = _data;
    final readable = _current && data?.current == true;
    return Scaffold(
      appBar: AppBar(
        title: Text(widget.preferences ? '原通知偏好' : '原对话记录'),
        actions: [
          if (_threadID != null)
            TextButton(
              onPressed: _loading
                  ? null
                  : () {
                      setState(() => _threadID = null);
                      unawaited(_load());
                    },
              child: const Text('全部记录'),
            ),
          IconButton(
            tooltip: '重新读取',
            onPressed: _loading || !_current ? null : () => _load(),
            icon: const Icon(Icons.refresh),
          ),
        ],
      ),
      body: SafeArea(
        child: ListView(
          padding: const EdgeInsets.all(20),
          children: [
            if (_loading) const LinearProgressIndicator(),
            if (_error != null) Text(_error!),
            if (!readable && !_loading && _error == null)
              const Text('原记录暂不可读，请重新读取。'),
            if (readable) ...[
              if (widget.preferences)
                ..._preferences(data!.preferences)
              else if (_threadID != null) ...[
                const Text('原助手对话，仅作本人历史记录。'),
                const SizedBox(height: 16),
                for (final message in data!.messages)
                  Padding(
                    padding: const EdgeInsets.only(bottom: 20),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          message.role == 'USER' ? '我' : '原助手',
                          style: Theme.of(context).textTheme.labelLarge,
                        ),
                        const SizedBox(height: 4),
                        SelectableText(message.body),
                        Text(
                          message.createdAt.toLocal().toString(),
                          style: Theme.of(context).textTheme.bodySmall,
                        ),
                      ],
                    ),
                  ),
                if (data.messages.isEmpty) const Text('这段对话没有消息。'),
              ] else ...[
                const Text('查看原旅程与草稿对话；不会继续执行原来的请求。'),
                const SizedBox(height: 12),
                if (_threads.isEmpty) const Text('没有可读取的原对话记录。'),
                for (final record in _threads)
                  ListTile(
                    contentPadding: EdgeInsets.zero,
                    title: Text(
                      record.summary.isEmpty ? '原对话记录' : record.summary,
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                    ),
                    subtitle: Text(switch (record.contextKind) {
                      'DRAFT_CONTEXT' => '原草稿对话',
                      'JOURNEY_UNAVAILABLE' => '原旅程已不可用，仍可查看本人对话',
                      _ => '旅程原对话',
                    }),
                    trailing: const Icon(Icons.chevron_right),
                    onTap: _loading
                        ? null
                        : () {
                            setState(() => _threadID = record.id);
                            unawaited(_load());
                          },
                  ),
                if (_next != null)
                  TextButton(
                    onPressed: _loading ? null : () => _load(more: true),
                    child: const Text('继续读取'),
                  ),
              ],
            ],
          ],
        ),
      ),
    );
  }

  List<Widget> _preferences(Map<String, dynamic>? p) {
    if (p == null) return [const Text('没有迁入的原通知偏好。')];
    const labels = {
      'chatPush': '会话提醒',
      'requestPush': '申请提醒',
      'commentPush': '评论提醒',
      'followPush': '关注提醒',
      'praiseSavePush': '点赞与收藏提醒',
      'journeyPush': '旅程提醒',
      'systemPush': '系统提醒',
      'previewEnabled': '内容预览',
    };
    final settings = p['settings'] as Map<String, dynamic>;
    return [
      const Text('原偏好只作历史保留。当前提醒方式需在通知设置中单独选择。'),
      const SizedBox(height: 16),
      for (final key in labels.keys)
        ListTile(
          contentPadding: EdgeInsets.zero,
          title: Text(labels[key]!),
          trailing: Text(settings[key] == true ? '原设置：开启' : '原设置：关闭'),
        ),
      if (p['collaborationPush'] != null)
        Text('原协作提醒：${p['collaborationPush'] == true ? '开启' : '关闭'}'),
      if (p['timeZone'] != '') Text('原时区：${p['timeZone']}'),
      Text('原版本：${p['sourceVersion']}'),
    ];
  }
}
