import 'package:flutter/material.dart';
import 'journey_detail.dart';
import 'collection_control.dart';

class JourneyDetailEditor extends StatelessWidget {
  const JourneyDetailEditor({
    super.key,
    required this.value,
    required this.current,
    required this.onChanged,
    required this.changes,
  });
  final JourneyDetails value;
  final Listenable changes;
  final bool Function() current;
  final ValueChanged<JourneyDetails> onChanged;
  static const _labels = {
    'title': '名称（可选）',
    'note': '安排说明',
    'kind': '类型',
    'dayIndex': '第几天',
    'startDay': '开始日序',
    'endDay': '结束日序',
    'startDate': '开始日期（YYYY-MM-DD）',
    'endDate': '结束日期（YYYY-MM-DD）',
    'startTime': '开始时间（HH:mm）',
    'endTime': '结束时间（HH:mm）',
    'timeLabel': '时间说明',
    'transportMode': '交通方式',
    'serviceNumber': '班次',
    'departureName': '出发地',
    'arrivalName': '到达地',
    'customAddress': '地址说明',
    'sourceLabel': '参考来源',
    'sourceText': '参考原文',
  };
  static String name(JourneyEntry e) => e.text('title').isNotEmpty
      ? e.text('title')
      : switch (e.type) {
          'STOP' => '停留点',
          'ARRANGEMENT' => '行程安排',
          _ => '笔记',
        };
  static String reviewText(JourneyEntry e, JourneyDetails details) {
    final names = {for (final row in details.itinerary) row.id: name(row)};
    return [
      '${e.values['position']}. ${name(e)}',
      for (final key in _labels.keys)
        if (journeyDayFields.contains(key)) ...[
          if (e.day(key) != null) '${_labels[key]}：${e.day(key)}',
        ] else if (key != 'title' && e.text(key).isNotEmpty)
          '${_labels[key]}：${e.text(key)}',
      for (final pair in const {
        'stopId': '关联停留点',
        'fromStopId': '出发停留点',
        'toStopId': '到达停留点',
      }.entries)
        if (e.text(pair.key).isNotEmpty)
          '${pair.value}：${names[e.text(pair.key)] ?? '停留点暂不可读'}',
    ].join('\n');
  }

