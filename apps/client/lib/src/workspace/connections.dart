import 'connection_request_decision_operation.dart';
import 'chat_message_operation.dart';
import 'chat_message_pending_store.dart';
import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';
import 'package:http/http.dart' as http;

import '../auth/birdtie_auth_controller.dart';
import '../config/birdtie_environment.dart';
import 'support_page.dart';
import 'social_intent_drafts.dart';
import 'entity_share_pending_store.dart';
import 'chat_entity_router.dart';
import 'notification_destination_router.dart';
import 'supplier_profile_api.dart' show supplierStamp;
import 'entity_action_contract.dart';
import 'connection_request_review_page.dart';

String _requestState(String state) => switch (state) {
  'pending' => '待处理',
  'accepted' => '已接受',
  'declined' => '已拒绝',
  'withdrawn' => '已撤回',
  'expired' => '已过期',
  _ => '状态待核实',
};

class ContactRequest {
  const ContactRequest({
    required this.id,
    required this.direction,
    required this.otherAccountId,
    required this.otherName,
    required this.note,
    required this.state,
    required this.conversationId,
    required this.scope,
    this.policyDisposition = '',
    this.screeningStatus = '',
    this.expiresAt,
    this.createdAt,
  });
  final String id;
  final String direction;
  final String otherAccountId;
  final String otherName;
  final String note;
  final String state;
  final String conversationId;
  final String scope;
  final String policyDisposition;
  final String screeningStatus;
  final DateTime? expiresAt;
  final DateTime? createdAt;
  bool get pendingReview => direction == 'incoming' && state == 'pending' &&
      policyDisposition == 'SCREEN' && screeningStatus == 'PENDING_REVIEW';

  factory ContactRequest.fromJson(Map<String, dynamic> json) => ContactRequest(
    id: json['id'] as String,
    direction: json['direction'] as String? ?? '',
    otherAccountId: json['otherAccountId'] as String? ?? '',
    otherName: json['otherName'] as String? ?? '',
    note: json['note'] as String? ?? '',
    state: json['state'] as String? ?? '',
    conversationId: json['conversationId'] as String? ?? '',
    scope: json['scope'] as String? ?? 'conversation',
    policyDisposition: json['policyDisposition'] as String? ?? '',
    screeningStatus: json['screeningStatus'] as String? ?? '',
    expiresAt: _requestStamp(json['expiresAt']),
    createdAt: _requestStamp(json['createdAt']),
  );

  factory ContactRequest.fromReviewJson(Map<String, dynamic> value) {
    final r = ContactRequest.fromJson(value);
    if (value.keys.any((k) => !_requestWireFields.contains(k)) ||
        !chatEntityUUID.hasMatch(r.id) ||
        !chatEntityUUID.hasMatch(r.otherAccountId) ||
        !const {'incoming', 'outgoing'}.contains(r.direction) ||
        !const {'friend', 'conversation'}.contains(r.scope) ||
        !const {'pending', 'accepted', 'declined', 'withdrawn', 'expired'}.contains(r.state) ||
        !const {'', 'REQUEST', 'SCREEN'}.contains(r.policyDisposition) ||
        !const {'', 'PENDING_REVIEW'}.contains(r.screeningStatus) ||
        r.screeningStatus == 'PENDING_REVIEW' &&
            (r.state != 'pending' || r.policyDisposition != 'SCREEN') ||
        r.state == 'pending' && r.policyDisposition == 'SCREEN' &&
            r.screeningStatus != 'PENDING_REVIEW' ||
        r.expiresAt == null || r.createdAt == null ||
        !r.expiresAt!.isAfter(r.createdAt!) ||
        r.note.length > 1000 || r.otherName.isEmpty || r.otherName.length > 500 ||
        r.conversationId.isNotEmpty && !chatEntityUUID.hasMatch(r.conversationId)) {
      throw const FormatException('申请格式不符，请重新核实');
    }
    return r;
  }
}

const _requestWireFields = {'id','direction','otherAccountId','otherName','cityId',
  'note','scope','state','conversationId','policyDisposition','screeningStatus','createdAt','expiresAt'};

DateTime? _requestStamp(dynamic value) {
  if (value == null) return null;
  try { return supplierStamp(value); } catch (_) { return null; }
}

class ConnectionReviewException implements Exception {
  const ConnectionReviewException(this.status, {this.errorCode});
  final int status;
  final String? errorCode;
}

/// The original decision DTO is deliberately smaller than the list DTO.
class ConnectionDecisionReceipt {
  const ConnectionDecisionReceipt(this.id, this.state, this.conversationID);
  final String id, state, conversationID;
}

class FriendTie {
  const FriendTie({
    required this.id,
    required this.otherAccountId,
    required this.otherName,
  });
  final String id;
  final String otherAccountId;
  final String otherName;

  factory FriendTie.fromJson(Map<String, dynamic> json) => FriendTie(
    id: json['id'] as String,
    otherAccountId: json['otherAccountId'] as String,
    otherName: json['otherName'] as String,
  );
}

class HumanConversation {
  const HumanConversation({
    required this.id,
    required this.otherAccountId,
    required this.otherName,
    this.unreadCount = 0,
  });
  final String id;
  final String otherAccountId;
  final String otherName;
  final int unreadCount;

  factory HumanConversation.fromJson(Map<String, dynamic> json) =>
      HumanConversation(
        id: json['id'] as String,
        otherAccountId: json['otherAccountId'] as String,
        otherName: json['otherName'] as String,
        unreadCount: json['unreadCount'] as int? ?? 0,
      );
}

class HumanMessage {
  const HumanMessage({
    required this.id,
    required this.senderAccountId,
    required this.body,
    required this.createdAt,
    this.entity,
  });
  final String id;
  final String senderAccountId;
  final String body;
  final DateTime createdAt;
  final ChatEntityCard? entity;

  factory HumanMessage.fromJson(Map<String, dynamic> json) => HumanMessage(
    id: json['id'] as String,
    senderAccountId: json['senderAccountId'] as String,
    body: json['body'] as String,
    createdAt: DateTime.parse(json['createdAt'] as String),
    entity: json['entity'] is Map<String, dynamic>
        ? ChatEntityCard.fromJson(json['entity'] as Map<String, dynamic>)
        : null,
  );
}

class ChatEntityCard {
  const ChatEntityCard({
    required this.type,
    required this.available,
    this.id,
    this.title,
  });
  final String type;
  final bool available;
  final String? id;
  final String? title;

  factory ChatEntityCard.fromJson(Map<String, dynamic> json) => ChatEntityCard(
    type: json['type'] as String,
    available: json['available'] as bool? ?? false,
    id: json['id'] as String?,
    title: json['title'] as String?,
  );

  String get typeLabel => switch (type) {
    'activity' => '活动',
    'place' => '地点',
    'person' => '用户',
    'community' => '社群',
    'organization' => '组织',
    'business' => '商家',
    'moment' => '动态',
    _ => '内容',
  };
}

Object? _activeEntityShareToken;

/// Recipient selection is a draft; only the concrete preview can send.
Future<void> shareEntityToChat(
  BuildContext context, {
  required String? Function() authorizationHeader,
  required String type,
  required String id,
  String? apiBaseUrl,
  http.Client? client,
  Listenable? identityChanges,
  String? Function()? workspaceID,
  EntitySharePendingStore? pendingStore,
}) async {
  if (authorizationHeader() == null || workspaceID?.call() != null) {
    ScaffoldMessenger.of(
      context,
    ).showSnackBar(const SnackBar(content: Text('请使用已登录的个人身份给好友分享。')));
    return;
  }
  if (!chatEntityTypes.contains(type) || !chatEntityUUID.hasMatch(id)) return;
  if (_activeEntityShareToken != null) return;
  final flowToken = Object();
  _activeEntityShareToken = flowToken;
  try {
    await showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      builder: (_) => _EntityShareSheet(
        authorizationHeader: authorizationHeader,
        type: type,
        id: id,
        apiBaseUrl: apiBaseUrl,
        client: client,
        identityChanges: identityChanges,
        workspaceID: workspaceID,
        pendingStore: pendingStore ?? const SecureEntitySharePendingStore(),
        flowToken: flowToken,
      ),
    );
  } finally {
    if (identical(_activeEntityShareToken, flowToken)) {
      _activeEntityShareToken = null;
    }
  }
}

class _ShareRecipient {
  const _ShareRecipient(
    this.accountID,
    this.name, {
    this.conversationID,
    this.tieID,
  });
  final String accountID, name;
  final String? conversationID, tieID;
}

class _EntityShareSheet extends StatefulWidget {
  const _EntityShareSheet({
    required this.authorizationHeader,
    required this.type,
    required this.id,
    required this.pendingStore,
    required this.flowToken,
    this.apiBaseUrl,
    this.client,
    this.identityChanges,
    this.workspaceID,
  });
  final String? Function() authorizationHeader;
  final String type, id;
  final String? apiBaseUrl;
  final http.Client? client;
  final Listenable? identityChanges;
  final String? Function()? workspaceID;
  final EntitySharePendingStore pendingStore;
  final Object flowToken;
  @override
  State<_EntityShareSheet> createState() => _EntityShareSheetState();
}

