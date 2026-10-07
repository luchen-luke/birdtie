import 'package:birdtie_client/src/content/moment_time_choice.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('五种精度按 UTC 日历解析，未知不制造时间，非法日期拒绝', () {
    expect(MomentTimeValue.unknown.payload, {'timePrecision': 'unknown'});
    for (final sample in {
      'year': '2025',
      'month': '2025-09',
      'day': '2024-02-29',
      'instant': '2025-09-12 09:30:12.123456',
    }.entries) {
      final value = MomentTimeValue.parse(sample.key, sample.value);
      expect(value.precision, sample.key);
      expect(value.occurredAt!.isUtc, isTrue);
      expect(value.formText, sample.value);
    }
    for (final invalid in [
      '2025-02-29',
      '2025-13-01',
      '2025-04-31',
      '0000-01-01',
    ]) {
      expect(
        () => MomentTimeValue.parse('day', invalid),
        throwsFormatException,
      );
    }
    for (final invalid in [
      '2025-09-12 24:00',
      '2025-09-12 09:30Z',
      '2025-09-12 09:30+08:00',
    ]) {
      expect(
        () => MomentTimeValue.parse('instant', invalid),
        throwsFormatException,
      );
    }
    expect(
      () => MomentTimeValue.fromWire('unknown', DateTime.utc(2025)),
      throwsFormatException,
    );
    expect(() => MomentTimeValue.fromWire('year', null), throwsFormatException);
    expect(
      () => MomentTimeValue.fromWire('other', null),
      throwsFormatException,
    );
  });
  test('已有粗精度保留原 UTC 日历与序列化时间，准确时刻保留微秒', () {
    final year = MomentTimeValue.fromWire(
      'year',
      DateTime.parse('2025-01-01T00:00:00Z'),
    );
    expect(year.label, '2025年');
    final offset = MomentTimeValue.fromWire(
      'day',
      DateTime.parse('2025-01-01T01:00:00+08:00'),
    );
    expect(offset.label, '2024年12月31日');
    final instant = MomentTimeValue.parse(
      'instant',
      '2025-09-12 09:30:12.123456',
    );
    expect(instant.payload['occurredAt'], '2025-09-12T09:30:12.123456Z');
    expect(instant.label, contains('UTC'));
  });
  testWidgets('未知保持空白；选择年月并输入，错误与未知切换可恢复', (tester) async {
    MomentTimeValue? selected = MomentTimeValue.unknown;
    final form = GlobalKey<FormState>();
    final semantics = tester.ensureSemantics();
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: Form(
            key: form,
            child: MomentTimeChoice(
              initialValue: MomentTimeValue.unknown,
              onChanged: (value) => selected = value,
            ),
          ),
        ),
      ),
    );
    expect(find.byType(TextFormField), findsNothing);
    await tester.tap(find.text('不确定'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('记得年月').last);
    await tester.pumpAndSettle();
    expect(selected, isNull);
    expect(
      tester.widget<TextFormField>(find.byType(TextFormField)).controller!.text,
      '',
    );
    await tester.enterText(find.byType(TextFormField), '2025-13');
    expect(form.currentState!.validate(), isFalse);
    await tester.pump();
    expect(find.textContaining('请填写有效时间'), findsOneWidget);
    await tester.enterText(find.byType(TextFormField), '2025-09');
    expect(form.currentState!.validate(), isTrue);
    expect(selected!.label, '2025年9月');
    await tester.tap(find.text('记得年月'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('不确定').last);
    await tester.pumpAndSettle();
    expect(selected, same(MomentTimeValue.unknown));
    expect(find.byType(TextFormField), findsNothing);
    semantics.dispose();
  });
  testWidgets('小屏大字、键盘与辅助语义保留时间输入和下一项', (tester) async {
    tester.view.physicalSize = const Size(320, 640);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final semantics = tester.ensureSemantics();
    final next = FocusNode();
    addTearDown(next.dispose);
    await tester.pumpWidget(
      MaterialApp(
        home: MediaQuery(
          data: const MediaQueryData(
            size: Size(320, 640),
            textScaler: TextScaler.linear(2),
            viewInsets: EdgeInsets.only(bottom: 260),
          ),
          child: Scaffold(
            body: SingleChildScrollView(
              child: Form(
                child: Padding(
                  padding: const EdgeInsets.all(16),
                  child: Column(
                    children: [
                      MomentTimeChoice(
                        initialValue: MomentTimeValue.parse(
                          'instant',
                          '2025-09-12 09:30',
                        ),
                        onChanged: (_) {},
                      ),
                      TextFormField(
                        focusNode: next,
                        decoration: const InputDecoration(labelText: '下一项'),
                      ),
                    ],
                  ),
                ),
              ),
            ),
          ),
        ),
      ),
    );
    expect(tester.takeException(), isNull);
    expect(find.text('时刻（UTC）'), findsOneWidget);
    expect(find.textContaining('按 UTC 填写'), findsOneWidget);
    final input = find.byKey(const ValueKey('moment-time-instant'));
    await tester.ensureVisible(input);
    await tester.tap(input);
    await tester.sendKeyEvent(LogicalKeyboardKey.tab);
    await tester.pump();
    expect(next.hasFocus, isTrue);
    expect(tester.takeException(), isNull);
    expect(tester.getSemantics(input).toStringDeep(), contains('UTC'));
    semantics.dispose();
  });
}
