import 'dart:async';
import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import '../auth/birdtie_auth_controller.dart';
import 'activity_participation_disclosure_api.dart';
import 'activity_participation_disclosure_controller.dart';

class ActivityParticipationDisclosurePage extends StatefulWidget {
  const ActivityParticipationDisclosurePage({
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
  State<ActivityParticipationDisclosurePage> createState() =>
      _DisclosurePageState();
}

class _DisclosurePageState extends State<ActivityParticipationDisclosurePage> {
  ActivityParticipationDisclosureController _create() =>
      ActivityParticipationDisclosureController(
        api: ActivityParticipationDisclosureAPI(
          client: widget.client,
          apiBaseUrl: widget.apiBaseUrl,
        ),
        identity: () => ParticipationDisclosureIdentity(
          widget.auth.authorizationHeader,
          widget.auth.accountID,
          widget.organizationWorkspaceID?.call(),
        ),
      );
  late ActivityParticipationDisclosureController _data = _create();
  Timer? _timer;
  DialogRoute<bool>? _dialog;
  DialogRoute<Duration>? _expiryDialog;
  DateTime? _expiryDeadline;
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
      if (_expiryDialog != null &&
          (!_data.current ||
              _expiryDeadline?.isAfter(DateTime.now()) != true)) {
        _closeDialog();
      }
      if (_dialog != null && _data.review == null) _closeDialog();
      if (_data.own != null) {
        setState(() {});
      }
    });
  }

  void _changed() {
    if (mounted) setState(() {});
  }

  void _closeDialog() {
    final expiry = _expiryDialog;
    _expiryDialog = null;
    _expiryDeadline = null;
    if (expiry?.isActive ?? false) Navigator.of(context).removeRoute(expiry!);
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
  void didUpdateWidget(ActivityParticipationDisclosurePage old) {
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
      _data.removeListener(_changed);
      _data.dispose();
      _data = _create();
      _data.addListener(_changed);
      final created = _data;
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (!mounted || !identical(created, _data)) return;
        _closeDialog();
        unawaited(created.load());
      });
    } else {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) _identity();
      });
    }
  }

  String _state(String value) => switch (value) {
    'PUBLIC' => '公开报名',
    'PRIVATE' => '仅自己可见',
    _ => '未公开',
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
              Text(p.record.title),
              const SizedBox(height: 12),
              Text(
                '本人报名：${switch (p.record.status) {
                  'going' => '已报名',
                  'pending' => '待确认',
                  _ => '已取消',
                }}',
              ),
              if (p.record.startsAt != null)
                Text('活动开始：${_time(p.record.startsAt!)}'),
              if (p.record.endsAt != null)
                Text('活动结束：${_time(p.record.endsAt!)}'),
              Text(
                p.operation == 'PUBLIC'
                    ? '受众：允许查看当前公开来源的用户；共同报名引荐还需双方分别允许共同信息展示和本人审阅。'
                    : '受众：仅自己可见。此操作不取消原活动报名。',
              ),
              const SizedBox(height: 12),
              Text('${_state(p.record.visibility)} → ${_state(p.operation)}'),
              const SizedBox(height: 12),
              Text(p.consequence),
              const SizedBox(height: 12),
              if (p.targetExpiresAt != null)
                Text('公开报名截止 ${_time(p.targetExpiresAt!)}'),
              Text('这次预览有效至 ${_time(p.expiresAt)}'),
              const SizedBox(height: 12),
              const Text('公开的是本人的活动报名，不代表实际到场或成员资格，也不会发送邀请、消息或取消报名。'),
            ],
          ),
        ),
        actions: [
          TextButton(
            style: TextButton.styleFrom(minimumSize: const Size(48, 48)),
            onPressed: () => Navigator.of(context).pop(false),
            child: const Text('取消'),
          ),
          FilledButton(
            style: FilledButton.styleFrom(minimumSize: const Size(48, 48)),
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
        style: OutlinedButton.styleFrom(minimumSize: const Size(48, 48)),
        onPressed: action,
        child: Padding(
          padding: const EdgeInsets.symmetric(vertical: 12),
          child: Text(label, textAlign: TextAlign.center),
        ),
      ),
    ),
  );
  Future<void> _public(ParticipationDisclosureRecord r) async {
    final data = _data;
    if (!data.canPrepare || !r.sourceAvailable || r.sourceExpiresAt == null) {
      return;
    }
    final max =
        r.sourceExpiresAt!.isBefore(
          DateTime.now().toUtc().add(const Duration(hours: 24)),
        )
        ? r.sourceExpiresAt!
        : DateTime.now().toUtc().add(const Duration(hours: 24));
    final generation = data.generation;
    final route = DialogRoute<Duration>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('选择这次公开期限'),
        content: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(r.title),
              const SizedBox(height: 12),
              Text('最多公开至 ${_time(max)}；活动或来源提前失效时将不再公开。'),
              const SizedBox(height: 12),
              for (final hours in [1, 6, 24])
                _action('$hours小时（不超过活动有效期）', () {
                  Navigator.of(ctx).pop(Duration(hours: hours));
                }),
            ],
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(ctx).pop(),
            child: const Text('取消'),
          ),
        ],
      ),
    );
    _expiryDialog = route;
    _expiryDeadline = max;
    final duration = await Navigator.of(context).push(route);
    if (identical(_expiryDialog, route)) _expiryDialog = null;
    if (!mounted ||
        !identical(data, _data) ||
        generation != data.generation ||
        !data.canPrepare ||
        duration == null) {
      return;
    }
    final requested = DateTime.now().toUtc().add(duration);
    data.chooseExpiry(requested.isBefore(max) ? requested : max);
    await _prepare(r.participationID, 'PUBLIC');
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: const Text('我的公开报名')),
    body: SafeArea(
      child: ListView(
        padding: const EdgeInsets.all(20),
        children: [
          const Text(
            '由你决定哪些报名可以被看见',
            style: TextStyle(fontSize: 24, fontWeight: FontWeight.w700),
          ),
          const SizedBox(height: 12),
          const Text('报名默认仅自己可见。公开后，可作为共同报名的引荐依据；它不代表实际到场、组织成员或邀请许可。'),
          const SizedBox(height: 12),
          if (!_data.current) const Text('请使用当前个人账号重新打开。账号或工作区变化后，旧预览已失效。'),
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
            _data.unknown ? '只读核查当前报名声明' : '刷新当前报名声明',
            _data.current && !_data.busy ? _data.load : null,
          ),
          if (_data.own != null) ...[
            const SizedBox(height: 16),
            Text(
              _data.resultOnly ? '本次操作结果（刷新可查看全部报名）' : '我的报名',
              style: const TextStyle(fontWeight: FontWeight.w700),
            ),
            if (_data.own!.records.isEmpty)
              const Padding(
                padding: EdgeInsets.symmetric(vertical: 12),
                child: Text('暂无活动报名。先在活动详情中报名，再回来选择是否公开。'),
              ),
            for (final r in _data.own!.records)
              Padding(
                padding: const EdgeInsets.symmetric(vertical: 14),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      r.title,
                      style: const TextStyle(fontWeight: FontWeight.w600),
                    ),
                    const SizedBox(height: 6),
                    Text(
                      r.visibility == 'PRIVATE'
                          ? '仅自己可见'
                          : r.effectivePublic &&
                                r.disclosureExpiresAt!.isAfter(DateTime.now())
                          ? '当前公开'
                          : '公开声明已失效',
                    ),
                    if (r.effectivePublic && r.disclosureExpiresAt != null)
                      Text('公开至 ${_time(r.disclosureExpiresAt!)}'),
                    if (!r.sourceAvailable)
                      const Text('活动当前不可公开展示；你仍可将自己的声明设为仅自己可见。'),
                    if (r.visibility == 'PUBLIC')
                      _action(
                        '检查设为仅自己可见',
                        _data.canPrepare
                            ? () => _prepare(r.participationID, 'PRIVATE')
                            : null,
                      ),
                    if (r.sourceAvailable &&
                        r.sourceExpiresAt!.isAfter(DateTime.now()))
                      _action(
                        '选择期限并检查公开报名',
                        _data.canPrepare ? () => _public(r) : null,
                      ),
                  ],
                ),
              ),
            if (_data.own!.truncated) const Text('本次仅展示前100条报名，列表并不完整。'),
          ],
        ],
      ),
    ),
  );
  @override
  void dispose() {
    final routes = <ModalRoute<dynamic>?>[_dialog, _expiryDialog];
    _dialog = null;
    _expiryDialog = null;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      for (final route in routes) {
        if (route?.isActive == true) {
          route!.navigator?.removeRoute(route);
        }
      }
    });
    _timer?.cancel();
    widget.auth.removeListener(_identity);
    widget.workspaceChanges?.removeListener(_identity);
    _data.removeListener(_changed);
    _data.dispose();
    super.dispose();
  }
}
