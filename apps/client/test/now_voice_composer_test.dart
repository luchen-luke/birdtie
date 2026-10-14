import 'dart:async';

import 'package:birdtie_client/src/app/birdtie_surfaces.dart';
import 'package:birdtie_client/src/workspace/agent_composer.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/voice_input.dart';
import 'package:birdtie_client/src/workspace/voice_input_consent.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';

const _service = VoiceSystemService(
  id: 'com.example/.Speech',
  name: '测试系统语音服务',
);
const _activity = VoiceSystemActivity(
  id: 'com.example/.SpeechActivity',
  name: '测试系统语音界面',
);

class _Job {
  _Job(this.id, this.language, this.local, this.approved, this.phase);
  final String id, language;
  final bool local, approved;
  final ValueChanged<VoiceInputPhase> phase;
  final result = Completer<String>();
}

class _Voice extends VoiceInputPort {
  bool local = true, localReady = true, granted = true;
  bool bundled = false;
  VoiceSystemActivity? activity;
  Completer<bool>? permission;
  final jobs = <_Job>[], cancelled = <String>[], stopped = <String>[];
  int permissionCalls = 0;
  @override
  Future<VoiceAvailability> availability() async => VoiceAvailability(
    onDevice: local,
    bundledOffline: bundled,
    systemService: _service,
    systemActivity: activity,
  );
  @override
  Future<bool> requestPermission(String requestId) {
    permissionCalls++;
    return permission?.future ?? Future.value(granted);
  }

  @override
  Future<bool> localLanguageReady(String id, String language) async =>
      localReady;
  @override
  Future<String> recognize({
    required String requestId,
    required String language,
    required bool onDevice,
    required VoiceSystemService? systemService,
    required bool systemApproved,
    required ValueChanged<VoiceInputPhase> onPhase,
  }) {
    final job = _Job(requestId, language, onDevice, systemApproved, onPhase);
    jobs.add(job);
    onPhase(VoiceInputPhase.listening);
    return job.result.future;
  }

  @override
  Future<void> stop(String id) async {
    stopped.add(id);
  }

  @override
  Future<String> recognizeActivity({
    required String requestId,
    required String language,
    required VoiceSystemActivity activity,
    required bool systemApproved,
    required bool Function() onLaunch,
  }) {
    expect(systemApproved, isTrue);
    expect(onLaunch(), isTrue);
    final job = _Job(requestId, language, false, systemApproved, (_) {});
    jobs.add(job);
    return job.result.future;
  }

  @override
  Future<void> cancel(String id) async {
    cancelled.add(id);
  }

  @override
  void dispose() {
    for (final j in jobs) {
      if (!j.result.isCompleted) j.result.complete('销毁后迟到');
    }
  }
}

TextField _field(WidgetTester t) => t.widget<TextField>(find.byType(TextField));
Future<AgentWorkspaceController> _mount(
  WidgetTester t,
  VoiceInputPort voice,
  List<String> sent, {
  ValueNotifier<int>? source,
  double scale = 1,
  double inset = 0,
  Brightness brightness = Brightness.light,
  GlobalKey<NavigatorState>? navigator,
  VoiceInputConsentStore? consent,
}) async {
  final workspace = AgentWorkspaceController()
    ..task = const AgentTask(id: 'original', query: '原任务', status: 'COMPLETED');
  addTearDown(workspace.dispose);
  await t.pumpWidget(
    MaterialApp(
      navigatorKey: navigator,
      theme: birdtieTheme(brightness),
      home: MediaQuery(
        data: MediaQueryData(
          textScaler: TextScaler.linear(scale),
          viewInsets: EdgeInsets.only(bottom: inset),
        ),
        child: Scaffold(
          body: Align(
            alignment: Alignment.bottomCenter,
            child: AgentComposer(
              workspace: workspace,
              onSubmit: sent.add,
              onSearchArea: () {},
              hasSearchArea: false,
              voiceInputPort: voice,
              voiceConsentStore: consent ?? MemoryVoiceInputConsentStore(),
              materialContext: () => source?.value,
              materialContextChanges: source,
            ),
          ),
        ),
      ),
    ),
  );
  await t.pump();
  return workspace;
}

