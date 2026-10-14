import 'dart:convert';
import 'package:flutter/material.dart';
import '../app/birdtie_app_bar.dart';
import '../city/public_city_controller.dart';
import 'activity_detail_sheet.dart';
import 'place_detail_sheet.dart';
import 'remote_inbox_source.dart';

class InboxReviewResultPage extends StatefulWidget {
  const InboxReviewResultPage({
    super.key,
    required this.item,
    required this.source,
    required this.current,
    required this.identityChanges,
    required this.ownerID,
  });
  final InboxItem item;
  final RemoteInboxSource source;
  final bool Function() current;
  final Listenable identityChanges;
  final String? Function() ownerID;
  @override
  State<InboxReviewResultPage> createState() => _ReviewState();
}

class _ReviewState extends State<InboxReviewResultPage> {
  InboxReviewResult? _result;
  String? _error;
  bool _busy = false, _retired = false;
  int _serial = 0;
  late final _sourceVersion = widget.source.identityVersion;
  bool get _current =>
      mounted &&
      !_retired &&
      widget.current() &&
      widget.source.active &&
      widget.source.identityVersion == _sourceVersion;
  @override
  void initState() {
    super.initState();
    widget.identityChanges.addListener(_changed);
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (_current) _load();
    });
  }

  void _changed() {
    if (_current || _retired) return;
    _retired = true;
    ++_serial;
    setState(() {
      _result = null;
      _busy = false;
      _error = '身份或来源已变化，请返回通知重新打开。';
    });
  }

  Future<InboxReviewResult?> _load() async {
    if (!_current) return null;
    final serial = ++_serial;
    setState(() {
      _busy = true;
      _result = null;
      _error = null;
    });
    try {
      final v = await widget.source.reviewResult(widget.item);
      if (!_current || serial != _serial) return null;
      setState(() => _result = v);
      return v;
    } catch (_) {
      if (_current && serial == _serial)
        setState(() => _error = '当前审核结果暂不可读取，或此通知已不可访问。请重新核实。');
      return null;
    } finally {
      if (_current && serial == _serial) setState(() => _busy = false);
    }
  }

  Future<void> _open() async {
    final before = _result;
    if (!_current || _busy || before?.targetID == null) return;
    final v = await _load();
    if (!_current || v == null) return;
    if (v.targetID == null ||
        v.targetID != before!.targetID ||
        v.status != before.status) {
      setState(() => _error = '审核结果或可访问内容已变化，请检查当前结果。');
      return;
    }
    if (v.kind == 'place_candidate') {
      await showModalBottomSheet<void>(
        context: context,
        isScrollControlled: true,
        builder: (_) => PlaceDetailSheet(
          placeID: v.targetID!,
          authorizationHeader: () =>
              _current ? widget.source.authorizationHeader() : null,
          apiBaseUrl: widget.source.followApiBaseUrl,
          client: widget.source.followClient,
          ownerID: widget.ownerID,
          workspaceChanges: widget.identityChanges,
          publicContentFirst: true,
          onOpenActivity: (a) {
            if (_current) _activity(a.id);
          },
        ),
      );
    } else {
      await _activity(v.targetID!);
    }
  }

  Future<void> _activity(String id) async {
    if (!_current) return;
    final bearer = widget.source.authorizationHeader();
    if (bearer == null) return;
    setState(() => _busy = true);
    try {
      final response = await widget.source.followClient
          .get(
            Uri.parse(
              '${widget.source.followApiBaseUrl.replaceFirst(RegExp(r"/$"), "")}/v1/activities/${Uri.encodeComponent(id)}',
            ),
            headers: {'Authorization': bearer},
          )
          .timeout(const Duration(seconds: 10));
      if (!_current || bearer != widget.source.authorizationHeader()) return;
      if (response.statusCode != 200 || response.bodyBytes.length > 1024 * 1024)
        throw const FormatException('活动当前不可访问');
      final activity = PublicActivity.fromJson(
        jsonDecode(utf8.decode(response.bodyBytes))['data'],
      );
      if (activity.id != id) throw const FormatException('活动身份不符');
      await showModalBottomSheet<void>(
        context: context,
        isScrollControlled: true,
        builder: (_) => ActivityDetailSheet(
          activity: activity,
          authorizationHeader: () =>
              _current ? widget.source.authorizationHeader() : null,
          apiBaseUrl: widget.source.followApiBaseUrl,
          client: widget.source.followClient,
          identityChanges: widget.identityChanges,
          accountID: widget.ownerID,
          domainCurrent: () => _current,
          entrySource: 'inbox_review',
        ),
      );
    } catch (_) {
      if (_current) setState(() => _error = '正式活动当前不可访问，请重新读取审核结果。');
    } finally {
      if (_current) setState(() => _busy = false);
    }
  }

  @override
  void dispose() {
    ++_serial;
    widget.identityChanges.removeListener(_changed);
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final v = _current ? _result : null;
    return Scaffold(
      appBar: birdtieAppBar(context, title: const Text('本人提交的审核结果')),
      body: SafeArea(
        child: ListView(
          padding: const EdgeInsets.all(20),
          children: [
            if (_busy) const LinearProgressIndicator(),
            if (v != null) ...[
              Text(switch (v.status) {
                'published' => '审核通过，已发布',
                'linked_duplicate' => '审核通过，已关联已有地点',
                _ => '此次提交未通过审核',
              }, style: Theme.of(context).textTheme.titleLarge),
              const SizedBox(height: 12),
              if (v.targetID != null)
                FilledButton(
                  onPressed: _busy ? null : _open,
                  child: Text(
                    v.kind == 'place_candidate' ? '查看正式地点' : '查看正式活动',
                  ),
                )
              else
                Text(
                  v.status == 'rejected'
                      ? '本次提交没有发布为公开内容。'
                      : '正式内容当前不可访问，审核结果仍保留。',
                ),
            ],
            if (_error != null)
              Semantics(liveRegion: true, child: Text(_error!)),
            if (!_current) const Text('身份或来源已变化，请返回通知重新打开。'),
            if (_current)
              TextButton(
                onPressed: _busy ? null : _load,
                child: const Text('重新读取审核结果'),
              ),
          ],
        ),
      ),
    );
  }
}
