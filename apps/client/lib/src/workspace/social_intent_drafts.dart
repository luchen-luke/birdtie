import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';
import 'social_intent_draft_model.dart';
import 'social_intent_creation_pending_store.dart';
import 'social_intent_creation_api.dart';

class SocialIntentDraftPage extends StatefulWidget {
  const SocialIntentDraftPage({
    super.key,
    required this.authorizationHeader,
    this.authorizationChanges,
    this.apiBaseUrl,
    this.client,
    this.agentTaskId,
    this.suggestedTitle,
    this.suggestedCategory,
    this.suggestedArea,
    this.onClose,
    this.contextNote,
    this.initialModality,
    this.ownerID,
    this.workspaceID,
    this.workspaceChanges,
    this.current,
    this.onManageIntent,
    this.pendingStore,
  });
  final String? Function() authorizationHeader;
  final Listenable? authorizationChanges, workspaceChanges;
  final String? apiBaseUrl,
      agentTaskId,
      suggestedTitle,
      suggestedCategory,
      suggestedArea,
      contextNote,
      initialModality;
  final http.Client? client;
  final String? Function()? ownerID, workspaceID;
  final bool Function()? current;
  final VoidCallback? onClose;
  final ValueChanged<String>? onManageIntent;
  final SocialIntentCreationPendingStore? pendingStore;
  @override
  State<SocialIntentDraftPage> createState() => _SocialIntentDraftPageState();
}

class _DraftBinding {
  const _DraftBinding(
    this.token,
    this.owner,
    this.workspace,
    this.valid,
    this.base,
    this.client,
    this.task,
    this.pendingStore,
  );
  final String? token, owner, workspace, task;
  final bool valid;
  final String base;
  final http.Client? client;
  final SocialIntentCreationPendingStore pendingStore;
  bool same(_DraftBinding other) =>
      token == other.token &&
      owner == other.owner &&
      workspace == other.workspace &&
      valid == other.valid &&
      base == other.base &&
      identical(client, other.client) &&
      identical(pendingStore, other.pendingStore) &&
      task == other.task;
  bool get personal => token != null && workspace == null && valid;
}

class _SocialIntentDraftPageState extends State<SocialIntentDraftPage> {
  late http.Client _client;
  late _DraftBinding _binding;
  late SocialIntentDraftModel _draft;
  final _title = TextEditingController(),
      _category = TextEditingController(),
      _area = TextEditingController(),
      _platform = TextEditingController();
  bool _loading = false,
      _saving = false,
      _sourceInvalidated = false,
      _organized = false,
      _expanded = false,
      _creationUncertain = false,
      _pendingBlocked = false,
      _pendingDifferentDraft = false;
  int _serial = 0;
  BuildContext? _dialogContext;
  String? _error, _listError, _noEffectNotice;
  SocialIntentCreationReceipt? _receipt, _priorReceipt, _verifiedPrevious;
  bool get _draftLocked => _creationUncertain && !_pendingDifferentDraft;
  PendingSocialIntentCreation? _pending;
  SocialIntentCreationPendingStore get _pendingStore =>
      widget.pendingStore ?? const SecureSocialIntentCreationPendingStore();
  List<Map<String, dynamic>> _drafts = const [];

  _DraftBinding _capture() => _DraftBinding(
    widget.authorizationHeader(),
    widget.ownerID?.call(),
    widget.workspaceID?.call(),
    widget.current?.call() ?? true,
    widget.apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl,
    widget.client,
    widget.agentTaskId,
    _pendingStore,
  );
  Uri _endpoint(_DraftBinding b, [String suffix = '']) => Uri.parse(
    '${b.base.replaceFirst(RegExp(r'/$'), '')}/v1/me/social-intents$suffix',
  );
  bool _current(int serial, _DraftBinding b) =>
      mounted && serial == _serial && b.same(_binding) && b.same(_capture());
  void _syncEditors() {
    _title.text = _draft.title;
    _category.text = _draft.category;
    _area.text = _draft.area;
    _platform.text = _draft.platform;
  }

