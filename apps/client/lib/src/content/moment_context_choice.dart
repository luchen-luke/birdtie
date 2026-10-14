import 'package:flutter/material.dart';
import '../workspace/now_ui_tokens.dart';

class MomentContextChoice extends StatelessWidget {
  const MomentContextChoice({
    super.key,
    required this.label,
    required this.value,
    required this.available,
    required this.onChanged,
  });

  final String label;
  final String value;
  final Map<String, String> available;
  final ValueChanged<String> onChanged;

  @override
  Widget build(BuildContext context) {
    final options = <String, String>{...available};
    if (value.isNotEmpty && !options.containsKey(value)) {
      options[value] = '此前关联的$label';
    }
    Widget choiceLabel(String title) => ConstrainedBox(
      constraints: const BoxConstraints(minHeight: 48),
      child: Padding(
        padding: const EdgeInsets.symmetric(vertical: 8),
        child: Text(title),
      ),
    );
    return Padding(
      padding: const EdgeInsets.only(bottom: 10),
      child: DropdownButtonFormField<String>(
        initialValue: value,
        isExpanded: true,
        isDense: false,
        itemHeight: null,
        style: NowUiTokens.of(context).body,
        decoration: InputDecoration(labelText: label),
        selectedItemBuilder: (context) => [
          value.isEmpty ? choiceLabel('不关联') : const SizedBox.shrink(),
          for (final entry in options.entries)
            entry.key == value
                ? choiceLabel(entry.value)
                : const SizedBox.shrink(),
        ],
        items: [
          DropdownMenuItem(value: '', child: choiceLabel('不关联')),
          for (final entry in options.entries)
            DropdownMenuItem(value: entry.key, child: choiceLabel(entry.value)),
        ],
        onChanged: (selected) => onChanged(selected ?? ''),
      ),
    );
  }
}
