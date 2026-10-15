import 'package:flutter/material.dart';

import '../auth/session_vault.dart';
import 'human_causal_control.dart';

/// A navigation request carries an identity, never content or a write approval.
class NowMomentEditorRequest {
  const NowMomentEditorRequest({
    required String momentID,
    required this.ownerID,
    required this.authorizationHeader,
    required this.apiBaseUrl,
    required this.sourceCurrent,
  }) : momentID = momentID;

  const NowMomentEditorRequest.create({
    required this.ownerID,
    required this.authorizationHeader,
    required this.apiBaseUrl,
    required this.sourceCurrent,
  }) : momentID = null;

  final String? momentID;
  final String ownerID, authorizationHeader, apiBaseUrl;
  final bool Function() sourceCurrent;
}

/// One capability per running app, above every route in its Navigator.
class NowMomentEditorScope extends StatefulWidget {
  const NowMomentEditorScope({super.key, required this.child});
  final Widget child;

  static NowMomentEditorController? maybeOf(BuildContext context) => context
      .dependOnInheritedWidgetOfExactType<_NowMomentEditorInherited>()
      ?.controller;

  @override
  State<NowMomentEditorScope> createState() => _NowMomentEditorScopeState();
}

class _NowMomentEditorScopeState extends State<NowMomentEditorScope> {
  final _controller = NowMomentEditorController._();

  @override
  Widget build(BuildContext context) =>
      _NowMomentEditorInherited(controller: _controller, child: widget.child);

  @override
  void dispose() {
    _controller._closed = true;
    _controller._host = null;
    super.dispose();
  }
}

class _NowMomentEditorInherited extends InheritedWidget {
  const _NowMomentEditorInherited({
    required this.controller,
    required super.child,
  });
  final NowMomentEditorController controller;
  @override
  bool updateShouldNotify(_NowMomentEditorInherited oldWidget) =>
      !identical(controller, oldWidget.controller);
}

class NowMomentEditorController {
  NowMomentEditorController._();
  _NowMomentEditorHostState? _host;
  bool _opening = false, _closed = false;

  Future<bool> open(NowMomentEditorRequest request) async {
    final host = _host;
    if (_closed || _opening || host == null || !host.mounted) return false;
    host._observeIdentity();
    final binding = host._binding, generation = host._generation;
    final route = host._route, navigator = host._navigator;
    String origin;
    try {
      origin = canonicalAuthOrigin(request.apiBaseUrl);
    } on FormatException {
      return false;
    }
    bool current() {
      if (_closed || !identical(_host, host) || !host.mounted) return false;
      host._observeIdentity();
      return generation == host._generation &&
          binding == host._binding &&
          identical(route, host._route) &&
          route?.isActive == true &&
          identical(navigator, route?.navigator);
    }

    final momentID = request.momentID;
    if ((momentID == null
            ? host.widget.onCreate == null
            : !humanControlID(momentID)) ||
        !humanControlID(request.ownerID) ||
        request.authorizationHeader.isEmpty ||
        origin.isEmpty ||
        binding.$1 != request.ownerID ||
        binding.$2 != request.authorizationHeader ||
        binding.$3 != origin ||
        binding.$4 != null ||
        navigator == null ||
        !current() ||
        !host.widget.canOpen() ||
        !request.sourceCurrent()) {
      return false;
    }
    _opening = true;
    try {
      // The source is checked before transferring ownership to the original
      // workspace route. Disposing a popped source must not cancel that handoff.
      navigator.popUntil((candidate) => identical(candidate, route));
      if (!current() || !host.widget.canOpen()) return false;
      if (momentID == null) {
        await host.widget.onCreate!(current);
      } else {
        await host.widget.onOpen(momentID, current);
      }
      return current();
    } catch (_) {
      return false;
    } finally {
      _opening = false;
    }
  }
}

/// Registers the existing Now route; opening an editor never creates a workspace.
class NowMomentEditorHost extends StatefulWidget {
  const NowMomentEditorHost({
    super.key,
    required this.ownerID,
    required this.authorizationHeader,
    required this.apiBaseUrl,
    required this.workspaceID,
    required this.canOpen,
    required this.onOpen,
    required this.child,
    this.onCreate,
    this.identityChanges,
  });
  final String? Function() ownerID, authorizationHeader, workspaceID;
  final String Function() apiBaseUrl;
  final bool Function() canOpen;
  final Future<void> Function(String momentID, bool Function() current) onOpen;
  final Future<void> Function(bool Function() current)? onCreate;
  final Listenable? identityChanges;
  final Widget child;

  @override
  State<NowMomentEditorHost> createState() => _NowMomentEditorHostState();
}

class _NowMomentEditorHostState extends State<NowMomentEditorHost> {
  NowMomentEditorController? _controller;
  ModalRoute<dynamic>? _route;
  NavigatorState? _navigator;
  late (String?, String?, String, String?) _binding;
  int _generation = 0;

  (String?, String?, String, String?) _readBinding() {
    var origin = '';
    try {
      origin = canonicalAuthOrigin(widget.apiBaseUrl());
    } on FormatException {
      // An invalid or absent origin cannot receive an authenticated handoff.
    }
    return (
      widget.ownerID(),
      widget.authorizationHeader(),
      origin,
      widget.workspaceID(),
    );
  }

  void _observeIdentity() {
    final next = _readBinding();
    if (next == _binding) return;
    _binding = next;
    ++_generation;
  }

  @override
  void initState() {
    super.initState();
    _binding = _readBinding();
    widget.identityChanges?.addListener(_observeIdentity);
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    final controller = NowMomentEditorScope.maybeOf(context);
    final route = ModalRoute.of(context);
    final navigator = Navigator.maybeOf(context, rootNavigator: true);
    if (!identical(controller, _controller) ||
        !identical(route, _route) ||
        !identical(navigator, _navigator)) {
      _detach();
      _controller = controller;
      _route = route;
      _navigator = navigator;
    }
    if (controller != null &&
        route != null &&
        navigator != null &&
        identical(route.navigator, navigator) &&
        (controller._host == null || identical(controller._host, this))) {
      controller._host = this;
    }
  }

  @override
  void didUpdateWidget(NowMomentEditorHost oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (!identical(oldWidget.identityChanges, widget.identityChanges)) {
      oldWidget.identityChanges?.removeListener(_observeIdentity);
      widget.identityChanges?.addListener(_observeIdentity);
    }
    _observeIdentity();
  }

  void _detach() {
    ++_generation;
    if (identical(_controller?._host, this)) _controller?._host = null;
  }

  @override
  Widget build(BuildContext context) => widget.child;

  @override
  void dispose() {
    _detach();
    widget.identityChanges?.removeListener(_observeIdentity);
    super.dispose();
  }
}
