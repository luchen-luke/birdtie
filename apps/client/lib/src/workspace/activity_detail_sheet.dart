import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import 'package:share_plus/share_plus.dart';
import 'package:url_launcher/url_launcher.dart';

import '../city/public_city_controller.dart';
import 'activity_analytics.dart';
import 'activity_conversation_page.dart';
import 'saved_items.dart';
import 'support_page.dart';
import 'human_causal_operation_controller.dart';
import '../config/birdtie_environment.dart';
import 'connections.dart';
import 'entity_action_contract.dart';
import 'entity_action_dispatcher.dart';
import 'activity_participation_operation.dart';
import 'activity_participation_pending_store.dart';
import 'activity_participation_operation_controller.dart';
import 'entity_cover.dart';

class ActivityDetailSheet extends StatefulWidget {
  const ActivityDetailSheet({
    super.key,
    required this.activity,
    required this.authorizationHeader,
    this.apiBaseUrl,
    this.client,
    this.onParticipationChanged,
    this.saved,
    this.shareText,
    this.openExternal,
    this.onOpenOrganization,
    this.onOpenOrganizer,
    this.entrySource = 'direct',
    this.identityChanges,
    this.workspaceID,
    this.accountID,
    this.participationPendingStore,
    this.domainCurrent,
  });

  final PublicActivity activity;
  final String? Function() authorizationHeader;
  final String? apiBaseUrl;
  final http.Client? client;
  final VoidCallback? onParticipationChanged;
  final SavedController? saved;
  final Future<void> Function(String)? shareText;
  final Future<bool> Function(Uri)? openExternal;
  final ValueChanged<String>? onOpenOrganization;
  final ValueChanged<PublicActivityOrganizer>? onOpenOrganizer;
  final String entrySource;
  final Listenable? identityChanges;
  final String? Function()? workspaceID;
  final String? Function()? accountID;
  final ParticipationPendingStore? participationPendingStore;
  final bool Function()? domainCurrent;

  @override
  State<ActivityDetailSheet> createState() => _ActivityDetailSheetState();
}

class _ActivityDetailSheetState extends State<ActivityDetailSheet> {
  static const _configuredBase = BirdtieEnvironment.apiBaseUrl;
  late http.Client _client = widget.client ?? http.Client();
  bool _ownsClient = false;
  late PublicActivity _activity = widget.activity;
  String? _participationStatus;
  String? _error;
  bool _loading = true;
  bool _busy = false;
  bool _saving = false;
  bool _detailRecorded = false;
  int _identityEpoch = 0;
  final _reportSource = HumanCausalSourceSerial();
  void _observeReportSource() {
    _reportSource.observe((
      widget.activity.id,
      widget.client,
      widget.apiBaseUrl,
      widget.accountID?.call(),
      widget.workspaceID?.call(),
    ));
    _reportSource.notifyReadBindingChanged();
  }

  ParticipationOperationController? _recovery;
  bool _recoveryWasPending = false;
  late (String?, String?, String?) _identity;
  (String?, String?, String?) get _currentIdentity => (
    widget.authorizationHeader(),
    widget.workspaceID?.call(),
    widget.accountID?.call(),
  );
  ({int epoch, String? token, String? workspace, String? owner, String id})
  _frame() => (
    epoch: _identityEpoch,
    token: widget.authorizationHeader(),
    workspace: widget.workspaceID?.call(),
    owner: widget.accountID?.call(),
    id: widget.activity.id,
  );
  bool _current(
    ({int epoch, String? token, String? workspace, String? owner, String id})
    frame,
  ) =>
      mounted &&
      frame.epoch == _identityEpoch &&
      frame.token == widget.authorizationHeader() &&
      frame.workspace == widget.workspaceID?.call() &&
      frame.owner == widget.accountID?.call() &&
      widget.domainCurrent?.call() != false &&
      frame.id == widget.activity.id;

  Uri _url(String suffix) => Uri.parse(
    '${(widget.apiBaseUrl ?? _configuredBase).replaceFirst(RegExp(r'/$'), '')}/v1/activities/${Uri.encodeComponent(widget.activity.id)}$suffix',
  );