class _EntityShareSheetState extends State<_EntityShareSheet> {
  late final ConnectionSource _source;
  late final (String?, String?) _identity;
  List<_ShareRecipient> _recipients = [];
  List<PendingEntityShare> _pending = [];
  String? _owner, _title, _error, _result;
  bool _loading = true, _busy = false, _expired = false;
  bool _newShareReady = false;
  int _epoch = 0;
  (String?, String?) get _current =>
      (widget.authorizationHeader(), widget.workspaceID?.call());
  bool get _valid => mounted && !_expired && _identity == _current;
  @override
  void initState() {
    super.initState();
    _identity = _current;
    _source = ConnectionSource(
      authorizationHeader: () => _valid ? _identity.$1 : null,
      client: widget.client,
      apiBaseUrl: widget.apiBaseUrl,
    );
    widget.identityChanges?.addListener(_changed);
    unawaited(_load());
  }

  void _changed() {
    if (_identity == _current || _expired) return;
    ++_epoch;
    setState(() => _expired = true);
  }

  @override
  void didUpdateWidget(_EntityShareSheet old) {
    super.didUpdateWidget(old);
    if (old.id != widget.id ||
        old.type != widget.type ||
        old.authorizationHeader != widget.authorizationHeader) {
      ++_epoch;
      _expired = true;
    }
  }

  Future<void> _load() async {
    final epoch = ++_epoch;
    setState(() {
      _loading = true;
      _newShareReady = false;
      _title = null;
    });
    try {
      final owner = await _source.currentPersonID();
      if (!_valid || epoch != _epoch) return;
      final pending = await widget.pendingStore.read(
        _source.environment,
        owner,
      );
      if (!_valid || epoch != _epoch) return;
      final conversations = await _source.conversations();
      if (!_valid || epoch != _epoch) return;
      final ties = await _source.ties();
      if (!_valid || epoch != _epoch) return;
      final list = <String, _ShareRecipient>{};
      for (final c in conversations) {
        if (!chatEntityUUID.hasMatch(c.id) ||
            !chatEntityUUID.hasMatch(c.otherAccountId)) {
          throw const FormatException();
        }
        list[c.otherAccountId] = _ShareRecipient(
          c.otherAccountId,
          c.otherName,
          conversationID: c.id,
        );
      }
      for (final tie in ties) {
        if (!chatEntityUUID.hasMatch(tie.id) ||
            !chatEntityUUID.hasMatch(tie.otherAccountId)) {
          throw const FormatException();
        }
        list.putIfAbsent(
          tie.otherAccountId,
          () =>
              _ShareRecipient(tie.otherAccountId, tie.otherName, tieID: tie.id),
        );
      }
      // An unknown result must remain findable even if its source was withdrawn.
      String? title;
      try {
        title = await _source.shareTargetTitle(widget.type, widget.id);
      } catch (_) {
        if (pending.isEmpty) rethrow;
      }
      if (!_valid || epoch != _epoch) return;
      setState(() {
        _owner = owner;
        _pending = pending;
        _recipients = list.values.toList();
        _title = title;
        _error = null;
        _newShareReady = title != null;
      });
    } catch (_) {
      if (_valid && epoch == _epoch) {
        setState(() => _error = '好友、分享恢复记录或当前内容暂不可读取。请重试。');
      }
    } finally {
      if (_valid && epoch == _epoch) setState(() => _loading = false);
    }
  }

  String _recipientName(String conversationID) {
    for (final r in _recipients) {
      if (r.conversationID == conversationID) return r.name;
    }
    return '原私信会话';
  }

  Future<bool> _confirm(
    String recipient,
    String title,
    String type, {
    bool retry = false,
  }) async {
    final epoch = _epoch;
    final answer = await showDialog<bool>(
      useRootNavigator: false,
      context: context,
      builder: (c) => AlertDialog(
        scrollable: true,
        title: Text(retry ? '重试原分享' : '确认发送卡片'),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text('以当前个人身份发送'),
            const SizedBox(height: 12),
            Text('收件人：$recipient'),
            const SizedBox(height: 12),
            Text(
              '${ChatEntityCard(type: type, available: true).typeLabel}：$title',
            ),
            const SizedBox(height: 12),
            Text(
              retry
                  ? '仍使用原操作编号；即使上次已提交也不会重复发送。'
                  : '发送引用卡片，不包含私人位置、记忆或匹配理由。对方是否可查看由当前权限决定。',
            ),
          ],
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(c, false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(c, true),
            child: Text(retry ? '确认重试原分享' : '确认发送'),
          ),
        ],
      ),
    );
    return answer == true && _valid && epoch == _epoch;
  }

  Future<void> _share(_ShareRecipient recipient) async {
    if (_busy ||
        _loading ||
        !_newShareReady ||
        !_valid ||
        _owner == null ||
        _title == null) {
      return;
    }
    setState(() => _busy = true);
    try {
      // A prior unknown operation to this recipient must be reconciled first.
      if (_pending.any(
        (p) =>
            p.conversationID == recipient.conversationID &&
            p.type == widget.type &&
            p.entityID == widget.id,
      )) {
        setState(() => _error = '这张卡片有待核实的原分享。请先核实原结果。');
        return;
      }
      if (!await _confirm(recipient.name, _title!, widget.type)) return;
      // A new concrete approval starts a new result lifecycle. The previous
      // operation's success must not describe this pending or failed attempt.
      setState(() {
        _result = null;
        _error = null;
      });
      HumanConversation? conversation;
      if (recipient.conversationID == null) {
        conversation = await _source.startFriendChat(recipient.tieID!);
        if (!_valid) return;
        if (conversation.otherAccountId != recipient.accountID ||
            !chatEntityUUID.hasMatch(conversation.id)) {
          throw const FormatException('好友会话不符');
        }
        final opened = conversation;
        setState(
          () => _recipients = [
            for (final r in _recipients)
              if (r.accountID == recipient.accountID)
                _ShareRecipient(r.accountID, r.name, conversationID: opened.id)
              else
                r,
          ],
        );
      }
      final resolvedConversation = recipient.conversationID ?? conversation!.id;
      final currentPending = await widget.pendingStore.read(
        _source.environment,
        _owner!,
      );
      if (!_valid) return;
      setState(() => _pending = currentPending);
      if (currentPending.any(
        (p) =>
            p.conversationID == resolvedConversation &&
            p.type == widget.type &&
            p.entityID == widget.id,
      )) {
        setState(() => _error = '这张卡片有待核实的原分享。请先核实原结果。');
        return;
      }
      final value = PendingEntityShare(
        operationID: newEntityShareOperationID(),
        conversationID: resolvedConversation,
        type: widget.type,
        entityID: widget.id,
      );
      await widget.pendingStore.write(_source.environment, _owner!, value);
      if (!_valid) return;
      setState(() => _pending = [..._pending, value]);
      try {
        final receipt = await _source.submitEntityShare(value, _owner!);
        if (!_valid) return;
        await _accepted(value, receipt);
      } catch (_) {
        if (_valid) setState(() => _error = '分享结果待核实，可能已发送。请核实原结果，不要重新发送。');
      }
    } catch (_) {
      if (_valid) setState(() => _error = '未获确认。请刷新好友或内容，再检查当前操作。');
    } finally {
      if (_valid) setState(() => _busy = false);
    }
  }

  Future<void> _accepted(PendingEntityShare value, HumanMessage message) async {
    // Clear only after an authoritative original-message receipt, never on timeout.
    await widget.pendingStore.delete(
      _source.environment,
      _owner!,
      value.operationID,
    );
    if (!_valid) return;
    setState(() {
      _pending.removeWhere((p) => p.operationID == value.operationID);
      _error = null;
      _result = message.entity?.available == true
          ? '已发送到私信。可在消息中找回。'
          : '已发送到私信；内容当前已不可查看。';
    });
  }

  Future<void> _recover(PendingEntityShare value, {bool retry = false}) async {
    if (_busy || !_valid || _owner == null) return;
    setState(() {
      _busy = true;
      _result = null;
      _error = null;
    });
    try {
      final receipt = await _source.recoverEntityShare(value, _owner!);
      if (!_valid) return;
      if (receipt != null) {
        await _accepted(value, receipt);
        return;
      }
      if (!retry) {
        setState(() => _error = '当前未查到原回执，不等于未发送。可稍后再次核实，或检查后重试原操作。');
        return;
      }
      final recipients = _recipients
          .where((r) => r.conversationID == value.conversationID)
          .toList();
      if (recipients.length != 1) {
        setState(() => _error = '暂不能核实具体收件人。请先在个人消息中找到原会话；这里只核实原结果，不重新发送。');
        return;
      }
      final title = await _source.shareTargetTitle(value.type, value.entityID);
      if (!_valid ||
          !await _confirm(
            '${recipients.single.name} · 账号 ${recipients.single.accountID.substring(recipients.single.accountID.length - 6)}',
            title,
            value.type,
            retry: true,
          )) {
        return;
      }
      final sent = await _source.submitEntityShare(value, _owner!);
      if (_valid) await _accepted(value, sent);
    } catch (_) {
      if (_valid) setState(() => _error = '原分享结果仍待核实。恢复记录已保留，不会自动续发。');
    } finally {
      if (_valid) setState(() => _busy = false);
    }
  }

  @override
  void dispose() {
    ++_epoch;
    if (identical(_activeEntityShareToken, widget.flowToken)) {
      _activeEntityShareToken = null;
    }
    widget.identityChanges?.removeListener(_changed);
    _source.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => SafeArea(
    child: SizedBox(
      height: MediaQuery.sizeOf(context).height * .78,
      child: ListView(
        padding: const EdgeInsets.all(24),
        children: [
          Text('发给好友', style: Theme.of(context).textTheme.titleLarge),
          const SizedBox(height: 12),
          if (_expired)
            const Text('工作身份已变化。请关闭后重新选择收件人；原分享不会自动续发。')
          else ...[
            if (_loading) const LinearProgressIndicator(),
            if (_title != null)
              Text(
                '${ChatEntityCard(type: widget.type, available: true).typeLabel}：$_title',
              ),
            if (_result != null)
              Semantics(liveRegion: true, child: Text(_result!)),
            if (_error != null) ...[
              Semantics(liveRegion: true, child: Text(_error!)),
              if (!_busy)
                TextButton(onPressed: _load, child: const Text('刷新当前状态')),
            ],
            if (_pending.isNotEmpty) ...[
              const SizedBox(height: 24),
              Text('待核实的原分享', style: Theme.of(context).textTheme.titleMedium),
              for (final p in _pending)
                Padding(
                  padding: const EdgeInsets.symmetric(vertical: 8),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        '${_recipientName(p.conversationID)} · ${ChatEntityCard(type: p.type, available: true).typeLabel}',
                      ),
                      Wrap(
                        spacing: 8,
                        children: [
                          TextButton(
                            onPressed: _busy ? null : () => _recover(p),
                            child: const Text('核实原结果'),
                          ),
                          TextButton(
                            onPressed: _busy
                                ? null
                                : () => _recover(p, retry: true),
                            child: const Text('检查并重试原分享'),
                          ),
                        ],
                      ),
                    ],
                  ),
                ),
            ],
            if (!_loading && _recipients.isEmpty)
              const Text('暂无已接受好友或获准私信。请先建立联系。'),
            if (_title != null) ...[
              const SizedBox(height: 24),
              const Text('选择收件人后还需检查并确认发送。'),
              for (final r in _recipients)
                ListTile(
                  title: Text(r.name),
                  subtitle: Text(
                    '个人账号 · ${r.accountID.substring(r.accountID.length - 6)}',
                  ),
                  leading: const Icon(Icons.person_outline),
                  enabled: !_busy && !_loading && _newShareReady,
                  onTap: () => _share(r),
                ),
            ],
          ],
          const SizedBox(height: 12),
          TextButton(
            onPressed: () => Navigator.pop(context),
            child: const Text('关闭'),
          ),
        ],
      ),
    ),
  );
}

