import 'moment_control_controller_test.dart';
import 'moment_control_test.dart';
import 'package:birdtie_client/src/content/moment_context_choice.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets('私人草稿情境选择显示真实名称和可移除的旧关联', (tester) async {
    String? selected;
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: MomentContextChoice(
            label: '社群',
            value: 'old-community',
            available: const {'active-community': '羽毛球社群'},
            onChanged: (value) => selected = value,
          ),
        ),
      ),
    );
    expect(find.text('此前关联的社群'), findsOneWidget);
    await tester.tap(find.text('此前关联的社群'));
    await tester.pumpAndSettle();
    expect(find.text('羽毛球社群'), findsOneWidget);
    await tester.tap(find.text('不关联').last);
    await tester.pumpAndSettle();
    expect(selected, '');
  });

  test('私人动态草稿保留稳定情境引用，切换登录后立即隐藏旧数据', () async {
    final f = MomentFixture();
    addTearDown(f.dispose);
    final created = await f.controller.previewCreate(
      cityID: 'aberdeen-gb',
      title: '周末记录',
      body: '只给自己看',
      placeID: mcTarget,
      activityID: mcOp,
      communityID: mcOwner,
      organizationID: mcTarget,
    );
    expect(created, isNotNull);
    expect(await f.controller.commit(created!), isTrue);
    expect(created.input.input!['placeId'], mcTarget);
    expect(created.input.input!['activityId'], mcOp);
    final draft = f.controller.moments.single;
    expect(draft.isEditableDraft, isTrue);
    final edited = await f.controller.previewUpdate(
      moment: draft,
      title: '修改标题',
      body: '',
      placeID: draft.placeID,
    );
    expect(edited, isNotNull);
    expect(edited!.input.input!.containsKey('activityId'), isFalse);
    expect(edited.input.input!.containsKey('communityId'), isFalse);
    expect(await f.controller.commit(edited), isTrue);
    f.identity.value = ('Bearer other', mcOp);
    expect(f.controller.moments, isEmpty);
  });
}
