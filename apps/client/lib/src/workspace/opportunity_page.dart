import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;

import '../auth/birdtie_auth_controller.dart';
import '../city/public_city_controller.dart';
import '../config/birdtie_environment.dart';
import 'activity_detail_sheet.dart';
import 'opportunity_reasons.dart';
import 'sponsored_opportunities.dart';

class OpportunityPage extends StatefulWidget {
  const OpportunityPage({
    super.key,
    required this.auth,
    this.apiBaseUrl,
    this.client,
    this.onOpenActivity,
  });
  final BirdtieAuthController auth;
  final String? apiBaseUrl;
  final http.Client? client;
  final ValueChanged<String>? onOpenActivity;

  @override
  State<OpportunityPage> createState() => _OpportunityPageState();
}

class _OpportunityPageState extends State<OpportunityPage> {
  late final http.Client _client = widget.client ?? http.Client();
  String? _token, _error;
  int _serial = 0;
  bool _loading = false, _mutating = false, _opening = false, _loaded = false;
  List<_Opportunity> _candidates = [];
  List<SponsoredOpportunity> _sponsored = [];
  bool _sponsoredUnavailable = false;
  List<Map<String, dynamic>> _intents = [];
  BuildContext? _overlayContext;

  String get _base => widget.apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl;
  bool get _busy => _loading || _mutating || _opening;
  Uri _url(String path) =>
      Uri.parse('${_base.replaceFirst(RegExp(r'/$'), '')}/v1/$path');
  Map<String, String> _headers(String token, {bool json = false}) => {
    'Authorization': token,
    if (json) 'Content-Type': 'application/json',
  };
  bool _current(int serial, String token) =>
      mounted && serial == _serial && widget.auth.authorizationHeader == token;
  Map<String, dynamic> _envelope(http.Response response) {
    if (response.statusCode != 200) throw StateError('unavailable');
    return jsonDecode(utf8.decode(response.bodyBytes)) as Map<String, dynamic>;
  }

  @override
  void initState() {
    super.initState();
    _token = widget.auth.authorizationHeader;
    widget.auth.addListener(_authChanged);
    unawaited(_load());
  }

