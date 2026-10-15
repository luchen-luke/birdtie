import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:http/http.dart' as http;

import '../content/moment_control.dart';
import 'human_causal_control.dart';
import 'public_moment_api.dart';

class MomentComment {
  const MomentComment(
    this.id,
    this.body,
    this.createdAt,
    this.author,
    this.canDelete,
  );
  final String id, body;
  final DateTime createdAt;
  final PublicMomentAuthorProfile? author;
  final bool canDelete;

  factory MomentComment.decode(Object? raw) {
    final d = humanControlObject(raw, {
      'id',
      'body',
      'createdAt',
      'authorId',
      'authorPublicProfile',
      'canDelete',
    });
    if (!humanControlID(d['id']) ||
        d['body'] is! String ||
        (d['body'] as String).isEmpty ||
        utf8.encode(d['body']).length > 2000 ||
        d['canDelete'] is! bool) {
      throw const FormatException('评论格式不符');
    }
    return MomentComment(
      d['id'],
      d['body'],
      humanControlTime(d['createdAt']),
      PublicMomentAuthorProfile.fromPublicPair(d),
      d['canDelete'],
    );
  }
}

class MomentInteractionPage {
  const MomentInteractionPage(
    this.momentID,
    this.revision,
    this.likeCount,
    this.commentCount,
    this.liked,
    this.likeVersion,
    this.comments,
    this.nextCursor,
    this.observedAt,
    this.validUntil,
    this.sourceVersion,
  );
  final String momentID, sourceVersion;
  final int revision, likeCount, commentCount, likeVersion;
  final bool liked;
  final List<MomentComment> comments;
  final String? nextCursor;
  final DateTime observedAt, validUntil;
  bool get current => validUntil.isAfter(DateTime.now().toUtc());
  factory MomentInteractionPage.decode(Object? raw, String id) {
    final d = humanControlObject(raw, {
      'schemaVersion',
      'momentId',
      'momentRevision',
      'likeCount',
      'commentCount',
      'liked',
      'likeVersion',
      'comments',
      'nextCursor',
      'observedAt',
      'validUntil',
      'sourceVersion',
    });
    final observed = humanControlTime(d['observedAt']),
        until = humanControlTime(d['validUntil']);
    if (d['schemaVersion'] != 'moment-interaction-page-v1' ||
        d['momentId'] != id ||
        !humanControlID(id) ||
        !humanControlVersion(d['momentRevision'], zero: false) ||
        d['momentRevision'] < 2 ||
        !humanControlVersion(d['likeVersion']) ||
        d['likeCount'] is! int ||
        d['likeCount'] < 0 ||
        d['commentCount'] is! int ||
        d['commentCount'] < 0 ||
        d['liked'] is! bool ||
        d['comments'] is! List ||
        (d['comments'] as List).length > 20 ||
        !humanControlHash(d['sourceVersion']) ||
        !until.isAfter(observed) ||
        until.isAfter(observed.add(const Duration(seconds: 60))) ||
        observed.isAfter(
          DateTime.now().toUtc().add(const Duration(seconds: 2)),
        ) ||
        !until.isAfter(DateTime.now().toUtc())) {
      throw const FormatException('动态互动资料不符或已到期');
    }
    final cursor = d['nextCursor'];
    if (cursor != null &&
        (cursor is! String ||
            cursor.isEmpty ||
            cursor.length > 1500 ||
            !RegExp(r'^[A-Za-z0-9_-]+$').hasMatch(cursor))) {
      throw const FormatException('评论翻页位置不符');
    }
    final comments = (d['comments'] as List)
        .map(MomentComment.decode)
        .toList(growable: false);
    if (comments.map((c) => c.id).toSet().length != comments.length ||
        comments.length > d['commentCount']) {
      throw const FormatException('评论列表不符');
    }
    return MomentInteractionPage(
      id,
      d['momentRevision'],
      d['likeCount'],
      d['commentCount'],
      d['liked'],
      d['likeVersion'],
      List.unmodifiable(comments),
      cursor,
      observed,
      until,
      d['sourceVersion'],
    );
  }
}

