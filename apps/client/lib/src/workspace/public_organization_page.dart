import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import 'package:url_launcher/url_launcher.dart';

import '../city/public_city_controller.dart';
import 'support_page.dart';
import '../config/birdtie_environment.dart';
import 'connections.dart';
import 'follow_button.dart';
import 'supplier_profile_api.dart';
import 'entity_action_contract.dart';
import 'entity_action_dispatcher.dart';

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
  });

  final String id;
  final String name;
  final String organizationType;
  final String description;
  final String verificationStatus;
  final bool agentAvailable;
  final List<String> officialLinks;
  final List<PublicActivity> upcomingActivities;

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
  });

  final String organizationID;
  final String? Function() authorizationHeader;
  final ValueChanged<PublicActivity> onOpenActivity;
  final String? apiBaseUrl;
  final http.Client? client;
  final Future<bool> Function(Uri)? openExternal;
  final String? Function()? workspaceID;
  final Listenable? identityChanges;

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
  late (String?, String?) _identity;
  (String?, String?) get _current =>
      (widget.authorizationHeader(), widget.workspaceID?.call());

  @override
  void initState() {
    super.initState();
    _ownsClient = widget.client == null;
    _identity = _current;
    widget.identityChanges?.addListener(_identityChanged);
    _load();
  }

  @override
  void dispose() {
    _epoch++;
    widget.identityChanges?.removeListener(_identityChanged);
    _question.dispose();
    if (_ownsClient) _client.close();
    super.dispose();
  }

  void _identityChanged() {
    if (_identity == _current) return;
    _identity = _current;
    _epoch++;
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
          authorizationHeader: () => current() ? identity.$1 : null,
          identityChanges: widget.identityChanges,
          workspaceID: widget.workspaceID,
          client: _client,
          type: 'organization',
          id: id,
          apiBaseUrl: widget.apiBaseUrl,
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
    return Scaffold(
      appBar: AppBar(title: const Text('组织资料')),
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
                  Text(
                    organization.name,
                    style: Theme.of(context).textTheme.headlineMedium?.copyWith(
                      fontWeight: FontWeight.w700,
                      color: const Color(0xFF193B32),
                    ),
                  ),
                  FollowButton(
                    targetType: 'ORGANIZATION',
                    targetID: widget.organizationID,
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
                  const SizedBox(height: 10),
                  Text(
                    '${_typeLabel(organization.organizationType)} · ${_verificationLabel(organization.verificationStatus)}',
                  ),
                  const SizedBox(height: 8),
                  Text(
                    organization.verificationStatus == 'verified'
                        ? '组织身份已通过 Birdtie 核验；不表示活动质量、交易安全或平台推荐背书。'
                        : '这个组织的身份尚未通过 Birdtie 核验，请自行核对活动信息。',
                    style: const TextStyle(color: Color(0xFF5A695F)),
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
                        builder: (_) => SupportPage(
                          authorizationHeader: widget.authorizationHeader,
                          apiBaseUrl: widget.apiBaseUrl,
                          targetType: 'organization',
                          targetID: organization.id,
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
