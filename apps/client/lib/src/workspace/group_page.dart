import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;

import '../auth/birdtie_auth_controller.dart';
import '../city/public_city_controller.dart';

class GroupPage extends StatefulWidget {
  const GroupPage({super.key, required this.auth, required this.city});
  final BirdtieAuthController auth;
  final PublicCityController city;

  @override
  State<GroupPage> createState() => _GroupPageState();
}

class _GroupPageState extends State<GroupPage> {
  static const _apiBase = String.fromEnvironment('BIRDTIE_API_BASE_URL');
  final _client = http.Client();
  final _name = TextEditingController();
  final _summary = TextEditingController();
  final _sourceLabel = TextEditingController();
  final _sourceURL = TextEditingController();
  final _rightsNote = TextEditingController();
  List<Map<String, dynamic>> _groups = const [];
  bool _busy = false;
  bool _failed = false;
  bool _signedIn = false;
  int _serial = 0;
  String? _message;

  Uri _endpoint(String path) =>
      Uri.parse('${_apiBase.replaceFirst(RegExp(r'/$'), '')}$path');
  Map<String, String> _headers({bool json = false}) => {
    'Authorization': widget.auth.authorizationHeader!,
    if (json) 'Content-Type': 'application/json',
  };

  @override
  void initState() {
    super.initState();
    _signedIn = widget.auth.signedIn;
    widget.auth.addListener(_onAuthChanged);
    if (_signedIn && _apiBase.isNotEmpty) _load();
  }

  void _onAuthChanged() {
    if (_signedIn == widget.auth.signedIn) return;
    _signedIn = widget.auth.signedIn;
    if (!_signedIn) {
      ++_serial;
      _name.clear();
      _summary.clear();
      _sourceLabel.clear();
      _sourceURL.clear();
      _rightsNote.clear();
      setState(() {
        _groups = const [];
        _busy = false;
        _failed = false;
        _message = null;
      });
    } else if (_apiBase.isNotEmpty) {
      _load();
    }
  }

  Future<void> _load() async {
    final serial = ++_serial;
    setState(() {
      _busy = true;
      _failed = false;
    });
    try {
      final response = await _client
          .get(_endpoint('/v1/me/communities'), headers: _headers())
          .timeout(const Duration(seconds: 10));
      if (response.statusCode != 200) throw StateError('Groups unavailable');
      final records =
          (jsonDecode(response.body) as Map<String, dynamic>)['data']
              as List<dynamic>;
      if (!mounted || serial != _serial) return;
      setState(
        () =>
            _groups = [for (final raw in records) raw as Map<String, dynamic>],
      );
    } catch (_) {
      if (mounted && serial == _serial) setState(() => _failed = true);
    } finally {
      if (mounted && serial == _serial) setState(() => _busy = false);
    }
  }

  Future<void> _submit() async {
    final serial = _serial;
    final cityID = widget.city.selectedCity?.id;
    if (cityID == null) {
      setState(() => _message = 'Choose a city first.');
      return;
    }
    setState(() {
      _busy = true;
      _message = null;
    });
    try {
      final response = await _client
          .post(
            _endpoint('/v1/cities/${Uri.encodeComponent(cityID)}/communities'),
            headers: _headers(json: true),
            body: jsonEncode({
              'name': _name.text.trim(),
              'summary': _summary.text.trim(),
              'sourceLabel': _sourceLabel.text.trim(),
              'sourceUrl': _sourceURL.text.trim(),
              'rightsNote': _rightsNote.text.trim(),
              'expiresAt': DateTime.now()
                  .toUtc()
                  .add(const Duration(days: 90))
                  .toIso8601String(),
            }),
          )
          .timeout(const Duration(seconds: 12));
      if (response.statusCode != 201) {
        throw StateError('Check the source, rights note and required fields.');
      }
      if (!mounted || serial != _serial || !widget.auth.signedIn) return;
      _name.clear();
      _summary.clear();
      _sourceLabel.clear();
      _sourceURL.clear();
      _rightsNote.clear();
      if (!mounted) return;
      setState(() => _message = 'Submitted for independent city review.');
      await _load();
    } catch (_) {
      if (mounted && serial == _serial && widget.auth.signedIn) {
        setState(
          () => _message = 'Could not submit. Check all fields and try again.',
        );
      }
    } finally {
      if (mounted && widget.auth.signedIn) setState(() => _busy = false);
    }
  }

