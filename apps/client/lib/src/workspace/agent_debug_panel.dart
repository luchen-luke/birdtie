import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';

import 'agent_debug_diagnostics.dart';

class AgentDebugPanel extends StatelessWidget {
  const AgentDebugPanel({
    super.key,
    required this.apiBase,
    required this.signedIn,
  });

  final String apiBase;
  final bool signedIn;

  @override
  Widget build(BuildContext context) {
    if (!kDebugMode) return const SizedBox.shrink();
    return AnimatedBuilder(
      animation: AgentDebugDiagnostics.instance,
      builder: (context, _) {
        final data = AgentDebugDiagnostics.instance;
        final uri = Uri.tryParse(data.apiBase ?? apiBase);
        final endpoint = uri == null || uri.host.isEmpty
            ? '未请求'
            : '${uri.scheme}://${uri.host}${uri.hasPort ? ':${uri.port}' : ''}';
        final environment = uri == null || uri.host.isEmpty
            ? '未配置'
            : (uri.host == '127.0.0.1' || uri.host == 'localhost')
            ? '本地开发'
            : '远程环境';
        return Card(
          child: Padding(
            padding: const EdgeInsets.all(12),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                const Text(
                  '调试诊断',
                  style: TextStyle(fontWeight: FontWeight.w700),
                ),
                Text('环境：$environment · API：$endpoint'),
                Text('当前登录：${signedIn ? '是' : '否'}'),
                if (data.requestId != null)
                  Text('请求时登录：${data.authenticated ? '是' : '否'}'),
                Text('最近状态：${data.status ?? '无请求'}'),
                Text('结果数：${data.resultCount?.toString() ?? '—'}'),
                Text('请求 ID：${data.requestId ?? '—'}'),
                if (data.error != null) Text('错误：${data.error}'),
              ],
            ),
          ),
        );
      },
    );
  }
}
