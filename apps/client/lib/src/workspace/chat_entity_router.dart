import 'dart:async';
import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';
import 'package:http/http.dart' as http;
import '../auth/birdtie_auth_controller.dart';
import '../city/public_city_controller.dart';
import '../config/birdtie_environment.dart';
import 'activity_detail_sheet.dart';
import 'community_api.dart';
import 'community_page.dart';
import 'connections.dart';
import 'entity_share_pending_store.dart';
import 'place_detail_sheet.dart';
import 'public_business_page.dart';
import 'public_moment_page.dart';
import 'public_organization_page.dart';
import 'public_person_page.dart';

class _BorrowedChatClient extends http.BaseClient {
  _BorrowedChatClient(this.client);
  final http.Client client;
  @override
  Future<http.StreamedResponse> send(http.BaseRequest request) =>
      client.send(request);
  @override
  void close() {}
}

/// Opens stable domain IDs only. Display text and arbitrary links cannot route.
Future<void> openChatEntity(
  BuildContext context,
  ChatEntityCard card, {
  required BirdtieAuthController auth,
  Listenable? workspaceChanges,
  String? Function()? workspaceID,
  String? apiBaseUrl,
  http.Client? client,
}) async {
  if (!card.available ||
      !chatEntityTypes.contains(card.type) ||
      card.id == null ||
      !chatEntityUUID.hasMatch(card.id!)) {
    return;
  }
  await Navigator.of(context).push<void>(
    MaterialPageRoute(
      builder: (_) => ChatEntityDetail(
        type: card.type,
        id: card.id!,
        auth: auth,
        workspaceChanges: workspaceChanges,
        workspaceID: workspaceID,
        apiBaseUrl: apiBaseUrl,
        client: client,
      ),
    ),
  );
}

/// The nested navigator owns every child dialog and route. Identity changes
/// expire the complete subtree, including pending approvals; it never switches
/// an old operation to a newly selected account or organization.
class ChatEntityDetail extends StatefulWidget {
  const ChatEntityDetail({
    super.key,
    required this.type,
    required this.id,
    required this.auth,
    this.workspaceChanges,
    this.workspaceID,
    this.apiBaseUrl,
    this.client,
  });
  final String type, id;
  final BirdtieAuthController auth;
  final Listenable? workspaceChanges;
  final String? Function()? workspaceID;
  final String? apiBaseUrl;
  final http.Client? client;
  @override
  State<ChatEntityDetail> createState() => _ChatEntityDetailState();
}

class _ChatEntityDetailState extends State<ChatEntityDetail> {
  late final (String?, String?, String?) _identity;
  late final Listenable _changes;
  late final http.Client _client;
  late final bool _ownsClient;
  late final String _apiBaseUrl;
  final _navigator = GlobalKey<NavigatorState>();
  CommunityApi? _community;
  PublicActivity? _activity;
  bool _expired = false;
  bool _listening = true;
  String? _error;
  int _serial = 0;
  (String?, String?, String?) get _current => (
    widget.auth.authorizationHeader,
    widget.auth.accountID,
    widget.workspaceID?.call(),
  );
  bool get _valid => mounted && !_expired && _identity == _current;
  String? _token() => _valid ? _identity.$1 : null;
  String get _base => _apiBaseUrl;
  @override
  void initState() {
    super.initState();
    _identity = _current;
    _ownsClient = widget.client == null;
    _apiBaseUrl = (widget.apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl)
        .replaceFirst(RegExp(r'/$'), '');
    _client = widget.client ?? http.Client();
    _changes = Listenable.merge([widget.auth, widget.workspaceChanges]);
    _changes.addListener(_changed);
    if (!chatEntityTypes.contains(widget.type) ||
        !chatEntityUUID.hasMatch(widget.id)) {
      _error = '这张卡片无法打开。';
    } else if (widget.type == 'activity') {
      unawaited(_readActivity());
    } else if (widget.type == 'community') {
      _community = CommunityApi(
        authorizationHeader: _token,
        apiBaseUrl: _base,
        client: _BorrowedChatClient(_client),
      );
    }
  }

