import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;

import '../auth/birdtie_auth_controller.dart';

class ContactRequest {
  const ContactRequest({
    required this.id,
    required this.direction,
    required this.otherAccountId,
    required this.otherName,
    required this.note,
    required this.state,
    required this.conversationId,
  });
  final String id;
  final String direction;
  final String otherAccountId;
  final String otherName;
  final String note;
  final String state;
  final String conversationId;

  factory ContactRequest.fromJson(Map<String, dynamic> json) => ContactRequest(
    id: json['id'] as String,
    direction: json['direction'] as String? ?? '',
    otherAccountId: json['otherAccountId'] as String? ?? '',
    otherName: json['otherName'] as String? ?? '',
    note: json['note'] as String? ?? '',
    state: json['state'] as String? ?? '',
    conversationId: json['conversationId'] as String? ?? '',
  );
}

class HumanConversation {
  const HumanConversation({
    required this.id,
    required this.otherAccountId,
    required this.otherName,
  });
  final String id;
  final String otherAccountId;
  final String otherName;

  factory HumanConversation.fromJson(Map<String, dynamic> json) =>
      HumanConversation(
        id: json['id'] as String,
        otherAccountId: json['otherAccountId'] as String,
        otherName: json['otherName'] as String,
      );
}

class HumanMessage {
  const HumanMessage({
    required this.id,
    required this.senderAccountId,
    required this.body,
    required this.createdAt,
  });
  final String id;
  final String senderAccountId;
  final String body;
  final DateTime createdAt;

  factory HumanMessage.fromJson(Map<String, dynamic> json) => HumanMessage(
    id: json['id'] as String,
    senderAccountId: json['senderAccountId'] as String,
    body: json['body'] as String,
    createdAt: DateTime.parse(json['createdAt'] as String),
  );
}

class ConnectionSource {
  ConnectionSource({
    required this.authorizationHeader,
    http.Client? client,
    String? apiBaseUrl,
  }) : _client = client ?? http.Client(),
       _ownsClient = client == null,
       _apiBaseUrl = apiBaseUrl ?? apiBase;

  static const apiBase = String.fromEnvironment('BIRDTIE_API_BASE_URL');
  final String? Function() authorizationHeader;
  final http.Client _client;
  final bool _ownsClient;
  final String _apiBaseUrl;
  bool get configured => _apiBaseUrl.isNotEmpty;

  Uri _endpoint(String path) =>
      Uri.parse('${_apiBaseUrl.replaceFirst(RegExp(r'/$'), '')}$path');
  Map<String, String> _headers({bool json = false}) {
    final token = authorizationHeader();
    if (token == null) throw StateError('Sign in to contact people');
    return {
      'Authorization': token,
      if (json) 'Content-Type': 'application/json',
    };
  }

  Future<void> request(
    String recipientAccountId,
    String cityId,
    String note,
  ) async {
    final response = await _client
        .post(
          _endpoint('/v1/me/connection-requests'),
          headers: _headers(json: true),
          body: jsonEncode({
            'recipientAccountId': recipientAccountId,
            'cityId': cityId,
            'note': note,
          }),
        )
        .timeout(const Duration(seconds: 12));
    if (response.statusCode != 201) {
      throw StateError('Contact request unavailable: ${response.statusCode}');
    }
  }

  Future<List<ContactRequest>> requests() async {
    final response = await _client
        .get(_endpoint('/v1/me/connection-requests'), headers: _headers())
        .timeout(const Duration(seconds: 12));
    if (response.statusCode != 200) throw StateError('Requests unavailable');
    final rows =
        (jsonDecode(response.body) as Map<String, dynamic>)['data']
            as List<dynamic>;
    return [
      for (final row in rows)
        ContactRequest.fromJson(row as Map<String, dynamic>),
    ];
  }

  Future<void> decide(String id, String action) async {
    final response = await _client
        .post(
          _endpoint(
            '/v1/me/connection-requests/${Uri.encodeComponent(id)}/decision',
          ),
          headers: _headers(json: true),
          body: jsonEncode({'action': action}),
        )
        .timeout(const Duration(seconds: 12));
    if (response.statusCode != 200) throw StateError('Decision unavailable');
  }

  Future<List<HumanConversation>> conversations() async {
    final response = await _client
        .get(_endpoint('/v1/me/conversations'), headers: _headers())
        .timeout(const Duration(seconds: 12));
    if (response.statusCode != 200) {
      throw StateError('Conversations unavailable');
    }
    final rows =
        (jsonDecode(response.body) as Map<String, dynamic>)['data']
            as List<dynamic>;
    return [
      for (final row in rows)
        HumanConversation.fromJson(row as Map<String, dynamic>),
    ];
  }

