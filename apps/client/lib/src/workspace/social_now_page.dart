import 'dart:async';
import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import '../auth/birdtie_auth_controller.dart';
import '../city/public_city_controller.dart';
import 'activity_detail_sheet.dart';
import 'opportunity_page.dart';
import 'social_now_controller.dart';

/// Ordinary personal browsing. It never reads private Agent context or grants.
class SocialNowPage extends StatefulWidget {
  const SocialNowPage({
    super.key,
    required this.auth,
    this.client,
    this.apiBaseUrl,
    this.workspaceChanges,
    this.organizationWorkspaceID,
    this.current,
  });
  final BirdtieAuthController auth;
  final http.Client? client;
  final String? apiBaseUrl;
  final Listenable? workspaceChanges;
  final String? Function()? organizationWorkspaceID;
  final bool Function()? current;
  @override
  State<SocialNowPage> createState() => _SocialNowPageState();
}

class _SocialNowPageState extends State<SocialNowPage> {
  late final http.Client _client = widget.client ?? http.Client();
  late final SocialNowController _data = SocialNowController(
    authorizationHeader: () =>
        _sourceCurrent ? widget.auth.authorizationHeader : null,
    accountID: () => _sourceCurrent ? widget.auth.accountID : null,
    organizationWorkspaceID: () => widget.organizationWorkspaceID?.call(),
    client: _client,
    apiBaseUrl: widget.apiBaseUrl,
  );
  bool _sourceRetired = false;
  bool get _sourceCurrent {
    if (!mounted || _sourceRetired) return false;
    if (widget.current?.call() == false) {
      _sourceRetired = true;
      return false;
    }
    return true;
  }

  Route<void>? _child;
  final ScrollController _scroll = ScrollController();
  bool _opening = false;
  String? _detailError;

  @override
  void initState() {
    super.initState();
    widget.auth.addListener(_identityChanged);
    widget.workspaceChanges?.addListener(_identityChanged);
    _data.addListener(_changed);
    unawaited(_data.load());
  }

