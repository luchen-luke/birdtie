import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import '../app/birdtie_surfaces.dart';
import 'package:http/http.dart' as http;
import 'package:share_plus/share_plus.dart';
import 'package:url_launcher/url_launcher.dart';

import '../city/public_city_controller.dart';
import '../config/birdtie_environment.dart';
import '../content/moment_publication_controller.dart';
import '../content/moment_publication_sheet.dart';
import 'connections.dart';
import 'support_page.dart';
import 'follow_button.dart';
import 'place_history_controller.dart';
import 'place_history_section.dart';
import 'verified_booking_controller.dart';
import 'verified_booking_section.dart';
import 'private_place_memory_page.dart';
import 'private_place_memory_api.dart';
import 'notification_destination_router.dart';
import 'entity_action_contract.dart';
import 'entity_action_dispatcher.dart';

typedef _PlaceReadFrame = ({
  String? token,
  String? workspace,
  String place,
  int epoch,
});

class PlaceDetailSheet extends StatefulWidget {
  const PlaceDetailSheet({
    super.key,
    required this.placeID,
    required this.authorizationHeader,
    required this.onOpenActivity,
    this.onOpenOrganization,
    this.onOpenBusiness,
    this.shareText,
    this.openExternal,
    this.apiBaseUrl,
    this.client,
    this.organizationWorkspaceID,
    this.workspaceChanges,
    this.onOpenMoment,
    this.privatePlacePendingStore,
  });

  final String placeID;
  final String? Function() authorizationHeader;
  final ValueChanged<PublicActivity> onOpenActivity;
  final ValueChanged<String>? onOpenOrganization;
  final ValueChanged<String>? onOpenBusiness;
  final Future<void> Function(String)? shareText;
  final Future<bool> Function(Uri)? openExternal;
  final String? apiBaseUrl;
  final http.Client? client;
  final String? Function()? organizationWorkspaceID;
  final Listenable? workspaceChanges;
  final ValueChanged<String>? onOpenMoment;
  final PlaceDeclarationPendingStore? privatePlacePendingStore;

  @override
  State<PlaceDetailSheet> createState() => _PlaceDetailSheetState();
}

class _PlaceDetailSheetState extends State<PlaceDetailSheet> {
  late final http.Client? _borrowedClient;
  late final bool _ownsClient;
  late final http.Client _client;
  late final String? _apiBaseUrl;
  late final String? Function() _authorization;
  late final String? Function()? _workspaceID;
  late final Listenable? _workspaceChanges;
  late final PlaceDeclarationPendingStore? _pendingStore;
  bool _connectionRetired = false;
  late PlaceHistoryController _history;
  late VerifiedBookingController _booking;
  MomentPublicationController? _publicationController;
  int _ownMomentRequest = 0;
  int _identityEpoch = 0;
  final _privateEntryChanges = ValueNotifier<int>(0);
  bool _privateEntryRetired = false;
  bool _openingPrivateMemory = false;
  String? _identityToken, _identityWorkspace;
  PublicPlace? _place;
  List<PublicActivity> _activities = const [];
  List<Map<String, dynamic>> _ownMoments = const [];
  String? _momentsToken;
  Map<String, dynamic>? _venue;
  bool _loading = true;
  bool _ownMomentsAvailable = true;
  String? _error;
  String? _activitiesError;
  String? _venueError;
  String? _actionError;

  String get _base => (_apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl)
      .replaceFirst(RegExp(r'/$'), '');
  bool get _personal => widget.organizationWorkspaceID?.call() == null;
  _PlaceReadFrame _frame() => (
    token: widget.authorizationHeader(),
    workspace: widget.organizationWorkspaceID?.call(),
    place: widget.placeID,
    epoch: _identityEpoch,
  );
  bool _current(_PlaceReadFrame frame) =>
      mounted &&
      !_connectionRetired &&
      frame.token == widget.authorizationHeader() &&
      frame.workspace == widget.organizationWorkspaceID?.call() &&
      frame.place == widget.placeID &&
      frame.epoch == _identityEpoch;
  Map<String, String> _frameHeaders(_PlaceReadFrame frame) =>
      frame.token == null ? const {} : {'Authorization': frame.token!};

  Uri _placeURL([String suffix = '']) => Uri.parse(
    '$_base/v1/places/${Uri.encodeComponent(widget.placeID)}$suffix',
  );

