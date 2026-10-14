import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;

import '../config/birdtie_environment.dart';

typedef _FollowSource = ({
  String? token,
  String? owner,
  String? workspace,
  String targetType,
  String targetID,
  String base,
  http.Client client,
});
typedef _FollowFrame = ({_FollowSource source, int generation});

class _FollowRejected implements Exception {}

/// One-way Follow never establishes friendship, membership, or private access.
class FollowButton extends StatefulWidget {
  const FollowButton({
    super.key,
    required this.targetType,
    required this.targetID,
    required this.authorizationHeader,
    this.accountID,
    this.workspaceID,
    this.identityChanges,
    this.apiBaseUrl,
    this.client,
  });
  final String targetType;
  final String targetID;
  final String? Function() authorizationHeader;
  final String? Function()? accountID;
  final String? Function()? workspaceID;
  final Listenable? identityChanges;
  final String? apiBaseUrl;
  final http.Client? client;
  @override
  State<FollowButton> createState() => _FollowButtonState();
}

class _FollowButtonState extends State<FollowButton> {
  late http.Client _client;
  late bool _ownsClient;
  late _FollowSource _source;
  bool? _followed;
  bool _busy = false;
  bool _reading = false;
  bool _uncertain = false;
  bool _hasWriteReceipt = false;
  bool _syncScheduled = false;
  String? _notice;
  int _generation = 0;
  int _readSerial = 0;

  _FollowSource _capture() => (
    token: widget.authorizationHeader(),
    owner: widget.accountID?.call(),
    workspace: widget.workspaceID?.call(),
    targetType: widget.targetType,
    targetID: widget.targetID,
    base: (widget.apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl).replaceFirst(
      RegExp(r'/$'),
      '',
    ),
    client: _client,
  );
  bool _personal(_FollowSource source) =>
      source.token != null &&
      source.workspace == null &&
      source.base.isNotEmpty &&
      !(source.targetType == 'PERSON' &&
          source.owner != null &&
          source.owner == source.targetID);
  _FollowFrame get _frame => (source: _source, generation: _generation);
  bool _current(_FollowFrame frame) =>
      mounted &&
      frame.generation == _generation &&
      frame.source == _source &&
      frame.source == _capture() &&
      _personal(frame.source);
  Uri _endpoint(_FollowSource source) =>
      Uri.parse('${source.base}/v1/me/follows');

  @override
  void initState() {
    super.initState();
    _ownsClient = widget.client == null;
    _client = widget.client ?? http.Client();
    _source = _capture();
    widget.identityChanges?.addListener(_identityChanged);
    unawaited(_read(_frame));
  }

  void _identityChanged() {
    // Real notifications retire A -> B -> A even when the final token matches.
    if (mounted) _retire();
  }

  void _retire() {
    ++_generation;
    ++_readSerial;
    setState(() {
      _source = _capture();
      _followed = null;
      _busy = false;
      _reading = false;
      _uncertain = false;
      _hasWriteReceipt = false;
      _notice = null;
    });
    unawaited(_read(_frame));
  }

  @override
  void didUpdateWidget(FollowButton oldWidget) {
    super.didUpdateWidget(oldWidget);
    final transportChanged =
        !identical(oldWidget.client, widget.client) ||
        oldWidget.apiBaseUrl != widget.apiBaseUrl;
    if (!identical(oldWidget.client, widget.client)) {
      if (_ownsClient) _client.close();
      _client = widget.client ?? http.Client();
      _ownsClient = widget.client == null;
    }
    final notificationsChanged = !identical(
      oldWidget.identityChanges,
      widget.identityChanges,
    );
    if (notificationsChanged) {
      oldWidget.identityChanges?.removeListener(_identityChanged);
      widget.identityChanges?.addListener(_identityChanged);
    }
    if (transportChanged || notificationsChanged || _source != _capture())
      _retire();
  }

  @override
  void dispose() {
    ++_generation;
    ++_readSerial;
    widget.identityChanges?.removeListener(_identityChanged);
    if (_ownsClient) _client.close();
    super.dispose();
  }

  Future<bool> _read(_FollowFrame frame) async {
    if (!_current(frame)) return false;
    final serial = ++_readSerial;
    setState(() => _reading = true);
    try {
      final response = await frame.source.client
          .get(
            _endpoint(frame.source),
            headers: {'Authorization': frame.source.token!},
          )
          .timeout(const Duration(seconds: 12));
      if (!_current(frame) || serial != _readSerial) return false;
      if (response.statusCode != 200) return false;
      final body = jsonDecode(utf8.decode(response.bodyBytes));
      if (body is! Map<String, dynamic> || body['data'] is! List) return false;
      final rows = body['data'] as List;
      for (final row in rows) {
        if (row is! Map<String, dynamic> ||
            row['target'] is! Map<String, dynamic>)
          return false;
        final target = row['target'] as Map<String, dynamic>;
        if (target['targetType'] is! String || target['targetId'] is! String)
          return false;
      }
      final followed = rows.any((raw) {
        final target =
            (raw as Map<String, dynamic>)['target'] as Map<String, dynamic>;
        return target['targetType'] == frame.source.targetType &&
            target['targetId'] == frame.source.targetID;
      });
      if (!_current(frame) || serial != _readSerial) return false;
      setState(() => _followed = followed);
      return true;
    } catch (_) {
      return false;
    } finally {
      if (_current(frame) && serial == _readSerial)
        setState(() => _reading = false);
    }
  }

