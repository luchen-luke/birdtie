import 'package:flutter/material.dart';
import 'package:url_launcher/url_launcher.dart';

import 'amap_session_consent.dart';
import 'map_privacy_consent.dart';

final _openPrivacySheets = <MapPrivacyConsentController, Future<bool>>{};

/// Shows the actual SDK disclosure, not a provider-selection/enable form.
/// A current refusal is respected until the person explicitly asks to review.
Future<bool> showMapServicePrivacySheet(
  BuildContext context, {
  MapPrivacyConsentController? controller,
  required bool Function() current,
  bool reviewExisting = false,
}) async {
  final consent = controller ?? AMapSessionConsent.controller;
  await consent.load();
  if (!context.mounted || !current()) return false;
  if (consent.hasDecision && !reviewExisting) return consent.accepted;
  final existing = _openPrivacySheets[consent];
  if (existing != null) {
    final accepted = await existing;
    return accepted && context.mounted && current() && consent.accepted;
  }
  final shown = showDialog<bool>(
    context: context,
    builder: (_) => _MapPrivacyDialog(consent: consent, current: current),
  ).then((result) => result == true);
  _openPrivacySheets[consent] = shown;
  try {
    return await shown && context.mounted && current() && consent.accepted;
  } finally {
    if (identical(_openPrivacySheets[consent], shown)) {
      _openPrivacySheets.remove(consent);
    }
  }
}

class _MapPrivacyDialog extends StatefulWidget {
  const _MapPrivacyDialog({required this.consent, required this.current});
  final MapPrivacyConsentController consent;
  final bool Function() current;
  @override
  State<_MapPrivacyDialog> createState() => _MapPrivacyDialogState();
}

class _MapPrivacyDialogState extends State<_MapPrivacyDialog> {
  bool _choosing = false;
  String? _linkError;

  Future<void> _choose(bool accepted) async {
    if (_choosing || widget.consent.busy) return;
    if (!widget.current()) {
      Navigator.pop(context, false);
      return;
    }
    setState(() => _choosing = true);
    final saved = await widget.consent.setDecision(accepted);
    if (!mounted) return;
    if (saved) {
      Navigator.pop(context, accepted && widget.current());
    } else {
      setState(() => _choosing = false);
    }
  }

  Future<void> _openPolicy() async {
    if (!widget.current()) return;
    var opened = false;
    try {
      opened = await launchUrl(
        Uri.parse(mapPrivacyPolicyURL),
        mode: LaunchMode.externalApplication,
      );
    } catch (_) {}
    if (mounted)
      setState(() => _linkError = opened ? null : '暂时无法打开隐私权政策，请稍后重试。');
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: widget.consent,
    builder: (context, _) {
      final busy = _choosing || widget.consent.busy;
      final error = widget.consent.error;
      return PopScope(
        canPop: !busy,
        child: AlertDialog(
          title: const Text('地图服务隐私说明'),
          content: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 420),
            child: SingleChildScrollView(
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  const Text(mapPrivacyDisclosure),
                  const SizedBox(height: 12),
                  TextButton(
                    onPressed: busy ? null : _openPolicy,
                    child: const Text('查看高德地图开放平台隐私权政策'),
                  ),
                  if (_linkError != null)
                    Text(
                      _linkError!,
                      style: TextStyle(
                        color: Theme.of(context).colorScheme.error,
                      ),
                    ),
                  if (error != null) ...[
                    const SizedBox(height: 8),
                    Semantics(liveRegion: true, child: Text(error)),
                    TextButton(
                      onPressed: busy ? null : widget.consent.load,
                      child: const Text('重试读取'),
                    ),
                  ],
                ],
              ),
            ),
          ),
          actions: [
            TextButton(
              onPressed: busy || error != null ? null : () => _choose(false),
              child: const Text('暂不使用'),
            ),
            FilledButton(
              onPressed: busy || error != null ? null : () => _choose(true),
              child: Text(busy ? '正在保存…' : '同意'),
            ),
          ],
        ),
      );
    },
  );
}
