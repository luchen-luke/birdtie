"""PLAN by default. Read only the fixed synthetic QA03 unified editor thread."""
import argparse
import hashlib
import json
import re
from datetime import datetime, timezone
from pathlib import Path
from urllib.error import HTTPError
from urllib.request import HTTPRedirectHandler, ProxyHandler, Request, build_opener

BASE = 'https://47.110.154.133'
ROOT_MOMENT = '96e29d12-2a35-47e3-aeb3-75a8425542f5'
THREAD = '8ca660be-f15f-4bad-acc9-87d9a1f25036'
WORK = Path(__file__).resolve().parent
UUID = re.compile(r'[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}')
LOCATION_HEADERS = {'X-Birdtie-Map-Locations': 'locations-v2'}
DETAIL_HEADERS = {'X-Birdtie-Public-Moment-Context': 'city-v1',
                  'X-Birdtie-Public-Moment-Media': 'photo-v1'}


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):
        return None


def stamp():
    return datetime.now(timezone.utc).isoformat()


def require(condition, code):
    if not condition:
        raise RuntimeError(code)


def text_summary(value):
    require(isinstance(value, str), 'TEXT_SCHEMA_INVALID')
    # Only explicitly recognizable ASCII QA prose is copied into the receipt.
    # Any other text remains in RAM; only its length and hash leave the reader.
    safe = (len(value) <= 300 and value.isascii() and
            re.fullmatch(r'(?:QA[-_ ].*|Birdtie device(?:17|18|19)\b.*)', value))
    return {'utf8Bytes': len(value.encode()), 'characters': len(value),
            'sha256': hashlib.sha256(value.encode()).hexdigest(),
            'safeQAContent': value if safe or value == '' else None}


def locations_summary(value, moment, revision, own):
    require(isinstance(value, dict) and value.get('schemaVersion') == 'moment-locations-v2'
            and value.get('momentId') == moment and value.get('momentRevision') == revision
            and isinstance(value.get('items'), list) and len(value['items']) <= 50,
            'LOCATION_SIDECAR_BINDING_INVALID')
    result = []
    for i, item in enumerate(value['items']):
        require(item.get('position' if own else 'displayOrder') == i,
                'LOCATION_ORDER_INVALID')
        require(item.get('kind') in {'CITY', 'PLACE', 'REFERENCE'}, 'LOCATION_KIND_INVALID')
        require('legacyReference' not in item, 'LEGACY_REFERENCE_FORBIDDEN_IN_QA_THREAD')
        result.append({k: item[k] for k in ('id', 'kind', 'precision', 'cityId', 'cityName',
                       'placeId', 'placeName', 'referenceId', 'referenceName', 'available') if k in item})
    return result


def media_summary(value, moment, revision):
    require(isinstance(value, dict) and value.get('momentId') == moment
            and value.get('momentRevision') == revision
            and value.get('schemaVersion') in {'public-moment-media-set-v1', 'public-moment-media-set-v2'}
            and value.get('status') in {'EMPTY', 'READY', 'DELETED'}
            and isinstance(value.get('attachments'), list) and len(value['attachments']) <= 9,
            'MEDIA_SIDECAR_BINDING_INVALID')
    items = []
    for i, item in enumerate(value['attachments']):
        kind = item.get('kind', 'IMAGE')
        require(item.get('position') == i and kind in {'IMAGE', 'VIDEO'}
                and UUID.fullmatch(item.get('mediaId', ''))
                and isinstance(item.get('mediaVersion'), int), 'MEDIA_SELECTOR_INVALID')
        items.append({'position': i, 'kind': kind, 'mediaId': item['mediaId'],
                      'mediaVersion': item['mediaVersion']})
    require(not any(x['kind'] == 'VIDEO' for x in items) or len(items) == 1,
            'MEDIA_IMAGE_VIDEO_MIX_FORBIDDEN')
    return {'schemaVersion': value['schemaVersion'], 'status': value['status'],
            'setVersion': value['setVersion'], 'count': len(items), 'items': items}


