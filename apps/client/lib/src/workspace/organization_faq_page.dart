import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';

class OrganizationFAQ {
  const OrganizationFAQ({
    required this.id,
    required this.question,
    required this.answer,
    required this.published,
  });

  final String id;
  final String question;
  final String answer;
  final bool published;

  factory OrganizationFAQ.fromJson(Map<String, dynamic> data) =>
      OrganizationFAQ(
        id: data['id'] as String,
        question: data['question'] as String,
        answer: data['answer'] as String,
        published: data['published'] as bool? ?? false,
      );
}

class OrganizationFAQPage extends StatefulWidget {
  const OrganizationFAQPage({
    super.key,
    required this.organizationID,
    required this.authorizationHeader,
    this.apiBaseUrl,
    this.client,
  });

  final String organizationID;
  final String? Function() authorizationHeader;
  final String? apiBaseUrl;
  final http.Client? client;

  @override
  State<OrganizationFAQPage> createState() => _OrganizationFAQPageState();
}

class _OrganizationFAQPageState extends State<OrganizationFAQPage> {
  static const _configuredBase = BirdtieEnvironment.apiBaseUrl;
  late final http.Client _client = widget.client ?? http.Client();
  List<OrganizationFAQ> _items = const [];
  bool _loading = true;
  String? _error;

  Uri _url([String suffix = '']) {
    final base = widget.apiBaseUrl ?? _configuredBase;
    return Uri.parse(
      '${base.replaceFirst(RegExp(r'/$'), '')}/v1/me/organizations/${Uri.encodeComponent(widget.organizationID)}/faqs$suffix',
    );
  }

  Map<String, String> _headers({bool json = false}) => {
    'Authorization': ?widget.authorizationHeader(),
    if (json) 'Content-Type': 'application/json',
  };

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    if (widget.client == null) _client.close();
    super.dispose();
  }

  Future<void> _load() async {
    if ((widget.apiBaseUrl ?? _configuredBase).isEmpty) {
      setState(() {
        _loading = false;
        _error = '问答服务暂不可用。';
      });
      return;
    }
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final response = await _client
          .get(_url(), headers: _headers())
          .timeout(const Duration(seconds: 12));
      if (response.statusCode != 200) throw StateError('FAQ load failed');
      final rows =
          (jsonDecode(response.body) as Map<String, dynamic>)['data']
              as List<dynamic>;
      if (mounted) {
        setState(() {
          _items = [
            for (final row in rows)
              OrganizationFAQ.fromJson(row as Map<String, dynamic>),
          ];
          _loading = false;
        });
      }
    } catch (_) {
      if (mounted) {
        setState(() {
          _loading = false;
          _error = '问答加载失败，请重试。';
        });
      }
    }
  }

  Future<void> _edit([OrganizationFAQ? existing]) async {
    final input =
        await showDialog<({String question, String answer, bool published})>(
          context: context,
          builder: (context) => _FAQEditorDialog(existing: existing),
        );
    if (input == null || !mounted) return;
    try {
      final response =
          await (existing == null
                  ? _client.post(
                      _url(),
                      headers: _headers(json: true),
                      body: jsonEncode({
                        'question': input.question,
                        'answer': input.answer,
                        'published': input.published,
                      }),
                    )
                  : _client.put(
                      _url('/${Uri.encodeComponent(existing.id)}'),
                      headers: _headers(json: true),
                      body: jsonEncode({
                        'question': input.question,
                        'answer': input.answer,
                        'published': input.published,
                      }),
                    ))
              .timeout(const Duration(seconds: 12));
      if (response.statusCode != (existing == null ? 201 : 200)) {
        throw StateError('FAQ save failed');
      }
      await _load();
    } catch (_) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(const SnackBar(content: Text('问答保存失败，请重试。')));
      }
    }
  }

  Future<void> _delete(OrganizationFAQ item) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('删除这条问答？'),
        content: Text(item.question),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('删除'),
          ),
        ],
      ),
    );
    if (confirmed != true || !mounted) return;
    try {
      final response = await _client
          .delete(_url('/${Uri.encodeComponent(item.id)}'), headers: _headers())
          .timeout(const Duration(seconds: 12));
      if (response.statusCode != 204) throw StateError('FAQ delete failed');
      await _load();
    } catch (_) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(const SnackBar(content: Text('问答删除失败，请重试。')));
      }
    }
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: const Text('组织常见问答')),
    body: RefreshIndicator(
      onRefresh: _load,
      child: ListView(
        physics: const AlwaysScrollableScrollPhysics(),
        padding: const EdgeInsets.fromLTRB(20, 16, 20, 40),
        children: [
          const Text('管理员维护的已发布问答，仅在组织身份通过核验后用于组织 Agent 回答。'),
          const SizedBox(height: 16),
          FilledButton.icon(
            onPressed: () => _edit(),
            icon: const Icon(Icons.add),
            label: const Text('新增问答'),
          ),
          const SizedBox(height: 20),
          if (_loading) const Center(child: CircularProgressIndicator()),
          if (_error != null)
            TextButton(onPressed: _load, child: Text(_error!)),
          if (!_loading && _error == null && _items.isEmpty)
            const Text('还没有常见问答。可以先保存草稿。'),
          for (final item in _items)
            Card(
              child: Padding(
                padding: const EdgeInsets.all(14),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      item.question,
                      style: Theme.of(context).textTheme.titleMedium,
                    ),
                    const SizedBox(height: 6),
                    Text(item.answer),
                    const SizedBox(height: 8),
                    Text(item.published ? '已发布' : '草稿 · 仅管理员可见'),
                    Wrap(
                      children: [
                        TextButton(
                          onPressed: () => _edit(item),
                          child: const Text('编辑'),
                        ),
                        TextButton(
                          onPressed: () => _delete(item),
                          child: const Text('删除'),
                        ),
                      ],
                    ),
                  ],
                ),
              ),
            ),
        ],
      ),
    ),
  );
}