  @override
  void initState() {
    super.initState();
    _ownsClient = widget.client == null;
    _identity = _currentIdentity;
    _observeReportSource();
    widget.identityChanges?.addListener(_identityChanged);
    widget.saved?.addListener(_onSavedChange);
    _createRecovery();
    _refresh();
  }

  void _createRecovery() {
    _recovery?.removeListener(_recoveryChanged);
    _recovery?.dispose();
    _recovery = null;
    _recoveryWasPending = false;
    final frame = _frame();
    if (frame.owner == null ||
        !participationID(frame.owner!) ||
        frame.token == null ||
        frame.workspace != null) {
      return;
    }
    _recovery = ParticipationOperationController(
      activityID: frame.id,
      base: widget.apiBaseUrl ?? _configuredBase,
      client: _client,
      ownerID: () => widget.accountID?.call(),
      authorizationHeader: widget.authorizationHeader,
      workspaceID: widget.workspaceID,
      identityChanges: widget.identityChanges,
      domainCurrent: () => _current(frame),
      store: widget.participationPendingStore,
    )..addListener(_recoveryChanged);
    unawaited(_recovery!.initialize());
  }

  void _recoveryChanged() {
    if (!mounted) return;
    final recovery = _recovery;
    if (recovery == null || !recovery.current) return;
    final finished = _recoveryWasPending && recovery.pending == null;
    _recoveryWasPending = recovery.pending != null;
    setState(() {});
    // A historical receipt never replaces current participation state.
    if (finished) {
      widget.onParticipationChanged?.call();
      unawaited(_refresh());
    }
  }

