import 'dart:async';
import 'package:birdtie_client/src/workspace/organization_human_control.dart';
import 'package:birdtie_client/src/workspace/organization_map_location_api.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'organization_human_control_test.dart';

Map<String, dynamic> orgCanonicalWire({
  int revision = 3,
  String visibility = 'public',
}) => {
  'organizationId': orgTestID,
  'cityId': 'aberdeen-gb',
  'latitude': 57.1,
  'longitude': -2.1,
  'visibility': visibility,
  'reviewStatus': 'approved',
  'revision': revision,
};
Map<String, dynamic> orgProposalWire({
  String id = orgTestProposal,
  int version = 2,
  int canonical = 3,
  String status = 'PENDING',
  bool review = false,
  bool withdraw = false,
}) => {
  'id': id,
  'organizationId': orgTestID,
  'cityId': 'aberdeen-gb',
  'latitude': 57.2,
  'longitude': -2.2,
  'status': status,
  'version': version,
  'canonicalRevision': canonical,
  'submittedBy': orgTestTarget,
  'canApprove': review,
  'canReject': review,
  'canWithdraw': withdraw,
};
Map<String, dynamic> orgMapWire({
  String role = 'owner',
  int authority = 4,
  Map<String, dynamic>? canonical,
  bool noCanonical = false,
  List<Map<String, dynamic>>? proposals,
  int lease = 25,
}) => {
  'organizationId': orgTestID,
  'authorityVersion': authority,
  'role': role,
  'canonical': noCanonical ? null : canonical ?? orgCanonicalWire(),
  'proposals': proposals ?? [],
  'observedAt': DateTime.now().toUtc().toIso8601String(),
  'expiresAt': DateTime.now()
      .toUtc()
      .add(Duration(seconds: lease))
      .toIso8601String(),
};

class OrgOwnershipClient extends MockClient {
  OrgOwnershipClient(super.fn);
  bool closed = false;
  @override
  void close() {
    closed = true;
    super.close();
  }
}

void main() {
  test(
    '139 nonmember city reviewer has original canonical point and exact proposal actions',
    () {
      final s = OrganizationMapControl.fromJson(
        orgMapWire(
          role: 'reviewer',
          proposals: [orgProposalWire(review: true)],
        ),
        orgTestID,
      );
      expect(s.role, 'reviewer');
      expect(s.canManage, isFalse);
      expect(s.proposals.single.canApprove, isTrue);
      expect(s.canonical!.latitude, 57.1);
      expect(s.proposals.single.latitude, 57.2);
      expect(s.canonicalRevision, 3);
    },
  );
  test(
    '139 coordinates allow actual WGS84 boundaries and reject unpaired or nonfinite points',
    () {
      expect(organizationWGS84(-90, 180), isTrue);
      expect(organizationWGS84(90, -180), isTrue);
      for (final pair in [
        (double.nan, 0),
        (0, double.infinity),
        (91, 0),
        (0, -181),
        (0, null),
      ])
        expect(organizationWGS84(pair.$1, pair.$2), isFalse);
      final p = orgCanonicalWire();
      expect(
        () => OrganizationCanonicalPoint.fromJson({
          ...p,
          'latitude': 91,
        }, orgTestID),
        throwsFormatException,
      );
      expect(
        () => OrganizationCanonicalPoint.fromJson({
          ...p,
          'coordinateSystem': 'gcj02',
        }, orgTestID),
        throwsFormatException,
      );
    },
  );
  test(
    '139 map target, source lease, duplicate and completed permission flags are strict',
    () {
      final data = orgMapWire(proposals: [orgProposalWire(review: true)]);
      for (final invalid in [
        {...data, 'organizationId': orgTestTarget},
        {
          ...data,
          'expiresAt': DateTime.now()
              .toUtc()
              .add(const Duration(minutes: 1))
              .toIso8601String(),
        },
        {
          ...data,
          'proposals': [orgProposalWire(), orgProposalWire()],
        },
        {
          ...data,
          'proposals': [orgProposalWire(status: 'APPROVED', review: true)],
        },
        {
          ...data,
          'proposals': [
            {...orgProposalWire(), 'canApprove': 1},
          ],
        },
      ])
        expect(
          () => OrganizationMapControl.fromJson(invalid, orgTestID),
          throwsFormatException,
        );
    },
  );
  test(
    '139 map API personal transport and borrowed lifetime preserve original source',
    () async {
      final client = OrgOwnershipClient((r) async {
        expectSync(
          r.url.path,
          '/v1/me/organizations/$orgTestID/map-location/control',
        );
        expectSync(r.headers['Authorization'], 'Bearer personal');
        expectSync(
          r.headers.keys.where((k) => k.toLowerCase().contains('workspace')),
          isEmpty,
        );
        return orgWire(orgMapWire());
      });
      final api = OrganizationMapLocationApi(
        authorizationHeader: () => 'Bearer personal',
        client: client,
        apiBaseUrl: orgTestBase,
      );
      expect((await api.control(orgTestID)).canonicalRevision, 3);
      api.dispose();
      expect(client.closed, isFalse);
      client.close();
    },
  );
  test(
    '139 map read original source guard retires late target ABA despite matching final ID',
    () async {
      final delayed = Completer<http.Response>();
      var current = true;
      final api = OrganizationMapLocationApi(
        authorizationHeader: () => 'Bearer personal',
        client: MockClient((r) => delayed.future),
        apiBaseUrl: orgTestBase,
      );
      final future = api.control(orgTestID, guard: () => current);
      current = false;
      delayed.complete(orgWire(orgMapWire()));
      await expectLater(future, throwsA(isA<OrganizationControlFailure>()));
      api.dispose();
    },
  );
  test(
    '139 submit and independent approval use distinct original canonical version axes',
    () {
      const submit = OrganizationControlInput(
        action: 'PROPOSE',
        operationID: orgTestOp,
        expectedAuthorityVersion: 4,
        cityID: 'aberdeen-gb',
        latitude: 57.2,
        longitude: -2.2,
        expectedCanonicalRevision: 3,
      );
      final proposed = OrganizationControlReceipt.fromJson(
        orgReceiptWire(submit.selector(orgTestID)),
        submit.selector(orgTestID),
      );
      expect(proposed.canonicalRevision, 3);
      expect(proposed.proposalVersion, 1);
      expect(proposed.authorityVersion, 4);
      const approve = OrganizationControlInput(
        action: 'APPROVE_POINT',
        operationID: orgTestOp,
        expectedAuthorityVersion: 4,
        proposalID: orgTestProposal,
        expectedProposalVersion: 2,
        expectedCanonicalRevision: 3,
      );
      final approved = OrganizationControlReceipt.fromJson(
        orgReceiptWire(approve.selector(orgTestID)),
        approve.selector(orgTestID),
      );
      expect(approved.canonicalRevision, 4);
      expect(approved.proposalVersion, 3);
      expect(approved.authorityVersion, 4);
      expect(
        () => OrganizationControlReceipt.fromJson({
          ...orgReceiptWire(approve.selector(orgTestID)),
          'canonicalRevision': 3,
        }, approve.selector(orgTestID)),
        throwsFormatException,
      );
    },
  );
}