class ConnectionSource {
  ConnectionSource({
    required this.authorizationHeader,
    http.Client? client,
    String? apiBaseUrl,
  }) : _client = client ?? http.Client(),
       _ownsClient = client == null,
       _apiBaseUrl = apiBaseUrl ?? apiBase;

  static const apiBase = BirdtieEnvironment.apiBaseUrl;
  final String? Function() authorizationHeader;
  final http.Client _client;
  http.Client get followClient => _client;
  String get followApiBaseUrl => _apiBaseUrl;
  final bool _ownsClient;
  final String _apiBaseUrl;
  bool _closed = false;
  bool get isClosed => _closed;
  bool get configured => _apiBaseUrl.isNotEmpty;
  String get environment => _apiBaseUrl.replaceFirst(RegExp(r'/$'), '');

  Future<String> currentPersonID() async {
    final response = await _client
        .get(_endpoint('/v1/me'), headers: _headers())
        .timeout(const Duration(seconds: 12));
    if (response.statusCode != 200) throw StateError('请重新登录');
    final data =
        (jsonDecode(utf8.decode(response.bodyBytes))
            as Map<String, dynamic>)['data'];
    if (data is! Map<String, dynamic> ||
        data['accountType'] != 'person' ||
        data['id'] is! String ||
        !chatEntityUUID.hasMatch(data['id'])) {
      throw const FormatException('当前个人身份不符');
    }
    return data['id'] as String;
  }

  Future<String> shareTargetTitle(String type, String id) async {
    if (!chatEntityTypes.contains(type) || !chatEntityUUID.hasMatch(id)) {
      throw const FormatException('卡片标识不符');
    }
    final path = switch (type) {
      'person' => '/v1/accounts/$id/profile',
      'activity' => '/v1/activities/$id',
      'place' => '/v1/places/$id',
      'community' => '/v1/communities/$id',
      'organization' => '/v1/organizations/$id',
      'business' => '/v1/businesses/$id',
      _ => '/v1/moments/$id',
    };
    final response = await _client
        .get(_endpoint(path), headers: _headers())
        .timeout(const Duration(seconds: 12));
    if (response.statusCode != 200) throw StateError('内容暂不可分享');
    final data =
        (jsonDecode(utf8.decode(response.bodyBytes))
            as Map<String, dynamic>)['data'];
    final idKey = type == 'person' ? 'accountId' : 'id';
    final titleKey = switch (type) {
      'person' => 'displayName',
      'activity' || 'moment' => 'title',
      _ => 'name',
    };
    if (data is! Map<String, dynamic> ||
        data[idKey] != id ||
        data[titleKey] is! String ||
        (data[titleKey] as String).trim().isEmpty ||
        type == 'person' && data['visibility'] != 'public') {
      throw const FormatException('公开目标不符');
    }
    return data[titleKey] as String;
  }

  Future<HumanMessage?> recoverEntityShare(
    PendingEntityShare value,
    String ownerID,
  ) async {
    final response = await _client
        .get(
          _endpoint(
            '/v1/me/conversations/${value.conversationID}/entity-shares/${value.operationID}',
          ),
          headers: _headers(),
        )
        .timeout(const Duration(seconds: 12));
    if (response.statusCode == 404) return null;
    if (response.statusCode != 200) throw StateError('分享结果暂时无法核实');
    return _entityReceipt(response, value, ownerID);
  }

  Future<HumanMessage> submitEntityShare(
    PendingEntityShare value,
    String ownerID,
  ) async {
    PendingEntityShare.fromJson(value.toJson());
    final response = await _client
        .post(
          _endpoint(
            '/v1/me/conversations/${value.conversationID}/entity-shares',
          ),
          headers: _headers(json: true),
          body: jsonEncode({
            'operationId': value.operationID,
            'entity': {'type': value.type, 'id': value.entityID},
          }),
        )
        .timeout(const Duration(seconds: 12));
    if (response.statusCode != 200 && response.statusCode != 201) {
      throw StateError('分享未获确认，请核实原操作');
    }
    return _entityReceipt(response, value, ownerID);
  }

  HumanMessage _entityReceipt(
    http.Response response,
    PendingEntityShare expected,
    String ownerID,
  ) {
    final envelope = jsonDecode(utf8.decode(response.bodyBytes));
    final data = envelope is Map<String, dynamic> ? envelope['data'] : null;
    if (data is! Map<String, dynamic> ||
        data.length != 2 ||
        data.keys.toSet().difference({'operationId', 'message'}).isNotEmpty ||
        data['operationId'] != expected.operationID ||
        data['message'] is! Map<String, dynamic>) {
      throw const FormatException('分享回执不符');
    }
    final m = data['message'] as Map<String, dynamic>;
    const messageKeys = {
      'id',
      'conversationId',
      'senderAccountId',
      'speakerKind',
      'body',
      'entity',
      'createdAt',
    };
    if (m.length != messageKeys.length ||
        m.keys.toSet().difference(messageKeys).isNotEmpty ||
        m['conversationId'] != expected.conversationID ||
        m['senderAccountId'] != ownerID ||
        m['speakerKind'] != 'human' ||
        m['body'] != '分享了一张卡片' ||
        m['id'] is! String ||
        !chatEntityUUID.hasMatch(m['id']) ||
        m['entity'] is! Map<String, dynamic>) {
      throw const FormatException('消息回执不符');
    }
    final e = m['entity'] as Map<String, dynamic>;
    if (e.keys.toSet().difference({
          'type',
          'available',
          'id',
          'title',
        }).isNotEmpty ||
        e['type'] != expected.type ||
        e['available'] is! bool ||
        e['available'] == true &&
            (e['id'] != expected.entityID ||
                e['title'] is! String ||
                (e['title'] as String).isEmpty) ||
        e['available'] == false &&
            (e.containsKey('id') || e.containsKey('title'))) {
      throw const FormatException('卡片回执不符');
    }
    try {
      supplierStamp(m['createdAt']);
    } catch (_) {
      throw const FormatException('消息回执日期不符');
    }
    return HumanMessage.fromJson(m);
  }

  Uri _endpoint(String path) =>
      Uri.parse('${_apiBaseUrl.replaceFirst(RegExp(r'/$'), '')}$path');
  Map<String, String> _headers({bool json = false}) {
    if (_closed) throw StateError('个人消息连接已关闭');
    final token = authorizationHeader();
    if (token == null) throw StateError('Sign in to contact people');
    return {
      'Authorization': token,
      if (json) 'Content-Type': 'application/json',
    };
  }

