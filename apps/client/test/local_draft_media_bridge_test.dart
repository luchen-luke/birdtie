import 'dart:convert';

import 'package:birdtie_client/src/workspace/local_draft_media_bridge.dart';
import 'package:birdtie_client/src/workspace/typed_local_draft.dart';
import 'package:flutter_test/flutter_test.dart';

TypedLocalDraft _draft({String owner = 'owner', String? task}) =>
    TypedLocalDraft(
      id: 'draft',
      owner: owner,
      origin: 'https://local-bridge.test',
      kind: 'intent',
      text: '本人尚未提交的内容',
      taskID: task,
      references: const [
        LocalDraftReference(kind: 'place', id: 'original-place'),
      ],
    );

LocalDraftMediaBridge _bridge() {
  var id = 0;
  return LocalDraftMediaBridge(
    newSelector: () =>
        'c6000000-0000-4000-8000-${(++id).toString().padLeft(12, '0')}',
  );
}

LocalDraftMediaSelection _selected(
  Object identity,
  Object source, {
  bool current = true,
  String state = 'local',
  bool Function()? sourceCurrent,
}) => LocalDraftMediaSelection(
  identity: identity,
  source: source,
  current: current,
  state: state,
  sourceCurrent: sourceCurrent,
);

void main() {
  test(
    'current predicate cannot release an old source after changing namespace',
    () {
      final bridge = _bridge(), draft = _draft(), source = Object();
      final binding = bridge.bind(LocalDraftMediaNamespace.fromDraft(draft));
      var change = false;
      bool current() {
        if (change) {
          bridge.bind(
            LocalDraftMediaNamespace.fromDraft(_draft(owner: 'changed')),
          );
        }
        return true;
      }

      final next = bridge.sync(
        binding,
        draft,
        kind: 'image',
        selections: [_selected(source, source, sourceCurrent: current)],
      );
      final capture = bridge.capture(binding, next.attachments.single)!;
      change = true;
      expect(capture.source, isNull);
      final fresh = bridge.bind(LocalDraftMediaNamespace.fromDraft(draft));
      expect(
        () => bridge.sync(
          fresh,
          draft,
          kind: 'image',
          selections: [_selected(source, source, sourceCurrent: current)],
        ),
        throwsStateError,
      );
      expect(bridge.isCurrent(fresh), isFalse);
    },
  );
  test(
    'persistent projection contains only opaque selector kind and state',
    () {
      final bridge = _bridge(), draft = _draft();
      final binding = bridge.bind(LocalDraftMediaNamespace.fromDraft(draft));
      final secret = {
        'pixels': [2, 9],
        'path': '/private/photo.png',
        'token': 'Bearer secret',
        'approval': 'approval',
        'receipt': 'server-receipt',
        'sourceTitle': '私人来源标题',
      };
      final next = bridge.sync(
        binding,
        draft,
        kind: 'image',
        selections: [_selected(('RAM-only-key', 1), secret)],
      );
      final attachment = next.attachments.single;
      expect(attachment.toJson().keys.toSet(), {'id', 'kind', 'state'});
      expect(attachment.id, isNot(contains('RAM-only-key')));
      final wire = jsonEncode(next.toJson());
      for (final value in [
        'pixels',
        '/private/photo.png',
        'Bearer secret',
        'approval',
        'server-receipt',
        '私人来源标题',
        'RAM-only-key',
      ]) {
        expect(wire, isNot(contains(value)));
      }
      expect(bridge.capture(binding, attachment)!.source, same(secret));
      expect(next.references, orderedEquals(draft.references));
      expect(
        TypedLocalDraft.fromJson(next.toJson()).attachments.single.id,
        attachment.id,
      );
    },
  );

  test('same selection and semantic metadata return identical draft', () {
    final bridge = _bridge(), initial = _draft(), source = Object();
    final binding = bridge.bind(LocalDraftMediaNamespace.fromDraft(initial));
    final first = bridge.sync(
      binding,
      initial,
      kind: 'image',
      selections: [_selected(('controller', 4), source)],
    );
    for (var n = 0; n < 10; n++) {
      final second = bridge.sync(
        binding,
        first,
        kind: 'image',
        selections: [_selected(('controller', 4), source)],
      );
      expect(second, same(first));
      expect(second.revision, first.revision);
      expect(
        bridge.bind(LocalDraftMediaNamespace.fromDraft(second)),
        same(binding),
      );
    }
  });

  test(
    'source replacement on same key keeps selector but retires old capture',
    () {
      final bridge = _bridge(),
          initial = _draft(),
          old = Object(),
          next = Object();
      final binding = bridge.bind(LocalDraftMediaNamespace.fromDraft(initial));
      final first = bridge.sync(
        binding,
        initial,
        kind: 'image',
        selections: [_selected(('controller', 4), old)],
      );
      final captured = bridge.capture(binding, first.attachments.single)!;
      final renewed = bridge.sync(
        binding,
        first,
        kind: 'image',
        selections: [_selected(('controller', 4), next)],
      );
      expect(renewed, same(first));
      expect(captured.source, isNull);
      expect(
        bridge.capture(binding, renewed.attachments.single)!.source,
        same(next),
      );
      final changed = bridge.sync(
        binding,
        renewed,
        kind: 'image',
        selections: [_selected(('controller', 5), next)],
      );
      expect(changed.attachments.single.id, isNot(first.attachments.single.id));
      expect(bridge.capture(binding, first.attachments.single), isNull);
    },
  );

  test('kind updates preserve order and remove only owned selector', () {
    final bridge = _bridge();
    var draft = _draft().edit(
      attachments: const [
        LocalDraftAttachment(
          id: 'restored-image',
          kind: 'image',
          state: 'unknown',
        ),
        LocalDraftAttachment(
          id: 'unrelated-file',
          kind: 'file',
          state: 'uploaded',
        ),
      ],
    );
    final binding = bridge.bind(LocalDraftMediaNamespace.fromDraft(draft));
    final image = Object(), video = Object();
    draft = bridge.sync(
      binding,
      draft,
      kind: 'image',
      selections: [_selected(image, image)],
    );
    draft = bridge.sync(
      binding,
      draft,
      kind: 'video',
      selections: [_selected(video, video)],
    );
    final stable = draft;
    for (var n = 0; n < 4; n++) {
      draft = bridge.sync(
        binding,
        draft,
        kind: 'image',
        selections: [_selected(image, image)],
      );
      draft = bridge.sync(
        binding,
        draft,
        kind: 'video',
        selections: [_selected(video, video)],
      );
      expect(draft, same(stable));
    }
    final removed = bridge.sync(binding, draft, kind: 'image', selections: []);
    expect(
      removed.attachments.map((a) => a.id).take(2),
      orderedEquals(['restored-image', 'unrelated-file']),
    );
    expect(removed.attachments.last.kind, 'video');
    expect(removed.references, orderedEquals(draft.references));
    expect(removed.attachments.first.state, 'unknown');
    expect(
      bridge.displayNeedsReselect(binding, removed.attachments.first),
      isTrue,
    );
  });

  test(
    'restored historical states never capture RAM or imply reusable receipt',
    () {
      final bridge = _bridge();
      final draft = _draft().edit(
        attachments: const [
          LocalDraftAttachment(
            id: 'local-before-restart',
            kind: 'image',
            state: 'local',
          ),
          LocalDraftAttachment(
            id: 'uploaded-history',
            kind: 'video',
            state: 'uploaded',
          ),
          LocalDraftAttachment(
            id: 'unknown-history',
            kind: 'audio',
            state: 'unknown',
          ),
        ],
      );
      final restored = TypedLocalDraft.fromJson(draft.toJson());
      final binding = bridge.bind(LocalDraftMediaNamespace.fromDraft(restored));
      for (final attachment in restored.attachments) {
        expect(bridge.displayNeedsReselect(binding, attachment), isTrue);
        expect(bridge.capture(binding, attachment), isNull);
      }
      expect(
        bridge.sync(binding, restored, kind: 'image', selections: []),
        same(restored),
      );
    },
  );

  test(
    'current controller loss retires captured source without dropping unknown',
    () {
      final bridge = _bridge(), initial = _draft(), source = Object();
      var current = true;
      final binding = bridge.bind(LocalDraftMediaNamespace.fromDraft(initial));
      final draft = bridge.sync(
        binding,
        initial,
        kind: 'video',
        selections: [
          _selected(
            source,
            source,
            state: 'unknown',
            sourceCurrent: () => current,
          ),
        ],
      );
      final captured = bridge.capture(binding, draft.attachments.single)!;
      current = false;
      expect(captured.source, isNull);
      expect(
        bridge.displayNeedsReselect(binding, draft.attachments.single),
        isTrue,
      );
      current = true;
      expect(captured.source, isNull);
      expect(draft.attachments.single.state, 'unknown');
      expect(draft.attachments, hasLength(1));
    },
  );

  test('namespace ABA permanently retires old frames and late sync', () {
    final bridge = _bridge(), a = _draft(), source = Object();
    final old = bridge.bind(LocalDraftMediaNamespace.fromDraft(a));
    final selected = bridge.sync(
      old,
      a,
      kind: 'image',
      selections: [_selected(source, source)],
    );
    final captured = bridge.capture(old, selected.attachments.single)!;
    final b = _draft(owner: 'another-owner');
    bridge.bind(LocalDraftMediaNamespace.fromDraft(b));
    final fresh = bridge.bind(LocalDraftMediaNamespace.fromDraft(a));
    expect(fresh, isNot(same(old)));
    expect(bridge.isCurrent(old), isFalse);
    expect(captured.source, isNull);
    expect(bridge.capture(fresh, selected.attachments.single), isNull);
    expect(
      () => bridge.sync(
        old,
        selected,
        kind: 'image',
        selections: [_selected(source, source)],
      ),
      throwsStateError,
    );
    final task = selected.edit(taskID: 'new-real-task');
    bridge.bind(LocalDraftMediaNamespace.fromDraft(task));
    expect(bridge.isCurrent(fresh), isFalse);
    expect(task.attachments.single.id, selected.attachments.single.id);
  });

  test(
    'explicit source retirement keeps unknown history and unrelated live slot',
    () {
      final bridge = _bridge(),
          initial = _draft(),
          image = Object(),
          video = Object();
      final binding = bridge.bind(LocalDraftMediaNamespace.fromDraft(initial));
      var draft = bridge.sync(
        binding,
        initial,
        kind: 'image',
        selections: [_selected(image, image, state: 'unknown')],
      );
      draft = bridge.sync(
        binding,
        draft,
        kind: 'video',
        selections: [_selected(video, video)],
      );
      final oldImage = bridge.capture(binding, draft.attachments.first)!;
      bridge.retireSources(binding, kind: 'image');
      expect(oldImage.source, isNull);
      expect(draft.attachments.first.state, 'unknown');
      expect(draft.attachments, hasLength(2));
      expect(
        bridge.capture(binding, draft.attachments.last)!.source,
        same(video),
      );
      final replacement = bridge.sync(
        binding,
        draft,
        kind: 'image',
        selections: [_selected(image, Object(), state: 'unknown')],
      );
      expect(replacement, same(draft));
      expect(oldImage.source, isNull);
      expect(bridge.capture(binding, replacement.attachments.first), isNotNull);
    },
  );

  test('namespace isolates origin workspace recipient and target keys', () {
    final bridge = _bridge();
    LocalDraftMediaNamespace namespace({
      String origin = 'https://a.test',
      String? workspace,
      String target = 'first',
    }) => LocalDraftMediaNamespace(
      owner: 'owner',
      origin: origin,
      workspaceID: workspace,
      targetKey: target,
    );
    var old = bridge.bind(namespace());
    for (final next in [
      namespace(origin: 'https://b.test'),
      namespace(workspace: 'organization'),
      namespace(target: 'recipient-2'),
    ]) {
      final fresh = bridge.bind(next);
      expect(bridge.isCurrent(old), isFalse);
      expect(bridge.isCurrent(fresh), isTrue);
      old = fresh;
    }
    bridge.dispose();
    expect(bridge.isCurrent(old), isFalse);
    expect(() => bridge.bind(namespace()), throwsStateError);
  });

  test(
    'capacity and closed input reject atomically without removing unrelated',
    () {
      final bridge = _bridge(), source = Object();
      final draft = _draft().edit(
        attachments: List.generate(
          10,
          (i) => LocalDraftAttachment(
            id: 'unrelated-$i',
            kind: 'file',
            state: 'local',
          ),
        ),
      );
      final binding = bridge.bind(LocalDraftMediaNamespace.fromDraft(draft));
      expect(
        () => bridge.sync(
          binding,
          draft,
          kind: 'image',
          selections: [_selected(source, source)],
        ),
        throwsFormatException,
      );
      expect(draft.attachments, hasLength(10));
      expect(
        bridge.sync(binding, draft, kind: 'image', selections: []),
        same(draft),
      );
      expect(
        () => bridge.sync(
          binding,
          draft,
          kind: 'server-approved',
          selections: [],
        ),
        throwsFormatException,
      );
      expect(
        () => bridge.sync(
          binding,
          draft,
          kind: 'image',
          selections: [_selected(source, source, state: 'approved')],
        ),
        throwsFormatException,
      );
      expect(
        () => bridge.sync(
          binding,
          draft,
          kind: 'image',
          selections: [_selected(source, source), _selected(source, source)],
        ),
        throwsFormatException,
      );
    },
  );
}