  Future<List<HumanMessage>> messages(String conversationId) async {
    final response = await _client
        .get(
          _endpoint(
            '/v1/me/conversations/${Uri.encodeComponent(conversationId)}/messages',
          ),
          headers: _headers(),
        )
        .timeout(const Duration(seconds: 12));
    if (response.statusCode != 200) throw StateError('Messages unavailable');
    final rows =
        (jsonDecode(response.body) as Map<String, dynamic>)['data']
            as List<dynamic>;
    return [
      for (final row in rows)
        HumanMessage.fromJson(row as Map<String, dynamic>),
    ];
  }

  Future<void> send(String conversationId, String body) async {
    final response = await _client
        .post(
          _endpoint(
            '/v1/me/conversations/${Uri.encodeComponent(conversationId)}/messages',
          ),
          headers: _headers(json: true),
          body: jsonEncode({'body': body}),
        )
        .timeout(const Duration(seconds: 12));
    if (response.statusCode != 201) throw StateError('Message unavailable');
  }

  void dispose() {
    if (_ownsClient) _client.close();
  }
}

class SocialInboxSection extends StatefulWidget {
  const SocialInboxSection({super.key, required this.auth});
  final BirdtieAuthController auth;

  @override
  State<SocialInboxSection> createState() => _SocialInboxSectionState();
}

class _SocialInboxSectionState extends State<SocialInboxSection> {
  late final ConnectionSource _source;
  List<ContactRequest> _requests = const [];
  List<HumanConversation> _conversations = const [];
  bool _loading = false;
  bool _failed = false;
  String? _busyID;
  int _serial = 0;

  @override
  void initState() {
    super.initState();
    _source = ConnectionSource(
      authorizationHeader: () => widget.auth.authorizationHeader,
    );
    if (widget.auth.signedIn && _source.configured) unawaited(_load());
  }

  Future<void> _load() async {
    if (!widget.auth.signedIn || _loading) return;
    final serial = ++_serial;
    setState(() {
      _loading = true;
      _failed = false;
    });
    try {
      final requests = await _source.requests();
      final conversations = await _source.conversations();
      if (!mounted || serial != _serial) return;
      setState(() {
        _requests = requests;
        _conversations = conversations;
      });
    } catch (_) {
      if (mounted && serial == _serial) setState(() => _failed = true);
    } finally {
      if (mounted && serial == _serial) setState(() => _loading = false);
    }
  }

  Future<void> _decide(ContactRequest request, String action) async {
    if (_busyID != null) return;
    setState(() => _busyID = request.id);
    try {
      await _source.decide(request.id, action);
      await _load();
    } catch (_) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(content: Text('Could not update this request.')),
        );
      }
    } finally {
      if (mounted) setState(() => _busyID = null);
    }
  }

  void _openConversation(HumanConversation conversation) {
    Navigator.push(
      context,
      MaterialPageRoute<void>(
        builder: (context) => Scaffold(
          appBar: AppBar(title: Text(conversation.otherName)),
          body: HumanConversationPage(
            auth: widget.auth,
            conversation: conversation,
          ),
        ),
      ),
    );
  }

  @override
  void dispose() {
    ++_serial;
    _source.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    if (!widget.auth.signedIn || !_source.configured) {
      return const SizedBox.shrink();
    }
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Row(
          children: [
            const Expanded(child: Text('Requests and conversations')),
            IconButton(
              tooltip: 'Refresh requests and conversations',
              onPressed: _loading ? null : _load,
              icon: const Icon(Icons.refresh),
            ),
          ],
        ),
        if (_loading) const LinearProgressIndicator(),
        if (_failed)
          TextButton(
            onPressed: _load,
            child: const Text('Could not load contacts. Retry'),
          ),
        for (final request in _requests)
          ListTile(
            contentPadding: EdgeInsets.zero,
            leading: const Icon(Icons.person_add_alt_outlined),
            title: Text(request.otherName),
            subtitle: Text(
              '${request.direction == 'incoming' ? 'Received' : 'Sent'} · ${request.state}${request.note.isEmpty ? '' : '\n${request.note}'}',
              maxLines: 3,
              overflow: TextOverflow.ellipsis,
            ),
            trailing: request.state != 'pending'
                ? null
                : PopupMenuButton<String>(
                    tooltip: 'Request actions',
                    enabled: _busyID == null,
                    onSelected: (action) => _decide(request, action),
                    itemBuilder: (context) => request.direction == 'incoming'
                        ? const [
                            PopupMenuItem(
                              value: 'accept',
                              child: Text('Accept'),
                            ),
                            PopupMenuItem(
                              value: 'decline',
                              child: Text('Decline'),
                            ),
                          ]
                        : const [
                            PopupMenuItem(
                              value: 'withdraw',
                              child: Text('Withdraw'),
                            ),
                          ],
                  ),
          ),
        for (final conversation in _conversations)
          ListTile(
            contentPadding: EdgeInsets.zero,
            leading: const Icon(Icons.chat_bubble_outline),
            title: Text(conversation.otherName),
            subtitle: const Text('Human conversation'),
            onTap: () => _openConversation(conversation),
            trailing: const Icon(Icons.chevron_right),
          ),
        if (!_loading &&
            !_failed &&
            _requests.isEmpty &&
            _conversations.isEmpty)
          const Padding(
            padding: EdgeInsets.symmetric(vertical: 12),
            child: Text('No contact requests or conversations yet.'),
          ),
        const Divider(height: 28),
      ],
    );
  }
}

