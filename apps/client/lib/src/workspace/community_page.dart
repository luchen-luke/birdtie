import 'package:flutter/material.dart';

import '../auth/birdtie_auth_controller.dart';
import '../city/public_city_controller.dart';
import 'community_api.dart';
import 'organization_activity_api.dart';
import 'organization_console.dart';
import 'support_page.dart';
import 'organization_workspaces.dart';
import 'activity_detail_sheet.dart';
import 'connections.dart';
import 'entity_action_contract.dart';
import 'entity_action_dispatcher.dart';
import 'follow_button.dart';
import 'community_conversation_page.dart';
import 'community_members_controller.dart';
import 'community_member_management_sheet.dart';

String communityVisibilityLabel(String value) => switch (value) {
  'public' => '公开',
  'private' => '私密',
  _ => '隐藏',
};

String communityJoinLabel(String value) => switch (value) {
  'open' => '直接加入',
  'request' => '申请加入',
  _ => '仅限邀请',
};

class CommunityPage extends StatefulWidget {
  const CommunityPage({
    super.key,
    required this.auth,
    required this.city,
    this.organizations,
    this.api,
  });

  final BirdtieAuthController auth;
  final PublicCityController city;
  final OrganizationWorkspaceController? organizations;
  final CommunityApi? api;

  @override
  State<CommunityPage> createState() => _CommunityPageState();
}

class _CommunityPageState extends State<CommunityPage> {
  late final CommunityApi _api =
      widget.api ??
      CommunityApi(authorizationHeader: () => widget.auth.authorizationHeader);
  List<CommunityItem> _mine = const [];
  List<CommunityItem> _discover = const [];
  bool _loading = false;
  bool _failed = false;
  bool _working = false;
  int _serial = 0;
  int _authorityGeneration = 0;
  String? _seenToken, _seenCity;

  @override
  void initState() {
    super.initState();
    _seenToken = widget.auth.authorizationHeader;
    _seenCity = widget.city.selectedCity?.id;
    widget.auth.addListener(_authChanged);
    widget.city.addListener(_cityChanged);
    if (widget.auth.signedIn) _load();
  }

  void _authChanged() {
    if (!mounted || _seenToken == widget.auth.authorizationHeader) return;
    _seenToken = widget.auth.authorizationHeader;
    ++_serial;
    ++_authorityGeneration;
    setState(() {
      _mine = const [];
      _discover = const [];
      _working = false;
    });
    if (widget.auth.signedIn) {
      _load();
    } else {
      setState(() {
        _mine = const [];
        _discover = const [];
        _loading = false;
        _failed = false;
      });
    }
  }

  void _cityChanged() {
    final city = widget.city.selectedCity?.id;
    if (city == _seenCity) return;
    _seenCity = city;
    if (mounted && widget.auth.signedIn) {
      _load();
    }
  }

  Future<void> _load() async {
    final serial = ++_serial;
    setState(() {
      _loading = true;
      _failed = false;
    });
    try {
      final mine = await _api.mine();
      final discover = await _api.discover(
        cityId: widget.city.selectedCity?.id,
      );
      if (!mounted || serial != _serial) return;
      setState(() {
        _mine = mine;
        _discover = discover;
      });
    } catch (_) {
      if (mounted && serial == _serial) setState(() => _failed = true);
    } finally {
      if (mounted && serial == _serial) setState(() => _loading = false);
    }
  }

