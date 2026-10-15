import 'dart:async';
import 'package:flutter/material.dart';
import '../app/birdtie_app_bar.dart';
import '../content/private_moment_controller.dart';
import '../content/moment_control.dart';
import 'now_ui_tokens.dart';

/// Uses the original human Moment controller and immutable operation journal.
/// The caller owns the borrowed controller and its transport.
class PublicMomentControlPage extends StatefulWidget {
  const PublicMomentControlPage({
    super.key,
    required this.moments,
    required this.momentID,
    this.current,
    this.identityChanges,
  });
  final PrivateMomentController moments;
  final String momentID;
  final bool Function()? current;
  final Listenable? identityChanges;
  @override
  State<PublicMomentControlPage> createState() =>
      _PublicMomentControlPageState();
}

class _PublicMomentControlPageState extends State<PublicMomentControlPage> {
  final _title = TextEditingController(), _body = TextEditingController();
  PrivateMoment? _moment;
  MomentControlPreview? _ownedPreview;
  String? _error;
  bool _loading = true, _dirty = false, _retired = false;
  int _request = 0;
  late final int _ownerEpoch;
  bool get _current =>
      mounted &&
      !_retired &&
      widget.current?.call() != false &&
      widget.moments.logicalIdentityEpoch == _ownerEpoch;
  @override
  void initState() {
    super.initState();
    _ownerEpoch = widget.moments.logicalIdentityEpoch;
    widget.moments.retirePreview();
    widget.identityChanges?.addListener(_identityChanged);
    widget.moments.addListener(_changed);
    unawaited(_load());
  }

  void _identityChanged() {
    if (!_current && !_retired) {
      _retired = true;
      ++_request;
      _dropOwnPreview();
      _title.clear();
      _body.clear();
      if (mounted)
        setState(() {
          _moment = null;
          _loading = false;
          _error = '当前身份或页面来源已变化，请返回后重新打开。';
        });
    }
  }

  void _changed() {
    if (!mounted) return;
    if (!_current) {
      _identityChanged();
      return;
    }
    setState(() {});
  }