class HumanConversationPage extends StatefulWidget {
  const HumanConversationPage({
    super.key,
    required this.auth,
    required this.conversation,
  });
  final BirdtieAuthController auth;
  final HumanConversation conversation;

  @override
  State<HumanConversationPage> createState() => _HumanConversationPageState();
}

class _HumanConversationPageState extends State<HumanConversationPage> {
  final TextEditingController _text = TextEditingController();
  late final ConnectionSource _source;
  List<HumanMessage> _messages = const [];
  bool _loading = false;
  bool _sending = false;
  bool _failed = false;
  int _serial = 0;

  @override
  void initState() {
    super.initState();
    _source = ConnectionSource(
      authorizationHeader: () => widget.auth.authorizationHeader,
    );
    unawaited(_load());
  }

  Future<void> _load() async {
    final serial = ++_serial;
    setState(() {
      _loading = true;
      _failed = false;
    });
    try {
      final messages = await _source.messages(widget.conversation.id);
      if (mounted && serial == _serial) setState(() => _messages = messages);
    } catch (_) {
      if (mounted && serial == _serial) setState(() => _failed = true);
    } finally {
      if (mounted && serial == _serial) setState(() => _loading = false);
    }
  }

  Future<void> _send() async {
    final body = _text.text.trim();
    if (body.isEmpty || _sending) return;
    setState(() => _sending = true);
    try {
      await _source.send(widget.conversation.id, body);
      _text.clear();
      await _load();
    } catch (_) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(
            content: Text(
              'Message not sent. Check the conversation and retry.',
            ),
          ),
        );
      }
    } finally {
      if (mounted) setState(() => _sending = false);
    }
  }

  @override
  void dispose() {
    ++_serial;
    _text.dispose();
    _source.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => Column(
    children: [
      const Padding(
        padding: EdgeInsets.all(12),
        child: Text(
          'Human conversation · messages are sent only when you tap Send.',
        ),
      ),
      if (_loading) const LinearProgressIndicator(),
      if (_failed)
        TextButton(
          onPressed: _load,
          child: const Text('Messages unavailable. Retry'),
        ),
      Expanded(
        child: RefreshIndicator(
          onRefresh: _load,
          child: ListView(
            padding: const EdgeInsets.all(16),
            children: [
              if (_messages.isEmpty && !_loading)
                const Text('No messages yet.'),
              for (final message in _messages)
                Align(
                  alignment:
                      message.senderAccountId ==
                          widget.conversation.otherAccountId
                      ? Alignment.centerLeft
                      : Alignment.centerRight,
                  child: Card(
                    child: Padding(
                      padding: const EdgeInsets.all(12),
                      child: Text(message.body),
                    ),
                  ),
                ),
            ],
          ),
        ),
      ),
      SafeArea(
        top: false,
        child: Padding(
          padding: const EdgeInsets.all(12),
          child: Row(
            children: [
              Expanded(
                child: TextField(
                  controller: _text,
                  maxLength: 2000,
                  maxLines: 3,
                  minLines: 1,
                  decoration: const InputDecoration(hintText: 'Message'),
                  onSubmitted: (_) => _send(),
                ),
              ),
              IconButton(
                tooltip: 'Send human message',
                onPressed: _sending ? null : _send,
                icon: const Icon(Icons.send),
              ),
            ],
          ),
        ),
      ),
    ],
  );
}