  Future<void> request(
    String recipientAccountId,
    String cityId,
    String note, {
    EntityActionDescriptor? approved,
  }) async {
    if (approved != null &&
        (approved.target != EntityActionRef('person', recipientAccountId) ||
            approved.kind != EntityActionKind.connect ||
            approved.operation != 'REQUEST_CONVERSATION')) {
      throw StateError('私信申请对象或操作已变化。');
    }
    final response = await _client
        .post(
          _endpoint('/v1/me/connection-requests'),
          headers: {
            ..._headers(json: true),
            if (approved != null) ...approved.conditionHeaders,
          },
          body: jsonEncode({
            'recipientAccountId': recipientAccountId,
            'cityId': cityId,
            'note': note,
          }),
        )
        .timeout(const Duration(seconds: 12));
    if (response.statusCode != 201) {
      throw StateError('Contact request unavailable: ${response.statusCode}');
    }
  }

  Future<void> requestFriend(
    String recipientAccountId,
    String note, {
    EntityActionDescriptor? approved,
  }) async {
    if (approved != null &&
        (approved.target != EntityActionRef('person', recipientAccountId) ||
            approved.kind != EntityActionKind.connect ||
            approved.operation != 'REQUEST_FRIEND')) {
      throw StateError('好友申请对象或操作已变化。');
    }
    final response = await _client
        .post(
          _endpoint('/v1/me/connection-requests'),
          headers: {
            ..._headers(json: true),
            if (approved != null) ...approved.conditionHeaders,
          },
          body: jsonEncode({
            'recipientAccountId': recipientAccountId,
            'scope': 'friend',
            'note': note,
          }),
        )
        .timeout(const Duration(seconds: 12));
    if (response.statusCode != 201) {
      throw StateError('好友申请暂不可用：${response.statusCode}');
    }
  }

  Future<List<FriendTie>> ties() async {
    final response = await _client
        .get(_endpoint('/v1/me/ties'), headers: _headers())
        .timeout(const Duration(seconds: 12));
    if (response.statusCode != 200) throw StateError('好友列表暂不可用');
    final rows =
        (jsonDecode(utf8.decode(response.bodyBytes))
                as Map<String, dynamic>)['data']
            as List<dynamic>;
    return [
      for (final row in rows) FriendTie.fromJson(row as Map<String, dynamic>),
    ];
  }

  Future<void> removeTie(String id) async {
    final response = await _client
        .delete(
          _endpoint('/v1/me/ties/${Uri.encodeComponent(id)}'),
          headers: _headers(),
        )
        .timeout(const Duration(seconds: 12));
    if (response.statusCode != 204) throw StateError('移除好友失败');
  }

  Future<void> blockAccount(String accountId) async {
    final response = await _client
        .post(
          _endpoint('/v1/me/blocks'),
          headers: _headers(json: true),
          body: jsonEncode({'accountId': accountId}),
        )
        .timeout(const Duration(seconds: 12));
    if (response.statusCode != 204) throw StateError('屏蔽账号失败');
  }

  Future<HumanConversation> startFriendChat(
    String tieId, {
    EntityActionDescriptor? approved,
  }) async {
    if (approved != null &&
        (approved.target.type != 'person' ||
            approved.kind != EntityActionKind.message ||
            approved.operation != 'OPEN_CHAT')) {
      throw StateError('好友会话对象或操作已变化。');
    }
    final response = await _client
        .post(
          _endpoint('/v1/me/ties/${Uri.encodeComponent(tieId)}/conversation'),
          headers: {
            ..._headers(),
            if (approved != null) ...approved.conditionHeaders,
          },
        )
        .timeout(const Duration(seconds: 12));
    if (response.statusCode != 200) throw StateError('暂时无法开启私信');
    final data =
        (jsonDecode(utf8.decode(response.bodyBytes))
                as Map<String, dynamic>)['data']
            as Map<String, dynamic>;
    return HumanConversation.fromJson(data);
  }

  Future<List<ContactRequest>> requests() async {
    final response = await _client
        .get(_endpoint('/v1/me/connection-requests'), headers: _headers())
        .timeout(const Duration(seconds: 12));
    if (response.statusCode != 200) throw StateError('Requests unavailable');
    final rows =
        (jsonDecode(utf8.decode(response.bodyBytes))
                as Map<String, dynamic>)['data']
            as List<dynamic>;
    return [
      for (final row in rows)
        ContactRequest.fromJson(row as Map<String, dynamic>),
    ];
  }

  Future<void> decide(String id, String action) async {
    final response = await _client
        .post(
          _endpoint(
            '/v1/me/connection-requests/${Uri.encodeComponent(id)}/decision',
          ),
          headers: _headers(json: true),
          body: jsonEncode({'action': action}),
        )
        .timeout(const Duration(seconds: 12));
    if (response.statusCode != 200) throw StateError('Decision unavailable');
  }

  Future<List<ContactRequest>> reviewRequests() async {
    final response = await _client.get(
      _endpoint('/v1/me/connection-requests'), headers: _headers(),
    ).timeout(const Duration(seconds: 12));
    if (response.statusCode != 200) throw ConnectionReviewException(response.statusCode);
    final root = jsonDecode(utf8.decode(response.bodyBytes));
    if (root is! Map<String, dynamic> || root['data'] is! List ||
        (root['data'] as List).length > 100) {
      throw const FormatException('申请列表不符');
    }
    return [for (final row in root['data'] as List)
      ContactRequest.fromReviewJson(row as Map<String, dynamic>)];
  }

  Future<ConnectionDecisionReceipt> decideReviewed(ContactRequest original, String action) async {
    final expected = const {'accept': 'accepted', 'decline': 'declined', 'withdraw': 'withdrawn'}[action];
    if (expected == null || !chatEntityUUID.hasMatch(original.id)) {
      throw const FormatException('申请操作不符');
    }
    final response = await _client.post(
      _endpoint('/v1/me/connection-requests/${original.id}/decision'),
      headers: _headers(json: true), body: jsonEncode({'action': action}),
    ).timeout(const Duration(seconds: 12));
    if (response.statusCode != 200) {
      // Only closed original error codes; never retain a server message/body.
      String? code;
      try {
        if (response.bodyBytes.length <= 4096) {
          final failure = jsonDecode(utf8.decode(response.bodyBytes));
          final error = failure is Map<String, dynamic> ? failure['error'] : null;
          if (error is Map<String, dynamic> && const {
            'connection_conflict', 'invalid_decision', 'message_policy_changed',
          }.contains(error['code'])) {
            code = error['code'] as String;
          }
        }
      } catch (_) { /* An unknown error is not evidence that no write occurred. */ }
      throw ConnectionReviewException(response.statusCode, errorCode: code);
    }
    final root = jsonDecode(utf8.decode(response.bodyBytes));
    if (root is! Map<String, dynamic> || root['data'] is! Map<String, dynamic>) {
      throw const FormatException('申请回执不符');
    }
    final v = root['data'] as Map<String, dynamic>, chat = v['conversationId'];
    final cid = chat is String ? chat : '';
    if (v.keys.any((k) => !_requestWireFields.contains(k)) ||
        v['id'] != original.id || v['state'] != expected ||
        v['direction'] != null && v['direction'] != '' && v['direction'] != original.direction ||
        v['otherAccountId'] != null && v['otherAccountId'] != '' && v['otherAccountId'] != original.otherAccountId ||
        v['otherName'] != null && v['otherName'] != '' && v['otherName'] != original.otherName ||
        v['scope'] != original.scope || v['note'] != original.note ||
        _requestStamp(v['createdAt']) != original.createdAt ||
        _requestStamp(v['expiresAt']) != original.expiresAt ||
        original.scope == 'friend' && cid.isNotEmpty ||
        original.scope == 'conversation' && expected == 'accepted' && !chatEntityUUID.hasMatch(cid) ||
        expected != 'accepted' && cid.isNotEmpty) {
      throw const FormatException('申请回执与本次操作不符');
    }
    return ConnectionDecisionReceipt(original.id, expected, cid);
  }

  Future<ConnectionRequestOperationReceipt> decideReviewedOperation(
    String owner, ContactRequest original, String action, String operation,
  ) async {
    connectionDecisionDigest(owner,original.id,action);
    if (!chatEntityUUID.hasMatch(operation)) throw const FormatException('服务操作编号不符');
    final response = await _client.post(
      _endpoint('/v1/me/connection-requests/${original.id}/decision'),
      headers: _headers(json: true),
      body: jsonEncode({'action':action,'operationId':operation}),
    ).timeout(const Duration(seconds:12));
    return _operationReceipt(response,owner,original.id,operation,action,original.scope);
  }

  Future<ConnectionRequestOperationReceipt> readDecisionOperation(
    String owner, String request, String operation, String action, String scope,
  ) async {
    connectionDecisionDigest(owner,request,action);
    if (!chatEntityUUID.hasMatch(operation)) throw const FormatException('服务操作编号不符');
    final response = await _client.get(
      _endpoint('/v1/me/connection-requests/$request/decision-operations/$operation'),
      headers: _headers(),
    ).timeout(const Duration(seconds:12));
    return _operationReceipt(response,owner,request,operation,action,scope);
  }

