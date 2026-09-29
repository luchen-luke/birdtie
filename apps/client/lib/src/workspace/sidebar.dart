import 'package:flutter/material.dart';

import '../city/public_city_controller.dart';
import 'agent_workspace_controller.dart';
import 'organization_workspaces.dart';

enum SidebarDestination { home, activities, groups, saved, profile, settings }

class Sidebar extends StatelessWidget {
  const Sidebar({
    super.key,
    required this.workspace,
    required this.city,
    required this.onCitySelected,
    required this.onNew,
    required this.onDestination,
    required this.onRecent,
    required this.organizations,
    required this.onCreateOrganization,
  });
  final AgentWorkspaceController workspace;
  final PublicCityController city;
  final ValueChanged<String> onCitySelected;
  final VoidCallback onNew;
  final ValueChanged<SidebarDestination> onDestination;
  final ValueChanged<AgentTask> onRecent;
  final OrganizationWorkspaceController organizations;
  final VoidCallback onCreateOrganization;

  @override
  Widget build(BuildContext context) => Drawer(
    backgroundColor: const Color(0xFFFCFBF8),
    child: SafeArea(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          const Padding(
            padding: EdgeInsets.fromLTRB(24, 22, 18, 18),
            child: Text(
              'birdtie',
              style: TextStyle(
                fontSize: 28,
                fontWeight: FontWeight.w700,
                letterSpacing: -1.2,
                color: Color(0xFF193B32),
              ),
            ),
          ),
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 16),
            child: FilledButton.icon(
              onPressed: onNew,
              icon: const Icon(Icons.add, size: 19),
              label: const Text('New'),
              style: FilledButton.styleFrom(
                backgroundColor: const Color(0xFF193B32),
                minimumSize: const Size.fromHeight(44),
              ),
            ),
          ),
          const SizedBox(height: 6),
          AnimatedBuilder(
            animation: organizations,
            builder: (context, _) => Padding(
              padding: const EdgeInsets.symmetric(horizontal: 16),
              child: PopupMenuButton<String>(
                onSelected: (value) {
                  if (value == '__personal') {
                    organizations.select(null);
                  } else if (value == '__create') {
                    onCreateOrganization();
                  } else {
                    for (final item in organizations.organizations) {
                      if (item.id == value) organizations.select(item);
                    }
                  }
                },
                itemBuilder: (context) => [
                  const PopupMenuItem(
                    value: '__personal',
                    child: Text('Personal Workspace'),
                  ),
                  for (final item in organizations.organizations)
                    PopupMenuItem(
                      value: item.id,
                      child: Text('${item.name} · ${item.role.toUpperCase()}'),
                    ),
                  const PopupMenuDivider(),
                  const PopupMenuItem(
                    value: '__create',
                    child: Text('Create organization'),
                  ),
                ],
                child: ListTile(
                  dense: true,
                  shape: RoundedRectangleBorder(
                    borderRadius: BorderRadius.circular(12),
                  ),
                  tileColor: const Color(0xFFF0F1EC),
                  leading: Icon(
                    organizations.active == null
                        ? Icons.person_outline
                        : Icons.apartment_outlined,
                  ),
                  title: Text(
                    organizations.active?.name ?? 'Personal Workspace',
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                  ),
                  subtitle: Text(
                    organizations.active == null
                        ? 'Personal Agent'
                        : 'Organization Agent · ${organizations.active!.role.toUpperCase()}',
                  ),
                  trailing: const Icon(Icons.unfold_more, size: 18),
                ),
              ),
            ),
          ),
          if (city.selectedCity != null)
            Padding(
              padding: const EdgeInsets.fromLTRB(24, 0, 24, 10),
              child: PopupMenuButton<String>(
                enabled: city.cities.length > 1,
                onSelected: onCitySelected,
                itemBuilder: (context) => [
                  for (final option in city.cities)
                    PopupMenuItem(value: option.id, child: Text(option.name)),
                ],
                child: Row(
                  children: [
                    const Icon(
                      Icons.location_on_outlined,
                      size: 18,
                      color: Color(0xFF747B73),
                    ),
                    const SizedBox(width: 8),
                    Text(
                      city.selectedCity!.name,
                      style: const TextStyle(color: Color(0xFF193B32)),
                    ),
                    if (city.cities.length > 1)
                      const Icon(Icons.keyboard_arrow_down, size: 18),
                  ],
                ),
              ),
            ),
          _item(
            Icons.map_outlined,
            'Home',
            () => onDestination(SidebarDestination.home),
          ),
          _item(
            Icons.event_outlined,
            'My Activities',
            () => onDestination(SidebarDestination.activities),
          ),
          _item(
            Icons.group_outlined,
            'Groups',
            () => onDestination(SidebarDestination.groups),
          ),
          _item(
            Icons.bookmark_outline,
            'Saved',
            () => onDestination(SidebarDestination.saved),
          ),
          const Divider(height: 25, indent: 24, endIndent: 24),
          const Padding(
            padding: EdgeInsets.fromLTRB(24, 0, 24, 8),
            child: Text(
              'RECENT AGENT TASKS',
              style: TextStyle(
                fontSize: 10,
                letterSpacing: 1.2,
                fontWeight: FontWeight.w700,
                color: Color(0xFF747B73),
              ),
            ),
          ),
          Expanded(
            child: AnimatedBuilder(
              animation: workspace,
              builder: (context, _) => ListView(
                children: [
                  if (workspace.recent.isEmpty)
                    const Padding(
                      padding: EdgeInsets.fromLTRB(24, 8, 24, 8),
                      child: Text(
                        'Tasks you start will appear here.',
                        style: TextStyle(
                          fontSize: 12,
                          color: Color(0xFF747B73),
                        ),
                      ),
                    ),
                  for (final task in workspace.recent)
                    _item(
                      Icons.chat_bubble_outline,
                      task.query,
                      () => onRecent(task),
                    ),
                ],
              ),
            ),
          ),
          const Divider(height: 1, indent: 24, endIndent: 24),
          _item(
            Icons.person_outline,
            'Profile',
            () => onDestination(SidebarDestination.profile),
          ),
          _item(
            Icons.settings_outlined,
            'Settings',
            () => onDestination(SidebarDestination.settings),
          ),
          const SizedBox(height: 12),
        ],
      ),
    ),
  );

  Widget _item(IconData icon, String label, VoidCallback onTap) => ListTile(
    contentPadding: const EdgeInsets.symmetric(horizontal: 24),
    leading: Icon(icon, color: const Color(0xFF193B32), size: 21),
    title: Text(
      label,
      maxLines: 1,
      overflow: TextOverflow.ellipsis,
      style: const TextStyle(fontSize: 14, color: Color(0xFF193B32)),
    ),
    onTap: onTap,
  );
}
