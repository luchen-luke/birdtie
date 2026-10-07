import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';
import 'package:http/http.dart' as http;

import '../config/birdtie_environment.dart';
import 'model_egress_api.dart' show egressIDValid, egressTime;

/// Shape validation only. The native route owns Session and current source ACL.
Map<String, dynamic> decodeSharedHistory(String body, String target) {
  final envelope = jsonDecode(body);
  if (envelope is! Map<String, dynamic> ||
      envelope.keys.any((k) => k != 'data')) {
    throw const FormatException('共同信息格式不完整');
  }
  final v = envelope['data'];
  const keys = {
    'schema',
    'viewerId',
    'targetId',
    'observedAt',
    'validUntil',
    'attendance',
    'visit',
    'mutualCount',
    'communities',
    'activities',
    'places',
    'communitiesTruncated',
    'activitiesTruncated',
    'placesTruncated',
  };
  if (v is! Map<String, dynamic> ||
      v.keys.any((k) => !keys.contains(k)) ||
      v['schema'] != 'shared-relationship-history-v1' ||
      !egressIDValid(v['viewerId']) ||
      !egressIDValid(target) ||
      v['targetId'] != target ||
      v['viewerId'] == target ||
      v['attendance'] != 'UNKNOWN' ||
      v['visit'] != 'UNKNOWN' ||
      v['mutualCount'] is! int ||
      (v['mutualCount'] as int) < 0 ||
      v['communitiesTruncated'] is! bool ||
      v['activitiesTruncated'] is! bool ||
      (v.containsKey('placesTruncated') && v['placesTruncated'] is! bool)) {
    throw const FormatException('共同信息当前边界无效');
  }
  final observed = egressTime(v['observedAt']),
      until = egressTime(v['validUntil']);
  if (!until.isAfter(observed) ||
      until.difference(observed) > const Duration(seconds: 30)) {
    throw const FormatException('共同信息期限无效');
  }
  final activityIDs = <String>{};
  void refs(String kind, Set<String> ids) {
    final rows = v[kind];
    if (rows is! List || rows.length > 50) {
      throw const FormatException('共同信息列表无效');
    }
    final allowed = kind == 'activities'
        ? {'id', 'title', 'startsAt', 'endsAt', 'timeZone', 'modality'}
        : {'id', 'title'};
    for (final row in rows) {
      if (row is! Map<String, dynamic> ||
          row.keys.any((k) => !allowed.contains(k)) ||
          !egressIDValid(row['id']) ||
          !ids.add(row['id'] as String) ||
          row['title'] is! String ||
          (row['title'] as String).trim().isEmpty ||
          (row['title'] as String).runes.length > 4096) {
        throw const FormatException('共同信息条目无效');
      }
      if (kind == 'activities' &&
          (!egressTime(row['endsAt']).isAfter(egressTime(row['startsAt'])) ||
              row['timeZone'] is! String ||
              (row['timeZone'] as String).isEmpty ||
              !{
                'in_person',
                'online',
                'hybrid',
                'unspecified',
              }.contains(row['modality']))) {
        throw const FormatException('活动时间无效');
      }
    }
  }

  refs('communities', <String>{});
  refs('activities', activityIDs);
  final places = v['places'] ?? <dynamic>[];
  if (places is! List || places.length > 50) {
    throw const FormatException('关联地点无效');
  }
  final seen = <String>{};
  for (final p in places) {
    if (p is! Map<String, dynamic> ||
        p.keys.any((k) => !{'id', 'title', 'activityIds'}.contains(k)) ||
        !egressIDValid(p['id']) ||
        !seen.add(p['id'] as String) ||
        p['title'] is! String ||
        (p['title'] as String).trim().isEmpty ||
        (p['title'] as String).runes.length > 4096 ||
        p['activityIds'] is! List) {
      throw const FormatException('关联地点无效');
    }
    final ids = p['activityIds'] as List;
    if (ids.isEmpty ||
        ids.length > 50 ||
        ids.toSet().length != ids.length ||
        ids.any((id) => !activityIDs.contains(id))) {
      throw const FormatException('地点缺少活动关联');
    }
  }
  return {...v, 'places': places};
}

