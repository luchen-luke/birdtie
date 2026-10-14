import 'dart:async';
import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import '../app/birdtie_app_bar.dart';
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
        appBar: birdtieAppBar(context, title: const Text('好友分享的意图')),
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
          appBar: birdtieAppBar(context, title: const Text('活动详情')),
          body: ActivityDetailSheet(
            accountID: () =>
                _data.current(generation) ? widget.auth.accountID : null,
            identityChanges: Listenable.merge([
              widget.auth,
              widget.workspaceChanges,
            ]),
            workspaceID: widget.organizationWorkspaceID,
            domainCurrent: () => _sourceCurrent && _data.current(generation),
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

  Widget _heading(String title) => Padding(
    padding: const EdgeInsets.only(top: 24, bottom: 8),
    child: Text(title, style: Theme.of(context).textTheme.titleLarge),
  );
  List<Widget> _activities(String title, List<SocialNowActivity> items) =>
      items.isEmpty
      ? []
      : [
          _heading(title),
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
    appBar: birdtieAppBar(
      context,
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
                if (_data.intents.isEmpty &&
                    _data.friendActivities.isEmpty &&
                    _data.communityActivities.isEmpty &&
                    _data.otherActivities.isEmpty)
                  const Padding(
                    padding: EdgeInsets.symmetric(vertical: 12),
                    child: Text('暂无社交近况'),
                  ),
                if (_data.intents.isNotEmpty) _heading('好友的想法'),
                for (final item in _data.intents)
                  ListTile(
                    contentPadding: EdgeInsets.zero,
                    leading: const Icon(Icons.chat_bubble_outline),
                    title: Text(item.title),
                    trailing: const Icon(Icons.chevron_right),
                    onTap: _busy ? null : () => _intent(item),
                  ),
                ..._activities('好友主办的活动', _data.friendActivities),
                ..._activities('已加入社群的活动', _data.communityActivities),
                ..._activities('其他活动机会', _data.otherActivities),
              ],
              const SizedBox(height: 24),
              ListTile(
                contentPadding: EdgeInsets.zero,
                title: const Text('调整找活动需求'),
                trailing: const Icon(Icons.chevron_right),
                onTap: _busy ? null : _manageIntents,
              ),
            ],
          ),
  );
}
