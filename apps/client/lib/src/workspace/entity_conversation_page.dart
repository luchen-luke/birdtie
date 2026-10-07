import 'dart:async';
import 'dart:convert';
import 'dart:math';

import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;

import '../config/birdtie_environment.dart';
import 'support_page.dart';

enum ConversationKind { activity, community }

class EntityConversationPage extends StatefulWidget {
  const EntityConversationPage({
    super.key,
    required this.entityID,
    required this.entityTitle,
    required this.authorizationHeader,
    required this.kind,
    this.apiBaseUrl,
    this.client,
  });
  final ConversationKind kind;
  final String entityID;
  final String entityTitle;
  final String? Function() authorizationHeader;
  final String? apiBaseUrl;
  final http.Client? client;

  @override
  State<EntityConversationPage> createState() => _EntityConversationPageState();
}

class _EntityConversationPageState extends State<EntityConversationPage> {
  late final http.Client _client = widget.client ?? http.Client();
  final _composer = TextEditingController();
  Timer? _identityWatch;
  String? _token;
  Map<String, dynamic>? _state;
  List<Map<String, dynamic>> _messages = [];
  bool _busy = false;
  bool _hasMore = false;
  String? _error;
  String? _pendingID;
  String? _pendingBody;
  int _serial = 0;
  String get _base => widget.apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl;
  bool get _community => widget.kind == ConversationKind.community;
  String get _label => _community ? '社群交流' : '活动交流';
  String get _notice => _community
      ? '加入社群后还需主动加入交流。加入后，你的名称和消息会被其他已加入的有效成员看见；社群管理者可移除消息，你可以举报。退出社群或交流后失去访问权限。'
      : '确认报名后还需主动加入交流。加入后，你的名称和消息会被其他已加入的参与者看见；主办方可移除消息，你可以举报。取消报名或退出交流后失去访问权限。';
  Uri _url([String suffix = '']) => Uri.parse(
    '${_base.replaceFirst(RegExp(r'/$'), '')}/v1/${_community ? 'communities' : 'activities'}/${Uri.encodeComponent(widget.entityID)}/conversation$suffix',
  );

  @override
  void initState() {
    super.initState();
    _token = widget.authorizationHeader();
    // Only observe identity changes; messages refresh after actions or by request.
    _identityWatch = Timer.periodic(
      const Duration(seconds: 1),
      (_) => _identityChanged(),
    );
    unawaited(_load());
  }

  void _identityChanged() {
    final token = widget.authorizationHeader();
    if (!mounted || token == _token) return;
    ++_serial;
    setState(() {
      _token = token;
      _state = null;
      _messages = [];
      _hasMore = false;
      _error = null;
      _busy = false;
      _pendingID = null;
      _pendingBody = null;
      _composer.clear();
    });
    if (token != null) unawaited(_load());
  }

  @override
  void dispose() {
    ++_serial;
    _identityWatch?.cancel();
    _composer.dispose();
    if (widget.client == null) _client.close();
    super.dispose();
  }

  bool _current(int serial, String token) =>
      mounted && serial == _serial && token == widget.authorizationHeader();
  Map<String, dynamic> _data(http.Response response, int expected) {
    if (response.statusCode != expected) {
      throw _ChatFailure(response.statusCode);
    }
    return (jsonDecode(utf8.decode(response.bodyBytes))
            as Map<String, dynamic>)['data']
        as Map<String, dynamic>;
  }

  String _failure(Object error) {
    if (error is _ChatFailure) {
      switch (error.code) {
        case 401:
          return '登录已失效，请重新登录后进入。';
        case 403:
          return '你没有执行此操作的权限。';
        case 404:
          return _community
              ? '有效社群成员可以进入交流。待审批、受邀或已退出的成员无法访问。'
              : '确认报名的参与者和主办方可以进入活动交流。退出或取消报名后无法访问。';
        case 409:
          return _community
              ? '消息已被移除或发生冲突，请刷新后确认。'
              : '活动已结束或取消，或这条消息已被移除。请刷新后确认。';
        case 429:
          return '操作较频繁，请稍后重试。';
      }
    }
    return '暂时无法连接$_label，请重试。未确认发送的内容会保留。';
  }

  void _recordFailure(Object error) {
    _error = _failure(error);
    if (error is _ChatFailure && [401, 403, 404].contains(error.code)) {
      _state = null;
      _messages = [];
      _hasMore = false;
    }
  }