class SharedSocialContextPanel extends StatefulWidget {
  const SharedSocialContextPanel({
    super.key,
    required this.accountID,
    required this.authorizationHeader,
    this.apiBaseUrl,
    this.client,
    this.identityChanges,
    this.workspaceID,
  });
  final String accountID;
  final String? Function() authorizationHeader;
  final String? apiBaseUrl;
  final http.Client? client;
  final Listenable? identityChanges;
  final String? Function()? workspaceID;
  @override
  State<SharedSocialContextPanel> createState() =>
      _SharedSocialContextPanelState();
}

class _SharedSocialContextPanelState extends State<SharedSocialContextPanel> {
  late http.Client _client;
  late bool _ownsClient;
  Map<String, dynamic>? _data;
  String? _token, _workspace;
  bool _failed = false, _unavailable = false, _loading = false;
  int _epoch = 0;
  Timer? _expiry;
  bool _scheduled = false;
  String get _base => widget.apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl;
  bool get _personal =>
      _token != null &&
      _workspace == null &&
      egressIDValid(widget.accountID) &&
      _base.isNotEmpty;
  @override
  void initState() {
    super.initState();
    _bind();
    widget.identityChanges?.addListener(_identityChanged);
    _scheduleLoad();
  }

  void _bind() {
    _client = widget.client ?? http.Client();
    _ownsClient = widget.client == null;
    _token = widget.authorizationHeader();
    _workspace = widget.workspaceID?.call();
  }

  void _clear() {
    ++_epoch;
    _expiry?.cancel();
    _expiry = null;
    _data = null;
    _failed = false;
    _unavailable = false;
    _loading = false;
  }

