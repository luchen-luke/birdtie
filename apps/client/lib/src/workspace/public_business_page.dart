import 'dart:async';

import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import 'package:url_launcher/url_launcher.dart';

import '../city/public_city_controller.dart';
import 'business_api.dart';
import 'supplier_profile_api.dart';
import 'connections.dart';
import 'entity_action_contract.dart';
import 'entity_action_dispatcher.dart';

/// Ordinary public read. Merchant management facts are never supplied here.
class PublicBusinessPage extends StatefulWidget {
  const PublicBusinessPage({
    super.key,
    required this.businessID,
    required this.authorizationHeader,
    required this.onOpenActivity,
    this.workspaceID,
    this.identityChanges,
    this.apiBaseUrl,
    this.client,
    this.openExternal,
  });
  final String businessID;
  final String? Function() authorizationHeader;
  final String? Function()? workspaceID;
  final Listenable? identityChanges;
  final ValueChanged<PublicActivity> onOpenActivity;
  final String? apiBaseUrl;
  final http.Client? client;
  final Future<bool> Function(Uri)? openExternal;
  @override
  State<PublicBusinessPage> createState() => _PublicBusinessPageState();
}

class _PublicBusinessPageState extends State<PublicBusinessPage> {
  Future<void> _shareToFriend() async {
    final epoch = _epoch, identity = _current, id = widget.businessID;
    bool current() =>
        mounted &&
        epoch == _epoch &&
        identity == _current &&
        id == widget.businessID;
    await runEntityAction(
      context,
      ref: EntityActionRef('business', id),
      kind: EntityActionKind.share,
      authorizationHeader: () => current() ? identity.$1 : null,
      workspaceID: widget.workspaceID,
      identityChanges: widget.identityChanges,
      client: widget.client,
      apiBaseUrl: widget.apiBaseUrl,
      domainCurrent: current,
      handler: (_) async {
        if (!current()) return;
        await shareEntityToChat(
          context,
          authorizationHeader: () => current() ? identity.$1 : null,
          identityChanges: widget.identityChanges,
          workspaceID: widget.workspaceID,
          client: widget.client,
          type: 'business',
          id: id,
          apiBaseUrl: widget.apiBaseUrl,
        );
      },
    );
  }

  late SupplierProfileApi _api;
  PublicBusinessProfile? _profile;
  String? _error;
  bool _loading = true, _opening = false;
  int _epoch = 0;
  late (String?, String?) _identity;
  (String?, String?) get _current =>
      (widget.authorizationHeader(), widget.workspaceID?.call());
  @override
  void initState() {
    super.initState();
    _identity = _current;
    _api = SupplierProfileApi(
      authorizationHeader: () => widget.authorizationHeader(),
      workspaceID: () => widget.workspaceID?.call(),
      client: widget.client,
      apiBaseUrl: widget.apiBaseUrl,
    );
    widget.identityChanges?.addListener(_changed);
    unawaited(_load());
  }

  void _changed() {
    if (_identity == _current) return;
    _identity = _current;
    _epoch++;
    _profile = null;
    _opening = false;
    unawaited(_load());
  }

  @override
  void didUpdateWidget(PublicBusinessPage old) {
    super.didUpdateWidget(old);
    final transportChanged =
        !identical(old.client, widget.client) ||
        old.apiBaseUrl != widget.apiBaseUrl;
    if (transportChanged) {
      _epoch++;
      _api.dispose();
      _api = SupplierProfileApi(
        authorizationHeader: () => widget.authorizationHeader(),
        workspaceID: () => widget.workspaceID?.call(),
        client: widget.client,
        apiBaseUrl: widget.apiBaseUrl,
      );
    }

    if (old.identityChanges != widget.identityChanges) {
      old.identityChanges?.removeListener(_changed);
      widget.identityChanges?.addListener(_changed);
    }
    if (transportChanged ||
        !identical(old.authorizationHeader, widget.authorizationHeader) ||
        !identical(old.workspaceID, widget.workspaceID) ||
        !identical(old.identityChanges, widget.identityChanges) ||
        old.businessID != widget.businessID ||
        _identity != _current) {
      _identity = _current;
      unawaited(_load());
    }
  }

  @override
  void dispose() {
    _epoch++;
    widget.identityChanges?.removeListener(_changed);
    _api.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    final epoch = ++_epoch, identity = _current;
    setState(() {
      _loading = true;
      _opening = false;
      _profile = null;
      _error = null;
    });
    try {
      final p = await _api.readPublic(widget.businessID);
      if (mounted && epoch == _epoch && identity == _current) {
        setState(() {
          _profile = p;
          _loading = false;
        });
      }
    } catch (e) {
      if (mounted && epoch == _epoch && identity == _current) {
        setState(() {
          _loading = false;
          _error = e is BusinessApiException ? e.message : '商家资料暂不可用，请重新读取。';
        });
      }
    }
  }