  Future<void> _load() async {
    if (!_current) return;
    final request = ++_request;
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final m = await widget.moments.readOwned(
        widget.momentID,
        current: () => _current && request == _request,
      );
      if (!_current || request != _request) return;
      if (m == null || m.status != 'published' || m.visibility != 'public') {
        setState(() {
          _moment = null;
          _error = '这条动态已删除、不是本人公开动态，或暂不可读取。';
        });
        return;
      }
      setState(() {
        _moment = m;
        if (!_dirty) {
          _title.text = m.title;
          _body.text = m.body;
        }
      });
    } catch (_) {
      if (_current && request == _request)
        setState(() => _error = '本人动态暂不可读取，请重试。编辑内容已保留。');
    } finally {
      if (_current && request == _request) setState(() => _loading = false);
    }
  }

  void _edited(String _) {
    _dirty = true;
    _dropOwnPreview();
    setState(() => _error = null);
  }

  Future<void> _preview({bool deletion = false}) async {
    final m = _moment, c = widget.moments;
    if (!_current || m == null || c.operationBusy) return;
    try {
      if (deletion) {
        _ownedPreview = await c.previewWithdraw(m, current: () => _current);
      } else {
        _ownedPreview = await c.previewPublicUpdate(
          moment: m,
          title: _title.text.trim(),
          body: _body.text.trim(),
          current: () => _current,
        );
      }
      if (_current) setState(() {});
    } catch (_) {
      if (_current) setState(() => _error = '请检查标题和正文，再查看本次修改。');
    }
  }

  Future<void> _confirm(MomentControlPreview p) async {
    final c = widget.moments;
    if (!_current ||
        !_forThisMoment(p) ||
        !identical(p, _ownedPreview) ||
        !c.previewCurrent(p, current: () => _current))
      return;
    final done = await c.commit(p, current: () => _current);
    if (mounted && _current && done) {
      _retired = true;
      Navigator.of(context).pop(p.input.action);
    }
  }

  bool _forThisMoment(MomentControlPreview p) =>
      p.input.momentID == widget.momentID &&
      const {'UPDATE_PUBLIC', 'WITHDRAW'}.contains(p.input.action);
  void _dropOwnPreview() {
    final p = _ownedPreview;
    _ownedPreview = null;
    if (p != null &&
        _forThisMoment(p) &&
        identical(widget.moments.operationPreview, p))
      widget.moments.retirePreview();
  }

  @override
  void dispose() {
    _retired = true;
    ++_request;
    widget.identityChanges?.removeListener(_identityChanged);
    widget.moments.removeListener(_changed);
    _dropOwnPreview();
    _title.dispose();
    _body.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final c = widget.moments;
    final candidate = c.operationPreview;
    final p =
        candidate != null &&
            _forThisMoment(candidate) &&
            identical(candidate, _ownedPreview)
        ? candidate
        : null;
    final tokens = NowUiTokens.of(context);
    final ready = _current && !_loading && _moment != null;
    final canWrite = ready && c.canStartOperation;
    return Scaffold(
      appBar: birdtieAppBar(
        context,
        title: Text('管理这条动态', style: tokens.pageTitle),
      ),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          if (_loading) const LinearProgressIndicator(),
          if (_error != null) ...[
            Text(_error!, style: tokens.body),
            if (_current)
              TextButton(
                onPressed: _loading ? null : _load,
                child: const Text('重新读取'),
              ),
          ],
          if (ready) ...[
            Text('本人动态 · 公开可见', style: tokens.metadata),
            const SizedBox(height: 16),
            if (_moment!.title.isNotEmpty)
              TextField(
                controller: _title,
                enabled: !c.operationBusy,
                maxLength: 160,
                onChanged: _edited,
                decoration: const InputDecoration(labelText: '标题'),
              ),
            const SizedBox(height: 12),
            TextField(
              controller: _body,
              enabled: !c.operationBusy,
              minLines: 4,
              maxLines: 12,
              onChanged: _edited,
              decoration: const InputDecoration(labelText: '正文'),
            ),
            const SizedBox(height: 12),
            Wrap(
              spacing: 12,
              children: [
                FilledButton(
                  onPressed: canWrite ? () => _preview() : null,
                  child: const Text('查看本次修改'),
                ),
                TextButton(
                  onPressed: canWrite ? () => _preview(deletion: true) : null,
                  child: const Text('删除动态'),
                ),
              ],
            ),
          ],
          if (_current && p != null) ...[
            const Divider(height: 32),
            Text(
              p.input.action == 'WITHDRAW' ? '确认删除这条动态' : '确认本次公开修改',
              style: tokens.sheetTitle,
            ),
            const SizedBox(height: 12),
            if (p.input.action == 'WITHDRAW')
              Text(
                '这条动态将停止在列表、地点和地图中展示。删除动态不代表原始媒体已立即物理清除。',
                style: tokens.body,
              )
            else ...[
              if ((p.input.input!['title'] as String).isNotEmpty) ...[
                Text(
                  p.input.input!['title'] as String,
                  style: tokens.placeTitle,
                ),
                const SizedBox(height: 8),
              ],
              if ((p.input.input!['body'] as String).isNotEmpty)
                Text(p.input.input!['body'] as String, style: tokens.body),
              const SizedBox(height: 12),
              Text(
                p.preservedMedia != null
                    ? '受众仍为公开；这次保留已核实的${p.preservedMedia!.attachments.length}项媒体。'
                    : '受众仍为公开。未列为保留的旧图片将撤回，之后可重新添加。',
                style: tokens.metadata,
              ),
            ],
            const SizedBox(height: 16),
            FilledButton(
              onPressed:
                  c.operationBusy ||
                      !c.previewCurrent(p, current: () => _current)
                  ? null
                  : () => _confirm(p),
              child: Text(
                c.operationBusy
                    ? '正在提交…'
                    : p.input.action == 'WITHDRAW'
                    ? '确认删除动态'
                    : '确认保存公开修改',
              ),
            ),
            TextButton(
              onPressed: c.operationBusy
                  ? null
                  : () {
                      _dropOwnPreview();
                      setState(() {});
                    },
              child: const Text('返回编辑'),
            ),
          ],
          if (_current && (c.operationError ?? c.operationNotice) != null)
            Padding(
              padding: const EdgeInsets.only(top: 12),
              child: Text(
                c.operationError ?? c.operationNotice!,
                style: tokens.body,
              ),
            ),
          if (_current)
            for (final original in c.recoveryOperations.where(
              (p) =>
                  p.momentID == widget.momentID &&
                  const {
                    'UPDATE_PUBLIC',
                    'WITHDRAW',
                    'PUBLISH',
                  }.contains(p.action),
            )) ...[
              const Divider(height: 24),
              Text('上次操作结果待核实，不会重新发送。', style: tokens.body),
              Wrap(
                spacing: 12,
                children: [
                  TextButton(
                    onPressed: c.operationBusy
                        ? null
                        : () {
                            if (_current)
                              unawaited(c.recoverOperation(original));
                          },
                    child: const Text('核实原操作'),
                  ),
                  TextButton(
                    onPressed: c.operationBusy
                        ? null
                        : () {
                            if (_current)
                              unawaited(
                                c.recoverOperation(original, stop: true),
                              );
                          },
                    child: const Text('停止原操作'),
                  ),
                ],
              ),
            ],
        ],
      ),
    );
  }
}