  ConnectionRequestOperationReceipt _operationReceipt(http.Response response,
    String owner,String request,String operation,String action,String scope) {
    if (response.statusCode != 200) {
      String? code;
      try {
        if(response.bodyBytes.length<=4096){
          final root=jsonDecode(utf8.decode(response.bodyBytes));
          final e=root is Map<String,dynamic>?root['error']:null;
          if(e is Map<String,dynamic> && const {'connection_operation_changed','invalid_decision','invalid_operation_id'}.contains(e['code'])) code=e['code'];
        }
      } catch (_) { /* Unknown errors are not causal receipts. */ }
      throw ConnectionReviewException(response.statusCode,errorCode:code);
    }
    if(response.bodyBytes.length>4096) throw const FormatException('服务回执过大');
    final root=jsonDecode(utf8.decode(response.bodyBytes));
    if(root is! Map<String,dynamic> || root.length!=1 || root['data'] is! Map<String,dynamic>) throw const FormatException('服务回执格式不符');
    return ConnectionRequestOperationReceipt.decode(root['data'],owner:owner,
      request:request,operation:operation,action:action,scope:scope);
  }

  Future<List<HumanConversation>> conversations() async {
    final response = await _client
        .get(_endpoint('/v1/me/conversations'), headers: _headers())
        .timeout(const Duration(seconds: 12));
    if (response.statusCode != 200) {
      throw StateError('Conversations unavailable');
    }
    final rows =
        (jsonDecode(utf8.decode(response.bodyBytes))
                as Map<String, dynamic>)['data']
            as List<dynamic>;
    return [
      for (final row in rows)
        HumanConversation.fromJson(row as Map<String, dynamic>),
    ];
  }

  Future<List<HumanMessage>> messages(String conversationId) async {
    final response = await _client
        .get(
          _endpoint(
            '/v1/me/conversations/${Uri.encodeComponent(conversationId)}/messages',
          ),
          headers: _headers(),
        )
        .timeout(const Duration(seconds: 12));
    if (response.statusCode != 200) throw StateError('Messages unavailable');
    final rows =
        (jsonDecode(utf8.decode(response.bodyBytes))
                as Map<String, dynamic>)['data']
            as List<dynamic>;
    return [
      for (final row in rows)
        HumanMessage.fromJson(row as Map<String, dynamic>),
    ];
  }

  Future<void> markRead(String conversationId, String throughMessageId) async {
    final response = await _client
        .post(
          _endpoint(
            '/v1/me/conversations/${Uri.encodeComponent(conversationId)}/read',
          ),
          headers: _headers(json: true),
          body: jsonEncode({'throughMessageId': throughMessageId}),
        )
        .timeout(const Duration(seconds: 12));
    if (response.statusCode != 200) throw StateError('对话已读状态更新失败');
  }

  Future<HumanMessageOperationReceipt> sendMessageOperation(String owner,
    PendingHumanMessage pending, String body) async {
    if (humanMessagePayloadDigest(pending.conversationID,body) != pending.payloadDigest) {
      throw StateError('发送正文已变化，请重新检查。');
    }
    final response = await _client.post(
      _endpoint('/v1/me/conversations/${pending.conversationID}/message-operations'),
      headers: _headers(json:true),
      body: jsonEncode({'operationId':pending.operationID,'body':body}),
    ).timeout(const Duration(seconds:12));
    if(response.statusCode != 201) throw StateError('发送结果暂未确认，请核实原操作。');
    return HumanMessageOperationReceipt.decode(
      (jsonDecode(utf8.decode(response.bodyBytes)) as Map<String,dynamic>)['data'], owner,pending);
  }

  Future<HumanMessageOperationReceipt> readMessageOperation(String owner,
    PendingHumanMessage pending) async {
    final response = await _client.get(
      _endpoint('/v1/me/conversations/${pending.conversationID}/message-operations/${pending.operationID}'),
      headers:_headers(),
    ).timeout(const Duration(seconds:12));
    if(response.statusCode != 200) throw StateError('尚无法核实这次发送，请保留原记录后重试核实。');
    return HumanMessageOperationReceipt.decode(
      (jsonDecode(utf8.decode(response.bodyBytes)) as Map<String,dynamic>)['data'],owner,pending);
  }

  Future<void> send(String conversationId, String body) async {
    final response = await _client
        .post(
          _endpoint(
            '/v1/me/conversations/${Uri.encodeComponent(conversationId)}/messages',
          ),
          headers: _headers(json: true),
          body: jsonEncode({'body': body}),
        )
        .timeout(const Duration(seconds: 12));
    if (response.statusCode != 201) throw StateError('消息发送失败');
  }

  Future<void> sendEntity(String conversationId, String type, String id) async {
    final response = await _client
        .post(
          _endpoint(
            '/v1/me/conversations/${Uri.encodeComponent(conversationId)}/messages',
          ),
          headers: _headers(json: true),
          body: jsonEncode({
            'entity': {'type': type, 'id': id},
          }),
        )
        .timeout(const Duration(seconds: 12));
    if (response.statusCode != 201) throw StateError('卡片发送失败');
  }

  void dispose() {
    if (_closed) return;
    _closed = true;
    if (_ownsClient) _client.close();
  }
}

class SocialInboxSection extends StatefulWidget {
  const SocialInboxSection({
    super.key,
    required this.auth,
    this.workspaceChanges,
    this.workspaceID,
    this.client,
    this.apiBaseUrl,
  });
  final BirdtieAuthController auth;
  final Listenable? workspaceChanges;
  final String? Function()? workspaceID;
  final http.Client? client;
  final String? apiBaseUrl;

  @override
  State<SocialInboxSection> createState() => _SocialInboxSectionState();
}

class _SocialInboxSectionState extends State<SocialInboxSection> {
  late ConnectionSource _source;
  int _identityEpoch = 0;
  final _reviewChanges = ValueNotifier<int>(0);
  List<ContactRequest> _requests = const [];
  List<FriendTie> _ties = const [];
  List<HumanConversation> _conversations = const [];
  bool _loading = false;
  bool _failed = false;
  String? _busyID;
  int _serial = 0;
  late (String?, String?, String?) _identity;
  (String?, String?, String?) get _currentIdentity => (
    widget.auth.authorizationHeader,
    widget.auth.accountID,
    widget.workspaceID?.call(),
  );

  @override
  void initState() {
    super.initState();
    _identity = _currentIdentity;
    widget.auth.addListener(_identityChanged);
    widget.workspaceChanges?.addListener(_identityChanged);
    _source = ConnectionSource(
      authorizationHeader: () => widget.auth.authorizationHeader,
      client: widget.client,
      apiBaseUrl: widget.apiBaseUrl,
    );
    if (widget.auth.signedIn && widget.workspaceID?.call() == null && _source.configured) unawaited(_load());
  }

  void _identityChanged() {
    if (_identity == _currentIdentity) return;
    _identity = _currentIdentity;
    ++_serial;
    ++_identityEpoch;
    _reviewChanges.value++;
    setState(() {
      _requests = const [];
      _ties = const [];
      _conversations = const [];
      _loading = false;
      _failed = false;
      _busyID = null;
    });
    if (widget.auth.signedIn &&
        widget.workspaceID?.call() == null &&
        _source.configured) {
      unawaited(_load());
    }
  }

  @override
  void didUpdateWidget(SocialInboxSection old) {
    super.didUpdateWidget(old);
    if (old.auth != widget.auth) {
      old.auth.removeListener(_identityChanged);
      widget.auth.addListener(_identityChanged);
    }
    if (old.workspaceChanges != widget.workspaceChanges) {
      old.workspaceChanges?.removeListener(_identityChanged);
      widget.workspaceChanges?.addListener(_identityChanged);
    }
    if (old.auth != widget.auth || old.client != widget.client ||
        old.apiBaseUrl != widget.apiBaseUrl || old.workspaceChanges != widget.workspaceChanges ||
        old.workspaceID != widget.workspaceID) {
      ++_serial; ++_identityEpoch; _reviewChanges.value++;
      _source.dispose();
      _source = ConnectionSource(authorizationHeader: () => widget.auth.authorizationHeader,
        client: widget.client, apiBaseUrl: widget.apiBaseUrl);
      _identity = _currentIdentity;
      _requests = const []; _ties = const []; _conversations = const [];
      _loading = false; _failed = false; _busyID = null;
      if (widget.auth.signedIn && widget.workspaceID?.call() == null && _source.configured) {
        unawaited(_load());
      }
    } else { _identityChanged(); }
  }

  Future<void> _load() async {
    if (!widget.auth.signedIn || widget.workspaceID?.call() != null || _loading) return;
    final serial = ++_serial;
    setState(() {
      _loading = true;
      _failed = false;
    });
    try {
      final requests = await _source.requests();
      if (!mounted || serial != _serial) return;
      final ties = await _source.ties();
      if (!mounted || serial != _serial) return;
      final conversations = await _source.conversations();
      if (!mounted || serial != _serial) return;
      setState(() {
        _requests = requests;
        _ties = ties;
        _conversations = conversations;
      });
    } catch (_) {
      if (mounted && serial == _serial) setState(() => _failed = true);
    } finally {
      if (mounted && serial == _serial) setState(() => _loading = false);
    }
  }

