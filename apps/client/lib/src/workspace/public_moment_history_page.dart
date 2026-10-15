import 'dart:async';
import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import '../app/birdtie_app_bar.dart';
import 'now_ui_tokens.dart';
import 'public_moment_history_api.dart';
import 'public_moment_history_controller.dart';
import 'public_moment_page.dart';

class PublicMomentHistoryPage extends StatefulWidget {
  const PublicMomentHistoryPage({
    super.key,
    required this.query,
    required this.authorizationHeader,
    this.cityName = '',
    this.ownerID,
    this.workspaceID,
    this.identityChanges,
    this.current,
    this.client,
    this.apiBaseUrl,
    this.onOpenAuthor,
  });
  final PublicMomentHistoryQuery query;
  final String cityName;
  final String? Function() authorizationHeader;
  final String? Function()? ownerID, workspaceID;
  final Listenable? identityChanges;
  final bool Function()? current;
  final http.Client? client;
  final String? apiBaseUrl;
  final ValueChanged<String>? onOpenAuthor;
  @override
  State<PublicMomentHistoryPage> createState() =>
      _PublicMomentHistoryPageState();
}

class _PublicMomentHistoryPageState extends State<PublicMomentHistoryPage> {
  late PublicMomentHistoryController _controller = _create();
  String _window = 'all';
  PublicMomentHistoryController _create() => PublicMomentHistoryController(
    api: PublicMomentHistoryApi(
      authorizationHeader: widget.authorizationHeader,
      client: widget.client,
      apiBaseUrl: widget.apiBaseUrl,
    ),
    query: widget.query,
    ownerID: widget.ownerID,
    workspaceID: widget.workspaceID,
    identityChanges: widget.identityChanges,
    sourceCurrent: widget.current,
  );
  @override
  void initState() {
    super.initState();
    unawaited(_controller.refresh());
  }

