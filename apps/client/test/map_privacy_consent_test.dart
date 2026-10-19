import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/city/map_privacy_consent.dart';
import 'package:flutter_test/flutter_test.dart';

class ControlledStore implements MapPrivacyConsentStore {
  String? record;
  bool failRead = false, failWrite = false, mismatch = false;
  int writes = 0;
  Completer<void>? writeGate;
  @override
  Future<String?> read() async {
    if (failRead) throw StateError('isolated read failure');
    return mismatch ? 'broken' : record;
  }

  @override
  Future<void> write(String value) async {
    writes++;
    if (writeGate != null) await writeGate!.future;
    if (failWrite) throw StateError('isolated write failure');
    record = value;
  }
}

void main() {
  test(
    'true consent waits durable write and read-back, then restores this version',
    () async {
      final store = ControlledStore()..writeGate = Completer<void>();
      final c = MapPrivacyConsentController(store: store);
      await c.load();
      expect(c.loaded, true);
      expect(c.hasDecision, false);
      expect(c.accepted, false);
      expect(store.writes, 0);
      final choosing = c.setDecision(true);
      expect(c.busy, true);
      expect(c.accepted, false);
      store.writeGate!.complete();
      expect(await choosing, true);
      expect(c.accepted, true);
      final raw = jsonDecode(store.record!) as Map;
      expect(raw.keys.toSet(), {
        'schema',
        'policyVersion',
        'policyURL',
        'disclosureSHA256',
        'decision',
      });
      expect(raw['policyVersion'], mapPrivacyPolicyVersion);
      final restarted = MapPrivacyConsentController(store: store);
      await restarted.load();
      expect(restarted.hasDecision, true);
      expect(restarted.accepted, true);
      c.dispose();
      restarted.dispose();
    },
  );
  test(
    'a real refusal is durable and does not become unknown on restart',
    () async {
      final store = ControlledStore();
      final c = MapPrivacyConsentController(store: store);
      expect(await c.setDecision(false), true);
      expect(c.hasDecision, true);
      expect(c.accepted, false);
      final restarted = MapPrivacyConsentController(store: store);
      await restarted.load();
      expect(restarted.hasDecision, true);
      expect(restarted.accepted, false);
      c.dispose();
      restarted.dispose();
    },
  );
  test(
    'read/write/readback faults never invent a decision or enable an SDK',
    () async {
      for (final failure in ['read', 'write', 'readback']) {
        final store = ControlledStore()
          ..failRead = failure == 'read'
          ..failWrite = failure == 'write'
          ..mismatch = failure == 'readback';
        final c = MapPrivacyConsentController(store: store);
        expect(await c.setDecision(true), false);
        expect(c.loaded, true);
        expect(c.hasDecision, false);
        expect(c.accepted, false);
        expect(c.error, isNotNull);
        if (failure == 'read') expect(store.writes, 0);
        c.dispose();
      }
    },
  );
  test(
    'changed disclosure or unreadable record is never inherited as agreement',
    () async {
      final store = ControlledStore();
      final old = MapPrivacyConsentController(store: store);
      await old.setDecision(true);
      final valid = jsonDecode(store.record!) as Map<String, dynamic>;
      for (final changed in [
        {...valid, 'policyVersion': 'old-version'},
        {...valid, 'policyVersion': 'amap-map-search-20261018-v1'},
        {...valid, 'disclosureSHA256': '0' * 64},
        {...valid, 'accountId': 'not-part-of-sdk-consent'},
      ]) {
        final c = MapPrivacyConsentController(
          store: MemoryMapPrivacyConsentStore(record: jsonEncode(changed)),
        );
        await c.load();
        expect(c.hasDecision, false);
        expect(c.accepted, false);
        c.dispose();
      }
      old.dispose();
    },
  );
  test(
    'revocation is immediate and failed persistence cannot resurrect old true in memory',
    () async {
      final store = ControlledStore();
      final c = MapPrivacyConsentController(store: store);
      await c.setDecision(true);
      store.writeGate = Completer<void>();
      store.failWrite = true;
      final revoke = c.setDecision(false);
      expect(c.accepted, false);
      store.writeGate!.complete();
      expect(await revoke, false);
      expect(c.hasDecision, false);
      await c.load();
      expect(c.accepted, false);
      expect(c.hasDecision, false);
      c.dispose();
    },
  );
}