  void _reset({bool suggestions = false}) {
    _draft = SocialIntentDraftModel(
      title: suggestions ? widget.suggestedTitle : null,
      category: suggestions ? widget.suggestedCategory : null,
      area: suggestions ? widget.suggestedArea : null,
      modality: suggestions ? widget.initialModality : null,
    );
    _organized = _draft.title.isNotEmpty;
    _expanded = false;
    _creationUncertain = false;
    _receipt = null;
    _priorReceipt = null;
    _verifiedPrevious = null;
    _pending = null;
    _pendingBlocked = false;
    _pendingDifferentDraft = false;
    _noEffectNotice = null;
    _syncEditors();
  }

  void _listen(SocialIntentDraftPage w, bool add) {
    final listeners = <Listenable>{
      if (w.authorizationChanges != null) w.authorizationChanges!,
      if (w.workspaceChanges != null) w.workspaceChanges!,
    };
    for (final item in listeners) {
      if (add) {
        item.addListener(_identityChanged);
      } else {
        item.removeListener(_identityChanged);
      }
    }
  }

  @override
  void initState() {
    super.initState();
    _client = widget.client ?? http.Client();
    _binding = _capture();
    _reset(suggestions: true);
    _listen(widget, true);
    _load();
  }

  @override
  void didUpdateWidget(covariant SocialIntentDraftPage oldWidget) {
    super.didUpdateWidget(oldWidget);
    _listen(oldWidget, false);
    _listen(widget, true);
    if (!identical(oldWidget.client, widget.client)) {
      if (oldWidget.client == null) _client.close();
      _client = widget.client ?? http.Client();
    }
    _identityChanged();
  }

  void _closeConfirmation() {
    final dialog = _dialogContext;
    _dialogContext = null;
    if (dialog == null || !dialog.mounted) return;
    final route = ModalRoute.of(dialog);
    if (route?.isCurrent == true) {
      Navigator.of(dialog).pop();
    } else if (route != null) {
      Navigator.of(dialog).removeRoute(route);
    }
  }

  void _identityChanged() {
    final next = _capture();
    if (next.same(_binding)) return;
    _binding = next;
    ++_serial;
    _closeConfirmation();
    _reset();
    _pendingDifferentDraft = true;
    setState(() {
      _drafts = const [];
      _loading = false;
      _saving = false;
      _error = null;
      _listError = null;
      _sourceInvalidated = widget.agentTaskId != null;
    });
    if (next.personal) _load(restoreInputs: false);
  }

  @override
  void dispose() {
    ++_serial;
    _listen(widget, false);
    _title.dispose();
    _category.dispose();
    _area.dispose();
    _platform.dispose();
    if (widget.client == null) _client.close();
    super.dispose();
  }

  Future<void> _load({bool restoreInputs = true}) async {
    final b = _capture();
    if (!b.personal || b.base.isEmpty) return;
    final serial = ++_serial, client = _client;
    setState(() {
      _loading = true;
      _listError = null;
      _drafts = const [];
    });
    try {
      if (socialIntentIDValid(b.owner)) {
        try {
          final pending = await b.pendingStore.read(b.base, b.owner!);
          if (!_current(serial, b)) return;
          _pendingBlocked = false;
          if (pending != null) {
            setState(() {
              _pending = pending;
              if (restoreInputs && !_pendingDifferentDraft) {
                _draft = SocialIntentDraftModel.fromPayload(pending.draft);
                _syncEditors();
                _organized = true;
              }
              _creationUncertain = true;
              _error = '已恢复原保存内容，正在核实结果；不会自动重新保存。';
            });
            await _checkPending(serial, b, client, pending);
            if (!_current(serial, b)) return;
          }
        } catch (_) {
          if (!_current(serial, b)) return;
          setState(() {
            _pendingBlocked = true;
            _error = '本机核实记录暂不可用，无法安全开始新的保存。请重新读取记录后再试。';
          });
        }
      }
      final response = await client
          .get(_endpoint(b), headers: {'Authorization': b.token!})
          .timeout(const Duration(seconds: 12));
      if (response.statusCode != 200) {
        throw const FormatException('list rejected');
      }
      final data = jsonDecode(utf8.decode(response.bodyBytes));
      if (data is! Map<String, dynamic> ||
          data['data'] is! List ||
          (data['data'] as List).length > 100) {
        throw const FormatException('invalid list');
      }
      final rows = (data['data'] as List).cast<Map<String, dynamic>>();
      if (rows.any(
        (r) =>
            r.containsKey('creatorAccountId') &&
            b.owner != null &&
            r['creatorAccountId'] != b.owner,
      )) {
        throw const FormatException('foreign owner');
      }
      if (_current(serial, b)) {
        setState(() => _drafts = List.unmodifiable(rows));
      }
    } catch (_) {
      if (_current(serial, b)) {
        setState(
          () => _listError = _receipt == null
              ? '草稿加载失败，请重试。'
              : '草稿已保存；列表刷新失败，可重新读取。',
        );
      }
    } finally {
      if (_current(serial, b)) setState(() => _loading = false);
    }
  }