String socialOperationDigest(String family, Map<String, dynamic> v) {
  if (family == 'MOMENT_INTERACTION') {
    return momentHash([
      'moment-interaction-v1',
      v['operationId'],
      v['action'],
      v['momentId'],
      v['expectedMomentRevision'],
      v['expectedLikeVersion'],
      v['commentId'],
      base64.encode(utf8.encode(v['body'] as String)),
    ]);
  }
  if (family == 'INBOX_MANAGEMENT') {
    return momentHash([
      'inbox-management-v1',
      v['operationId'],
      v['action'],
      v['itemId'],
      v['category'],
      v['unreadOnly'],
      v['cutoff'] == null ? '' : momentUTC(humanControlTime(v['cutoff'])),
      v['sourceVersion'] ?? '',
    ]);
  }
  throw const FormatException('操作类型不符');
}

class SocialOperationPending {
  const SocialOperationPending(
    this.family,
    this.operationID,
    this.action,
    this.targetID,
    this.digest,
    this.stage,
  );
  final String family, operationID, action, targetID, digest, stage;
  Map<String, dynamic> get json => {
    'schema': 'social-operation-v1',
    'family': family,
    'operationId': operationID,
    'action': action,
    'targetId': targetID,
    'payloadDigest': digest,
    'stage': stage,
  };
  factory SocialOperationPending.decode(Object? raw) {
    final d = humanControlObject(raw, {
      'schema',
      'family',
      'operationId',
      'action',
      'targetId',
      'payloadDigest',
      'stage',
    });
    final family = d['family'], action = d['action'], target = d['targetId'];
    if (d['schema'] != 'social-operation-v1' ||
        !humanControlID(d['operationId']) ||
        !humanControlHash(d['payloadDigest']) ||
        !const {'PREPARED', 'UNKNOWN'}.contains(d['stage']) ||
        target is! String ||
        (family == 'MOMENT_INTERACTION'
            ? !const {
                    'LIKE',
                    'UNLIKE',
                    'COMMENT',
                    'DELETE_COMMENT',
                  }.contains(action) ||
                  !humanControlID(target)
            : family == 'INBOX_MANAGEMENT'
            ? !const {
                    'MARK_ALL_READ',
                    'DELETE_ITEM',
                    'CLEAR_READ',
                  }.contains(action) ||
                  (action == 'DELETE_ITEM'
                      ? !humanControlID(target)
                      : target != '')
            : true)) {
      throw const FormatException('原操作恢复记录不符');
    }
    return SocialOperationPending(
      family,
      d['operationId'],
      action,
      target,
      d['payloadDigest'],
      d['stage'],
    );
  }
  bool same(SocialOperationPending other) =>
      family == other.family &&
      operationID == other.operationID &&
      action == other.action &&
      targetID == other.targetID &&
      digest == other.digest;
  SocialOperationPending get unknown => SocialOperationPending(
    family,
    operationID,
    action,
    targetID,
    digest,
    'UNKNOWN',
  );
}

abstract interface class SocialPendingStore {
  Future<SocialOperationPending?> read(String key);
  Future<void> write(String key, SocialOperationPending value);
  Future<void> remove(String key, SocialOperationPending value);
}

class SecureSocialPendingStore implements SocialPendingStore {
  const SecureSocialPendingStore({this.storage = const FlutterSecureStorage()});
  final FlutterSecureStorage storage;
  static final _tails = <String, Future<void>>{};
  Future<T> _lock<T>(String key, Future<T> Function() work) async {
    final previous = _tails[key] ?? Future<void>.value();
    final done = Completer<void>();
    _tails[key] = done.future;
    try {
      await previous.catchError((Object _) {});
      return await work();
    } finally {
      done.complete();
      if (identical(_tails[key], done.future)) _tails.remove(key);
    }
  }

  Future<SocialOperationPending?> _read(String key) async {
    final raw = await storage.read(key: key);
    return raw == null ? null : SocialOperationPending.decode(jsonDecode(raw));
  }

