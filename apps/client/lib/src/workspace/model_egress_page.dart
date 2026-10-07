import 'dart:async';
import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import '../auth/birdtie_auth_controller.dart';
import 'model_egress_api.dart';
import 'model_egress_controller.dart';

class ModelEgressPage extends StatefulWidget {
  const ModelEgressPage({
    super.key,
    required this.auth,
    this.client,
    this.apiBaseUrl,
    this.workspaceChanges,
    this.organizationWorkspaceID,
  });
  final BirdtieAuthController auth;
  final http.Client? client;
  final String? apiBaseUrl;
  final Listenable? workspaceChanges;
  final String? Function()? organizationWorkspaceID;
  @override
  State<ModelEgressPage> createState() => _ModelEgressPageState();
}

class _ModelEgressPageState extends State<ModelEgressPage> {
  ModelEgressController _createController() => ModelEgressController(
    api: ModelEgressAPI(client: widget.client, apiBaseUrl: widget.apiBaseUrl),
    authorizationHeader: () => widget.auth.authorizationHeader,
    accountID: () => widget.auth.accountID,
    organizationWorkspaceID: () => widget.organizationWorkspaceID?.call(),
  );
  late ModelEgressController _data = _createController();
  Route<bool>? _confirmation;
  @override
  void initState() {
    super.initState();
    widget.auth.addListener(_identity);
    widget.workspaceChanges?.addListener(_identity);
    _data.addListener(_changed);
    unawaited(_data.load());
  }

  void _changed() {
    if (mounted) setState(() {});
  }

  void _identity() {
    final before = _data.generation;
    _data.synchronizeIdentity();
    if (before != _data.generation) {
      final dialog = _confirmation;
      _confirmation = null;
      if (dialog?.isActive ?? false) Navigator.of(context).removeRoute(dialog!);
      if (_data.personal) {
        unawaited(_data.load());
      } else {
        _changed();
      }
    }
  }

  @override
  void didUpdateWidget(ModelEgressPage old) {
    super.didUpdateWidget(old);
    if (old.auth != widget.auth) {
      old.auth.removeListener(_identity);
      widget.auth.addListener(_identity);
    }
    if (old.workspaceChanges != widget.workspaceChanges) {
      old.workspaceChanges?.removeListener(_identity);
      widget.workspaceChanges?.addListener(_identity);
    }
    if (old.auth != widget.auth ||
        old.client != widget.client ||
        old.apiBaseUrl != widget.apiBaseUrl ||
        old.workspaceChanges != widget.workspaceChanges ||
        old.organizationWorkspaceID != widget.organizationWorkspaceID) {
      final dialog = _confirmation;
      _confirmation = null;
      if (dialog?.isActive ?? false) {
        Navigator.of(context).removeRoute(dialog!);
      }
      _data.removeListener(_changed);
      _data.dispose();
      _data = _createController();
      _data.addListener(_changed);
      unawaited(_data.load());
    } else {
      _identity();
    }
  }

  @override
  void dispose() {
    widget.auth.removeListener(_identity);
    widget.workspaceChanges?.removeListener(_identity);
    _data.removeListener(_changed);
    _data.dispose();
    super.dispose();
  }

  String _time(DateTime v) {
    final l = v.toLocal();
    return '${l.year}年${l.month}月${l.day}日 ${l.hour}:${l.minute.toString().padLeft(2, '0')}:${l.second.toString().padLeft(2, '0')}（本地时间）';
  }