  Future<void> _open(String value) async {
    final previous = _profile;
    if (previous == null || _opening) return;
    final epoch = _epoch, identity = _current;
    setState(() => _opening = true);
    try {
      final current = await _api.readPublic(widget.businessID);
      if (!mounted || epoch != _epoch || identity != _current) return;
      if (current.profileStatus != 'verified' ||
          current.version != previous.version ||
          current.validUntil == null ||
          !current.validUntil!.isAfter(DateTime.now().toUtc()) ||
          !current.links.contains(value)) {
        setState(() {
          _profile = current;
          _error = '资料已变化，请检查当前链接后再打开。';
        });
        return;
      }
      final uri = supplierHTTPS(value);
      if (uri == null) throw const BusinessApiException(503);
      final opened =
          await (widget.openExternal?.call(uri) ??
              launchUrl(uri, mode: LaunchMode.externalApplication));
      if (!opened && mounted && epoch == _epoch && identity == _current) {
        setState(() => _error = '链接未能打开，请检查设备设置。');
      }
    } catch (e) {
      if (mounted && epoch == _epoch && identity == _current) {
        setState(() {
          _profile = null;
          _error = e is BusinessApiException ? e.message : '暂时无法核实这个链接，请重新读取。';
        });
      }
    } finally {
      if (mounted && epoch == _epoch && identity == _current) {
        setState(() => _opening = false);
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final p = _profile;
    return Scaffold(
      appBar: AppBar(title: const Text('商家资料')),
      body: SafeArea(
        child: _loading
            ? const Center(child: CircularProgressIndicator())
            : ListView(
                padding: const EdgeInsets.all(24),
                children: [
                  if (_error != null) ...[
                    Text(_error!),
                    TextButton(onPressed: _load, child: const Text('重新读取商家资料')),
                  ],
                  if (p != null) ...[
                    Text(
                      p.name,
                      style: Theme.of(context).textTheme.headlineMedium,
                    ),
                    if (widget.authorizationHeader() != null &&
                        widget.workspaceID?.call() == null)
                      OutlinedButton.icon(
                        onPressed: _shareToFriend,
                        style: OutlinedButton.styleFrom(
                          minimumSize: const Size(48, 48),
                        ),
                        icon: const Icon(Icons.chat_bubble_outline),
                        label: const Text('发给好友'),
                      ),
                    const SizedBox(height: 12),
                    const Text('经营权已核验'),
                    const SizedBox(height: 8),
                    const Text('经营权核验说明商家主体的经营关系，不表示服务质量、交易安全或 Birdtie 推荐背书。'),
                    const SizedBox(height: 24),
                    if (p.profileStatus != 'verified')
                      const Text('商家尚无当前有效且获准公开的介绍与链接。'),
                    if (p.profileStatus == 'verified') ...[
                      if (p.description.isNotEmpty) Text(p.description),
                      if (p.reviewedAt != null)
                        Text('资料审核：${supplierDate(p.reviewedAt!)}'),
                      if (p.validUntil != null)
                        Text('公开有效期至：${supplierDate(p.validUntil!)}'),
                      const SizedBox(height: 16),
                      Text(
                        '商家提供的链接',
                        style: Theme.of(context).textTheme.titleMedium,
                      ),
                      if (p.links.isEmpty) const Text('商家未公开官方链接。'),
                      for (final link in p.links)
                        TextButton.icon(
                          onPressed: _opening ? null : () => _open(link),
                          icon: const Icon(Icons.open_in_new),
                          label: Text(link),
                        ),
                    ],
                    const SizedBox(height: 24),
                    Text(
                      '即将举行的公开活动',
                      style: Theme.of(context).textTheme.titleLarge,
                    ),
                    if (p.activities.isEmpty) const Text('目前没有即将举行的公开活动。'),
                    for (final activity in p.activities)
                      ListTile(
                        contentPadding: EdgeInsets.zero,
                        title: Text(activity.title),
                        subtitle: Text(activity.schedule),
                        trailing: const Icon(Icons.chevron_right),
                        onTap: () => widget.onOpenActivity(activity),
                      ),
                    TextButton(onPressed: _load, child: const Text('刷新公开资料')),
                  ],
                ],
              ),
      ),
    );
  }
}
