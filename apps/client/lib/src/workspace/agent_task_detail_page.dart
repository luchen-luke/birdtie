import 'dart:async';
import 'package:flutter/material.dart';
import 'agent_workspace_controller.dart';
import 'agent_request_failure.dart';
import 'remote_agent_task_source.dart';

/// Human read of the original task. No model run, inferred approval or retry
/// of the task's business effects is performed by this page.
class AgentTaskDetailPage extends StatefulWidget {
  const AgentTaskDetailPage({
    super.key,
    required this.taskID,
    required this.ownerID,
    required this.source,
  });
  final String taskID, ownerID;
  final RemoteAgentTaskSource source;
  @override
  State<AgentTaskDetailPage> createState() => _AgentTaskDetailPageState();
}

class _AgentTaskDetailPageState extends State<AgentTaskDetailPage> {
  AgentResult? _result;
  bool _loading = false;
  String? _error;
  String? _recovery;
  int _serial = 0;
  @override
  void initState() {
    super.initState();
    unawaited(_read());
  }

  Future<void> _read() async {
    final serial = ++_serial;
    setState(() {
      _loading = true;
      _error = null;
      _recovery = null;
      _result = null;
    });
    try {
      final result = await widget.source.readByID(
        widget.taskID,
        ownerID: widget.ownerID,
      );
      if (mounted && serial == _serial) setState(() => _result = result);
    } catch (error) {
      if (mounted && serial == _serial) {
        setState(() {
          _error = error is AgentRequestFailure
              ? error.userMessage
              : '任务已失效、不可访问或暂时无法读取。请重新核实。';
          _recovery = switch (error) {
            AgentRequestFailure(statusCode: 401) => '请先通过个人资料重新登录，再核实当前任务。',
            AgentRequestFailure(statusCode: 403) =>
              '请先确认当前身份和访问权限；恢复权限后再核实当前任务。',
            AgentRequestFailure(code: 'online_result_unknown') =>
              '原提交结果尚未确认；这里只读取原任务状态，不会重新提交。',
            _ => null,
          };
        });
      }
    } finally {
      if (mounted && serial == _serial) setState(() => _loading = false);
    }
  }

  @override
  void didUpdateWidget(AgentTaskDetailPage old) {
    super.didUpdateWidget(old);
    if (old.taskID != widget.taskID ||
        old.ownerID != widget.ownerID ||
        old.source != widget.source) {
      unawaited(_read());
    }
  }

  @override
  void dispose() {
    ++_serial;
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: const Text('我的任务')),
    body: _loading
        ? const Center(child: CircularProgressIndicator())
        : ListView(
            padding: const EdgeInsets.all(24),
            children: [
              if (_error != null) ...[
                Text(_error!),
                if (_recovery != null) Text(_recovery!),
                TextButton(onPressed: _read, child: const Text('重新核实')),
              ],
              if (_result case final result?) ...[
                Text(
                  result.task!.query,
                  style: Theme.of(context).textTheme.titleLarge,
                ),
                const SizedBox(height: 12),
                Text(switch (result.task!.status) {
                  'COMPLETED' => '查询完成',
                  'FAILED' => '查询未完成',
                  'ACTIVE' => '处理中',
                  _ => '当前状态暂不可识别',
                }),
                const SizedBox(height: 16),
                Text(result.responseMessage),
                const SizedBox(height: 16),
                const Text('这里只查看原任务及当前可见结果，不会重新提交、报名或执行其他动作。'),
                for (final activity in result.activities)
                  ListTile(
                    title: Text(activity.title),
                    subtitle: Text(activity.schedule),
                  ),
                for (final place in result.places)
                  ListTile(
                    title: Text(place.name),
                    subtitle: const Text('当前可见地点'),
                  ),
                for (final organization in result.organizations)
                  ListTile(
                    title: Text(organization.name),
                    subtitle: const Text('当前可见组织'),
                  ),
                TextButton(onPressed: _read, child: const Text('刷新当前状态')),
              ],
            ],
          ),
  );
}