  @override
  Future<SocialOperationPending?> read(String key) =>
      _lock(key, () => _read(key));
  @override
  Future<void> write(String key, SocialOperationPending value) =>
      _lock(key, () async {
        SocialOperationPending.decode(value.json);
        final old = await _read(key);
        if (old != null &&
                (!old.same(value) ||
                    old.stage == 'UNKNOWN' && value.stage != 'UNKNOWN') ||
            old == null && value.stage == 'UNKNOWN') {
          throw StateError('原准备记录已变化，不能重新提交');
        }
        await storage.write(key: key, value: jsonEncode(value.json));
        final actual = await _read(key);
        if (actual == null ||
            !actual.same(value) ||
            actual.stage != value.stage) {
          throw StateError('本机原操作尚未可靠保存');
        }
      });
  @override
  Future<void> remove(String key, SocialOperationPending value) =>
      _lock(key, () async {
        final old = await _read(key);
        if (old != null && old.same(value)) await storage.delete(key: key);
      });
}

class SocialOperationReceipt {
  const SocialOperationReceipt(
    this.status,
    this.commentID,
    this.affectedCount,
    this.unreadCount,
  );
  final String status;
  final String? commentID;
  final int? affectedCount, unreadCount;
  factory SocialOperationReceipt.decode(Object? raw, SocialOperationPending p) {
    final interaction = p.family == 'MOMENT_INTERACTION';
    final d = humanControlObject(
      raw,
      {
        'operationId',
        'status',
        interaction ? 'appliedVersion' : 'affectedCount',
      },
      optional: interaction
          ? {'action', 'momentId', 'commentId', 'payloadDigest', 'committedAt'}
          : {
              'action',
              'payloadDigest',
              'committedAt',
              'unreadCount',
              'cutoff',
              'sourceVersion',
            },
    );
    if (d['operationId'] != p.operationID ||
        !const {'SUCCEEDED', 'STOPPED'}.contains(d['status'])) {
      throw const FormatException('原操作回执不符');
    }
    final value = d[interaction ? 'appliedVersion' : 'affectedCount'];
    if (value is! int || value < 0 || !humanControlVersion(value)) {
      throw const FormatException('原操作结果不符');
    }
    if (d['status'] == 'STOPPED') {
      if (value != 0 || d.length != 3) {
        throw const FormatException('停止回执包含未发生效果');
      }
      return const SocialOperationReceipt('STOPPED', null, null, null);
    }
    if (d['action'] != p.action ||
        d['payloadDigest'] != p.digest ||
        d['committedAt'] == null) {
      throw const FormatException('成功事实与原操作不符');
    }
    humanControlTime(d['committedAt']);
    if (interaction &&
        (value < 1 ||
            d['momentId'] != p.targetID ||
            const {'COMMENT', 'DELETE_COMMENT'}.contains(p.action) &&
                !humanControlID(d['commentId']) ||
            const {'LIKE', 'UNLIKE'}.contains(p.action) &&
                d.containsKey('commentId'))) {
      throw const FormatException('原动态互动目标不符');
    }
    if (!interaction) {
      if (!humanControlHash(d['sourceVersion']) || d['cutoff'] == null) {
        throw const FormatException('原通知快照不符');
      }
      humanControlTime(d['cutoff']);
      if (d['unreadCount'] != null &&
          (d['unreadCount'] is! int || d['unreadCount'] < 0)) {
        throw const FormatException('权威未读数量不符');
      }
    }
    return SocialOperationReceipt(
      'SUCCEEDED',
      d['commentId'],
      interaction ? null : value,
      d['unreadCount'],
    );
  }
}