  Uri? get _navigationURL {
    final point = _place?.location;
    if (point?.hasPublicPoint != true) return null;
    return Uri.https('www.google.com', '/maps/search/', {
      'api': '1',
      'query': '${point!.latitude},${point.longitude}',
    });
  }

  Uri? _httpsURL(String? raw) {
    if (raw == null) return null;
    final url = Uri.tryParse(raw);
    if (url?.scheme != 'https' || url?.host.isEmpty != false) return null;
    return url;
  }

  @override
  void initState() {
    super.initState();
    _borrowedClient = widget.client;
    _ownsClient = _borrowedClient == null;
    _client = _borrowedClient ?? http.Client();
    _apiBaseUrl = widget.apiBaseUrl;
    _authorization = widget.authorizationHeader;
    _workspaceID = widget.organizationWorkspaceID;
    _workspaceChanges = widget.workspaceChanges;
    _pendingStore = widget.privatePlacePendingStore;
    _identityToken = widget.authorizationHeader();
    _identityWorkspace = widget.organizationWorkspaceID?.call();
    _workspaceChanges?.addListener(_identityChanged);
    _createHistory();
    _createBooking();
    _load();
  }

  void _createHistory() {
    _history = PlaceHistoryController(
      placeID: widget.placeID,
      authorizationHeader: () => widget.authorizationHeader(),
      client: _client,
      apiBaseUrl: widget.apiBaseUrl,
    );
    _history.refresh();
  }

  void _createBooking() {
    _booking = VerifiedBookingController(
      placeID: widget.placeID,
      authorizationHeader: () => widget.authorizationHeader(),
      workspaceID: () => widget.organizationWorkspaceID?.call(),
      authorityEpoch: () => _identityEpoch,
      client: _client,
      apiBaseUrl: widget.apiBaseUrl,
      openExternal: (url) =>
          widget.openExternal?.call(url) ??
          launchUrl(url, mode: LaunchMode.externalApplication),
    );
  }

  bool _businessID(dynamic value) =>
      value is String &&
      value != '00000000-0000-0000-0000-000000000000' &&
      RegExp(
        r'^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$',
      ).hasMatch(value);

  @override
  void didUpdateWidget(covariant PlaceDetailSheet oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (_connectionRetired) return;
    if (!identical(_borrowedClient, widget.client) ||
        _apiBaseUrl != widget.apiBaseUrl ||
        !identical(_authorization, widget.authorizationHeader) ||
        !identical(_workspaceID, widget.organizationWorkspaceID) ||
        !identical(_workspaceChanges, widget.workspaceChanges) ||
        !identical(_pendingStore, widget.privatePlacePendingStore)) {
      _connectionRetired = true;
      _privateEntryRetired = true;
      _identityEpoch++;
      _ownMomentRequest++;
      _workspaceChanges?.removeListener(_identityChanged);
      _publicationController?.invalidate(deferNotification: true);
      _history.invalidate(notify: false);
      _booking.invalidate(notify: false);
      _place = null;
      _activities = const [];
      _ownMoments = const [];
      _venue = null;
      _privateEntryChanges.value++;
      return;
    }
    if (oldWidget.placeID != widget.placeID) {
      _identityEpoch++;
      _privateEntryChanges.value++;
      _ownMomentRequest++;
      _publicationController?.invalidate(deferNotification: true);
      _history.dispose();
      _booking.dispose();
      _createHistory();
      _createBooking();
      _load();
    }
    _checkIdentity(rebuild: false);
  }

  void _identityChanged() => _checkIdentity(rebuild: true);
  void _checkIdentity({required bool rebuild}) {
    if (_connectionRetired) return;
    final token = widget.authorizationHeader();
    final workspace = widget.organizationWorkspaceID?.call();
    if (token == _identityToken && workspace == _identityWorkspace) return;
    _identityToken = token;
    _identityWorkspace = workspace;
    _identityEpoch++;
    _privateEntryChanges.value++;
    _ownMomentRequest++;
    _publicationController?.invalidate(deferNotification: true);
    _history.invalidate(notify: rebuild);
    _booking.invalidate(notify: rebuild);
    _ownMoments = const [];
    _momentsToken = null;
    _activities = const [];
    _venue = null;
    _place = null;
    _error = null;
    _activitiesError = null;
    _venueError = null;
    _loading = true;
    if (mounted && rebuild) setState(() {});
    final epoch = _identityEpoch;
    scheduleMicrotask(() {
      if (!mounted || epoch != _identityEpoch) return;
      unawaited(_history.refresh());
      unawaited(_load());
    });
  }