  Future<void> _create() async {
    final captured = widget.auth.authorizationHeader;
    final authorityGeneration = _authorityGeneration;
    final input = await showDialog<_CommunityForm>(
      useRootNavigator: false,
      context: context,
      builder: (context) =>
          _CreateCommunityDialog(cityId: widget.city.selectedCity?.id),
    );
    if (input == null ||
        !mounted ||
        captured == null ||
        captured != widget.auth.authorizationHeader ||
        authorityGeneration != _authorityGeneration) {
      return;
    }
    setState(() => _working = true);
    try {
      final created = await _api.create(
        name: input.name,
        description: input.description,
        visibility: input.visibility,
        joinPolicy: input.joinPolicy,
        cityId: input.cityId,
        avatarUrl: input.avatarUrl,
      );
      if (!mounted ||
          captured != widget.auth.authorizationHeader ||
          authorityGeneration != _authorityGeneration) {
        return;
      }
      await _load();
      if (!mounted ||
          captured != widget.auth.authorizationHeader ||
          authorityGeneration != _authorityGeneration) {
        return;
      }
      await _open(created);
    } catch (error) {
      if (mounted &&
          captured == widget.auth.authorizationHeader &&
          authorityGeneration == _authorityGeneration) {
        _message(error);
      }
    } finally {
      if (mounted &&
          captured == widget.auth.authorizationHeader &&
          authorityGeneration == _authorityGeneration) {
        setState(() => _working = false);
      }
    }
  }

  void _message(Object error) {
    final text = error is CommunityApiException
        ? error.message
        : '网络暂不可用，请稍后重试。';
    ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(text)));
  }

  Future<void> _open(CommunityItem item) async {
    await Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (context) => CommunityDetailPage(
          id: item.id,
          api: _api,
          auth: widget.auth,
          city: widget.city,
          organizations: widget.organizations,
        ),
      ),
    );
    if (mounted && widget.auth.signedIn) _load();
  }

  @override
  void dispose() {
    ++_serial;
    widget.auth.removeListener(_authChanged);
    widget.city.removeListener(_cityChanged);
    if (widget.api == null) _api.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    if (!widget.auth.signedIn) {
      return const Center(child: Text('请先登录，再查看和加入社群。'));
    }
    final managed = _mine.where((item) => item.managed).toList();
    final joined = _mine.where((item) => item.joined && !item.managed).toList();
    final pending = _mine
        .where(
          (item) => item.myStatus == 'pending' || item.myStatus == 'invited',
        )
        .toList();
    return RefreshIndicator(
      onRefresh: _load,
      child: ListView(
        padding: const EdgeInsets.all(20),
        children: [
          Row(
            children: [
              const Expanded(
                child: Text(
                  '我的社群',
                  style: TextStyle(fontSize: 22, fontWeight: FontWeight.w700),
                ),
              ),
              FilledButton.icon(
                onPressed: _working ? null : _create,
                icon: const Icon(Icons.add),
                label: const Text('创建社群'),
              ),
            ],
          ),
          const SizedBox(height: 6),
          const Text('和同城的人持续相聚，活动结束后关系仍在。'),
          if (_loading)
            const Padding(
              padding: EdgeInsets.only(top: 16),
              child: LinearProgressIndicator(),
            ),
          if (_failed)
            Padding(
              padding: const EdgeInsets.only(top: 16),
              child: TextButton(
                onPressed: _load,
                child: const Text('加载失败，点击重试'),
              ),
            ),
          if (!_loading && !_failed) ...[
            _section('我管理的', managed, empty: '你还没有管理的社群。'),
            _section('我加入的', joined, empty: '你还没有加入社群。'),
            _section('待处理', pending, empty: '没有待处理的申请或邀请。'),
            const Divider(height: 32),
            _section('发现社群', _discover, empty: '目前还没有可发现的社群。'),
          ],
        ],
      ),
    );
  }

  Widget _section(
    String title,
    List<CommunityItem> items, {
    required String empty,
  }) => Column(
    crossAxisAlignment: CrossAxisAlignment.start,
    children: [
      const SizedBox(height: 24),
      Text(
        title,
        style: const TextStyle(fontSize: 18, fontWeight: FontWeight.w700),
      ),
      const SizedBox(height: 8),
      if (items.isEmpty)
        Padding(
          padding: const EdgeInsets.symmetric(vertical: 10),
          child: Text(empty, style: const TextStyle(color: Colors.grey)),
        )
      else
        for (final item in items)
          ListTile(
            contentPadding: EdgeInsets.zero,
            leading: CircleAvatar(child: Text(item.name.characters.first)),
            title: Text(item.name),
            subtitle: Text(
              '${communityVisibilityLabel(item.visibility)} · '
              '${item.memberCount} 位成员 · ${communityJoinLabel(item.joinPolicy)}',
            ),
            trailing: const Icon(Icons.chevron_right),
            onTap: () => _open(item),
          ),
    ],
  );
}