class SocialOperationAPI {
  const SocialOperationAPI({
    required this.client,
    required this.baseUrl,
    required this.authorizationHeader,
    required this.current,
  });
  final http.Client client;
  final String baseUrl;
  final String? Function() authorizationHeader;
  final bool Function() current;
  Uri endpoint(String path) =>
      Uri.parse('${baseUrl.replaceFirst(RegExp(r'/$'), '')}$path');
  Future<Map<String, dynamic>> request(
    String method,
    String path, {
    Map<String, dynamic>? body,
    bool private = true,
  }) async {
    final token = authorizationHeader();
    if (!current() || private && token == null) throw StateError('请使用当前个人身份');
    final headers = <String, String>{
      'Authorization': ?token,
      if (body != null) 'Content-Type': 'application/json',
    };
    final request = http.Request(method, endpoint(path))
      ..headers.addAll(headers);
    if (body != null) request.body = jsonEncode(body);
    final response = await http.Response.fromStream(
      await client.send(request),
    ).timeout(const Duration(seconds: 12));
    if (!current() || token != authorizationHeader()) {
      throw StateError('身份或连接已变化');
    }
    if (response.statusCode != 200) {
      throw StateError('当前操作暂不可用（${response.statusCode}）');
    }
    if (response.bodyBytes.length > 256 * 1024) {
      throw const FormatException('操作响应过大');
    }
    final raw = humanControlObject(
      jsonDecode(utf8.decode(response.bodyBytes)),
      {'data'},
    );
    if (raw['data'] is! Map<String, dynamic>) {
      throw const FormatException('操作响应不符');
    }
    return raw['data'];
  }

  String familyPath(String family) => family == 'MOMENT_INTERACTION'
      ? '/v1/me/moment-interactions'
      : family == 'INBOX_MANAGEMENT'
      ? '/v1/me/inbox-management'
      : throw const FormatException('操作类型不符');
  Future<MomentInteractionPage> page(
    String id, {
    String? cursor,
    String? commentID,
  }) async {
    if (!humanControlID(id) ||
        cursor != null && commentID != null ||
        commentID != null && !humanControlID(commentID)) {
      throw const FormatException('评论目标不符');
    }
    final query = {
      if (cursor != null) 'cursor': cursor,
      if (commentID != null) 'commentId': commentID,
    };
    final path = Uri(
      path: '/v1/moments/$id/interactions',
      queryParameters: query.isEmpty ? null : query,
    ).toString();
    return MomentInteractionPage.decode(
      await request('GET', path, private: false),
      id,
    );
  }
}

/// The only durable material here is a selector for an original fact. Approval,
/// comment text, credentials and notification snapshots remain in memory.
class SocialOperationController extends ChangeNotifier {
  SocialOperationController({
    required this.api,
    required this.family,
    required this.ownerID,
    this.targetID = '',
    this.identityChanges,
    SocialPendingStore? pendingStore,
  }) : store = pendingStore ?? const SecureSocialPendingStore() {
    _owner = ownerID();
    _header = api.authorizationHeader();
    identityChanges?.addListener(_observe);
  }
  final SocialOperationAPI api;
  final String family, targetID;
  final String? Function() ownerID;
  final Listenable? identityChanges;
  final SocialPendingStore store;
  String? _owner, _header;
  int _epoch = 0, _draft = 0;
  bool _disposed = false, _retired = false;
  bool busy = false, ready = false;
  String? error, notice;
  SocialOperationPending? pending;
  SocialOperationReceipt? receipt;
  String get _key =>
      'birdtie.social-operation.v1.${momentHash([api.baseUrl, _owner, family, targetID])}';
  bool get current =>
      !_disposed &&
      !_retired &&
      api.current() &&
      _owner == ownerID() &&
      api.authorizationHeader() != null;
  bool get canStart =>
      current && ready && !busy && pending == null && humanControlID(_owner);
  void _notify() {
    if (!_disposed) notifyListeners();
  }

  void _observe() {
    if (_disposed) return;
    if (_owner != ownerID() || !api.current()) {
      _retired = true;
      ++_epoch;
      ready = false;
      pending = null;
      receipt = null;
      error = '个人身份或来源已变化，请从当前入口重新打开。';
    } else if (_header != api.authorizationHeader()) {
      _header = api.authorizationHeader();
      ++_epoch;
      retirePreview();
      ready = false;
      unawaited(restore());
    }
    _notify();
  }

  void retirePreview() {
    ++_draft;
  }

  bool _valid(int epoch, String? header, int? draft) =>
      current &&
      _epoch == epoch &&
      api.authorizationHeader() == header &&
      (draft == null || draft == _draft);
  Future<void> restore() async {
    if (!current || !humanControlID(_owner)) return;
    final epoch = _epoch, header = api.authorizationHeader();
    try {
      final value = await store.read(_key);
      if (!_valid(epoch, header, null)) return;
      if (value != null &&
          (value.family != family ||
              family == 'MOMENT_INTERACTION' && value.targetID != targetID)) {
        throw const FormatException('本机原操作归属不符');
      }
      pending = value;
      ready = true;
      error = null;
      _notify();
    } catch (_) {
      if (_valid(epoch, header, null)) {
        ready = false;
        error = '本机原操作尚未恢复，请重试。';
        _notify();
      }
    }
  }

