import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';

/// Contains all child routes and approval dialogs under one original identity.
/// Once retired, returning to the same identity cannot revive the old subtree.
class NotificationDestinationBoundary extends StatefulWidget {
  const NotificationDestinationBoundary({
    super.key,
    required this.identityChanges,
    required this.current,
    required this.builder,
  });
  final Listenable identityChanges;
  final bool Function() current;
  final WidgetBuilder builder;
  @override
  State<NotificationDestinationBoundary> createState() => _BoundaryState();
}

class _BoundaryState extends State<NotificationDestinationBoundary> {
  bool _retired = false;
  final _navigator = GlobalKey<NavigatorState>();
  bool get _valid => mounted && !_retired && widget.current();
  @override
  void initState() {
    super.initState();
    _retired = !widget.current();
    widget.identityChanges.addListener(_changed);
  }

  void _changed() {
    if (_retired || widget.current()) return;
    _retired = true;
    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) setState(() {});
      });
    } else {
      setState(() {});
    }
  }

  @override
  void didUpdateWidget(NotificationDestinationBoundary old) {
    super.didUpdateWidget(old);
    if (old.identityChanges != widget.identityChanges) {
      old.identityChanges.removeListener(_changed);
      widget.identityChanges.addListener(_changed);
      _retired = true;
    }
    if (!widget.current()) _retired = true;
  }

  @override
  void dispose() {
    _retired = true;
    widget.identityChanges.removeListener(_changed);
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    if (!_valid) {
      return Scaffold(
        appBar: AppBar(title: const Text('请重新打开内容')),
        body: const Padding(
          padding: EdgeInsets.all(24),
          child: Text('工作身份或来源已变化，请返回当前入口重新核实。'),
        ),
      );
    }
    return NavigatorPopHandler<Object?>(
      onPopWithResult: (result) {
        if (_valid) _navigator.currentState?.pop(result);
      },
      child: Navigator(
        key: _navigator,
        onGenerateRoute: (_) => MaterialPageRoute<void>(
          builder: (inner) => _DestinationBackScope(
            valid: () => _valid,
            onBack: () => Navigator.of(context).pop(),
            child: widget.builder(inner),
          ),
        ),
      ),
    );
  }
}

class _DestinationBackScope extends StatefulWidget {
  const _DestinationBackScope({
    required this.valid,
    required this.onBack,
    required this.child,
  });
  final bool Function() valid;
  final VoidCallback onBack;
  final Widget child;
  @override
  State<_DestinationBackScope> createState() => _DestinationBackScopeState();
}

class _DestinationBackScopeState extends State<_DestinationBackScope> {
  bool _registered = false;
  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    if (_registered) return;
    _registered = true;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted || !widget.valid()) return;
      ModalRoute.of(context)?.addLocalHistoryEntry(
        LocalHistoryEntry(
          onRemove: () {
            if (mounted && widget.valid()) widget.onBack();
          },
        ),
      );
    });
  }

  @override
  Widget build(BuildContext context) => widget.child;
}
