import 'dart:async';
import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import 'package:uuid/uuid.dart';
import '../app/birdtie_app_bar.dart';
import '../city/public_city_controller.dart';
import 'organization_coordinate_picker.dart';
import 'organization_human_control.dart';
import 'organization_map_location_api.dart';

class OrganizationMapLocationPage extends StatefulWidget {
  const OrganizationMapLocationPage({
    super.key,
    required this.organizationID,
    required this.cityID,
    required this.authorizationHeader,
    this.ownerID,
    this.identityChanges,
    this.sourceCurrent,
    this.city,
    this.citySource,
    this.api,
    this.client,
    this.apiBaseUrl,
    this.pendingStore,
  });
  final String organizationID, cityID;
  final String? Function() authorizationHeader;
  final String? Function()? ownerID;
  final Listenable? identityChanges;
  final bool Function()? sourceCurrent;
  final PublicCity? city;
  final PublicCity? Function()? citySource;
  final OrganizationMapLocationApi? api;
  final http.Client? client;
  final String? apiBaseUrl;
  final OrganizationControlPendingStore? pendingStore;
  @override
  State<OrganizationMapLocationPage> createState() =>
      _OrganizationMapLocationPageState();
}

class _OrganizationMapLocationPageState
    extends State<OrganizationMapLocationPage> {
  final _latitude = TextEditingController(),
      _longitude = TextEditingController();
  late OrganizationMapLocationApi _api;
  late OrganizationHumanPageBinding _binding;
  OrganizationMapControl? _snapshot;
  bool _loading = true;
  String? _error;
  int _readEpoch = 0, _draftEpoch = 0;
  Timer? _lease;
  final _localIdentity = ValueNotifier<int>(0);
  int _pickerRoutes = 0;
  bool _pageDisposed = false;
  OrganizationControlOperation get _op =>
      _binding.operation(widget.organizationID);
  PublicCity? get _city => widget.citySource?.call() ?? widget.city;
  Object get _cityIdentity =>
      (widget.cityID, _city?.id, _city?.map?.provider, _city?.map?.sourceRef);
  bool get _source => _binding.current && _snapshot?.current == true;
  @override
  void initState() {
    super.initState();
    _configure();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) unawaited(_load());
    });
  }

  void _configure() {
    _api =
        widget.api ??
        OrganizationMapLocationApi(
          authorizationHeader: () => widget.authorizationHeader(),
          client: widget.client,
          apiBaseUrl: widget.apiBaseUrl,
        );
    _binding = OrganizationHumanPageBinding(
      api: _api.controlApi,
      target: () => (
        widget.organizationID,
        widget.api,
        widget.client,
        widget.apiBaseUrl,
        _cityIdentity,
      ),
      header: () => widget.authorizationHeader(),
      ownerID: widget.ownerID == null ? null : () => widget.ownerID!(),
      identityChanges: widget.identityChanges,
      sourceCurrent: () => mounted && widget.sourceCurrent?.call() != false,
      store: widget.pendingStore,
      onRetired: (scopeChanged) {
        _lease?.cancel();
        _snapshot = null;
        ++_readEpoch;
        ++_draftEpoch;
        _localIdentity.value++;
        if (scopeChanged) {
          _latitude.clear();
          _longitude.clear();
        }
        organizationPageUpdate(this, () {
          if (mounted)
            setState(() {
              _loading = false;
              _error = '当前身份、组织或城市来源已变化，请重新读取。';
            });
        });
      },
    );
    _op.addListener(_changed);
  }

  void _changed() => organizationPageUpdate(this, () {
    if (mounted) setState(() {});
  });
  @override
  void didUpdateWidget(OrganizationMapLocationPage old) {
    super.didUpdateWidget(old);
    if (old.organizationID != widget.organizationID ||
        old.cityID != widget.cityID ||
        old.api != widget.api ||
        old.client != widget.client ||
        old.apiBaseUrl != widget.apiBaseUrl ||
        old.identityChanges != widget.identityChanges ||
        (old.ownerID == null) != (widget.ownerID == null)) {
      _op.removeListener(_changed);
      _binding.dispose();
      if (old.api == null) _api.dispose();
      _snapshot = null;
      _latitude.clear();
      _longitude.clear();
      ++_draftEpoch;
      _localIdentity.value++;
      _configure();
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) unawaited(_load());
      });
    } else {
      _binding.observe();
    }
  }

  Future<void> _load() async {
    final guard = _binding.capture(), read = ++_readEpoch;
    if (!guard()) return;
    _lease?.cancel();
    _op.invalidatePreview();
    setState(() {
      _loading = true;
      _error = null;
      _snapshot = null;
    });
    try {
      await _binding.resolveOwner();
      if (!guard() || read != _readEpoch) return;
      await _binding.restore(widget.organizationID);
      if (!guard() || read != _readEpoch) return;
      final s = await _api.control(widget.organizationID, guard: guard);
      if (!guard() || read != _readEpoch) return;
      setState(() => _snapshot = s);
      _lease = Timer(s.expiresAt.difference(DateTime.now().toUtc()), () {
        if (guard() && identical(_snapshot, s)) {
          _op.invalidatePreview();
          _localIdentity.value++;
          setState(() {
            _snapshot = null;
            _error = '位置资料已到期，请重新读取。';
          });
        }
      });
    } catch (e) {
      if (guard() && read == _readEpoch)
        setState(() {
          _snapshot = null;
          _error = organizationControlMessage(e);
        });
    } finally {
      if (guard() && read == _readEpoch) setState(() => _loading = false);
    }
  }

  void _edited() {
    ++_draftEpoch;
    _op.invalidatePreview();
    setState(() {});
  }

  Future<void> _pick(OrganizationMapControl source, Object originalCity) async {
    final guard = _binding.capture(),
        s = source,
        city = _city,
        cityIdentity = originalCity;
    if (!identical(_snapshot, s) ||
        cityIdentity != _cityIdentity ||
        !_source ||
        !s.canManage ||
        city == null ||
        city.id != widget.cityID ||
        _op.busy)
      return;
    bool current() =>
        guard() &&
        _source &&
        identical(_snapshot, s) &&
        _cityIdentity == cityIdentity;
    final identity = Listenable.merge([
      if (widget.identityChanges != null) widget.identityChanges!,
      _localIdentity,
    ]);
    final route = MaterialPageRoute<OrganizationDraftCoordinate>(
      builder: (_) => OrganizationCoordinatePickerPage(
        city: city,
        identityChanges: identity,
        sourceCurrent: current,
      ),
    );
    ++_pickerRoutes;
    OrganizationDraftCoordinate? selected;
    try {
      selected = await Navigator.of(context).push(route);
    } finally {
      unawaited(
        route.completed.then((_) {
          --_pickerRoutes;
          if (_pageDisposed && _pickerRoutes == 0) _localIdentity.dispose();
        }),
      );
    }
    if (selected == null || !current() || !mounted) return;
    if (!organizationWGS84(selected.latitude, selected.longitude)) return;
    _latitude.text = '${selected.latitude}';
    _longitude.text = '${selected.longitude}';
    _edited();
  }

  Future<void> _action(
    String action, {
    OrganizationLocationProposal? proposal,
    OrganizationMapControl? source,
  }) async {
    final s = source ?? _snapshot,
        guard = _binding.capture(),
        draft = _draftEpoch;
    if (s == null || !_source || !_op.canWrite) return;
    bool allowed() =>
        _source &&
        identical(_snapshot, s) &&
        (proposal == null || s.proposals.contains(proposal)) &&
        s.authorityVersion < organizationMaxControlVersion &&
        switch (action) {
          'PROPOSE' =>
            s.canManage &&
                widget.cityID.isNotEmpty &&
                s.canonicalRevision < organizationMaxControlVersion &&
                draft == _draftEpoch,
          'HIDE_POINT' =>
            s.canManage &&
                s.canonical != null &&
                s.canonical!.visibility == 'public' &&
                s.canonicalRevision < organizationMaxControlVersion,
          'APPROVE_POINT' =>
            proposal?.canApprove == true &&
                proposal!.submittedBy != _binding.owner &&
                proposal.canonicalRevision == s.canonicalRevision,
          'REJECT_POINT' =>
            proposal?.canReject == true &&
                proposal!.submittedBy != _binding.owner &&
                proposal.canonicalRevision == s.canonicalRevision,
          'WITHDRAW_POINT' => proposal?.canWithdraw == true,
          _ => false,
        };
    bool current() => guard() && allowed();
    if (!current()) return;
    final lat = action == 'PROPOSE'
            ? double.tryParse(_latitude.text.trim())
            : null,
        lon = action == 'PROPOSE'
            ? double.tryParse(_longitude.text.trim())
            : null;
    if (action == 'PROPOSE' && !organizationWGS84(lat, lon)) {
      setState(() => _error = '请输入成对、有限的 WGS84 纬度与经度（纬度±90，经度±180）。');
      return;
    }
    final input = OrganizationControlInput(
      action: action,
      operationID: const Uuid().v4(),
      expectedAuthorityVersion: s.authorityVersion,
      cityID: action == 'PROPOSE' ? widget.cityID : '',
      latitude: lat ?? 0,
      longitude: lon ?? 0,
      proposalID: proposal?.id ?? '',
      expectedProposalVersion: proposal?.version ?? 0,
      expectedCanonicalRevision: action == 'WITHDRAW_POINT'
          ? 0
          : s.canonicalRevision,
    );
    final p = await _op.preview(input);
    if (p == null || !current() || !mounted) return;
    final title = switch (action) {
      'PROPOSE' => '确认位置提案',
      'APPROVE_POINT' => '确认批准位置',
      'REJECT_POINT' => '确认拒绝提案',
      'WITHDRAW_POINT' => '确认撤回提案',
      _ => '确认隐藏公开点',
    };
    final consequence = switch (action) {
      'PROPOSE' => '仅提交组织位置提案。原公开点保持原状；须由不同的当前城市审核者批准后才替换。',
      'APPROVE_POINT' => '你将独立批准这份具体提案并替换原公开点。只有组织身份与当前公开条件仍有效时才会在地图显示。',
      'REJECT_POINT' => '这份提案不再等待批准；原公开点保持原状。',
      'WITHDRAW_POINT' => '撤回这份待审提案；原公开点保持原状。',
      _ => '原公开点将隐藏，不在地图展示。不会从活动或成员位置推断新坐标。',
    };
    final target = action == 'PROPOSE'
        ? '${_city?.name ?? widget.cityID}\nWGS84 纬度 $lat，经度 $lon'
        : proposal != null
        ? '提案 ${s.proposals.indexOf(proposal) + 1}\n${proposal.cityID}\nWGS84 纬度 ${proposal.latitude}，经度 ${proposal.longitude}\n提案版本 ${proposal.version}'
        : '原公开点\nWGS84 纬度 ${s.canonical?.latitude}，经度 ${s.canonical?.longitude}';
    final yes = await confirmOrganizationControl(
      context,
      _op,
      p,
      title: title,
      target: '$target\n原公开点版本 ${s.canonicalRevision}',
      consequence: consequence,
      sourceCurrent: current,
    );
    if (!yes || !current()) return;
    final r = await _op.submit(
      p,
      input.selector(widget.organizationID),
      sourceCurrent: current,
    );
    if (r != null && guard() && mounted) await _load();
  }

  Future<void> _recover({required bool stop}) async {
    final guard = _binding.capture(), r = await _op.recover(stop: stop);
    if (r != null && guard() && mounted) await _load();
  }

  @override
  void dispose() {
    _lease?.cancel();
    _op.removeListener(_changed);
    _binding.dispose();
    _pageDisposed = true;
    _localIdentity.value++;
    if (_pickerRoutes == 0) _localIdentity.dispose();
    if (widget.api == null) _api.dispose();
    _latitude.dispose();
    _longitude.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final s = _source ? _snapshot : null;
    return Scaffold(
      appBar: birdtieAppBar(context, title: const Text('组织地图位置')),
      body: ListView(
        padding: const EdgeInsets.all(20),
        children: [
          const Text('只提交组织明确授权公开的地点。请勿填写成员住址、私人位置或从活动地点推断组织坐标。'),
          if (_loading) const LinearProgressIndicator(),
          if (_error != null)
            Text(_error!, key: const Key('organization_map_error')),
          organizationOperationRecovery(_op, _recover),
          TextButton(
            onPressed: _op.busy ? null : _load,
            child: const Text('重新读取位置'),
          ),
          if (s != null) ...[
            Text(
              s.canonical == null
                  ? '尚无公开点。'
                  : s.canonical!.visibility == 'hidden'
                  ? '原公开点已隐藏。'
                  : s.canonical!.reviewStatus == 'approved'
                  ? '原公开点已审核；实际展示仍取决于当前组织身份和公开条件。'
                  : '原点尚未获准公开。',
              key: const Key('organization_canonical_status'),
            ),
            if (s.canonical != null)
              Text(
                '原点 WGS84：${s.canonical!.latitude}, ${s.canonical!.longitude} · 版本 ${s.canonicalRevision}',
              ),
            if (s.canManage) ...[
              const SizedBox(height: 16),
              const Text(
                '新位置提案',
                style: TextStyle(fontWeight: FontWeight.w600),
              ),
              if (widget.cityID.isEmpty)
                const Text('请先选择实际城市才能提交或地图选点。已有提案仍可按当前权限审核。'),
              OutlinedButton(
                key: const Key('organization_pick_coordinate'),
                onPressed:
                    _op.busy ||
                        widget.cityID.isEmpty ||
                        _city == null ||
                        _city!.id != widget.cityID
                    ? null
                    : () => _pick(s, _cityIdentity),
                child: const Text('在地图选择 WGS84 坐标'),
              ),
              const Text('地图不支持可靠 WGS84 选点时，可手动填写已核对的授权坐标。取消选点不会改变本页草稿。'),
              TextField(
                key: const Key('organization_latitude'),
                controller: _latitude,
                decoration: const InputDecoration(labelText: '纬度（WGS84）'),
                keyboardType: const TextInputType.numberWithOptions(
                  decimal: true,
                  signed: true,
                ),
                onChanged: (_) => _edited(),
              ),
              TextField(
                key: const Key('organization_longitude'),
                controller: _longitude,
                decoration: const InputDecoration(labelText: '经度（WGS84）'),
                keyboardType: const TextInputType.numberWithOptions(
                  decimal: true,
                  signed: true,
                ),
                onChanged: (_) => _edited(),
              ),
              FilledButton(
                key: const Key('organization_preview_point'),
                onPressed: _op.canWrite && widget.cityID.isNotEmpty
                    ? () => _action('PROPOSE', source: s)
                    : null,
                child: const Text('检查位置提案'),
              ),
              if (s.canonical?.visibility == 'public')
                TextButton(
                  key: const Key('organization_hide_point'),
                  onPressed: _op.canWrite
                      ? () => _action('HIDE_POINT', source: s)
                      : null,
                  child: const Text('隐藏原公开点'),
                ),
            ],
            if (s.proposals.isNotEmpty)
              const Padding(
                padding: EdgeInsets.only(top: 16),
                child: Text('位置提案'),
              ),
            for (var i = 0; i < s.proposals.length; i++)
              Column(
                key: Key('organization_proposal_${s.proposals[i].id}'),
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    '提案 ${i + 1} · ${switch (s.proposals[i].status) {
                      'PENDING' => '待审核',
                      'APPROVED' => '已批准',
                      'REJECTED' => '已拒绝',
                      _ => '已撤回',
                    }}',
                  ),
                  Text(
                    'WGS84：${s.proposals[i].latitude}, ${s.proposals[i].longitude} · 提案版本 ${s.proposals[i].version}',
                  ),
                  Wrap(
                    children: [
                      if (s.proposals[i].canApprove)
                        TextButton(
                          key: Key('organization_approve_${s.proposals[i].id}'),
                          onPressed: _op.canWrite
                              ? () => _action(
                                  'APPROVE_POINT',
                                  proposal: s.proposals[i],
                                  source: s,
                                )
                              : null,
                          child: const Text('检查批准位置'),
                        ),
                      if (s.proposals[i].canReject)
                        TextButton(
                          onPressed: _op.canWrite
                              ? () => _action(
                                  'REJECT_POINT',
                                  proposal: s.proposals[i],
                                  source: s,
                                )
                              : null,
                          child: const Text('检查拒绝提案'),
                        ),
                      if (s.proposals[i].canWithdraw)
                        TextButton(
                          onPressed: _op.canWrite
                              ? () => _action(
                                  'WITHDRAW_POINT',
                                  proposal: s.proposals[i],
                                  source: s,
                                )
                              : null,
                          child: const Text('检查撤回提案'),
                        ),
                    ],
                  ),
                ],
              ),
          ],
        ],
      ),
    );
  }
}