  Future<SocialOperationReceipt?> run(
    BuildContext context,
    Map<String, dynamic> input,
    String title,
    String details, {
    bool Function()? contentCurrent,
  }) async {
    if (!canStart || contentCurrent?.call() == false) return null;
    if (family == 'MOMENT_INTERACTION' && input['momentId'] != targetID) {
      error = '操作目标与当前动态不符，请重新打开。';
      _notify();
      return null;
    }
    final epoch = _epoch, header = api.authorizationHeader(), draft = _draft;
    busy = true;
    error = notice = null;
    receipt = null;
    _notify();
    SocialOperationPending? local;
    try {
      final raw = await api.request(
        'POST',
        '${api.familyPath(family)}/previews',
        body: input,
      );
      if (!_valid(epoch, header, draft) || contentCurrent?.call() == false) {
        return null;
      }
      final inbox = family == 'INBOX_MANAGEMENT';
      final p = humanControlObject(raw, {
        'operationId',
        'action',
        'payloadDigest',
        'observedAt',
        'expiresAt',
        'approval',
        'input',
        'sourceVersion',
        if (inbox) ...{'cutoff', 'count'},
      });
      final echoed = humanControlObject(
        p['input'],
        inbox
            ? {
                'operationId',
                'action',
                'itemId',
                'category',
                'unreadOnly',
                'cutoff',
                'sourceVersion',
              }
            : {
                'operationId',
                'action',
                'momentId',
                'expectedMomentRevision',
                'expectedLikeVersion',
                'commentId',
                'body',
              },
      );
      if (input.entries.any((e) => echoed[e.key] != e.value) ||
          p['operationId'] != input['operationId'] ||
          p['action'] != input['action'] ||
          !humanControlHash(p['sourceVersion']) ||
          p['payloadDigest'] != socialOperationDigest(family, echoed) ||
          p['approval'] is! String ||
          (p['approval'] as String).isEmpty ||
          utf8.encode(p['approval']).length > 8192) {
        throw const FormatException('具体预览与当前内容不符');
      }
      final until = humanControlTime(p['expiresAt']);
      humanControlLease(humanControlTime(p['observedAt']), until);
      if (inbox &&
          (p['count'] is! int ||
              p['count'] < 0 ||
              p['cutoff'] != echoed['cutoff'] ||
              p['sourceVersion'] != echoed['sourceVersion'])) {
        throw const FormatException('通知快照不符');
      }
      if (!context.mounted) return null;
      final approved = await showDialog<bool>(
        context: context,
        builder: (dialog) => AlertDialog(
          title: Text(title),
          content: SingleChildScrollView(
            child: Text(
              inbox
                  ? '$details\n\n本次将处理 ${p['count']} 条通知。之后新增的通知会保留。'
                  : details,
            ),
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(dialog, false),
              child: const Text('取消'),
            ),
            FilledButton(
              onPressed: () => Navigator.pop(dialog, true),
              child: const Text('确认'),
            ),
          ],
        ),
      );
      if (approved == true && !until.isAfter(DateTime.now().toUtc())) {
        error = '这次批准已到期，请重新预览并确认。';
        return null;
      }
      if (approved != true ||
          !_valid(epoch, header, draft) ||
          contentCurrent?.call() == false ||
          !until.isAfter(DateTime.now().toUtc())) {
        return null;
      }
      local = SocialOperationPending(
        family,
        input['operationId'],
        input['action'],
        inbox ? input['itemId'] : targetID,
        p['payloadDigest'],
        'PREPARED',
      );
      await store.write(_key, local);
      if (!_valid(epoch, header, draft) || contentCurrent?.call() == false) {
        return null;
      }
      pending = local;
      await store.write(_key, local.unknown);
      if (!_valid(epoch, header, draft) || contentCurrent?.call() == false) {
        return null;
      }
      pending = local.unknown;
      // Verify that another view has not retired this prepared selector.
      final actual = await store.read(_key);
      if (actual == null ||
          !actual.same(local) ||
          actual.stage != 'UNKNOWN' ||
          !_valid(epoch, header, draft) ||
          contentCurrent?.call() == false) {
        throw StateError('原准备记录已变化，未再次提交');
      }
      final commit = {
        ...echoed,
        'sourceVersion': p['sourceVersion'],
        'approval': p['approval'],
      };
      final result = await api.request(
        'PUT',
        '${api.familyPath(family)}/operations/${local.operationID}',
        body: commit,
      );
      if (!_valid(epoch, header, null)) return null;
      final fact = SocialOperationReceipt.decode(result, local);
      await store.remove(_key, local);
      if (!_valid(epoch, header, null)) return null;
      pending = null;
      receipt = fact;
      notice = fact.status == 'SUCCEEDED'
          ? '原操作已完成。当前数量与内容会重新读取。'
          : '原操作已停止，未产生新效果。';
      return fact;
    } catch (_) {
      if (_valid(epoch, header, null)) {
        if (local != null) {
          pending = await store.read(_key).catchError((Object _) => local);
          error = '结果尚未核实，请核实或停止原操作。';
        } else {
          error = '暂时无法完成预览，当前输入已保留。';
        }
      }
      return null;
    } finally {
      if (!_disposed) {
        busy = false;
        _notify();
      }
    }
  }

  Future<SocialOperationReceipt?> recover({bool stop = false}) async {
    final p = pending;
    if (p == null || busy || !current) return null;
    final epoch = _epoch, header = api.authorizationHeader();
    busy = true;
    error = null;
    _notify();
    try {
      final raw = await api.request(
        stop ? 'POST' : 'GET',
        '${api.familyPath(family)}/operations/${p.operationID}${stop ? '/stop' : ''}',
      );
      if (!_valid(epoch, header, null)) return null;
      final value = SocialOperationReceipt.decode(raw, p);
      await store.remove(_key, p);
      if (!_valid(epoch, header, null)) return null;
      pending = null;
      receipt = value;
      notice = value.status == 'SUCCEEDED' ? '原操作已完成，请重新查看当前内容。' : '原操作已停止。';
      return value;
    } catch (_) {
      if (_valid(epoch, header, null)) error = '原结果暂未核实，本机记录已保留。';
      return null;
    } finally {
      if (!_disposed) {
        busy = false;
        _notify();
      }
    }
  }

  @override
  void dispose() {
    _disposed = true;
    ++_epoch;
    identityChanges?.removeListener(_observe);
    super.dispose();
  }
}