Future<void> _start(WidgetTester t) async {
  await t.tap(find.byTooltip('语音输入'));
  await t.pumpAndSettle();
  final notice = find.byKey(const Key('now-voice-consent-confirm'));
  if (notice.evaluate().isNotEmpty) {
    await t.tap(notice);
    await t.pumpAndSettle();
  }
}

Future<void> _unmount(WidgetTester t) async {
  await t.pumpWidget(const SizedBox.shrink());
  await t.pump();
}

void main() {
  testWidgets('本机Whisper首次说明可取消、独立认可且后续不重复，不自动联网或发送', (t) async {
    final voice = _Voice()..bundled = true;
    final sent = <String>[], consent = MemoryVoiceInputConsentStore();
    // Prior system-local disclosure does not acknowledge bundled processing.
    await consent.acknowledge(null);
    await _mount(t, voice, sent, consent: consent);
    await t.enterText(find.byType(TextField), '原草稿');
    await t.tap(find.byTooltip('语音输入'));
    await t.pumpAndSettle();
    expect(find.textContaining('声音只在本机处理，不上传'), findsOneWidget);
    expect(find.textContaining('最多录音30秒'), findsOneWidget);
    expect(find.byType(RadioListTile<String>), findsNothing);
    expect(voice.permissionCalls, 0);
    await t.tap(find.text('取消'));
    await t.pumpAndSettle();
    expect(voice.jobs, isEmpty);
    expect(_field(t).controller!.text, '原草稿');
    expect(await consent.acknowledged(const VoiceBundledEngine()), isFalse);
    await _start(t);
    expect(voice.jobs.single.local, isTrue);
    expect(voice.jobs.single.approved, isFalse);
    expect(voice.jobs.single.language, 'auto');
    voice.jobs.single.result.complete('附近');
    await t.pumpAndSettle();
    expect(sent, isEmpty);
    await t.tap(find.byTooltip('语音输入'));
    await t.pumpAndSettle();
    expect(find.byKey(const Key('now-voice-consent-confirm')), findsNothing);
    expect(voice.jobs, hasLength(2));
    voice.jobs.last.result.complete('咖啡馆');
    await t.pumpAndSettle();
    expect(sent, isEmpty);
    expect(find.byType(TextField), findsOneWidget);
    await _unmount(t);
  });
  testWidgets('本机模型未准备好保留同一草稿，只提示键盘且不打开系统联网认可', (t) async {
    final voice = _Voice()
      ..bundled = true
      ..localReady = false
      ..activity = _activity;
    final sent = <String>[];
    await _mount(t, voice, sent);
    await t.enterText(find.byType(TextField), '原草稿');
    final editor = _field(t).controller;
    await _start(t);
    expect(find.textContaining('本机语音模型未准备好'), findsOneWidget);
    expect(find.byKey(const Key('now-voice-consent-confirm')), findsNothing);
    expect(voice.jobs, isEmpty);
    expect(_field(t).controller, same(editor));
    expect(editor!.text, '原草稿');
    expect(sent, isEmpty);
    await _unmount(t);
  });
  testWidgets('系统语音界面一次说明、预期离开和恢复只填原单草稿', (t) async {
    final voice = _Voice()
      ..local = false
      ..activity = _activity;
    final sent = <String>[], consent = MemoryVoiceInputConsentStore();
    final ws = await _mount(t, voice, sent, consent: consent);
    final task = ws.task, editor = _field(t).controller;
    addTearDown(
      () => t.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed),
    );
    await t.enterText(find.byType(TextField), '原草稿 ');
    await t.tap(find.byTooltip('语音输入'));
    await t.pumpAndSettle();
    expect(find.textContaining('将打开设备的语音界面'), findsOneWidget);
    expect(find.byType(RadioListTile<String>), findsNothing);
    await t.tap(find.byKey(const Key('now-voice-consent-confirm')));
    await t.pumpAndSettle();
    expect(voice.jobs, hasLength(1));
    expect(voice.permissionCalls, 0);
    expect(voice.jobs.single.language, 'auto');
    expect(_field(t).decoration!.hintText, '等待系统语音界面…');
    t.binding.handleAppLifecycleStateChanged(AppLifecycleState.inactive);
    t.binding.handleAppLifecycleStateChanged(AppLifecycleState.hidden);
    t.binding.handleAppLifecycleStateChanged(AppLifecycleState.paused);
    await t.pump();
    expect(voice.cancelled, isEmpty);
    voice.jobs.single.result.complete('附近 cafe');
    await t.pump();
    expect(editor!.text, '原草稿 ');
    t.binding.handleAppLifecycleStateChanged(AppLifecycleState.hidden);
    t.binding.handleAppLifecycleStateChanged(AppLifecycleState.inactive);
    t.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
    await t.pumpAndSettle();
    expect(_field(t).controller, same(editor));
    expect(editor.text, '原草稿 附近 cafe');
    expect(ws.task, same(task));
    expect(sent, isEmpty);
    await _start(t);
    expect(find.byKey(const Key('now-voice-consent-confirm')), findsNothing);
    expect(voice.jobs, hasLength(2));
    expect(voice.permissionCalls, 0);
    await t.tap(find.byKey(const Key('now-voice-cancel')));
    voice.jobs.last.result.complete('取消后不回填');
    await t.pumpAndSettle();
    expect(editor.text, '原草稿 附近 cafe');
    expect(sent, isEmpty);
    await _unmount(t);
  });
  for (final kind in ['账号来源ABA', '任务', '草稿编辑', '页面覆盖']) {
    testWidgets('系统语音界面$kind立即退休且迟到结果零填稿零发送', (t) async {
      final voice = _Voice()
        ..local = false
        ..activity = _activity;
      final sent = <String>[],
          source = ValueNotifier<int>(1),
          navigator = GlobalKey<NavigatorState>();
      addTearDown(source.dispose);
      final ws = await _mount(
        t,
        voice,
        sent,
        source: source,
        navigator: navigator,
      );
      await t.enterText(find.byType(TextField), '保留原稿');
      await _start(t);
      expect(voice.jobs, hasLength(1));
      if (kind == '账号来源ABA') {
        source.value = 2;
        source.value = 1;
      }
      if (kind == '任务') {
        ws.newTask();
      }
      if (kind == '草稿编辑') {
        await t.enterText(find.byType(TextField), '本人改写');
      }
      if (kind == '页面覆盖') {
        navigator.currentState!.push(
          MaterialPageRoute<void>(
            builder: (_) => const Scaffold(body: Text('合成新页面')),
          ),
        );
      }
      await t.pumpAndSettle();
      expect(voice.cancelled, contains(voice.jobs.single.id));
      voice.jobs.single.result.complete('退休后私密合成文本');
      await t.pumpAndSettle();
      if (kind == '页面覆盖') {
        navigator.currentState!.pop();
        await t.pumpAndSettle();
      }
      expect(_field(t).controller!.text, isNot(contains('退休后')));
      if (kind == '草稿编辑') expect(_field(t).controller!.text, '本人改写');
      expect(sent, isEmpty);
      expect(voice.permissionCalls, 0);
      await _unmount(t);
    });
  }
  for (final brightness in Brightness.values) {
    for (final scale in [1.0, 1.3, 2.0]) {
      testWidgets('窄屏 $brightness 字号$scale 转写立即显示明确发送，键盘下手动提交', (t) async {
        t.view.physicalSize = const Size(320, 760);
        t.view.devicePixelRatio = 1;
        addTearDown(t.view.resetPhysicalSize);
        addTearDown(t.view.resetDevicePixelRatio);
        final voice = _Voice(), sent = <String>[];
        final ws = await _mount(
          t,
          voice,
          sent,
          scale: scale,
          inset: 280,
          brightness: brightness,
        );
        final task = ws.task;
        final controller = _field(t).controller;
        final focus = _field(t).focusNode;
        await _start(t);
        await t.tap(find.byTooltip('停止录音并转写'));
        await t.pump();
        expect(find.byTooltip('发送需求'), findsOneWidget);
        final primary = find.byKey(const Key('now-composer-primary-action'));
        expect(find.byTooltip('正在处理语音'), findsOneWidget);
        expect(
          t
              .widget<IconButton>(
                find.byKey(const Key('now-composer-microphone')),
              )
              .onPressed,
          isNull,
        );
        expect(t.widget<IconButton>(primary).onPressed, isNull);
        expect(sent, isEmpty);
        const transcript = '阿伯丁 Aberdeen 有没有好的餐厅？';
        voice.jobs.single.result.complete(transcript);
        await t.pumpAndSettle();
        expect(_field(t).controller!.text, transcript);
        expect(_field(t).controller, same(controller));
        expect(_field(t).focusNode, same(focus));
        expect(ws.task, same(task));
        expect(find.byType(TextField), findsOneWidget);
        expect(sent, isEmpty);
        await t.showKeyboard(find.byType(TextField));
        await t.pump();
        expect(find.byTooltip('发送需求').hitTestable(), findsOneWidget);
        final send = primary.hitTestable();
        expect(send, findsOneWidget);
        final button = t.widget<IconButton>(send);
        expect(button.onPressed, isNotNull);
        final style = button.style!;
        final bg = style.backgroundColor!.resolve({})!;
        final fg = style.foregroundColor!.resolve({})!;
        final luminance = [bg.computeLuminance(), fg.computeLuminance()]
          ..sort();
        expect(
          (luminance.last + .05) / (luminance.first + .05),
          greaterThanOrEqualTo(4.5),
        );
        expect(style.shape!.resolve({}), isA<CircleBorder>());
        final size = t.getSize(send);
        expect(size.width, greaterThanOrEqualTo(48));
        expect(size.height, greaterThanOrEqualTo(48));
        expect(t.getRect(send).bottom, lessThanOrEqualTo(480));
        await t.tap(send);
        await t.pump();
        expect(sent, [transcript]);
        expect(_field(t).controller!.text, isEmpty);
        expect(find.byTooltip('语音输入').hitTestable(), findsOneWidget);
        final idle = t.widget<IconButton>(primary);
        expect(idle.style!.backgroundColor?.resolve({}), isNull);
        expect(t.takeException(), isNull);
        await _unmount(t);
      });
    }
  }
  for (final stage in [
    'availability',
    'permission',
    'localSupport',
    'recognize',
  ]) {
    testWidgets('原生 $stage 永不回复后卸载立即清理watchdog，迟到不回填', (t) async {
      final channel = MethodChannel('voice-native-silent-$stage');
      final pending = Completer<Object?>();
      String? request;
      t.binding.defaultBinaryMessenger.setMockMethodCallHandler(channel, (
        call,
      ) async {
        if (call.method == stage) {
          request = (call.arguments as Map?)?['requestId'];
          return pending.future;
        }
        if (call.method == 'availability') {
          return {'onDevice': true, 'systemService': null};
        }
        final id = (call.arguments as Map)['requestId'];
        if (call.method == 'permission') {
          return {'requestId': id, 'granted': true};
        }
        if (call.method == 'localSupport') {
          return {'requestId': id, 'ready': true};
        }
        return null;
      });
      final port = NativeVoiceInputPort(
            channel: channel,
            supported: () => true,
          ),
          sent = <String>[];
      await _mount(t, port, sent);
      if (stage != 'availability') await _start(t);
      await _unmount(t);
      // Real binary-messenger reply arrives after State and channel owner retired.
      pending.complete(switch (stage) {
        'availability' => {'onDevice': true, 'systemService': null},
        'permission' => {'requestId': request, 'granted': true},
        'localSupport' => {'requestId': request, 'ready': true},
        _ => {'requestId': request, 'text': '不该回填的旧文字'},
      });
      await t.pump();
      expect(sent, isEmpty);
      expect(t.takeException(), isNull);
    });
  }
  testWidgets('真实语音端口只回填原单草稿，编辑后才本人发送', (t) async {
    final voice = _Voice(), sent = <String>[];
    final ws = await _mount(t, voice, sent);
    final task = ws.task,
        controller = _field(t).controller,
        focus = _field(t).focusNode;
    await _start(t);
    expect(voice.jobs, hasLength(1));
    expect(find.byType(TextField), findsOneWidget);
    expect(
      t.getSize(find.byTooltip('停止录音并转写')).height,
      greaterThanOrEqualTo(48),
    );
    expect(
      t.getSize(find.byTooltip('取消语音输入')).height,
      greaterThanOrEqualTo(48),
    );
    await t.tap(find.byTooltip('停止录音并转写'));
    await t.pump();
    expect(voice.stopped, [voice.jobs.single.id]);
    expect(sent, isEmpty);
    voice.jobs.single.result.complete('Aberdeen 今天想找 a restaurant');
    await t.pumpAndSettle();
    expect(_field(t).controller!.text, 'Aberdeen 今天想找 a restaurant');
    expect(sent, isEmpty);
    expect(_field(t).controller, same(controller));
    expect(_field(t).focusNode, same(focus));
    expect(ws.task, same(task));
    await t.enterText(find.byType(TextField), '编辑后的中文原话');
    await t.tap(find.byTooltip('发送需求'));
    await t.pump();
    expect(sent, ['编辑后的中文原话']);
    expect(t.takeException(), isNull);
    await _unmount(t);
  });
  testWidgets('系统处理首次简短说明可取消，后续直接采音且每次仍申请权限', (t) async {
    final voice = _Voice()..local = false, sent = <String>[];
    final consent = MemoryVoiceInputConsentStore();
    await _mount(t, voice, sent, consent: consent);
    await t.tap(find.byTooltip('语音输入'));
    await t.pumpAndSettle();
    expect(find.text('语音输入'), findsOneWidget);
    expect(find.textContaining('可能联网处理声音'), findsOneWidget);
    expect(find.textContaining('详情可在设置中查看'), findsOneWidget);
    expect(voice.permissionCalls, 0);
    expect(voice.jobs, isEmpty);
    await t.tap(find.text('取消'));
    await t.pumpAndSettle();
    expect(voice.permissionCalls, 0);
    expect(await consent.acknowledged(_service), isFalse);
    for (var n = 0; n < 2; n++) {
      await t.tap(find.byTooltip('语音输入'));
      await t.pumpAndSettle();
      final confirm = find.byKey(const Key('now-voice-consent-confirm'));
      if (n == 0) {
        expect(confirm, findsOneWidget);
        expect(voice.permissionCalls, 0);
        await t.tap(confirm);
        await t.pumpAndSettle();
      } else {
        expect(confirm, findsNothing);
      }
      expect(await consent.acknowledged(_service), isTrue);
      final job = voice.jobs.last;
      expect(job.local, isFalse);
      expect(job.approved, isTrue);
      await t.tap(find.byTooltip('取消语音输入'));
      await t.pump();
      job.result.complete('取消旧文字');
      await t.pump();
      expect(_field(t).controller!.text, isEmpty);
      expect(sent, isEmpty);
    }
    expect(voice.permissionCalls, 2);
    expect(t.takeException(), isNull);
    await _unmount(t);
  });
  testWidgets('自动识别英语并保留原话，不翻译或自动发送', (t) async {
    final voice = _Voice()..local = false, sent = <String>[];
    await _mount(t, voice, sent);
    await t.tap(find.byTooltip('语音输入'));
    await t.pumpAndSettle();
    expect(find.byType(DropdownButtonFormField<String>), findsNothing);
    expect(find.byType(RadioListTile<String>), findsNothing);
    await t.tap(find.byKey(const Key('now-voice-consent-confirm')));
    await t.pumpAndSettle();
    expect(voice.jobs.single.language, 'auto');
    voice.jobs.single.result.complete('Where is Aberdeen Art Gallery?');
    await t.pumpAndSettle();
    expect(_field(t).controller!.text, 'Where is Aberdeen Art Gallery?');
    expect(sent, isEmpty);
    await _unmount(t);
  });
  testWidgets('未就绪本地模型不能静默改用系统服务', (t) async {
    final voice = _Voice()..localReady = false, sent = <String>[];
    await _mount(t, voice, sent);
    await _start(t);
    expect(find.textContaining('可能联网处理声音'), findsOneWidget);
    expect(voice.jobs, isEmpty);
    await t.tap(find.text('取消'));
    await t.pumpAndSettle();
    expect(voice.jobs, isEmpty);
    expect(sent, isEmpty);
    await _unmount(t);
  });
  testWidgets('本地自动模型未就绪时独立说明系统处理，保留识别原话', (t) async {
    final voice = _Voice()..localReady = false, sent = <String>[];
    await _mount(t, voice, sent);
    await t.tap(find.byTooltip('语音输入'));
    await t.pumpAndSettle();
    await t.tap(find.byKey(const Key('now-voice-consent-confirm')));
    await t.pumpAndSettle();
    expect(find.textContaining('可能联网处理声音'), findsOneWidget);
    expect(find.byType(DropdownButtonFormField<String>), findsNothing);
    expect(find.byType(RadioListTile<String>), findsNothing);
    expect(voice.jobs, isEmpty);
    await t.tap(find.byKey(const Key('now-voice-consent-confirm')));
    await t.pumpAndSettle();
    expect(voice.jobs.single.language, 'auto');
    expect(voice.jobs.single.local, isFalse);
    expect(voice.jobs.single.approved, isTrue);
    voice.jobs.single.result.complete('Please find Aberdeen Art Gallery.');
    await t.pumpAndSettle();
    expect(_field(t).controller!.text, 'Please find Aberdeen Art Gallery.');
    expect(sent, isEmpty);
    await _unmount(t);
  });
  for (final fallback in [false, true]) {
    testWidgets('同意窗${fallback ? '本地回退' : '首次系统'}被新页面覆盖再返回，旧确认零采音', (t) async {
      final voice = _Voice()
            ..local = fallback
            ..localReady = false,
          sent = <String>[],
          navigator = GlobalKey<NavigatorState>();
      await _mount(t, voice, sent, navigator: navigator);
      await t.tap(find.byTooltip('语音输入'));
      await t.pumpAndSettle();
      if (fallback) {
        await t.tap(find.byKey(const Key('now-voice-consent-confirm')));
        await t.pumpAndSettle();
      }
      expect(find.textContaining('可能联网处理声音'), findsOneWidget);
      final permissions = voice.permissionCalls;
      unawaited(
        navigator.currentState!.push(
          MaterialPageRoute<void>(
            builder: (_) => const Scaffold(body: Text('另一个完整页面')),
          ),
        ),
      );
      await t.pumpAndSettle();
      navigator.currentState!.pop();
      await t.pumpAndSettle();
      await t.tap(find.byKey(const Key('now-voice-consent-confirm')));
      await t.pumpAndSettle();
      expect(voice.jobs, isEmpty);
      expect(voice.permissionCalls, permissions);
      expect(sent, isEmpty);
      expect(
        t
            .widget<TextButton>(
              find.byKey(const Key('now-voice-consent-confirm')),
            )
            .onPressed,
        isNull,
      );
      await t.tap(find.text('取消'));
      await t.pumpAndSettle();
      await _start(t);
      expect(voice.jobs, hasLength(1));
      voice.jobs.single.result.complete('本人重新发起');
      await t.pumpAndSettle();
      expect(_field(t).controller!.text, '本人重新发起');
      expect(sent, isEmpty);
      await _unmount(t);
    });
  }
  testWidgets('拒权后原键盘发送正常且可再次本人重试', (t) async {
    final voice = _Voice()..granted = false, sent = <String>[];
    await _mount(t, voice, sent);
    await _start(t);
    expect(voice.jobs, isEmpty);
    expect(find.textContaining('未获麦克风权限'), findsOneWidget);
    await t.enterText(find.byType(TextField), '仍可键盘输入');
    await t.pump();
    await t.tap(find.byTooltip('发送需求'));
    await t.pump();
    expect(sent, ['仍可键盘输入']);
    voice.granted = true;
    await _start(t);
    expect(voice.jobs, hasLength(1));
    voice.jobs.single.result.complete('重新识别');
    await t.pumpAndSettle();
    expect(_field(t).controller!.text, '重新识别');
    await _unmount(t);
  });
  for (final kind in ['编辑后清空', 'composing', '任务', '主体来源ABA']) {
    testWidgets('$kind 退休旧语音，迟到回包不填稿不发送', (t) async {
      final voice = _Voice(), sent = <String>[], source = ValueNotifier(1);
      addTearDown(source.dispose);
      final ws = await _mount(t, voice, sent, source: source);
      await _start(t);
      final job = voice.jobs.single;
      if (kind == '编辑后清空') {
        await t.enterText(find.byType(TextField), '本人新草稿');
        await t.enterText(find.byType(TextField), '');
      }
      if (kind == 'composing') {
        _field(t).controller!.value = const TextEditingValue(
          text: '',
          composing: TextRange(start: 0, end: 0),
        );
      }
      if (kind == '任务') {
        ws.newTask();
      }
      if (kind == '主体来源ABA') {
        source.value = 2;
        source.value = 1;
      }
      await t.pump();
      expect(voice.cancelled, contains(job.id));
      job.result.complete('不该回填的旧账号文字');
      await t.pumpAndSettle();
      expect(_field(t).controller!.text, isEmpty);
      expect(sent, isEmpty);
      expect(t.takeException(), isNull);
      await _unmount(t);
    });
  }
  testWidgets('完整页面覆盖Now立即取消，同身份迟到结果也不能回填', (t) async {
    final voice = _Voice(),
        sent = <String>[],
        navigator = GlobalKey<NavigatorState>();
    await _mount(t, voice, sent, navigator: navigator);
    await _start(t);
    final job = voice.jobs.single;
    unawaited(
      navigator.currentState!.push(
        MaterialPageRoute<void>(
          builder: (_) => const Scaffold(body: Text('完整页面')),
        ),
      ),
    );
    await t.pumpAndSettle();
    expect(voice.cancelled, contains(job.id));
    job.result.complete('覆盖期间旧文字');
    await t.pump();
    navigator.currentState!.pop();
    await t.pumpAndSettle();
    expect(_field(t).controller!.text, isEmpty);
    expect(sent, isEmpty);
    await _unmount(t);
  });
  testWidgets('等待权限时切主体再切回，迟到授予不能开麦', (t) async {
    final voice = _Voice()..permission = Completer<bool>(),
        sent = <String>[],
        source = ValueNotifier(1);
    addTearDown(source.dispose);
    await _mount(t, voice, sent, source: source);
    await _start(t);
    source.value = 2;
    source.value = 1;
    voice.permission!.complete(true);
    await t.pumpAndSettle();
    expect(voice.jobs, isEmpty);
    expect(sent, isEmpty);
    await _unmount(t);
  });
  testWidgets('本人权限窗口inactive后有效resume可继续，授予期间未采音', (t) async {
    final voice = _Voice()..permission = Completer<bool>(), sent = <String>[];
    addTearDown(
      () => t.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed),
    );
    await _mount(t, voice, sent);
    await _start(t);
    t.binding.handleAppLifecycleStateChanged(AppLifecycleState.inactive);
    voice.permission!.complete(true);
    await t.pump();
    expect(voice.jobs, isEmpty);
    t.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
    await t.pump();
    expect(voice.jobs, hasLength(1));
    voice.jobs.single.result.complete('本人授权后的草稿');
    await t.pumpAndSettle();
    expect(_field(t).controller!.text, '本人授权后的草稿');
    expect(sent, isEmpty);
    await _unmount(t);
  });
  testWidgets('实际hidden后迟到授权不会采音，resume后仍可本人重试', (t) async {
    final voice = _Voice()..permission = Completer<bool>(), sent = <String>[];
    addTearDown(
      () => t.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed),
    );
    await _mount(t, voice, sent);
    await _start(t);
    t.binding.handleAppLifecycleStateChanged(AppLifecycleState.inactive);
    t.binding.handleAppLifecycleStateChanged(AppLifecycleState.hidden);
    t.binding.handleAppLifecycleStateChanged(AppLifecycleState.paused);
    voice.permission!.complete(true);
    await t.pump();
    t.binding.handleAppLifecycleStateChanged(AppLifecycleState.hidden);
    t.binding.handleAppLifecycleStateChanged(AppLifecycleState.inactive);
    t.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
    await t.pump();
    expect(voice.jobs, isEmpty);
    voice.permission = null;
    await _start(t);
    expect(voice.jobs, hasLength(1));
    voice.jobs.single.result.complete('重新本人发起');
    await t.pumpAndSettle();
    expect(_field(t).controller!.text, '重新本人发起');
    expect(sent, isEmpty);
    await _unmount(t);
  });
  testWidgets('240字节边界完整回填不截断且仍不自动发送', (t) async {
    final voice = _Voice(), sent = <String>[];
    await _mount(t, voice, sent);
    await _start(t);
    final text = List.filled(80, '中').join();
    voice.jobs.single.result.complete(text);
    await t.pumpAndSettle();
    expect(_field(t).controller!.text, text);
    expect(sent, isEmpty);
    await _unmount(t);
  });
  testWidgets('超过240字节转写不截断，明确提示且原稿保留', (t) async {
    final voice = _Voice(), sent = <String>[];
    await _mount(t, voice, sent);
    await _start(t);
    final text = List.filled(81, '中').join();
    voice.jobs.single.result.complete(text);
    await t.pumpAndSettle();
    expect(_field(t).controller!.text, text);
    expect(find.text('文字较长，已保留，请缩短后发送。'), findsOneWidget);
    final action = find.byKey(const Key('now-composer-primary-action'));
    expect(t.widget<IconButton>(action).onPressed, isNull);
    await t.showKeyboard(find.byType(TextField));
    await t.testTextInput.receiveAction(TextInputAction.send);
    await t.pumpAndSettle();
    expect(_field(t).controller!.text, text);
    expect(sent, isEmpty);
    await t.enterText(find.byType(TextField), '附近的咖啡馆');
    await t.pump();
    expect(t.widget<IconButton>(action).onPressed, isNotNull);
    await t.testTextInput.receiveAction(TextInputAction.send);
    await t.pumpAndSettle();
    expect(sent, ['附近的咖啡馆']);
    expect(_field(t).controller!.text, isEmpty);
    await _unmount(t);
  });
  for (final scale in [1.0, 1.3, 2.0]) {
    testWidgets('320窄屏 $scale倍语音状态48触控，键盘开合保留editor与job', (t) async {
      t.view.physicalSize = const Size(320, 760);
      t.view.devicePixelRatio = 1;
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      final voice = _Voice(), sent = <String>[];
      await _mount(t, voice, sent, scale: scale);
      await _start(t);
      final controller = _field(t).controller,
          focus = _field(t).focusNode,
          id = voice.jobs.single.id;
      await t.showKeyboard(find.byType(TextField));
      await t.pump();
      expect(voice.jobs.single.id, id);
      expect(_field(t).controller, same(controller));
      expect(_field(t).focusNode, same(focus));
      expect(find.byType(TextField), findsOneWidget);
      expect(
        t.getSize(find.byTooltip('取消语音输入')).height,
        greaterThanOrEqualTo(48),
      );
      expect(
        t.getSize(find.byTooltip('停止录音并转写')).height,
        greaterThanOrEqualTo(48),
      );
      expect(t.takeException(), isNull);
      voice.jobs.single.result.complete('长中文与 Aberdeen 混排');
      await t.pumpAndSettle();
      expect(_field(t).controller!.text, '长中文与 Aberdeen 混排');
      expect(sent, isEmpty);
      await _unmount(t);
    });
  }
}