  void _changed() {
    if (_identity == _current || _expired) return;
    _retire();
    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) setState(() {});
      });
    } else {
      setState(() {});
    }
  }

  void _retire() {
    ++_serial;
    _expired = true;
    _activity = null;
    _error = null;
    if (_listening) {
      _listening = false;
      _changes.removeListener(_changed);
    }
  }

  @override
  void didUpdateWidget(ChatEntityDetail old) {
    super.didUpdateWidget(old);
    if (old.id != widget.id ||
        old.type != widget.type ||
        old.auth != widget.auth ||
        old.workspaceChanges != widget.workspaceChanges ||
        old.workspaceID != widget.workspaceID ||
        old.apiBaseUrl != widget.apiBaseUrl ||
        old.client != widget.client ||
        _identity != _current) {
      _retire();
    }
  }

  Future<void> _readActivity() async {
    final serial = ++_serial;
    setState(() => _error = null);
    try {
      if (_base.isEmpty) throw StateError('活动服务尚未配置');
      final r = await _client
          .get(
            Uri.parse('$_base/v1/activities/${widget.id}'),
            headers: {'Authorization': ?_token()},
          )
          .timeout(const Duration(seconds: 12));
      if (r.statusCode != 200) throw StateError('活动不可访问');
      final value = PublicActivity.fromJson(
        (jsonDecode(utf8.decode(r.bodyBytes)) as Map<String, dynamic>)['data']
            as Map<String, dynamic>,
      );
      if (value.id != widget.id) throw const FormatException('活动标识不符');
      if (_valid && serial == _serial) setState(() => _activity = value);
    } catch (_) {
      if (_valid && serial == _serial) {
        setState(() => _error = '活动已失效、不可访问，或暂时无法读取。');
      }
    }
  }

  void _open(String type, String id) {
    if (!_valid ||
        !chatEntityTypes.contains(type) ||
        !chatEntityUUID.hasMatch(id)) {
      return;
    }
    _navigator.currentState?.push<void>(
      MaterialPageRoute(
        builder: (_) => ChatEntityDetail(
          type: type,
          id: id,
          auth: widget.auth,
          workspaceChanges: widget.workspaceChanges,
          workspaceID: widget.workspaceID,
          apiBaseUrl: _base,
          client: _client,
        ),
      ),
    );
  }

  Widget _detail() => switch (widget.type) {
    'activity' => Scaffold(
      appBar: AppBar(title: const Text('活动详情')),
      body: ActivityDetailSheet(
        activity: _activity!,
        authorizationHeader: _token,
        apiBaseUrl: _base,
        client: _client,
        identityChanges: _changes,
        workspaceID: widget.workspaceID,
        entrySource: 'human_chat',
        onOpenOrganization: (id) => _open('organization', id),
        onOpenOrganizer: (o) => _open(switch (o.type) {
          'PERSON' => 'person',
          'BUSINESS' => 'business',
          'COMMUNITY' => 'community',
          _ => 'organization',
        }, o.id),
      ),
    ),
    'place' => Scaffold(
      appBar: AppBar(title: const Text('地点详情')),
      body: PlaceDetailSheet(
        placeID: widget.id,
        authorizationHeader: _token,
        apiBaseUrl: _base,
        client: _client,
        workspaceChanges: _changes,
        organizationWorkspaceID: widget.workspaceID,
        onOpenActivity: (a) => _open('activity', a.id),
        onOpenOrganization: (id) => _open('organization', id),
        onOpenBusiness: (id) => _open('business', id),
        onOpenMoment: (id) => _open('moment', id),
      ),
    ),
    'person' => PublicPersonPage(
      auth: widget.auth,
      accountID: widget.id,
      authorizationHeader: _token,
      identityChanges: _changes,
      workspaceID: widget.workspaceID,
      apiBaseUrl: _base,
      client: _client,
    ),
    'community' => CommunityDetailPage(
      id: widget.id,
      api: _community!,
      auth: widget.auth,
    ),
    'organization' => PublicOrganizationPage(
      organizationID: widget.id,
      authorizationHeader: _token,
      identityChanges: _changes,
      workspaceID: widget.workspaceID,
      apiBaseUrl: _base,
      client: _client,
      onOpenActivity: (a) => _open('activity', a.id),
    ),
    'business' => PublicBusinessPage(
      businessID: widget.id,
      authorizationHeader: _token,
      identityChanges: _changes,
      workspaceID: widget.workspaceID,
      apiBaseUrl: _base,
      client: _client,
      onOpenActivity: (a) => _open('activity', a.id),
    ),
    _ => PublicMomentPage(
      momentID: widget.id,
      authorizationHeader: _token,
      identityChanges: _changes,
      workspaceID: widget.workspaceID,
      apiBaseUrl: _base,
      client: _client,
      onOpenPlace: (id) => _open('place', id),
    ),
  };
  @override
  void dispose() {
    _retire();
    _community?.dispose();
    if (_ownsClient) _client.close();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    if (_expired) {
      return Scaffold(
        appBar: AppBar(title: const Text('请重新打开内容')),
        body: ListView(
          padding: const EdgeInsets.all(24),
          children: [
            const Text('工作身份已变化，或内容连接已更改。请返回当前会话，再打开卡片。'),
            const SizedBox(height: 16),
            TextButton(
              style: TextButton.styleFrom(minimumSize: const Size(48, 48)),
              onPressed: () => Navigator.of(context).maybePop(),
              child: const Text('返回当前会话'),
            ),
          ],
        ),
      );
    }
    if (_error != null) {
      return Scaffold(
        appBar: AppBar(title: const Text('内容暂不可查看')),
        body: Center(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Text(_error!),
              if (widget.type == 'activity')
                TextButton(onPressed: _readActivity, child: const Text('重新核实')),
            ],
          ),
        ),
      );
    }
    if (widget.type == 'activity' && _activity == null) {
      return const Scaffold(body: Center(child: CircularProgressIndicator()));
    }
    return Navigator(
      key: _navigator,
      onGenerateRoute: (_) {
        return MaterialPageRoute<void>(
          builder: (_) => _ChatBackScope(
            enabled: () => _valid,
            onBack: () => Navigator.of(context).pop(),
            child: _detail(),
          ),
        );
      },
    );
  }
}

/// Local history needs an installed ModalRoute. Register after its first frame,
/// and suppress removal callbacks when identity expiry disposes the subtree.
class _ChatBackScope extends StatefulWidget {
  const _ChatBackScope({
    required this.enabled,
    required this.onBack,
    required this.child,
  });
  final bool Function() enabled;
  final VoidCallback onBack;
  final Widget child;
  @override
  State<_ChatBackScope> createState() => _ChatBackScopeState();
}

class _ChatBackScopeState extends State<_ChatBackScope> {
  bool _registered = false;
  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    if (_registered) return;
    _registered = true;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted || !widget.enabled()) return;
      ModalRoute.of(context)?.addLocalHistoryEntry(
        LocalHistoryEntry(
          onRemove: () {
            if (mounted && widget.enabled()) widget.onBack();
          },
        ),
      );
    });
  }

  @override
  Widget build(BuildContext context) => widget.child;
}
