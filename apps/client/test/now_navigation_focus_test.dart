import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'now_scope_recovery_test.dart';

void main() {
  testWidgets('Now带安全草稿打开侧栏再返回不自动弹键盘', (t) async {
    final f = NowFixture();
    addTearDown(f.dispose);
    await f.mount(t);
    await t.enterText(nowField(), '未发送的周末地点草稿');
    expect(t.widget<TextField>(nowField()).focusNode!.hasFocus, true);
    await nowTap(t, find.byTooltip('打开侧边栏'));
    await nowTap(t, find.text('个人资料'));
    await t.pageBack();
    await t.pumpAndSettle();
    expect(t.widget<TextField>(nowField()).controller!.text, '未发送的周末地点草稿');
    expect(t.widget<TextField>(nowField()).focusNode!.hasFocus, false);
    expect(t.testTextInput.isVisible, false);
    await f.unmount(t);
  });
  testWidgets('当前真实编辑仍允许主动唤起键盘', (t) async {
    final f = NowFixture();
    addTearDown(f.dispose);
    await f.mount(t);
    await nowTap(t, nowField());
    expect(t.widget<TextField>(nowField()).focusNode!.hasFocus, true);
    expect(t.testTextInput.isVisible, true);
    await f.unmount(t);
  });
  testWidgets('真实侧栏个人页面返回20轮保持安全草稿且仅主动编辑弹键盘', (t) async {
    final f = NowFixture();
    addTearDown(f.dispose);
    await f.mount(t);
    for (var round = 0; round < 20; round++) {
      await nowTap(t, nowField());
      expect(t.testTextInput.isVisible, true);
      await t.enterText(nowField(), '第$round轮未发草稿');
      await nowTap(t, find.byTooltip('打开侧边栏'));
      await nowTap(t, find.text('个人资料'));
      await nowBack(t);
      expect(t.widget<TextField>(nowField()).controller!.text, '第$round轮未发草稿');
      expect(t.widget<TextField>(nowField()).focusNode!.hasFocus, false);
      expect(t.testTextInput.isVisible, false);
      expect(f.source.queries, isEmpty);
    }
    expect(t.takeException(), isNull);
    await f.unmount(t);
  });
  testWidgets('真实Inbox与选城返回保持草稿且不恢复IME', (t) async {
    final f = NowFixture();
    addTearDown(f.dispose);
    await f.mount(t);
    for (final destination in ['打开收件箱', 'city']) {
      await nowTap(t, nowField());
      await t.enterText(nowField(), '安全的未发送草稿');
      await nowTap(
        t,
        destination == 'city'
            ? find.text('当前：甲验收城市')
            : find.byTooltip(destination),
      );
      await nowBack(t);
      expect(t.widget<TextField>(nowField()).controller!.text, '安全的未发送草稿');
      expect(t.widget<TextField>(nowField()).focusNode!.hasFocus, false);
      expect(t.testTextInput.isVisible, false);
    }
    await f.unmount(t);
  });
}
