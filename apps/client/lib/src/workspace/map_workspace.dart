import 'dart:async';

import 'package:flutter/material.dart';

import '../auth/birdtie_auth_controller.dart';
import '../city/public_city_controller.dart';
import '../content/private_moment_controller.dart';
import '../legacy/legacy_shell.dart';
import 'agent_composer.dart';
import 'activity_plans.dart';
import 'connections.dart';
import 'agent_result_sheet.dart';
import 'agent_workspace_controller.dart';
import 'inbox.dart';
import 'group_page.dart';
import 'map_canvas.dart';
import 'remote_agent_task_source.dart';
import 'saved_items.dart';
import 'settings_page.dart';
import 'organization_workspaces.dart';
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
  late final AgentWorkspaceController _workspace;
  late final SavedController _saved;
  late final ActivityPlansController _plans;
  late final ConnectionSource _connections;
  late final OrganizationWorkspaceController _organizations;
  bool _wasSignedIn = false;

  @override
  void initState() {
    super.initState();
    _workspace = AgentWorkspaceController(
      source: RemoteAgentTaskSource.apiBase.isEmpty
          ? const LocalAgentTaskSource()
          : RemoteAgentTaskSource(
              cityID: () => widget.city.selectedCity?.id,
              authorizationHeader: () => widget.auth.authorizationHeader,
              organizationWorkspaceID: () => _organizations.active?.id,
            ),
    );
    _saved = SavedController(
      authorizationHeader: () => widget.auth.authorizationHeader,
    );
    _plans = ActivityPlansController(
      authorizationHeader: () => widget.auth.authorizationHeader,
    );
    _connections = ConnectionSource(
      authorizationHeader: () => widget.auth.authorizationHeader,
    );
    _organizations = OrganizationWorkspaceController(
      authorizationHeader: () => widget.auth.authorizationHeader,
    );
    _organizations.addListener(_refreshWorkspaceTasks);
    widget.auth.addListener(_onAuthChange);
    _wasSignedIn = widget.auth.signedIn;
    if (widget.auth.signedIn) {
      unawaited(_workspace.loadRecent());
      unawaited(_saved.load());
      unawaited(_plans.load());
      unawaited(_organizations.load());
    }
  }

  void _onAuthChange() {
    if (_wasSignedIn == widget.auth.signedIn) return;
    _wasSignedIn = widget.auth.signedIn;
    if (_wasSignedIn) {
      unawaited(_workspace.loadRecent());
      unawaited(_saved.load());
      unawaited(_plans.load());
      unawaited(_organizations.load());
    } else {
      _workspace.clearAccountContext();
      _saved.clear();
      _plans.clear();
      _organizations.clear();
    }
  }

  @override
  void dispose() {
    widget.auth.removeListener(_onAuthChange);
    _organizations.removeListener(_refreshWorkspaceTasks);
    _workspace.dispose();
    _saved.dispose();
    _plans.dispose();
    _connections.dispose();
    _organizations.dispose();
    super.dispose();
  }

  void _refreshWorkspaceTasks() {
    if (!widget.auth.signedIn) return;
    _workspace.clearAccountContext();
    unawaited(_workspace.loadRecent());
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
        child: InboxPanel(auth: widget.auth),
      ),
    );
  }

  Future<void> _createOrganization() async {
    if (!widget.auth.signedIn) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('Sign in to create an organization.')),
      );
      return;
    }
    final name = TextEditingController();
    var type = 'student_society';
    var saving = false;
    await showDialog<void>(
      context: context,
      builder: (dialogContext) => StatefulBuilder(
        builder: (dialogContext, update) => AlertDialog(
          title: const Text('Create organization'),
          content: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              TextField(
                controller: name,
                maxLength: 160,
                decoration: const InputDecoration(
                  labelText: 'Organization name',
                ),
              ),
              DropdownButtonFormField<String>(
                initialValue: type,
                decoration: const InputDecoration(labelText: 'Type'),
                items: const [
                  DropdownMenuItem(
                    value: 'student_society',
                    child: Text('Student society'),
                  ),
                  DropdownMenuItem(value: 'club', child: Text('Club')),
                  DropdownMenuItem(value: 'business', child: Text('Business')),
                  DropdownMenuItem(
                    value: 'university',
                    child: Text('University'),
                  ),
                  DropdownMenuItem(
                    value: 'community',
                    child: Text('Community'),
                  ),
                  DropdownMenuItem(value: 'venue', child: Text('Venue')),
                  DropdownMenuItem(
                    value: 'nonprofit',
                    child: Text('Nonprofit'),
                  ),
                  DropdownMenuItem(value: 'other', child: Text('Other')),
                ],
                onChanged: (value) {
                  if (value != null) update(() => type = value);
                },
              ),
              const SizedBox(height: 8),
              const Text(
                'Your account will become the first owner. Organization workspaces keep their Agent authority separate from your personal data.',
                style: TextStyle(fontSize: 12),
              ),
            ],
          ),
          actions: [
            TextButton(
              onPressed: saving ? null : () => Navigator.pop(dialogContext),
              child: const Text('Cancel'),
            ),
            FilledButton(
              onPressed: saving
                  ? null
                  : () async {
                      update(() => saving = true);
                      try {
                        await _organizations.create(name.text.trim(), type);
                        if (dialogContext.mounted) Navigator.pop(dialogContext);
                      } catch (_) {
                        if (mounted) {
                          ScaffoldMessenger.of(context).showSnackBar(
                            const SnackBar(
                              content: Text(
                                'Could not create organization. Try again.',
                              ),
                            ),
                          );
                        }
                      } finally {
                        if (dialogContext.mounted) update(() => saving = false);
                      }
                    },
              child: const Text('Create'),
            ),
          ],
        ),
      ),
    );
    name.dispose();
  }

  Future<void> _requestContact(AgentPerson person) async {
    if (!widget.auth.signedIn) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('Sign in to request contact.')),
      );
      return;
    }
    final cityID = widget.city.selectedCity?.id;
    if (cityID == null) return;
    final note = TextEditingController();
    var sending = false;
    await showDialog<void>(
      context: context,
      builder: (dialogContext) => StatefulBuilder(
        builder: (dialogContext, update) => AlertDialog(
          title: Text('Request contact with ${person.displayName}'),
          content: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              const Text(
                'They choose whether to accept. Your note and display name will be shared with them.',
              ),
              TextField(
                controller: note,
                maxLength: 280,
                maxLines: 3,
                decoration: const InputDecoration(labelText: 'Your message'),
              ),
            ],
          ),
          actions: [
            TextButton(
              onPressed: sending ? null : () => Navigator.pop(dialogContext),
              child: const Text('Cancel'),
            ),
            FilledButton(
              onPressed: sending
                  ? null
                  : () async {
                      final body = note.text.trim();
                      if (body.isEmpty) return;
                      update(() => sending = true);
                      try {
                        await _connections.request(
                          person.accountID,
                          cityID,
                          body,
                        );
                        if (dialogContext.mounted) Navigator.pop(dialogContext);
                        if (mounted) {
                          ScaffoldMessenger.of(context).showSnackBar(
                            const SnackBar(
                              content: Text(
                                'Contact request sent. Check Inbox for the response.',
                              ),
                            ),
                          );
                        }
                      } catch (_) {
                        if (mounted) {
                          ScaffoldMessenger.of(context).showSnackBar(
                            const SnackBar(
                              content: Text(
                                'Request unavailable. Check eligibility or try later.',
                              ),
                            ),
                          );
                        }
                      } finally {
                        if (dialogContext.mounted) {
                          update(() => sending = false);
                        }
                      }
                    },
              child: const Text('Send request'),
            ),
          ],
        ),
      ),
    );
    note.dispose();
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
      SidebarDestination.activities => MyActivitiesPage(plans: _plans),
      SidebarDestination.profile => LegacyProfilePage(
        auth: widget.auth,
        moments: widget.moments,
        city: widget.city,
      ),
      SidebarDestination.groups => GroupPage(
        auth: widget.auth,
        city: widget.city,
      ),
      SidebarDestination.saved => SavedPage(saved: _saved),
      SidebarDestination.settings => SettingsPage(
        auth: widget.auth,
        city: widget.city,
        moments: widget.moments,
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
        onCitySelected: (id) {
          _workspace.newTask();
          widget.city.selectCity(id);
        },
        onNew: () {
          _workspace.newTask();
          Navigator.pop(context);
        },
        onDestination: _destination,
        onRecent: (task) {
          Navigator.pop(context);
          if (task.cityID != null) widget.city.selectCity(task.cityID!);
          unawaited(
            _workspace.reopen(task, widget.city.activities, widget.city.places),
          );
        },
        organizations: _organizations,
        onCreateOrganization: _createOrganization,
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
                  child: AgentResultSheet(
                    workspace: _workspace,
                    saved: _saved,
                    plans: _plans,
                    onContact: _requestContact,
                  ),
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
