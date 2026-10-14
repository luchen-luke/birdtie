import 'dart:convert';
import 'dart:async';

import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import 'package:url_launcher/url_launcher.dart';

import '../app/birdtie_app_bar.dart';
import '../city/public_city_controller.dart';
import 'support_page.dart';
import 'human_causal_operation_controller.dart';
import '../config/birdtie_environment.dart';
import 'connections.dart';
import 'follow_button.dart';
import 'supplier_profile_api.dart';
import 'entity_action_contract.dart';
import 'entity_action_dispatcher.dart';
import 'entity_cover.dart';

class PublicOrganization {
  const PublicOrganization({
    required this.id,
    required this.name,
    required this.organizationType,
    required this.description,
    required this.verificationStatus,
    required this.agentAvailable,
    required this.officialLinks,
    required this.upcomingActivities,
    this.cover,
  });

  final String id;
  final String name;
  final String organizationType;
  final String description;
  final String verificationStatus;
  final bool agentAvailable;
  final List<String> officialLinks;
  final List<PublicActivity> upcomingActivities;
  final EntityCoverMetadata? cover;

  factory PublicOrganization.fromJson(Map<String, dynamic> data) {
    if (data['id'] is! String ||
        data['name'] is! String ||
        data['upcomingActivities'] is! List ||
        (data['upcomingActivities'] as List).length > 20 ||
        !const {
          'unverified',
          'pending',
          'rejected',
          'verified',
        }.contains(data['verificationStatus'])) {
      throw const FormatException('Invalid organization profile');
    }
    for (final v in data['upcomingActivities'] as List) {
      if (v is! Map<String, dynamic> ||
          v['visibility'] != 'public' ||
          v['organizer'] is! Map<String, dynamic> ||
          (v['organizer'] as Map)['type'] != 'ORGANIZATION' ||
          (v['organizer'] as Map)['id'] != data['id']) {
        throw const FormatException('Invalid public organization activity');
      }
    }
    return PublicOrganization(
      id: data['id'] as String,
      cover: EntityCoverMetadata.optional(
        data['cover'],
        EntityCoverRef('organization', data['id'] as String),
      ),
      name: data['name'] as String,
      organizationType: data['organizationType'] as String? ?? 'other',
      description: data['description'] as String? ?? '',
      verificationStatus: data['verificationStatus'] as String? ?? 'unverified',
      agentAvailable: data['agentAvailable'] as bool? ?? false,
      officialLinks: [
        for (final link in data['officialLinks'] as List<dynamic>? ?? const [])
          if (link is String) link,
      ],
      upcomingActivities: [
        for (final activity
            in data['upcomingActivities'] as List<dynamic>? ?? const [])
          PublicActivity.fromJson(activity as Map<String, dynamic>),
      ],
    );
  }
}

class PublicOrganizationPage extends StatefulWidget {
  const PublicOrganizationPage({
    super.key,
    required this.organizationID,
    required this.authorizationHeader,
    required this.onOpenActivity,
    this.apiBaseUrl,
    this.client,
    this.openExternal,
    this.workspaceID,
    this.identityChanges,
    this.ownerID,
    this.onOpenMapLocation,
  });

  final String organizationID;
  final String? Function() authorizationHeader;
  final ValueChanged<PublicActivity> onOpenActivity;
  final String? apiBaseUrl;
  final http.Client? client;
  final Future<bool> Function(Uri)? openExternal;
  final String? Function()? workspaceID;
  final Listenable? identityChanges;
  final String? Function()? ownerID;
  final ValueChanged<String>? onOpenMapLocation;

  @override
  State<PublicOrganizationPage> createState() => _PublicOrganizationPageState();
}

class _PublicOrganizationPageState extends State<PublicOrganizationPage> {
  static const _configuredBase = BirdtieEnvironment.apiBaseUrl;
  late http.Client _client = widget.client ?? http.Client();
  bool _ownsClient = false;
  final TextEditingController _question = TextEditingController();
  PublicOrganization? _organization;
  bool _loading = true;
  bool _asking = false;
  String? _error;
  String? _askError;
  String? _answer;
  String? _answerStatus;
  List<Map<String, dynamic>> _sources = const [];
  int _epoch = 0;
  final _reportSource = HumanCausalSourceSerial();
  void _observeReportSource() {
    _reportSource.observe((
      widget.organizationID,
      widget.client,
      widget.apiBaseUrl,
      widget.ownerID?.call(),
      widget.workspaceID?.call(),
    ));
    _reportSource.notifyReadBindingChanged();
  }