  Future<void> _load({bool earlier = false}) async {
    _identityChanged();
    final token = widget.authorizationHeader();
    if (_busy || token == null || _base.isEmpty) return;
    final serial = ++_serial;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final state = _data(
        await _client
            .get(_url(), headers: {'Authorization': token})
            .timeout(const Duration(seconds: 12)),
        200,
      );
      List<Map<String, dynamic>> messages = [];
      var hasMore = false;
      if (state['joined'] == true) {
        final cursor = earlier && _messages.isNotEmpty
            ? '?before=${Uri.encodeComponent(_messages.first['id'] as String)}'
            : '';
        final page = _data(
          await _client
              .get(_url('/messages$cursor'), headers: {'Authorization': token})
              .timeout(const Duration(seconds: 12)),
          200,
        );
        messages = (page['messages'] as List).cast<Map<String, dynamic>>();
        hasMore = page['hasMore'] == true;
      }
      if (_current(serial, token)) {
        setState(() {
          _state = state;
          _messages = earlier && state['joined'] == true
              ? [...messages, ..._messages]
              : messages;
          _hasMore = hasMore;
        });
      }
    } catch (error) {
      if (_current(serial, token)) setState(() => _recordFailure(error));
    } finally {
      if (_current(serial, token)) setState(() => _busy = false);
    }
  }

  Future<void> _change(
    String method,
    String suffix, [
    Map<String, dynamic>? body,
  ]) async {
    _identityChanged();
    final token = widget.authorizationHeader();
    if (_busy || token == null || _base.isEmpty) return;
    final serial = ++_serial;
    setState(() {
      _busy = true;
      _error = null;
    });
    var changed = false;
    try {
      final headers = {
        'Authorization': token,
        'Content-Type': 'application/json',
      };
      final response =
          await (method == 'POST'
                  ? _client.post(
                      _url(suffix),
                      headers: headers,
                      body: jsonEncode(body),
                    )
                  : _client.delete(_url(suffix), headers: headers))
              .timeout(const Duration(seconds: 12));
      final expected = method == 'DELETE'
          ? 204
          : suffix == '/messages'
          ? 201
          : 200;
      if (response.statusCode != expected) {
        throw _ChatFailure(response.statusCode);
      }
      if (!_current(serial, token)) return;
      changed = true;
      if (method == 'DELETE' && suffix.isEmpty) {
        _state = null;
        _messages = [];
        _hasMore = false;
      } else if (method == 'DELETE' && suffix.startsWith('/messages/')) {
        final id = suffix.substring('/messages/'.length);
        _messages = _messages
            .map(
              (message) => message['id'] == id
                  ? {...message, 'body': '', 'removed': true}
                  : message,
            )
            .toList();
      }
      if (suffix == '/messages' || (method == 'DELETE' && suffix.isEmpty)) {
        _composer.clear();
        _pendingID = null;
        _pendingBody = null;
      }
    } catch (error) {
      if (_current(serial, token)) setState(() => _recordFailure(error));
    } finally {
      if (_current(serial, token)) setState(() => _busy = false);
    }
    if (changed && _current(serial, token)) await _load();
  }

  static String _newMessageID() {
    final random = Random.secure();
    final bytes = List<int>.generate(16, (_) => random.nextInt(256));
    bytes[6] = (bytes[6] & 0x0f) | 0x40;
    bytes[8] = (bytes[8] & 0x3f) | 0x80;
    final hex = bytes.map((b) => b.toRadixString(16).padLeft(2, '0')).join();
    return '${hex.substring(0, 8)}-${hex.substring(8, 12)}-${hex.substring(12, 16)}-${hex.substring(16, 20)}-${hex.substring(20)}';
  }

  void _send() {
    if (_state?['canSend'] != true || _busy) return;
    final body = _composer.text.trim();
    if (body.isEmpty) return;
    if (body.runes.length > 2000) {
      setState(() => _error = '每条消息最多 2000 字。');
      return;
    }
    if (_pendingBody != body) {
      _pendingBody = body;
      _pendingID = _newMessageID();
    }
    unawaited(
      _change('POST', '/messages', {
        'body': body,
        'clientMessageId': _pendingID,
      }),
    );
  }

  Future<void> _confirmChange(
    String title,
    String method,
    String suffix,
  ) async {
    final confirmed = await showDialog<bool>(
      useRootNavigator: false,
      context: context,
      builder: (context) => AlertDialog(
        title: Text(title),
        content: Text(
          suffix.isEmpty
              ? (_community
                    ? '退出后无法读取对话；仍为有效成员时可再次主动加入。社群成员关系不会改变。'
                    : '退出后无法读取对话；符合报名条件时可再次主动加入。报名状态不会改变。')
              : '移除后内容无法恢复，其他参与者只能看到移除提示。',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('返回'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('确认'),
          ),
        ],
      ),
    );
    if (confirmed == true && mounted) await _change(method, suffix);
  }

  Widget _message(Map<String, dynamic> message) {
    final own = message['senderAccountId'] == _state?['viewerAccountId'];
    final removed = message['removed'] == true;
    return ListTile(
      title: Text('${message['senderName']}'),
      subtitle: Text(removed ? '这条消息已被移除' : message['body'] as String),
      trailing: removed
          ? null
          : PopupMenuButton<String>(
              tooltip: '消息操作',
              onSelected: (action) {
                if (action == 'remove') {
                  unawaited(
                    _confirmChange(
                      own ? '撤回这条消息？' : '移除这条消息？',
                      'DELETE',
                      '/messages/${message['id']}',
                    ),
                  );
                } else {
                  Navigator.of(context).push(
                    MaterialPageRoute<void>(
                      builder: (_) => SupportPage(
                        authorizationHeader: widget.authorizationHeader,
                        apiBaseUrl: _base,
                        targetType: _community
                            ? 'community_message'
                            : 'activity_message',
                        targetID: message['id'] as String,
                      ),
                    ),
                  );
                }
              },
              itemBuilder: (_) => [
                if (own || _state?['moderator'] == true)
                  PopupMenuItem(
                    value: 'remove',
                    child: Text(own ? '撤回消息' : '移除消息'),
                  ),
                if (!own)
                  const PopupMenuItem(value: 'report', child: Text('举报消息')),
              ],
            ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final token = widget.authorizationHeader();
    if (token != _token) {
      WidgetsBinding.instance.addPostFrameCallback((_) => _identityChanged());
    }
    final authorized = token != null && token == _token;
    final joined = authorized && _state?['joined'] == true;
    return Scaffold(
      appBar: AppBar(
        title: Text(_label),
        actions: [
          IconButton(
            tooltip: '刷新消息',
            onPressed: _busy || !authorized ? null : _load,
            icon: const Icon(Icons.refresh),
          ),
        ],
      ),
      body: !authorized
          ? Center(child: Text('登录后可进入$_label。'))
          : _base.isEmpty
          ? const Center(child: Text('请连接 Birdtie 交流服务。'))
          : Column(
              children: [
                Padding(
                  padding: const EdgeInsets.all(16),
                  child: Text(
                    widget.entityTitle,
                    style: Theme.of(context).textTheme.titleMedium,
                  ),
                ),
                if (_busy) const LinearProgressIndicator(),
                if (_error != null)
                  Padding(
                    padding: const EdgeInsets.all(12),
                    child: TextButton(
                      onPressed: _busy ? null : _load,
                      child: Text(_error!),
                    ),
                  ),
                if (!joined)
                  Expanded(
                    child: ListView(
                      padding: const EdgeInsets.all(20),
                      children: [
                        Text(_notice),
                        const SizedBox(height: 16),
                        if (_state?['canJoin'] == true)
                          FilledButton(
                            onPressed: _busy
                                ? null
                                : () =>
                                      _change('POST', '', {'confirmed': true}),
                            child: const Text('同意并加入交流'),
                          ),
                        if (_state != null && _state?['canJoin'] != true)
                          Text(
                            _community ? '社群目前不接受交流加入。' : '活动已结束或取消，无法新加入交流。',
                          ),
                      ],
                    ),
                  )
                else ...[
                  if (_state?['canSend'] != true)
                    Padding(
                      padding: const EdgeInsets.all(12),
                      child: Text(
                        _community ? '社群交流目前不可发言。' : '活动已结束或取消，可查看历史，不能继续发言。',
                      ),
                    ),
                  Expanded(
                    child: ListView(
                      children: [
                        if (_hasMore)
                          TextButton(
                            onPressed: _busy
                                ? null
                                : () => _load(earlier: true),
                            child: const Text('查看更早的消息'),
                          ),
                        if (_messages.isEmpty)
                          Padding(
                            padding: const EdgeInsets.all(24),
                            child: Text(
                              _community
                                  ? '还没有消息，聊聊社群近况吧。'
                                  : '还没有消息，聊聊集合时间或活动安排吧。',
                            ),
                          ),
                        ..._messages.map(_message),
                      ],
                    ),
                  ),
                  TextButton(
                    onPressed: _busy
                        ? null
                        : () => _confirmChange('退出$_label？', 'DELETE', ''),
                    child: const Text('退出交流'),
                  ),
                  if (_state?['canSend'] == true)
                    SafeArea(
                      top: false,
                      child: Padding(
                        padding: const EdgeInsets.all(12),
                        child: Row(
                          crossAxisAlignment: CrossAxisAlignment.end,
                          children: [
                            Expanded(
                              child: TextField(
                                controller: _composer,
                                enabled: !_busy,
                                minLines: 1,
                                maxLines: 4,
                                decoration: InputDecoration(
                                  hintText: _community ? '聊聊社群近况…' : '聊聊活动安排…',
                                  border: const OutlineInputBorder(),
                                ),
                              ),
                            ),
                            const SizedBox(width: 8),
                            FilledButton(
                              onPressed: _busy ? null : _send,
                              child: const Text('发送'),
                            ),
                          ],
                        ),
                      ),
                    ),
                ],
              ],
            ),
    );
  }
}

class _ChatFailure implements Exception {
  const _ChatFailure(this.code);
  final int code;
}