  void _organize() {
    _identityChanged();
    if (_sourceInvalidated || _receipt != null || _draftLocked) return;
    setState(() {
      _draft.organize();
      _syncEditors();
      _organized = true;
      _error = null;
    });
  }

  Future<void> _save() async {
    if (_saving ||
        _loading ||
        _pendingBlocked ||
        _creationUncertain ||
        _receipt != null ||
        _priorReceipt != null) {
      return;
    }
    _identityChanged();
    if (_sourceInvalidated) {
      setState(() => _error = '登录状态已更新，请返回 Agent 重新选择本人的活动查询。');
      return;
    }
    final missing = _draft.missing(now: DateTime.now());
    if (missing.isNotEmpty) {
      setState(() {
        _organized = true;
        _error =
            _draft.modality != null &&
                ((_draft.modality != 'ONLINE' && _draft.area.trim().isEmpty) ||
                    (_draft.modality == 'HYBRID' &&
                        _draft.platform.trim().isEmpty))
            ? '请填写标题及当前方式所需的区域或线上方式。'
            : missing.take(2).join('\n');
      });
      return;
    }
    final b = _capture();
    if (!b.personal || b.base.isEmpty || !socialIntentIDValid(b.owner)) {
      setState(
        () => _error = b.workspace != null
            ? '请切回个人工作区保存本人的私人草稿。'
            : '请先登录并核实本人账号后保存草稿。',
      );
      return;
    }
    final serial = ++_serial, client = _client;
    late PendingSocialIntentCreation pending;
    try {
      pending = PendingSocialIntentCreation.capture(
        environment: b.base,
        owner: b.owner!,
        source: b.task ?? '',
        draft: _draft.payload(fromTask: b.task != null),
      );
    } catch (_) {
      if (_current(serial, b)) {
        setState(() => _error = '原草稿内容或活动查询无法核实，请检查后再保存。');
      }
      return;
    }
    setState(() {
      _saving = true;
      _error = null;
    });
    try {
      await b.pendingStore.write(b.base, b.owner!, pending);
      if (!_current(serial, b)) return;
      setState(() {
        _pending = pending;
        _pendingDifferentDraft = false;
      });
    } catch (_) {
      if (_current(serial, b)) {
        setState(() {
          _saving = false;
          _pendingBlocked = true;
          _error = '本机核实记录未安全保存，尚未提交。请重新读取记录后再试。';
        });
      }
      return;
    }
    await _submitPending(serial, b, client, pending);
  }

  Future<void> _submitPending(
    int serial,
    _DraftBinding b,
    http.Client client,
    PendingSocialIntentCreation pending,
  ) async {
    try {
      final receipt =
          await SocialIntentCreationAPI(
            client: client,
            base: b.base,
            token: b.token!,
          ).create(
            owner: pending.owner,
            operation: pending.operation,
            digest: pending.digest,
            source: pending.source,
            draft: pending.draft,
          );
      if (!_current(serial, b)) return;
      await _acceptCreation(serial, b, pending, receipt);
      if (_current(serial, b)) await _load();
    } catch (_) {
      if (_current(serial, b)) {
        _unknown(
          pending.source.isEmpty
              ? '保存结果未知，请核实本人记录，不要重复保存。'
              : '此查询的原保存结果未知，当前回执无法核实它的来源。请核实本人记录，不要重复保存。',
        );
      }
    } finally {
      if (_current(serial, b)) setState(() => _saving = false);
    }
  }