class SocialOperationRecovery extends StatelessWidget {
  const SocialOperationRecovery({
    super.key,
    required this.controller,
    this.onResolved,
  });
  final SocialOperationController controller;
  final VoidCallback? onResolved;
  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: controller,
    builder: (_, _) => Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        if (controller.error != null) Text(controller.error!),
        if (controller.notice != null) Text(controller.notice!),
        if (controller.pending != null) const Text('这次操作的结果待核实。'),
        if (controller.pending != null)
          Wrap(
            children: [
              TextButton(
                onPressed: controller.busy
                    ? null
                    : () async {
                        if (await controller.recover() != null) {
                          onResolved?.call();
                        }
                      },
                child: const Text('核实原操作'),
              ),
              TextButton(
                onPressed: controller.busy
                    ? null
                    : () async {
                        if (await controller.recover(stop: true) != null) {
                          onResolved?.call();
                        }
                      },
                child: const Text('停止未完成操作'),
              ),
            ],
          ),
        if (!controller.ready && controller.current)
          TextButton(
            onPressed: controller.restore,
            child: const Text('恢复本机操作'),
          ),
      ],
    ),
  );
}

class MomentInteractionPanel extends StatefulWidget {
  const MomentInteractionPanel({
    super.key,
    required this.momentID,
    required this.authorizationHeader,
    required this.ownerID,
    required this.apiBaseUrl,
    required this.client,
    required this.current,
    this.identityChanges,
    this.commentID,
    this.recoveryOnly = false,
  });
  final String momentID, apiBaseUrl;
  final String? commentID;
  final bool recoveryOnly;
  final String? Function() authorizationHeader, ownerID;
  final http.Client client;
  final bool Function() current;
  final Listenable? identityChanges;
  @override
  State<MomentInteractionPanel> createState() => _MomentInteractionPanelState();
}

