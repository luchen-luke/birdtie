class AgentRequestFailure implements Exception {
  const AgentRequestFailure(this.userMessage, {this.statusCode, this.code});

  final String userMessage;
  final int? statusCode;
  final String? code;

  bool get needsCity => code == 'NEEDS_CITY';

  bool get permitsUnchangedRetry =>
      !needsCity &&
      statusCode != 401 &&
      statusCode != 403 &&
      code != 'online_result_unknown' &&
      code != 'QUERY_TOO_LONG' &&
      code != 'PUBLIC_CONTEXT_EXPIRED';

  String? get recoveryMessage {
    if (statusCode == 401) {
      return '请先通过个人资料重新登录，再发起查询。';
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
    return null;
  }

  @override
  String toString() => userMessage;
}
