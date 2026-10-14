import 'dart:async';
import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import 'package:uuid/uuid.dart';
import '../app/birdtie_app_bar.dart';
import '../config/birdtie_environment.dart';
import 'human_causal_control.dart';
import 'human_causal_pending_store.dart';
import 'human_causal_operation_controller.dart';

class OrganizationFAQ {
  const OrganizationFAQ({
    required this.id,
    required this.question,
    required this.answer,
    required this.published,
    this.revision = 0,
  });
  final String id, question, answer;
  final bool published;
  final int revision;
  factory OrganizationFAQ.fromJson(Map<String, dynamic> data) =>
      OrganizationFAQ(
        id: data['id'] as String,
        question: data['question'] as String,
        answer: data['answer'] as String,
        published: data['published'] as bool,
        revision: data['revision'] as int? ?? 0,
      );
}

class OrganizationFAQPage extends StatefulWidget {
  const OrganizationFAQPage({
    super.key,
    required this.organizationID,
    required this.authorizationHeader,
    this.ownerID,
    this.sourceCurrent,
    this.identityChanges,
    this.apiBaseUrl,
    this.client,
    this.pendingStore,
  });
  final String organizationID;
  final String? Function() authorizationHeader;
  final String? Function()? ownerID;
  final bool Function()? sourceCurrent;
  final Listenable? identityChanges;
  final String? apiBaseUrl;
  final http.Client? client;
  final HumanCausalPendingStore? pendingStore;
  @override
  State<OrganizationFAQPage> createState() => _OrganizationFAQPageState();
}