  Future<void> _decide(ContactRequest request, String action) async {
    if (_busyID != null) return;
    final identity = _currentIdentity, epoch = _identityEpoch, source = _source, auth = widget.auth;
    bool current() => mounted && epoch == _identityEpoch &&
      identity == _currentIdentity && identical(source, _source) && identical(auth, widget.auth) &&
      widget.workspaceID?.call() == null && source.authorizationHeader() == identity.$1;
    if (!current()) return;
    final workspaceChanges = widget.workspaceChanges, workspaceID = widget.workspaceID;
    final changes = Listenable.merge([auth, workspaceChanges, _reviewChanges]);
    setState(() => _busyID = request.id);
    try {
      await Navigator.of(context).push(MaterialPageRoute<void>(builder: (_) => NotificationDestinationBoundary(
        identityChanges: changes,
        current: current,
        builder: (_) => ConnectionRequestReviewPage(auth: auth, source: source,
          requestID: request.id, initialAction: action, current: current,
          workspaceChanges: workspaceChanges, organizationWorkspaceID: workspaceID),
      )));
      if (current()) await _load();
    } catch (_) {
      if (mounted && current()) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(const SnackBar(content: Text('申请暂不可审阅，请重新核实。')));
      }
    } finally {
      if (current()) setState(() => _busyID = null);
    }
  }

  Future<void> _manageTie(FriendTie tie, String action) async {
    if (_busyID != null) return;
    final identity = _identity, serial = _serial;
    bool current() =>
        mounted && identity == _currentIdentity && serial == _serial;
    if (action == 'report') {
      await _reportAccount(tie.otherAccountId);
      return;
    }
    if (action == 'chat') {
      setState(() => _busyID = tie.id);
      try {
        final conversation = await _source.startFriendChat(tie.id);
        if (current()) unawaited(_openConversation(conversation));
      } catch (_) {
        if (mounted) {
          ScaffoldMessenger.of(
            context,
          ).showSnackBar(const SnackBar(content: Text('暂时无法打开私信，请稍后重试。')));
        }
      } finally {
        if (mounted) setState(() => _busyID = null);
      }
      return;
    }
    final block = action == 'block';
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: Text(
          block ? '屏蔽并移除 ${tie.otherName}？' : '移除好友 ${tie.otherName}？',
        ),
        content: Text(
          block
              ? '屏蔽后双方无法互相查看资料，也不能发送联系申请或私信。取消屏蔽不会自动恢复好友关系。'
              : '移除后双方不再是好友。若要重新成为好友，需要重新发送并接受申请。',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(dialogContext, false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(dialogContext, true),
            child: Text(block ? '确认屏蔽' : '确认移除'),
          ),
        ],
      ),
    );
    if (confirmed != true || !current()) return;
    setState(() => _busyID = tie.id);
    try {
      if (block) {
        await _source.blockAccount(tie.otherAccountId);
      } else {
        await _source.removeTie(tie.id);
      }
      await _load();
    } catch (_) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(const SnackBar(content: Text('好友操作失败，请重试。')));
      }
    } finally {
      if (mounted) setState(() => _busyID = null);
    }
  }

  Future<void> _openConversation(HumanConversation conversation) async {
    final identity = _identity, serial = _serial;
    bool current() =>
        mounted && identity == _currentIdentity && serial == _serial;
    await Navigator.push(
      context,
      MaterialPageRoute<void>(
        builder: (context) => HumanConversationRoute(
          auth: widget.auth,
          conversation: conversation,
          workspaceChanges: widget.workspaceChanges,
          workspaceID: widget.workspaceID,
          actions: [
            PopupMenuButton<String>(
              tooltip: '对话安全操作',
              onSelected: (action) async {
                if (!current()) return;
                if (action == 'report') {
                  await _reportAccount(conversation.otherAccountId);
                } else if (action == 'block') {
                  await _blockConversationContact(conversation);
                }
              },
              itemBuilder: (context) => const [
                PopupMenuItem(value: 'report', child: Text('举报此账号')),
                PopupMenuItem(value: 'block', child: Text('屏蔽此账号')),
              ],
            ),
          ],
        ),
      ),
    );
    if (mounted) unawaited(_load());
  }

  Future<void> _reportAccount(String accountId) {
    final identity = _identity, serial = _serial;
    return Navigator.push(
      context,
      MaterialPageRoute<void>(
        builder: (context) => SupportPage(
          authorizationHeader: () =>
              mounted && identity == _currentIdentity && serial == _serial
              ? identity.$1
              : null,
          targetType: 'account',
          targetID: accountId,
        ),
      ),
    );
  }

  Future<void> _blockConversationContact(HumanConversation conversation) async {
    final identity = _identity, serial = _serial;
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: Text('屏蔽 ${conversation.otherName}？'),
        content: const Text('屏蔽后双方无法互相查看资料或继续私信。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(dialogContext, false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(dialogContext, true),
            child: const Text('确认屏蔽'),
          ),
        ],
      ),
    );
    if (confirmed != true ||
        !mounted ||
        serial != _serial ||
        identity != _currentIdentity) {
      return;
    }
    try {
      await _source.blockAccount(conversation.otherAccountId);
      if (mounted) Navigator.pop(context);
    } catch (_) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(const SnackBar(content: Text('屏蔽失败，请重试。')));
      }
    }
  }

  @override
  void dispose() {
    ++_serial;
    ++_identityEpoch;
    _reviewChanges.value++;
    widget.auth.removeListener(_identityChanged);
    widget.workspaceChanges?.removeListener(_identityChanged);
    _source.dispose();
    _reviewChanges.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    if (!widget.auth.signedIn ||
        widget.workspaceID?.call() != null ||
        !_source.configured) {
      return const SizedBox.shrink();
    }
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Row(
          children: [
            const Expanded(child: Text('好友与对话')),
            IconButton(
              tooltip: '写社交意图草稿',
              onPressed: () => Navigator.push(
                context,
                MaterialPageRoute<void>(
                  builder: (context) => SocialIntentDraftPage(
                    authorizationHeader: () => widget.auth.authorizationHeader,
                    authorizationChanges: widget.auth,
                  ),
                ),
              ),
              icon: const Icon(Icons.edit_note),
            ),
            IconButton(
              tooltip: '刷新好友、申请与对话',
              onPressed: _loading ? null : _load,
              icon: const Icon(Icons.refresh),
            ),
          ],
        ),
        if (_loading) const LinearProgressIndicator(),
        if (_failed)
          TextButton(onPressed: _load, child: const Text('联系人加载失败，点击重试')),
        for (final request in _requests)
          ListTile(
            contentPadding: EdgeInsets.zero,
            leading: const Icon(Icons.person_add_alt_outlined),
            title: Text(request.otherName),
            subtitle: Text(
              '${request.scope == 'friend' ? '好友申请' : '联系申请'} · ${request.direction == 'incoming' ? '收到' : '已发送'} · ${_requestState(request.state)}${request.pendingReview ? '\n待人工审阅；尚未进行 Agent 筛查。' : ''}${request.expiresAt == null ? '' : '\n有效期至 ${request.expiresAt!.toLocal().year}年${request.expiresAt!.toLocal().month}月${request.expiresAt!.toLocal().day}日（设备当地时间）'}${request.note.isEmpty ? '' : '\n${request.note}'}',
              maxLines: 5,
              overflow: TextOverflow.ellipsis,
            ),
            trailing: request.state != 'pending'
                ? null
                : PopupMenuButton<String>(
                    tooltip: '申请操作',
                    enabled: _busyID == null,
                    onSelected: (action) => _decide(request, action),
                    itemBuilder: (context) => request.direction == 'incoming'
                        ? const [
                            PopupMenuItem(value: 'accept', child: Text('接受')),
                            PopupMenuItem(value: 'decline', child: Text('拒绝')),
                          ]
                        : const [
                            PopupMenuItem(value: 'withdraw', child: Text('撤回')),
                          ],
                  ),
          ),
        for (final tie in _ties)
          ListTile(
            contentPadding: EdgeInsets.zero,
            leading: const Icon(Icons.people_outline),
            title: Text(tie.otherName),
            subtitle: const Text('好友'),
            trailing: PopupMenuButton<String>(
              tooltip: '好友操作',
              enabled: _busyID == null,
              onSelected: (action) => _manageTie(tie, action),
              itemBuilder: (context) => const [
                PopupMenuItem(value: 'chat', child: Text('发消息')),
                PopupMenuItem(value: 'remove', child: Text('移除好友')),
                PopupMenuItem(value: 'block', child: Text('屏蔽账号')),
                PopupMenuItem(value: 'report', child: Text('举报此账号')),
              ],
            ),
          ),
        for (final conversation in _conversations)
          ListTile(
            contentPadding: EdgeInsets.zero,
            leading: const Icon(Icons.chat_bubble_outline),
            title: Text(conversation.otherName),
            subtitle: Text(
              conversation.unreadCount > 0
                  ? '${conversation.unreadCount} 条未读消息'
                  : '私信对话',
            ),
            onTap: () => _openConversation(conversation),
            trailing: const Icon(Icons.chevron_right),
          ),
        if (!_loading &&
            !_failed &&
            _requests.isEmpty &&
            _ties.isEmpty &&
            _conversations.isEmpty)
          const Padding(
            padding: EdgeInsets.symmetric(vertical: 12),
            child: Text('还没有好友、申请或对话。'),
          ),
        const Divider(height: 28),
      ],
    );
  }
}

