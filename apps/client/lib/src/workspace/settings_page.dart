import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;

import '../auth/birdtie_auth_controller.dart';
import '../city/public_city_controller.dart';
import '../content/private_moment_controller.dart';
import '../legacy/legacy_shell.dart';

class SettingsPage extends StatefulWidget {
  const SettingsPage({
    super.key,
    required this.auth,
    required this.city,
    required this.moments,
    this.client,
    this.apiBaseUrl,
  });

  final BirdtieAuthController auth;
  final PublicCityController city;
  final PrivateMomentController moments;
  final http.Client? client;
  final String? apiBaseUrl;

  @override
  State<SettingsPage> createState() => _SettingsPageState();
}

class _SettingsPageState extends State<SettingsPage> {
  static const _defaultApiBase = String.fromEnvironment('BIRDTIE_API_BASE_URL');
  late final http.Client _client;
  String get _apiBase => widget.apiBaseUrl ?? _defaultApiBase;
  List<Map<String, dynamic>> _blocks = const [];
  List<Map<String, dynamic>> _grants = const [];
  bool _loading = false;
  bool _failed = false;
  String? _workingID;
  int _serial = 0;

  Uri _endpoint(String path) =>
      Uri.parse('${_apiBase.replaceFirst(RegExp(r'/$'), '')}$path');

  @override
  void initState() {
    super.initState();
    _client = widget.client ?? http.Client();
    widget.auth.addListener(_onAuthChanged);
    if (widget.auth.signedIn) unawaited(_load());
  }

  void _onAuthChanged() {
    if (!widget.auth.signedIn) {
      ++_serial;
      setState(() {
        _blocks = const [];
        _grants = const [];
        _loading = false;
        _workingID = null;
      });
    } else if (_blocks.isEmpty && _grants.isEmpty && !_loading) {
      unawaited(_load());
    } else {
      setState(() {});
    }
  }

  Future<void> _load() async {
    final token = widget.auth.authorizationHeader;
    if (token == null || _apiBase.isEmpty) return;
    final serial = ++_serial;
    setState(() {
      _loading = true;
      _failed = false;
    });
    try {
      final responses = await Future.wait([
        _client.get(
          _endpoint('/v1/me/blocks'),
          headers: {'Authorization': token},
        ),
        _client.get(
          _endpoint('/v1/me/consents'),
          headers: {'Authorization': token},
        ),
      ]).timeout(const Duration(seconds: 12));
      if (responses.any((response) => response.statusCode != 200)) {
        throw StateError('Settings unavailable');
      }
      if (!mounted || serial != _serial || !widget.auth.signedIn) return;
      setState(() {
        _blocks = [
          for (final raw
              in (jsonDecode(responses[0].body) as Map<String, dynamic>)['data']
                  as List<dynamic>)
            raw as Map<String, dynamic>,
        ];
        _grants = [
          for (final raw
              in (jsonDecode(responses[1].body) as Map<String, dynamic>)['data']
                  as List<dynamic>)
            raw as Map<String, dynamic>,
        ];
      });
    } catch (_) {
      if (mounted && serial == _serial) setState(() => _failed = true);
    } finally {
      if (mounted && serial == _serial) setState(() => _loading = false);
    }
  }