  @override
  void dispose() {
    _privateEntryRetired = true;
    _privateEntryChanges.value++;
    _privateEntryChanges.dispose();
    if (!_connectionRetired) {
      _workspaceChanges?.removeListener(_identityChanged);
    }
    _ownMomentRequest++;
    _history.dispose();
    _booking.dispose();
    if (_ownsClient) _client.close();
    super.dispose();
  }

  Future<void> _openPrivateMemory() async {
    if (_connectionRetired) return;
    final frame = _frame(), place = _place;
    if (_openingPrivateMemory ||
        !_personal ||
        frame.token == null ||
        widget.workspaceChanges == null ||
        place == null) {
      return;
    }
    setState(() => _openingPrivateMemory = true);
    try {
      // Me resolves the actual opaque Session. Do not infer an owner from the
      // token, place, historical profile or another workspace's cached ID.
      final response = await _client
          .get(Uri.parse('$_base/v1/me'), headers: _frameHeaders(frame))
          .timeout(const Duration(seconds: 12));
      if (!mounted || !_current(frame)) return;
      if (response.statusCode != 200) {
        throw StateError('Current identity unavailable');
      }
      final actor =
          (jsonDecode(utf8.decode(response.bodyBytes))
                  as Map<String, dynamic>)['data']
              as Map<String, dynamic>;
      final owner = actor['id'];
      if (actor['accountType'] != 'person' || !_businessID(owner)) {
        throw StateError('Personal identity required');
      }
      bool current() => !_privateEntryRetired && _current(frame) && _personal;
      final changes = Listenable.merge([
        widget.workspaceChanges,
        _privateEntryChanges,
      ]);
      await Navigator.of(context).push<void>(
        MaterialPageRoute(
          builder: (_) => NotificationDestinationBoundary(
            identityChanges: changes,
            current: current,
            builder: (_) => PrivatePlaceMemoryPage(
              placeID: frame.place,
              placeName: place.name,
              authorizationHeader: () => current() ? frame.token : null,
              accountID: () => current() ? owner as String : null,
              identityChanges: changes,
              organizationWorkspaceID: widget.organizationWorkspaceID,
              client: _client,
              apiBaseUrl: _base,
              pendingStore: widget.privatePlacePendingStore,
            ),
          ),
        ),
      );
    } catch (_) {
      if (mounted && _current(frame)) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(content: Text('请重新登录本人账号后查看地点记录；服务暂不可用时可稍后重试。')),
        );
      }
    } finally {
      if (mounted) setState(() => _openingPrivateMemory = false);
    }
  }

  Future<void> _load() async {
    if (_connectionRetired) return;
    final frame = _frame();
    _booking.invalidate(notify: false);
    if (mounted) {
      setState(() {
        _loading = true;
        _error = null;
        _activitiesError = null;
        _venueError = null;
        _actionError = null;
        _place = null;
        _activities = const [];
        _ownMoments = const [];
        _momentsToken = null;
        _venue = null;
        _ownMomentsAvailable = true;
      });
    }
    try {
      if (_base.isEmpty) throw StateError('API missing');
      final response = await _client
          .get(_placeURL(), headers: _frameHeaders(frame))
          .timeout(const Duration(seconds: 12));
      if (response.statusCode != 200) throw StateError('Place unavailable');
      final place = PublicPlace.fromJson(
        (jsonDecode(utf8.decode(response.bodyBytes))
                as Map<String, dynamic>)['data']
            as Map<String, dynamic>,
      );
      if (place.id != frame.place) throw StateError('Place mismatch');
      if (!_current(frame)) return;
      setState(() => _place = place);
      await Future.wait([
        if (frame.workspace == null) _loadActivities(frame),
        _loadVenue(frame),
        if (frame.token != null && frame.workspace == null)
          _loadOwnMoments(frame.token!),
      ]);
    } catch (_) {
      if (_current(frame)) setState(() => _error = '地点资料加载失败，请重试。');
    } finally {
      if (_current(frame)) setState(() => _loading = false);
    }
  }

  Future<void> _loadActivities(_PlaceReadFrame frame) async {
    if (!_current(frame)) return;
    try {
      final response = await _client
          .get(_placeURL('/activities'), headers: _frameHeaders(frame))
          .timeout(const Duration(seconds: 12));
      if (response.statusCode != 200) {
        throw StateError('Activities unavailable');
      }
      final rows =
          (jsonDecode(utf8.decode(response.bodyBytes))
                  as Map<String, dynamic>)['data']
              as List<dynamic>;
      if (_current(frame)) {
        setState(
          () => _activities = rows
              .map(
                (row) => PublicActivity.fromJson(row as Map<String, dynamic>),
              )
              .toList(),
        );
      }
    } catch (_) {
      if (_current(frame)) setState(() => _activitiesError = '活动暂不可用，请稍后重试。');
    }
  }

  Future<void> _loadVenue(_PlaceReadFrame frame) async {
    if (!_current(frame)) return;
    try {
      final response = await _client
          .get(_placeURL('/venue'), headers: _frameHeaders(frame))
          .timeout(const Duration(seconds: 12));
      if (response.statusCode == 404) return;
      if (response.statusCode != 200) throw StateError('Venue unavailable');
      final data =
          (jsonDecode(utf8.decode(response.bodyBytes))
                  as Map<String, dynamic>)['data']
              as Map<String, dynamic>;
      if (data['placeId'] != frame.place) {
        throw StateError('Venue place mismatch');
      }
      if (_current(frame)) {
        _booking.adopt(data, notify: false);
        setState(() => _venue = data);
      }
    } catch (_) {
      if (_current(frame)) setState(() => _venueError = '场地资料暂不可用，请稍后重试。');
    }
  }

  Future<void> _loadOwnMoments(String token) async {
    if (_connectionRetired) return;
    final frame = _frame();
    if (frame.workspace != null || frame.token != token) return;
    final target = widget.placeID, request = ++_ownMomentRequest;
    try {
      if (mounted) {
        setState(() {
          _momentsToken = token;
          _ownMoments = const [];
          _ownMomentsAvailable = true;
        });
      }
      final response = await _client
          .get(
            Uri.parse('$_base/v1/me/moments'),
            headers: {'Authorization': token},
          )
          .timeout(const Duration(seconds: 12));
      if (response.statusCode != 200) throw StateError('Moments unavailable');
      final rows =
          (jsonDecode(utf8.decode(response.bodyBytes))
                  as Map<String, dynamic>)['data']
              as List<dynamic>;
      if (_current(frame) &&
          widget.authorizationHeader() == token &&
          widget.placeID == target &&
          request == _ownMomentRequest) {
        setState(
          () => _ownMoments = rows
              .whereType<Map<String, dynamic>>()
              .where((row) => row['placeId'] == target)
              .toList(),
        );
      }
    } catch (_) {
      if (_current(frame) &&
          widget.authorizationHeader() == token &&
          widget.placeID == target &&
          request == _ownMomentRequest) {
        setState(() => _ownMomentsAvailable = false);
      }
    }
  }

  Future<void> _publication(Map<String, dynamic> moment) async {
    if (_connectionRetired) return;
    final frame = _frame();
    final target = widget.placeID;
    final token = widget.authorizationHeader();
    final id = moment['id'];
    if (token == null ||
        frame.workspace != null ||
        token != _momentsToken ||
        id is! String ||
        moment['revision'] is! int ||
        moment['placeId'] != widget.placeID) {
      return;
    }
    final withdrawal =
        moment['status'] == 'published' && moment['visibility'] == 'public';
    final controller = MomentPublicationController(
      momentID: id,
      placeID: widget.placeID,
      authorizationHeader: () => widget.authorizationHeader(),
      organizationWorkspaceID: () => widget.organizationWorkspaceID?.call(),
      client: _client,
      apiBaseUrl: widget.apiBaseUrl,
    );
    _publicationController = controller;
    try {
      await showModalBottomSheet<bool>(
        context: context,
        isScrollControlled: true,
        useSafeArea: true,
        builder: (_) => MomentPublicationSheet(
          controller: controller,
          withdrawal: withdrawal,
        ),
      );
      if (_current(frame) &&
          widget.authorizationHeader() == token &&
          widget.placeID == target) {
        await Future.wait([_loadOwnMoments(token), _history.refresh()]);
      }
    } finally {
      if (identical(_publicationController, controller)) {
        _publicationController = null;
      }
      controller.dispose();
    }
  }

  Future<void> _share({EntityActionDescriptor? approved}) async {
    final frame = _frame();
    final place = _place;
    if (place == null || !_current(frame) || frame.workspace != null) return;
    if (approved == null) {
      final shown = [
        place.name,
        place.addressLabel,
        _navigationURL?.toString() ?? '',
      ].join('\u0000');
      await runEntityAction(
        context,
        ref: EntityActionRef('place', frame.place),
        kind: EntityActionKind.share,
        operation: 'EXPORT_PUBLIC',
        authorizationHeader: () => _current(frame) ? frame.token : null,
        workspaceID: widget.organizationWorkspaceID,
        identityChanges: widget.workspaceChanges,
        client: _client,
        apiBaseUrl: _apiBaseUrl,
        domainCurrent: () => _current(frame),
        prepareReview: (_, _) async {
          final response = await _client
              .get(_placeURL(), headers: _frameHeaders(frame))
              .timeout(const Duration(seconds: 12));
          if (!_current(frame) || response.statusCode != 200) return false;
          final current = PublicPlace.fromJson(
            (jsonDecode(utf8.decode(response.bodyBytes))
                    as Map<String, dynamic>)['data']
                as Map<String, dynamic>,
          );
          if (current.id != frame.place) return false;
          _place = current;
          final now = [
            current.name,
            current.addressLabel,
            _navigationURL?.toString() ?? '',
          ].join('\u0000');
          if (now != shown) {
            setState(() => _actionError = '分享内容已变化，请检查最新公开详情后重新发起。');
            return false;
          }
          return true;
        },
        reviewDetails: (_) =>
            '公开内容：${_place!.name}\n地址：${_place!.addressLabel}',
        handler: (a) => _share(approved: a),
      );
      return;
    }
    if (approved.operation != 'EXPORT_PUBLIC' ||
        approved.target != EntityActionRef('place', frame.place)) {
      return;
    }
    final text = [
      place.name,
      if (place.addressLabel.isNotEmpty) '地址：${place.addressLabel}',
      if (_navigationURL case final url?) url.toString(),
    ].join('\n');
    try {
      if (widget.shareText case final share?) {
        await share(text);
      } else {
        final box = context.findRenderObject() as RenderBox?;
        await SharePlus.instance.share(
          ShareParams(
            text: text,
            title: place.name,
            sharePositionOrigin: box == null
                ? null
                : box.localToGlobal(Offset.zero) & box.size,
          ),
        );
      }
    } catch (_) {
      if (mounted) setState(() => _actionError = '分享暂不可用，请稍后重试。');
    }
  }

  Future<void> _open(Uri url) async {
    try {
      final opened =
          await (widget.openExternal?.call(url) ??
              launchUrl(url, mode: LaunchMode.externalApplication));
      if (!opened && mounted) {
        setState(() => _actionError = '无法打开链接，请检查设备设置。');
      }
    } catch (_) {
      if (mounted) setState(() => _actionError = '无法打开链接，请检查设备设置。');
    }
  }

  Future<void> _navigate() async {
    final frame = _frame(), shown = _navigationURL, place = _place;
    if (shown == null ||
        place == null ||
        frame.workspace != null ||
        !_current(frame)) {
      return;
    }
    await runEntityAction(
      context,
      ref: EntityActionRef('place', frame.place),
      kind: EntityActionKind.navigate,
      authorizationHeader: () => _current(frame) ? frame.token : null,
      workspaceID: widget.organizationWorkspaceID,
      identityChanges: widget.workspaceChanges,
      client: _client,
      apiBaseUrl: _apiBaseUrl,
      domainCurrent: () => _current(frame),
      prepareReview: (_, _) async {
        final response = await _client
            .get(_placeURL(), headers: _frameHeaders(frame))
            .timeout(const Duration(seconds: 12));
        if (!_current(frame) || response.statusCode != 200) return false;
        final current = PublicPlace.fromJson(
          (jsonDecode(utf8.decode(response.bodyBytes))
                  as Map<String, dynamic>)['data']
              as Map<String, dynamic>,
        );
        if (current.id != frame.place) return false;
        _place = current;
        final same =
            _navigationURL == shown &&
            current.name == place.name &&
            current.addressLabel == place.addressLabel;
        if (!same) {
          setState(() => _actionError = '导航地点已变化，请检查最新详情后重新发起。');
          return false;
        }
        return true;
      },
      reviewDetails: (_) =>
          '目的地：${_place!.name}\n地址：${_place!.addressLabel.isEmpty ? '公开资料未注明地址' : _place!.addressLabel}\n只使用当前公开准确地点，下一步打开外部地图。',
      handler: (_) async {
        if (_current(frame) && _navigationURL == shown) await _open(shown);
      },
    );
  }

  Future<void> _shareToFriend() async {
    final frame = _frame();
    if (!_current(frame) || frame.workspace != null || frame.token == null) {
      return;
    }
    await runEntityAction(
      context,
      ref: EntityActionRef('place', frame.place),
      kind: EntityActionKind.share,
      authorizationHeader: () => _current(frame) ? frame.token : null,
      workspaceID: widget.organizationWorkspaceID,
      identityChanges: widget.workspaceChanges,
      client: _client,
      apiBaseUrl: _apiBaseUrl,
      domainCurrent: () => _current(frame),
      handler: (_) async {
        if (!_current(frame)) return;
        await shareEntityToChat(
          context,
          authorizationHeader: () => _current(frame) ? frame.token : null,
          identityChanges: widget.workspaceChanges,
          workspaceID: widget.organizationWorkspaceID,
          client: _client,
          type: 'place',
          id: frame.place,
          apiBaseUrl: _apiBaseUrl,
        );
      },
    );
  }

  String _label(String code) => switch (code) {
    'sports_venue' => '体育场馆',
    'community_venue' => '社区场地',
    'library' => '图书馆',
    'badminton' => '羽毛球',
    'sports' => '运动',
    'meeting' => '会议',
    'social' => '社交',
    'culture' => '文化',
    _ => '其他',
  };

  String _reservationLabel(String code) => switch (code) {
    'none' => '未提供预约方式',
    'contact' => '可联系场地咨询',
    'external_url' => '有外部预约说明',
    _ => '预约方式尚未核实',
  };

  @override
  Widget build(BuildContext context) {
    if (_connectionRetired) {
      return BirdtieSurface(
        kind: BirdtieSurfaceKind.critical,
        child: SafeArea(
          child: ListView(
            padding: const EdgeInsets.all(20),
            children: [
              const Text('地点页面连接已变更，请返回后重新打开。'),
              const SizedBox(height: 16),
              const Text('旧详情、预览和确认不能继续使用。未确认的地点声明仍需核实原记录；没有自动重发、撤回或回滚。'),
              const SizedBox(height: 16),
              FilledButton(
                style: FilledButton.styleFrom(minimumSize: const Size(48, 48)),
                onPressed: Navigator.of(context).canPop()
                    ? () => Navigator.of(context).maybePop()
                    : null,
                child: const Padding(
                  padding: EdgeInsets.symmetric(vertical: 12),
                  child: Text('返回'),
                ),
              ),
            ],
          ),
        ),
      );
    }
    final place = _place;
    final venue = _venue;
    final navigation = _navigationURL;
    final businessFrame = _frame();
    final source = _httpsURL(place?.source.reference);
    final ownToken = _personal ? widget.authorizationHeader() : null;
    final ownMomentVisible = ownToken != null && ownToken == _momentsToken;
    return BirdtieSurface(
      kind:
          _error != null ||
              _actionError != null ||
              _venueError != null ||
              _activitiesError != null
          ? BirdtieSurfaceKind.critical
          : BirdtieSurfaceKind.content,
      child: SafeArea(
        child: Padding(
          padding: const EdgeInsets.fromLTRB(20, 18, 20, 26),
          child: ListView(
            shrinkWrap: true,
            children: [
              if (_loading) const LinearProgressIndicator(),
              if (_error != null) ...[
                Text(_error!),
                TextButton(onPressed: _load, child: const Text('重试')),
              ],
              if (place != null) ...[
                Text(place.name, style: Theme.of(context).textTheme.titleLarge),
                if (place.categoryCode.isNotEmpty)
                  Text('地点类别：${_label(place.categoryCode)}'),
                if (place.summary.isNotEmpty) Text(place.summary),
                if (place.addressLabel.isNotEmpty)
                  Text('地址：${place.addressLabel}'),
                Text('资料来源：${place.source.label}'),
                if (place.source.freshness == 'review_needed')
                  const Text('资料更新后尚待复核'),
                if (place.source.freshness == 'unverified')
                  const Text('资料尚未核验'),
                const SizedBox(height: 12),
                Wrap(
                  spacing: 8,
                  children: [
                    if (navigation != null)
                      OutlinedButton.icon(
                        onPressed: _navigate,
                        icon: const Icon(Icons.directions_outlined),
                        label: const Text('导航'),
                      ),
                    OutlinedButton.icon(
                      onPressed: _share,
                      icon: const Icon(Icons.share_outlined),
                      label: const Text('分享地点'),
                    ),
                    if (_personal)
                      OutlinedButton.icon(
                        onPressed: _personal ? _shareToFriend : null,
                        icon: const Icon(Icons.chat_bubble_outline),
                        label: const Text('发给好友'),
                      ),
                    if (source != null)
                      TextButton(
                        onPressed: () => _open(source),
                        child: const Text('查看资料来源'),
                      ),
                  ],
                ),
                if (_actionError != null) Text(_actionError!),
                const SizedBox(height: 16),
                Text('举办条件', style: Theme.of(context).textTheme.titleMedium),
                if (_venueError != null) Text(_venueError!),
                if (!_loading && _venueError == null && venue == null)
                  const Text('暂无已审核的场地举办资料；不能据此判断是否适合办活动。'),
                if (venue != null) ...[
                  if (venue['capacity'] is int)
                    Text('资料所列容量：${venue['capacity']} 人'),
                  Text(
                    _reservationLabel(
                      venue['reservationSupport'] as String? ?? 'unknown',
                    ),
                  ),
                  if (venue['suitability'] case final List<dynamic> values)
                    if (values.isNotEmpty)
                      Text(
                        '适合：${values.whereType<String>().map(_label).join('、')}',
                      ),
                  if (venue['amenities'] case final List<dynamic> values)
                    if (values.isNotEmpty)
                      Text(
                        '设施：${values.whereType<String>().map(_label).join('、')}',
                      ),
                  VerifiedBookingSection(controller: _booking),
                  if (venue['operatorOrganizationName'] case final String name)
                    if (name.isNotEmpty)
                      widget.onOpenOrganization != null &&
                              venue['operatorOrganizationId'] is String
                          ? TextButton(
                              onPressed: () => widget.onOpenOrganization!(
                                venue['operatorOrganizationId'] as String,
                              ),
                              child: Text('场地关联组织：$name'),
                            )
                          : Text('场地关联组织：$name'),
                  if (venue['businesses'] case final List<dynamic> businesses)
                    for (final business
                        in businesses.whereType<Map<String, dynamic>>())
                      if (business['name'] is String)
                        Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text('已核验经营关系：${business['name']}'),
                            Wrap(
                              spacing: 8,
                              children: [
                                if (widget.onOpenBusiness != null &&
                                    _businessID(business['id']))
                                  TextButton(
                                    onPressed: () {
                                      if (_current(businessFrame)) {
                                        widget.onOpenBusiness!(
                                          business['id'] as String,
                                        );
                                      }
                                    },
                                    child: const Text('商家资料'),
                                  ),
                                if (_personal && business['id'] is String)
                                  FollowButton(
                                    targetType: 'BUSINESS',
                                    targetID: business['id'] as String,
                                    authorizationHeader:
                                        widget.authorizationHeader,
                                    apiBaseUrl: widget.apiBaseUrl,
                                    client: widget.client,
                                  ),
                                if (_personal && business['id'] is String)
                                  TextButton(
                                    onPressed: () => runEntityAction(
                                      context,
                                      ref: EntityActionRef(
                                        'business',
                                        business['id'] as String,
                                      ),
                                      kind: EntityActionKind.share,
                                      authorizationHeader: () =>
                                          _current(businessFrame)
                                          ? businessFrame.token
                                          : null,
                                      identityChanges: widget.workspaceChanges,
                                      workspaceID:
                                          widget.organizationWorkspaceID,
                                      client: _client,
                                      apiBaseUrl: _apiBaseUrl,
                                      domainCurrent: () =>
                                          _current(businessFrame),
                                      handler: (_) async {
                                        if (!_current(businessFrame)) return;
                                        await shareEntityToChat(
                                          context,
                                          authorizationHeader: () =>
                                              _current(businessFrame)
                                              ? businessFrame.token
                                              : null,
                                          identityChanges:
                                              widget.workspaceChanges,
                                          workspaceID:
                                              widget.organizationWorkspaceID,
                                          client: _client,
                                          type: 'business',
                                          id: business['id'] as String,
                                          apiBaseUrl: _apiBaseUrl,
                                        );
                                      },
                                    ),
                                    child: const Text('发给好友'),
                                  ),
                                if (_personal && business['id'] is String)
                                  TextButton(
                                    onPressed: () => Navigator.push(
                                      context,
                                      MaterialPageRoute<void>(
                                        builder: (_) => SupportPage(
                                          authorizationHeader:
                                              widget.authorizationHeader,
                                          targetType: 'business',
                                          targetID: business['id'] as String,
                                          apiBaseUrl: widget.apiBaseUrl,
                                        ),
                                      ),
                                    ),
                                    child: const Text('举报商家'),
                                  ),
                              ],
                            ),
                          ],
                        ),
                ],
                const SizedBox(height: 16),
                if (!_personal)
                  const Text('当前为组织工作区，仅展示地点公开资料。请返回个人工作区查看个人记录和可见活动。'),
                if (_personal)
                  Text('这里的活动', style: Theme.of(context).textTheme.titleMedium),
                if (_activitiesError != null) Text(_activitiesError!),
                if (_personal &&
                    !_loading &&
                    _activitiesError == null &&
                    _activities.isEmpty)
                  const Text('目前没有可查看的近期活动。'),
                for (final activity in _activities)
                  ListTile(
                    title: Text(activity.title),
                    subtitle: Text(activity.schedule),
                    onTap: () => widget.onOpenActivity(activity),
                  ),
                const SizedBox(height: 16),
                if (_personal &&
                    widget.authorizationHeader() != null &&
                    widget.workspaceChanges != null &&
                    _place != null)
                  ListTile(
                    leading: const Icon(Icons.lock_outline),
                    title: const Text('我的地点记录'),
                    subtitle: const Text('仅自己可见；收藏不等于喜欢，记录关联不代表真实到访。'),
                    trailing: _openingPrivateMemory
                        ? const SizedBox(
                            width: 20,
                            height: 20,
                            child: CircularProgressIndicator(),
                          )
                        : const Icon(Icons.chevron_right),
                    onTap: _openingPrivateMemory ? null : _openPrivateMemory,
                  ),
                PlaceHistorySection(
                  controller: _history,
                  onOpenMoment: widget.onOpenMoment,
                ),
                if (ownToken != null) ...[
                  const SizedBox(height: 16),
                  Text(
                    '我关联这里的记录',
                    style: Theme.of(context).textTheme.titleMedium,
                  ),
                  const Text('私人草稿仅自己可见，不会随地点分享。明确公开的记录可撤回。'),
                  if (!ownMomentVisible) const Text('登录状态已变化，请重新打开地点。'),
                  if (ownMomentVisible && !_ownMomentsAvailable)
                    const Text('私人记录暂不可用，请稍后重试。'),
                  if (ownMomentVisible &&
                      !_loading &&
                      _ownMomentsAvailable &&
                      _ownMoments.isEmpty)
                    const Text('最近没有找到关联这里的私人记录。'),
                  if (ownMomentVisible)
                    for (final moment in _ownMoments)
                      ListTile(
                        leading: Icon(
                          moment['status'] == 'published' &&
                                  moment['visibility'] == 'public'
                              ? Icons.public
                              : Icons.lock_outline,
                        ),
                        title: Text(moment['title'] as String? ?? ''),
                        subtitle: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            if ((moment['body'] as String? ?? '').isNotEmpty)
                              Text(moment['body'] as String),
                            Text(
                              moment['status'] == 'published' &&
                                      moment['visibility'] == 'public'
                                  ? '已明确公开'
                                  : '仅自己可见，不会随地点分享。',
                            ),
                          ],
                        ),
                        trailing:
                            moment['id'] is String &&
                                moment['revision'] is int &&
                                ((moment['status'] == 'draft' &&
                                        moment['visibility'] == 'private' &&
                                        moment['locationPrecision'] ==
                                            'place') ||
                                    (moment['status'] == 'published' &&
                                        moment['visibility'] == 'public'))
                            ? TextButton(
                                onPressed: () => _publication(moment),
                                child: Text(
                                  moment['status'] == 'published'
                                      ? '撤回公开'
                                      : '查看公开预览',
                                ),
                              )
                            : null,
                      ),
                ],
              ],
            ],
          ),
        ),
      ),
    );
  }
}