/// Binds the outer title and safety actions to the same identity as the body.
class HumanConversationRoute extends StatefulWidget {
  const HumanConversationRoute({
    super.key,
    required this.auth,
    required this.conversation,
    this.workspaceChanges,
    this.workspaceID,
    this.actions = const [],
    this.apiBaseUrl,
    this.client,
    this.pendingStore,
    this.messagePendingStore,
  });
  final BirdtieAuthController auth;
  final HumanConversation conversation;
  final Listenable? workspaceChanges;
  final String? Function()? workspaceID;
  final List<Widget> actions;
  final String? apiBaseUrl;
  final http.Client? client;
  final EntitySharePendingStore? pendingStore;
  final HumanMessagePendingStore? messagePendingStore;
  @override
  State<HumanConversationRoute> createState() => _HumanConversationRouteState();
}

class _RetiredHumanConversation extends StatelessWidget {
  const _RetiredHumanConversation();
  @override
  Widget build(BuildContext context) => SafeArea(
    child: ListView(
      padding: const EdgeInsets.all(24),
      children: [
        const Text('工作身份已变化，或消息连接已更改。请返回个人消息，重新选择会话。'),
        const SizedBox(height: 16),
        TextButton(
          style: TextButton.styleFrom(minimumSize: const Size(48, 48)),
          onPressed: () => Navigator.of(context).maybePop(),
          child: const Text('返回个人消息'),
        ),
      ],
    ),
  );
}

class _HumanConversationRouteState extends State<HumanConversationRoute> {
  late final (String?, String?, String?) _identity;
  late final Listenable _changes;
  bool _expired = false;
  bool _listening = true;
  (String?, String?, String?) get _current => (
    widget.auth.authorizationHeader,
    widget.auth.accountID,
    widget.workspaceID?.call(),
  );
  @override
  void initState() {
    super.initState();
    _identity = _current;
    _changes = Listenable.merge([widget.auth, widget.workspaceChanges]);
    _changes.addListener(_changed);
  }

  void _changed() {
    if (_expired || _identity == _current) return;
    _retire();
    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) setState(() {});
      });
    } else {
      setState(() {});
    }
  }

  void _retire() {
    _expired = true;
    if (_listening) {
      _listening = false;
      _changes.removeListener(_changed);
    }
  }

  @override
  void didUpdateWidget(HumanConversationRoute old) {
    super.didUpdateWidget(old);
    if (old.auth != widget.auth ||
        old.workspaceChanges != widget.workspaceChanges ||
        old.workspaceID != widget.workspaceID ||
        old.apiBaseUrl != widget.apiBaseUrl ||
        old.client != widget.client ||
        old.pendingStore != widget.pendingStore ||
        old.messagePendingStore != widget.messagePendingStore ||
        old.conversation.id != widget.conversation.id ||
        old.conversation.otherAccountId != widget.conversation.otherAccountId ||
        _identity != _current) {
      _retire();
    }
  }

  @override
  void dispose() {
    _retire();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final expired = _expired || _identity.$3 != null;
    return Scaffold(
      appBar: AppBar(
        title: Text(expired ? '个人消息' : widget.conversation.otherName),
        actions: expired ? const [] : widget.actions,
      ),
      body: expired
          ? const _RetiredHumanConversation()
          : HumanConversationPage(
              auth: widget.auth,
              conversation: widget.conversation,
              workspaceChanges: widget.workspaceChanges,
              workspaceID: widget.workspaceID,
              apiBaseUrl: widget.apiBaseUrl,
              client: widget.client,
              pendingStore: widget.pendingStore,
              messagePendingStore: widget.messagePendingStore,
            ),
    );
  }
}

class HumanConversationPage extends StatefulWidget {
  const HumanConversationPage({
    super.key,
    required this.auth,
    required this.conversation,
    this.workspaceChanges,
    this.workspaceID,
    this.apiBaseUrl,
    this.client,
    this.pendingStore,
    this.messagePendingStore,
  });
  final BirdtieAuthController auth;
  final HumanConversation conversation;
  final Listenable? workspaceChanges;
  final String? Function()? workspaceID;
  final String? apiBaseUrl;
  final http.Client? client;
  final EntitySharePendingStore? pendingStore;
  final HumanMessagePendingStore? messagePendingStore;
  @override
  State<HumanConversationPage> createState() => _HumanConversationPageState();
}

class _HumanConversationPageState extends State<HumanConversationPage> {
  final _text = TextEditingController();
  late final ConnectionSource _source;
  late final EntitySharePendingStore _pendingStore;
  late final HumanMessagePendingStore _messageStore;
  List<PendingHumanMessage> _messagePending = [];
  bool _journalReady = false;
  String? _messageFeedback;
  late final (String?, String?, String?) _identity;
  late final Listenable _changes;
  List<HumanMessage> _messages = [];
  List<PendingEntityShare> _pending = [];
  bool _loading = false, _sending = false, _failed = false, _expired = false;
  bool _listening = true;
  int _serial = 0;
  (String?, String?, String?) get _current => (
    widget.auth.authorizationHeader,
    widget.auth.accountID,
    widget.workspaceID?.call(),
  );
  bool get _valid => mounted && !_expired && _identity == _current;
  @override
  void initState() {
    super.initState();
    _identity = _current;
    _source = ConnectionSource(
      authorizationHeader: () => _valid ? _identity.$1 : null,
      apiBaseUrl: widget.apiBaseUrl,
      client: widget.client,
    );
    _pendingStore =
        widget.pendingStore ?? const SecureEntitySharePendingStore();
    _messageStore = widget.messagePendingStore ?? SecureHumanMessagePendingStore();
    _changes = Listenable.merge([widget.auth, widget.workspaceChanges]);
    _changes.addListener(_changed);
    unawaited(_load());
  }