def execute(args):
    require(re.fullmatch(r'[a-z0-9-]{1,90}', args.name), 'OUTPUT_NAME_INVALID')
    out = WORK / args.name
    out.mkdir(exist_ok=False)
    opener = build_opener(ProxyHandler({}), NoRedirect())
    token = None
    events = []
    result = {'status': 'FAIL', 'started': stamp(), 'origin': BASE,
              'syntheticAccount': 'SYNTHETIC_QA03_ONLY', 'legacyOwnersAccessed': 0,
              'contentWrites': 0, 'previewsCreated': 0, 'mediaBytesRead': 0,
              'phoneSessionMutated': False, 'otherQAAccountLogin': False,
              'otherQAAccountBoundary': 'NOT_RUN_ONLY_QA03_SESSION_AUTHORIZED',
              'ownSessionLogout204': False, 'ownSessionRejected401': False,
              'rootMomentId': ROOT_MOMENT, 'threadId': THREAD,
              'expectedState': args.expect_state, 'members': []}

    def call(label, method, path, body=None, expected=(200,), auth=True, headers=None):
        require(method == 'GET' or path in {'/v1/auth/dev-phone/code',
                '/v1/auth/dev-phone/verify', '/v1/session/logout'}, 'CONTENT_MUTATION_FORBIDDEN')
        require(len(events) < 160, 'REQUEST_COUNT_BOUND_EXCEEDED')
        h = {'Accept': 'application/json', **(headers or {})}
        if auth:
            require(token is not None, 'NO_HELPER_SESSION')
            h['Authorization'] = 'Bearer ' + token
        raw = None if body is None else json.dumps(body).encode()
        if raw is not None:
            h['Content-Type'] = 'application/json'
        try:
            with opener.open(Request(BASE + path, data=raw, headers=h, method=method), timeout=20) as response:
                status = response.status
                payload = response.read(2 * 1024 * 1024 + 1)
        except HTTPError as error:
            status = error.code
            payload = error.read(8192)
        require(len(payload) <= 2 * 1024 * 1024, 'BOUNDED_RESPONSE_EXCEEDED')
        value = {} if not payload else json.loads(payload)
        require(isinstance(value, dict), 'RESPONSE_ENVELOPE_INVALID')
        code = (value.get('error') or {}).get('code')
        code = code if code is None or re.fullmatch(r'[A-Za-z0-9_-]{1,100}', str(code)) else 'redacted'
        events.append({'label': label, 'method': method, 'status': status, 'machineCode': code})
        require(status in expected, 'UNEXPECTED_HTTP_STATUS')
        return status, value.get('data')

    try:
        call('fixed-development-challenge', 'POST', '/v1/auth/dev-phone/code',
             {'phone': '+447700900003'}, auth=False)
        _, login = call('fixed-development-login', 'POST', '/v1/auth/dev-phone/verify',
                        {'phone': '+447700900003', 'code': '123456'}, auth=False)
        token = login['accessToken']
        result['ownSessionCreated'] = True
        _, actor = call('current-synthetic-person', 'GET', '/v1/me')
        require(actor.get('accountType') == 'person' and UUID.fullmatch(actor.get('id', '')),
                'SYNTHETIC_ACTOR_INVALID')
        owner = actor['id']
        _, original = call('exact-own-original-moment', 'GET', '/v1/me/moments/' + ROOT_MOMENT)
        require(original.get('id') == ROOT_MOMENT and original.get('authorAccountId') == owner,
                'ORIGINAL_MOMENT_OWNER_MISMATCH')
        _, owned = call('exact-own-thread', 'GET', '/v1/me/moment-threads/' + THREAD)
        require(owned.get('schemaVersion') == 'owned-moment-thread-v2'
                and owned.get('threadId') == THREAD and owned.get('rootMomentId') == ROOT_MOMENT
                and owned.get('audience') == 'PUBLIC'
                and owned.get('status') in {'published', 'withdrawn'}, 'OWNED_THREAD_BINDING_INVALID')
        active, retired = owned['members'], owned['retiredMembers']
        require(isinstance(active, list) and 1 <= len(active) <= 20
                and isinstance(retired, list) and len(retired) <= 20, 'THREAD_MEMBER_BOUND_INVALID')
        require(all(m.get('position') == i and m.get('status') in {'AVAILABLE', 'UNAVAILABLE'}
                    for i, m in enumerate(active)), 'CURRENT_DISPLAY_ORDER_INVALID')
        require(all(m.get('status') == 'RETIRED' and UUID.fullmatch(m.get('momentId', ''))
                    for m in retired), 'RETIRED_MEMBER_INVALID')
        active_ids = [m['momentId'] for m in active if m.get('momentId') is not None]
        retired_ids = [m['momentId'] for m in retired]
        require(len(set(active_ids)) == len(active_ids) and len(set(retired_ids)) == len(retired_ids),
                'DUPLICATE_CURRENT_MEMBER')
        require(ROOT_MOMENT in set(active_ids + retired_ids), 'ORIGINAL_ID_NOT_RETAINED')
        result.update(threadVersion=owned['version'], threadStatus=owned['status'],
                      currentDisplayMembers=active, retiredMembers=retired,
                      originalMomentRetained=True)
        thread_status, public_thread = call('anonymous-thread', 'GET', '/v1/moment-threads/' + THREAD,
                                          expected=(200, 404), auth=False)
        if owned['status'] == 'published':
            require(thread_status == 200 and public_thread.get('schemaVersion') == 'moment-thread-view-v2'
                    and public_thread.get('threadId') == THREAD and public_thread.get('version') == owned['version']
                    and public_thread.get('members') == active, 'PUBLIC_THREAD_DISPLAY_MISMATCH')
            require(public_thread.get('rootMomentId') == (ROOT_MOMENT if ROOT_MOMENT in active_ids else None),
                    'PUBLIC_ROOT_IDENTITY_INVALID')
            require(public_thread.get('canWithdraw') is False, 'ANONYMOUS_WITHDRAW_CAPABILITY_LEAK')
        else:
            require(thread_status == 404, 'WITHDRAWN_THREAD_STILL_PUBLIC')
        result['anonymousThreadStatus'] = thread_status
        call('anonymous-owned-thread-boundary', 'GET', '/v1/me/moment-threads/' + THREAD,
             expected=(401,), auth=False)
        all_ids = list(dict.fromkeys(active_ids + retired_ids))
        for index, mid in enumerate(all_ids):
            require(UUID.fullmatch(mid), 'MEMBER_ID_INVALID')
            _, own = call('own-member-' + str(index), 'GET', '/v1/me/moments/' + mid)
            require(own.get('id') == mid and own.get('authorAccountId') == owner
                    and own.get('status') in {'published', 'withdrawn'}, 'MEMBER_OWNER_STATE_INVALID')
            revision = own['revision']
            matching = [m for m in active + retired if m.get('momentId') == mid]
            require(matching and all(m['revision'] == revision for m in matching), 'MEMBER_REVISION_MISMATCH')
            is_public = own['status'] == 'published' and own.get('visibility') == 'public' and mid in active_ids
            expected = (200,) if is_public else (404,)
            status, public = call('anonymous-member-' + str(index), 'GET', '/v1/moments/' + mid,
                                  expected=expected, auth=False, headers=DETAIL_HEADERS)
            row = {'momentId': mid, 'revision': revision, 'status': own['status'],
                   'visibility': own['visibility'], 'originalMoment': mid == ROOT_MOMENT,
                   'displayPosition': next((m['position'] for m in active if m.get('momentId') == mid), None),
                   'retiredOriginalPosition': next((m['position'] for m in retired if m['momentId'] == mid), None),
                   'body': text_summary(own['body']), 'title': text_summary(own['title']),
                   'anonymousDetailStatus': status}
            call('anonymous-owner-member-boundary-' + str(index), 'GET', '/v1/me/moments/' + mid,
                 expected=(401,), auth=False)
            _, loc = call('anonymous-locations-' + str(index), 'GET', '/v1/moments/' + mid + '/locations',
                          expected=expected, auth=False, headers=LOCATION_HEADERS)
            _, media = call('anonymous-media-' + str(index), 'GET', '/v1/moments/' + mid + '/public-media',
                            expected=expected, auth=False)
            if is_public:
                require(public.get('id') == mid and public.get('revision') == revision
                        and public.get('body') == own['body'] and public.get('title') == own['title'],
                        'PUBLIC_DETAIL_REVISION_OR_CONTENT_MISMATCH')
                _, own_loc = call('owner-locations-' + str(index), 'GET',
                                  '/v1/me/moments/' + mid + '/locations', headers=LOCATION_HEADERS)
                row['locations'] = locations_summary(loc, mid, revision, False)
                row['ownerLocations'] = locations_summary(own_loc, mid, revision, True)
                row['media'] = media_summary(media, mid, revision)
            else:
                row.update(locations=None, media=None,
                           retiredPublicBodyLocationsMediaUnavailable=True)
            result['members'].append(row)
        # Confirm the same thread generation after the per-member GETs.
        _, fresh = call('owned-thread-final-same-generation', 'GET', '/v1/me/moment-threads/' + THREAD)
        require(all(fresh.get(k) == owned.get(k) for k in ('version', 'status', 'members', 'retiredMembers')),
                'THREAD_CHANGED_DURING_READBACK')
        if args.expect_state == 'published':
            require(owned['status'] == 'published' and active_ids, 'EXPECTED_PUBLISHED_THREAD')
        if args.expect_state == 'withdrawn':
            require(owned['status'] == 'withdrawn' and not active_ids
                    and all(v['status'] == 'withdrawn' for v in result['members']),
                    'EXPECTED_WHOLE_WITHDRAWN_THREAD')
        if args.expect_mixed_three:
            require(len(active_ids) == 3 and not retired, 'EXPECTED_THREE_ACTIVE_NO_RETIRED')
            ordered = sorted(result['members'], key=lambda v: v['displayPosition'])
            require(ordered[0]['media']['count'] == 1 and ordered[0]['media']['items'][0]['kind'] == 'VIDEO',
                    'FIRST_MEMBER_NOT_SINGLE_VIDEO')
            require(ordered[1]['momentId'] == ROOT_MOMENT and ordered[1]['media']['count'] == 1
                    and ordered[1]['media']['items'][0]['kind'] == 'IMAGE' and ordered[1]['locations'] == []
                    and ordered[1]['body']['utf8Bytes'] == 0, 'SECOND_ORIGINAL_IMAGE_MISMATCH')
            require(ordered[2]['body']['utf8Bytes'] > 0 and ordered[2]['media']['count'] == 0,
                    'THIRD_MEMBER_NOT_TEXT')
            for row, city, name in ((ordered[0], 'reykjavik-is', 'reykjav'),
                                    (ordered[2], 'london-gb', 'london')):
                require(len(row['locations']) == 1 and
                        (row['locations'][0].get('cityId') == city or
                         name in (row['locations'][0].get('referenceName', '') +
                                  row['locations'][0].get('cityName', '')).lower()),
                        'MIXED_MEMBER_CITY_MISMATCH')
            result['exactMixedThreeVerified'] = True
        result['status'] = 'PASS_METADATA_READBACK'
    except Exception as error:
        result['failureType'] = type(error).__name__
        result['failureCode'] = str(error) if isinstance(error, RuntimeError) and re.fullmatch(r'[A-Z0-9_]+', str(error)) else 'CONTROLLED_READBACK_FAILURE'
    finally:
        if token is not None:
            try:
                call('logout-only-helper-session', 'POST', '/v1/session/logout', expected=(204,))
                result['ownSessionLogout204'] = True
                call('rejected-revoked-helper-session', 'GET', '/v1/me', expected=(401,))
                result['ownSessionRejected401'] = True
            except Exception:
                result.update(status='FAIL', cleanupFailure='OWN_SESSION_REVOCATION_UNCONFIRMED')
        token = None
        result.update(completed=stamp(), events=events,
                      helperSHA256=hashlib.sha256(Path(__file__).read_bytes()).hexdigest())
        (out / 'receipt.json').write_text(json.dumps(result, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
        print(json.dumps(result, ensure_ascii=False))
    return 0 if result['status'] == 'PASS_METADATA_READBACK' and result['ownSessionLogout204'] and result['ownSessionRejected401'] else 1


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--execute', action='store_true')
    parser.add_argument('--name', default='device19-thread-readback01')
    parser.add_argument('--expect-state', choices=('observe', 'published', 'withdrawn'), default='observe')
    parser.add_argument('--expect-mixed-three', action='store_true')
    args = parser.parse_args()
    if not args.execute:
        print(json.dumps({'status': 'PLAN_NOT_EXECUTED', 'rootMomentId': ROOT_MOMENT, 'threadId': THREAD,
                          'onlySyntheticQA03Session': True, 'contentWrites': 0, 'previews': 0,
                          'mediaBytes': 0, 'otherQALogin': False, 'logoutOwnSession': True}))
    else:
        raise SystemExit(execute(args))