  @override
  void didUpdateWidget(covariant OpportunityPage oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.auth != widget.auth) {
      oldWidget.auth.removeListener(_authChanged);
      widget.auth.addListener(_authChanged);
      _token = '';
      _authChanged();
    }
  }

  void _closeOverlay() {
    final overlay = _overlayContext;
    _overlayContext = null;
    if (overlay == null || !overlay.mounted) return;
    final route = ModalRoute.of(overlay);
    if (route == null || !route.isActive) return;
    if (route.isCurrent) {
      Navigator.of(overlay).pop();
    } else {
      Navigator.of(overlay).removeRoute(route);
    }
  }

  void _authChanged() {
    final token = widget.auth.authorizationHeader;
    if (token == _token) return;
    _token = token;
    ++_serial;
    _closeOverlay();
    setState(() {
      _candidates = [];
      _sponsored = [];
      _sponsoredUnavailable = false;
      _intents = [];
      _error = null;
      _loading = _mutating = _opening = _loaded = false;
    });
    if (token != null) unawaited(_load());
  }

  @override
  void dispose() {
    ++_serial;
    widget.auth.removeListener(_authChanged);
    if (widget.client == null) _client.close();
    super.dispose();
  }

  bool _eligible(Map<String, dynamic> row) {
    final id = row['id'];
    final owner = row['creatorAccountId'];
    final expires = DateTime.tryParse(row['expiresAt'] as String? ?? '');
    return id is String &&
        _uuid.hasMatch(id) &&
        row['title'] is String &&
        (row['title'] as String).trim().isNotEmpty &&
        (owner == null || owner == widget.auth.accountID) &&
        row['type'] == 'FIND_ACTIVITY' &&
        row['audience'] == 'PRIVATE' &&
        row['modality'] == 'IN_PERSON' &&
        (row['status'] == 'DRAFT' || row['status'] == 'ACTIVE') &&
        expires != null &&
        expires.isAfter(DateTime.now()) &&
        row['constraints'] is Map<String, dynamic>;
  }

  Future<void> _load() async {
    final token = widget.auth.authorizationHeader;
    if (_busy || token == null || _base.isEmpty) return;
    final serial = ++_serial;
    setState(() {
      _loading = true;
      _loaded = false;
      _error = null;
      _candidates = [];
      _sponsored = [];
      _sponsoredUnavailable = false;
      _intents = [];
    });
    try {
      final responses = await Future.wait([
        _client.get(_url('me/opportunities'), headers: _headers(token)),
        _client.get(_url('me/social-intents'), headers: _headers(token)),
      ]).timeout(const Duration(seconds: 12));
      if (!_current(serial, token)) return;
      final opportunities = _envelope(responses[0]);
      if (opportunities['source'] != 'RULE_BASED' ||
          opportunities['ruleVersion'] != 'activity-place-v2') {
        throw const FormatException('unsupported opportunity source');
      }
      final candidates = (opportunities['data'] as List<dynamic>)
          .map((row) => _Opportunity.fromJson(row as Map<String, dynamic>))
          .toList();
      if (candidates.length > 50) throw const FormatException('result limit');
      final disclosure = decodeSponsoredDisclosure(
        opportunities,
        eligibleTargets: {
          for (final c in candidates) 'ACTIVITY:${c.activityId}': c.title,
        },
      );
      final intents = (_envelope(responses[1])['data'] as List<dynamic>)
          .cast<Map<String, dynamic>>()
          .where(_eligible)
          .toList();
      setState(() {
        _candidates = candidates;
        _sponsored = disclosure.items;
        _sponsoredUnavailable = disclosure.unavailable;
        _intents = intents;
        _loaded = true;
      });
    } catch (_) {
      if (_current(serial, token)) {
        setState(() => _error = '活动机会暂不可用，请重试。尚未确认的结果不会显示。');
      }
    } finally {
      if (_current(serial, token)) setState(() => _loading = false);
    }
  }

  Future<void> _transition(
    Map<String, dynamic> row, {
    required bool activate,
  }) async {
    final token = widget.auth.authorizationHeader;
    if (_busy || token == null || !_eligible(row)) return;
    final serial = ++_serial;
    final constraints = row['constraints'] as Map<String, dynamic>;
    String? placeName;
    setState(() {
      _candidates = [];
      _sponsored = [];
      _sponsoredUnavailable = false;
      _loaded = false;
      _error = null;
    });
    if (activate && constraints['placeId'] is String) {
      setState(() => _mutating = true);
      try {
        final place =
            _envelope(
                  await _client
                      .get(
                        _url(
                          'places/${Uri.encodeComponent(constraints['placeId'] as String)}',
                        ),
                        headers: _headers(token),
                      )
                      .timeout(const Duration(seconds: 12)),
                )['data']
                as Map<String, dynamic>;
        placeName = place['name'] as String;
        if (placeName.trim().isEmpty) {
          throw const FormatException('missing place');
        }
      } catch (_) {
        if (_current(serial, token)) {
          setState(() => _error = '指定地点暂不可核对，尚未启用意图。请刷新后重试。');
        }
        return;
      } finally {
        if (_current(serial, token)) setState(() => _mutating = false);
      }
    }
    if (!mounted || !_current(serial, token) || !_eligible(row)) return;
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (dialog) {
        _overlayContext = dialog;
        return AlertDialog(
          title: Text(activate ? '预览并启用找活动意图' : '取消这条找活动意图？'),
          content: SingleChildScrollView(
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(row['title'] as String? ?? '找活动意图'),
                if (activate) ...[
                  if (constraints['category'] is String &&
                      (constraints['category'] as String).isNotEmpty)
                    Text(
                      '类别：${_categoryName(constraints['category'] as String)}',
                    ),
                  if (placeName != null) Text('公开地点：$placeName'),
                  if (constraints['areaLabel'] is String &&
                      (constraints['areaLabel'] as String).isNotEmpty)
                    Text('你填写的大致区域：${constraints['areaLabel']}'),
                  Text(
                    '到期：${_date(DateTime.parse(row['expiresAt'] as String))}',
                  ),
                  const SizedBox(height: 12),
                  const Text(
                    '仅供你找活动，不会公开。启用后按你明确填写的条件读取当前有权查看的已发布活动，不会自动报名、邀请或发送消息。',
                  ),
                ] else
                  const Text('取消后不再以这条意图生成机会。你的其他意图和已经主动做出的报名不受此操作影响。'),
              ],
            ),
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(dialog, false),
              child: const Text('返回'),
            ),
            FilledButton(
              onPressed: () => Navigator.pop(dialog, true),
              child: Text(activate ? '确认启用' : '确认取消'),
            ),
          ],
        );
      },
    );
    _overlayContext = null;
    if (!_current(serial, token)) return;
    if (confirmed != true || !_eligible(row)) {
      await _load();
      return;
    }
    final requestSerial = ++_serial;
    setState(() {
      _mutating = true;
      _error = null;
      _candidates = [];
      _sponsored = [];
      _sponsoredUnavailable = false;
    });
    try {
      _envelope(
        await _client
            .post(
              _url(
                'me/social-intents/${Uri.encodeComponent(row['id'] as String)}/${activate ? 'activate' : 'cancel'}',
              ),
              headers: _headers(token, json: true),
              body: jsonEncode({'confirmed': true}),
            )
            .timeout(const Duration(seconds: 12)),
      );
      if (!_current(requestSerial, token)) return;
      setState(() => _mutating = false);
      await _load();
    } catch (_) {
      if (_current(requestSerial, token)) {
        setState(
          () => _error = activate
              ? '启用尚未确认，请刷新核对；过期或条件无效的意图无法使用。'
              : '取消尚未确认，请刷新核对当前状态。',
        );
      }
    } finally {
      if (_current(requestSerial, token)) setState(() => _mutating = false);
    }
  }

  Future<void> _open(_Opportunity candidate) async {
    final token = widget.auth.authorizationHeader;
    if (_busy || token == null || !_candidates.contains(candidate)) return;
    final serial = _serial;
    if (widget.onOpenActivity != null) {
      widget.onOpenActivity!(candidate.activityId);
      return;
    }
    setState(() {
      _opening = true;
      _error = null;
    });
    try {
      final raw =
          _envelope(
                await _client
                    .get(
                      _url(
                        'activities/${Uri.encodeComponent(candidate.activityId)}',
                      ),
                      headers: _headers(token),
                    )
                    .timeout(const Duration(seconds: 12)),
              )['data']
              as Map<String, dynamic>;
      if (!mounted || !_current(serial, token)) return;
      final activity = PublicActivity.fromJson(raw);
      if (activity.id != candidate.activityId) {
        throw const FormatException('wrong activity');
      }
      setState(() => _opening = false);
      await showModalBottomSheet<void>(
        context: context,
        isScrollControlled: true,
        builder: (sheet) {
          _overlayContext = sheet;
          return ActivityDetailSheet(
            activity: activity,
            authorizationHeader: () =>
                widget.auth.authorizationHeader == token ? token : null,
            apiBaseUrl: _base,
            client: _client,
            entrySource: 'opportunity',
          );
        },
      );
      _overlayContext = null;
    } catch (_) {
      if (_current(serial, token)) {
        setState(() => _error = '活动详情当前不可访问，请刷新后重试。');
      }
    } finally {
      if (_current(serial, token)) setState(() => _opening = false);
    }
  }

  String _date(DateTime value) {
    final date = value.toLocal();
    return '${date.month}月${date.day}日 ${date.hour.toString().padLeft(2, '0')}:${date.minute.toString().padLeft(2, '0')}（手机时区）';
  }

  String _categoryName(String value) => switch (value) {
    'badminton' => '羽毛球',
    'sports' => '运动',
    'social' => '社交',
    'student_event' => '学生活动',
    'food' => '美食',
    'study' => '学习',
    _ => value,
  };

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(
      title: const Text('为你找到的活动'),
      actions: [
        IconButton(
          onPressed: _busy ? null : _load,
          tooltip: '刷新活动机会',
          icon: const Icon(Icons.refresh),
        ),
      ],
    ),
    body: widget.auth.authorizationHeader == null
        ? const Center(child: Text('登录后查看本人有权访问的活动机会。'))
        : _base.isEmpty
        ? const Center(child: Text('请连接 Birdtie 后查看活动机会。'))
        : ListView(
            padding: const EdgeInsets.all(20),
            children: [
              const Text(
                '从你想做的事出发',
                style: TextStyle(fontSize: 22, fontWeight: FontWeight.w700),
              ),
              const SizedBox(height: 8),
              const Text(
                '依据你启用的找活动意图和当前权限进行基础规则匹配。这里不会自动报名、邀请或发送消息；区域文字不代表距离或所在地。',
              ),
              if (_busy) const LinearProgressIndicator(),
              if (_error != null)
                TextButton(
                  onPressed: _busy ? null : _load,
                  child: Text(_error!),
                ),
              if (_loaded && _candidates.isEmpty)
                const Padding(
                  padding: EdgeInsets.symmetric(vertical: 16),
                  child: Text('暂无符合当前条件且有权查看的活动。可以调整意图或稍后刷新。'),
                ),
              if (_candidates.isNotEmpty)
                const Padding(
                  padding: EdgeInsets.only(top: 16),
                  child: Text('当前找到的活动'),
                ),
              for (final candidate in _candidates)
                Padding(
                  padding: const EdgeInsets.symmetric(vertical: 16),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        candidate.title,
                        style: Theme.of(context).textTheme.titleMedium,
                      ),
                      Text(
                        '${candidate.placeName} · ${_date(candidate.startsAt)}',
                      ),
                      for (final reason in candidate.reasons) Text(reason),
                      const SizedBox(height: 8),
                      OutlinedButton(
                        onPressed: _busy ? null : () => _open(candidate),
                        child: const Text('查看活动详情'),
                      ),
                    ],
                  ),
                ),
              SponsoredOpportunitiesSection(
                items: _sponsored,
                unavailable: _sponsoredUnavailable,
                onOpen: _busy
                    ? null
                    : (item) {
                        if (!_sponsored.contains(item) ||
                            item.targetType != 'ACTIVITY') {
                          return;
                        }
                        final matches = _candidates.where(
                          (c) => c.activityId == item.targetID,
                        );
                        if (matches.isNotEmpty) unawaited(_open(matches.first));
                      },
              ),
              const Divider(height: 28),
              Text('本人找活动来源', style: Theme.of(context).textTheme.titleMedium),
              const Text('仅显示未过期的私人线下找活动草稿和已启用意图。启用不改变私人受众。'),
              if (_loaded && _intents.isEmpty)
                const Text(
                  '还没有可用来源。返回首页，用 Agent 找活动，在结果中选择“保存为社交意图草稿”；保存后回到此页刷新、预览并启用。',
                ),
              for (final row in _intents)
                Padding(
                  padding: const EdgeInsets.symmetric(vertical: 12),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(row['title'] as String? ?? '找活动意图'),
                      Text(
                        row['status'] == 'ACTIVE'
                            ? '已启用 · 仅自己使用'
                            : '草稿 · 仅自己可见',
                      ),
                      Text(
                        '到期：${_date(DateTime.parse(row['expiresAt'] as String))}',
                      ),
                      Wrap(
                        spacing: 8,
                        children: [
                          if (row['status'] == 'DRAFT')
                            TextButton(
                              onPressed: _busy
                                  ? null
                                  : () => _transition(row, activate: true),
                              child: const Text('预览并启用'),
                            ),
                          TextButton(
                            onPressed: _busy
                                ? null
                                : () => _transition(row, activate: false),
                            child: const Text('取消找活动意图'),
                          ),
                        ],
                      ),
                    ],
                  ),
                ),
              TextButton(
                onPressed: () =>
                    Navigator.of(context).popUntil((route) => route.isFirst),
                child: const Text('返回首页，找活动'),
              ),
            ],
          ),
  );
}