class _OrganizationFAQPageState extends State<OrganizationFAQPage> {
  late http.Client _client;
  late bool _ownsClient;
  late HumanCausalOperationController _data;
  final _question = TextEditingController(), _answer = TextEditingController();
  bool _editing = false, _published = false;
  String _faqID = '';
  int _revision = 0;
  String? _formError;
  void _bind() {
    _client = widget.client ?? http.Client();
    _ownsClient = widget.client == null;
    _data = HumanCausalOperationController(
      api: HumanCausalApi(
        client: _client,
        baseUrl: widget.apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl,
        authorizationHeader: () => widget.authorizationHeader(),
      ),
      family: HumanCausalFamily.faq,
      organizationID: widget.organizationID,
      ownerID: widget.ownerID == null ? null : () => widget.ownerID!(),
      identityChanges: widget.identityChanges,
      sourceCurrent: () => mounted && (widget.sourceCurrent?.call() ?? true),
      targetKey: () =>
          (widget.organizationID, widget.client, widget.apiBaseUrl),
      pendingStore: widget.pendingStore,
    );
    _data.addListener(_changed);
    final c = _data;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted && identical(_data, c)) unawaited(c.initialize());
    });
  }

  void _clear() {
    _question.clear();
    _answer.clear();
    _editing = false;
    _faqID = '';
    _revision = 0;
    _published = false;
    _formError = null;
  }

  void _changed() {
    if (!mounted) return;
    if (!_data.current) _clear();
    setState(() {});
  }

  @override
  void initState() {
    super.initState();
    _bind();
  }

  @override
  void didUpdateWidget(OrganizationFAQPage old) {
    super.didUpdateWidget(old);
    if (old.organizationID != widget.organizationID ||
        old.client != widget.client ||
        old.apiBaseUrl != widget.apiBaseUrl ||
        old.identityChanges != widget.identityChanges ||
        old.pendingStore != widget.pendingStore ||
        (old.ownerID == null) != (widget.ownerID == null)) {
      _data.removeListener(_changed);
      _data.dispose();
      if (_ownsClient) _client.close();
      _clear();
      _bind();
    }
  }

  @override
  void dispose() {
    _data.removeListener(_changed);
    _data.dispose();
    _question.dispose();
    _answer.dispose();
    if (_ownsClient) _client.close();
    super.dispose();
  }

  void _edit([Map<String, dynamic>? row]) {
    if (!_data.canStart) return;
    setState(() {
      _editing = true;
      _faqID = row?['id'] as String? ?? '';
      _revision = row?['revision'] as int? ?? 0;
      _question.text = row?['question'] as String? ?? '';
      _answer.text = row?['answer'] as String? ?? '';
      _published = row?['published'] as bool? ?? false;
      _formError = null;
    });
  }

  Future<void> _save() async {
    final c = _data;
    final input = HumanFAQInput(
      organizationID: widget.organizationID,
      operationID: const Uuid().v4(),
      action: _faqID.isEmpty ? 'CREATE' : 'UPDATE',
      expectedVersion: _revision,
      faqID: _faqID,
      question: _question.text.trim(),
      answer: _answer.text.trim(),
      published: _published,
    );
    if (!input.valid) {
      setState(() => _formError = '问题需2–300字，回答需1–3000字。');
      return;
    }
    final r = await c.checkAndConfirm(context, input);
    if (mounted && identical(c, _data) && c.current && r?.status == 'SUCCEEDED')
      setState(_clear);
  }

  @override
  Widget build(BuildContext context) {
    final c = _data, s = c.snapshot, e = c.epoch;
    final current = c.current && humanControlID(c.owner);
    bool frame() =>
        mounted &&
        identical(c, _data) &&
        c.current &&
        e == c.epoch &&
        identical(s, c.snapshot) &&
        s?.current == true;
    final rows = current && s is HumanFAQSnapshot
        ? s.items
        : const <Map<String, dynamic>>[];
    final editCurrent =
        _faqID.isEmpty ||
        rows.any((r) => r['id'] == _faqID && r['revision'] == _revision);
    return Scaffold(
      appBar: birdtieAppBar(context, title: const Text('组织常见问答')),
      body: ListView(
        padding: const EdgeInsets.fromLTRB(20, 16, 20, 40),
        children: [
          if (!c.current)
            Text(c.error ?? '当前身份或组织来源已变化。')
          else ...[
            HumanCausalOperationPanel(controller: c),
            if (current && s is HumanFAQSnapshot) ...[
              const Text('管理员维护的已发布问答，仅在组织身份通过核验后用于组织 Agent 回答。'),
              const SizedBox(height: 16),
              if (!_editing)
                FilledButton.icon(
                  onPressed: c.canStart
                      ? () {
                          if (frame()) _edit();
                        }
                      : null,
                  icon: const Icon(Icons.add),
                  label: const Text('新增问答'),
                ),
              if (_editing && editCurrent) ...[
                Text(
                  _faqID.isEmpty ? '新增常见问答' : '编辑常见问答',
                  style: Theme.of(context).textTheme.titleMedium,
                ),
                TextField(
                  key: const Key('faq-question'),
                  controller: _question,
                  enabled: !c.busy && c.pending == null,
                  maxLength: 300,
                  decoration: const InputDecoration(labelText: '问题'),
                  onChanged: (_) => c.invalidatePreview(),
                ),
                TextField(
                  key: const Key('faq-answer'),
                  controller: _answer,
                  enabled: !c.busy && c.pending == null,
                  maxLength: 3000,
                  maxLines: 5,
                  decoration: const InputDecoration(labelText: '回答'),
                  onChanged: (_) => c.invalidatePreview(),
                ),
                SwitchListTile(
                  title: const Text('发布给组织 Agent'),
                  subtitle: const Text('组织核验通过后，公开提问可使用此回答。'),
                  value: _published,
                  onChanged: !c.busy && c.pending == null
                      ? (v) {
                          if (frame()) {
                            c.invalidatePreview();
                            setState(() => _published = v);
                          }
                        }
                      : null,
                ),
                if (_formError != null) Text(_formError!),
                Wrap(
                  children: [
                    TextButton(
                      onPressed: !c.busy && c.pending == null
                          ? () {
                              if (frame()) setState(_clear);
                            }
                          : null,
                      child: const Text('取消'),
                    ),
                    FilledButton(
                      key: const Key('faq-check'),
                      onPressed: c.canStart
                          ? () {
                              if (frame()) unawaited(_save());
                            }
                          : null,
                      child: const Text('检查保存问答'),
                    ),
                  ],
                ),
              ] else if (_editing)
                const Text('原问答版本已变化，旧编辑不再提交。请取消并重新选择当前问答。'),
              if (_editing && !editCurrent)
                TextButton(
                  onPressed: () => setState(_clear),
                  child: const Text('取消旧编辑'),
                ),
              const SizedBox(height: 20),
              if (rows.isEmpty) const Text('还没有常见问答。可以先保存草稿。'),
              for (final row in rows)
                Padding(
                  padding: const EdgeInsets.only(bottom: 16),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        row['question'] as String,
                        style: Theme.of(context).textTheme.titleMedium,
                      ),
                      const SizedBox(height: 6),
                      Text(row['answer'] as String),
                      Text(row['published'] == true ? '已发布' : '草稿 · 仅管理员可见'),
                      Wrap(
                        children: [
                          TextButton(
                            onPressed: c.canStart
                                ? () {
                                    if (frame()) _edit(row);
                                  }
                                : null,
                            child: const Text('编辑'),
                          ),
                          TextButton(
                            onPressed: c.canStart
                                ? () {
                                    if (frame())
                                      unawaited(
                                        c.checkAndConfirm(
                                          context,
                                          HumanFAQInput(
                                            organizationID:
                                                widget.organizationID,
                                            operationID: const Uuid().v4(),
                                            action: 'DELETE',
                                            faqID: row['id'] as String,
                                            expectedVersion:
                                                row['revision'] as int,
                                          ),
                                        ),
                                      );
                                  }
                                : null,
                            child: const Text('删除'),
                          ),
                        ],
                      ),
                    ],
                  ),
                ),
            ] else if (!c.busy)
              const Text('当前组织问答不可读，不能开始新的修改；本人原操作仍可核实。'),
          ],
        ],
      ),
    );
  }
}