  late (String?, String?, String?) _identity;
  (String?, String?, String?) get _current => (
    widget.authorizationHeader(),
    widget.workspaceID?.call(),
    widget.ownerID?.call(),
  );
  bool _mapControlAvailable = false;
  DateTime? _mapControlExpiresAt;
  Timer? _mapControlExpiry;
  void _retireMapControl() {
    _mapControlExpiry?.cancel();
    _mapControlAvailable = false;
    _mapControlExpiresAt = null;
  }

  @override
  void initState() {
    super.initState();
    _ownsClient = widget.client == null;
    _identity = _current;
    _observeReportSource();
    widget.identityChanges?.addListener(_identityChanged);
    _load();
  }

  @override
  void dispose() {
    _reportSource.dispose();
    _epoch++;
    _retireMapControl();
    widget.identityChanges?.removeListener(_identityChanged);
    _question.dispose();
    if (_ownsClient) _client.close();
    super.dispose();
  }

  void _identityChanged() {
    _observeReportSource();
    if (_identity == _current) return;
    _identity = _current;
    _epoch++;
    _retireMapControl();
    _organization = null;
    _answer = null;
    _sources = const [];
    _asking = false;
    _question.clear();
    _load();
  }

  @override
  void didUpdateWidget(PublicOrganizationPage old) {
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
    if (transportChanged ||
        !identical(old.authorizationHeader, widget.authorizationHeader) ||
        !identical(old.workspaceID, widget.workspaceID) ||
        !identical(old.ownerID, widget.ownerID) ||
        !identical(old.identityChanges, widget.identityChanges) ||
        old.organizationID != widget.organizationID ||
        _identity != _current) {
      _identity = _current;
      _question.clear();
      _load();
    }
  }

  Future<void> _ask() async {
    final query = _question.text.trim();
    if (query.runes.length < 2 || query.runes.length > 240 || _asking) return;
    final base = widget.apiBaseUrl ?? _configuredBase;
    final epoch = _epoch, identity = _current;
    setState(() {
      _asking = true;
      _askError = null;
    });
    try {
      final response = await _client
          .post(
            Uri.parse(
              '${base.replaceFirst(RegExp(r'/$'), '')}/v1/organizations/${Uri.encodeComponent(widget.organizationID)}/agent/ask',
            ),
            headers: {
              'Authorization': ?widget.authorizationHeader(),
              'Content-Type': 'application/json',
            },
            body: jsonEncode({'query': query}),
          )
          .timeout(const Duration(seconds: 12));
      if (response.statusCode != 200) {
        throw StateError('Agent answer unavailable');
      }
      final data =
          (jsonDecode(response.body) as Map<String, dynamic>)['data']
              as Map<String, dynamic>;
      if (mounted && epoch == _epoch && identity == _current) {
        setState(() {
          _answer = data['answer'] as String?;
          _answerStatus = data['status'] as String?;
          _sources = [
            for (final source in data['sources'] as List<dynamic>? ?? const [])
              source as Map<String, dynamic>,
          ];
        });
      }
    } catch (_) {
      if (mounted && epoch == _epoch && identity == _current) {
        setState(() => _askError = '组织 Agent 暂时无法回答，请重试。');
      }
    } finally {
      if (mounted && epoch == _epoch && identity == _current) {
        setState(() => _asking = false);
      }
    }
  }

  Future<void> _load() async {
    _retireMapControl();
    final epoch = ++_epoch, identity = _current;
    final base = widget.apiBaseUrl ?? _configuredBase;
    if (base.isEmpty) {
      setState(() {
        _loading = false;
        _error = '组织资料暂不可用，请连接 Birdtie 服务。';
      });
      return;
    }
    setState(() {
      _loading = true;
      _error = null;
      _organization = null;
      _answer = null;
      _sources = const [];
      _asking = false;
    });
    try {
      final response = await _client
          .get(
            Uri.parse(
              '${base.replaceFirst(RegExp(r'/$'), '')}/v1/organizations/${Uri.encodeComponent(widget.organizationID)}',
            ),
            headers: {'Authorization': ?widget.authorizationHeader()},
          )
          .timeout(const Duration(seconds: 12));
      if (response.statusCode != 200) {
        throw StateError('Organization unavailable');
      }
      final organization = PublicOrganization.fromJson(
        (jsonDecode(response.body) as Map<String, dynamic>)['data']
            as Map<String, dynamic>,
      );
      if (organization.id != widget.organizationID) {
        throw const FormatException('Organization changed');
      }
      if (mounted && epoch == _epoch && identity == _current) {
        setState(() {
          _organization = organization;
          _loading = false;
        });
        await _loadMapControl(epoch, identity);
      }
    } catch (_) {
      if (mounted && epoch == _epoch && identity == _current) {
        setState(() {
          _loading = false;
          _error = '组织资料加载失败，请重试。';
        });
      }
    }
  }