class _MomentInteractionPanelState extends State<MomentInteractionPanel> {
  bool sourceRetired = false;
  late final SocialOperationAPI api = SocialOperationAPI(
    client: widget.client,
    baseUrl: widget.apiBaseUrl,
    authorizationHeader: () => widget.authorizationHeader(),
    current: () => mounted && !sourceRetired && widget.current(),
  );
  late final SocialOperationController control = SocialOperationController(
    api: api,
    family: 'MOMENT_INTERACTION',
    ownerID: () => widget.ownerID(),
    targetID: widget.momentID,
    identityChanges: widget.identityChanges,
  );
  final text = TextEditingController();
  MomentInteractionPage? page;
  List<MomentComment> comments = [];
  String? error;
  bool loading = false;
  int serial = 0;
  Timer? lease;
  bool anchored = true;
  @override
  void initState() {
    super.initState();
    text.addListener(control.retirePreview);
    unawaited(control.restore());
    if (!widget.recoveryOnly) unawaited(load());
  }

  @override
  void didUpdateWidget(MomentInteractionPanel old) {
    super.didUpdateWidget(old);
    if (old.client != widget.client ||
        old.apiBaseUrl != widget.apiBaseUrl ||
        old.momentID != widget.momentID ||
        old.identityChanges != widget.identityChanges) {
      sourceRetired = true;
      ++serial;
      lease?.cancel();
      page = null;
      comments = [];
      text.clear();
      control.retirePreview();
    }
  }

  @override
  void dispose() {
    ++serial;
    lease?.cancel();
    text.removeListener(control.retirePreview);
    text.dispose();
    control.dispose();
    super.dispose();
  }

  Future<void> load({bool more = false}) async {
    if (loading || sourceRetired || !widget.current()) return;
    final token = widget.authorizationHeader(),
        owner = widget.ownerID(),
        request = ++serial,
        cursor = more ? page?.nextCursor : null;
    if (more && cursor == null) return;
    setState(() {
      loading = true;
      error = null;
    });
    try {
      final value = await api.page(
        widget.momentID,
        cursor: cursor,
        commentID: !more && anchored ? widget.commentID : null,
      );
      if (!mounted ||
          request != serial ||
          !widget.current() ||
          token != widget.authorizationHeader() ||
          owner != widget.ownerID()) {
        return;
      }
      if (more && value.revision != page?.revision) {
        throw StateError('动态已修改，请刷新评论');
      }
      final previous = page;
      var expiry = value.validUntil;
      // Pagination must never extend the lease of bodies from earlier pages.
      if (more && previous != null && previous.validUntil.isBefore(expiry))
        expiry = previous.validUntil;
      if (!expiry.isAfter(DateTime.now().toUtc()))
        throw StateError('原评论快照已到期，请刷新');
      final accepted = MomentInteractionPage(
        value.momentID,
        value.revision,
        value.likeCount,
        value.commentCount,
        value.liked,
        value.likeVersion,
        value.comments,
        value.nextCursor,
        value.observedAt,
        expiry,
        value.sourceVersion,
      );
      setState(() {
        page = accepted;
        comments = more
            ? [
                ...comments,
                ...value.comments.where(
                  (c) => !comments.any((old) => old.id == c.id),
                ),
              ]
            : value.comments;
      });
      lease?.cancel();
      lease = Timer(expiry.difference(DateTime.now().toUtc()), () {
        if (mounted && identical(accepted, page)) {
          setState(() {
            page = null;
            comments = [];
            error = '评论资料已到期，请刷新查看。';
          });
        }
      });
    } catch (_) {
      if (mounted && request == serial) {
        setState(() {
          page = null;
          comments = [];
          error = widget.commentID != null && anchored
              ? '这条评论已删除、不可查看，或暂时无法读取。'
              : '互动暂不可读取，请重试。';
        });
      }
    } finally {
      if (mounted && request == serial) setState(() => loading = false);
    }
  }

