import 'package:flutter/foundation.dart';

/// Debug-only metadata for the latest Agent request. Never stores credentials or query text.
class AgentDebugDiagnostics extends ChangeNotifier {
  AgentDebugDiagnostics();

  static final AgentDebugDiagnostics instance = AgentDebugDiagnostics();

  String? requestId;
  String? apiBase;
  String? status;
  String? error;
  int? resultCount;
  bool authenticated = false;

  void started({
    required String requestId,
    required String apiBase,
    required bool authenticated,
  }) {
    if (!kDebugMode) return;
    this.requestId = requestId;
    this.apiBase = apiBase;
    this.authenticated = authenticated;
    status = '请求中';
    error = null;
    resultCount = null;
    notifyListeners();
  }

  void finished({
    required String requestId,
    required String status,
    required int? resultCount,
    String? error,
  }) {
    if (!kDebugMode || requestId != this.requestId) return;
    this.status = status;
    this.resultCount = resultCount;
    this.error = error;
    notifyListeners();
  }
}
