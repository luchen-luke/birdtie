class AgentRequestFailure implements Exception {
  const AgentRequestFailure(this.userMessage, {this.statusCode, this.code});

  final String userMessage;
  final int? statusCode;
  final String? code;

  bool get needsCity => code == 'NEEDS_CITY';

  bool get _aiServiceUnavailable =>
      statusCode == 503 && code == 'ai_service_unavailable';

  bool get permitsUnchangedRetry =>
      !needsCity &&
      !_aiServiceUnavailable &&
      statusCode != 401 &&
      statusCode != 403 &&
      code != 'online_result_unknown' &&
      code != 'QUERY_TOO_LONG' &&
      code != 'PUBLIC_CONTEXT_EXPIRED';

  String? get recoveryMessage {
    if (statusCode == 401) {
      return '重新登录后可继续；原内容不会自动发送。';
    }
    if (statusCode == 403) {
      return '请先确认当前身份和访问权限；恢复权限后再发起查询。';
    }
    if (code == 'online_result_unknown') {
      return '请先从最近对话核对提交结果；不会重复发送原请求。';
    }
    if (code == 'QUERY_TOO_LONG') {
      return '请缩短内容或开始新任务。';
    }
    if (code == 'PUBLIC_CONTEXT_EXPIRED') {
      return '本轮范围或来源已变化，请重新发起查询。';
    }
    if (_aiServiceUnavailable) {
      return '可从最近对话查看之前的内容，服务恢复后再发起查询。';
    }
    return null;
  }

  @override
  String toString() => userMessage;
}