  void _changed() {
    if (_identity == _current || _expired) return;
    _retire();
    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) setState(() {});
      });
    } else {
      setState(() {});
    }
  }

  void _retire() {
    _expired = true;
    ++_serial;
    _messages = [];
    _pending = [];
    _messagePending = [];
    _journalReady = false;
    _messageFeedback = null;
    _loading = false;
    _sending = false;
    _failed = false;
    _text.clear();
    if (_listening) {
      _listening = false;
      _changes.removeListener(_changed);
    }
  }

  @override
  void didUpdateWidget(HumanConversationPage old) {
    super.didUpdateWidget(old);
    if (old.auth != widget.auth ||
        old.conversation.id != widget.conversation.id ||
        old.workspaceChanges != widget.workspaceChanges ||
        old.workspaceID != widget.workspaceID ||
        old.apiBaseUrl != widget.apiBaseUrl ||
        old.client != widget.client ||
        old.pendingStore != widget.pendingStore ||
        old.messagePendingStore != widget.messagePendingStore ||
        old.conversation.otherAccountId != widget.conversation.otherAccountId ||
        _identity != _current) {
      _retire();
    }
  }

  Future<void> _load() async {
    if (!_valid || _identity.$3 != null) return;
    final serial = ++_serial;
    setState(() {
      _loading = true;
      _failed = false;
      _journalReady = false;
    });
    try {
      final owner = _identity.$2;
      if (owner == null || !chatEntityUUID.hasMatch(owner)) throw StateError('请在本人账号中读取发送记录。');
      unawaited(_loadMessageJournal(serial, owner));
      if (!_valid || serial != _serial) return;
      final messages = await _source.messages(widget.conversation.id);
      if (!_valid || serial != _serial) return;
      if (messages.isNotEmpty) {
        await _source.markRead(widget.conversation.id, messages.last.id);
        if (!_valid || serial != _serial) return;
      }
      setState(() => _messages = messages);
      if (chatEntityUUID.hasMatch(owner)) {
        final pending = await _pendingStore.read(_source.environment, owner);
        if (_valid && serial == _serial) {
          setState(
            () => _pending = pending
                .where((p) => p.conversationID == widget.conversation.id)
                .toList(),
          );
        }
      }
    } catch (_) {
      if (_valid && serial == _serial) setState(() => _failed = true);
    } finally {
      if (_valid && serial == _serial) setState(() => _loading = false);
    }
  }

  Future<void> _loadMessageJournal(int serial, String owner) async {
    if (!_messageCurrent(serial)) return;
    try {
      final journal = await _messageStore.read(_source.environment, owner);
      if (!_messageCurrent(serial)) return;
      setState(() {
        _messagePending = journal
            .where((p) => p.conversationID == widget.conversation.id)
            .toList();
        _journalReady = true;
      });
    } catch (_) {
      if (!_messageCurrent(serial)) return;
      setState(() {
        _journalReady = false;
        _messageFeedback = '发送恢复记录暂不可读取，新消息暂不能发送；请刷新核对。';
      });
    }
  }

  bool _messageCurrent(int serial) => _valid && serial == _serial && _identity.$3 == null;

  Future<void> _send() async {
    final body = _text.text.trim(), serial = _serial, owner = _identity.$2;
    if (body.isEmpty || _sending || !_messageCurrent(serial) || owner == null ||
      !_journalReady || _messagePending.isNotEmpty) { return; }
    if (utf8.encode(body).length > 2000 || body.contains('\u0000')) {
      setState(()=>_messageFeedback='消息过长，请缩短后再发送。');return;
    }
    final p = PendingHumanMessage(operationID:newEntityShareOperationID(),
      conversationID:widget.conversation.id,payloadDigest:humanMessagePayloadDigest(widget.conversation.id,body));
    setState(()=>_sending=true);
    var journalAttempted=false;
    try {
      journalAttempted=true;
      await _messageStore.write(_source.environment,owner,p);
      if (!_messageCurrent(serial)) return;
      setState(()=>_messagePending=[..._messagePending,p]);
      await _source.sendMessageOperation(owner,p,body);
      if (!_messageCurrent(serial)) return;
      await _messageStore.delete(_source.environment,owner,p);
      if (!_messageCurrent(serial)) return;
      setState(() {
        _messagePending.removeWhere((v)=>v.operationID==p.operationID);
        _sending=false;_messageFeedback='已核实消息已保存到会话，不代表对方已阅读。';
      });
      if (humanMessagePayloadDigest(widget.conversation.id, _text.text.trim()) == p.payloadDigest) {
        _text.clear();
      }
      await _load();
    } catch (_) {
      if (_messageCurrent(serial)) { setState(() {
        // Even a storage error may have persisted metadata. Re-read before new writes.
        if (_messagePending.isEmpty && journalAttempted) _journalReady=false;
        _messageFeedback=_messagePending.isEmpty
          ? '发送记录未获确认，消息没有继续提交；请刷新核对恢复记录。'
          : '发送结果暂未确认，请核实这次发送，不要重复发送。';
      }); }
    } finally {
      if (_messageCurrent(serial)) setState(()=>_sending=false);
    }
  }

  Future<void> _verifyMessage(PendingHumanMessage p) async {
    final serial=_serial,owner=_identity.$2;
    if (!_messageCurrent(serial)||_sending||owner==null||!_messagePending.contains(p)) return;
    setState(()=>_sending=true);
    try {
      await _source.readMessageOperation(owner,p);
      if (!_messageCurrent(serial)||!_messagePending.contains(p)) return;
      await _messageStore.delete(_source.environment,owner,p);
      if (!_messageCurrent(serial)) return;
      setState(() {
        _messagePending.removeWhere((v)=>v.operationID==p.operationID);
        _messageFeedback='已核实原消息已保存到会话，不代表对方已阅读。';
        _sending=false;
      });
      // Preserve an unrelated edited draft; never restore the original message text.
      if (humanMessagePayloadDigest(widget.conversation.id,_text.text.trim())==p.payloadDigest) _text.clear();
      await _load();
    } catch (_) {
      if (_messageCurrent(serial)) setState(()=>_messageFeedback='尚无法核实这次发送；原记录已保留，不会自动重发。');
    } finally { if (_messageCurrent(serial)) setState(()=>_sending=false); }
  }

  Future<void> _recover(PendingEntityShare pending, BuildContext inner) async {
    if (!_valid) return;
    await shareEntityToChat(
      inner,
      authorizationHeader: () => _valid ? _identity.$1 : null,
      identityChanges: _changes,
      workspaceID: widget.workspaceID,
      apiBaseUrl: widget.apiBaseUrl,
      client: widget.client,
      type: pending.type,
      id: pending.entityID,
      pendingStore: _pendingStore,
    );
    if (_valid) await _load();
  }

  @override
  void dispose() {
    _retire();
    _source.dispose();
    _text.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    if (_expired || _identity.$3 != null) {
      return const _RetiredHumanConversation();
    }
    return NotificationDestinationBoundary(
      identityChanges: _changes,
      current: () => _valid && _identity.$3 == null,
      builder: _buildConversation,
    );
  }

  Widget _buildConversation(BuildContext inner) => LayoutBuilder(
    builder: (context, constraints) {
      final height =
          (constraints.maxHeight.isFinite
                  ? constraints.maxHeight
                  : MediaQuery.sizeOf(context).height)
              .clamp(0.0, double.infinity);
      return SizedBox(
        height: height,
        child: Column(
          children: [
            ConstrainedBox(
              constraints: BoxConstraints(maxHeight: height * .25),
              child: SingleChildScrollView(
                child: Column(
                  children: [
                    const Padding(
                      padding: EdgeInsets.all(12),
                      child: Text('人与人私信 · 不加入 Agent 上下文。'),
                    ),
                    if (_loading) const LinearProgressIndicator(),
                    if (_messageFeedback != null) Text(_messageFeedback!),
                    if (!_journalReady) const Text('发送恢复记录暂不可用，请先刷新核对。'),
                    for(final p in _messagePending)
                      TextButton.icon(onPressed:_sending ? null : ()=>_verifyMessage(p),
                        icon:const Icon(Icons.history),label:const Text('核实这次发送')),
                    if (_failed)
                      TextButton(
                        onPressed: _load,
                        child: const Text('消息或恢复记录暂不可用，点击重试'),
                      ),
                  ],
                ),
              ),
            ),
            Expanded(
              child: RefreshIndicator(
                onRefresh: _load,
                child: ListView(
                  padding: const EdgeInsets.all(16),
                  children: [
                    for (final p in _pending)
                      TextButton.icon(
                        onPressed: () => _recover(p, inner),
                        icon: const Icon(Icons.history),
                        label: const Text('核实这次卡片分享'),
                      ),
                    if (_messages.isEmpty && !_loading) const Text('还没有消息。'),
                    for (final message in _messages)
                      Align(
                        alignment:
                            message.senderAccountId ==
                                widget.conversation.otherAccountId
                            ? Alignment.centerLeft
                            : Alignment.centerRight,
                        child: Card(
                          child: Padding(
                            padding: const EdgeInsets.all(12),
                            child: Column(
                              crossAxisAlignment: CrossAxisAlignment.start,
                              children: [
                                if (message.entity case final entity?)
                                  Semantics(
                                    label: entity.available
                                        ? '${entity.typeLabel}卡片：${entity.title}'
                                        : '${entity.typeLabel}卡片已不可查看',
                                    child: ListTile(
                                      contentPadding: EdgeInsets.zero,
                                      leading: const Icon(Icons.link_outlined),
                                      title: Text(
                                        entity.available
                                            ? entity.title ?? entity.typeLabel
                                            : '${entity.typeLabel}已不可查看',
                                      ),
                                      subtitle: Text(entity.typeLabel),
                                      onTap:
                                          entity.available &&
                                              chatEntityTypes.contains(
                                                entity.type,
                                              ) &&
                                              entity.id != null &&
                                              chatEntityUUID.hasMatch(
                                                entity.id!,
                                              )
                                          ? () => openChatEntity(
                                              inner,
                                              entity,
                                              auth: widget.auth,
                                              workspaceChanges:
                                                  widget.workspaceChanges,
                                              workspaceID: widget.workspaceID,
                                              apiBaseUrl: widget.apiBaseUrl,
                                              client: widget.client,
                                            )
                                          : null,
                                    ),
                                  ),
                                if (message.entity == null ||
                                    message.body != '分享了一张卡片')
                                  Text(message.body),
                                if (message.senderAccountId ==
                                    widget.conversation.otherAccountId)
                                  Align(
                                    alignment: Alignment.centerRight,
                                    child: TextButton(
                                      onPressed: () => Navigator.push(
                                        inner,
                                        MaterialPageRoute<void>(
                                          builder: (_) => SupportPage(
                                            authorizationHeader: () =>
                                                _valid ? _identity.$1 : null,
                                            targetType: 'message',
                                            targetID: message.id,
                                          ),
                                        ),
                                      ),
                                      child: const Text('举报此消息'),
                                    ),
                                  ),
                              ],
                            ),
                          ),
                        ),
                      ),
                  ],
                ),
              ),
            ),
            ConstrainedBox(
              constraints: BoxConstraints(maxHeight: height * .5),
              child: SingleChildScrollView(
                child: SafeArea(
                  top: false,
                  child: Padding(
                    padding: const EdgeInsets.all(12),
                    child: Row(
                      children: [
                        Expanded(
                          child: TextField(
                            controller: _text,
                            maxLength: 2000,
                            maxLines: 3,
                            minLines: 1,
                            decoration: const InputDecoration(hintText: '输入消息'),
                            onSubmitted: (_) => _send(),
                          ),
                        ),
                        IconButton(
                          tooltip: '发送消息',
                          constraints: const BoxConstraints(
                            minWidth: 48,
                            minHeight: 48,
                          ),
                          onPressed: _sending || !_journalReady || _messagePending.isNotEmpty ? null : _send,
                          icon: const Icon(Icons.send),
                        ),
                      ],
                    ),
                  ),
                ),
              ),
            ),
          ],
        ),
      );
    },
  );
}
