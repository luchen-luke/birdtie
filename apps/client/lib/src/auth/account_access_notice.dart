import 'package:flutter/material.dart';

// A compact presentation of the caller's actual authentication state. Callers
// retain the existing identity/session guards and supply real navigation only.
class AccountAccessNotice extends StatelessWidget {
  const AccountAccessNotice({
    super.key,
    required this.title,
    required this.message,
    this.restoring = false,
    this.actionLabel,
    this.onAction,
  });
  final String title, message;
  final bool restoring;
  final String? actionLabel;
  final VoidCallback? onAction;

  @override
  Widget build(BuildContext context) => SingleChildScrollView(
    padding: const EdgeInsets.all(24),
    child: Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Text(title, style: Theme.of(context).textTheme.titleMedium),
        const SizedBox(height: 12),
        Text(message, style: Theme.of(context).textTheme.bodyMedium),
        if (restoring) ...[
          const SizedBox(height: 16),
          Semantics(label: '正在恢复账户', child: const LinearProgressIndicator()),
        ],
        if (onAction != null && actionLabel != null && !restoring) ...[
          const SizedBox(height: 20),
          FilledButton(
            onPressed: onAction,
            style: FilledButton.styleFrom(
              minimumSize: const Size.fromHeight(48),
            ),
            child: Text(actionLabel!),
          ),
        ],
      ],
    ),
  );
}