  Future<void> _withdraw(String id) async {
    final serial = _serial;
    setState(() {
      _busy = true;
      _message = null;
    });
    try {
      final response = await _client
          .post(
            _endpoint('/v1/me/communities/${Uri.encodeComponent(id)}/withdraw'),
            headers: _headers(),
          )
          .timeout(const Duration(seconds: 10));
      if (response.statusCode != 204) throw StateError('Withdraw failed');
      if (!mounted || serial != _serial || !widget.auth.signedIn) return;
      await _load();
    } catch (_) {
      if (mounted && serial == _serial && widget.auth.signedIn) {
        setState(() => _message = 'Could not withdraw this group.');
      }
    } finally {
      if (mounted && widget.auth.signedIn) setState(() => _busy = false);
    }
  }

  @override
  void dispose() {
    ++_serial;
    widget.auth.removeListener(_onAuthChanged);
    _client.close();
    _name.dispose();
    _summary.dispose();
    _sourceLabel.dispose();
    _sourceURL.dispose();
    _rightsNote.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    if (_apiBase.isEmpty) {
      return const Center(
        child: Text('Connect the Birdtie API to manage groups.'),
      );
    }
    if (!widget.auth.signedIn) {
      return const Center(
        child: Text('Sign in from Profile to manage your groups.'),
      );
    }
    return ListView(
      padding: const EdgeInsets.all(20),
      children: [
        const Text(
          'My groups',
          style: TextStyle(fontSize: 21, fontWeight: FontWeight.w700),
        ),
        const SizedBox(height: 8),
        const Text(
          'Public groups appear in Birdtie only after a different city reviewer checks the source and rights.',
          style: TextStyle(color: Color(0xFF747B73)),
        ),
        if (_busy) const LinearProgressIndicator(),
        if (_failed)
          TextButton(
            onPressed: _load,
            child: const Text('Could not load groups. Retry'),
          ),
        if (!_failed && !_busy && _groups.isEmpty)
          const Padding(
            padding: EdgeInsets.symmetric(vertical: 20),
            child: Text('You have not submitted a group yet.'),
          ),
        for (final group in _groups)
          ListTile(
            contentPadding: EdgeInsets.zero,
            title: Text(group['name'] as String),
            subtitle: Text(switch (group['status'] as String) {
              'draft' => 'Awaiting city review',
              'published' => 'Published',
              _ => 'Hidden',
            }),
            trailing: group['status'] == 'hidden'
                ? null
                : TextButton(
                    onPressed: _busy
                        ? null
                        : () => _withdraw(group['id'] as String),
                    child: const Text('Withdraw'),
                  ),
          ),
        const Divider(height: 36),
        const Text(
          'Submit a group',
          style: TextStyle(fontSize: 18, fontWeight: FontWeight.w700),
        ),
        const SizedBox(height: 8),
        const Text(
          'Only submit a group you own or are authorized to represent. Provide a public HTTPS source and explain your right to publish it.',
          style: TextStyle(color: Color(0xFF747B73)),
        ),
        _field(_name, 'Group name', 160),
        _field(_summary, 'What the group does', 3000, lines: 3),
        _field(_sourceLabel, 'Source name', 120),
        _field(_sourceURL, 'Public HTTPS source URL', 1000),
        _field(
          _rightsNote,
          'Your authority or rights to publish',
          1000,
          lines: 3,
        ),
        if (_message != null)
          Padding(
            padding: const EdgeInsets.symmetric(vertical: 8),
            child: Text(
              _message!,
              style: const TextStyle(color: Color(0xFF193B32)),
            ),
          ),
        const SizedBox(height: 12),
        FilledButton(
          onPressed: _busy ? null : _submit,
          child: const Text('Submit for review'),
        ),
      ],
    );
  }

  Widget _field(
    TextEditingController controller,
    String label,
    int limit, {
    int lines = 1,
  }) => Padding(
    padding: const EdgeInsets.only(top: 14),
    child: TextField(
      controller: controller,
      maxLength: limit,
      maxLines: lines,
      decoration: InputDecoration(
        labelText: label,
        border: const OutlineInputBorder(),
      ),
    ),
  );
}