  Future<void> _stopParticipationSubmission() async {
    final recovery = _recovery, frame = _frame();
    final pending = recovery?.pending;
    if (recovery == null || pending == null || recovery.busy) return;
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text(pending.stopRequested ? '再次确认停止提交' : '停止这次提交'),
        content: const Text('停止后，这次操作不会继续提交。它可能已经完成，停止提交不会取消已有报名；请随后核对当前报名状态。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('返回'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('确认停止提交'),
          ),
        ],
      ),
    );
    if (confirmed == true &&
        _current(frame) &&
        identical(recovery, _recovery) &&
        identical(pending, recovery.pending)) {
      await recovery.stopAfterConfirmation(confirmedPending: pending);
    }
  }

  void _identityChanged() {
    _observeReportSource();
    if (_identity == _currentIdentity) return;
    _identity = _currentIdentity;
    ++_identityEpoch;
    _participationStatus = null;
    _busy = false;
    _saving = false;
    _detailRecorded = false;
    _createRecovery();
    unawaited(_refresh());
  }

  @override
  void didUpdateWidget(ActivityDetailSheet old) {
    super.didUpdateWidget(old);
    _observeReportSource();
    final transportChanged =
        !identical(old.client, widget.client) ||
        old.apiBaseUrl != widget.apiBaseUrl;
    if (!identical(old.client, widget.client)) {
      if (_ownsClient) _client.close();
      _client = widget.client ?? http.Client();
      _ownsClient = widget.client == null;
    }

    if (old.identityChanges != widget.identityChanges) {
      old.identityChanges?.removeListener(_identityChanged);
      widget.identityChanges?.addListener(_identityChanged);
    }
    if (!identical(old.saved, widget.saved)) {
      old.saved?.removeListener(_onSavedChange);
      widget.saved?.addListener(_onSavedChange);
    }
    if (transportChanged ||
        !identical(old.authorizationHeader, widget.authorizationHeader) ||
        !identical(old.workspaceID, widget.workspaceID) ||
        !identical(old.accountID, widget.accountID) ||
        !identical(old.domainCurrent, widget.domainCurrent) ||
        !identical(
          old.participationPendingStore,
          widget.participationPendingStore,
        ) ||
        !identical(old.identityChanges, widget.identityChanges) ||
        old.activity.id != widget.activity.id ||
        _identity != _currentIdentity) {
      _identity = _currentIdentity;
      ++_identityEpoch;
      _participationStatus = null;
      _busy = false;
      _saving = false;
      _detailRecorded = false;
      _createRecovery();
      unawaited(_refresh());
    }
  }

  void _onSavedChange() {
    if (mounted) setState(() {});
  }

  @override
  void dispose() {
    _reportSource.dispose();
    ++_identityEpoch;
    widget.identityChanges?.removeListener(_identityChanged);
    widget.saved?.removeListener(_onSavedChange);
    _recovery?.removeListener(_recoveryChanged);
    _recovery?.dispose();
    if (_ownsClient) _client.close();
    super.dispose();
  }

  Future<void> _refresh() async {
    final frame = _frame();
    final headers = {
      'Authorization': ?frame.token,
      'X-Birdtie-Entry-Source': widget.entrySource,
    };
    if ((widget.apiBaseUrl ?? _configuredBase).isEmpty) {
      if (mounted) {
        setState(() {
          _loading = false;
          _error = '暂时无法连接活动服务。';
        });
      }
      return;
    }
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final detail = await _client
          .get(_url(''), headers: headers)
          .timeout(const Duration(seconds: 12));
      if (detail.statusCode != 200) throw StateError('无法读取活动详情');
      final current = PublicActivity.fromJson(
        (jsonDecode(detail.body) as Map<String, dynamic>)['data']
            as Map<String, dynamic>,
      );
      if (!_current(frame)) return;
      if (current.id != frame.id) throw const FormatException('活动标识不符');
      if (!_detailRecorded && current.organizationID != null) {
        _detailRecorded = true;
        unawaited(
          recordActivityView(
            current.id,
            'detail',
            widget.entrySource,
            apiBaseUrl: widget.apiBaseUrl ?? _configuredBase,
          ),
        );
      }
      String? status;
      if (frame.token != null && frame.workspace == null) {
        final own = await _client
            .get(_url('/participations/me'), headers: headers)
            .timeout(const Duration(seconds: 12));
        if (own.statusCode != 200) throw StateError('无法读取报名状态');
        final data = (jsonDecode(own.body) as Map<String, dynamic>)['data'];
        if (data is Map<String, dynamic>) status = data['status'] as String?;
      }
      if (!_current(frame)) return;
      setState(() {
        _activity = current;
        _participationStatus = status;
        _loading = false;
      });
    } catch (_) {
      if (!_current(frame)) return;
      setState(() {
        _loading = false;
        _error = '活动状态加载失败，请重试。';
      });
    }
  }

  Future<void> _changeParticipation({EntityActionDescriptor? approved}) async {
    if (_busy || _recovery?.blocked == true) return;
    final frame = _frame();
    if (frame.token == null || frame.workspace != null) return;
    if (approved == null) {
      await runEntityAction(
        context,
        ref: EntityActionRef('activity', frame.id),
        kind: EntityActionKind.join,
        authorizationHeader: () => _current(frame) ? frame.token : null,
        workspaceID: widget.workspaceID,
        identityChanges: widget.identityChanges,
        client: _client,
        apiBaseUrl: widget.apiBaseUrl,
        domainCurrent: () => _current(frame),
        prepareReview: (_, _) async {
          final shown = _participationTerms(_activity);
          final response = await _client
              .get(_url(''), headers: {'Authorization': frame.token!})
              .timeout(const Duration(seconds: 12));
          if (!_current(frame) || response.statusCode != 200) return false;
          final current = PublicActivity.fromJson(
            (jsonDecode(response.body) as Map<String, dynamic>)['data']
                as Map<String, dynamic>,
          );
          if (current.id != frame.id) return false;
          if (_participationTerms(current) != shown) {
            setState(() {
              _activity = current;
              _error = '活动条件已变化，请检查最新费用、时间和报名要求后重新发起。';
            });
            return false;
          }
          return true;
        },
        reviewDetails: (_) =>
            '时间：${_activity.schedule.isNotEmpty ? _activity.schedule : _activity.startsAt.toIso8601String()}\n费用：${_activity.priceMinor == 0 ? '免费' : '${_activity.currency} ${(_activity.priceMinor / 100).toStringAsFixed(2)}'}\n报名要求：${_activity.eligibility.isEmpty ? '未注明额外要求' : _activity.eligibility}',
        handler: (a) => _changeParticipation(approved: a),
      );
      return;
    }
    final already =
        _participationStatus == 'going' || _participationStatus == 'pending';
    if (approved.target != EntityActionRef('activity', frame.id) ||
        approved.operation != (already ? 'CANCEL_RSVP' : 'JOIN')) {
      await _refresh();
      throw StateError('参加状态已变化，请重新检查。');
    }
    final recovery = _recovery;
    if (recovery == null || !recovery.current) {
      setState(() => _error = '无法核对当前个人身份，暂不提交报名操作。');
      return;
    }
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await recovery.submit(approved);
    } catch (error) {
      if (!_current(frame)) return;
      setState(() {
        _error = '报名结果尚未核实，请核对原操作。';
      });
    } finally {
      if (_current(frame)) setState(() => _busy = false);
    }
  }

  String _participationTerms(PublicActivity a) => jsonEncode([
    a.id,
    a.title,
    a.startsAt.toUtc().toIso8601String(),
    a.endsAt.toUtc().toIso8601String(),
    a.timeZone,
    a.schedule,
    a.endSchedule,
    a.priceMinor,
    a.currency,
    a.capacity,
    a.eligibility,
    a.status,
    a.modality,
    a.placeName,
  ]);

  Future<void> _toggleSaved({EntityActionDescriptor? approved}) async {
    final saved = widget.saved;
    if (saved == null || _saving) return;
    final frame = _frame();
    if (approved == null) {
      await runEntityAction(
        context,
        ref: EntityActionRef('activity', frame.id),
        kind: EntityActionKind.save,
        authorizationHeader: () => _current(frame) ? frame.token : null,
        workspaceID: widget.workspaceID,
        identityChanges: widget.identityChanges,
        client: _client,
        apiBaseUrl: widget.apiBaseUrl,
        domainCurrent: () => _current(frame),
        handler: (a) => _toggleSaved(approved: a),
      );
      return;
    }
    if (approved.target != EntityActionRef('activity', frame.id) ||
        saved.authorizationHeader() != frame.token ||
        !const {'SAVE', 'UNSAVE'}.contains(approved.operation)) {
      throw StateError('收藏状态或身份已变化，请重新检查。');
    }
    setState(() {
      _saving = true;
      _error = null;
    });
    try {
      await saved.toggle(
        'activity',
        _activity.id,
        entrySource: widget.entrySource,
        approved: approved,
      );
    } catch (_) {
      if (_current(frame)) {
        setState(() {
          _error = '收藏更新失败，请重试。';
        });
      }
    } finally {
      if (_current(frame)) {
        setState(() {
          _saving = false;
        });
      }
    }
  }

  Future<void> _shareToFriend() async {
    final frame = _frame();
    await runEntityAction(
      context,
      ref: EntityActionRef('activity', frame.id),
      kind: EntityActionKind.share,
      authorizationHeader: () => _current(frame) ? frame.token : null,
      workspaceID: widget.workspaceID,
      identityChanges: widget.identityChanges,
      client: _client,
      apiBaseUrl: widget.apiBaseUrl,
      domainCurrent: () => _current(frame),
      handler: (_) async {
        if (!_current(frame)) return;
        await shareEntityToChat(
          context,
          authorizationHeader: () => widget.authorizationHeader(),
          identityChanges: _reportSource,
          ownerID: widget.accountID == null
              ? null
              : () => widget.accountID?.call(),
          workspaceID: () => widget.workspaceID?.call(),
          sourceKey: () => _reportSource.generation,
          sourceCurrent: () => mounted && widget.domainCurrent?.call() != false,
          type: 'activity',
          id: frame.id,
          apiBaseUrl: widget.apiBaseUrl,
          client: _client,
        );
      },
    );
  }

  Future<void> _share({EntityActionDescriptor? approved}) async {
    final frame = _frame();
    if (!_current(frame) || frame.workspace != null) return;
    if (approved == null) {
      final shown = [
        _activity.title,
        _activity.summary,
        _activity.schedule,
        _activity.placeName,
        _activity.modality,
        _activity.physicalPlaceStatus,
      ].join('\u0000');
      await runEntityAction(
        context,
        ref: EntityActionRef('activity', frame.id),
        kind: EntityActionKind.share,
        operation: 'EXPORT_PUBLIC',
        authorizationHeader: () => _current(frame) ? frame.token : null,
        workspaceID: widget.workspaceID,
        identityChanges: widget.identityChanges,
        client: _client,
        apiBaseUrl: widget.apiBaseUrl,
        domainCurrent: () => _current(frame),
        prepareReview: (_, _) async {
          final response = await _client
              .get(
                _url(''),
                headers: frame.token == null
                    ? const {}
                    : {'Authorization': frame.token!},
              )
              .timeout(const Duration(seconds: 12));
          if (!_current(frame) || response.statusCode != 200) return false;
          final current = PublicActivity.fromJson(
            (jsonDecode(utf8.decode(response.bodyBytes))
                    as Map<String, dynamic>)['data']
                as Map<String, dynamic>,
          );
          if (current.id != frame.id) return false;
          final now = [
            current.title,
            current.summary,
            current.schedule,
            current.placeName,
            current.modality,
            current.physicalPlaceStatus,
          ].join('\u0000');
          if (now != shown) {
            setState(() {
              _activity = current;
              _error = '分享内容已变化，请检查最新公开详情后重新发起。';
            });
            return false;
          }
          return true;
        },
        reviewDetails: (_) =>
            '公开内容：${_activity.title}\n${_activity.summary}\n时间：${_activity.schedule}\n地点：${_activity.placeName}',
        handler: (a) => _share(approved: a),
      );
      return;
    }
    if (approved.operation != 'EXPORT_PUBLIC' ||
        approved.target != EntityActionRef('activity', frame.id)) {
      return;
    }
    final text = [
      _activity.title,
      if (_activity.schedule.isNotEmpty) '时间：${_activity.schedule}',
      if (_activity.placeName.isNotEmpty) '地点：${_activity.placeName}',
      if (_activity.physicalPlaceStatus == 'tbd') '地点：待定',
      if (_activity.physicalPlaceStatus == 'confirmed' &&
          _activity.placeName.isEmpty)
        '地点信息暂不可用，请向主办方确认',
      if (_activity.modality == 'online') '形式：线上活动',
      if (_activity.modality == 'hybrid') '形式：线上＋线下活动',
      if (_activity.summary.isNotEmpty) _activity.summary,
    ].join('\n');
    try {
      if (widget.shareText case final share?) {
        await share(text);
      } else {
        final box = context.findRenderObject() as RenderBox?;
        await SharePlus.instance.share(
          ShareParams(
            text: text,
            title: _activity.title,
            sharePositionOrigin: box == null
                ? null
                : box.localToGlobal(Offset.zero) & box.size,
          ),
        );
      }
    } catch (_) {
      if (mounted) {
        setState(() {
          _error = '分享暂不可用，请稍后重试。';
        });
      }
    }
  }

  Future<void> _openLink(Uri uri) async {
    try {
      final opened =
          await (widget.openExternal?.call(uri) ??
              launchUrl(uri, mode: LaunchMode.externalApplication));
      if (!opened && mounted) {
        setState(() {
          _error = '无法打开链接，请检查设备设置。';
        });
      }
    } catch (_) {
      if (mounted) {
        setState(() {
          _error = '无法打开链接，请检查设备设置。';
        });
      }
    }
  }

  Uri? get _navigationURL {
    if (_activity.physicalPlaceStatus != 'confirmed' &&
        _activity.modality != 'unspecified') {
      return null;
    }
    final location = _activity.location;
    if (location?.hasPublicPoint != true) return null;
    return Uri.https('www.google.com', '/maps/search/', {
      'api': '1',
      'query': '${location!.latitude},${location.longitude}',
    });
  }

  Future<void> _navigate() async {
    final frame = _frame(),
        shown = _navigationURL,
        shownTitle = _activity.title;
    if (shown == null || frame.workspace != null) return;
    await runEntityAction(
      context,
      ref: EntityActionRef('activity', frame.id),
      kind: EntityActionKind.navigate,
      authorizationHeader: () => _current(frame) ? frame.token : null,
      workspaceID: widget.workspaceID,
      identityChanges: widget.identityChanges,
      client: _client,
      apiBaseUrl: widget.apiBaseUrl,
      domainCurrent: () => _current(frame),
      prepareReview: (_, _) async {
        final response = await _client
            .get(
              _url(''),
              headers: {if (frame.token != null) 'Authorization': frame.token!},
            )
            .timeout(const Duration(seconds: 12));
        if (!_current(frame) || response.statusCode != 200) return false;
        final current = PublicActivity.fromJson(
          (jsonDecode(response.body) as Map<String, dynamic>)['data']
              as Map<String, dynamic>,
        );
        if (current.id != frame.id) return false;
        final old = _activity;
        _activity = current;
        final same =
            _navigationURL == shown &&
            current.title == shownTitle &&
            current.placeName == old.placeName &&
            current.modality == old.modality;
        if (!same) {
          setState(() => _error = '导航地点已变化，请检查最新详情后重新发起。');
          return false;
        }
        return true;
      },
      reviewDetails: (_) =>
          '目的地：${_activity.placeName.isEmpty ? _activity.title : _activity.placeName}\n只使用当前公开的准确地点，下一步打开外部地图。',
      handler: (_) async {
        if (_current(frame) && _navigationURL == shown) await _openLink(shown);
      },
    );
  }

  Uri? _httpsURL(String value) {
    final uri = Uri.tryParse(value);
    return uri?.scheme == 'https' && uri?.host.isNotEmpty == true ? uri : null;
  }

  String get _timeLabel {
    if (_activity.schedule.isEmpty) return '未注明';
    final start = _activity.schedule;
    final end = _activity.endSchedule;
    final dayEnd = start.lastIndexOf('）');
    final sameDay =
        dayEnd >= 0 && end.startsWith(start.substring(0, dayEnd + 1));
    final endLabel = sameDay ? end.substring(dayEnd + 1) : end;
    final zone = switch (_activity.timeZone) {
      'Europe/London' => '英国当地时间',
      'Asia/Shanghai' => '中国当地时间',
      _ => _activity.timeZone,
    };
    return '${_activity.startsAt.year}年$start${end.isEmpty ? '' : ' 至 $endLabel'}${zone.isEmpty ? '' : ' · $zone'}';
  }

  @override
  Widget build(BuildContext context) {
    final joined =
        _participationStatus == 'going' || _participationStatus == 'pending';
    final open =
        _activity.status == 'upcoming' || _activity.status == 'ongoing';
    final full =
        _activity.capacity != null &&
        (_activity.participantCount ?? 0) >= _activity.capacity!;
    final saved = widget.saved;
    final navigation = _navigationURL;
    final official = _httpsURL(_activity.officialURL);
    final source = _httpsURL(_activity.source.reference);
    final coverFrame = _frame();
    final price = _activity.priceMinor == 0
        ? '免费'
        : '${_activity.currency} ${(_activity.priceMinor / 100).toStringAsFixed(2)}';
    return SafeArea(
      child: ConstrainedBox(
        constraints: BoxConstraints(
          maxHeight: MediaQuery.sizeOf(context).height * 0.8,
        ),
        child: SingleChildScrollView(
          padding: const EdgeInsets.fromLTRB(24, 26, 24, 36),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              if (!_loading &&
                  _error == null &&
                  _activity.cover != null &&
                  _activity.cover!.ref ==
                      EntityCoverRef('activity', _activity.id))
                EntityCover(
                  key: ValueKey((
                    'activity-cover',
                    _activity.id,
                    coverFrame.epoch,
                  )),
                  metadata: _loading ? null : _activity.cover,
                  api: EntityCoverAPI(
                    client: _client,
                    baseUrl: widget.apiBaseUrl ?? _configuredBase,
                    authorizationHeader: widget.authorizationHeader,
                    current: () => _current(coverFrame),
                  ),
                  identityChanges: widget.identityChanges,
                  current: () =>
                      !_loading && _error == null && _current(coverFrame),
                  width: 320,
                  height: 180,
                  label: '${_activity.title}的活动封面',
                ),
              Text(
                _activity.title,
                style: Theme.of(context).textTheme.titleLarge,
              ),
              const SizedBox(height: 8),
              Text(switch (_activity.status) {
                'upcoming' => '即将开始',
                'ongoing' => '进行中',
                'cancelled' => '活动已取消',
                'past' || 'completed' => '活动已结束',
                _ => '活动状态待核对',
              }, style: Theme.of(context).textTheme.bodySmall),
              const SizedBox(height: 18),
              if (_activity.organizer case final organizer?)
                widget.onOpenOrganizer == null
                    ? Text('主办方：${organizer.name}')
                    : TextButton.icon(
                        onPressed: () => widget.onOpenOrganizer!(organizer),
                        icon: Icon(switch (organizer.type) {
                          'PERSON' => Icons.person_outline,
                          'COMMUNITY' => Icons.groups_outlined,
                          _ => Icons.business_outlined,
                        }),
                        label: Text('查看主办方：${organizer.name}'),
                      )
              else if (_activity.hostLabel.isNotEmpty)
                _activity.organizationID == null ||
                        widget.onOpenOrganization == null
                    ? Text('主办方：${_activity.hostLabel}')
                    : TextButton.icon(
                        onPressed: () => widget.onOpenOrganization!(
                          _activity.organizationID!,
                        ),
                        icon: const Icon(Icons.groups_outlined),
                        label: Text('查看主办方：${_activity.hostLabel}'),
                      ),
              if (_activity.placeName.isNotEmpty)
                Text('地点：${_activity.placeName}'),
              if (_activity.physicalPlaceStatus == 'tbd') const Text('地点：待定'),
              if (_activity.physicalPlaceStatus == 'confirmed' &&
                  _activity.placeName.isEmpty)
                const Text('地点信息暂不可用，请向主办方确认'),
              if (_activity.modality == 'online') const Text('形式：线上活动'),
              if (_activity.modality == 'hybrid') const Text('形式：线上＋线下活动'),
              if (_activity.schedule.isNotEmpty) Text('时间：$_timeLabel'),
              const SizedBox(height: 8),
              Text('费用：$price'),
              Text(
                '参加条件：${_activity.eligibility.isEmpty ? '未注明' : _activity.eligibility}',
              ),
              if (_activity.capacity != null)
                Text(
                  '名额：${_activity.participantCount ?? 0} / ${_activity.capacity}${full ? ' · 已满' : ''}',
                ),
              if (_activity.summary.isNotEmpty) ...[
                const SizedBox(height: 18),
                Text(_activity.summary),
              ],
              if (_activity.description.isNotEmpty &&
                  _activity.description != _activity.summary) ...[
                const SizedBox(height: 12),
                Text(_activity.description),
              ],
              if (_activity.source.label.isNotEmpty) ...[
                const SizedBox(height: 18),
                Text('来源：${_activity.source.label}'),
              ],
              if (official != null)
                TextButton.icon(
                  onPressed: () => _openLink(official),
                  icon: const Icon(Icons.open_in_new),
                  label: const Text('主办方提供的链接'),
                ),
              if (source != null && source != official)
                TextButton.icon(
                  onPressed: () => _openLink(source),
                  icon: const Icon(Icons.open_in_new),
                  label: const Text('查看活动来源'),
                ),
              const SizedBox(height: 12),
              if (_loading) const LinearProgressIndicator(),
              if (_error != null) ...[
                Text(
                  _error!,
                  style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                    color: Theme.of(context).colorScheme.error,
                  ),
                ),
                TextButton(onPressed: _refresh, child: const Text('重新读取详情')),
              ],
              if (!_loading) ...[
                if (_recovery?.message case final message?) ...[
                  Text(message, style: Theme.of(context).textTheme.bodyMedium),
                  const SizedBox(height: 8),
                ],
                if (_recovery?.pending != null)
                  Wrap(
                    spacing: 8,
                    runSpacing: 8,
                    children: [
                      OutlinedButton(
                        onPressed: _recovery!.busy ? null : _recovery!.verify,
                        child: const Text('核对原操作'),
                      ),
                      TextButton(
                        onPressed: _recovery!.busy
                            ? null
                            : _stopParticipationSubmission,
                        child: Text(
                          _recovery!.pending!.stopRequested
                              ? '再次确认停止提交'
                              : '停止这次提交',
                        ),
                      ),
                    ],
                  ),
                if (_participationStatus == 'going') const Text('你已报名'),
                if (_participationStatus == 'pending') const Text('报名待确认'),
                if (widget.authorizationHeader() == null)
                  const Text('登录后可以报名活动。')
                else if (widget.workspaceID?.call() != null)
                  const Text('请切回个人身份报名活动。')
                else if (open || joined)
                  FilledButton(
                    onPressed:
                        _busy ||
                            _recovery?.blocked == true ||
                            (_error != null && !full) ||
                            (full && !joined)
                        ? null
                        : _changeParticipation,
                    child: Text(
                      _busy
                          ? '正在更新…'
                          : joined
                          ? '取消报名'
                          : full
                          ? '名额已满'
                          : '报名参加',
                    ),
                  ),
                const SizedBox(height: 10),
                Wrap(
                  spacing: 8,
                  runSpacing: 4,
                  children: [
                    if (widget.authorizationHeader() != null)
                      OutlinedButton.icon(
                        onPressed: () => Navigator.of(context).push(
                          MaterialPageRoute<void>(
                            builder: (_) => ActivityConversationPage(
                              activityID: _activity.id,
                              activityTitle: _activity.title,
                              ownerID: widget.accountID,
                              identityChanges: widget.identityChanges,
                              sourceCurrent: () =>
                                  mounted && widget.workspaceID?.call() == null,
                              authorizationHeader: widget.authorizationHeader,
                              apiBaseUrl: widget.apiBaseUrl,
                              client: widget.client,
                            ),
                          ),
                        ),
                        icon: const Icon(Icons.forum_outlined),
                        label: const Text('活动交流'),
                      ),
                    if (saved != null)
                      OutlinedButton.icon(
                        onPressed:
                            widget.authorizationHeader() == null || _saving
                            ? null
                            : _toggleSaved,
                        icon: Icon(
                          saved.contains('activity', _activity.id)
                              ? Icons.bookmark
                              : Icons.bookmark_border,
                        ),
                        label: Text(
                          _saving
                              ? '正在更新…'
                              : saved.contains('activity', _activity.id)
                              ? '已收藏'
                              : '收藏',
                        ),
                      ),
                    OutlinedButton.icon(
                      onPressed: _share,
                      icon: const Icon(Icons.share_outlined),
                      label: const Text('分享'),
                    ),
                    OutlinedButton.icon(
                      onPressed: _shareToFriend,
                      icon: const Icon(Icons.chat_bubble_outline),
                      label: const Text('发给好友'),
                    ),
                    TextButton.icon(
                      onPressed: () => Navigator.of(context).push(
                        MaterialPageRoute<void>(
                          builder: (_) => humanSupportDestination(
                            authorizationHeader: widget.authorizationHeader,
                            apiBaseUrl: widget.apiBaseUrl ?? _configuredBase,
                            targetType: 'activity',
                            targetID: _activity.id,
                            ownerID: widget.accountID,
                            identityChanges: Listenable.merge([
                              widget.identityChanges,
                              _reportSource,
                            ]),
                            sourceCurrent: () =>
                                mounted && widget.workspaceID?.call() == null,
                            client: _client,
                            sourceKey: () => _reportSource.generation,
                          ),
                        ),
                      ),
                      icon: const Icon(Icons.flag_outlined),
                      label: const Text('举报活动'),
                    ),
                    if (navigation != null)
                      OutlinedButton.icon(
                        onPressed: _navigate,
                        icon: const Icon(Icons.directions_outlined),
                        label: const Text('导航'),
                      ),
                  ],
                ),
              ],
            ],
          ),
        ),
      ),
    );
  }
}
