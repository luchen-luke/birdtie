import 'package:flutter/material.dart';

import '../city/public_city_controller.dart';
import 'agent_workspace_controller.dart';
import 'organization_workspaces.dart';
import 'now_glass_surface.dart';
import 'now_ui_tokens.dart';

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
    this.onJourneys,
    this.onWorkspaceSelected,
    this.signedIn = true,
    this.sessionRestoring = false,
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
  final VoidCallback? onJourneys;
  final VoidCallback? onWorkspaceSelected;
  final bool signedIn;
  final bool sessionRestoring;

  bool get _maySwitch =>
      signedIn &&
      !sessionRestoring &&
      organizations.authorizationHeader() != null;

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
    width: (MediaQuery.sizeOf(context).width - NowUiTokens.actionHitSize)
        .clamp(
          0.0,
          (NowUiTokens.sidebarWidth +
                  (MediaQuery.textScalerOf(context).scale(16) - 16) * 3)
              .clamp(NowUiTokens.sidebarWidth, NowUiTokens.sidebarMaxWidth),
        )
        .toDouble(),
    backgroundColor: Colors.transparent,
    elevation: 0,
    shape: const RoundedRectangleBorder(
      borderRadius: BorderRadius.horizontal(
        right: Radius.circular(NowUiTokens.sheetCornerTop),
      ),
    ),
    clipBehavior: Clip.antiAlias,
    child: NowGlassSurface(
      role: NowGlassRole.navigation,
      radius: 0,
      backdropBlur: false,
      child: SafeArea(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(24, 12, 12, 8),
              child: Row(
                children: [
                  Expanded(
                    child: Text(
                      'birdtie',
                      style: NowUiTokens.of(context).pageTitle,
                    ),
                  ),
                  IconButton(
                    key: const Key('sidebar-close'),
                    tooltip: '关闭侧边栏',
                    onPressed: Scaffold.maybeOf(context)?.closeDrawer,
                    style: IconButton.styleFrom(
                      minimumSize: const Size.square(NowUiTokens.actionHitSize),
                      tapTargetSize: MaterialTapTargetSize.padded,
                    ),
                    icon: const Icon(Icons.close, size: 20),
                  ),
                ],
              ),
            ),
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 0, 16, 16),
              child: FilledButton.icon(
                key: const Key('sidebar-new-conversation'),
                onPressed: onNew,
                icon: const Icon(Icons.add, size: 20),
                label: const Text('新建对话'),
                style: FilledButton.styleFrom(
                  backgroundColor: Theme.of(context).colorScheme.primary,
                  minimumSize: const Size.fromHeight(NowUiTokens.actionHitSize),
                  padding: const EdgeInsets.symmetric(
                    horizontal: 12,
                    vertical: 12,
                  ),
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
                    if (signedIn && !sessionRestoring) ...[
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
                          if (organizations.active == null &&
                              onJourneys != null)
                            _item(Icons.luggage_outlined, '我的旅程', onJourneys!),
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
                            () =>
                                onDestination(SidebarDestination.organization),
                          ),
                    ],
                    if (workspace.recentLoading ||
                        workspace.recentError != null ||
                        workspace.recent.isNotEmpty) ...[
                      const Divider(height: 25, indent: 24, endIndent: 24),
                      Padding(
                        padding: EdgeInsets.fromLTRB(24, 0, 24, 8),
                        child: Text(
                          '最近对话',
                          style: Theme.of(context).textTheme.bodySmall,
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
                              Text(
                                switch (workspace.recentFailure?.statusCode) {
                                  401 => '登录已失效，请重新登录。',
                                  403 => '当前身份无法查看这些对话。',
                                  _ => '最近对话加载失败。',
                                },
                              ),
                              if (workspace.permitsRecentRetry)
                                TextButton(
                                  key: const Key('sidebar-retry-history'),
                                  style: TextButton.styleFrom(
                                    minimumSize: const Size(0, 48),
                                  ),
                                  onPressed: workspace.retryRecent,
                                  child: const Text('重试'),
                                ),
                            ],
                          ),
                        ),
                    ],
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
                    child: Text('账户', style: NowUiTokens.of(context).chrome),
                  ),
                  const SizedBox(width: 12),
                  AnimatedBuilder(
                    animation: organizations,
                    builder: (context, _) => PopupMenuButton<String>(
                      key: const Key('sidebar-account'),
                      tooltip: sessionRestoring
                          ? '正在恢复账户'
                          : !signedIn
                          ? '登录 / 账户 · 未登录'
                          : '账户 · ${organizations.active?.name ?? '个人身份'}',
                      onSelected: _accountSelected,
                      itemBuilder: (context) => [
                        PopupMenuItem(
                          value: 'profile',
                          enabled: !sessionRestoring,
                          child: Text(
                            sessionRestoring
                                ? '正在恢复账户'
                                : signedIn
                                ? '个人资料与账户'
                                : '登录 / 账户',
                          ),
                        ),
                        const PopupMenuItem(
                          value: 'settings',
                          child: Text('设置'),
                        ),
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
                      child: Semantics(
                        button: true,
                        child: SizedBox.square(
                          dimension: 48,
                          child: Center(
                            child: Icon(
                              Icons.account_circle_outlined,
                              size: 28,
                              color: Theme.of(context).colorScheme.primary,
                            ),
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
    ),
  );

  Widget _item(IconData icon, String label, VoidCallback onTap) => Builder(
    builder: (context) => ListTile(
      minTileHeight: 48,
      contentPadding: const EdgeInsets.symmetric(horizontal: 24),
      leading: Icon(
        icon,
        color: Theme.of(context).colorScheme.primary,
        size: 21,
      ),
      title: Text(label, style: Theme.of(context).textTheme.titleSmall),
      onTap: onTap,
    ),
  );
}