  Future<void> _acceptCreation(
    int serial,
    _DraftBinding b,
    PendingSocialIntentCreation pending,
    SocialIntentCreationReceipt receipt,
  ) async {
    if (!_current(serial, b)) return;
    bool cleared = false;
    try {
      await b.pendingStore.delete(b.base, b.owner!, pending);
      cleared = true;
    } catch (_) {}
    if (!_current(serial, b)) return;
    setState(() {
      _pending = cleared ? null : pending;
      _creationUncertain = !cleared;
      _saving = false;
      if (_pendingDifferentDraft) {
        _verifiedPrevious =
            receipt.committed || receipt.reason == 'SOURCE_ALREADY_EXISTS'
            ? receipt
            : null;
        _receipt = null;
        _priorReceipt = null;
      } else {
        _receipt = receipt.committed ? receipt : null;
        _priorReceipt = receipt.reason == 'SOURCE_ALREADY_EXISTS'
            ? receipt
            : null;
      }
      _error = receipt.committed
          ? (cleared ? null : '原保存已核实，但本机核实记录尚未清理。重新读取可继续核实。')
          : (receipt.reason == 'SOURCE_ALREADY_EXISTS'
                ? '此查询此前已保存意图；本次输入没有再次保存。请查看原意图并在那里修改。'
                : '保存被拒绝，请检查内容后再试。');
      _noEffectNotice = receipt.committed
          ? null
          : switch (receipt.reason) {
              'EXPIRED' => '寻找有效期已过，原操作未产生意图。修改后可明确保存新的草稿。',
              'SOURCE_CHANGED' => '原保存期间相关信息已变化，原操作未产生意图。请检查内容后再保存。',
              'SOURCE_UNAVAILABLE' => '原活动查询或资料已不可用，原操作未产生意图。',
              'SOURCE_ALREADY_EXISTS' => '以下入口使用原意图的真实ID；不会把本次未保存的输入冒充原内容。',
              _ => null,
            };
    });
  }

  Future<void> _checkPending(
    int serial,
    _DraftBinding b,
    http.Client client,
    PendingSocialIntentCreation pending,
  ) async {
    try {
      final receipt =
          await SocialIntentCreationAPI(
            client: client,
            base: b.base,
            token: b.token!,
          ).read(
            owner: pending.owner,
            operation: pending.operation,
            digest: pending.digest,
            source: pending.source,
          );
      if (!_current(serial, b)) return;
      if (receipt == null) {
        _unknown('尚未查到原保存回执，结果仍待核实；这不代表保存失败。可再次核实，或明确重试同一次保存。');
        return;
      }
      await _acceptCreation(serial, b, pending, receipt);
    } catch (_) {
      if (_current(serial, b)) {
        _unknown(
          pending.source.isEmpty
              ? '原保存结果未知，暂时无法核实。请恢复当前本人登录后再次核实，不要重新创建。'
              : '此查询的原保存结果未知，当前回执无法核实它的来源。请核实本人记录，不要重复保存。',
        );
      }
    }
  }

  Future<void> _retryOriginal() async {
    _identityChanged();
    final b = _capture(), pending = _pending;
    if (_saving ||
        _loading ||
        pending == null ||
        !b.personal ||
        b.owner != pending.owner ||
        !b.same(_binding) ||
        _receipt != null ||
        _priorReceipt != null) {
      return;
    }
    try {
      pending.bound(b.base, b.owner!);
    } catch (_) {
      return;
    }
    final serial = ++_serial, client = _client;
    setState(() {
      _saving = true;
      _error = null;
    });
    // A fresh explicit click retries only the immutable original request/key;
    // restoring storage, returning to a page or logging in never executes it.
    await _submitPending(serial, b, client, pending);
  }

  void _unknown(String message) => setState(() {
    _creationUncertain = true;
    _saving = false;
    _error = message;
  });