  Future<void> _toggle() async {
    final frame = _frame;
    if (!_current(frame) ||
        _busy ||
        _reading ||
        _uncertain ||
        _followed == null)
      return;
    final wasFollowed = _followed!;
    setState(() {
      _busy = true;
      _notice = null;
      _hasWriteReceipt = false;
    });
    try {
      final endpoint = _endpoint(frame.source);
      final response = wasFollowed
          ? await frame.source.client
                .delete(
                  endpoint.replace(
                    path:
                        '${endpoint.path}/'
                        '${Uri.encodeComponent(frame.source.targetType)}/'
                        '${Uri.encodeComponent(frame.source.targetID)}',
                  ),
                  headers: {'Authorization': frame.source.token!},
                )
                .timeout(const Duration(seconds: 12))
          : await frame.source.client
                .post(
                  endpoint,
                  headers: {
                    'Authorization': frame.source.token!,
                    'Content-Type': 'application/json',
                  },
                  body: jsonEncode({
                    'targetType': frame.source.targetType,
                    'targetId': frame.source.targetID,
                  }),
                )
                .timeout(const Duration(seconds: 12));
      if (!_current(frame)) return;
      if (response.statusCode != (wasFollowed ? 204 : 201)) {
        if (response.statusCode >= 400 && response.statusCode < 500)
          throw _FollowRejected();
        throw StateError('follow_result_unconfirmed');
      }
      setState(() {
        _hasWriteReceipt = true;
        _followed = null;
        _notice = '操作已提交，正在读取当前关注状态。';
      });
      final readable = await _read(frame);
      if (!_current(frame)) return;
      setState(() {
        _uncertain = !readable;
        _notice = readable ? null : '已收到操作回执，但当前关系未核实。请重新读取，不会自动重发。';
      });
    } catch (error) {
      if (!_current(frame)) return;
      setState(() {
        _followed = null;
        _uncertain = error is! _FollowRejected;
        _notice = _uncertain
            ? '关注操作结果未核实。仅重新读取当前关系，不会自动重发。'
            : '关注操作未完成。请重新读取当前关系。';
      });
      // GET proves current state, not the result of the original unknown write:
      // the existing Follow API has no operation-result endpoint.
      await _read(frame);
    } finally {
      if (_current(frame)) setState(() => _busy = false);
    }
  }

  Future<void> _recheck() async {
    if (_busy || _reading) return;
    final frame = _frame;
    final readable = await _read(frame);
    if (_current(frame) && readable && _hasWriteReceipt) {
      setState(() {
        _uncertain = false;
        _notice = null;
      });
    }
    // Unknown writes stay read-only in this frame; no implicit POST retry.
  }

  @override
  Widget build(BuildContext context) {
    if (_source != _capture()) {
      if (!_syncScheduled) {
        _syncScheduled = true;
        WidgetsBinding.instance.addPostFrameCallback((_) {
          _syncScheduled = false;
          if (mounted && _source != _capture()) _retire();
        });
      }
      return const SizedBox.shrink();
    }
    if (!_personal(_source) || (_followed == null && _notice == null))
      return const SizedBox.shrink();
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        if (_followed != null && !_uncertain)
          Tooltip(
            message: '关注不会建立好友关系或获得私密内容权限',
            child: TextButton.icon(
              style: TextButton.styleFrom(minimumSize: const Size(48, 48)),
              onPressed: _busy || _reading ? null : _toggle,
              icon: Icon(
                _followed!
                    ? Icons.notifications_active_outlined
                    : Icons.add_circle_outline,
              ),
              label: Text(_followed! ? '已关注' : '关注'),
            ),
          ),
        if (_notice != null) ...[
          Semantics(liveRegion: true, child: Text(_notice!)),
          if (_followed != null)
            Text('当前关系：${_followed! ? '已关注' : '未关注'}（重新读取）'),
          TextButton.icon(
            style: TextButton.styleFrom(minimumSize: const Size(48, 48)),
            onPressed: _busy || _reading ? null : _recheck,
            icon: const Icon(Icons.refresh),
            label: const Text('重新读取关注状态'),
          ),
        ],
      ],
    );
  }
}