  Future<void> act(String action, {String commentID = ''}) async {
    final source = page;
    if (source == null || !source.current || !control.canStart) return;
    final body = action == 'COMMENT' ? text.text.trim() : '';
    if (action == 'COMMENT' &&
        (body.isEmpty || utf8.encode(body).length > 2000)) {
      return;
    }
    final label = switch (action) {
      'LIKE' => '赞这条动态',
      'UNLIKE' => '取消赞',
      'COMMENT' => '发布评论',
      _ => '删除本人评论',
    };
    final result = await control.run(
      context,
      {
        'operationId': newMomentOperationID(),
        'action': action,
        'momentId': widget.momentID,
        'expectedMomentRevision': source.revision,
        'expectedLikeVersion': source.likeVersion,
        'commentId': commentID,
        'body': body,
      },
      label,
      action == 'COMMENT'
          ? '评论会公开显示在这条动态下：\n\n$body'
          : action == 'LIKE'
          ? '作者可收到这次点赞的站内通知。'
          : action == 'UNLIKE'
          ? '取消这条动态的点赞。'
          : '这条本人评论将停止公开展示。',
      contentCurrent: () =>
          mounted &&
          widget.current() &&
          identical(source, page) &&
          source.current,
    );
    if (!mounted) return;
    if (result?.status == 'SUCCEEDED' &&
        action == 'COMMENT' &&
        text.text.trim() == body) {
      text.clear();
    }
    if (result != null) await load();
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: control,
    builder: (_, _) => widget.recoveryOnly
        ? SocialOperationRecovery(controller: control)
        : sourceRetired
        ? const Text('动态来源已变化，请返回当前入口重新打开。')
        : Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              const Divider(height: 28),
              const Text('动态互动'),
              if (loading) const LinearProgressIndicator(),
              if (error != null) Text(error!),
              Wrap(
                children: [
                  TextButton(
                    onPressed: !loading ? load : null,
                    child: const Text('刷新互动'),
                  ),
                  if (widget.commentID != null && anchored)
                    TextButton(
                      onPressed: () {
                        anchored = false;
                        unawaited(load());
                      },
                      child: const Text('查看全部评论'),
                    ),
                ],
              ),
              if (page case final value?) ...[
                Text('${value.likeCount} 个赞 · ${value.commentCount} 条评论'),
                TextButton.icon(
                  onPressed: control.canStart && !loading && value.current
                      ? () => act(value.liked ? 'UNLIKE' : 'LIKE')
                      : null,
                  icon: Icon(
                    value.liked ? Icons.favorite : Icons.favorite_border,
                  ),
                  label: Text(value.liked ? '取消赞' : '赞'),
                ),
                for (final comment in comments)
                  Padding(
                    key: ValueKey('moment-comment:${comment.id}'),
                    padding: const EdgeInsets.symmetric(vertical: 8),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(comment.author?.displayName ?? '用户评论'),
                        Text(comment.body),
                        if (comment.canDelete)
                          TextButton(
                            onPressed: control.canStart
                                ? () => act(
                                    'DELETE_COMMENT',
                                    commentID: comment.id,
                                  )
                                : null,
                            child: const Text('删除本人评论'),
                          ),
                      ],
                    ),
                  ),
                if (value.nextCursor != null)
                  TextButton(
                    onPressed: loading ? null : () => load(more: true),
                    child: const Text('更多评论'),
                  ),
                if (humanControlID(widget.ownerID()) &&
                    widget.authorizationHeader() != null) ...[
                  TextField(
                    controller: text,
                    maxLines: 3,
                    minLines: 1,
                    enabled: !control.busy && control.pending == null,
                    decoration: const InputDecoration(labelText: '写一条公开评论'),
                  ),
                  TextButton(
                    onPressed: control.canStart ? () => act('COMMENT') : null,
                    child: const Text('发布评论'),
                  ),
                ] else
                  const Text('登录个人账号后可以点赞和评论。'),
              ],
              SocialOperationRecovery(
                controller: control,
                onResolved: () => unawaited(load()),
              ),
            ],
          ),
  );
}