  Future<void> _loadMapControl(
    int epoch,
    (String?, String?, String?) identity,
  ) async {
    // This is the existing human authority, independent of Organization Agent
    // availability and membership. Only actual eligible proposals grant review.
    if (widget.onOpenMapLocation == null ||
        identity.$1 == null ||
        identity.$2 != null ||
        identity.$3 == null)
      return;
    try {
      final base = widget.apiBaseUrl ?? _configuredBase;
      final response = await _client
          .get(
            Uri.parse(
              '${base.replaceFirst(RegExp(r'/$'), '')}/v1/me/organizations/'
              '${Uri.encodeComponent(widget.organizationID)}/map-location/control',
            ),
            headers: {'Authorization': identity.$1!},
          )
          .timeout(const Duration(seconds: 10));
      if (!mounted ||
          epoch != _epoch ||
          identity != _current ||
          response.statusCode != 200)
        return;
      final data =
          (jsonDecode(response.body) as Map<String, dynamic>)['data']
              as Map<String, dynamic>;
      final observed = DateTime.parse(data['observedAt'] as String).toUtc();
      final expires = DateTime.parse(data['expiresAt'] as String).toUtc();
      final proposals = data['proposals'] as List;
      if (data['organizationId'] != widget.organizationID ||
          data['authorityVersion'] is! int ||
          data['authorityVersion'] < 0 ||
          !expires.isAfter(observed) ||
          expires.difference(observed) > const Duration(seconds: 30) ||
          !expires.isAfter(DateTime.now().toUtc()) ||
          proposals.length > 100)
        return;
      final eligible =
          const {'owner', 'admin'}.contains(data['role']) ||
          proposals.any(
            (p) =>
                p is Map &&
                p['organizationId'] == widget.organizationID &&
                p['status'] == 'PENDING' &&
                (p['canApprove'] == true || p['canReject'] == true),
          );
      if (!eligible) return;
      setState(() {
        _mapControlAvailable = true;
        _mapControlExpiresAt = expires;
      });
      _mapControlExpiry = Timer(expires.difference(DateTime.now().toUtc()), () {
        if (mounted && epoch == _epoch && identity == _current)
          setState(_retireMapControl);
      });
    } catch (_) {
      // Management availability does not replace a valid public profile or
      // infer a role when the private authority cannot be read.
    }
  }

