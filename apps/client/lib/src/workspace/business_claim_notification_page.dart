import 'dart:async';

import 'package:flutter/material.dart';

import 'business_api.dart';
import 'business_console_page.dart' show businessStateLabel;

/// Resolves the original management resource; notification text is not a
/// cached business claim, public profile, approval or Business Agent output.
class BusinessClaimNotificationPage extends StatefulWidget {
  const BusinessClaimNotificationPage({
    super.key,
    required this.businessID,
    required this.ownerID,
    required this.authorizationHeader,
    required this.isCurrent,
    required this.api,
  });
  final String businessID, ownerID;
  final String? Function() authorizationHeader;
  final bool Function() isCurrent;
  final BusinessApi api;

  @override
  State<BusinessClaimNotificationPage> createState() =>
      _BusinessClaimNotificationPageState();
}

class _BusinessClaimNotificationPageState
    extends State<BusinessClaimNotificationPage> {
  late final String _businessID, _ownerID;
  late final String? _bearer;
  late final BusinessApi _api;
  late final String? Function() _authorization;
  late final bool Function() _currentFrame;
  BusinessConsoleSnapshot? _snapshot;
  String? _error;
  bool _retired = false, _loading = false;
  int _serial = 0;

  @override
  void initState() {
    super.initState();
    _businessID = widget.businessID;
    _ownerID = widget.ownerID;
    _authorization = widget.authorizationHeader;
    _currentFrame = widget.isCurrent;
    _bearer = _authorization();
    _api = widget.api;
    unawaited(_load());
  }

  bool _valid() {
    if (_retired || !mounted) return false;
    if (_bearer == null ||
        !BusinessApi.validID(_businessID) ||
        !BusinessApi.validID(_ownerID) ||
        _authorization() != _bearer ||
        !_currentFrame()) {
      _retired = true;
      _serial++;
      _snapshot = null;
      return false;
    }
    return true;
  }

  @override
  void didUpdateWidget(covariant BusinessClaimNotificationPage oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (widget.businessID != _businessID ||
        widget.ownerID != _ownerID ||
        !identical(widget.api, _api) ||
        !identical(widget.authorizationHeader, _authorization) ||
        !identical(widget.isCurrent, _currentFrame)) {
      _retired = true;
      _snapshot = null;
      _serial++;
    }
  }

  Future<void> _load() async {
    if (!_valid() || _loading) return;
    final serial = ++_serial;
    setState(() {
      _loading = true;
      _snapshot = null;
      _error = null;
    });
    try {
      final value = await _api.read(_businessID);
      if (!_valid() || serial != _serial) return;
      if (value.business.id != _businessID ||
          !value.canManage ||
          !const {'owner', 'admin'}.contains(value.business.role)) {
        throw const BusinessApiException(403);
      }
      setState(() => _snapshot = value);
    } on Object catch (error) {
      if (!_valid() || serial != _serial) return;
      setState(
        () => _error = error is BusinessApiException
            ? error.message
            : '商家审核状态暂不可用，请重新读取。',
      );
    } finally {
      if (_valid() && serial == _serial) {
        setState(() => _loading = false);
      }
    }
  }

  @override
  void dispose() {
    _retired = true;
    _serial++;
    // The route borrows BusinessApi from its original Inbox source.
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final current = _valid();
    final value = current ? _snapshot : null;
    return Scaffold(
      appBar: AppBar(title: const Text('商家经营权审核')),
      body: SafeArea(
        child: ListView(
          padding: const EdgeInsets.all(24),
          children: [
            if (!current)
              const Text('账号、会话或来源已变化，请返回收件箱重新打开。')
            else if (_loading)
              const Center(child: CircularProgressIndicator())
            else ...[
              if (_error != null) Text(_error!),
              if (value != null) ...[
                Text(
                  value.business.name,
                  style: Theme.of(context).textTheme.headlineSmall,
                ),
                const SizedBox(height: 16),
                Text(
                  '当前经营权状态：${businessStateLabel(value.business.claimStatus)}',
                ),
                if (value.claim?['version'] case final int version)
                  Text('审核资料版本：$version'),
                const SizedBox(height: 16),
                const Text('这是商家工作台的当前状态，可能与收到通知时不同。经营权核验不代表交易背书。'),
                const SizedBox(height: 12),
                const Text('管理资料与成员请从设置中的商家工作台进入。'),
              ],
              const SizedBox(height: 16),
              FilledButton.tonalIcon(
                onPressed: _load,
                icon: const Icon(Icons.refresh),
                label: const Text('重新读取当前状态'),
              ),
            ],
          ],
        ),
      ),
    );
  }
}
