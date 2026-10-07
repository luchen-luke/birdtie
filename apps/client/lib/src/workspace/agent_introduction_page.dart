import 'dart:async';
import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import '../auth/birdtie_auth_controller.dart';
import 'agent_introduction_api.dart';
import 'agent_introduction_controller.dart';

class AgentIntroductionPage extends StatefulWidget {
  const AgentIntroductionPage({
    super.key,
    required this.auth,
    this.client,
    this.apiBaseUrl,
    this.workspaceChanges,
    this.organizationWorkspaceID,
    this.initialSourceIntentID,
    this.onFindPeople,
    this.onOpenPerson,
    this.onManageFindPeople,
  });
  final BirdtieAuthController auth;
  final http.Client? client;
  final String? apiBaseUrl;
  final Listenable? workspaceChanges;
  final String? Function()? organizationWorkspaceID;
  final String? initialSourceIntentID;
  final ValueChanged<String>? onFindPeople, onOpenPerson;
  final VoidCallback? onManageFindPeople;
  @override
  State<AgentIntroductionPage> createState() => _AgentIntroductionPageState();
}

class _AgentIntroductionPageState extends State<AgentIntroductionPage> {
  AgentIntroductionController _create() => AgentIntroductionController(
    api: AgentIntroductionAPI(
      client: widget.client,
      apiBaseUrl: widget.apiBaseUrl,
    ),
    authorizationHeader: () => widget.auth.authorizationHeader,
    accountID: () => widget.auth.accountID,
    organizationWorkspaceID: () => widget.organizationWorkspaceID?.call(),
    initialSourceIntentID: widget.initialSourceIntentID,
  );
  late AgentIntroductionController _data = _create();
  Timer? _timer;
  Route<bool>? _dialog;
  @override
  void initState() {
    super.initState();
    widget.auth.addListener(_identity);
    widget.workspaceChanges?.addListener(_identity);
    _data.addListener(_changed);
    unawaited(_data.load());
    _timer = Timer.periodic(const Duration(seconds: 1), (_) {
      if (mounted) setState(_data.expire);
    });
  }

  void _changed() {
    if (mounted) setState(() {});
  }

  void _manage() {
    if (_data.identityCurrent && !_data.busy && !_data.unknownSave) {
      widget.onManageFindPeople?.call();
    }
  }

  void _removeDialog() {
    final r = _dialog;
    _dialog = null;
    if (r?.isActive ?? false) Navigator.of(context).removeRoute(r!);
  }

  void _identity() {
    final old = _data.generation;
    _data.synchronizeIdentity();
    if (old != _data.generation) {
      _removeDialog();
      if (_data.personal) {
        unawaited(_data.load());
      } else {
        _changed();
      }
    }
  }