  @override
  void didUpdateWidget(covariant SocialNowPage oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.current != widget.current) _sourceRetired = true;
    if (oldWidget.auth != widget.auth) {
      oldWidget.auth.removeListener(_identityChanged);
      widget.auth.addListener(_identityChanged);
    }
    if (oldWidget.workspaceChanges != widget.workspaceChanges) {
      oldWidget.workspaceChanges?.removeListener(_identityChanged);
      widget.workspaceChanges?.addListener(_identityChanged);
    }
    _identityChanged();
  }

  void _changed() {
    if (mounted) {
      setState(() {});
      if (_data.error != null && _scroll.hasClients) _scroll.jumpTo(0);
    }
  }

  void _identityChanged() {
    final previous = _data.generation;
    _data.synchronizeIdentity();
    if (previous == _data.generation) return;
    final child = _child;
    _child = null;
    if (child?.isActive ?? false) Navigator.of(context).removeRoute(child!);
    _opening = false;
    _detailError = null;
    if (_data.personal) unawaited(_data.load());
  }

  @override
  void dispose() {
    widget.auth.removeListener(_identityChanged);
    widget.workspaceChanges?.removeListener(_identityChanged);
    _data.removeListener(_changed);
    _data.dispose();
    _scroll.dispose();
    if (widget.client == null) _client.close();
    super.dispose();
  }

  bool get _busy => _data.busy || _opening;
  Future<void> _refresh() async {
    if (_busy || !_sourceCurrent) return;
    setState(() => _detailError = null);
    await _data.load();
  }

  Future<void> _manageIntents() async {
    if (!_sourceCurrent) return;
    final generation = _data.generation;
    await _push(
      OpportunityPage(
        auth: widget.auth,
        client: _client,
        apiBaseUrl: _data.base,
      ),
    );
    if (mounted && _data.current(generation)) await _refresh();
  }

  String _date(DateTime value) {
    final local = value.toLocal();
    return '${local.year}年${local.month}月${local.day}日 ${local.hour.toString().padLeft(2, '0')}:${local.minute.toString().padLeft(2, '0')}（手机时区）';
  }

  Future<void> _push(Widget page) async {
    if (!_sourceCurrent) return;
    final route = MaterialPageRoute<void>(builder: (_) => page);
    _child = route;
    await Navigator.of(context).push(route);
    if (_child == route) _child = null;
  }

  Future<void> _intent(SocialNowIntent signal) async {
    if (_busy || !_sourceCurrent) return;
    final current = await _data.readIntent(signal);
    if (!mounted || !_sourceCurrent || current == null) return;
    await _push(
      Scaffold(
        appBar: AppBar(title: const Text('好友分享的意图')),
        body: ListView(
          padding: const EdgeInsets.all(24),
          children: [
            Text(
              current.title,
              style: Theme.of(context).textTheme.headlineSmall,
            ),
            const SizedBox(height: 16),
            Text(switch (current.modality) {
              'ONLINE' => '线上',
              'HYBRID' => '线上与线下',
              _ => '线下',
            }),
            const SizedBox(height: 8),
            Text('意图有效至：${_date(current.expiresAt)}'),
            const SizedBox(height: 24),
            const Text('这是好友当前明确分享、你有权查看的意图。想法可能变化；这里不会代你答应、邀请或发送消息。'),
          ],
        ),
      ),
    );
  }

  Future<void> _activity(SocialNowActivity signal) async {
    if (_busy || !_sourceCurrent) return;
    final generation = _data.generation;
    final token = widget.auth.authorizationHeader;
    setState(() {
      _opening = true;
      _detailError = null;
    });
    try {
      if (!await _data.load() || !_data.current(generation)) return;
      final current = [
        ..._data.friendActivities,
        ..._data.communityActivities,
        ..._data.otherActivities,
      ];
      if (!current.any((item) => item.id == signal.id)) {
        throw StateError('Source unavailable');
      }
      final response = await _client
          .get(
            _data.url('activities/${signal.id}'),
            headers: {'Authorization': token!},
          )
          .timeout(const Duration(seconds: 12));
      if (!mounted || !_data.current(generation)) return;
      if (response.statusCode != 200) throw StateError('Unavailable');
      final raw =
          (jsonDecode(utf8.decode(response.bodyBytes))
                  as Map<String, dynamic>)['data']
              as Map<String, dynamic>;
      final activity = PublicActivity.fromJson(raw);
      if (activity.id != signal.id) throw const FormatException('Wrong entity');
      await _push(
        Scaffold(
          appBar: AppBar(title: const Text('活动详情')),
          body: ActivityDetailSheet(
            activity: activity,
            authorizationHeader: () => _data.current(generation) ? token : null,
            apiBaseUrl: _data.base,
            client: _client,
            entrySource: 'social_now',
          ),
        ),
      );
    } catch (_) {
      if (mounted && _data.current(generation)) {
        setState(() => _detailError = '活动或推荐来源已变化，请刷新后重试。');
        if (_scroll.hasClients) _scroll.jumpTo(0);
      }
    } finally {
      if (mounted && _data.current(generation)) {
        setState(() => _opening = false);
      }
    }
  }

  Widget _heading(String title, String explanation) => Padding(
    padding: const EdgeInsets.only(top: 24, bottom: 8),
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(title, style: Theme.of(context).textTheme.titleLarge),
        const SizedBox(height: 4),
        Text(explanation),
      ],
    ),
  );
  List<Widget> _activities(
    String title,
    String explanation,
    List<SocialNowActivity> items,
  ) => [
    _heading(title, explanation),
    if (items.isEmpty)
      const Padding(
        padding: EdgeInsets.symmetric(vertical: 12),
        child: Text('当前没有可显示的活动。'),
      ),
    for (final item in items)
      ListTile(
        contentPadding: EdgeInsets.zero,
        title: Text(item.title),
        subtitle: Padding(
          padding: const EdgeInsets.only(top: 6),
          child: Text(
            '${item.placeName}\n${_date(item.startsAt)}\n${item.reasons.join('；')}',
          ),
        ),
        trailing: const Icon(Icons.chevron_right),
        onTap: _busy ? null : () => _activity(item),
      ),
  ];
  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(
      title: const Text('我的社交近况'),
      actions: [
        IconButton(
          tooltip: '刷新社交近况',
          onPressed: _busy || !_data.personal ? null : _refresh,
          icon: const Icon(Icons.refresh),
        ),
      ],
    ),
    body: !_data.personal
        ? const Center(
            child: Padding(
              padding: EdgeInsets.all(24),
              child: Text('请以已登录的个人身份查看社交近况。'),
            ),
          )
        : ListView(
            controller: _scroll,
            padding: const EdgeInsets.fromLTRB(20, 12, 20, 32),
            children: [
              Text(
                '看看身边正在发生什么',
                style: Theme.of(context).textTheme.headlineSmall,
              ),
              const SizedBox(height: 8),
              const Text('只显示好友明确分享的意图，以及与你启用的活动意图相符的机会。活动推荐不表示好友已报名或参加。'),
              if (_busy)
                const Padding(
                  padding: EdgeInsets.only(top: 16),
                  child: LinearProgressIndicator(),
                ),
              if (_data.error != null || _detailError != null)
                Padding(
                  padding: const EdgeInsets.only(top: 16),
                  child: Semantics(
                    liveRegion: true,
                    child: Text(
                      _data.error ?? _detailError!,
                      semanticsLabel: _data.error ?? _detailError!,
                    ),
                  ),
                ),
              if (_data.loaded) ...[
                _heading('好友明确分享的意图', '最多 10 条；只包含当前可见、仍有效的想法。'),
                if (_data.intents.isEmpty)
                  const Padding(
                    padding: EdgeInsets.symmetric(vertical: 12),
                    child: Text('当前没有可显示的好友意图。'),
                  ),
                for (final item in _data.intents)
                  ListTile(
                    contentPadding: EdgeInsets.zero,
                    leading: const Icon(Icons.chat_bubble_outline),
                    title: Text(item.title),
                    subtitle: const Text('好友分享 · 打开前重新核对'),
                    trailing: const Icon(Icons.chevron_right),
                    onTap: _busy ? null : () => _intent(item),
                  ),
                ..._activities(
                  '好友主办的活动',
                  '最多 5 条；来自当前好友关系。',
                  _data.friendActivities,
                ),
                ..._activities(
                  '已加入社群的活动',
                  '最多 5 条；来自你当前已加入的社群。',
                  _data.communityActivities,
                ),
                ..._activities(
                  '其他活动机会',
                  '最多 10 条；依据你的活动意图和当前可见范围。',
                  _data.otherActivities,
                ),
                if (_data.limited)
                  const Padding(
                    padding: EdgeInsets.only(top: 16),
                    child: Text('本次只展示有限的近况，不代表好友或社群的全部动态。'),
                  ),
              ],
              const SizedBox(height: 24),
              const Divider(),
              ListTile(
                contentPadding: EdgeInsets.zero,
                title: const Text('管理我的活动意图'),
                subtitle: const Text('检查或调整已保存的找活动意图。'),
                trailing: const Icon(Icons.chevron_right),
                onTap: _busy ? null : _manageIntents,
              ),
            ],
          ),
  );
}
