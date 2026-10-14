import 'dart:async';
import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import 'package:uuid/uuid.dart';
import '../app/birdtie_app_bar.dart';
import '../config/birdtie_environment.dart';
import 'human_causal_control.dart';
import 'human_causal_pending_store.dart';
import 'human_causal_operation_controller.dart';

class PrivacyControlPage extends StatefulWidget {
  const PrivacyControlPage({
    super.key,
    required this.family,
    required this.targetID,
    required this.authorizationHeader,
    this.ownerID,
    this.identityChanges,
    this.sourceCurrent,
    this.apiBaseUrl,
    this.client,
    this.pendingStore,
  });
  final HumanCausalFamily family;
  final String targetID;
  final String? Function() authorizationHeader;
  final String? Function()? ownerID;
  final Listenable? identityChanges;
  final bool Function()? sourceCurrent;
  final String? apiBaseUrl;
  final http.Client? client;
  final HumanCausalPendingStore? pendingStore;
  @override
  State<PrivacyControlPage> createState() => _PrivacyControlPageState();
}

class _PrivacyControlPageState extends State<PrivacyControlPage> {
  String _expiry(Object value) {
    final time = DateTime.tryParse(value.toString())?.toLocal();
    if (time == null) return '期限暂不可读';
    String two(int n) => n.toString().padLeft(2, '0');
    return '${time.year}年${time.month}月${time.day}日 ${two(time.hour)}:${two(time.minute)}（本机时间）';
  }

  late http.Client _client;
  late bool _ownsClient;
  late HumanCausalOperationController _data;
  void _bind() {
    if (!const {
      HumanCausalFamily.block,
      HumanCausalFamily.grantRevoke,
    }.contains(widget.family))
      throw ArgumentError('privacy control family');
    _client = widget.client ?? http.Client();
    _ownsClient = widget.client == null;
    _data = HumanCausalOperationController(
      api: HumanCausalApi(
        client: _client,
        baseUrl: widget.apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl,
        authorizationHeader: () => widget.authorizationHeader(),
      ),
      family: widget.family,
      targetID: widget.targetID,
      ownerID: widget.ownerID == null ? null : () => widget.ownerID!(),
      identityChanges: widget.identityChanges,
      sourceCurrent: () => mounted && (widget.sourceCurrent?.call() ?? true),
      targetKey: () =>
          (widget.family, widget.targetID, widget.client, widget.apiBaseUrl),
      pendingStore: widget.pendingStore,
    );
    final data = _data;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted && identical(_data, data)) unawaited(data.initialize());
    });
  }

  @override
  void initState() {
    super.initState();
    _bind();
  }

  @override
  void didUpdateWidget(PrivacyControlPage old) {
    super.didUpdateWidget(old);
    if (old.family != widget.family ||
        old.targetID != widget.targetID ||
        old.client != widget.client ||
        old.apiBaseUrl != widget.apiBaseUrl ||
        old.identityChanges != widget.identityChanges ||
        old.pendingStore != widget.pendingStore ||
        (old.ownerID == null) != (widget.ownerID == null)) {
      _data.dispose();
      if (_ownsClient) _client.close();
      _bind();
    }
  }

  @override
  void dispose() {
    _data.dispose();
    if (_ownsClient) _client.close();
    super.dispose();
  }

  Future<void> _act(HumanCausalInput input) async {
    await _data.checkAndConfirm(context, input);
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: _data,
    builder: (context, _) {
      final c = _data,
          s = c.snapshot,
          e = c.epoch,
          current = c.current && humanControlID(c.owner),
          target = widget.targetID;
      bool frame() =>
          mounted &&
          identical(c, _data) &&
          c.current &&
          c.epoch == e &&
          widget.targetID == target &&
          identical(c.snapshot, s) &&
          s?.current == true;
      return Scaffold(
        appBar: birdtieAppBar(
          context,
          title: Text(
            widget.family == HumanCausalFamily.block ? '屏蔽设置' : '撤销资料访问授权',
          ),
        ),
        body: !current && !c.busy
            ? Center(
                child: Padding(
                  padding: const EdgeInsets.all(20),
                  child: Text(c.error ?? '当前个人身份不可用。'),
                ),
              )
            : ListView(
                padding: const EdgeInsets.all(20),
                children: [
                  HumanCausalOperationPanel(controller: c),
                  if (current && s is HumanBlockSnapshot) ...[
                    Text(s.blocked ? '此账号当前已被屏蔽。' : '此账号当前未被屏蔽。'),
                    const SizedBox(height: 12),
                    const Text('屏蔽会按原规则停止联系与资料访问。取消屏蔽不会恢复旧好友关系、旧授权或删除历史消息。'),
                    Text(
                      '账号标识末六位：${target.length >= 6 ? target.substring(target.length - 6) : '不可用'}',
                    ),
                    const SizedBox(height: 16),
                    FilledButton(
                      key: const Key('human-privacy-apply'),
                      onPressed:
                          c.canStart && (s.blocked ? s.canUnblock : s.canBlock)
                          ? () {
                              if (frame())
                                unawaited(
                                  _act(
                                    HumanBlockInput(
                                      operationID: const Uuid().v4(),
                                      action: s.blocked ? 'UNBLOCK' : 'BLOCK',
                                      accountID: target,
                                      expectedVersion: s.version,
                                    ),
                                  ),
                                );
                            }
                          : null,
                      child: Text(s.blocked ? '检查取消屏蔽' : '检查屏蔽'),
                    ),
                  ],
                  if (current && s is HumanGrantSnapshot) ...[
                    const Text('这是本人的资料访问授权。撤销会阻止继续按此授权读取，不会抹去已经发生的读取。'),
                    const SizedBox(height: 12),
                    Text(
                      '授权标识末六位：${target.length >= 6 ? target.substring(target.length - 6) : '不可用'}',
                    ),
                    if (s.grant['expiresAt'] != null)
                      Text('原有效期限：${_expiry(s.grant['expiresAt']!)}'),
                    if (s.grant['revokedAt'] != null) const Text('当前授权已撤销。'),
                    const SizedBox(height: 16),
                    FilledButton(
                      key: const Key('human-privacy-apply'),
                      onPressed: c.canStart && s.canRevoke
                          ? () {
                              if (frame())
                                unawaited(
                                  _act(
                                    HumanGrantInput(
                                      operationID: const Uuid().v4(),
                                      grantID: target,
                                      expectedVersion:
                                          s.grant['revision'] as int,
                                    ),
                                  ),
                                );
                            }
                          : null,
                      child: const Text('检查撤销授权'),
                    ),
                  ],
                  if (current && s == null && !c.busy)
                    const Text('当前资料暂不可读，不能开始新的修改。原操作核实不会授予新权限。'),
                  if (c.receipt != null && !c.busy)
                    TextButton(
                      key: const Key('human-privacy-return'),
                      onPressed: () {
                        if (c.current && e == c.epoch)
                          Navigator.of(context).pop();
                      },
                      child: const Text('返回'),
                    ),
                ],
              ),
      );
    },
  );
}