  Widget _button(String label, VoidCallback? action) => Padding(
    padding: const EdgeInsets.symmetric(vertical: 6),
    child: SizedBox(
      width: double.infinity,
      child: OutlinedButton(
        onPressed: action,
        child: Padding(
          padding: const EdgeInsets.symmetric(vertical: 12),
          child: Text(label, textAlign: TextAlign.center),
        ),
      ),
    ),
  );
  Future<void> _approve() async {
    final data = _data;
    final p = _data.preview, g = _data.generation;
    if (p == null || _confirmation != null || _data.busy) return;
    final dialog = DialogRoute<bool>(
      context: context,
      builder: (inner) => AlertDialog(
        title: const Text('确认此版本的本地许可'),
        content: SingleChildScrollView(
          child: Text(
            '主体：${widget.auth.displayName ?? '当前个人账号'}（本人）\n'
            '任务查询：${(p.data['request']['messages'] as List).where((m) => m['role'] == 'user').map((m) => m['content']).join('\n')}\n'
            '目标：${p.data['destination']['Provider']} / ${p.data['destination']['Model']} / ${p.data['destination']['Version']}\n'
            '地区：${const {'UK': '英国', 'EU': '欧盟', 'US': '美国', 'APAC': '亚太'}[p.data['region']]}\n'
            '保留：无状态、不存储（未核验供应商）\n'
            '提示词版本：${p.data['request']['prompt_version']}\n'
            '价格版本：${p.data['priceVersion']}\n'
            '费用上界：${(p.data['upper']['costMicros'] as int) / 1000000} ${p.data['currency']}（本地合成估计）\n'
            '期限：${_time(p.expiry)}\n\n'
            '只确认此具体版本的本地许可。模型服务当前不可用，不会执行网络请求、收费、写入记忆或报名。',
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(inner, false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(inner, true),
            child: const Text('确认本地许可'),
          ),
        ],
      ),
    );
    _confirmation = dialog;
    final yes = await Navigator.of(context).push(dialog);
    if (_confirmation == dialog) _confirmation = null;
    if (!mounted) return;
    _data.synchronizeIdentity();
    if (yes == true &&
        identical(data, _data) &&
        g == _data.generation &&
        identical(_data.preview, p)) {
      await _data.approve();
    }
  }

  @override
  Widget build(BuildContext context) {
    final enabled = !_data.busy, p = _data.preview;
    return Scaffold(
      appBar: AppBar(title: const Text('模型请求与预算')),
      body: ListView(
        padding: const EdgeInsets.all(20),
        children: [
          const Text(
            '本人任务的人审记录',
            style: TextStyle(fontSize: 22, fontWeight: FontWeight.w700),
          ),
          const Padding(
            padding: EdgeInsets.symmetric(vertical: 12),
            child: Text(
              '模型服务当前不可用。这里仅检查具体请求、确认或撤回本地许可，查看已有预算；不会调用模型或收费。价格与 token 上界为本地合成配置，不是供应商计费证明。',
            ),
          ),
          if (!_data.personal)
            const Text('请使用已登录的个人身份查看；组织工作台不能沿用此许可。')
          else ...[
            if (_data.busy) const LinearProgressIndicator(),
            if (_data.message != null)
              Semantics(
                liveRegion: true,
                child: Padding(
                  padding: const EdgeInsets.symmetric(vertical: 12),
                  child: Text(_data.message!),
                ),
              ),
            _button('刷新已有配置与原记录', enabled ? () => _data.load() : null),
            if (_data.options.isEmpty && !_data.busy)
              const Text('尚无可用的本人任务配置。需要已有任务、固定配置、价格依据和预算；这里不会自动创建或增加额度。'),
            if (_data.options.isNotEmpty)
              const Text(
                '选择已配置的本人任务',
                style: TextStyle(fontWeight: FontWeight.w700),
              ),
            for (final o in _data.options)
              ListTile(
                contentPadding: EdgeInsets.zero,
                title: Text(o.query),
                subtitle: Text(
                  '${o.data['destination']['Provider']} / ${o.data['destination']['Model']} · ${o.data['currency']}本地合成配置',
                ),
                selected: _data.selected?.key == o.key,
                onTap: enabled && !_data.unknown && !_data.unknownPreview
                    ? () => _data.choose(o)
                    : null,
              ),
            for (final b in _data.budgets)
              Padding(
                padding: const EdgeInsets.symmetric(vertical: 5),
                child: Text(
                  '${const {'TENANT_PERSON': '个人租户', 'SUBJECT_PERSON': '本人主体', 'ROOT': '任务组', 'TASK': '所选任务'}[b.data['scope']]}\n'
                  '已用尝试 ${b.data['allocated']['requests']} / ${b.data['limits']['requests']}\n'
                  '输入 token ${b.data['allocated']['inputTokens']} / ${b.data['limits']['inputTokens']}；输出 token ${b.data['allocated']['outputTokens']} / ${b.data['limits']['outputTokens']}\n'
                  '已分配费用 ${(b.data['allocated']['costMicros'] as int) / 1000000} / ${(b.data['limits']['costMicros'] as int) / 1000000} ${b.data['currency']}（合成账目）',
                ),
              ),
            if (_data.selected != null)
              _button(
                '检查具体请求版本',
                enabled &&
                        !_data.unknown &&
                        !_data.unknownPreview &&
                        _data.budgets.length == 4
                    ? () => _data.prepare()
                    : null,
              ),
            if (p != null) ...[
              const Divider(height: 32),
              const Text(
                '具体请求预览',
                style: TextStyle(fontSize: 20, fontWeight: FontWeight.w700),
              ),
              Text(
                '目标：${p.data['destination']['Provider']} / ${p.data['destination']['Model']} / ${p.data['destination']['Version']}',
              ),
              Text(
                '地区：${const {'UK': '英国', 'EU': '欧盟', 'US': '美国', 'APAC': '亚太'}[p.data['region']]}；保留策略：无状态、不存储（本地合同，未核验供应商）',
              ),
              Text(
                '提示词版本：${p.data['request']['prompt_version']}\n输入版本：${p.data['request']['input_schema_version']}\n输出版本：${p.data['request']['output_schema_version']}\n价格版本：${p.data['priceVersion']}',
              ),
              Text(
                '输入上界 ${p.data['upper']['inputTokens']}，输出上界 ${p.data['upper']['outputTokens']} token；费用上界 ${(p.data['upper']['costMicros'] as int) / 1000000} ${p.data['currency']}（合成估计，未分配）',
              ),
              Text('具体许可期限：${_time(p.expiry)}'),
              for (final m in p.data['request']['messages'])
                Padding(
                  padding: const EdgeInsets.symmetric(vertical: 10),
                  child: SelectableText(
                    '${m['role'] == 'user' ? '本人查询' : '系统提示词'}\n${m['content']}',
                  ),
                ),
              if (p.data['status'] == 'DRAFT')
                _button(
                  '确认此版本的本地许可',
                  enabled && !_data.unknown ? () => _approve() : null,
                ),
              _button('退出检查', enabled ? () => _data.cancelReview() : null),
            ],
            if (_data.pendingID != null)
              _button(
                '核实同一原记录',
                enabled ? () => _data.inspect(_data.pendingID!) : null,
              ),
            if (_data.current?.revocable == true)
              _button(
                '撤回这项原记录',
                enabled ? () => _data.revoke(_data.current!.id) : null,
              ),
            const Divider(height: 32),
            const Text(
              '已有的人审记录',
              style: TextStyle(fontSize: 20, fontWeight: FontWeight.w700),
            ),
            const Text('最近 50 项；重新读取原记录才能判断是否仍可检查。换会话后旧批准不会恢复。'),
            for (final r in _data.receipts)
              ListTile(
                contentPadding: EdgeInsets.zero,
                title: Text(
                  '${const {'DRAFT': '待检查', 'APPROVED': '已确认本地许可', 'REVOKED': '已撤回'}[r.status]} · ${r.data['priceVersion']}',
                ),
                subtitle: Text('期限：${_time(egressTime(r.data['expiresAt']))}'),
                trailing: const Icon(Icons.chevron_right),
                onTap: enabled ? () => _data.inspect(r.id) : null,
              ),
          ],
        ],
      ),
    );
  }
}
