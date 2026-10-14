import 'package:flutter_test/flutter_test.dart';
import 'package:birdtie_client/src/workspace/agent_visual_pending_store.dart';
import 'agent_visual_controller_test.dart' show visualOwner, visualSource;

void main() {
  test('original city slug is retained and separated from online selector', () {
    final city = SecureAgentVisualPendingStore.key('https://a.test', visualOwner, 'CITY:aberdeen');
    final online = SecureAgentVisualPendingStore.key('https://a.test', visualOwner, 'ONLINE:aberdeen');
    expect(city,isNot(online));
    expect(()=>SecureAgentVisualPendingStore.key('https://a.test',visualOwner,'CITY:bad\nsource'),throwsFormatException);
  });
  test(
    'recovery selectors separate owner, source, origin; no text or pixel key',
    () {
      final a = SecureAgentVisualPendingStore.key(
        'https://a.test',
        visualOwner,
        visualSource,
      );
      expect(
        a,
        SecureAgentVisualPendingStore.key(
          'https://a.test/',
          visualOwner,
          visualSource,
        ),
      );
      expect(
        a,
        isNot(
          SecureAgentVisualPendingStore.key(
            'https://b.test',
            visualOwner,
            visualSource,
          ),
        ),
      );
      expect(
        a,
        isNot(
          SecureAgentVisualPendingStore.key(
            'https://a.test',
            visualSource,
            visualOwner,
          ),
        ),
      );
      expect(a.contains(visualOwner), isFalse);
      for (final origin in [
        'https://user:password@a.test',
        'https://a.test/?token=secret',
        'ftp://a.test',
      ]) {
        expect(
          () => SecureAgentVisualPendingStore.key(
            origin,
            visualOwner,
            visualSource,
          ),
          throwsFormatException,
        );
      }
    },
  );
}