class _CommunityForm {
  const _CommunityForm({
    required this.name,
    required this.description,
    required this.avatarUrl,
    required this.visibility,
    required this.joinPolicy,
    required this.cityId,
  });
  final String name;
  final String description;
  final String avatarUrl;
  final String visibility;
  final String joinPolicy;
  final String cityId;
}

class _CreateCommunityDialog extends StatefulWidget {
  const _CreateCommunityDialog({this.cityId});
  final String? cityId;
  @override
  State<_CreateCommunityDialog> createState() => _CreateCommunityDialogState();
}

class _CreateCommunityDialogState extends State<_CreateCommunityDialog> {
  final _name = TextEditingController();
  final _description = TextEditingController();
  final _avatar = TextEditingController();
  String _visibility = 'public';
  String _joinPolicy = 'request';
  bool _useCity = true;

  @override
  void dispose() {
    _name.dispose();
    _description.dispose();
    _avatar.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: const Text('创建社群'),
    content: SizedBox(
      width: 440,
      child: SingleChildScrollView(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            TextField(
              controller: _name,
              maxLength: 160,
              decoration: const InputDecoration(labelText: '社群名称'),
            ),
            TextField(
              controller: _description,
              maxLength: 3000,
              maxLines: 3,
              decoration: const InputDecoration(labelText: '介绍'),
            ),
            TextField(
              controller: _avatar,
              decoration: const InputDecoration(labelText: '头像 HTTPS 链接（选填）'),
            ),
            DropdownButtonFormField<String>(
              initialValue: _visibility,
              decoration: const InputDecoration(labelText: '可见范围'),
              items: const [
                DropdownMenuItem(value: 'public', child: Text('公开')),
                DropdownMenuItem(value: 'private', child: Text('私密：可发现，详情有限')),
                DropdownMenuItem(value: 'hidden', child: Text('隐藏：仅通过链接或邀请')),
              ],
              onChanged: (value) => setState(() => _visibility = value!),
            ),
            DropdownButtonFormField<String>(
              initialValue: _joinPolicy,
              decoration: const InputDecoration(labelText: '加入方式'),
              items: const [
                DropdownMenuItem(value: 'open', child: Text('直接加入')),
                DropdownMenuItem(value: 'request', child: Text('申请加入')),
                DropdownMenuItem(value: 'invite_only', child: Text('仅限邀请')),
              ],
              onChanged: (value) => setState(() => _joinPolicy = value!),
            ),
            if (widget.cityId != null)
              SwitchListTile(
                contentPadding: EdgeInsets.zero,
                title: const Text('关联当前城市'),
                value: _useCity,
                onChanged: (value) => setState(() => _useCity = value),
              ),
          ],
        ),
      ),
    ),
    actions: [
      TextButton(
        onPressed: () => Navigator.pop(context),
        child: const Text('取消'),
      ),
      FilledButton(
        onPressed: () {
          final name = _name.text.trim();
          if (name.characters.length < 2) {
            ScaffoldMessenger.of(
              context,
            ).showSnackBar(const SnackBar(content: Text('社群名称至少需要 2 个字。')));
            return;
          }
          Navigator.pop(
            context,
            _CommunityForm(
              name: name,
              description: _description.text.trim(),
              avatarUrl: _avatar.text.trim(),
              visibility: _visibility,
              joinPolicy: _joinPolicy,
              cityId: _useCity ? widget.cityId ?? '' : '',
            ),
          );
        },
        child: const Text('创建'),
      ),
    ],
  );
}

