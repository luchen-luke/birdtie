import 'package:flutter/material.dart';

import 'verified_booking_controller.dart';

String _localDate(DateTime value) {
  final d = value.toLocal();
  String two(int v) => v.toString().padLeft(2, '0');
  return '${d.year}年${two(d.month)}月${two(d.day)}日 ${two(d.hour)}:${two(d.minute)}';
}

class VerifiedBookingSection extends StatelessWidget {
  const VerifiedBookingSection({super.key, required this.controller});
  final VerifiedBookingController controller;

  Future<void> _preview(BuildContext context) async {
    final preview = await controller.prepare();
    if (preview == null || !context.mounted) return;
    final approved = await showDialog<bool>(
      context: context,
      useRootNavigator: false,
      builder: (context) => AnimatedBuilder(
        animation: controller,
        builder: (context, _) {
          final current = controller.isCurrent(preview);
          return AlertDialog(
            scrollable: true,
            title: const Text('打开外部预约页面？'),
            content: SingleChildScrollView(
              child: current
                  ? Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        const Text('将离开 Birdtie。打开页面不代表已预约，价格和空位以对方页面确认为准。'),
                        SelectableText('预约地址：${preview.metadata.bookingURL}'),
                        SelectableText('资料来源：${preview.metadata.sourceURL}'),
                        Text('审核时间：${_localDate(preview.metadata.reviewedAt)}'),
                        Text('有效至：${_localDate(preview.metadata.expiresAt)}'),
                      ],
                    )
                  : const Text('资料、登录状态或工作区已变化，请返回检查后重新确认。'),
            ),
            actions: [
              TextButton(
                style: TextButton.styleFrom(minimumSize: const Size(48, 48)),
                onPressed: () => Navigator.pop(context, false),
                child: const Text('取消'),
              ),
              FilledButton(
                style: FilledButton.styleFrom(minimumSize: const Size(48, 48)),
                onPressed: current ? () => Navigator.pop(context, true) : null,
                child: const Text('确认打开'),
              ),
            ],
          );
        },
      ),
    );
    if (approved == true) {
      await controller.approve(preview);
    } else {
      controller.cancel(preview);
    }
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: controller,
    builder: (context, _) {
      final metadata = controller.currentIdentity ? controller.metadata : null;
      return Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Text('未提供实时可预约信息；是否可订以外部页面确认为准。'),
          if (metadata != null) ...[
            SelectableText('预约资料来源：${metadata.sourceURL}'),
            Text('审核时间：${_localDate(metadata.reviewedAt)}'),
            Text('有效至：${_localDate(metadata.expiresAt)}'),
            if (metadata.support == 'contact')
              const Text('未提供具体联系方式，可查看资料来源了解。'),
            if (metadata.support == 'unknown') const Text('预约方式尚未核实。'),
            if (metadata.support == 'none') const Text('未提供预约方式。'),
            if (metadata.bookingURL != null)
              TextButton(
                style: TextButton.styleFrom(minimumSize: const Size(48, 48)),
                onPressed: controller.canPrepare
                    ? () => _preview(context)
                    : null,
                child: const Text('查看外部预约说明'),
              ),
          ],
          if (controller.busy)
            const LinearProgressIndicator(semanticsLabel: '正在核验预约资料'),
          if (controller.error != null)
            Semantics(liveRegion: true, child: Text(controller.error!)),
          if (controller.telemetryNotice != null)
            Semantics(
              liveRegion: true,
              child: Text(controller.telemetryNotice!),
            ),
          const Text(
            '确认打开后会报告地点入口的外跳统计，用于评估入口是否可用；不包含账号、外部链接或个人坐标，保留政策为30天。这不证明已预约。',
          ),
          if (controller.error != null)
            TextButton(
              style: TextButton.styleFrom(minimumSize: const Size(48, 48)),
              onPressed: controller.busy ? null : () => _preview(context),
              child: const Text('刷新预约资料'),
            ),
        ],
      );
    },
  );
}
