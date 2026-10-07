import 'dart:async';

import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;

import 'business_api.dart';
import 'supplier_profile_api.dart';

class BusinessPublicPermissionPage extends StatefulWidget {
  const BusinessPublicPermissionPage({
    super.key,
    required this.businessID,
    required this.businessName,
    required this.authorizationHeader,
    required this.workspaceID,
    required this.identityChanges,
    this.apiBaseUrl,
    this.client,
  });
  final String businessID, businessName;
  final String? Function() authorizationHeader, workspaceID;
  final Listenable identityChanges;
  final String? apiBaseUrl;
  final http.Client? client;
  @override
  State<BusinessPublicPermissionPage> createState() =>
      _BusinessPublicPermissionPageState();
}

class _BusinessPublicPermissionPageState
    extends State<BusinessPublicPermissionPage> {
  late final SupplierProfileApi _api;
  BusinessPublicationState? _state;
  String? _error, _message;
  bool _busy = false, _checked = false;
  int _epoch = 0;
  late (String?, String?) _identity;
  (String?, String?) get _current =>
      (widget.authorizationHeader(), widget.workspaceID());
  @override
  void initState() {
    super.initState();
    _identity = _current;
    _api = SupplierProfileApi(
      authorizationHeader: () => widget.authorizationHeader(),
      workspaceID: () => widget.workspaceID(),
      apiBaseUrl: widget.apiBaseUrl,
      client: widget.client,
    );
    widget.identityChanges.addListener(_changed);
    unawaited(_load());
  }

  void _changed() {
    if (_identity == _current) return;
    _identity = _current;
    _epoch++;
    setState(() {
      _state = null;
      _checked = false;
      _busy = false;
      _message = null;
      _error = '工作身份已变化，请重新读取后检查。';
    });
  }

  @override
  void didUpdateWidget(BusinessPublicPermissionPage old) {
    super.didUpdateWidget(old);
    if (old.identityChanges != widget.identityChanges) {
      old.identityChanges.removeListener(_changed);
      widget.identityChanges.addListener(_changed);
    }
    if (old.businessID != widget.businessID || _identity != _current) {
      _identity = _current;
      _message = null;
      unawaited(_load());
    }
  }

  @override
  void dispose() {
    _epoch++;
    widget.identityChanges.removeListener(_changed);
    _api.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    final epoch = ++_epoch, identity = _current;
    setState(() {
      _busy = true;
      _checked = false;
      _state = null;
      _error = null;
    });
    try {
      final s = await _api.readPermission(widget.businessID);
      if (mounted && epoch == _epoch && identity == _current) {
        setState(() => _state = s);
      }
    } catch (e) {
      if (mounted && epoch == _epoch && identity == _current) {
        setState(
          () =>
              _error = e is BusinessApiException ? e.message : '无法读取公开权限，请重试。',
        );
      }
    } finally {
      if (mounted && epoch == _epoch && identity == _current) {
        setState(() => _busy = false);
      }
    }
  }

  Future<void> _submit(bool revoke) async {
    final s = _state;
    if (s == null || _busy || (!revoke && !_checked)) return;
    final preview = s.preview, epoch = _epoch, identity = _current;
    setState(() => _busy = true);
    try {
      final current = await _api.readPermission(widget.businessID);
      if (!mounted || epoch != _epoch || identity != _current) return;
      if (current.version != s.version ||
          !revoke &&
              (preview == null ||
                  current.preview?.snapshot != preview.snapshot)) {
        setState(() {
          _state = current;
          _checked = false;
          _error = '内容、权限或有效期已变化，请检查后重新确认。';
        });
        return;
      }
      if (revoke) {
        final yes = await showDialog<bool>(
          context: context,
          builder: (dialogContext) => AnimatedBuilder(
            animation: widget.identityChanges,
            builder: (_, _) => AlertDialog(
              title: const Text('撤回公开资料'),
              content: Text(
                '商家：${widget.businessName}\n撤回后公众将无法查看这次批准的介绍与链接；经营权状态和活动各自保留原权限。',
              ),
              actions: [
                TextButton(
                  onPressed: () => Navigator.pop(dialogContext, false),
                  child: const Text('取消'),
                ),
                FilledButton(
                  onPressed: epoch == _epoch && identity == _current
                      ? () => Navigator.pop(dialogContext, true)
                      : null,
                  child: Text(
                    epoch == _epoch && identity == _current
                        ? '确认撤回'
                        : '身份已变化，请取消并重新检查',
                  ),
                ),
              ],
            ),
          ),
        );
        if (yes != true ||
            !mounted ||
            epoch != _epoch ||
            identity != _current) {
          return;
        }
      }
      await _api.changePermission(widget.businessID, {
        'expectedVersion': s.version,
        'expectedProfileVersion': revoke ? 0 : preview!.version,
        'action': revoke ? 'revoke' : 'publish',
        'validUntil': revoke ? '' : preview!.until.toUtc().toIso8601String(),
        'sourceSnapshot': revoke ? '' : preview!.snapshot,
      });
      if (!mounted || epoch != _epoch || identity != _current) return;
      _message = '操作已提交，下面显示重新读取的当前公开状态。';
      await _load();
    } catch (e) {
      if (mounted && epoch == _epoch && identity == _current) {
        setState(() {
          _checked = false;
          _state = null;
          _error = e is BusinessApiException ? e.message : '操作结果尚未确认，请重新读取。';
        });
        if (e is BusinessApiException && e.outcomeUnknown) {
          _message = '提交结果待核实，不会自动重发。';
          await _load();
        }
      }
    } finally {
      if (mounted && epoch == _epoch && identity == _current) {
        setState(() => _busy = false);
      }
    }
  }

  String _status(String s) => switch (s) {
    'active' => '当前公开',
    'revoked' => '已撤回',
    'source_changed' => '旧批准已失效',
    'unpublished' => '尚未批准公开',
    _ => '状态待核实',
  };
  @override
  Widget build(BuildContext context) {
    final s = _state, p = s?.preview;
    return Scaffold(
      appBar: AppBar(title: const Text('公开商家资料')),
      body: SafeArea(
        child: ListView(
          padding: const EdgeInsets.all(24),
          children: [
            Text(
              widget.businessName,
              style: Theme.of(context).textTheme.headlineSmall,
            ),
            const SizedBox(height: 12),
            const Text('以本人商家所有者身份操作。管理审核、公开资料与 Agent 使用许可分别生效。'),
            if (_message != null) Text(_message!),
            if (_error != null) Text(_error!),
            if (_busy) const LinearProgressIndicator(),
            if (s != null) ...[
              const SizedBox(height: 16),
              Text('公开状态：${_status(s.status)}'),
              if (p == null) const Text('当前没有可批准公开的已审核资料。请先完成资料审核。'),
              if (p != null) ...[
                const SizedBox(height: 24),
                Text('公众将看到的资料', style: Theme.of(context).textTheme.titleLarge),
                Text('商家名称：${p.name}'),
                Text('介绍：${p.description.isEmpty ? '未填写' : p.description}'),
                Text('官方链接：${p.links.isEmpty ? '未填写' : p.links.join('\n')}'),
                Text('资料审核：${supplierDate(p.reviewedAt)}'),
                Text('公开有效期至：${supplierDate(p.until)}'),
                const SizedBox(height: 12),
                const Text(
                  '受众是所有公众。仅公开上述名称、介绍和链接；营业资料、证明文件与审核人员不随此批准公开。资料修改、撤权或过期后需要重新批准。这不是交易背书。',
                ),
                CheckboxListTile(
                  contentPadding: EdgeInsets.zero,
                  value: _checked,
                  onChanged: _busy
                      ? null
                      : (v) => setState(() => _checked = v == true),
                  title: const Text('我已检查具体资料、公众受众和有效期，并同意公开'),
                ),
                FilledButton(
                  onPressed: _busy || !_checked ? null : () => _submit(false),
                  child: const Text('确认公开这些资料'),
                ),
              ],
              if (s.version > 0 && s.status != 'revoked')
                TextButton(
                  onPressed: _busy ? null : () => _submit(true),
                  child: const Text('撤回公开资料'),
                ),
            ],
            TextButton(
              onPressed: _busy ? null : _load,
              child: const Text('重新读取公开状态'),
            ),
          ],
        ),
      ),
    );
  }
}
