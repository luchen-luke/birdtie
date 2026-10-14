import 'dart:async';
import 'package:flutter/material.dart';
import 'system_push_controller.dart';

class SystemPushSettingsPage extends StatefulWidget {
  const SystemPushSettingsPage({super.key, required this.controller});
  final SystemPushController controller;
  @override
  State<SystemPushSettingsPage> createState() => _PushSettingsState();
}

class _PushSettingsState extends State<SystemPushSettingsPage> {
  @override
  void initState() {
    super.initState();
    unawaited(widget.controller.initialize());
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: widget.controller,
    builder: (context, _) {
      final c = widget.controller;
      return Scaffold(
        appBar: AppBar(title: const Text('系统通知')),
        body: SafeArea(
          child: ListView(
            padding: const EdgeInsets.all(20),
            children: [
              const Text('启用后，本设备可收到新的通知提示。系统提示只显示“有新的通知，打开查看”；消息内容仍在应用内读取。'),
              const SizedBox(height: 16),
              Text(
                c.view == null
                    ? '本设备通知状态尚未核实'
                    : c.enabled
                    ? '本设备已启用系统通知'
                    : '本设备尚未启用系统通知',
              ),
              if (c.error != null)
                Padding(
                  padding: const EdgeInsets.symmetric(vertical: 16),
                  child: Semantics(liveRegion: true, child: Text(c.error!)),
                ),
              if (c.busy)
                const Padding(
                  padding: EdgeInsets.all(16),
                  child: Center(child: CircularProgressIndicator()),
                ),
              if (c.uncertain)
                OutlinedButton(
                  key: const Key('system-push-recover'),
                  onPressed: c.current && !c.busy ? c.recover : null,
                  child: const Text('核实原操作'),
                )
              else ...[
                FilledButton(
                  key: const Key('system-push-enable'),
                  onPressed: c.canEnable ? c.enable : null,
                  child: Text(c.enabled ? '重新检查权限并绑定' : '启用系统通知'),
                ),
                if (c.view?.enabled == true)
                  OutlinedButton(
                    key: const Key('system-push-disable'),
                    onPressed: c.current && !c.busy ? c.disable : null,
                    child: const Text('停用本设备通知'),
                  ),
                TextButton(
                  key: const Key('system-push-refresh'),
                  onPressed: c.current && !c.busy ? c.refresh : null,
                  child: const Text('刷新状态'),
                ),
              ],
              const SizedBox(height: 16),
              const Text('切号、会话到期或权限变化后，旧通知须重新核实。停用会阻止后续投递，已在途的系统提示仍可能出现。'),
              const SizedBox(height: 16),
              const Text('操作结果不确定时会保留原操作编号；不会自动重复注册或投递。'),
            ],
          ),
        ),
      );
    },
  );
}