class _FAQEditorDialog extends StatefulWidget {
  const _FAQEditorDialog({this.existing});
  final OrganizationFAQ? existing;

  @override
  State<_FAQEditorDialog> createState() => _FAQEditorDialogState();
}

class _FAQEditorDialogState extends State<_FAQEditorDialog> {
  late final TextEditingController _question = TextEditingController(
    text: widget.existing?.question,
  );
  late final TextEditingController _answer = TextEditingController(
    text: widget.existing?.answer,
  );
  late bool _published = widget.existing?.published ?? false;

  @override
  void dispose() {
    _question.dispose();
    _answer.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: Text(widget.existing == null ? '新增常见问答' : '编辑常见问答'),
    content: SingleChildScrollView(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          TextField(
            controller: _question,
            maxLength: 300,
            decoration: const InputDecoration(labelText: '问题'),
          ),
          TextField(
            controller: _answer,
            maxLength: 3000,
            maxLines: 5,
            decoration: const InputDecoration(labelText: '回答'),
          ),
          SwitchListTile(
            title: const Text('发布给组织 Agent'),
            subtitle: const Text('仅当组织身份已认证，公开提问才能使用此回答。'),
            value: _published,
            onChanged: (value) => setState(() => _published = value),
          ),
        ],
      ),
    ),
    actions: [
      TextButton(
        onPressed: () => Navigator.pop(context),
        child: const Text('取消'),
      ),
      FilledButton(
        onPressed: () {
          if (_question.text.trim().runes.length < 2 ||
              _answer.text.trim().isEmpty) {
            return;
          }
          Navigator.pop(context, (
            question: _question.text.trim(),
            answer: _answer.text.trim(),
            published: _published,
          ));
        },
        child: const Text('保存'),
      ),
    ],
  );
}