  @override
  void didUpdateWidget(PublicMomentHistoryPage old) {
    super.didUpdateWidget(old);
    if (old.query.binding != widget.query.binding ||
        old.client != widget.client ||
        old.apiBaseUrl != widget.apiBaseUrl ||
        old.identityChanges != widget.identityChanges ||
        old.current != widget.current ||
        old.authorizationHeader != widget.authorizationHeader ||
        old.ownerID != widget.ownerID ||
        old.workspaceID != widget.workspaceID) {
      _controller.dispose();
      _controller = _create();
      unawaited(_controller.refresh());
    }
  }

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  void _filter({String? scope, String? window}) {
    _window = window ?? _window;
    final days = int.tryParse(_window), now = DateTime.now().toUtc();
    unawaited(
      _controller.select(
        PublicMomentHistoryQuery(
          scope: scope ?? _controller.query.scope,
          cityID: _controller.query.cityID,
          authorID: _controller.query.authorID,
          from: days == null ? null : now.subtract(Duration(days: days)),
          to: days == null ? null : now,
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: birdtieAppBar(
      context,
      title: Text(widget.query.scope == 'author' ? 'TA 的公开动态' : '城市动态'),
    ),
    body: SafeArea(
      child: AnimatedBuilder(
        animation: _controller,
        builder: (context, _) {
          final tokens = NowUiTokens.of(context), items = _controller.items;
          return RefreshIndicator(
            onRefresh: _controller.refresh,
            child: ListView(
              key: PageStorageKey(
                'public-moment-history:${widget.query.authorID}:${widget.query.cityID}',
              ),
              physics: const AlwaysScrollableScrollPhysics(),
              padding: const EdgeInsets.fromLTRB(24, 12, 24, 32),
              children: [
                if (widget.cityName.isNotEmpty)
                  Padding(
                    padding: const EdgeInsets.only(bottom: 12),
                    child: Text(widget.cityName, style: tokens.placeTitle),
                  ),
                Wrap(
                  spacing: 12,
                  runSpacing: 8,
                  crossAxisAlignment: WrapCrossAlignment.center,
                  children: [
                    if (widget.query.scope != 'author')
                      DropdownButton<String>(
                        value: _controller.query.scope,
                        items: [
                          for (final option in const {
                            'city': '全部公开动态',
                            'following': '我关注的人',
                            'friends': '我的好友',
                          }.entries)
                            DropdownMenuItem(
                              value: option.key,
                              enabled:
                                  option.key == 'city' ||
                                  widget.authorizationHeader() != null,
                              child: Text(option.value, style: tokens.body),
                            ),
                        ],
                        onChanged: _controller.current
                            ? (value) {
                                if (value != null) _filter(scope: value);
                              }
                            : null,
                      ),
                    DropdownButton<String>(
                      value: _window,
                      items: [
                        for (final option in const {
                          'all': '全部时间',
                          '7': '近 7 天',
                          '30': '近 30 天',
                          '90': '近 90 天',
                        }.entries)
                          DropdownMenuItem(
                            value: option.key,
                            child: Text(option.value, style: tokens.body),
                          ),
                      ],
                      onChanged: _controller.current
                          ? (value) {
                              if (value != null) _filter(window: value);
                            }
                          : null,
                    ),
                    IconButton(
                      tooltip: '刷新动态',
                      onPressed: _controller.loading || !_controller.current
                          ? null
                          : _controller.refresh,
                      icon: const Icon(Icons.refresh),
                    ),
                  ],
                ),
                if (_controller.loading && items.isEmpty)
                  Padding(
                    padding: const EdgeInsets.symmetric(vertical: 24),
                    child: Text('正在读取动态…', style: tokens.body),
                  ),
                if (_controller.error case final error?)
                  Padding(
                    padding: const EdgeInsets.symmetric(vertical: 16),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(error, style: tokens.body),
                        TextButton(
                          onPressed: _controller.current && !_controller.loading
                              ? _controller.refresh
                              : null,
                          child: const Text('重新读取'),
                        ),
                      ],
                    ),
                  ),
                if (!_controller.loading &&
                    _controller.error == null &&
                    items.isEmpty)
                  Padding(
                    padding: const EdgeInsets.symmetric(vertical: 24),
                    child: Text('这个范围还没有可查看的公开动态。', style: tokens.body),
                  ),
                for (final item in items) ...[
                  const Divider(height: 32),
                  ListTile(
                    contentPadding: EdgeInsets.zero,
                    minVerticalPadding: 12,
                    title: item.title.isEmpty
                        ? null
                        : Text(item.title, style: tokens.placeTitle),
                    subtitle: Padding(
                      padding: const EdgeInsets.only(top: 8),
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          if (item.body.isNotEmpty)
                            Text(
                              item.body,
                              maxLines: 3,
                              overflow: TextOverflow.ellipsis,
                              style: tokens.body,
                            ),
                          const SizedBox(height: 8),
                          Text(
                            [
                              if (item.authorPublicProfile != null)
                                item.authorPublicProfile!.displayName,
                              if (item.placeName.isNotEmpty ||
                                  item.cityName.isNotEmpty)
                                item.locationPrecision == 'place'
                                    ? item.placeName
                                    : item.cityName,
                              MaterialLocalizations.of(
                                context,
                              ).formatMediumDate(item.publishedAt.toLocal()),
                            ].join(' · '),
                            style: tokens.metadata,
                          ),
                          if (item.photo != null)
                            Text('含图片', style: tokens.metadata),
                        ],
                      ),
                    ),
                    trailing: const Icon(Icons.chevron_right),
                    onTap: () {
                      if (!_controller.current ||
                          !item.current ||
                          !_controller.items.any((m) => identical(m, item)))
                        return;
                      Navigator.of(context).push<void>(
                        MaterialPageRoute(
                          builder: (_) => PublicMomentPage(
                            momentID: item.id,
                            authorizationHeader: widget.authorizationHeader,
                            ownerID: widget.ownerID,
                            workspaceID: widget.workspaceID,
                            identityChanges: widget.identityChanges,
                            client: widget.client,
                            apiBaseUrl: widget.apiBaseUrl,
                            onOpenAuthor: widget.onOpenAuthor,
                          ),
                        ),
                      );
                    },
                  ),
                ],
                if (_controller.nextCursor != null)
                  Align(
                    alignment: Alignment.centerLeft,
                    child: TextButton(
                      onPressed: _controller.loading || !_controller.current
                          ? null
                          : _controller.loadMore,
                      child: Text(_controller.loading ? '正在读取…' : '查看更早的动态'),
                    ),
                  ),
              ],
            ),
          );
        },
      ),
    ),
  );
}