class CommunityDetailPage extends StatefulWidget {
  const CommunityDetailPage({
    super.key,
    required this.id,
    required this.api,
    this.auth,
    this.city,
    this.organizations,
  });
  final String id;
  final CommunityApi api;
  final BirdtieAuthController? auth;
  final PublicCityController? city;
  final OrganizationWorkspaceController? organizations;
  @override
  State<CommunityDetailPage> createState() => _CommunityDetailPageState();
}

class _CommunityDetailPageState extends State<CommunityDetailPage> {
  CommunityItem? _item;
  List<CommunityMember> _members = const [];
  List<CommunityMember> _requests = const [];
  bool _loading = true;
  bool _working = false;
  String? _error;
  int _serial = 0;
  int _authorityGeneration = 0;
  String? _authority;

  Future<void> _createActivity(CommunityItem community) async {
    final api = OrganizationActivityApi(
      authorizationHeader: () => widget.auth?.authorizationHeader,
    );
    try {
      final changed = await Navigator.of(context).push<bool>(
        MaterialPageRoute(
          builder: (_) => ActivityEditorPage(
            api: api,
            auth: widget.auth!,
            city: widget.city!,
            organizations: widget.organizations!,
            initialOrganizer: ActivityOrganizer(
              type: 'COMMUNITY',
              id: community.id,
              name: community.name,
            ),
          ),
        ),
      );
      if (changed == true && mounted) {
        await widget.city!.loadActivities();
        if (mounted) setState(() {});
      }
    } finally {
      api.dispose();
    }
  }

  @override
  void initState() {
    super.initState();
    _authority = widget.api.authorizationHeader();
    widget.auth?.addListener(_authorityChanged);
    _load();
  }

  void _authorityChanged() {
    final now = widget.api.authorizationHeader();
    if (now == _authority) return;
    _authority = now;
    ++_serial;
    ++_authorityGeneration;
    setState(() {
      _item = null;
      _members = const [];
      _requests = const [];
      _working = false;
      _loading = false;
      _error = '账号已变化，请重新读取社群状态。';
    });
  }

  @override
  void dispose() {
    widget.auth?.removeListener(_authorityChanged);
    super.dispose();
  }

  bool _current(int serial, String? token) =>
      mounted &&
      serial == _serial &&
      token != null &&
      token == widget.api.authorizationHeader();