  @override
  void didUpdateWidget(AgentIntroductionPage old) {
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
        old.initialSourceIntentID != widget.initialSourceIntentID) {
      _removeDialog();
      _data.removeListener(_changed);
      _data.dispose();
      _data = _create();
      _data.addListener(_changed);
      unawaited(_data.load());
    } else {
      _identity();
    }
  }

  @override
  void dispose() {
    _timer?.cancel();
    widget.auth.removeListener(_identity);
    widget.workspaceChanges?.removeListener(_identity);
    _data.removeListener(_changed);
    _data.dispose();
    super.dispose();
  }

  String _time(DateTime v) {
    final d = v.toLocal();
    return '${d.year}年${d.month}月${d.day}日 ${d.hour}:${d.minute.toString().padLeft(2, '0')}（本地时间）';
  }

  Widget _button(String text, VoidCallback? action) => Padding(
    padding: const EdgeInsets.symmetric(vertical: 6),
    child: SizedBox(
      width: double.infinity,
      child: OutlinedButton(
        onPressed: action,
        child: Padding(
          padding: const EdgeInsets.symmetric(vertical: 12),
          child: Text(text, textAlign: TextAlign.center),
        ),
      ),
    ),
  );
  Future<void> _editPolicy() async {
    _data.synchronizeIdentity();
    final data = _data, p = data.policy, g = data.generation;
    if (p == null || data.busy || data.unknownSave || _dialog != null) return;
    var unknown = p.rules['UNKNOWN_PERSON'] == 'REVIEW_REQUIRED',
        community = p.rules['SHARED_COMMUNITY'] == 'REVIEW_REQUIRED',
        activity = p.rules['SHARED_ACTIVITY'] == 'REVIEW_REQUIRED',
        days = 7;
    final editor = DialogRoute<bool>(
      context: context,
      builder: (c) => StatefulBuilder(
        builder: (c, set) => AlertDialog(
          title: const Text('编辑本人社交偏好'),
          content: SingleChildScrollView(
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                const Text('仅表示允许本人检查建议；不授权消息、模型或对方身份。其他四类偏好保持原值。'),
                CheckboxListTile(
                  contentPadding: EdgeInsets.zero,
                  title: const Text('尚未建立关系的人：允许本人审阅'),
                  value: unknown,
                  onChanged: (v) => set(() => unknown = v ?? false),
                ),
                CheckboxListTile(
                  contentPadding: EdgeInsets.zero,
                  title: const Text('共同社群声明：允许本人审阅'),
                  subtitle: const Text('仅使用双方明确公开的社群兴趣自声明；不代表成员资格。'),
                  value: community,
                  onChanged: (v) => set(() => community = v ?? false),
                ),
                CheckboxListTile(
                  contentPadding: EdgeInsets.zero,
                  title: const Text('共同公开报名：允许本人审阅'),
                  subtitle: const Text('须双方逐活动明确公开当前报名；不代表到场。此设置不会公开任何报名。'),
                  value: activity,
                  onChanged: (v) => set(() => activity = v ?? false),
                ),
                DropdownButtonFormField<int>(
                  initialValue: days,
                  isExpanded: true,
                  decoration: const InputDecoration(labelText: '本次偏好有效期'),
                  items: [
                    for (final n in [1, 7, 30])
                      DropdownMenuItem(value: n, child: Text('$n 天')),
                  ],
                  onChanged: (v) => set(() => days = v ?? 7),
                ),
              ],
            ),
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.of(c).pop(false),
              child: const Text('取消'),
            ),
            TextButton(
              onPressed: () => Navigator.of(c).pop(true),
              child: const Text('检查具体变更'),
            ),
          ],
        ),
      ),
    );
    _dialog = editor;
    final prepared = await Navigator.of(context).push<bool>(editor);
    if (identical(_dialog, editor)) _dialog = null;
    if (!mounted ||
        !identical(data, _data) ||
        data.generation != g ||
        prepared != true ||
        !identical(data.policy, p)) {
      return;
    }
    data.preparePolicy(
      unknownPerson: unknown,
      community: community,
      sharedActivity: activity,
      days: days,
    );
    final review = data.review;
    if (review == null) return;
    final confirm = DialogRoute<bool>(
      context: context,
      builder: (c) => AlertDialog(
        title: const Text('确认此版本的本人偏好'),
        content: SingleChildScrollView(
          child: Text(
            '本人：${widget.auth.displayName}\n原策略版本：${p.revision}\n${[for (final k in introductionCategories) '${introductionCategoryLabels[k]}：${review.rules[k] == 'REVIEW_REQUIRED' ? '需本人审阅' : '关闭'}'].join('\n')}\n有效至：${_time(review.expiry)}\n\n保存会替换此类偏好及期限。不会发送申请、消息、引荐或模型请求；只有当前服务提供的共同公开报名来源才可使用；不代表到场。',
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(c).pop(false),
            child: const Text('返回'),
          ),
          TextButton(
            onPressed: () => Navigator.of(c).pop(true),
            child: const Text('保存本人偏好'),
          ),
        ],
      ),
    );
    _dialog = confirm;
    final accepted = await Navigator.of(context).push<bool>(confirm);
    if (identical(_dialog, confirm)) _dialog = null;
    if (!mounted || !identical(data, _data) || data.generation != g) return;
    if (accepted == true) {
      await data.savePolicy(review);
    } else {
      data.cancelPolicy();
    }
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(
      title: const Text('看看共同意图'),
      actions: [
        IconButton(
          tooltip: '刷新当前意图与设置',
          onPressed: _data.busy ? null : _data.load,
          icon: const Icon(Icons.refresh),
        ),
      ],
    ),
    body: !_data.personal
        ? const Padding(
            padding: EdgeInsets.all(20),
            child: Text('请使用已登录的个人身份查看；组织工作区不能沿用本人结果。'),
          )
        : ListView(
            padding: const EdgeInsets.all(20),
            children: [
              const Text(
                '从当前公开的找搭子意图，看看双方明确声明的共同依据。',
                style: TextStyle(fontSize: 20, fontWeight: FontWeight.w600),
              ),
              const SizedBox(height: 12),
              const Text('这不是现实关系、位置或长期兴趣判断。不会读取私密偏好、发送申请或调用模型。'),
              if (_data.busy)
                const Padding(
                  padding: EdgeInsets.symmetric(vertical: 16),
                  child: LinearProgressIndicator(),
                ),
              if (_data.message != null)
                Padding(
                  padding: const EdgeInsets.symmetric(vertical: 12),
                  child: Text(
                    _data.message!,
                    key: const Key('introduction_message'),
                  ),
                ),
              if (_data.consent == false) ...[
                const Text('找新朋友设置尚未开启。请在原流程中自主检查和选择，不会自动开启。'),
                _button(
                  '前往找新朋友',
                  widget.onManageFindPeople == null ||
                          _data.busy ||
                          _data.unknownSave
                      ? null
                      : _manage,
                ),
              ],
              if (_data.available.isEmpty && !_data.busy)
                const Padding(
                  padding: EdgeInsets.symmetric(vertical: 12),
                  child: Text('还没有当前公开且有效的找搭子意图。请先在找新朋友中创建并确认公开。'),
                ),
              if (_data.available.isEmpty && _data.consent != false)
                _button(
                  '前往管理找新朋友',
                  widget.onManageFindPeople == null ||
                          _data.busy ||
                          _data.unknownSave
                      ? null
                      : _manage,
                ),
              DropdownButtonFormField<String>(
                key: ValueKey((_data.generation, _data.selectedID)),
                initialValue: _data.selected?.id,
                isExpanded: true,
                decoration: const InputDecoration(labelText: '选择本人当前公开意图'),
                items: [
                  for (final i in _data.available)
                    DropdownMenuItem(
                      value: i.id,
                      child: Text(
                        i.title,
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                      ),
                    ),
                ],
                onChanged: _data.busy || _data.unknownSave
                    ? null
                    : (id) {
                        if (id != null) _data.choose(id);
                      },
              ),
              if (_data.selected != null)
                Padding(
                  padding: const EdgeInsets.symmetric(vertical: 12),
                  child: Text(
                    '所选意图：${_data.selected!.title}\n有效至：${_time(_data.selected!.expiry)}',
                  ),
                ),
              if (_data.policy != null) ...[
                Text(
                  _data.policy!.reviewEnabled &&
                          _data.policy!.expiry!.isAfter(_data.now)
                      ? '本人当前偏好：允许审阅尚未建立关系的公开意图建议'
                      : '本人当前偏好：关闭或已过期。需要时可自行检查并修改。',
                ),
                if (_data.policy!.expiry != null)
                  Text('策略原期限：${_time(_data.policy!.expiry!)}'),
              ],
              _button(
                '检查本人社交偏好',
                _data.policy == null || _data.busy || _data.unknownSave
                    ? null
                    : _editPolicy,
              ),
              _button('查看共同依据', _data.canSearch ? _data.search : null),
              Padding(
                padding: const EdgeInsets.symmetric(vertical: 12),
                child: Text(
                  _data.result?.activityAvailable == true
                      ? '共同公开报名仅使用双方逐活动明确公开、仍有效的报名；不代表到场或成员资格。'
                      : '尚未核验当前服务的共同公开报名来源。不会将私密报名、动态关联或成员地址作为公开依据。',
                ),
              ),
              if (_data.result != null) ...[
                Text(_data.result!.explanation),
                Text('本次核实：${_time(_data.result!.observedAt)}'),
                if (_data.result!.candidates.isEmpty)
                  const Padding(
                    padding: EdgeInsets.symmetric(vertical: 12),
                    child: Text('当前没有可展示的共同公开声明。可以修改原意图或稍后刷新。'),
                  ),
                if (_data.result!.truncated)
                  const Text('本次只展示有界范围内的结果，不代表全部候选。'),
                for (final c in _data.result!.candidates)
                  Padding(
                    padding: const EdgeInsets.symmetric(vertical: 12),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          c.name,
                          style: Theme.of(context).textTheme.titleMedium,
                        ),
                        for (final text in c.basis.values) Text(text),
                        Text('有效至：${_time(c.expiry)}'),
                        _button(
                          '查看此人资料',
                          widget.onOpenPerson != null &&
                                  _data.candidateCurrent(c)
                              ? () {
                                  if (_data.candidateCurrent(c)) {
                                    widget.onOpenPerson!(c.accountID);
                                  }
                                }
                              : null,
                        ),
                      ],
                    ),
                  ),
              ],
              _button(
                '回找新朋友检查申请',
                widget.onFindPeople != null && _data.currentSourceID != null
                    ? () {
                        final id = _data.currentSourceID;
                        if (id != null) widget.onFindPeople!(id);
                      }
                    : null,
              ),
              const Text('申请要在原流程中重新核实并由你单独确认，对方接受后才建立关系。此页不会直接打开陌生人的聊天。'),
            ],
          ),
  );
}
