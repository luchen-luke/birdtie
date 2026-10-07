import 'dart:async';
import 'package:birdtie_client/src/workspace/verified_booking_controller.dart';
import 'package:birdtie_client/src/workspace/verified_booking_section.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'verified_booking_controller_test.dart'
    show bookingNow, bookingData, bookingReply;

void main() {
  testWidgets('320大字键盘下外跳检查可滚动，具体打开与取消均48dp', (tester) async {
    tester.view.physicalSize = const Size(320, 640);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    var opens = 0, reports = 0;
    final c = VerifiedBookingController(
      placeID: 'place',
      authorizationHeader: () => null,
      now: () => bookingNow,
      apiBaseUrl: 'https://api.test',
      client: MockClient((r) async {
        if (r.method == 'POST') {
          reports++;
          return http.Response('{}', 503);
        }
        return bookingReply(bookingData());
      }),
      openExternal: (_) async {
        opens++;
        return true;
      },
    );
    c.adopt(bookingData());
    await tester.pumpWidget(
      MaterialApp(
        builder: (context, child) => MediaQuery(
          data: MediaQuery.of(context).copyWith(
            textScaler: const TextScaler.linear(3),
            viewInsets: const EdgeInsets.only(bottom: 260),
          ),
          child: child!,
        ),
        home: Scaffold(
          body: SingleChildScrollView(
            child: VerifiedBookingSection(controller: c),
          ),
        ),
      ),
    );
    final entry = find.text('查看外部预约说明');
    await tester.ensureVisible(entry);
    await tester.pumpAndSettle();
    await tester.tap(entry);
    await tester.pumpAndSettle();
    expect(tester.takeException(), isNull);
    final confirm = find.widgetWithText(FilledButton, '确认打开');
    final cancel = find.widgetWithText(TextButton, '取消');
    expect(tester.getSize(confirm).height, greaterThanOrEqualTo(48));
    expect(tester.getSize(cancel).height, greaterThanOrEqualTo(48));
    await tester.ensureVisible(cancel);
    await tester.pumpAndSettle();
    await tester.tap(cancel);
    await tester.pumpAndSettle();
    expect(opens, 0);
    expect(reports, 0);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
    c.dispose();
  });
  testWidgets('核验30秒期限自然到期，旧确认不能打开或记录', (tester) async {
    var now = bookingNow, opens = 0, reports = 0;
    final c = VerifiedBookingController(
      placeID: 'place',
      authorizationHeader: () => null,
      now: () => now,
      apiBaseUrl: 'https://api.test',
      client: MockClient((r) async {
        if (r.method == 'POST') reports++;
        return bookingReply(bookingData());
      }),
      openExternal: (_) async {
        opens++;
        return true;
      },
    );
    c.adopt(bookingData());
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(body: VerifiedBookingSection(controller: c)),
      ),
    );
    await tester.tap(find.text('查看外部预约说明'));
    await tester.pumpAndSettle();
    now = now.add(const Duration(seconds: 31));
    await tester.pump(const Duration(seconds: 1));
    expect(
      tester
          .widget<FilledButton>(find.widgetWithText(FilledButton, '确认打开'))
          .onPressed,
      isNull,
    );
    expect(find.textContaining('预约地址：'), findsNothing);
    await tester.tap(find.text('取消'));
    await tester.pumpAndSettle();
    expect(opens, 0);
    expect(reports, 0);
    await tester.pumpWidget(const SizedBox());
    c.dispose();
  });
  testWidgets('嵌套详情撤除时外部预约确认一并撤除且没有打开副作用', (t) async {
    var opens = 0;
    final visible = ValueNotifier(true);
    final c = VerifiedBookingController(
      placeID: 'place',
      authorizationHeader: () => null,
      now: () => bookingNow,
      apiBaseUrl: 'https://api.test',
      client: MockClient((_) async => bookingReply(bookingData())),
      openExternal: (_) async {
        opens++;
        return true;
      },
    );
    c.adopt(bookingData());
    await t.pumpWidget(
      MaterialApp(
        home: ValueListenableBuilder<bool>(
          valueListenable: visible,
          builder: (context, show, _) => show
              ? Navigator(
                  onGenerateRoute: (_) => MaterialPageRoute<void>(
                    builder: (_) =>
                        Scaffold(body: VerifiedBookingSection(controller: c)),
                  ),
                )
              : const Scaffold(body: Text('身份已失效')),
        ),
      ),
    );
    await t.pumpAndSettle();
    await t.tap(find.text('查看外部预约说明'));
    await t.pumpAndSettle();
    expect(find.text('确认打开'), findsOneWidget);
    visible.value = false;
    await t.pumpAndSettle();
    expect(find.text('确认打开'), findsNothing);
    expect(opens, 0);
    await t.pumpWidget(const SizedBox());
    c.dispose();
    visible.dispose();
  });
  testWidgets(
    'unavailable source can be reread without reopening old approval',
    (tester) async {
      var unavailable = true, opens = 0;
      final c = VerifiedBookingController(
        placeID: 'place',
        authorizationHeader: () => null,
        now: () => bookingNow,
        apiBaseUrl: 'https://api.test',
        client: MockClient(
          (_) async => unavailable
              ? http.Response('{}', 404)
              : bookingReply(bookingData(url: 'https://example.org/new')),
        ),
        openExternal: (_) async {
          opens++;
          return true;
        },
      );
      c.adopt(bookingData());
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(body: VerifiedBookingSection(controller: c)),
        ),
      );
      await tester.tap(find.text('查看外部预约说明'));
      await tester.pumpAndSettle();
      expect(find.textContaining('不可查看'), findsOneWidget);
      expect(opens, 0);
      unavailable = false;
      await tester.tap(find.text('刷新预约资料'));
      await tester.pumpAndSettle();
      expect(
        find.textContaining('预约地址：https://example.org/new'),
        findsOneWidget,
      );
      expect(opens, 0);
      await tester.tap(find.text('确认打开'));
      await tester.pumpAndSettle();
      expect(opens, 1);
      await tester.pumpWidget(const SizedBox());
      c.dispose();
    },
  );
  for (final approve in [false, true]) {
    testWidgets(
      'external page ${approve ? 'confirm' : 'cancel'} with Chinese provenance and no booking claim',
      (tester) async {
        var opens = 0, reads = 0, reports = 0;
        final c = VerifiedBookingController(
          placeID: 'place',
          authorizationHeader: () => null,
          now: () => bookingNow,
          apiBaseUrl: 'https://api.test',
          client: MockClient((request) async {
            if (request.method == 'POST') {
              reports++;
              return http.Response('{}', 503);
            }
            reads++;
            return bookingReply(bookingData());
          }),
          openExternal: (_) async {
            opens++;
            return true;
          },
        );
        c.adopt(bookingData());
        await tester.pumpWidget(
          MaterialApp(
            home: Scaffold(body: VerifiedBookingSection(controller: c)),
          ),
        );
        expect(find.textContaining('未提供实时可预约信息'), findsOneWidget);
        await tester.tap(find.text('查看外部预约说明'));
        await tester.pumpAndSettle();
        expect(find.textContaining('打开页面不代表已预约'), findsOneWidget);
        expect(
          find.textContaining('资料来源：https://example.org/source'),
          findsWidgets,
        );
        expect(opens, 0);
        await tester.tap(find.text(approve ? '确认打开' : '取消'));
        await tester.pumpAndSettle();
        expect(opens, approve ? 1 : 0);
        expect(reads, approve ? 2 : 1);
        expect(reports, approve ? 1 : 0);
        if (approve) expect(find.textContaining('外跳统计未确认'), findsOneWidget);
        expect(tester.takeException(), isNull);
        await tester.pumpWidget(const SizedBox());
        c.dispose();
      },
    );
  }
  testWidgets(
    'workspace change hides preview facts and disables old approval',
    (tester) async {
      String? workspace;
      var opens = 0;
      final c = VerifiedBookingController(
        placeID: 'place',
        authorizationHeader: () => null,
        workspaceID: () => workspace,
        now: () => bookingNow,
        apiBaseUrl: 'https://api.test',
        client: MockClient((_) async => bookingReply(bookingData())),
        openExternal: (_) async {
          opens++;
          return true;
        },
      );
      c.adopt(bookingData());
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(body: VerifiedBookingSection(controller: c)),
        ),
      );
      await tester.tap(find.text('查看外部预约说明'));
      await tester.pumpAndSettle();
      workspace = 'org';
      c.syncIdentity();
      await tester.pump();
      expect(find.textContaining('工作区已变化'), findsOneWidget);
      expect(find.textContaining('预约地址：'), findsNothing);
      expect(
        tester.widget<FilledButton>(find.byType(FilledButton)).onPressed,
        isNull,
      );
      expect(opens, 0);
      await tester.tap(find.text('取消'));
      await tester.pumpAndSettle();
      await tester.pumpWidget(const SizedBox());
      c.dispose();
    },
  );
  testWidgets('natural expiry closes approval facts and marks source expired', (
    tester,
  ) async {
    var now = bookingNow;
    final c = VerifiedBookingController(
      placeID: 'place',
      authorizationHeader: () => null,
      now: () => now,
      apiBaseUrl: 'https://api.test',
      client: MockClient((_) async => bookingReply(bookingData())),
      openExternal: (_) async => true,
    );
    c.adopt(bookingData());
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(body: VerifiedBookingSection(controller: c)),
      ),
    );
    await tester.tap(find.text('查看外部预约说明'));
    await tester.pumpAndSettle();
    now = DateTime.utc(2026, 10, 4, 12);
    await tester.pump(const Duration(seconds: 1));
    expect(find.textContaining('预约地址：'), findsNothing);
    expect(find.textContaining('预约资料已过期'), findsOneWidget);
    await tester.tap(find.text('取消'));
    await tester.pumpAndSettle();
    await tester.pumpWidget(const SizedBox());
    c.dispose();
  });
  testWidgets('delayed prepare cannot open dialog after widget disposal', (
    tester,
  ) async {
    final pending = Completer<http.Response>();
    final c = VerifiedBookingController(
      placeID: 'place',
      authorizationHeader: () => null,
      now: () => bookingNow,
      apiBaseUrl: 'https://api.test',
      client: MockClient((_) => pending.future),
      openExternal: (_) async => true,
    );
    c.adopt(bookingData());
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(body: VerifiedBookingSection(controller: c)),
      ),
    );
    await tester.tap(find.text('查看外部预约说明'));
    await tester.pump();
    await tester.pumpWidget(const SizedBox());
    pending.complete(bookingReply(bookingData()));
    await tester.pump();
    expect(tester.takeException(), isNull);
    c.dispose();
  });
  for (final support in ['contact', 'unknown', 'none']) {
    testWidgets('honest $support without fake slots or contact', (
      tester,
    ) async {
      final c = VerifiedBookingController(
        placeID: 'place',
        authorizationHeader: () => null,
        now: () => bookingNow,
        openExternal: (_) async => true,
      );
      c.adopt(bookingData(support: support));
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(body: VerifiedBookingSection(controller: c)),
        ),
      );
      expect(find.text('查看外部预约说明'), findsNothing);
      expect(find.textContaining('未提供实时可预约信息'), findsOneWidget);
      if (support == 'contact') {
        expect(find.textContaining('未提供具体联系方式'), findsOneWidget);
      }
      await tester.pumpWidget(const SizedBox());
      c.dispose();
    });
  }
  testWidgets(
    'narrow mobile and 200 percent text keeps confirmation scrollable',
    (tester) async {
      tester.view.physicalSize = const Size(360, 740);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      final c = VerifiedBookingController(
        placeID: 'place',
        authorizationHeader: () => null,
        now: () => bookingNow,
        apiBaseUrl: 'https://api.test',
        client: MockClient((_) async => bookingReply(bookingData())),
        openExternal: (_) async => false,
      );
      c.adopt(bookingData());
      await tester.pumpWidget(
        MaterialApp(
          builder: (context, child) => MediaQuery(
            data: MediaQuery.of(
              context,
            ).copyWith(textScaler: const TextScaler.linear(2)),
            child: child!,
          ),
          home: Scaffold(
            body: SingleChildScrollView(
              child: VerifiedBookingSection(controller: c),
            ),
          ),
        ),
      );
      await tester.ensureVisible(find.text('查看外部预约说明'));
      await tester.tap(find.text('查看外部预约说明'));
      await tester.pumpAndSettle();
      expect(tester.takeException(), isNull);
      await tester.tap(find.text('确认打开'));
      await tester.pumpAndSettle();
      expect(find.textContaining('无法打开外部预约页面'), findsOneWidget);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
      c.dispose();
    },
  );
}
