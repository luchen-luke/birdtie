import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';
import '../auth/birdtie_auth_controller.dart';
import '../content/private_moment_controller.dart';
import '../content/private_moment_media_controller.dart';
import '../content/private_moment_media_sheet.dart';
import 'notification_destination_router.dart';

/// Human-only chooser for an existing owned private draft. Never creates a
/// Moment, treats a SocialIntent as a Moment, or copies a Now draft into it.
class NowPrivateMomentImagePage extends StatefulWidget {
  const NowPrivateMomentImagePage({
    super.key,
    required this.auth,
    required this.moments,
    required this.identityChanges,
    required this.current,
    required this.organizationWorkspaceID,
  });
  final BirdtieAuthController auth;
  final PrivateMomentController moments;
  final Listenable identityChanges;
  final bool Function() current;
  final String? Function() organizationWorkspaceID;
  @override
  State<NowPrivateMomentImagePage> createState() => _ImageEntryState();
}

class _ImageEntryState extends State<NowPrivateMomentImagePage> {
  late final BirdtieAuthController _auth;
  late final PrivateMomentController _moments;
  late final Listenable _boundChanges, _changes;
  late final bool Function() _sourceCurrent;
  late final String? Function() _organization;
  final _entryChanges = ValueNotifier<int>(0);
  late final String? _token, _owner;
  late final int _epoch;
  bool _retired = false, _opening = false;
  @override
  void initState() {
    super.initState();
    _auth = widget.auth;
    _moments = widget.moments;
    _boundChanges = widget.identityChanges;
    _sourceCurrent = widget.current;
    _organization = widget.organizationWorkspaceID;
    _changes = Listenable.merge([_boundChanges, _entryChanges]);
    _token = widget.auth.authorizationHeader;
    _owner = widget.auth.accountID;
    _epoch = widget.moments.identityEpoch;
    _boundChanges.addListener(_observe);
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (_current) _load();
    });
  }

  bool get _current =>
      mounted &&
      !_retired &&
      identical(_auth, widget.auth) &&
      identical(_moments, widget.moments) &&
      identical(_boundChanges, widget.identityChanges) &&
      identical(_sourceCurrent, widget.current) &&
      identical(_organization, widget.organizationWorkspaceID) &&
      widget.current() &&
      _token != null &&
      _owner != null &&
      _token == widget.auth.authorizationHeader &&
      _owner == widget.auth.accountID &&
      _token == widget.moments.authorizationHeader() &&
      _owner == widget.moments.ownerID?.call() &&
      _epoch == widget.moments.identityEpoch &&
      widget.organizationWorkspaceID() == null;

  void _observe() {
    if (_retired || _current) return;
    _retired = true;
    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) _entryChanges.value++;
      });
    } else {
      _entryChanges.value++;
    }
  }

  @override
  void didUpdateWidget(NowPrivateMomentImagePage old) {
    super.didUpdateWidget(old);
    _observe();
  }

  @override
  void dispose() {
    _retired = true;
    _boundChanges.removeListener(_observe);
    _entryChanges.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    if (!_current || widget.moments.loading) return;
    await widget.moments.refresh(current: () => _current);
    if (mounted && !_current) setState(() => _retired = true);
  }

  bool _version(PrivateMoment value) =>
      _current &&
      RegExp(
        r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
      ).hasMatch(value.id) &&
      value.authorAccountID == _owner &&
      value.visibility == 'private' &&
      value.status == 'draft' &&
      value.revision > 0 &&
      widget.moments.moments.any(
        (m) =>
            m.id == value.id &&
            m.authorAccountID == _owner &&
            m.revision == value.revision &&
            m.visibility == 'private' &&
            m.status == 'draft',
      );
  Future<void> _open(PrivateMoment value) async {
    if (_opening || !_version(value)) return;
    _opening = true;
    final c = PrivateMomentMediaController(
      momentID: value.id,
      momentRevision: value.revision,
      authorizationHeader: () => widget.auth.authorizationHeader,
      ownerID: () => widget.auth.accountID,
      identityChanges: _changes,
      sourceCurrent: () => _version(value),
      organizationWorkspaceID: widget.organizationWorkspaceID,
      client: widget.moments.borrowedClient,
      apiBaseUrl: widget.moments.apiBaseUrl,
    );
    try {
      await Navigator.of(context).push<void>(
        MaterialPageRoute(
          builder: (_) => NotificationDestinationBoundary(
            identityChanges: _changes,
            current: () => _version(value),
            builder: (_) => PrivateMomentMediaSheet(
              controller: c,
              momentTitle: value.title,
            ),
          ),
        ),
      );
    } finally {
      c.dispose();
      _opening = false;
    }
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(
      title: const Text('选择私人记录'),
      leading: IconButton(
        tooltip: '关闭选择私人记录',
        icon: const Icon(Icons.close),
        onPressed: () => Navigator.of(context).pop(),
      ),
    ),
    body: AnimatedBuilder(
      animation: _changes,
      builder: (context, _) {
        if (!_current) {
          _retired = true;
          return const Padding(
            padding: EdgeInsets.all(24),
            child: Text('身份或记录来源已变化，请返回 Now 重新打开。'),
          );
        }
        final m = widget.moments;
        if (m.loading) {
          return const Center(
            child: CircularProgressIndicator(semanticsLabel: '正在读取本人私人记录'),
          );
        }
        if (m.error != null) {
          return Padding(
            padding: const EdgeInsets.all(24),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(switch (m.readStatus) {
                  401 => '登录已失效，请关闭页面并重新登录，再打开本人的私人记录。',
                  403 => '当前身份没有读取权限，请关闭页面并回到本人的私人记录。',
                  _ => '${m.error} 读取失败不表示没有记录，请确认当前登录身份。',
                }),
                const SizedBox(height: 16),
                if (![401, 403].contains(m.readStatus))
                  FilledButton(onPressed: _load, child: const Text('重新读取私人记录')),
              ],
            ),
          );
        }
        final choices = m.moments.where((v) => _version(v)).toList();
        if (choices.isEmpty) {
          return const Padding(
            padding: EdgeInsets.all(24),
            child: Text('暂无可添加图片的私人草稿。请先到个人资料保存私人记录，再从这里选择。'),
          );
        }
        return ListView(
          padding: const EdgeInsets.all(16),
          children: [
            const Padding(
              padding: EdgeInsets.only(bottom: 16),
              child: Text('选择本人已保存的私人草稿。图片仅自己可读，不加入 Now 对话、不公开，也不发送给 AI。'),
            ),
            for (final choice in choices)
              ListTile(
                key: ValueKey('private-image-moment-${choice.id}'),
                title: Text(choice.title),
                subtitle: const Text('仅自己可见 · 草稿'),
                trailing: const Icon(Icons.chevron_right),
                onTap: () => _open(choice),
              ),
          ],
        );
      },
    ),
  );
}
