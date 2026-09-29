import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;

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

  factory OrganizationWorkspace.fromJson(Map<String, dynamic> json) =>
      OrganizationWorkspace(
        id: json['id'] as String,
        name: json['name'] as String,
        role: json['role'] as String,
        organizationType: json['organizationType'] as String,
      );
}

class OrganizationWorkspaceController extends ChangeNotifier {
  OrganizationWorkspaceController({required this.authorizationHeader});
  final String? Function() authorizationHeader;
  static const _base = String.fromEnvironment('BIRDTIE_API_BASE_URL');
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
    loading = true;
    notifyListeners();
    try {
      final response = await http.get(
        Uri.parse('$_base/v1/me/organizations'),
        headers: _headers,
      );
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
    } catch (_) {
      // Keep the last known workspace list if the organization service is unavailable.
    } finally {
      loading = false;
      notifyListeners();
    }
  }

  Future<void> create(String name, String organizationType) async {
    if (_base.isEmpty) throw StateError('api_unavailable');
    final response = await http.post(
      Uri.parse('$_base/v1/me/organizations'),
      headers: {..._headers, 'Content-Type': 'application/json'},
      body: jsonEncode({'name': name, 'organizationType': organizationType}),
    );
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
    organizations = const [];
    active = null;
    loading = false;
    notifyListeners();
  }
}