  Future<void> _header(BuildContext context) async {
    if (!current()) return;
    final fields = {
      'startDate': TextEditingController(text: value.startDate),
      'endDate': TextEditingController(text: value.endDate),
      'dateLabel': TextEditingController(text: value.dateLabel),
      'days': TextEditingController(
        text: value.days == 0 ? '' : '${value.days}',
      ),
    };
    String? error;
    try {
      final next = await showDialog<JourneyDetails>(
        context: context,
        builder: (context) => ListenableBuilder(
          listenable: changes,
          builder: (context, _) => !current()
              ? AlertDialog(
                  title: const Text('记录已变化'),
                  content: const Text('请关闭后重新打开当前记录。'),
                  actions: [
                    TextButton(
                      onPressed: () => Navigator.pop(context),
                      child: const Text('关闭'),
                    ),
                  ],
                )
              : StatefulBuilder(
                  builder: (context, setState) => AlertDialog(
                    title: const Text('旅程时间'),
                    scrollable: true,
                    content: Column(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        for (final e in fields.entries)
                          TextField(
                            controller: e.value,
                            decoration: InputDecoration(
                              labelText: {
                                'startDate': '开始日期（YYYY-MM-DD）',
                                'endDate': '结束日期（YYYY-MM-DD）',
                                'dateLabel': '日期说明',
                                'days': '计划天数（可选）',
                              }[e.key],
                            ),
                          ),
                        if (error != null) Text(error!),
                      ],
                    ),
                    actions: [
                      TextButton(
                        onPressed: () => Navigator.pop(context),
                        child: const Text('取消'),
                      ),
                      FilledButton(
                        onPressed: () {
                          if (!current()) return;
                          try {
                            final d = value.changed(
                              startDate: fields['startDate']!.text,
                              endDate: fields['endDate']!.text,
                              dateLabel: fields['dateLabel']!.text,
                              days: fields['days']!.text.isEmpty
                                  ? 0
                                  : int.parse(fields['days']!.text),
                            );
                            Navigator.pop(context, d);
                          } catch (_) {
                            setState(() => error = '请检查日期和天数；不确定的内容可以留空。');
                          }
                        },
                        child: const Text('保留修改'),
                      ),
                    ],
                  ),
                ),
        ),
      );
      if (next != null && current()) onChanged(next);
    } finally {
      for (final c in fields.values) c.dispose();
    }
  }

  Future<void> _edit(BuildContext context, [int? index]) async {
    if (!current()) return;
    final old = index == null ? null : value.itinerary[index];
    final fields = {
      for (final key in _labels.keys)
        key: TextEditingController(
          text: old == null
              ? ''
              : journeyDayFields.contains(key)
              ? old.day(key)?.toString() ?? ''
              : old.text(key),
        ),
    };
    var type = old?.type ?? 'NOTE';
    final refs = {
      for (final key in ['stopId', 'fromStopId', 'toStopId'])
        key: old?.text(key) ?? '',
    };
    String? error;
    try {
      final next = await showDialog<JourneyDetails>(
        context: context,
        builder: (context) => ListenableBuilder(
          listenable: changes,
          builder: (context, _) => !current()
              ? AlertDialog(
                  title: const Text('记录已变化'),
                  content: const Text('请关闭后重新打开当前记录。'),
                  actions: [
                    TextButton(
                      onPressed: () => Navigator.pop(context),
                      child: const Text('关闭'),
                    ),
                  ],
                )
              : StatefulBuilder(
                  builder: (context, setState) => AlertDialog(
                    title: Text(old == null ? '添加安排' : '编辑安排'),
                    scrollable: true,
                    content: SizedBox(
                      width: 480,
                      child: Column(
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          DropdownButtonFormField<String>(
                            initialValue: type,
                            decoration: const InputDecoration(
                              labelText: '内容类型',
                            ),
                            items: const [
                              DropdownMenuItem(
                                value: 'STOP',
                                child: Text('停留点'),
                              ),
                              DropdownMenuItem(
                                value: 'ARRANGEMENT',
                                child: Text('行程安排'),
                              ),
                              DropdownMenuItem(
                                value: 'NOTE',
                                child: Text('笔记'),
                              ),
                            ],
                            onChanged: old == null
                                ? (v) => setState(() => type = v!)
                                : null,
                          ),
                          for (final key in [
                            'title',
                            'note',
                            'dayIndex',
                            'startDate',
                            'endDate',
                            'startTime',
                            'endTime',
                            'timeLabel',
                          ])
                            TextField(
                              controller: fields[key],
                              minLines: key == 'note' ? 2 : 1,
                              maxLines: key == 'note' ? 5 : 1,
                              decoration: InputDecoration(
                                labelText: _labels[key],
                              ),
                            ),
                          ExpansionTile(
                            title: const Text('详细安排'),
                            children: [
                              for (final key in [
                                'kind',
                                'startDay',
                                'endDay',
                                'transportMode',
                                'serviceNumber',
                                'departureName',
                                'arrivalName',
                                'customAddress',
                              ])
                                TextField(
                                  controller: fields[key],
                                  decoration: InputDecoration(
                                    labelText: _labels[key],
                                  ),
                                ),
                              if (type == 'ARRANGEMENT')
                                for (final key in refs.keys)
                                  DropdownButtonFormField<String>(
                                    initialValue: refs[key],
                                    isExpanded: true,
                                    decoration: InputDecoration(
                                      labelText: {
                                        'stopId': '关联停留点',
                                        'fromStopId': '出发停留点',
                                        'toStopId': '到达停留点',
                                      }[key],
                                    ),
                                    items: [
                                      const DropdownMenuItem(
                                        value: '',
                                        child: Text('不关联'),
                                      ),
                                      for (final stop in value.itinerary.where(
                                        (e) => e.type == 'STOP',
                                      ))
                                        DropdownMenuItem(
                                          value: stop.id,
                                          child: Text(name(stop)),
                                        ),
                                    ],
                                    onChanged: (v) =>
                                        setState(() => refs[key] = v!),
                                  ),
                            ],
                          ),
                          ExpansionTile(
                            title: const Text('参考资料'),
                            children: [
                              for (final key in ['sourceLabel', 'sourceText'])
                                TextField(
                                  controller: fields[key],
                                  minLines: 1,
                                  maxLines: 5,
                                  decoration: InputDecoration(
                                    labelText: _labels[key],
                                  ),
                                ),
                              if (old?.origin?['sourceConfirmed'] is bool)
                                Text(
                                  old!.origin!['sourceConfirmed'] == true
                                      ? '原资料已确认'
                                      : '原资料尚未确认',
                                ),
                              if (old?.origin?['sourceConfidence'] is num)
                                Text(
                                  '原资料置信度：${old!.origin!['sourceConfidence']}',
                                ),
                            ],
                          ),
                          if (error != null) Text(error!),
                        ],
                      ),
                    ),
                    actions: [
                      TextButton(
                        onPressed: () => Navigator.pop(context),
                        child: const Text('取消'),
                      ),
                      FilledButton(
                        onPressed: () {
                          if (!current()) return;
                          try {
                            final row = JourneyEntry.decode(
                              {
                                if (old != null) ...old.toInput(),
                                'id':
                                    old?.id ??
                                    newCollectionControlOperationID(),
                                'entryType': type,
                                'position': index == null
                                    ? value.itinerary.length + 1
                                    : index + 1,
                                for (final k in journeyTextFields)
                                  k: fields[k]?.text ?? (old?.text(k) ?? ''),
                                for (final k in journeyDayFields)
                                  k: fields[k]!.text.isEmpty
                                      ? null
                                      : int.parse(fields[k]!.text),
                                for (final k in refs.keys)
                                  k: type == 'ARRANGEMENT' ? refs[k] : '',
                                if (old?.origin != null) 'origin': old!.origin,
                              },
                              index == null
                                  ? value.itinerary.length + 1
                                  : index + 1,
                            );
                            final rows = [...value.itinerary];
                            if (index == null) {
                              rows.add(row);
                            } else {
                              rows[index] = row;
                            }
                            Navigator.pop(
                              context,
                              value.changed(itinerary: rows),
                            );
                          } catch (_) {
                            setState(() => error = '请检查日期、时间和日序；不确定的内容可以留空。');
                          }
                        },
                        child: const Text('保留修改'),
                      ),
                    ],
                  ),
                ),
        ),
      );
      if (next != null && current()) onChanged(next);
    } finally {
      for (final c in fields.values) c.dispose();
    }
  }

  void _move(int from, int to) {
    if (!current()) return;
    final rows = [...value.itinerary];
    rows.insert(to, rows.removeAt(from));
    onChanged(value.changed(itinerary: rows));
  }

  Future<void> _remove(BuildContext context, int index) async {
    if (!current()) return;
    final e = value.itinerary[index];
    final linked = value.itinerary.any(
      (row) =>
          ['stopId', 'fromStopId', 'toStopId'].any((k) => row.text(k) == e.id),
    );
    if (linked) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('请先在相关安排中解除这个停留点的关联。')));
      return;
    }
    final yes = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('从草稿中移除这项安排？'),
        content: Text(name(e)),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('移除'),
          ),
        ],
      ),
    );
    if (yes == true && current())
      onChanged(
        value.changed(itinerary: [...value.itinerary]..removeAt(index)),
      );
  }

  @override
  Widget build(BuildContext context) => Column(
    crossAxisAlignment: CrossAxisAlignment.start,
    children: [
      Row(
        children: [
          const Expanded(child: Text('行程安排')),
          TextButton(
            onPressed: current() ? () => _header(context) : null,
            child: const Text('旅程时间'),
          ),
        ],
      ),
      if (value.dateLabel.isNotEmpty) Text(value.dateLabel),
      if (value.startDate.isNotEmpty || value.endDate.isNotEmpty)
        Text(
          [
            value.startDate,
            value.endDate,
          ].where((s) => s.isNotEmpty).join(' — '),
        ),
      if (value.days > 0) Text('${value.days} 天'),
      if (value.itinerary.isEmpty)
        const Padding(
          padding: EdgeInsets.symmetric(vertical: 12),
          child: Text('可以先记下想法，日期和地点以后再补。'),
        ),
      for (var i = 0; i < value.itinerary.length; i++)
        Card(
          margin: const EdgeInsets.symmetric(vertical: 5),
          child: Padding(
            padding: const EdgeInsets.all(12),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text('${i + 1}. ${name(value.itinerary[i])}'),
                if (value.itinerary[i].text('note').isNotEmpty)
                  Text(value.itinerary[i].text('note')),
                Text(
                  [
                    if (value.itinerary[i].day('dayIndex') != null)
                      '第 ${value.itinerary[i].day('dayIndex')} 天',
                    for (final k in [
                      'startDate',
                      'startTime',
                      'timeLabel',
                      'transportMode',
                      'serviceNumber',
                      'departureName',
                      'arrivalName',
                    ])
                      if (value.itinerary[i].text(k).isNotEmpty)
                        value.itinerary[i].text(k),
                  ].join(' · '),
                ),
                Wrap(
                  children: [
                    TextButton(
                      onPressed: current() ? () => _edit(context, i) : null,
                      child: const Text('编辑'),
                    ),
                    IconButton(
                      tooltip: '上移',
                      onPressed: current() && i > 0
                          ? () => _move(i, i - 1)
                          : null,
                      icon: const Icon(Icons.arrow_upward),
                    ),
                    IconButton(
                      tooltip: '下移',
                      onPressed: current() && i + 1 < value.itinerary.length
                          ? () => _move(i, i + 1)
                          : null,
                      icon: const Icon(Icons.arrow_downward),
                    ),
                    IconButton(
                      tooltip: '移除安排',
                      onPressed: current() ? () => _remove(context, i) : null,
                      icon: const Icon(Icons.delete_outline),
                    ),
                  ],
                ),
              ],
            ),
          ),
        ),
      OutlinedButton.icon(
        onPressed: current() && value.itinerary.length < 500
            ? () => _edit(context)
            : null,
        icon: const Icon(Icons.add),
        label: const Text('添加安排'),
      ),
    ],
  );
}