  Future<void> _remove(String path, String id) async {
    final token = widget.auth.authorizationHeader;
    if (token == null || _workingID != null) return;
    setState(() => _workingID = id);
    try {
      final response = await _client
          .delete(_endpoint(path), headers: {'Authorization': token})
          .timeout(const Duration(seconds: 12));
      if (response.statusCode != 204) {
        throw StateError('Settings update failed');
      }
      if (mounted) await _load();
    } catch (_) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(content: Text('Could not update privacy settings.')),
        );
      }
    } finally {
      if (mounted) setState(() => _workingID = null);
    }
  }

  void _openProfile() {
    Navigator.push(
      context,
      MaterialPageRoute<void>(
        builder: (context) => Scaffold(
          appBar: AppBar(title: const Text('Profile')),
          body: LegacyProfilePage(
            auth: widget.auth,
            city: widget.city,
            moments: widget.moments,
          ),
        ),
      ),
    );
  }

  @override
  void dispose() {
    ++_serial;
    widget.auth.removeListener(_onAuthChanged);
    if (widget.client == null) _client.close();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    if (_apiBase.isEmpty) {
      return const Center(child: Text('Settings requires the Birdtie API.'));
    }
    if (!widget.auth.signedIn) {
      return const Center(child: Text('Sign in to manage your settings.'));
    }
    final now = DateTime.now();
    final activeGrants = _grants.where((grant) {
      if (grant['revokedAt'] != null) return false;
      final expiry = DateTime.tryParse(grant['expiresAt'] as String? ?? '');
      return expiry == null || expiry.isAfter(now);
    });
    return RefreshIndicator(
      onRefresh: _load,
      child: ListView(
        padding: const EdgeInsets.fromLTRB(18, 20, 18, 36),
        children: [
          Text(
            widget.auth.displayName ?? 'Birdtie account',
            style: Theme.of(context).textTheme.titleLarge,
          ),
          Text(
            widget.auth.loginMethod == 'dev_phone'
                ? 'Local development account · phone ownership not verified'
                : 'Signed in to Birdtie',
            style: const TextStyle(color: Color(0xFF747B73)),
          ),
          const SizedBox(height: 16),
          ListTile(
            leading: const Icon(Icons.person_outline),
            title: const Text('Profile and visibility'),
            subtitle: const Text('Edit your public name, bio and visibility.'),
            onTap: _openProfile,
          ),
          const Divider(height: 32),
          Text(
            'Privacy and safety',
            style: Theme.of(context).textTheme.titleMedium,
          ),
          const SizedBox(height: 8),
          if (_loading) const LinearProgressIndicator(),
          if (_failed)
            TextButton(
              onPressed: _load,
              child: const Text('Privacy settings unavailable. Tap to retry.'),
            ),
          const SizedBox(height: 16),
          const Text('Blocked accounts'),
          if (!_loading && !_failed && _blocks.isEmpty)
            const Text('No blocked accounts.'),
          for (final block in _blocks)
            ListTile(
              title: Text(block['accountId'] as String? ?? 'Account'),
              subtitle: const Text(
                'Profile and public activity hidden from each other.',
              ),
              trailing: TextButton(
                onPressed: _workingID != null
                    ? null
                    : () => _remove(
                        '/v1/me/blocks/${Uri.encodeComponent(block['accountId'] as String)}',
                        block['accountId'] as String,
                      ),
                child: const Text('Unblock'),
              ),
            ),
          const SizedBox(height: 20),
          const Text('Profile access grants'),
          if (!_loading && !_failed && activeGrants.isEmpty)
            const Text('No active profile access grants.'),
          for (final grant in activeGrants)
            ListTile(
              title: Text(grant['recipientAccountId'] as String? ?? 'Account'),
              subtitle: Text('Access until ${grant['expiresAt'] ?? 'revoked'}'),
              trailing: TextButton(
                onPressed: _workingID != null
                    ? null
                    : () => _remove(
                        '/v1/me/consents/${Uri.encodeComponent(grant['id'] as String)}',
                        grant['id'] as String,
                      ),
                child: const Text('Revoke'),
              ),
            ),
          const SizedBox(height: 14),
          const Text(
            'Unblocking does not restore grants you previously revoked.',
            style: TextStyle(color: Color(0xFF747B73), fontSize: 12),
          ),
          const Divider(height: 40),
          OutlinedButton.icon(
            onPressed: widget.auth.busy ? null : widget.auth.signOut,
            icon: const Icon(Icons.logout),
            label: const Text('Sign out'),
          ),
        ],
      ),
    );
  }
}