  Future<void> _openLink(String value) async {
    final uri = supplierHTTPS(value);
    if (uri == null) return;
    final epoch = _epoch, identity = _current;
    try {
      final base = widget.apiBaseUrl ?? _configuredBase;
      final response = await _client
          .get(
            Uri.parse(
              '${base.replaceFirst(RegExp(r'/$'), '')}/v1/organizations/${Uri.encodeComponent(widget.organizationID)}',
            ),
            headers: {'Authorization': ?widget.authorizationHeader()},
          )
          .timeout(const Duration(seconds: 12));
      if (!mounted || epoch != _epoch || identity != _current) return;
      if (response.statusCode != 200) throw StateError('source unavailable');
      final current = PublicOrganization.fromJson(
        (jsonDecode(response.body) as Map<String, dynamic>)['data']
            as Map<String, dynamic>,
      );
      if (current.id != widget.organizationID) {
        throw const FormatException('Organization changed');
      }
      if (!current.officialLinks.contains(value)) {
        setState(() {
          _organization = current;
          _error = '组织资料已变化，请重新检查链接。';
        });
        return;
      }
      final opened =
          await (widget.openExternal?.call(uri) ??
              launchUrl(uri, mode: LaunchMode.externalApplication));
      if (!opened && mounted && epoch == _epoch && identity == _current) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(const SnackBar(content: Text('链接暂时无法打开。')));
      }
    } catch (_) {
      if (mounted && epoch == _epoch && identity == _current) {
        setState(() {
          _organization = null;
          _error = '暂时无法核实组织链接，请重新读取。';
        });
      }
      return;
    }
  }

  Future<void> _shareToFriend() async {
    final epoch = _epoch, identity = _current, id = widget.organizationID;
    bool current() =>
        mounted &&
        epoch == _epoch &&
        identity == _current &&
        id == widget.organizationID;
    await runEntityAction(
      context,
      ref: EntityActionRef('organization', id),
      kind: EntityActionKind.share,
      authorizationHeader: () => current() ? identity.$1 : null,
      workspaceID: widget.workspaceID,
      identityChanges: widget.identityChanges,
      client: _client,
      apiBaseUrl: widget.apiBaseUrl,
      domainCurrent: current,
      handler: (_) async {
        if (!current()) return;
        await shareEntityToChat(
          context,
          authorizationHeader: () => widget.authorizationHeader(),
          identityChanges: _reportSource,
          ownerID: widget.ownerID == null ? null : () => widget.ownerID?.call(),
          workspaceID: () => widget.workspaceID?.call(),
          sourceKey: () => _reportSource.generation,
          sourceCurrent: () => mounted,
          type: 'organization',
          id: id,
          apiBaseUrl: widget.apiBaseUrl,
          client: _client,
        );
      },
    );
  }

  static String _typeLabel(String type) => switch (type) {
    'student_society' => '学生社团',
    'club' => '俱乐部',
    'business' => '商家',
    'university' => '大学',
    'community' => '社区组织',
    'venue' => '场地机构',
    'nonprofit' => '非营利组织',
    _ => '组织',
  };

  static String _verificationLabel(String status) => switch (status) {
    'verified' => '已认证组织',
    'pending' => '身份认证审核中',
    'rejected' => '身份认证未通过',
    _ => '身份未认证',
  };

  @override
  Widget build(BuildContext context) {
    final organization = _organization;
    final coverEpoch = _epoch, coverIdentity = _current;
    bool coverCurrent() =>
        mounted &&
        !_loading &&
        _error == null &&
        coverEpoch == _epoch &&
        coverIdentity == _current &&
        identical(organization, _organization);
    final coverAPI = EntityCoverAPI(
      client: _client,
      baseUrl: widget.apiBaseUrl ?? _configuredBase,
      authorizationHeader: widget.authorizationHeader,
      current: coverCurrent,
    );
    return Scaffold(
      appBar: birdtieAppBar(context, title: const Text('组织资料')),
      body: _loading
          ? const Center(child: CircularProgressIndicator())
          : _error != null
          ? Center(
              child: TextButton(onPressed: _load, child: Text(_error!)),
            )
          : organization == null
          ? const Center(child: Text('没有找到这个组织。'))
          : RefreshIndicator(
              onRefresh: _load,
              child: ListView(
                physics: const AlwaysScrollableScrollPhysics(),
                padding: const EdgeInsets.fromLTRB(24, 24, 24, 40),
                children: [
                  if (organization.cover != null &&
                      organization.cover!.ref ==
                          EntityCoverRef('organization', organization.id))
                    EntityCover(
                      key: ObjectKey(organization),
                      metadata: organization.cover,
                      api: coverAPI,
                      identityChanges: widget.identityChanges,
                      current: coverCurrent,
                      width: 320,
                      height: 180,
                      label: '${organization.name}的组织封面',
                    ),
                  Text(
                    organization.name,
                    style: Theme.of(context).textTheme.headlineSmall,
                  ),
                  FollowButton(
                    targetType: 'ORGANIZATION',
                    targetID: widget.organizationID,
                    identityChanges: widget.identityChanges,
                    workspaceID: widget.workspaceID,
                    authorizationHeader: widget.authorizationHeader,
                    apiBaseUrl: widget.apiBaseUrl,
                    client: widget.client,
                  ),
                  if (organization.verificationStatus == 'verified')
                    TextButton.icon(
                      onPressed: _shareToFriend,
                      icon: const Icon(Icons.chat_bubble_outline),
                      label: const Text('发给好友'),
                    ),
                  if (_mapControlAvailable && widget.onOpenMapLocation != null)
                    TextButton.icon(
                      onPressed: () {
                        if (coverCurrent() &&
                            _mapControlAvailable &&
                            _mapControlExpiresAt?.isAfter(
                                  DateTime.now().toUtc(),
                                ) ==
                                true &&
                            widget.ownerID?.call() == coverIdentity.$3 &&
                            widget.workspaceID?.call() == null)
                          widget.onOpenMapLocation!(organization.id);
                      },
                      icon: const Icon(Icons.edit_location_alt_outlined),
                      label: const Text('地图位置管理与审核'),
                    ),
                  const SizedBox(height: 10),
                  Text(
                    '${_typeLabel(organization.organizationType)} · ${_verificationLabel(organization.verificationStatus)}',
                  ),
                  const SizedBox(height: 8),
                  Text(
                    organization.verificationStatus == 'verified'
                        ? '组织身份已通过 Birdtie 核验；不表示活动质量、交易安全或平台推荐背书。'
                        : '这个组织的身份尚未通过 Birdtie 核验，请自行核对活动信息。',
                    style: Theme.of(context).textTheme.bodySmall,
                  ),
                  if (organization.description.isNotEmpty) ...[
                    const SizedBox(height: 24),
                    Text(organization.description),
                  ],
                  if (organization.officialLinks.isNotEmpty) ...[
                    const SizedBox(height: 28),
                    Text(
                      '组织提供的链接',
                      style: Theme.of(context).textTheme.titleMedium,
                    ),
                    for (final link in organization.officialLinks)
                      if (supplierHTTPS(link) != null)
                        TextButton.icon(
                          onPressed: () => _openLink(link),
                          icon: const Icon(Icons.open_in_new),
                          label: Text(
                            link,
                            maxLines: 2,
                            overflow: TextOverflow.ellipsis,
                          ),
                        ),
                  ],
                  const SizedBox(height: 28),
                  if (organization.verificationStatus == 'verified' &&
                      organization.agentAvailable) ...[
                    Text(
                      '询问组织 Agent',
                      style: Theme.of(context).textTheme.titleLarge,
                    ),
                    const SizedBox(height: 6),
                    const Text('回答仅引用该组织已发布的问答与公开资料；没有依据时会明确说明。'),
                    const SizedBox(height: 10),
                    TextField(
                      controller: _question,
                      maxLength: 240,
                      textInputAction: TextInputAction.send,
                      onSubmitted: (_) => _ask(),
                      decoration: const InputDecoration(hintText: '例如：如何报名？'),
                    ),
                    FilledButton(
                      onPressed: _asking ? null : _ask,
                      child: Text(_asking ? '正在查找依据…' : '提问'),
                    ),
                    if (_askError != null) Text(_askError!),
                    if (_answer != null) ...[
                      const SizedBox(height: 12),
                      Text(_answerStatus == 'known' ? '组织 Agent 回答' : '目前无法确认'),
                      Text(_answer!),
                      for (final source in _sources)
                        Text('依据：${source['label'] as String? ?? '公开资料'}'),
                    ],
                  ] else
                    Text(
                      organization.verificationStatus != 'verified'
                          ? '组织 Agent 暂不可用：组织身份尚未通过核验。'
                          : '组织 Agent 暂不可用。',
                    ),
                  const SizedBox(height: 28),
                  TextButton.icon(
                    onPressed: () => Navigator.of(context).push(
                      MaterialPageRoute<void>(
                        builder: (_) => humanSupportDestination(
                          authorizationHeader: widget.authorizationHeader,
                          apiBaseUrl: widget.apiBaseUrl,
                          targetType: 'organization',
                          targetID: organization.id,
                          ownerID: widget.ownerID,
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
                    label: const Text('举报组织信息'),
                  ),
                  const SizedBox(height: 12),
                  Text(
                    '即将举行的活动',
                    style: Theme.of(context).textTheme.titleLarge,
                  ),
                  const SizedBox(height: 8),
                  if (organization.upcomingActivities.isEmpty)
                    const Text('目前没有即将举行的公开活动。')
                  else
                    for (final activity in organization.upcomingActivities)
                      ListTile(
                        contentPadding: EdgeInsets.zero,
                        leading:
                            activity.cover == null ||
                                activity.cover!.ref !=
                                    EntityCoverRef('activity', activity.id)
                            ? null
                            : EntityCover(
                                key: ObjectKey(activity),
                                metadata: activity.cover,
                                api: coverAPI,
                                identityChanges: widget.identityChanges,
                                current: coverCurrent,
                                width: 80,
                                height: 45,
                                showAttribution: false,
                                label: '${activity.title}的活动封面',
                              ),
                        title: Text(activity.title),
                        subtitle: Text(
                          '${activity.schedule} · ${activity.placeName.isEmpty ? '地点待公布' : activity.placeName}',
                        ),
                        trailing: const Icon(Icons.chevron_right),
                        onTap: () => widget.onOpenActivity(activity),
                      ),
                ],
              ),
            ),
    );
  }
}