  Future<void> _cancel(String id) async {
    if (_saving || _loading || _creationUncertain) return;
    _identityChanged();
    final b = _capture();
    if (!b.personal || b.base.isEmpty) {
      setState(() => _error = '请先登录个人账号后取消意图。');
      return;
    }
    bool exists() => _drafts.any(
      (r) =>
          r['id'] == id && ['DRAFT', 'ACTIVE', 'MATCHED'].contains(r['status']),
    );
    if (!exists()) return;
    final serial = ++_serial, client = _client;
    setState(() {
      _saving = true;
      _error = null;
    });
    try {
      final confirmed = await showDialog<bool>(
        context: context,
        builder: (dialogContext) {
          _dialogContext = dialogContext;
          return AlertDialog(
            title: const Text('取消这条意图？'),
            content: const Text('取消后不会再出现在发现结果中。'),
            actions: [
              TextButton(
                onPressed: () {
                  if (_current(serial, b)) Navigator.pop(dialogContext, false);
                },
                child: const Text('返回'),
              ),
              FilledButton(
                onPressed: () {
                  if (_current(serial, b)) Navigator.pop(dialogContext, true);
                },
                child: const Text('确认取消'),
              ),
            ],
          );
        },
      );
      _dialogContext = null;
      if (confirmed != true || !_current(serial, b) || !exists()) return;
      final response = await client
          .post(
            _endpoint(b, '/${Uri.encodeComponent(id)}/cancel'),
            headers: {
              'Authorization': b.token!,
              'Content-Type': 'application/json',
            },
            body: jsonEncode({'confirmed': true}),
          )
          .timeout(const Duration(seconds: 12));
      if (!_current(serial, b)) return;
      if (response.statusCode != 200) {
        throw const FormatException('cancel rejected');
      }
      setState(() => _saving = false);
      await _load();
    } catch (_) {
      if (_current(serial, b)) {
        setState(() => _error = '取消结果未确认，请读取原意图检查；不要反复取消。');
      }
    } finally {
      if (_current(serial, b)) setState(() => _saving = false);
    }
  }

  Future<DateTime?> _chooseTime(DateTime initial) async {
    final b = _capture(), serial = _serial;
    final now = DateTime.now();
    final day = await showDatePicker(
      context: context,
      initialDate: initial.toLocal().isBefore(now) ? now : initial.toLocal(),
      firstDate: DateTime(now.year, now.month, now.day),
      lastDate: now.add(const Duration(days: 90)),
      builder: (context, child) {
        _dialogContext = context;
        return child!;
      },
    );
    _dialogContext = null;
    if (!mounted || day == null || !_current(serial, b)) return null;
    final time = await showTimePicker(
      context: context,
      initialTime: TimeOfDay.fromDateTime(initial.toLocal()),
      builder: (context, child) {
        _dialogContext = context;
        return child!;
      },
    );
    _dialogContext = null;
    if (time == null || !_current(serial, b)) return null;
    return DateTime(
      day.year,
      day.month,
      day.day,
      time.hour,
      time.minute,
    ).toUtc();
  }

  Future<void> _changeExpiry() async {
    final b = _capture(), serial = _serial;
    final value = await _chooseTime(_draft.expiresAt);
    if (value != null && _current(serial, b)) {
      setState(() => _draft.expiresAt = value);
    }
  }

  String _stamp(DateTime value) {
    final d = value.toLocal();
    String pad(int n) => n.toString().padLeft(2, '0');
    return '${d.year}年${pad(d.month)}月${pad(d.day)}日 ${pad(d.hour)}:${pad(d.minute)}';
  }

