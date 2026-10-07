import 'package:flutter/material.dart';

import '../city/public_city_controller.dart';
import 'agent_workspace_controller.dart';
import 'organization_workspaces.dart';

enum SidebarDestination {
  home,
  activities,
  organization,
  business,
  groups,
  saved,
  profile,
  settings,
}

String _workspaceRoleLabel(String role) => switch (role.toLowerCase()) {
  'owner' => '所有者',
  'admin' => '管理员',
  'moderator' => '协管员',
  'member' => '成员',
  _ => '权限待确认',
};

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
    required this.onViewInvitations,
    this.onChooseCity,
    this.onTools,
    this.onWorkspaceSelected,
    this.signedIn = true,
  });
  final AgentWorkspaceController workspace;
  // Retained for callers; the Now top bar owns the only visible city selector.
  final PublicCityController city;
  final ValueChanged<String> onCitySelected;
  final VoidCallback onNew;
  final ValueChanged<SidebarDestination> onDestination;
  final ValueChanged<AgentTask> onRecent;
  final OrganizationWorkspaceController organizations;
  final VoidCallback onCreateOrganization;
  final VoidCallback onViewInvitations;
  final VoidCallback? onChooseCity;
  final VoidCallback? onTools;
  final VoidCallback? onWorkspaceSelected;
  final bool signedIn;

  bool get _maySwitch =>
      signedIn && organizations.authorizationHeader() != null;

  void _accountSelected(String value) {
    if (value == 'tools') {
      onTools?.call();
      return;
    }
    if (value == 'profile' || value == 'settings') {
      onDestination(
        value == 'profile'
            ? SidebarDestination.profile
            : SidebarDestination.settings,
      );
      return;
    }
    // Options shown before a popup closes are not an enduring authority grant.
    if (!_maySwitch) return;
    if (value == 'personal') {
      organizations.select(null);
      onWorkspaceSelected?.call();
    } else if (value == 'create') {
      onCreateOrganization();
    } else if (value == 'invitations') {
      onViewInvitations();
    } else if (value.startsWith('organization:')) {
      final id = value.substring('organization:'.length);
      for (final item in organizations.organizations) {
        if (item.id == id && item.canManage) {
          organizations.select(item);
          onWorkspaceSelected?.call();
          return;
        }
      }
    }
  }

  @override
  Widget build(BuildContext context) => Drawer(
    backgroundColor: const Color(0xFFFCFBF8),
    child: SafeArea(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          const Padding(
            padding: EdgeInsets.fromLTRB(24, 22, 18, 12),
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
          Expanded(
            child: AnimatedBuilder(
              animation: Listenable.merge([workspace, organizations]),
              builder: (context, _) => ListView(
                key: const PageStorageKey('sidebar-navigation-history'),
                padding: EdgeInsets.zero,
                children: [
                  _item(
                    Icons.map_outlined,
                    'Now',
                    () => onDestination(SidebarDestination.home),
                  ),
                  if (signedIn) ...[
                    ExpansionTile(
                      key: const PageStorageKey('sidebar-my-content'),
                      leading: const Icon(Icons.folder_open_outlined),
                      title: const Text('我的内容'),
                      children: [
                        _item(
                          Icons.event_outlined,
                          '我的活动',
                          () => onDestination(SidebarDestination.activities),
                        ),
                        _item(
                          Icons.group_outlined,
                          '社群',
                          () => onDestination(SidebarDestination.groups),
                        ),
                        _item(
                          Icons.bookmark_outline,
                          '收藏',
                          () => onDestination(SidebarDestination.saved),
                        ),
                        // Merchant permissions come from the native Business
                        // API, independently of Organization roles.
                        if (organizations.active == null)
                          _item(
                            Icons.storefront_outlined,
                            '我的商家与认领',
                            () => onDestination(SidebarDestination.business),
                          ),
                      ],
                    ),
                    if (organizations.active case final active?)
                      if (active.canManage)
                        _item(
                          Icons.dashboard_outlined,
                          '组织活动管理',
                          () => onDestination(SidebarDestination.organization),
                        ),
                  ],
                  const Divider(height: 25, indent: 24, endIndent: 24),
                  const Padding(
                    padding: EdgeInsets.fromLTRB(24, 0, 24, 8),
                    child: Text(
                      '最近对话',
                      style: TextStyle(
                        fontSize: 12,
                        fontWeight: FontWeight.w600,
                        color: Color(0xFF747B73),
                      ),
                    ),
                  ),
                  if (workspace.recentLoading)
                    const Padding(
                      padding: EdgeInsets.fromLTRB(24, 8, 24, 8),
                      child: Text('正在读取最近对话…'),
                    )
                  else if (workspace.recentError != null)
                    Padding(
                      padding: const EdgeInsets.fromLTRB(24, 8, 24, 8),
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          const Text('最近对话读取失败'),
                          Text(workspace.recentError!),
                          if (workspace.recentFailure?.statusCode == 401)
                            const Text('请先通过账户重新登录，再读取最近对话。')
                          else if (workspace.recentFailure?.statusCode == 403)
                            const Text('请先确认当前身份和访问权限；恢复权限后再读取最近对话。'),
                          if (workspace.permitsRecentRetry)
                            TextButton(
                              key: const Key('sidebar-retry-history'),
                              style: TextButton.styleFrom(
                                minimumSize: const Size(0, 48),
                              ),
                              onPressed: workspace.retryRecent,
                              child: const Text('重新读取最近对话'),
                            ),
                        ],
                      ),
                    )
                  else if (workspace.recent.isEmpty)
                    const Padding(
                      padding: EdgeInsets.fromLTRB(24, 8, 24, 8),
                      child: Text(
                        '你开始的对话会显示在这里。',
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
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 12, 16, 12),
            child: Row(
              children: [
                Expanded(
                  child: FilledButton.icon(
                    key: const Key('sidebar-new-conversation'),
                    onPressed: onNew,
                    icon: const Icon(Icons.add, size: 19),
                    label: const Text('新建对话'),
                    style: FilledButton.styleFrom(
                      backgroundColor: const Color(0xFF193B32),
                      minimumSize: const Size.fromHeight(48),
                      padding: const EdgeInsets.symmetric(
                        horizontal: 12,
                        vertical: 12,
                      ),
                    ),
                  ),
                ),
                const SizedBox(width: 12),
                AnimatedBuilder(
                  animation: organizations,
                  builder: (context, _) => PopupMenuButton<String>(
                    key: const Key('sidebar-account'),
                    tooltip: !signedIn
                        ? '登录 / 账户 · 未登录'
                        : '账户 · ${organizations.active?.name ?? '个人身份'}',
                    onSelected: _accountSelected,
                    itemBuilder: (context) => [
                      PopupMenuItem(
                        value: 'profile',
                        child: Text(signedIn ? '个人资料与账户' : '登录 / 账户'),
                      ),
                      const PopupMenuItem(value: 'settings', child: Text('设置')),
                      if (onTools != null)
                        const PopupMenuItem(
                          value: 'tools',
                          child: Text('更多工具'),
                        ),
                      if (_maySwitch) ...[
                        const PopupMenuDivider(),
                        const PopupMenuItem(
                          value: 'invitations',
                          child: Text('我的组织邀请'),
                        ),
                        const PopupMenuItem(
                          value: 'create',
                          child: Text('创建组织'),
                        ),
                        if (organizations.active != null ||
                            organizations.organizations.any(
                              (o) => o.canManage,
                            )) ...[
                          const PopupMenuDivider(),
                          const PopupMenuItem(
                            enabled: false,
                            child: Text('选择本次工作身份'),
                          ),
                          const PopupMenuItem(
                            value: 'personal',
                            child: Text('以个人身份使用'),
                          ),
                          for (final item in organizations.organizations)
                            if (item.canManage)
                              PopupMenuItem(
                                value: 'organization:${item.id}',
                                child: Text(
                                  '以 ${item.name} 身份使用 · ${_workspaceRoleLabel(item.role)}',
                                ),
                              ),
                        ],
                      ],
                    ],
                    child: ConstrainedBox(
                      constraints: const BoxConstraints(
                        minWidth: 64,
                        minHeight: 48,
                      ),
                      child: Padding(
                        padding: const EdgeInsets.symmetric(
                          horizontal: 8,
                          vertical: 6,
                        ),
                        child: Column(
                          mainAxisSize: MainAxisSize.min,
                          children: [
                            const Icon(
                              Icons.account_circle_outlined,
                              size: 28,
                              color: Color(0xFF193B32),
                            ),
                            const SizedBox(height: 4),
                            Text(
                              signedIn ? '账户' : '登录',
                              style: const TextStyle(
                                color: Color(0xFF193B32),
                                fontSize: 12,
                              ),
                            ),
                          ],
                        ),
                      ),
                    ),
                  ),
                ),
              ],
            ),
          ),
        ],
      ),
    ),
  );

  Widget _item(IconData icon, String label, VoidCallback onTap) => ListTile(
    minTileHeight: 48,
    contentPadding: const EdgeInsets.symmetric(horizontal: 24),
    leading: Icon(icon, color: const Color(0xFF193B32), size: 21),
    title: Text(
      label,
      maxLines: 2,
      overflow: TextOverflow.ellipsis,
      style: const TextStyle(fontSize: 14, color: Color(0xFF193B32)),
    ),
    onTap: onTap,
  );
}