final _uuid = RegExp(
  r'^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$',
);

class _Opportunity {
  const _Opportunity(
    this.activityId,
    this.title,
    this.placeName,
    this.startsAt,
    this.reasons,
  );
  final String activityId, title, placeName;
  final DateTime startsAt;
  final List<String> reasons;
  factory _Opportunity.fromJson(Map<String, dynamic> row) {
    final entity = row['entity'] as Map<String, dynamic>;
    final place = row['place'] as Map<String, dynamic>;
    final action = row['action'] as Map<String, dynamic>;
    final target = action['target'] as Map<String, dynamic>;
    final id = entity['id'] as String;
    final intentId = row['intentId'] as String;
    final title = row['title'] as String;
    final placeName = row['placeName'] as String;
    if (row['ruleVersion'] != 'activity-place-v2' ||
        entity['type'] != 'ACTIVITY' ||
        place['type'] != 'PLACE' ||
        place['id'] is! String ||
        !_uuid.hasMatch(place['id'] as String) ||
        !_uuid.hasMatch(id) ||
        !_uuid.hasMatch(intentId) ||
        row['id'] != '$intentId:$id' ||
        action['type'] != 'OPEN_ACTIVITY' ||
        target['type'] != 'ACTIVITY' ||
        target['id'] != id ||
        title.trim().isEmpty ||
        placeName.trim().isEmpty) {
      throw const FormatException('invalid opportunity action');
    }
    return _Opportunity(
      id,
      title,
      placeName,
      DateTime.parse(row['startsAt'] as String),
      projectOpportunityReasons(
        (row['reasonCodes'] as List<dynamic>? ?? []).cast<String>(),
        scheme: OpportunityReasonScheme.activityPlaceV2,
      ),
    );
  }
}
