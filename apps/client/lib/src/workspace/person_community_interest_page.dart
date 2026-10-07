import 'dart:async';
import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import '../auth/birdtie_auth_controller.dart';
import 'person_community_interest_api.dart';
import 'person_community_interest_controller.dart';

class PersonCommunityInterestPage extends StatefulWidget {
  const PersonCommunityInterestPage({
    super.key,
    required this.auth,
    this.client,
    this.apiBaseUrl,
    this.workspaceChanges,
    this.organizationWorkspaceID,
  });
  final BirdtieAuthController auth;
  final http.Client? client;
  final String? apiBaseUrl;
  final Listenable? workspaceChanges;
  final String? Function()? organizationWorkspaceID;
  @override
  State<PersonCommunityInterestPage> createState() => _InterestPageState();
}

class _InterestPageState extends State<PersonCommunityInterestPage> {
  PersonCommunityInterestController _create() =>
      PersonCommunityInterestController(
        api: PersonCommunityInterestAPI(
          client: widget.client,
          apiBaseUrl: widget.apiBaseUrl,
        ),
        identity: () => CommunityInterestIdentity(
          widget.auth.authorizationHeader,
          widget.auth.accountID,
          widget.organizationWorkspaceID?.call(),
        ),
      );
  late PersonCommunityInterestController _data = _create();
  Timer? _timer;
  DialogRoute<bool>? _dialog;
  @override
  void initState() {
    super.initState();
    widget.auth.addListener(_identity);
    widget.workspaceChanges?.addListener(_identity);
    _data.addListener(_changed);
    unawaited(_data.load());
    _timer = Timer.periodic(const Duration(seconds: 1), (_) {
      if (!mounted) return;
      _data.expire();
      if (_dialog != null && _data.review == null) _closeDialog();
    });
  }

  void _changed() {
    if (mounted) setState(() {});
  }

  void _closeDialog() {
    final route = _dialog;
    _dialog = null;
    if (route?.isActive ?? false) Navigator.of(context).removeRoute(route!);
  }

  void _identity() {
    final generation = _data.generation;
    _data.sync();
    if (generation != _data.generation) {
      _closeDialog();
      unawaited(_data.load());
    }
  }