  void _notify() {
    if (!mounted) return;
    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) setState(() {});
      });
    } else {
      setState(() {});
    }
  }

  void _identityChanged() {
    final t = widget.authorizationHeader(), w = widget.workspaceID?.call();
    if (t == _token && w == _workspace) return;
    _clear();
    _token = t;
    _workspace = w;
    _notify();
    _scheduleLoad();
  }

  void _scheduleLoad() {
    if (_scheduled) return;
    _scheduled = true;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      _scheduled = false;
      if (mounted && _personal) unawaited(_load());
    });
  }

  @override
  void didUpdateWidget(SharedSocialContextPanel old) {
    super.didUpdateWidget(old);
    final changed =
        old.accountID != widget.accountID ||
        old.apiBaseUrl != widget.apiBaseUrl ||
        old.client != widget.client ||
        old.authorizationHeader != widget.authorizationHeader ||
        old.identityChanges != widget.identityChanges ||
        old.workspaceID != widget.workspaceID;
    if (changed) {
      old.identityChanges?.removeListener(_identityChanged);
      _clear();
      if (_ownsClient) _client.close();
      _bind();
      widget.identityChanges?.addListener(_identityChanged);
      _scheduleLoad();
    } else {
      _identityChanged();
    }
  }

  @override
  void dispose() {
    _clear();
    widget.identityChanges?.removeListener(_identityChanged);
    if (_ownsClient) _client.close();
    super.dispose();
  }

  Future<void> _load() async {
    _identityChanged();
    if (!_personal) return;
    _clear();
    final epoch = _epoch,
        client = _client,
        token = _token,
        id = widget.accountID,
        base = _base;
    _loading = true;
    _notify();
    bool current() =>
        mounted &&
        epoch == _epoch &&
        identical(client, _client) &&
        token == widget.authorizationHeader() &&
        widget.workspaceID?.call() == null &&
        widget.accountID == id &&
        _base == base;
    final requestClock = Stopwatch()..start();
    try {
      final r = await client
          .get(
            Uri.parse(
              '${base.replaceFirst(RegExp(r'/$'), '')}/v1/accounts/${Uri.encodeComponent(id)}/shared-context',
            ),
            headers: {'Authorization': token!},
          )
          .timeout(const Duration(seconds: 12));
      if (!current()) return;
      if ({401, 403, 404}.contains(r.statusCode)) {
        _unavailable = true;
        return;
      }
      if (r.statusCode != 200) throw StateError('共同信息当前不可用');
      final v = decodeSharedHistory(utf8.decode(r.bodyBytes), id);
      final until = egressTime(v['validUntil']);
      // Server lease starts before receipt. Network/parse time consumes it;
      // device wall time may shorten this bound, but may never renew it.
      final relative =
          until.difference(egressTime(v['observedAt'])) - requestClock.elapsed;
      final absolute = until.difference(DateTime.now().toUtc());
      final remaining = relative < absolute ? relative : absolute;
      if (remaining <= Duration.zero) throw const FormatException('共同信息已失效');
      _data = v;
      _expiry = Timer(remaining, () {
        if (!current()) return;
        _data = null;
        _unavailable = true;
        _notify();
      });
    } catch (_) {
      if (current()) _failed = true;
    } finally {
      if (current()) {
        _loading = false;
        _notify();
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    if (widget.authorizationHeader() != _token ||
        widget.workspaceID?.call() != _workspace) {
      _clear();
      _token = widget.authorizationHeader();
      _workspace = widget.workspaceID?.call();
      _scheduleLoad();
    }
    if (!_personal || _unavailable) return const SizedBox.shrink();
    if (_loading) {
      return const Padding(
        padding: EdgeInsets.symmetric(vertical: 16),
        child: LinearProgressIndicator(),
      );
    }
    final buttonStyle = TextButton.styleFrom(minimumSize: const Size(48, 48));
    if (_failed) {
      return TextButton(
        style: buttonStyle,
        onPressed: _load,
        child: const Text('共同信息暂不可用，点击重试。'),
      );
    }
    final data = _data;
    if (data == null) return const SizedBox.shrink();
    final communities = data['communities'] as List,
        activities = data['activities'] as List,
        places = data['places'] as List,
        count = data['mutualCount'] as int;
    String date(dynamic value) => egressTime(value)
        .toUtc()
        .toIso8601String()
        .replaceFirst('T', ' ')
        .replaceFirst(RegExp(r'\.\d+Z$'), ' UTC')
        .replaceFirst('Z', ' UTC');
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const Divider(height: 32),
        Text('你们的共同信息', style: Theme.of(context).textTheme.titleMedium),
        const Text('只展示双方允许显示的当前共同信息。报名不代表到场，活动关联地点不代表到访。'),
        if (count == 0 &&
            communities.isEmpty &&
            activities.isEmpty &&
            places.isEmpty)
          const Padding(
            padding: EdgeInsets.symmetric(vertical: 12),
            child: Text('暂无可展示的共同信息。'),
          ),
        if (count > 0) Text('可展示的共同好友：$count 位'),
        if (communities.isNotEmpty) ...[
          const SizedBox(height: 12),
          const Text('共同公开社群'),
          for (final ref in communities) Text(ref['title'] as String),
          if (data['communitiesTruncated'] == true) const Text('已展示前 50 个社群。'),
        ],
        if (activities.isNotEmpty) ...[
          const SizedBox(height: 12),
          const Text('共同报名的公开活动'),
          for (final ref in activities) ...[
            Text(ref['title'] as String),
            Text(
              '${date(ref['startsAt'])} 至 ${date(ref['endsAt'])}；活动时区：${ref['timeZone']}（以上为 UTC 时间）',
            ),
          ],
          if (data['activitiesTruncated'] == true) const Text('已展示最近 50 个活动。'),
        ],
        if (places.isNotEmpty) ...[
          const SizedBox(height: 12),
          const Text('活动关联的公开地点'),
          for (final ref in places) Text(ref['title'] as String),
          if (data['placesTruncated'] == true) const Text('已展示前 50 个关联地点。'),
        ],
        TextButton(
          style: buttonStyle,
          onPressed: _load,
          child: const Text('刷新共同信息'),
        ),
      ],
    );
  }
}
