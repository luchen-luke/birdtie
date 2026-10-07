import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';
import 'package:http/http.dart' as http;
import '../auth/birdtie_auth_controller.dart';
import 'active_social_intent_api.dart';
import 'active_social_intent_controller.dart';
import 'intent_activity_conversion_page.dart';

String activeIntentTime(DateTime v) {
  final t = v.toLocal();
  return '${t.year}年${t.month}月${t.day}日 ${t.hour.toString().padLeft(2, '0')}:${t.minute.toString().padLeft(2, '0')}（本地时间）';
}

class ActiveSocialIntentPage extends StatefulWidget {
  const ActiveSocialIntentPage({
    super.key,
    required this.auth,
    this.client,
    this.apiBaseUrl,
    this.workspaceChanges,
    this.organizationWorkspaceID,
    this.initialIntentID,
  });
  final BirdtieAuthController auth;
  final http.Client? client;
  final String? apiBaseUrl;
  final Listenable? workspaceChanges;
  final String? Function()? organizationWorkspaceID;
  final String? initialIntentID;
  @override
  State<ActiveSocialIntentPage> createState() => _ActiveIntentPageState();
}

class _ActiveIntentPageState extends State<ActiveSocialIntentPage> {
  ActiveSocialIntentController _create() => ActiveSocialIntentController(
    api: ActiveSocialIntentAPI(
      client: widget.client,
      apiBaseUrl: widget.apiBaseUrl,
    ),
    identity: () => ActiveIntentIdentity(
      widget.auth.authorizationHeader,
      widget.auth.accountID,
      widget.organizationWorkspaceID?.call(),
    ),
  );
  late ActiveSocialIntentController _data = _create();
  final List<Route<dynamic>> _routes = [];
  Timer? _timer;
  DateTime? _dialogDeadline;
  NavigatorState? _navigator;
  @override
  void initState() {
    super.initState();
    widget.auth.addListener(_identity);
    widget.workspaceChanges?.addListener(_identity);
    _data.addListener(_changed);
    unawaited(_load(_data));
    _timer = Timer.periodic(const Duration(seconds: 1), (_) {
      if (!mounted) return;
      _data.expire();
      if (!_data.current || _dialogDeadline?.isAfter(DateTime.now()) == false) {
        _closeRoutes();
      }
      _changed();
    });
  }

  Future<void> _load(ActiveSocialIntentController data) async {
    await data.load();
    if (mounted &&
        identical(data, _data) &&
        data.current &&
        widget.initialIntentID != null) {
      data.select(widget.initialIntentID);
    }
  }