  @override
  void didUpdateWidget(PersonCommunityInterestPage old) {
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
        old.organizationWorkspaceID != widget.organizationWorkspaceID) {
      _closeDialog();
      _data.removeListener(_changed);
      _data.dispose();
      _data = _create();
      _data.addListener(_changed);
      unawaited(_data.load());
    } else {
      _identity();
    }
  }

  String _state(String value) => switch (value) {
    'PUBLIC' => '公开兴趣',
    'PRIVATE' => '仅自己可见',
    _ => '没有声明',
  };
  String _time(DateTime value) {
    final v = value.toLocal();
    return '${v.year}年${v.month}月${v.day}日 ${v.hour}:${v.minute.toString().padLeft(2, '0')}:${v.second.toString().padLeft(2, '0')}（本地时间）';
  }

  Future<void> _prepare(String id, String operation) async {
    final data = _data;
    final p = await data.prepare(id, operation);
    if (!mounted || !identical(data, _data) || p == null) return;
    final generation = data.generation;
    if (!data.reviewCurrent(p, generation)) return;
    final route = DialogRoute<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('检查这次具体声明'),
        content: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text('当前个人账号：${widget.auth.displayName ?? 'Birdtie 成员'}'),
              const SizedBox(height: 12),
              Text(p.name),
              const SizedBox(height: 12),
              Text('${_state(p.originalState)} → ${_state(p.targetState)}'),
              const SizedBox(height: 12),
              Text(p.consequence),
              const SizedBox(height: 12),
              Text('预览有效至 ${_time(p.expiresAt)}'),
              const SizedBox(height: 12),
              const Text('这是我对该社群的兴趣自声明，不代表成员资格，也不会发送邀请或消息。'),
            ],
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () {
              if (identical(data, _data) && data.reviewCurrent(p, generation)) {
                Navigator.of(context).pop(true);
              } else {
                Navigator.of(context).pop(false);
              }
            },
            child: const Text('批准这个版本'),
          ),
        ],
      ),
    );
    _dialog = route;
    final approved = await Navigator.of(context).push(route);
    if (identical(_dialog, route)) _dialog = null;
    if (!mounted || !identical(data, _data)) return;
    if (approved == true) {
      await data.approve(p, generation);
    } else {
      data.cancel();
    }
  }

  Widget _action(String label, VoidCallback? action) => Padding(
    padding: const EdgeInsets.symmetric(vertical: 4),
    child: SizedBox(
      width: double.infinity,
      child: OutlinedButton(
        onPressed: action,
        child: Padding(
          padding: const EdgeInsets.symmetric(vertical: 10),
          child: Text(label, textAlign: TextAlign.center),
        ),
      ),
    ),
  );
  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: const Text('我的社群兴趣')),
    body: SafeArea(
      child: ListView(
        padding: const EdgeInsets.all(20),
        children: [
          const Text(
            '让共同兴趣被看见',
            style: TextStyle(fontSize: 24, fontWeight: FontWeight.w700),
          ),
          const SizedBox(height: 12),
          const Text('选择当前公开社群，再检查是否公开你的兴趣。默认仅自己可见；公开不代表加入社群或取得成员资格。'),
          const SizedBox(height: 12),
          if (!_data.current) const Text('请使用当前个人账号。组织工作区不能修改个人兴趣。'),
          if (_data.busy) const LinearProgressIndicator(),
          if (_data.message != null)
            Semantics(
              liveRegion: true,
              child: Padding(
                padding: const EdgeInsets.symmetric(vertical: 12),
                child: Text(_data.message!),
              ),
            ),
          _action(
            _data.unknown ? '只读核查当前声明' : '刷新当前声明',
            _data.current && !_data.busy ? _data.load : null,
          ),
          if (_data.own != null) ...[
            const SizedBox(height: 16),
            Text(
              _data.resultOnly ? '本次操作结果（刷新可查看全部声明）' : '已声明的兴趣',
              style: const TextStyle(fontWeight: FontWeight.w700),
            ),
            if (_data.own!.records.isEmpty)
              const Padding(
                padding: EdgeInsets.symmetric(vertical: 12),
                child: Text('还没有社群兴趣声明。'),
              ),
            for (final r in _data.own!.records.where(
              (r) => r.state != 'ABSENT',
            ))
              Padding(
                padding: const EdgeInsets.symmetric(vertical: 10),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      r.name,
                      style: const TextStyle(fontWeight: FontWeight.w600),
                    ),
                    Text(_state(r.state)),
                    if (!r.sourceAvailable)
                      const Text('社群当前不可公开展示；你仍可隐藏或撤回自己的声明。'),
                    if (r.state != 'PRIVATE')
                      _action(
                        '检查设为仅自己可见',
                        _data.canPrepare
                            ? () => _prepare(r.communityID, 'PRIVATE')
                            : null,
                      ),
                    if (r.sourceAvailable && r.state != 'PUBLIC')
                      _action(
                        '检查公开兴趣',
                        _data.canPrepare
                            ? () => _prepare(r.communityID, 'PUBLIC')
                            : null,
                      ),
                    _action(
                      '检查撤回声明',
                      _data.canPrepare
                          ? () => _prepare(r.communityID, 'DELETE')
                          : null,
                    ),
                  ],
                ),
              ),
            if (_data.own!.truncated) const Text('本次仅展示前100条声明，列表并不完整。'),
          ],
          if (_data.available != null) ...[
            const SizedBox(height: 16),
            const Text('当前公开社群', style: TextStyle(fontWeight: FontWeight.w700)),
            if (_data.available!.options.isEmpty)
              const Padding(
                padding: EdgeInsets.symmetric(vertical: 12),
                child: Text('暂时没有可选的公开社群。不会从私密资料生成选项。'),
              ),
            RadioGroup<String>(
              groupValue: _data.selectedID,
              onChanged: (value) {
                if (_data.canPrepare) _data.select(value);
              },
              child: Column(
                children: [
                  for (final option in _data.available!.options)
                    RadioListTile<String>(
                      value: option.id,
                      title: Text(option.name),
                      enabled: _data.canPrepare,
                    ),
                ],
              ),
            ),
            if (_data.available!.truncated)
              const Text('本次仅展示前100个公开社群，列表并不完整。'),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                for (final value in ['PRIVATE', 'PUBLIC'])
                  ChoiceChip(
                    label: Text(_state(value)),
                    selected: _data.visibility == value,
                    onSelected: _data.canPrepare
                        ? (_) => _data.chooseVisibility(value)
                        : null,
                  ),
              ],
            ),
            _action(
              '检查兴趣声明',
              _data.canPrepare && _data.selectedID != null
                  ? () => _prepare(_data.selectedID!, _data.visibility)
                  : null,
            ),
          ],
        ],
      ),
    ),
  );
  @override
  void dispose() {
    _timer?.cancel();
    widget.auth.removeListener(_identity);
    widget.workspaceChanges?.removeListener(_identity);
    _data.removeListener(_changed);
    _data.dispose();
    super.dispose();
  }
}
