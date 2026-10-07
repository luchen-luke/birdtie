import 'package:flutter/material.dart';

import 'community_api.dart';
import 'community_members_controller.dart';

class CommunityMemberManagementSheet extends StatefulWidget {
  const CommunityMemberManagementSheet({super.key, required this.controller});
  final CommunityMembersController controller;
  @override
  State<CommunityMemberManagementSheet> createState() =>
      _CommunityMemberManagementSheetState();
}

class _CommunityMemberManagementSheetState
    extends State<CommunityMemberManagementSheet> {
  String? _friend;
  @override
  void initState() {
    super.initState();
    widget.controller.addListener(_changed);
    widget.controller.reload();
  }

  void _changed() {
    if (mounted) {
      if (!widget.controller.friends.any((f) => f.otherAccountId == _friend)) {
        _friend = null;
      }
      setState(() {});
    }
  }

  @override
  void dispose() {
    widget.controller.removeListener(_changed);
    super.dispose();
  }

  void _prepare(CommunityAction a, String target, String consequence) =>
      widget.controller.prepare(a, target: target, consequence: consequence);
  @override
  Widget build(BuildContext context) {
    final c = widget.controller,
        g = c.item,
        p = c.proposal,
        disabled = c.busy || c.loading;
    return SafeArea(
      child: Padding(
        padding: EdgeInsets.only(
          bottom: MediaQuery.viewInsetsOf(context).bottom,
        ),
        child: SizedBox(
          height: MediaQuery.sizeOf(context).height * .88,
          child: ListView(
            padding: const EdgeInsets.fromLTRB(20, 12, 20, 24),
            children: [
              Row(
                children: [
                  Expanded(
                    child: Text(
                      '成员管理',
                      style: Theme.of(context).textTheme.titleLarge,
                    ),
                  ),
                  IconButton(
                    style: IconButton.styleFrom(
                      minimumSize: const Size(48, 48),
                    ),
                    tooltip: '关闭成员管理',
                    onPressed: () => Navigator.pop(context),
                    icon: const Icon(Icons.close),
                  ),
                ],
              ),
              if (g != null)
                Text('${g.name} · 当前身份：${g.myRole == 'owner' ? '所有者' : '管理员'}'),
              if (c.loading)
                const Padding(
                  padding: EdgeInsets.symmetric(vertical: 24),
                  child: Center(child: CircularProgressIndicator()),
                ),
              if (c.message != null)
                Semantics(
                  liveRegion: true,
                  child: Padding(
                    padding: const EdgeInsets.symmetric(vertical: 12),
                    child: Text(c.message!),
                  ),
                ),
              if (p != null) ...[
                const Divider(height: 32),
                Text('检查操作', style: Theme.of(context).textTheme.titleMedium),
                Text('目标：${p.target}'),
                const SizedBox(height: 8),
                Text(p.consequence),
                const Padding(
                  padding: EdgeInsets.symmetric(vertical: 8),
                  child: Text('尚未提交。确认仅对应本次预览；状态变化后需要重新检查。'),
                ),
                Wrap(
                  spacing: 12,
                  runSpacing: 8,
                  children: [
                    OutlinedButton(
                      onPressed: c.busy ? null : c.cancelProposal,
                      child: const Text('返回修改'),
                    ),
                    FilledButton(
                      onPressed: c.busy ? null : () => c.confirm(),
                      child: Text(c.busy ? '正在核实…' : '确认提交'),
                    ),
                  ],
                ),
                const Divider(height: 32),
              ],
              if (g?.managed == true) ...[
                const SizedBox(height: 20),
                Text('邀请好友', style: Theme.of(context).textTheme.titleMedium),
                const Text('邀请送到对方的社群待处理列表；对方接受后才成为成员。'),
                if (c.friendMessage != null) Text(c.friendMessage!),
                if (c.friends.isEmpty)
                  const Padding(
                    padding: EdgeInsets.symmetric(vertical: 12),
                    child: Text('暂无可邀请好友。可先通过好友入口建立联系。'),
                  )
                else
                  DropdownButtonFormField<String>(
                    key: ValueKey(
                      'community_invite_${c.friends.length}_$_friend',
                    ),
                    initialValue: _friend,
                    isExpanded: true,
                    decoration: const InputDecoration(labelText: '选择邀请对象'),
                    items: c.friends
                        .map(
                          (f) => DropdownMenuItem(
                            value: f.otherAccountId,
                            child: Text(
                              f.otherName.isEmpty ? 'Birdtie 好友' : f.otherName,
                            ),
                          ),
                        )
                        .toList(),
                    onChanged: disabled
                        ? null
                        : (v) => setState(() => _friend = v),
                  ),
                Align(
                  alignment: Alignment.centerLeft,
                  child: FilledButton.icon(
                    onPressed: disabled || _friend == null
                        ? null
                        : () {
                            final f = c.friends.firstWhere(
                              (f) => f.otherAccountId == _friend,
                            );
                            _prepare(
                              CommunityAction.invite(g!.id, f.otherAccountId),
                              f.otherName,
                              '邀请加入“${g.name}”；不会自动加入或创建好友关系。',
                            );
                          },
                    icon: const Icon(Icons.person_add_outlined),
                    label: const Text('检查邀请'),
                  ),
                ),
                const SizedBox(height: 24),
                Text('待处理', style: Theme.of(context).textTheme.titleMedium),
                if (c.requests.isEmpty) const Text('暂无申请或邀请。'),
                for (final m in c.requests)
                  Padding(
                    padding: const EdgeInsets.symmetric(vertical: 8),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          m.displayName,
                          style: Theme.of(context).textTheme.titleSmall,
                        ),
                        Text(m.status == 'invited' ? '已邀请，等待对方接受' : '申请加入'),
                        Wrap(
                          spacing: 8,
                          children: m.status == 'invited'
                              ? [
                                  OutlinedButton(
                                    onPressed: disabled
                                        ? null
                                        : () => _prepare(
                                            CommunityAction.remove(g!.id, m.id),
                                            m.displayName,
                                            '撤销邀请后，对方无法用这份邀请加入。',
                                          ),
                                    child: const Text('检查撤销邀请'),
                                  ),
                                ]
                              : [
                                  TextButton(
                                    onPressed: disabled
                                        ? null
                                        : () => _prepare(
                                            CommunityAction.decide(
                                              g!.id,
                                              m.id,
                                              approve: false,
                                            ),
                                            m.displayName,
                                            '拒绝此次加入申请；不会移除既有好友关系。',
                                          ),
                                    child: const Text('检查拒绝'),
                                  ),
                                  FilledButton(
                                    onPressed: disabled
                                        ? null
                                        : () => _prepare(
                                            CommunityAction.decide(
                                              g!.id,
                                              m.id,
                                              approve: true,
                                            ),
                                            m.displayName,
                                            '通过后，对方可查看成员内容和成员名单。',
                                          ),
                                    child: const Text('检查通过'),
                                  ),
                                ],
                        ),
                      ],
                    ),
                  ),
                const SizedBox(height: 24),
                Text('当前成员', style: Theme.of(context).textTheme.titleMedium),
                for (final m in c.members)
                  ListTile(
                    contentPadding: EdgeInsets.zero,
                    title: Text(m.displayName),
                    subtitle: Text(m.roleLabel),
                    trailing:
                        m.role == 'owner' ||
                            (g!.myRole == 'admin' && m.role != 'member')
                        ? null
                        : PopupMenuButton<String>(
                            tooltip: '管理 ${m.displayName}',
                            enabled: !disabled,
                            onSelected: (v) {
                              switch (v) {
                                case 'remove':
                                  _prepare(
                                    CommunityAction.remove(g.id, m.id),
                                    m.displayName,
                                    '移除后将失去社群成员访问权限；活动报名与好友关系分别保留。',
                                  );
                                case 'admin':
                                case 'member':
                                  _prepare(
                                    CommunityAction.role(g.id, m.id, v),
                                    m.displayName,
                                    v == 'admin'
                                        ? '授予管理员权限，可审批申请和管理普通成员。'
                                        : '改为普通成员，不再拥有管理权限。',
                                  );
                                case 'transfer':
                                  _prepare(
                                    CommunityAction.transfer(
                                      g.id,
                                      m.userAccountId,
                                    ),
                                    m.displayName,
                                    '对方成为所有者，你将变为管理员，失去归档和所有权转让权限。',
                                  );
                              }
                            },
                            itemBuilder: (_) => [
                              const PopupMenuItem(
                                value: 'remove',
                                child: Text('移除成员'),
                              ),
                              ...(g.myRole == 'owner'
                                  ? [
                                      PopupMenuItem(
                                        value: m.role == 'admin'
                                            ? 'member'
                                            : 'admin',
                                        child: Text(
                                          m.role == 'admin'
                                              ? '改为普通成员'
                                              : '设为管理员',
                                        ),
                                      ),
                                      const PopupMenuItem(
                                        value: 'transfer',
                                        child: Text('转让所有权'),
                                      ),
                                    ]
                                  : []),
                            ],
                          ),
                  ),
              ],
              const SizedBox(height: 20),
              OutlinedButton.icon(
                onPressed: disabled ? null : () => c.reload(),
                icon: const Icon(Icons.refresh),
                label: const Text('刷新权威状态'),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