  Future<void> _load() async {
    final serial = ++_serial, token = widget.api.authorizationHeader();
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final item = await widget.api.detail(widget.id);
      if (!_current(serial, token)) return;
      final members = item.joined
          ? await widget.api.members(widget.id)
          : <CommunityMember>[];
      if (!_current(serial, token)) return;
      final requests = item.managed
          ? await widget.api.members(widget.id, requests: true)
          : <CommunityMember>[];
      if (!_current(serial, token)) return;
      await widget.city?.loadActivities();
      if (_current(serial, token)) {
        setState(() {
          _item = item;
          _members = members;
          _requests = requests;
        });
      }
    } catch (error) {
      if (_current(serial, token)) {
        if (error is CommunityApiException &&
            {401, 403, 404}.contains(error.status)) {
          _item = null;
          _members = const [];
          _requests = const [];
        }
        setState(
          () => _error = error is CommunityApiException
              ? error.message
              : '网络暂不可用，请稍后重试。',
        );
      }
    } finally {
      if (_current(serial, token)) setState(() => _loading = false);
    }
  }

  Future<bool> _action(Future<void> Function() action) async {
    if (_working) return false;
    final serial = _serial,
        generation = _authorityGeneration,
        token = widget.api.authorizationHeader();
    setState(() => _working = true);
    try {
      await action();
      if (!_current(serial, token)) return false;
      await _load();
      return mounted &&
          generation == _authorityGeneration &&
          token == widget.api.authorizationHeader();
    } catch (error) {
      if (mounted && _current(serial, token)) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(
              error is CommunityApiException && error.status < 500
                  ? error.message
                  : '结果待核实，请刷新当前状态，勿重复提交。',
            ),
          ),
        );
        await _load();
      }
      return false;
    } finally {
      if (mounted &&
          generation == _authorityGeneration &&
          token == widget.api.authorizationHeader()) {
        setState(() => _working = false);
      }
    }
  }

  Future<void> _entityAction(
    EntityActionKind kind, {
    bool joining = true,
  }) async {
    final item = _item,
        api = widget.api,
        serial = _serial,
        token = api.authorizationHeader(),
        generation = _authorityGeneration;
    if (item == null) return;
    bool current() =>
        _current(serial, token) &&
        api == widget.api &&
        generation == _authorityGeneration;
    final changes = Listenable.merge([widget.auth, widget.organizations]);
    await runEntityAction(
      context,
      ref: EntityActionRef('community', item.id),
      kind: kind,
      operation: kind == EntityActionKind.join && !joining
          ? (item.myStatus == 'invited' ? 'DECLINE_INVITATION' : 'LEAVE')
          : null,
      authorizationHeader: () => current() ? token : null,
      accountID: () => widget.auth?.accountID,
      workspaceID: () => widget.organizations?.active?.id,
      identityChanges: changes,
      client: api.followClient,
      apiBaseUrl: api.followApiBaseUrl,
      domainCurrent: current,
      handler: (descriptor) async {
        if (!current()) return;
        if (kind == EntityActionKind.share) {
          await shareEntityToChat(
            context,
            authorizationHeader: () => current() ? token : null,
            type: 'community',
            id: item.id,
            identityChanges: changes,
            workspaceID: () => widget.organizations?.active?.id,
            client: api.followClient,
            apiBaseUrl: api.followApiBaseUrl,
          );
          return;
        }
        if (kind == EntityActionKind.join) {
          if (joining &&
                  !const {
                    'JOIN',
                    'REQUEST_JOIN',
                    'ACCEPT_INVITATION',
                  }.contains(descriptor.operation) ||
              !joining &&
                  !const {
                    'LEAVE',
                    'DECLINE_INVITATION',
                  }.contains(descriptor.operation)) {
            throw StateError('加入状态已变化，请重新读取。');
          }
          await _action(
            () => joining
                ? api.join(item.id, approved: descriptor)
                : api.leave(item.id, approved: descriptor),
          );
        }
      },
    );
  }

  Future<void> _manage(CommunityItem item) async {
    final auth = widget.auth;
    if (auth == null || auth.accountID == null) return;
    final controller = CommunityMembersController(
      api: widget.api,
      communityId: item.id,
      actorId: () => auth.accountID,
      authority: auth,
    );
    try {
      await showModalBottomSheet<void>(
        context: context,
        isScrollControlled: true,
        useSafeArea: true,
        builder: (_) => CommunityMemberManagementSheet(controller: controller),
      );
      if (mounted) await _load();
    } finally {
      controller.dispose();
    }
  }

  Future<void> _archive(CommunityItem item) async {
    final actor = widget.auth?.accountID,
        token = widget.api.authorizationHeader(),
        serial = _serial;
    if (actor == null || token == null || _working) return;
    CommunityApproval approval;
    try {
      approval = await widget.api.preview(
        CommunityAction.archive(item.id),
        actorId: actor,
      );
    } catch (e) {
      if (mounted && _current(serial, token)) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(
              e is CommunityApiException ? e.message : '预览暂不可用，请刷新重试。',
            ),
          ),
        );
      }
      return;
    }
    if (!mounted || !_current(serial, token)) return;
    final confirmed = await showDialog<bool>(
      useRootNavigator: false,
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('归档社群？'),
        content: Text('目标：${item.name}\n归档后停止发现和加入，历史活动仍保留。当前以本人所有者身份操作。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('确认归档'),
          ),
        ],
      ),
    );
    if (confirmed != true || !mounted || !_current(serial, token)) return;
    final ok = await _action(
      () => widget.api.archive(item.id, approval: approval),
    );
    if (ok && mounted) {
      Navigator.pop(context);
    }
  }

  @override
  Widget build(BuildContext context) {
    final item = _item;
    final activities =
        widget.city?.activities
            .where(
              (activity) =>
                  activity.organizer?.type == 'COMMUNITY' &&
                  activity.organizer?.id == widget.id &&
                  (activity.status == 'upcoming' ||
                      activity.status == 'ongoing'),
            )
            .toList() ??
        const <PublicActivity>[];
    return Scaffold(
      appBar: AppBar(
        title: Text(item?.name ?? '社群详情'),
        actions: [
          if (item != null)
            IconButton(
              tooltip: '举报此社群',
              icon: const Icon(Icons.flag_outlined),
              onPressed: () => Navigator.push(
                context,
                MaterialPageRoute<void>(
                  builder: (_) => SupportPage(
                    authorizationHeader: () => widget.auth?.authorizationHeader,
                    targetType: 'community',
                    targetID: item.id,
                  ),
                ),
              ),
            ),
        ],
      ),
      body: _loading && item == null
          ? const Center(child: CircularProgressIndicator())
          : _error != null
          ? Center(
              child: TextButton(
                onPressed: _load,
                child: Text('${_error!} 点击重试'),
              ),
            )
          : item == null
          ? const Center(child: Text('社群暂不可访问。'))
          : RefreshIndicator(
              onRefresh: _load,
              child: ListView(
                padding: const EdgeInsets.all(20),
                children: [
                  Row(
                    children: [
                      CircleAvatar(
                        radius: 30,
                        child: Text(
                          item.name.characters.first,
                          style: const TextStyle(fontSize: 24),
                        ),
                      ),
                      const SizedBox(width: 16),
                      Expanded(
                        child: Text(
                          item.name,
                          style: const TextStyle(
                            fontSize: 23,
                            fontWeight: FontWeight.w700,
                          ),
                        ),
                      ),
                    ],
                  ),
                  const SizedBox(height: 12),
                  Text(
                    '${communityVisibilityLabel(item.visibility)} · '
                    '${communityJoinLabel(item.joinPolicy)} · ${item.memberCount} 位成员',
                  ),
                  if (item.joined)
                    OutlinedButton.icon(
                      onPressed: () => Navigator.of(context).push(
                        MaterialPageRoute<void>(
                          builder: (_) => CommunityConversationPage(
                            communityID: item.id,
                            communityTitle: item.name,
                            authorizationHeader: widget.api.authorizationHeader,
                            apiBaseUrl: widget.api.followApiBaseUrl,
                            client: widget.api.followClient,
                          ),
                        ),
                      ),
                      icon: const Icon(Icons.forum_outlined),
                      label: const Text('社群交流'),
                    ),
                  if (item.visibility == 'public' && widget.auth != null)
                    FollowButton(
                      targetType: 'COMMUNITY',
                      targetID: item.id,
                      authorizationHeader: () =>
                          widget.auth?.authorizationHeader,
                      apiBaseUrl: widget.api.followApiBaseUrl,
                      client: widget.api.followClient,
                    ),
                  if (item.visibility == 'public' && widget.auth != null)
                    TextButton.icon(
                      onPressed: () => _entityAction(EntityActionKind.share),
                      icon: const Icon(Icons.chat_bubble_outline),
                      label: const Text('发给好友'),
                    ),
                  const SizedBox(height: 12),
                  if (item.description.isNotEmpty)
                    Text(item.description)
                  else if (!item.joined && item.visibility != 'public')
                    const Text('加入后可查看完整介绍。'),
                  const SizedBox(height: 20),
                  if (item.managed &&
                      widget.auth != null &&
                      widget.city != null &&
                      widget.organizations != null)
                    FilledButton.icon(
                      key: const Key('community_create_activity'),
                      onPressed: widget.city!.selectedCity?.id == 'aberdeen-gb'
                          ? () => _createActivity(item)
                          : null,
                      icon: const Icon(Icons.event_outlined),
                      label: const Text('以社群名义发起活动'),
                    ),
                  if (widget.city != null) ...[
                    const SizedBox(height: 22),
                    const Text(
                      '近期活动',
                      style: TextStyle(
                        fontSize: 18,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                    if (widget.city!.activityError != null)
                      TextButton(
                        onPressed: _load,
                        child: const Text('活动加载失败，点击重试。'),
                      )
                    else if (activities.isEmpty)
                      const Text('暂无可查看的近期活动。')
                    else
                      for (final activity in activities)
                        ListTile(
                          contentPadding: EdgeInsets.zero,
                          title: Text(activity.title),
                          subtitle: Text(
                            '主办方：${activity.organizer?.name ?? item.name}',
                          ),
                          onTap: () => showModalBottomSheet<void>(
                            context: context,
                            isScrollControlled: true,
                            builder: (sheetContext) => ActivityDetailSheet(
                              activity: activity,
                              authorizationHeader: () =>
                                  widget.auth?.authorizationHeader,
                              entrySource: 'community',
                              onOpenOrganizer: (_) {
                                Navigator.of(sheetContext).pop();
                              },
                            ),
                          ),
                        ),
                  ],
                  if (!item.joined && item.myStatus != 'pending')
                    FilledButton(
                      onPressed:
                          _working ||
                              (item.joinPolicy == 'invite_only' &&
                                  item.myStatus != 'invited')
                          ? null
                          : () => _entityAction(EntityActionKind.join),
                      child: Text(
                        item.myStatus == 'invited'
                            ? '接受邀请'
                            : item.joinPolicy == 'request'
                            ? '申请加入'
                            : '加入社群',
                      ),
                    ),
                  if (item.myStatus == 'pending') const Text('申请已提交，等待管理员处理。'),
                  if (item.myStatus == 'pending' || item.myStatus == 'invited')
                    TextButton(
                      onPressed: _working
                          ? null
                          : () => _entityAction(
                              EntityActionKind.join,
                              joining: false,
                            ),
                      child: Text(item.myStatus == 'pending' ? '撤回申请' : '拒绝邀请'),
                    ),
                  if (item.joined && item.myRole != 'owner')
                    OutlinedButton(
                      onPressed: _working
                          ? null
                          : () => _entityAction(
                              EntityActionKind.join,
                              joining: false,
                            ),
                      child: const Text('退出社群'),
                    ),
                  if (item.joined) ...[
                    const SizedBox(height: 24),
                    const Text(
                      '成员',
                      style: TextStyle(
                        fontSize: 18,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                    if (_members.isEmpty)
                      const Text('暂无成员。')
                    else
                      for (final member in _members)
                        ListTile(
                          contentPadding: EdgeInsets.zero,
                          title: Text(member.displayName),
                          subtitle: Text(switch (member.role) {
                            'owner' => '所有者',
                            'admin' => '管理员',
                            _ => '成员',
                          }),
                        ),
                  ],
                  if (item.managed && widget.auth?.accountID != null) ...[
                    const SizedBox(height: 24),
                    Text('待处理：${_requests.length} 项'),
                    OutlinedButton.icon(
                      key: const Key('community_member_management'),
                      onPressed: _working ? null : () => _manage(item),
                      icon: const Icon(Icons.manage_accounts_outlined),
                      label: const Text('成员管理与邀请'),
                    ),
                  ],
                  if (item.myRole == 'owner' &&
                      widget.auth?.accountID != null) ...[
                    const SizedBox(height: 24),
                    TextButton(
                      onPressed: _working ? null : () => _archive(item),
                      child: const Text('归档社群'),
                    ),
                  ],
                ],
              ),
            ),
    );
  }
}
