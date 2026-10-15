import 'package:flutter/material.dart';

import '../workspace/map_display_policy.dart';
import '../workspace/map_entities.dart';

class MapReaderChanges extends ChangeNotifier {
  void changed() => notifyListeners();
}

/// Reads the current projection, rather than retaining the modal's old rows.
/// Each provider continues to own its original selection and camera behavior.
Future<MapEntity?> showCurrentMapClusterMembers({
  required BuildContext context,
  required MapEntity cluster,
  required Listenable changes,
  required List<MapEntity> Function() currentMembers,
  required Object? Function() currentScope,
  ValueChanged<({Object token, bool visible})>? onVisibilityChanged,
}) async {
  if (!context.mounted || ModalRoute.of(context)?.isCurrent != true) return null;
  final scope = currentScope();
  final token = Object();
  var visible = false;
  void reportVisibility(bool next) {
    if (visible == next) return;
    visible = next;
    onVisibilityChanged?.call((token: token, visible: next));
  }

  if (currentMembers().isEmpty) return null;
  try {
    // Transfer visibility before push can rebuild the underlying Now route.
    // The modal's own route observer still revokes it when covered or stale.
    reportVisibility(true);
    if (!context.mounted ||
        ModalRoute.of(context)?.isCurrent != true ||
        currentScope() != scope) return null;
    final selected = await showModalBottomSheet<MapEntity>(
      context: context,
      isScrollControlled: true,
      backgroundColor: Theme.of(context).colorScheme.surface,
      builder: (_) => CurrentMapReaderVisibility(
        current: () => currentScope() == scope,
        onChanged: reportVisibility,
        child: _CurrentMapClusterMembers(
          cluster: cluster,
          changes: changes,
          currentMembers: currentMembers,
          current: () => currentScope() == scope,
        ),
      ),
    );
    if (selected == null || currentScope() != scope) return null;
    // A returned row is an identifier for selection, never a cached approval.
    return currentMembers().where((e) => e.id == selected.id).firstOrNull;
  } finally {
    reportVisibility(false);
  }
}

/// A mounted modal may itself be covered. Subscribe to its own route state,
/// rather than borrowing visibility from the route underneath it.
class CurrentMapReaderVisibility extends StatefulWidget {
  const CurrentMapReaderVisibility({
    super.key,
    required this.current,
    required this.onChanged,
    required this.child,
  });
  final bool Function() current;
  final ValueChanged<bool> onChanged;
  final Widget child;

  @override
  State<CurrentMapReaderVisibility> createState() =>
      _CurrentMapReaderVisibilityState();
}

class _CurrentMapReaderVisibilityState
    extends State<CurrentMapReaderVisibility> {
  int _serial = 0;
  bool? _reported;

  void _scheduleVisibility() {
    final serial = ++_serial;
    final route = ModalRoute.of(context);
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted || serial != _serial) return;
      final visible = route?.isCurrent == true && widget.current();
      if (_reported == visible) return;
      _reported = visible;
      widget.onChanged(visible);
    });
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    _scheduleVisibility();
  }

  @override
  void didUpdateWidget(CurrentMapReaderVisibility oldWidget) {
    super.didUpdateWidget(oldWidget);
    _scheduleVisibility();
  }

  @override
  Widget build(BuildContext context) => widget.child;
}

class _CurrentMapClusterMembers extends StatefulWidget {
  const _CurrentMapClusterMembers({
    required this.cluster,
    required this.changes,
    required this.currentMembers,
    required this.current,
  });
  final MapEntity cluster;
  final Listenable changes;
  final List<MapEntity> Function() currentMembers;
  final bool Function() current;

  @override
  State<_CurrentMapClusterMembers> createState() =>
      _CurrentMapClusterMembersState();
}

class _CurrentMapClusterMembersState extends State<_CurrentMapClusterMembers> {
  final _scroll = ScrollController();
  bool _retired = false;

  @override
  void initState() {
    super.initState();
    widget.changes.addListener(_sourceChanged);
  }

  void _sourceChanged() {
    if (!mounted || _retired) return;
    // Scope ABA must close this reader, even if a new scope later looks equal.
    if (!widget.current() || widget.currentMembers().isEmpty) {
      _retired = true;
      setState(() {});
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (!mounted) return;
        final route = ModalRoute.of(context);
        if (route?.isCurrent == true) {
          Navigator.of(context).pop();
        } else if (route != null && route.isActive) {
          Navigator.of(context).removeRoute(route);
        }
      });
    } else {
      setState(() {});
    }
  }

  @override
  void dispose() {
    widget.changes.removeListener(_sourceChanged);
    _scroll.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final members = _retired || !widget.current()
        ? const <MapEntity>[]
        : widget.currentMembers();
    return SafeArea(
      child: SizedBox(
        key: const Key('current-map-cluster-members'),
        height: MediaQuery.sizeOf(context).height * .72,
        child: ListView(
          controller: _scroll,
          children: [
            ListTile(
              title: Text(
                members.isEmpty
                    ? '当前内容已更新'
                    : MapDisplayPolicy.countLabel(members),
              ),
              subtitle: Text(
                widget.cluster.placeID.isNotEmpty ? '同一场地的可见内容' : '附近的可见内容',
              ),
            ),
            for (final member in members)
              ListTile(
                key: ValueKey('current-map-member:${member.id}'),
                title: Text(member.title),
                subtitle: Text(member.subtitle),
                onTap: () {
                  if (_retired || !widget.current()) return;
                  final latest = widget
                      .currentMembers()
                      .where((e) => e.id == member.id)
                      .firstOrNull;
                  if (latest != null) Navigator.pop(context, latest);
                },
              ),
          ],
        ),
      ),
    );
  }
}
