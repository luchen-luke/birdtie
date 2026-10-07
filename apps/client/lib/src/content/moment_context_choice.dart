import 'package:flutter/material.dart';

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
    return Padding(
      padding: const EdgeInsets.only(bottom: 10),
      child: DropdownButtonFormField<String>(
        initialValue: value,
        isExpanded: true,
        decoration: InputDecoration(labelText: label),
        items: [
          const DropdownMenuItem(value: '', child: Text('不关联')),
          for (final entry in options.entries)
            DropdownMenuItem(
              value: entry.key,
              child: Text(entry.value, overflow: TextOverflow.ellipsis),
            ),
        ],
        onChanged: (selected) => onChanged(selected ?? ''),
      ),
    );
  }
}
