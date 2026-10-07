import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;

import '../config/birdtie_environment.dart';

/// One-way Follow never establishes friendship, membership, or private access.
class FollowButton extends StatefulWidget {
  const FollowButton({
    super.key,
    required this.targetType,
    required this.targetID,
    required this.authorizationHeader,
    this.apiBaseUrl,
    this.client,
  });

  final String targetType;
  final String targetID;
  final String? Function() authorizationHeader;
  final String? apiBaseUrl;
  final http.Client? client;

  @override
  State<FollowButton> createState() => _FollowButtonState();
}

class _FollowButtonState extends State<FollowButton> {
  late final http.Client _client = widget.client ?? http.Client();
  bool? _followed;
  bool _busy = false;
  int _serial = 0;
  String? _loadedToken;
  String? _loadingToken;

  String get _base => widget.apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl;
  Uri get _endpoint =>
      Uri.parse('${_base.replaceFirst(RegExp(r'/$'), '')}/v1/me/follows');

  @override
  void initState() {
    super.initState();
    unawaited(_load());
  }

  @override
  void didUpdateWidget(FollowButton oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.targetType != widget.targetType ||
        oldWidget.targetID != widget.targetID) {
      _followed = null;
      _loadedToken = null;
      unawaited(_load());
    }
  }

  @override
  void dispose() {
    ++_serial;
    if (widget.client == null) _client.close();
    super.dispose();
  }

  Future<void> _load() async {
    final token = widget.authorizationHeader();
    if (token == null || _base.isEmpty) return;
    _loadingToken = token;
    final serial = ++_serial;
    try {
      final response = await _client
          .get(_endpoint, headers: {'Authorization': token})
          .timeout(const Duration(seconds: 12));
      if (response.statusCode != 200) return;
      final rows =
          (jsonDecode(utf8.decode(response.bodyBytes))
                  as Map<String, dynamic>)['data']
              as List<dynamic>;
      if (!mounted ||
          serial != _serial ||
          widget.authorizationHeader() != token) {
        return;
      }
      setState(
        () {
          _loadedToken = token;
          _followed = rows.any((raw) {
            final target = (raw as Map<String, dynamic>)['target']
                as Map<String, dynamic>?;
            return target?['targetType'] == widget.targetType &&
                target?['targetId'] == widget.targetID;
          });
        },
      );
    } catch (_) {
      // The Follow control stays hidden if the optional service is unavailable.
    } finally {
      if (_loadingToken == token) _loadingToken = null;
    }
  }

  Future<void> _toggle() async {
    final token = widget.authorizationHeader();
    if (_busy || _followed == null || token == null || token != _loadedToken) {
      return;
    }
    setState(() => _busy = true);
    try {
      final http.Response response;
      if (_followed!) {
        response = await _client
            .delete(
              _endpoint.replace(
                path:
                    '${_endpoint.path}/${widget.targetType}/${widget.targetID}',
              ),
              headers: {'Authorization': token},
            )
            .timeout(const Duration(seconds: 12));
        if (response.statusCode != 204) throw StateError('unfollow failed');
      } else {
        response = await _client
            .post(
              _endpoint,
              headers: {
                'Authorization': token,
                'Content-Type': 'application/json',
              },
              body: jsonEncode({
                'targetType': widget.targetType,
                'targetId': widget.targetID,
              }),
            )
            .timeout(const Duration(seconds: 12));
        if (response.statusCode != 201) throw StateError('follow failed');
      }
      if (mounted && widget.authorizationHeader() == token) {
        setState(() => _followed = !_followed!);
      }
    } catch (_) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(const SnackBar(content: Text('关注操作失败，请稍后重试。')));
      }
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final token = widget.authorizationHeader();
    if (token != null &&
        token != _loadedToken &&
        token != _loadingToken &&
        _base.isNotEmpty) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted && widget.authorizationHeader() == token) unawaited(_load());
      });
    }
    if (_followed == null || token == null || token != _loadedToken) {
      return const SizedBox.shrink();
    }
    return Tooltip(
      message: '关注不会建立好友关系或获得私密内容权限',
      child: TextButton.icon(
        onPressed: _busy ? null : _toggle,
        icon: Icon(
          _followed!
              ? Icons.notifications_active_outlined
              : Icons.add_circle_outline,
        ),
        label: Text(_followed! ? '已关注' : '关注'),
      ),
    );
  }
}
