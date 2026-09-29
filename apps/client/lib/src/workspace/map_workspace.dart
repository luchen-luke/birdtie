import 'dart:async';

import 'package:flutter/material.dart';

import '../auth/birdtie_auth_controller.dart';
import '../city/public_city_controller.dart';
import '../content/private_moment_controller.dart';
import '../legacy/legacy_shell.dart';
import 'agent_composer.dart';
import 'agent_result_sheet.dart';
import 'agent_workspace_controller.dart';
import 'inbox.dart';
import 'map_canvas.dart';
import 'sidebar.dart';
import 'top_controls.dart';

class MapWorkspace extends StatefulWidget {
  const MapWorkspace({
    super.key,
    required this.city,
    required this.auth,
    required this.moments,
  });
  final PublicCityController city;
  final BirdtieAuthController auth;
  final PrivateMomentController moments;

  @override
  State<MapWorkspace> createState() => _MapWorkspaceState();
}

class _MapWorkspaceState extends State<MapWorkspace> {
  final _scaffold = GlobalKey<ScaffoldState>();
  final _workspace = AgentWorkspaceController();

  @override
  void dispose() {
    _workspace.dispose();
    super.dispose();
  }

  void _submit(String query) {
    unawaited(
      _workspace.submit(
        query,
        widget.city.activities,
        widget.city.places,
        cityID: widget.city.selectedCity?.id,
      ),
    );
  }

  void _inbox() {
    showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      backgroundColor: const Color(0xFFFCFBF8),
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(26)),
      ),
      builder: (context) => SizedBox(
        height: MediaQuery.sizeOf(context).height * 0.82,
        child: const InboxPanel(),
      ),
    );
  }

  void _destination(SidebarDestination destination) {
    Navigator.pop(context);
    if (destination == SidebarDestination.home) return;
    final title = switch (destination) {
      SidebarDestination.activities => 'My Activities',
      SidebarDestination.groups => 'Groups',
      SidebarDestination.saved => 'Saved',
      SidebarDestination.profile => 'Profile',
      SidebarDestination.settings => 'Settings',
      SidebarDestination.home => 'Home',
    };
    final content = switch (destination) {
      SidebarDestination.activities => Column(
        children: [
          const Padding(
            padding: EdgeInsets.fromLTRB(20, 12, 20, 0),
            child: Text(
              'Your activity list is not connected yet. Published city activities are shown below.',
              style: TextStyle(color: Color(0xFF747B73), fontSize: 12),
            ),
          ),
          Expanded(child: LegacyActivityPage(city: widget.city)),
        ],
      ),
      SidebarDestination.profile => LegacyProfilePage(
        auth: widget.auth,
        moments: widget.moments,
        city: widget.city,
      ),
      _ => Center(
        child: Padding(
          padding: const EdgeInsets.all(32),
          child: Text(
            '$title is not connected yet.',
            textAlign: TextAlign.center,
            style: const TextStyle(color: Color(0xFF747B73)),
          ),
        ),
      ),
    };
    Navigator.push(
      context,
      MaterialPageRoute<void>(
        builder: (context) => Scaffold(
          appBar: AppBar(title: Text(title)),
          body: content,
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: Listenable.merge([widget.city, _workspace]),
    builder: (context, _) => Scaffold(
      key: _scaffold,
      resizeToAvoidBottomInset: true,
      drawer: Sidebar(
        workspace: _workspace,
        city: widget.city,
        onNew: () {
          _workspace.newTask();
          Navigator.pop(context);
        },
        onDestination: _destination,
        onRecent: (task) {
          Navigator.pop(context);
          if (task.cityID != null) widget.city.selectCity(task.cityID!);
          _workspace.reopen(task, widget.city.activities, widget.city.places);
        },
      ),
      body: LayoutBuilder(
        builder: (context, constraints) => Stack(
          children: [
            Positioned.fill(
              child: MapCanvas(city: widget.city, workspace: _workspace),
            ),
            Positioned(
              top: 0,
              left: 0,
              right: 0,
              child: TopControls(
                onSidebar: () => _scaffold.currentState?.openDrawer(),
                onInbox: _inbox,
              ),
            ),
            if (_workspace.task != null &&
                _workspace.state != AgentViewState.typing)
              Positioned(
                left: 0,
                right: 0,
                bottom: MediaQuery.paddingOf(context).bottom + 84,
                child: ConstrainedBox(
                  constraints: BoxConstraints(
                    maxHeight: constraints.maxHeight - 120,
                  ),
                  child: AgentResultSheet(workspace: _workspace),
                ),
              ),
            Positioned(
              left: 16,
              right: 16,
              bottom: MediaQuery.paddingOf(context).bottom + 16,
              child: AgentComposer(workspace: _workspace, onSubmit: _submit),
            ),
          ],
        ),
      ),
    ),
  );
}
