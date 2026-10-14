import 'package:birdtie_client/src/workspace/contact_code.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  const id = 'd674bdf5-c8e4-48c4-8b72-f8d43c9aa64b';
  test('code contains only an opaque selector for server resolution', () {
    expect(contactCodeFor(id.toUpperCase()), 'birdtie://contact/$id');
    expect(contactAccountFromCode(contactCodeFor(id)), id);
  });
  test(
    'scanner rejects external URLs, auth data, encoded and unknown formats',
    () {
      for (final raw in [
        'https://birdtie.example/person/$id',
        'https://evil.example',
        'birdtie://person/$id',
        'birdtie://contact/$id?expires=1',
        'birdtie://contact/$id#fragment',
        'birdtie://contact/$id/',
        'birdtie://person/$id?access_token=secret',
        'birdtie://person/$id?expires=1',
        'birdtie://person/$id#fragment',
        'birdtie://token:secret@person/$id',
        'birdtie://person/%64${id.substring(1)}',
        'birdtie://person/$id/',
        'birdtie://person/$id\n',
        ' birdtie://person/$id',
        'birdtie://person/not-a-uuid',
        'birdtie://person/00000000-0000-0000-0000-000000000000',
        'birdtie://organization/$id',
        'tel:+123456789',
      ]) {
        expect(
          () => contactAccountFromCode(raw),
          throwsFormatException,
          reason: raw,
        );
      }
    },
  );
  test('invalid account IDs never become shareable QR payloads', () {
    expect(() => contactCodeFor('Bearer secret'), throwsFormatException);
    expect(
      () => contactCodeFor('00000000-0000-0000-0000-000000000000'),
      throwsFormatException,
    );
  });
  test('server expiry and revocation are explicit view states', () {
    final expires = DateTime.utc(2026, 10, 14);
    final active = ContactCodeView.fromJson({
      'version': 1,
      'codeId': id,
      'expiresAt': expires.toIso8601String(),
    });
    expect(
      active.activeAt(expires.subtract(const Duration(seconds: 1))),
      isTrue,
    );
    expect(active.activeAt(expires), isFalse);
    expect(
      ContactCodeView.fromJson({
        'version': 2,
        'codeId': null,
        'expiresAt': null,
      }).activeAt(expires),
      isFalse,
    );
    expect(
      () => ContactCodeView.fromJson({
        'version': 2,
        'codeId': id,
        'expiresAt': null,
      }),
      throwsFormatException,
    );
  });
}