  String _modeLabel(String? mode) => switch (mode) {
    'ONLINE' => '线上',
    'HYBRID' => '线下＋线上',
    'IN_PERSON' => '线下',
    _ => '待选择',
  };
  String _audienceLabel(Object? audience) => switch (audience) {
    'PUBLIC' => '公开',
    'FRIENDS' => '好友',
    'COMMUNITY' => '社群成员',
    'LOCAL' => '当前城市用户',
    'INVITE_ONLY' => '受邀者',
    _ => '仅自己',
  };
  String _statusLabel(Map<String, dynamic> row) => switch (row['status']) {
    'ACTIVE' => '已激活 · ${_audienceLabel(row['audience'])}',
    'MATCHED' => '已匹配 · ${_audienceLabel(row['audience'])}',
    'CONVERTED' => '已转化',
    'EXPIRED' => '已过期',
    'CANCELLED' => '已取消',
    _ => '草稿 · 仅自己可见',
  };
  Widget _input(
    TextEditingController controller,
    String label,
    String key,
    ValueChanged<String> update, {
    int max = 160,
  }) => TextField(
    key: Key(key),
    controller: controller,
    maxLength: max,
    minLines: 1,
    maxLines: key == 'intent-draft-title'
        ? 4
        : key == 'intent-draft-area'
        ? 3
        : 1,
    enabled: !_saving && !_sourceInvalidated && !_draftLocked,
    decoration: InputDecoration(labelText: label),
    onChanged: (value) => setState(() {
      update(value);
      _error = null;
    }),
  );
  Widget _editor(BuildContext context) {
    final showFields = _organized || _expanded;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text('先记下想做的事', style: Theme.of(context).textTheme.titleLarge),
        const SizedBox(height: 8),
        const Text('草稿仅自己可见。现在保存不会公开，也不会自动邀请其他人。'),
        const Text('保存时会在本机安全保留原操作和内容，用于断线核实；结果明确后清除，不会自动重试。'),
        if (!_binding.personal)
          const Padding(
            padding: EdgeInsets.symmetric(vertical: 8),
            child: Text('当前输入尚未保存，只保留在此页面。保存到账号需要本人登录；不支持匿名本机持久保存。'),
          ),
        if (widget.agentTaskId != null) ...[
          const SizedBox(height: 8),
          const Text('这条草稿来自你的 Agent 找活动查询。请先修改并确认内容；搜索本身不会创建社交意图。'),
          if (_sourceInvalidated) const Text('登录状态已更新，请返回 Agent 重新选择本人的活动查询。'),
        ],
        const SizedBox(height: 16),
        _input(_title, '想做什么', 'intent-draft-title', (v) => _draft.title = v),
        TextButton.icon(
          onPressed: _saving || _sourceInvalidated || _draftLocked
              ? null
              : _organize,
          icon: const Icon(Icons.edit_note),
          label: const Text('整理草稿'),
        ),
        if (_organized) ...[
          Text('我的理解：${_draft.title.isEmpty ? '还没有写下想做的事' : _draft.title}'),
          Text('参与方式：${_modeLabel(_draft.modality)} · 仅自己可见'),
          if (_draft.area.isNotEmpty) Text('大致区域：${_draft.area}'),
          if (_draft.areaSource != null) Text(_draft.areaSource!),
          if (_draft.timeNote != null && _draft.startsAt == null)
            Text('原句提到“${_draft.timeNote}”；具体活动时间未确定，没有自动换算。'),
        ],
        DropdownButtonFormField<String>(
          key: ValueKey('intent-mode-$_serial-${_draft.modality}'),
          initialValue: _draft.modality,
          decoration: const InputDecoration(
            labelText: '参与方式',
            hintText: '请选择参与方式',
          ),
          items: const [
            DropdownMenuItem(value: 'IN_PERSON', child: Text('线下')),
            DropdownMenuItem(value: 'ONLINE', child: Text('线上')),
            DropdownMenuItem(value: 'HYBRID', child: Text('线下＋线上')),
          ],
          onChanged: _saving || _sourceInvalidated || _draftLocked
              ? null
              : (v) => setState(() {
                  _draft.modality = v;
                  _error = null;
                }),
        ),
        if (_expanded)
          _input(
            _category,
            '活动类别（可选）',
            'intent-draft-category',
            (v) => _draft.category = v,
            max: 80,
          ),
        if (showFields &&
            _draft.modality != null &&
            _draft.modality != 'ONLINE')
          _input(_area, '大致区域', 'intent-draft-area', (v) {
            _draft.area = v;
            _draft.areaSource = '你填写的大致区域，不代表定位';
          }),
        if (showFields && _draft.modality == 'HYBRID' ||
            _expanded && _draft.modality == 'ONLINE')
          _input(
            _platform,
            _draft.modality == 'HYBRID' ? '线上参与方式' : '线上平台（可选）',
            'intent-draft-platform',
            (v) => _draft.platform = v,
            max: 80,
          ),
        TextButton(
          onPressed: _saving || _sourceInvalidated || _draftLocked
              ? null
              : () => setState(() => _expanded = !_expanded),
          child: Text(_expanded ? '返回草稿摘要' : '完整编辑'),
        ),
        Text('寻找有效期：${_stamp(_draft.expiresAt)}'),
        const Text('默认寻找有效期为七天，可修改；这不是活动开始或结束时间。'),
        TextButton(
          onPressed: _saving || _sourceInvalidated || _draftLocked
              ? null
              : _changeExpiry,
          child: const Text('修改寻找有效期'),
        ),
        if (_expanded) ...[
          Text(
            _draft.startsAt == null
                ? '活动时间：未确定（可选）'
                : '活动开始：${_stamp(_draft.startsAt!)}',
          ),
          if (_draft.endsAt != null) Text('活动结束：${_stamp(_draft.endsAt!)}'),
          Wrap(
            children: [
              TextButton(
                onPressed: _saving || _sourceInvalidated || _draftLocked
                    ? null
                    : () async {
                        final b = _capture(), serial = _serial;
                        final v = await _chooseTime(
                          _draft.startsAt ?? DateTime.now(),
                        );
                        if (v != null && _current(serial, b)) {
                          setState(() => _draft.startsAt = v);
                        }
                      },
                child: const Text('选择活动开始时间'),
              ),
              TextButton(
                onPressed: _saving || _sourceInvalidated || _draftLocked
                    ? null
                    : () async {
                        final b = _capture(), serial = _serial;
                        final v = await _chooseTime(
                          _draft.endsAt ?? _draft.startsAt ?? DateTime.now(),
                        );
                        if (v != null && _current(serial, b)) {
                          setState(() => _draft.endsAt = v);
                        }
                      },
                child: const Text('选择活动结束时间'),
              ),
              TextButton(
                onPressed: _saving || _sourceInvalidated || _draftLocked
                    ? null
                    : () => setState(() {
                        _draft.startsAt = null;
                        _draft.endsAt = null;
                      }),
                child: const Text('清除活动时间'),
              ),
            ],
          ),
        ],
        const SizedBox(height: 12),
        FilledButton(
          onPressed:
              _saving ||
                  _loading ||
                  _pendingBlocked ||
                  _sourceInvalidated ||
                  _creationUncertain ||
                  _priorReceipt != null
              ? null
              : _save,
          child: Text(_saving ? '正在保存…' : '保存私人草稿'),
        ),
        if (_creationUncertain)
          const Text(
            '核实列表不等于证明原提交成功。原操作标识和私人内容仅安全保存在本机待核实记录；核实后清除。离开或重启不会自动重试，也不会按相同标题认定已保存。',
          ),
        if (_pending != null) ...[
          if (_pendingDifferentDraft)
            const Text('当前输入尚未保存。此前的原操作正在单独核实，不会替换或提交当前输入。'),
          const Text('待核实的原保存（仅自己可见）'),
          Text('原内容：${_pending!.draft['title']}'),
          Text('原参与方式：${_modeLabel(_pending!.draft['modality'] as String?)}'),
          for (final key in ['category', 'areaLabel', 'onlinePlatform'])
            if ((_pending!.draft['constraints'] as Map)[key] != null)
              Text(
                '${switch (key) {
                  'category' => '原类别',
                  'areaLabel' => '原区域',
                  _ => '原线上方式',
                }}：${(_pending!.draft['constraints'] as Map)[key]}',
              ),
          if ((_pending!.draft['constraints'] as Map)['startsAt'] != null)
            Text(
              '原活动时间：${_stamp(DateTime.parse((_pending!.draft['constraints'] as Map)['startsAt'] as String))} 至 ${_stamp(DateTime.parse((_pending!.draft['constraints'] as Map)['endsAt'] as String))}',
            ),
          Text(
            '原寻找有效期：${_stamp(DateTime.parse(_pending!.draft['expiresAt'] as String))}（不是活动时间）',
          ),
        ],
        if (_creationUncertain &&
            _pending != null &&
            _receipt == null &&
            _priorReceipt == null)
          OutlinedButton(
            onPressed: _saving || _loading || _pendingBlocked
                ? null
                : _retryOriginal,
            child: const Text('重试原保存（内容不变）'),
          ),
        if (_noEffectNotice != null) Text(_noEffectNotice!),
        if (_priorReceipt != null && widget.onManageIntent != null)
          OutlinedButton(
            onPressed: () {
              final b = _capture();
              if (b.same(_binding) &&
                  b.personal &&
                  b.owner == _priorReceipt!.ownerID) {
                widget.onManageIntent!(_priorReceipt!.id);
              }
            },
            child: const Text('查看此前保存的意图'),
          ),
      ],
    );
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(
      title: const Text('我的社交意图'),
      leading: widget.onClose == null
          ? null
          : IconButton(
              tooltip: '返回本地地图',
              onPressed: widget.onClose,
              icon: const Icon(Icons.arrow_back),
            ),
    ),
    body: SingleChildScrollView(
      padding: const EdgeInsets.all(20),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (widget.contextNote != null) ...[
            Text(widget.contextNote!),
            const SizedBox(height: 12),
          ],
          if (_receipt == null)
            _editor(context)
          else ...[
            Text(
              _receipt!.intent!['audience'] == 'PRIVATE' &&
                      _receipt!.intent!['status'] == 'DRAFT'
                  ? '私人草稿已保存'
                  : '原保存已核实',
              style: Theme.of(context).textTheme.titleLarge,
            ),
            Text('想做的事：${_receipt!.intent!['title']}'),
            Text(
              _receipt!.intent!['audience'] == 'PRIVATE' &&
                      _receipt!.intent!['status'] == 'DRAFT'
                  ? '已核实本人账号、私人可见范围及草稿状态。没有公开、邀请或自动执行。'
                  : '当前状态：${_statusLabel(_receipt!.intent!)}。核查不会重新创建或公开。',
            ),
            if (widget.onManageIntent != null)
              FilledButton(
                onPressed: () {
                  final b = _capture();
                  if (b.same(_binding) &&
                      b.personal &&
                      b.owner == _receipt!.ownerID) {
                    widget.onManageIntent!(_receipt!.id);
                  }
                },
                child: const Text('查看已保存的意图'),
              ),
            TextButton(
              onPressed: _pending != null
                  ? null
                  : () => setState(() {
                      _reset();
                      _error = null;
                    }),
              child: const Text('再记下一件事'),
            ),
          ],
          if (_error != null)
            Padding(
              padding: const EdgeInsets.symmetric(vertical: 8),
              child: Text(
                _error!,
                style: TextStyle(color: Theme.of(context).colorScheme.error),
              ),
            ),
          if (_verifiedPrevious != null) ...[
            const Text('此前的保存已核实；当前输入仍未保存。'),
            Text(
              '此前意图：${_verifiedPrevious!.intent!['title']} · ${_statusLabel(_verifiedPrevious!.intent!)}',
            ),
            if (widget.onManageIntent != null)
              TextButton(
                onPressed: () {
                  final b = _capture();
                  if (b.same(_binding) &&
                      b.personal &&
                      b.owner == _verifiedPrevious!.ownerID) {
                    widget.onManageIntent!(_verifiedPrevious!.id);
                  }
                },
                child: const Text('查看此前已核实的意图'),
              ),
          ],
          const SizedBox(height: 24),
          Text('我的意图记录', style: Theme.of(context).textTheme.titleMedium),
          if (_loading) const LinearProgressIndicator(),
          if (_listError != null) Text(_listError!),
          TextButton(
            onPressed: _loading || _saving || !_binding.personal ? null : _load,
            child: Text(_creationUncertain ? '核实我的意图记录' : '重新读取记录'),
          ),
          if (!_loading && _drafts.isEmpty && _listError == null)
            const Text('还没有草稿。'),
          for (final row in _drafts)
            ListTile(
              title: Text(row['title'] as String? ?? '未命名意图'),
              subtitle: Text(
                '${_modeLabel(row['modality'] as String?)} · ${_statusLabel(row)}',
              ),
              onTap:
                  widget.onManageIntent == null ||
                      !socialIntentIDValid(row['id'])
                  ? null
                  : () {
                      if (_capture().same(_binding) && _binding.personal) {
                        widget.onManageIntent!(row['id'] as String);
                      }
                    },
              trailing: ['DRAFT', 'ACTIVE', 'MATCHED'].contains(row['status'])
                  ? IconButton(
                      tooltip: '取消这条意图',
                      onPressed: _saving || _loading || _creationUncertain
                          ? null
                          : () => _cancel(row['id'] as String),
                      icon: const Icon(Icons.cancel_outlined),
                    )
                  : null,
            ),
        ],
      ),
    ),
  );
}
