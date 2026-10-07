import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';

class OrganizationWorkspace {
  const OrganizationWorkspace({
    required this.id,
    required this.name,
    required this.role,
    required this.organizationType,
  });
  final String id;
  final String name;
  final String role;
  final String organizationType;

  bool get canManage => const {'owner', 'admin'}.contains(role.toLowerCase());

  factory OrganizationWorkspace.fromJson(Map<String, dynamic> json) =>
      OrganizationWorkspace(
        id: json['id'] as String,
        name: json['name'] as String,
        role: json['role'] as String,
        organizationType: json['organizationType'] as String,
      );
}

class OrganizationWorkspaceController extends ChangeNotifier {
  OrganizationWorkspaceController({
    required this.authorizationHeader,
    http.Client? client,
    String? apiBaseUrl,
  }) : _client = client ?? http.Client(),
       _ownsClient = client == null,
       _base = apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl;
  final String? Function() authorizationHeader;
  final String _base;
  final http.Client _client;
  final bool _ownsClient;
  int _request = 0;
  bool _closed = false;
  List<OrganizationWorkspace> organizations = const [];
  OrganizationWorkspace? active;
  bool loading = false;

  Map<String, String> get _headers {
    final headers = <String, String>{'Accept': 'application/json'};
    final authorization = authorizationHeader();
    if (authorization != null) headers['Authorization'] = authorization;
    return headers;
  }

  Future<void> load() async {
    final auth = authorizationHeader();
    if (auth == null || _base.isEmpty) {
      clear();
      return;
    }
    final request = ++_request;
    bool current() =>
        !_closed && request == _request && authorizationHeader() == auth;
    loading = true;
    notifyListeners();
    try {
      final response = await _client.get(
        Uri.parse('$_base/v1/me/organizations'),
        headers: {'Accept': 'application/json', 'Authorization': auth},
      );
      if (!current()) return;
      if (response.statusCode == 401 || response.statusCode == 403) {
        clear();
        return;
      }
      if (response.statusCode != 200) {
        throw StateError('organization_list_failed');
      }
      final data =
          (jsonDecode(response.body) as Map<String, dynamic>)['data']
              as List<dynamic>;
      organizations = data
          .map(
            (item) =>
                OrganizationWorkspace.fromJson(item as Map<String, dynamic>),
          )
          .toList();
      if (active != null) {
        OrganizationWorkspace? refreshed;
        for (final item in organizations) {
          if (item.id == active!.id) {
            refreshed = item;
            break;
          }
        }
        active = refreshed?.canManage == true ? refreshed : null;
      }
    } catch (_) {
      // Keep the last known workspace list if the organization service is unavailable.
    } finally {
      if (current()) {
        loading = false;
        notifyListeners();
      }
    }
  }

  Future<void> create(String name, String organizationType) async {
    if (_base.isEmpty) throw StateError('api_unavailable');
    final auth = authorizationHeader();
    if (auth == null) throw StateError('authorization_unavailable');
    final request = ++_request;
    loading = false;
    notifyListeners();
    final response = await _client.post(
      Uri.parse('$_base/v1/me/organizations'),
      headers: {..._headers, 'Content-Type': 'application/json'},
      body: jsonEncode({'name': name, 'organizationType': organizationType}),
    );
    if (_closed || request != _request || authorizationHeader() != auth) {
      throw StateError('organization_context_changed');
    }
    if (response.statusCode == 401 || response.statusCode == 403) {
      clear();
      throw StateError('organization_authorization_changed');
    }
    if (response.statusCode != 201) {
      throw StateError('organization_create_failed');
    }
    final data =
        (jsonDecode(response.body) as Map<String, dynamic>)['data']
            as Map<String, dynamic>;
    final created = OrganizationWorkspace.fromJson(data);
    organizations = [...organizations, created];
    active = created;
    notifyListeners();
  }

  void select(OrganizationWorkspace? organization) {
    active = organization;
    notifyListeners();
  }

  void clear() {
    ++_request;
    organizations = const [];
    active = null;
    loading = false;
    notifyListeners();
  }

  @override
  void dispose() {
    _closed = true;
    ++_request;
    if (_ownsClient) _client.close();
    super.dispose();
  }
}