  void _changed() {
    if (!mounted) return;
    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) setState(() {});
      });
    } else {
      setState(() {});
    }
  }

  void _closeRoutes({bool deferred = false}) {
    final owned = List<Route<dynamic>>.of(_routes);
    _routes.clear();
    _dialogDeadline = null;
    final nav = _navigator;
    void remove() {
      if (nav?.mounted != true) return;
      for (final r in owned.reversed) {
        if (r.isActive) nav!.removeRoute(r);
      }
    }

    if (deferred ||
        SchedulerBinding.instance.schedulerPhase ==
            SchedulerPhase.persistentCallbacks) {
      WidgetsBinding.instance.addPostFrameCallback((_) => remove());
    } else {
      remove();
    }
  }

  void _identity() {
    final generation = _data.generation;
    _data.sync();
    if (generation != _data.generation) _closeRoutes();
  }

  @override
  void didUpdateWidget(ActiveSocialIntentPage old) {
    super.didUpdateWidget(old);
    if (old.auth != widget.auth) {
      old.auth.removeListener(_identity);
      widget.auth.addListener(_identity);
    }
    if (old.workspaceChanges != widget.workspaceChanges) {
      old.workspaceChanges?.removeListener(_identity);
      widget.workspaceChanges?.addListener(_identity);
    }
    if (old.auth != widget.auth ||
        old.client != widget.client ||
        old.apiBaseUrl != widget.apiBaseUrl ||
        old.workspaceChanges != widget.workspaceChanges ||
        old.organizationWorkspaceID != widget.organizationWorkspaceID ||
        old.initialIntentID != widget.initialIntentID) {
      _data.removeListener(_changed);
      _data.dispose();
      _closeRoutes();
      _data = _create();
      _data.addListener(_changed);
      final created = _data;
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted && identical(created, _data)) unawaited(_load(created));
      });
    } else {
      _identity();
    }
  }

  Future<void> _conversion() async {
    final c = _data, item = c.selected;
    if (!c.ready ||
        item == null ||
        !item.sourceAvailable ||
        item.intent['type'] != 'FIND_ACTIVITY' ||
        !['ACTIVE', 'MATCHED', 'CONVERTED'].contains(item.status)) {
      return;
    }
    final route = MaterialPageRoute<void>(
      builder: (_) => IntentActivityConversionPage(
        auth: widget.auth,
        intentID: item.id,
        client: widget.client,
        apiBaseUrl: widget.apiBaseUrl,
        workspaceChanges: widget.workspaceChanges,
        organizationWorkspaceID: widget.organizationWorkspaceID,
      ),
    );
    final nav = Navigator.of(context);
    _navigator = nav;
    _routes.add(route);
    await nav.push(route);
    _routes.remove(route);
    if (mounted && identical(c, _data) && c.current) await _load(c);
  }

  Future<T?> _dialog<T>(WidgetBuilder builder) {
    final nav = Navigator.of(context);
    _navigator = nav;
    final route = DialogRoute<T>(context: context, builder: builder);
    _routes.add(route);
    return nav.push(route).whenComplete(() => _routes.remove(route));
  }

  Future<void> _prepare(String op, {Map<String, dynamic>? edit}) async {
    final data = _data;
    final p = await data.prepare(op, edit: edit);
    if (!mounted || !identical(data, _data) || p == null) return;
    final epoch = data.generation;
    if (!data.reviewCurrent(p, epoch)) return;
    _dialogDeadline = p.expiresAt;
    final ok = await _dialog<bool>(
      (context) => AlertDialog(
        title: const Text('检查这次具体意图'),
        content: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text('本人账号：${widget.auth.displayName ?? 'Birdtie 成员'}'),
              const SizedBox(height: 12),
              Text(p.after.title),
              Text('类型：${intentTypes[p.after.intent['type']]}'),
              Text('方式：${intentModes[p.after.intent['modality']]}'),
              Text('受众：${p.after.audienceLabel}'),
              Text('地点／线上方式：${p.after.locationLabel}'),
              if (p.after.constraints['onlinePlatform'] != null &&
                  p.after.intent['modality'] == 'HYBRID')
                Text('线上方式：${p.after.constraints['onlinePlatform']}'),
              if (p.after.constraints['category'] != null)
                Text('类别：${p.after.constraints['category']}'),
              if (p.after.constraints['minParticipants'] != null ||
                  p.after.constraints['maxParticipants'] != null)
                Text(
                  '人数：${p.after.constraints['minParticipants'] ?? '未设下限'}—${p.after.constraints['maxParticipants'] ?? '未设上限'}',
                ),
              Text(
                p.after.startsAt == null
                    ? '计划活动时间：未设定'
                    : '活动开始：${activeIntentTime(p.after.startsAt!)}',
              ),
              if (p.after.endsAt != null)
                Text('活动结束：${activeIntentTime(p.after.endsAt!)}'),
              Text('寻找截止：${activeIntentTime(p.after.expiresAt)}'),
              Text('操作后：${intentStates[p.after.status]}'),
              const SizedBox(height: 12),
              Text(p.explanation),
              Text('本次批准预览截止：${activeIntentTime(p.expiresAt)}'),
            ],
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('批准此具体版本'),
          ),
        ],
      ),
    );
    _dialogDeadline = null;
    if (!mounted || !identical(data, _data)) return;
    if (ok == true && data.reviewCurrent(p, epoch)) {
      await data.approve(p, epoch);
    } else {
      data.discardReview();
    }
  }

  Future<void> _edit() async {
    final data = _data, item = data.selected, options = data.options;
    if (!data.ready || item == null || options == null) return;
    final epoch = data.generation;
    _dialogDeadline = item.expiresAt;
    final result = await _dialog<Map<String, dynamic>>(
      (context) => _IntentEditDialog(item: item, options: options),
    );
    _dialogDeadline = null;
    if (!mounted ||
        !identical(data, _data) ||
        !data.ready ||
        data.generation != epoch ||
        result == null) {
      return;
    }
    await _prepare('EDIT', edit: result);
  }

  @override
  void dispose() {
    widget.auth.removeListener(_identity);
    widget.workspaceChanges?.removeListener(_identity);
    _timer?.cancel();
    _data.removeListener(_changed);
    _data.dispose();
    _closeRoutes(deferred: true);
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final item = _data.selected;
    return Scaffold(
      appBar: AppBar(
        title: const Text('我的社交意图'),
        actions: [
          IconButton(
            tooltip: '刷新当前意图',
            onPressed: _data.current && !_data.busy ? () => _data.load() : null,
            icon: const Icon(Icons.refresh),
          ),
        ],
      ),
      body: SafeArea(
        child: ListView(
          padding: const EdgeInsets.all(16),
          children: [
            const Text('先检查你的计划，再决定让谁看到。编辑不会自动重新公开。'),
            const SizedBox(height: 16),
            if (!_data.current)
              const Text('请在当前个人账号打开；组织工作区不能代本人批准。')
            else ...[
              if (_data.busy) const LinearProgressIndicator(),
              if (_data.message != null)
                Padding(
                  padding: const EdgeInsets.symmetric(vertical: 12),
                  child: Text(_data.message!),
                ),
              if (_data.unknown)
                FilledButton(
                  onPressed: !_data.busy ? () => _data.reconcile() : null,
                  child: const Text('核对原意图当前状态'),
                ),
              if (_data.list?.truncated == true)
                const Text('仅显示最近 100 个意图；列表未覆盖全部历史。'),
              if (_data.list != null && _data.list!.items.isEmpty)
                const Text('暂时没有已有意图。可从 Now 表达需求，检查草稿后再开启。'),
              if (_data.list != null)
                DropdownButtonFormField<String>(
                  initialValue:
                      _data.list!.items.any((i) => i.id == _data.selectedID)
                      ? _data.selectedID
                      : null,
                  isExpanded: true,
                  decoration: const InputDecoration(labelText: '选择已有意图'),
                  items: _data.list!.items
                      .map(
                        (i) => DropdownMenuItem(
                          value: i.id,
                          child: Text(
                            '${i.title} · ${intentStates[i.status]}',
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                          ),
                        ),
                      )
                      .toList(),
                  onChanged: _data.ready ? _data.select : null,
                ),
              if (item != null) ...[
                const SizedBox(height: 16),
                Text(item.title, style: Theme.of(context).textTheme.titleLarge),
                Text(
                  '当前：${item.expiresAt.isAfter(DateTime.now()) ? intentStates[item.status] : '已到期'} · ${intentModes[item.intent['modality']]}',
                ),
                Text('受众：${item.audienceLabel}'),
                Text('地点／线上方式：${item.locationLabel}'),
                if (!item.sourceAvailable)
                  const Text('当前来源暂不可用；可检查取消，不会展示隐藏地点资料。'),
                Text(
                  item.startsAt == null
                      ? '计划活动时间未设定（寻找截止不等于活动时间）'
                      : '活动：${activeIntentTime(item.startsAt!)} 至 ${activeIntentTime(item.endsAt!)}',
                ),
                Text('寻找截止：${activeIntentTime(item.expiresAt)}'),
                const SizedBox(height: 16),
                Wrap(
                  spacing: 8,
                  runSpacing: 8,
                  children: [
                    OutlinedButton(
                      onPressed:
                          _data.ready &&
                              item.editable(DateTime.now()) &&
                              _data.options != null
                          ? _edit
                          : null,
                      child: const Text('编辑并检查'),
                    ),
                    if (item.status == 'DRAFT')
                      FilledButton(
                        onPressed:
                            _data.ready &&
                                item.editable(DateTime.now()) &&
                                item.sourceAvailable
                            ? () => _prepare('ACTIVATE')
                            : null,
                        child: const Text('检查并开启'),
                      ),
                    if (item.intent['type'] == 'FIND_ACTIVITY' &&
                        [
                          'ACTIVE',
                          'MATCHED',
                          'CONVERTED',
                        ].contains(item.status))
                      OutlinedButton(
                        onPressed: _data.ready && item.sourceAvailable
                            ? _conversion
                            : null,
                        child: Text(
                          item.status == 'CONVERTED'
                              ? '查看原活动关联'
                              : '从已报名活动完成这条意图',
                        ),
                      ),
                    OutlinedButton(
                      onPressed: _data.ready && item.editable(DateTime.now())
                          ? () => _prepare('CANCEL')
                          : null,
                      child: const Text('检查取消'),
                    ),
                  ],
                ),
              ],
              const SizedBox(height: 16),
              TextButton(
                onPressed: _data.ready ? _data.reset : null,
                child: const Text('重置本地选择（不取消意图）'),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

class _IntentEditDialog extends StatefulWidget {
  const _IntentEditDialog({required this.item, required this.options});
  final ActiveSocialIntent item;
  final ActiveIntentOptions options;
  @override
  State<_IntentEditDialog> createState() => _IntentEditDialogState();
}

class _IntentEditDialogState extends State<_IntentEditDialog> {
  late Map<String, dynamic> draft = widget.item.draft;
  late final title = TextEditingController(text: widget.item.title),
      area = TextEditingController(
        text: widget.item.constraints['areaLabel'] as String? ?? '',
      ),
      platform = TextEditingController(
        text: widget.item.constraints['onlinePlatform'] as String? ?? '',
      ),
      category = TextEditingController(
        text: widget.item.constraints['category'] as String? ?? '',
      ),
      minimum = TextEditingController(
        text: widget.item.constraints['minParticipants']?.toString() ?? '',
      ),
      maximum = TextEditingController(
        text: widget.item.constraints['maxParticipants']?.toString() ?? '',
      );
  DateTime? start, end;
  late DateTime expiry = widget.item.expiresAt;
  String? place, city, community, error;
  late Set<String> invitees = Set<String>.from(
    widget.item.intent['inviteeAccountIds'] as List? ?? [],
  );
  @override
  void initState() {
    super.initState();
    start = widget.item.startsAt;
    end = widget.item.endsAt;
    place = widget.item.constraints['placeId'] as String?;
    city = widget.item.intent['cityId'] as String?;
    community = widget.item.intent['communityId'] as String?;
  }

  @override
  void dispose() {
    for (final c in [title, area, platform, category, minimum, maximum]) {
      c.dispose();
    }
    super.dispose();
  }

  // Date/time selection stays in this owned dialog; identity retirement removes it.
  Widget _time(String label, DateTime? value, ValueChanged<DateTime?> set) =>
      Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text('$label：${value == null ? '未设定' : activeIntentTime(value)}'),
          if (value != null)
            CalendarDatePicker(
              initialDate: value.toLocal(),
              firstDate: DateTime(2000),
              lastDate: DateTime(2200, 12, 31),
              onDateChanged: (v) => set(
                DateTime(
                  v.year,
                  v.month,
                  v.day,
                  value.toLocal().hour,
                  value.toLocal().minute,
                ),
              ),
            ),
          Wrap(
            spacing: 8,
            children: [
              if (value == null)
                TextButton(
                  onPressed: () =>
                      set(DateTime.now().add(const Duration(days: 1))),
                  child: Text('选择$label'),
                )
              else ...[
                DropdownButton<int>(
                  value: value.toLocal().hour,
                  items: List.generate(
                    24,
                    (i) => DropdownMenuItem(value: i, child: Text('$i 时')),
                  ),
                  onChanged: (h) {
                    if (h != null) {
                      set(
                        DateTime(
                          value.toLocal().year,
                          value.toLocal().month,
                          value.toLocal().day,
                          h,
                          value.toLocal().minute,
                        ),
                      );
                    }
                  },
                ),
                DropdownButton<int>(
                  value: value.toLocal().minute,
                  items: List.generate(
                    60,
                    (i) => DropdownMenuItem(value: i, child: Text('$i 分')),
                  ),
                  onChanged: (m) {
                    if (m != null) {
                      set(
                        DateTime(
                          value.toLocal().year,
                          value.toLocal().month,
                          value.toLocal().day,
                          value.toLocal().hour,
                          m,
                        ),
                      );
                    }
                  },
                ),
                if (label != '寻找截止')
                  TextButton(
                    onPressed: () => set(null),
                    child: const Text('清除'),
                  ),
              ],
            ],
          ),
        ],
      );
  Widget _pick(
    String label,
    String? value,
    Map<String, String> choices,
    ValueChanged<String?> change, {
    bool optional = false,
  }) => DropdownButtonFormField<String>(
    initialValue: choices.containsKey(value) ? value : null,
    isExpanded: true,
    decoration: InputDecoration(labelText: label),
    items: [
      if (optional) const DropdownMenuItem(value: '', child: Text('不选具体地点')),
      ...choices.entries.map(
        (v) => DropdownMenuItem(
          value: v.key,
          child: Text(v.value, maxLines: 2, overflow: TextOverflow.ellipsis),
        ),
      ),
    ],
    onChanged: change,
  );
  void _submit() {
    try {
      final mode = draft['modality'], aud = draft['audience'];
      final constraints = <String, dynamic>{
        if (category.text.trim().isNotEmpty) 'category': category.text.trim(),
        if (minimum.text.trim().isNotEmpty)
          'minParticipants': int.parse(minimum.text),
        if (maximum.text.trim().isNotEmpty)
          'maxParticipants': int.parse(maximum.text),
        if (mode != 'ONLINE' && place != null && place != '') 'placeId': place,
        if (mode != 'ONLINE' &&
            (place == null || place == '') &&
            area.text.trim().isNotEmpty)
          'areaLabel': area.text.trim(),
        if (mode != 'IN_PERSON' && platform.text.trim().isNotEmpty)
          'onlinePlatform': platform.text.trim(),
        if (start != null) 'startsAt': start!.toUtc().toIso8601String(),
        if (end != null) 'endsAt': end!.toUtc().toIso8601String(),
      };
      final next = <String, dynamic>{
        'title': title.text.trim(),
        'type': draft['type'],
        'modality': mode,
        'audience': aud,
        'constraints': constraints,
        'expiresAt': expiry.toUtc().toIso8601String(),
        if (draft['contextId'] != null &&
            mode == widget.item.intent['modality'])
          'contextId': draft['contextId'],
        if (aud == 'LOCAL') 'cityId': city,
        if (aud == 'COMMUNITY') 'communityId': community,
        if (aud == 'INVITE_ONLY')
          'inviteeAccountIds': (invitees.toList()..sort()),
      };
      ActiveSocialIntent({
        ...{
          'intent': {
            ...next,
            'id': widget.item.id,
            'creatorAccountId': widget.item.intent['creatorAccountId'],
            'status': 'DRAFT',
            'createdAt': widget.item.createdAt.toIso8601String(),
            'updatedAt': widget.item.updatedAt.toIso8601String(),
          },
          'version': widget.item.version,
          'sourceAvailable': true,
          'locationLabel': '待后端核对',
          'audienceLabel': '待后端核对',
        },
      }, widget.item.intent['creatorAccountId']);
      if (!expiry.isAfter(DateTime.now().add(const Duration(minutes: 1))) ||
          expiry.isAfter(DateTime.now().add(const Duration(days: 90)))) {
        throw const FormatException('截止须在 1 分钟后且不超过 90 天');
      }
      Navigator.pop(context, next);
    } catch (_) {
      setState(() => error = '请补齐所选范围、地点、人数或具体时间；起止时间需成对且结束晚于开始。');
    }
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: const Text('编辑意图草稿'),
    content: SingleChildScrollView(
      child: SizedBox(
        width: 520,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text('保存编辑会退回草稿；原有效意图会停止发现，需再次检查并开启。'),
            if (error != null)
              Text(
                error!,
                style: TextStyle(color: Theme.of(context).colorScheme.error),
              ),
            TextField(
              controller: title,
              maxLength: 160,
              decoration: const InputDecoration(labelText: '你想做什么'),
            ),
            _pick(
              '意图类型',
              draft['type'],
              intentTypes,
              (v) => setState(() => draft['type'] = v),
            ),
            _pick(
              '参与方式',
              draft['modality'],
              intentModes,
              (v) => setState(() => draft['modality'] = v),
            ),
            _pick(
              '谁可以看到',
              draft['audience'],
              intentAudiences,
              (v) => setState(() => draft['audience'] = v),
            ),
            if (draft['audience'] == 'LOCAL')
              _pick(
                '当前公开城市',
                city,
                widget.options.choices['cities']!,
                (v) => setState(() => city = v),
              ),
            if (draft['audience'] == 'COMMUNITY')
              _pick(
                '本人所在社群',
                community,
                widget.options.choices['communities']!,
                (v) => setState(() => community = v),
              ),
            if (draft['audience'] == 'INVITE_ONLY') ...[
              const Text('从当前好友选择邀请对象（最多 20 人；开启不自动发送邀请）'),
              ...widget.options.choices['invitees']!.entries.map(
                (v) => CheckboxListTile(
                  contentPadding: EdgeInsets.zero,
                  title: Text(v.value),
                  value: invitees.contains(v.key),
                  onChanged: (checked) => setState(() {
                    if (checked == true) {
                      if (invitees.length < 20) invitees.add(v.key);
                    } else {
                      invitees.remove(v.key);
                    }
                  }),
                ),
              ),
            ],
            if (draft['modality'] != 'ONLINE') ...[
              _pick(
                '公开地点（可选）',
                place,
                widget.options.choices['places']!,
                (v) => setState(() => place = v),
                optional: true,
              ),
              if (place == null || place == '')
                TextField(
                  controller: area,
                  maxLength: 160,
                  decoration: const InputDecoration(labelText: '粗略区域（不填私人地址）'),
                ),
            ],
            if (draft['modality'] != 'IN_PERSON')
              TextField(
                controller: platform,
                maxLength: 80,
                decoration: const InputDecoration(labelText: '线上平台／参与方式'),
              ),
            TextField(
              controller: category,
              maxLength: 80,
              decoration: const InputDecoration(labelText: '类别（可选）'),
            ),
            TextField(
              controller: minimum,
              keyboardType: TextInputType.number,
              decoration: const InputDecoration(labelText: '最少人数（可选）'),
            ),
            TextField(
              controller: maximum,
              keyboardType: TextInputType.number,
              decoration: const InputDecoration(labelText: '最多人数（可选）'),
            ),
            _time('活动开始', start, (v) => setState(() => start = v)),
            _time('活动结束', end, (v) => setState(() => end = v)),
            const Text('寻找截止仅决定意图多久有效，不代表活动时间。'),
            _time('寻找截止', expiry, (v) {
              if (v != null) setState(() => expiry = v);
            }),
            if (widget.options.truncated)
              const Text('每类仅显示前 100 项可用范围。缺少的项目不能凭空填写。'),
          ],
        ),
      ),
    ),
    actions: [
      TextButton(
        onPressed: () => Navigator.pop(context),
        child: const Text('取消编辑'),
      ),
      FilledButton(onPressed: _submit, child: const Text('检查具体修改')),
    ],
  );
}
