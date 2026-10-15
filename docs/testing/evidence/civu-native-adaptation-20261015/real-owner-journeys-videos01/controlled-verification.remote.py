# Appended to the previously approved remote-login helper prelude by the local
# wrapper. This file defines no transport, credential, new owner or write query.
# It deliberately emits aggregate counts and booleans only.
import datetime


def instant(value):
    require(isinstance(value, str), 'TIMESTAMP_INVALID')
    return datetime.datetime.fromisoformat(value.replace('Z', '+00:00'))


def check_journey(actual, expected):
    require(isinstance(actual, dict), 'JOURNEY_RESPONSE_INVALID')
    for key in ('id', 'ownerAccountId', 'cityId', 'title', 'summary',
                'visibility', 'status', 'revision'):
        require(actual.get(key) == expected[key], 'JOURNEY_HEADER_MISMATCH')
    require(actual.get('entries') == [], 'JOURNEY_COMPATIBILITY_NOT_EMPTY')
    require(actual.get('privatePlaceCopyRestricted', False) is False,
            'JOURNEY_COPY_AUTHORITY_CHANGED')
    for key in ('createdAt', 'updatedAt'):
        require(instant(actual.get(key)) == instant(expected[key]),
                'JOURNEY_TIMESTAMP_MISMATCH')
    require(actual.get('journeyDetails') == expected['journeyDetails'],
            'JOURNEY_ITINERARY_OR_ORIGIN_MISMATCH')
    details = actual['journeyDetails']
    require(details['schemaVersion'] == 'birdtie.journey-details.v1' and
            re.fullmatch('[0-9a-f]{64}', details.get('sourceVersion', '')) is not None,
            'JOURNEY_SOURCE_VERSION_MISSING')
    entries = details['itinerary']
    require([entry['position'] for entry in entries] == list(range(1, len(entries) + 1)),
            'JOURNEY_ORDER_CHANGED')
    by_id = {entry['id']: entry for entry in entries}
    require(len(by_id) == len(entries), 'JOURNEY_DUPLICATE_ENTRY')
    for entry in entries:
        for field in ('stopId', 'fromStopId', 'toStopId'):
            link = entry[field]
            require(not link or link in by_id and by_id[link]['entryType'] == 'STOP',
                    'JOURNEY_STOP_RELATION_CHANGED')


def check_video_receipt(actual, expected):
    require(isinstance(actual, dict), 'VIDEO_RECEIPT_INVALID')
    for key in ('schemaVersion', 'id', 'momentId', 'ownerAccountId', 'origin',
                'status', 'revision', 'position', 'mimeType', 'inputByteSize',
                'byteSize', 'sha256', 'width', 'height', 'durationMs', 'frameCount',
                'codecAudio', 'posterBytes', 'posterSha256', 'validationKind',
                'antivirusScan', 'privacyRisk'):
        require(actual.get(key) == expected[key], 'VIDEO_RECEIPT_MISMATCH')
    require(instant(actual.get('retainUntil')) == instant(expected['retainUntil']),
            'VIDEO_RETENTION_MISMATCH')


def binary_digest(path, token, expected_size, expected_sha, mime, limit):
    # Byte chunks remain transient process memory. No video, poster, URL,
    # authorization value, content digest or body is printed or saved.
    require(0 < expected_size <= limit, 'BINARY_SIZE_INVALID')
    req = urllib.request.Request(BASE + path,
                                 headers={'Authorization': 'Bearer ' + token},
                                 method='GET')
    total = 0
    digest = hashlib.sha256()
    try:
        with urllib.request.urlopen(req, timeout=40) as response:
            require(response.status == 200 and response.headers.get('Content-Type') == mime,
                    'BINARY_HTTP_CONTRACT_MISMATCH')
            require('no-store' in response.headers.get('Cache-Control', '').lower() and
                    response.headers.get('X-Content-Type-Options') == 'nosniff',
                    'BINARY_PRIVATE_CACHE_CONTRACT_MISMATCH')
            declared = response.headers.get('Content-Length')
            require(declared is not None and declared.isdecimal() and
                    int(declared) == expected_size, 'BINARY_LENGTH_MISMATCH')
            while True:
                chunk = response.read(min(1024 * 1024, limit - total + 1))
                if not chunk:
                    break
                total += len(chunk)
                require(total <= limit, 'BINARY_RESPONSE_TOO_LARGE')
                digest.update(chunk)
                del chunk
    except urllib.error.HTTPError:
        raise RuntimeError('BINARY_HTTP_FAILED') from None
    require(total == expected_size and digest.hexdigest() == expected_sha,
            'BINARY_SHA_MISMATCH')
    return total


owner = None
before_state = None
input_hashes = None
result.update(positiveOriginalVideoHTTP='NOT_RUN', originalVideoBytesHTTP='NOT_AVAILABLE_BY_API',
              selectedOwnerJourneyRead='NOT_RUN', ownerEnrollmentUnchanged=False,
              originalInputUnchanged=False, newOwnerEnrolled=False,
              newAccountActivated=False, defaultTLSValidated=False,
              visualVerification='NOT_RUN')
try:
    stage = 'protected_input'
    require(BASE == 'https://47.110.154.133', 'EXACT_PUBLIC_TEST_ORIGIN_REQUIRED')
    source_raw, manifest_raw = SOURCE.read_bytes(), MANIFEST.read_bytes()
    input_hashes = (hashlib.sha256(source_raw).hexdigest(),
                    hashlib.sha256(manifest_raw).hexdigest())
    source, manifest = json.loads(source_raw), json.loads(manifest_raw)
    require(manifest['sourceFileSha256'] == input_hashes[0], 'SOURCE_HASH_MISMATCH')

    stage = 'native_target_and_contract'
    target = query("""SELECT jsonb_build_object(
      'guard',current_database()='birdtie_native_hz_20261015' AND
        (SELECT oid=54087 AND pg_get_userbyid(datdba)='birdtie_native_hz'
         FROM pg_database WHERE datname=current_database()),
      'schema',(SELECT max(version) FROM birdtie_schema_migrations),
      'journeyContract',birdtie_journey_details_contract(),
      'videoContract',birdtie_legacy_private_video_contract(),
      'bindings',(SELECT coalesce(jsonb_agg(jsonb_build_object(
        'owner',owner_account_id,'source',source_user_id,
        'phoneDigest',encode(phone_digest,'hex'))),'[]'::jsonb)
        FROM legacy_dev_phone_bindings WHERE first_enrolled_at IS NOT NULL),
      'agentsPaused',(SELECT bool_and(a.status='suspended') FROM agents a
        JOIN legacy_entity_mappings m ON m.entity_kind='user' AND
        m.target_id=a.principal_account_id))""")
    require(target['guard'] is True and target['schema'] == 159 and
            target['journeyContract'] == 'journey-native-details-v1' and
            target['videoContract'] == 'private-moment-video-v1', 'TARGET_GUARD_FAILED')
    require(target['agentsPaused'] is True, 'MIGRATION_AGENT_ACTIVATION_DETECTED')
    require(len(target['bindings']) == 1, 'EXACT_EXISTING_OWNER_REQUIRED')
    binding = target['bindings'][0]
    owner = binding['owner']
    require(re.fullmatch(r'[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}', owner) is not None,
            'OWNER_ID_INVALID')

    stage = 'journey_database_facts'
    journey_facts = query("""SELECT jsonb_build_object(
      'runCounts',(SELECT counts FROM legacy_journey_adaptation_runs WHERE run_id='%s'),
      'adaptations',(SELECT count(*) FROM legacy_journey_adaptations WHERE run_id='%s'),
      'entries',(SELECT count(*) FROM journey_itinerary_entries e JOIN
        legacy_journey_adaptations m ON m.target_id=e.journey_id WHERE m.run_id='%s'),
      'origins',(SELECT count(*) FROM legacy_journey_entry_origins WHERE run_id='%s'),
      'tombstones',(SELECT count(*) FROM legacy_personal_archives WHERE run_id='%s'
        AND kind='journey' AND deleted),
      'sourceLive',(SELECT count(*) FROM legacy_personal_archives WHERE run_id='%s'
        AND kind='journey' AND NOT deleted),
      'headersBound',NOT EXISTS(SELECT 1 FROM legacy_journey_adaptations m
        LEFT JOIN journeys j ON j.id=m.target_id
        LEFT JOIN legacy_personal_archives ar ON ar.run_id=m.run_id AND ar.kind='journey'
          AND ar.source_key=m.source_key AND ar.source_sha256=m.source_sha256
          AND ar.owner_account_id=m.owner_account_id AND NOT ar.deleted
        LEFT JOIN legacy_entity_mappings u ON u.entity_kind='user' AND
          u.source_system=ar.source_system AND u.source_id=ar.source_owner_id AND
          u.run_id=ar.run_id AND u.target_id=ar.owner_account_id AND
          u.owner_account_id=ar.owner_account_id
        LEFT JOIN accounts a ON a.id=j.owner_account_id
        LEFT JOIN legacy_native_catalog_aliases jc ON jc.run_id=ar.run_id AND jc.kind='city'
          AND jc.source_id::text=ar.payload->'metadata'->>'sourceCityId'
        WHERE m.run_id='%s' AND(j.id IS NULL OR ar.id IS NULL OR u.target_id IS NULL
          OR a.account_type IS DISTINCT FROM 'person' OR
          a.status NOT IN('active','suspended') OR j.owner_account_id<>m.owner_account_id
          OR j.visibility<>'private' OR j.status<>'draft'
          OR j.detail_schema IS DISTINCT FROM 'birdtie.journey-details.v1'
          OR j.title<>ar.title OR coalesce(j.city_id,'') IS DISTINCT FROM coalesce(jc.target_city_id,'')
          OR coalesce(j.journey_start_date::text,'') IS DISTINCT FROM
            coalesce(ar.payload->'metadata'->>'sourceStartDate','')
          OR j.journey_end_date IS NOT NULL
          OR j.journey_days IS DISTINCT FROM
            coalesce(NULLIF(ar.payload->'metadata'->>'sourceDays','')::integer,0)
          OR j.journey_date_label IS DISTINCT FROM
            coalesce(ar.payload->'metadata'->>'sourceDateLabel',''))),
      'singleCanonicalSchedule',NOT EXISTS(SELECT 1 FROM journey_stops s JOIN
        legacy_journey_adaptations m ON m.target_id=s.journey_id WHERE m.run_id='%s'),
      'originsBound',NOT EXISTS(SELECT 1 FROM legacy_journey_entry_origins o
        LEFT JOIN legacy_journey_adaptations m ON m.run_id=o.run_id AND
          m.source_key=o.source_key AND m.target_id=o.journey_id
        LEFT JOIN legacy_personal_archives ar ON ar.run_id=m.run_id AND
          ar.kind='journey' AND ar.source_key=m.source_key AND
          ar.source_sha256=m.source_sha256 AND ar.owner_account_id=m.owner_account_id
          AND NOT ar.deleted
        LEFT JOIN journey_itinerary_entries e ON e.journey_id=o.journey_id AND e.entry_id=o.entry_id
        LEFT JOIN legacy_native_adaptation_entities pm ON pm.run_id=o.run_id AND pm.kind='place'
          AND pm.source_key=o.origin->>'sourcePlaceId'
        LEFT JOIN legacy_native_catalog_aliases ca ON ca.run_id=o.run_id AND ca.kind='city'
          AND ca.source_id::text=o.origin->>'sourceCityId'
        WHERE o.run_id='%s' AND(m.target_id IS NULL OR ar.id IS NULL OR e.entry_id IS NULL
          OR e.place_id IS DISTINCT FROM o.original_place_id OR
          (coalesce(o.origin->>'sourcePlaceId','')<>'' AND
            (pm.target_id IS NULL OR e.place_id::text IS DISTINCT FROM pm.target_id)) OR
          (coalesce(o.origin->>'sourcePlaceId','')='' AND e.place_id IS NOT NULL) OR
          (coalesce(o.origin->>'sourceCityId','')<>'' AND
            (ca.target_city_id IS NULL OR e.city_id IS DISTINCT FROM ca.target_city_id)) OR
          (coalesce(o.origin->>'sourceCityId','')='' AND e.city_id IS NOT NULL) OR
          e.entry_type IS DISTINCT FROM CASE o.origin->>'sourceType'
            WHEN 'stops' THEN 'STOP' WHEN 'arrangements' THEN 'ARRANGEMENT' WHEN 'notes' THEN 'NOTE' END OR
          e.material->>'id' IS DISTINCT FROM e.entry_id::text OR
          (e.material->>'position')::integer IS DISTINCT FROM e.sequence_no OR
          NOT EXISTS(SELECT 1 FROM jsonb_array_elements(ar.payload->(o.origin->>'sourceType')) src
            WHERE src->>'sourceId'=o.origin->>'sourceId' AND
            coalesce(NULLIF(src->>'position','')::bigint,0)=
              coalesce(NULLIF(o.origin->>'sourcePosition','')::bigint,0) AND
            coalesce(src->>'sourcePlaceId','')=coalesce(o.origin->>'sourcePlaceId','') AND
            coalesce(src->>'sourceCityId','')=coalesce(o.origin->>'sourceCityId','') AND
            coalesce(src->>'sourceDestinationId','')=coalesce(o.origin->>'sourceDestinationId','') AND
            coalesce(src->>'sourceStopId','')=coalesce(o.origin->>'sourceStopId','') AND
            coalesce(src->>'sourceFromStopId','')=coalesce(o.origin->>'sourceFromStopId','') AND
            coalesce(src->>'sourceToStopId','')=coalesce(o.origin->>'sourceToStopId','')))),
      'stopLinksBound',NOT EXISTS(SELECT 1 FROM journey_itinerary_entries e JOIN
        legacy_journey_adaptations m ON m.target_id=e.journey_id
        LEFT JOIN legacy_journey_entry_origins o ON o.journey_id=e.journey_id AND o.entry_id=e.entry_id
        WHERE m.run_id='%s' AND EXISTS(SELECT 1 FROM
          (VALUES('sourceStopId',e.stop_id),('sourceFromStopId',e.from_stop_id),
                 ('sourceToStopId',e.to_stop_id)) link(source_key,target_id)
          WHERE (coalesce(o.origin->>link.source_key,'')='' AND link.target_id IS NOT NULL)
          OR (coalesce(o.origin->>link.source_key,'')<>'' AND NOT EXISTS(
            SELECT 1 FROM journey_itinerary_entries stop JOIN legacy_journey_entry_origins so
              ON so.journey_id=stop.journey_id AND so.entry_id=stop.entry_id
            WHERE stop.journey_id=e.journey_id AND stop.entry_id=link.target_id AND
              stop.entry_type='STOP' AND so.origin->>'sourceId'=o.origin->>link.source_key))))
      )""" % ((RUN,) * 10))
    expected_counts = dict(journeys=22, entries=110, stops=24, arrangements=85,
                           notes=1, tombstones=12, unmappedPlaces=0, unmappedCities=0)
    require(journey_facts['runCounts'] == expected_counts and
            journey_facts['adaptations'] == 22 and journey_facts['entries'] == 110 and
            journey_facts['origins'] == 110 and journey_facts['tombstones'] == 12 and
            journey_facts['sourceLive'] == 22, 'JOURNEY_COUNTS_MISMATCH')
    require(all(journey_facts[key] is True for key in
                ('headersBound', 'singleCanonicalSchedule', 'originsBound', 'stopLinksBound')),
            'JOURNEY_OWNER_OR_PROVENANCE_MISMATCH')

    stage = 'video_database_facts'
    video_facts = query("""SELECT jsonb_build_object(
      'videos',(SELECT count(*) FROM private_moment_videos WHERE run_id='%s'),
      'items',(SELECT count(*) FROM legacy_private_video_import_items WHERE run_id='%s'),
      'runBound',EXISTS(SELECT 1 FROM legacy_private_video_import_runs r WHERE run_id='%s'
        AND protocol='birdtie-civu-private-videos-v1' AND source_file_sha256='%s'
        AND manifest_file_sha256='%s' AND matcher_file_sha256~'^[0-9a-f]{64}$'
        AND authenticated_seal_sha256~'^[0-9a-f]{64}$' AND counts=(SELECT
          jsonb_build_object('videos',count(*),'notes',count(DISTINCT source_note_id),
            'owners',count(DISTINCT owner_account_id)) FROM legacy_private_video_import_items
          WHERE run_id=r.run_id)),
      'bytesAndValidationBound',NOT EXISTS(SELECT 1 FROM private_moment_videos v
        LEFT JOIN legacy_private_video_import_items i ON i.video_id=v.id AND i.run_id=v.run_id
        LEFT JOIN legacy_native_adaptation_entities x ON x.run_id=i.run_id AND x.kind='moment'
          AND x.source_key=i.source_note_id::text AND x.target_id=i.moment_id::text AND
          x.owner_account_id=i.owner_account_id
        LEFT JOIN legacy_note_archives n ON n.source_note_id=i.source_note_id AND n.run_id=x.run_id
          AND n.owner_account_id=x.owner_account_id AND n.source_sha256=x.source_sha256
          AND n.source_sha256=i.source_note_sha256 AND NOT n.deleted
        LEFT JOIN legacy_entity_mappings u ON u.run_id=n.run_id AND u.source_system=n.source_system
          AND u.entity_kind='user' AND u.target_id=n.owner_account_id AND u.owner_account_id=n.owner_account_id
        LEFT JOIN moments m ON m.id=v.moment_id AND m.author_account_id=v.owner_account_id
        LEFT JOIN accounts a ON a.id=m.author_account_id
        LEFT JOIN media_assets asset ON asset.id=v.id AND asset.owner_account_id=v.owner_account_id
        WHERE v.run_id='%s' AND(i.video_id IS NULL OR x.target_id IS NULL OR n.id IS NULL
          OR u.target_id IS NULL OR m.id IS NULL OR a.account_type IS DISTINCT FROM 'person'
          OR a.status NOT IN('active','suspended') OR m.visibility<>'private' OR m.status<>'draft'
          OR v.owner_account_id<>i.owner_account_id OR v.moment_id<>i.moment_id
          OR v.source_position<>i.source_position OR v.input_sha256<>i.source_input_sha256
          OR v.input_size<>i.source_input_bytes OR v.content_sha256<>i.content_sha256
          OR v.content_size<>i.content_bytes OR v.poster_sha256<>i.poster_sha256
          OR v.poster_size<>i.poster_bytes OR v.status<>'ready_private'
          OR v.original IS NULL OR v.content IS NULL OR v.poster IS NULL
          OR octet_length(v.original)<>v.input_size OR encode(sha256(v.original),'hex')<>v.input_sha256
          OR octet_length(v.content)<>v.content_size OR encode(sha256(v.content),'hex')<>v.content_sha256
          OR octet_length(v.poster)<>v.poster_size OR encode(sha256(v.poster),'hex')<>v.poster_sha256
          OR v.width NOT BETWEEN 2 AND 1920 OR v.height NOT BETWEEN 2 AND 1920
          OR v.width::bigint*v.height>2073600 OR v.duration_ms NOT BETWEEN 1 AND 120000
          OR v.frame_count NOT BETWEEN 1 AND 7200 OR v.audio_packet_count NOT BETWEEN 0 AND 10000
          OR NOT((v.audio_codec='none' AND v.audio_packet_count=0 AND v.audio_packet_sha256='') OR
            (v.audio_codec='aac' AND v.audio_packet_count>0 AND v.audio_packet_sha256~'^[0-9a-f]{64}$'))
          OR v.validation_kind<>'FULL_VIDEO_DECODE_CANONICAL_REENCODE_V1'
          OR v.antivirus_scan<>'NOT_RUN' OR v.privacy_risk<>'UNKNOWN' OR v.retain_until<=clock_timestamp()
          OR asset.id IS NULL OR asset.state<>'ready_private' OR asset.mime_type<>'video/mp4'
          OR asset.sha256_hex<>v.content_sha256 OR asset.byte_size<>v.content_size
          OR NOT EXISTS(SELECT 1 FROM jsonb_array_elements(n.source_relationships->'media') rel
            WHERE rel->>'sourceMediaId'=i.source_media_id::text AND
              rel->>'position'=i.source_position::text)))
      )""" % (RUN, RUN, RUN, input_hashes[0], input_hashes[1], RUN))
    require(video_facts['videos'] == 5 and video_facts['items'] == 5 and
            video_facts['runBound'] is True and video_facts['bytesAndValidationBound'] is True,
            'VIDEO_DATABASE_BYTES_OR_PROVENANCE_MISMATCH')
    result.update(schema159ContractsMatched=True, nativeJourneys=22, nativeItineraryEntries=110,
                  journeyAdaptations=22, journeyTombstones=12, privateVideos=5,
                  allJourneyOwnerAndOriginBindingsMatched=True,
                  allVideoDatabaseOriginalContentPosterSHAMatched=True,
                  allVideoValidationFieldsMatched=True,
                  antivirusScanRemainsNotRun=True, privacyRiskRemainsUnknown=True)

    stage = 'existing_owner_identity'
    identities = [item for item in source['tables']['auth_identities']
                  if item['provider'] == 'phone' and item['user_id'] == binding['source']]
    require(len(identities) == 1, 'EXACT_EXISTING_PHONE_IDENTITY_REQUIRED')
    phone = normalize(identities[0]['subject'])
    require(hashlib.sha256(('birdtie-dev-phone:' + phone).encode()).hexdigest() ==
            binding['phoneDigest'], 'EXISTING_PHONE_BINDING_MISMATCH')
    before_state = state(owner)
    require(before_state['enrolledOwners'] == 1 and before_state['selectedEnrolled'] is True and
            before_state['selectedAccountActive'] is True and
            before_state['selectedProfilePrivate'] is True and
            before_state['selectedAgentsPaused'] is True and
            before_state['allMappingsUnclaimed'] is True, 'EXISTING_OWNER_STATE_MISMATCH')
    stage = 'fixed_login'
    token = login(phone)
    del phone
    require(data('GET', '/v1/me', token)['id'] == owner, 'FIXED_LOGIN_OWNER_MISMATCH')

    stage = 'owned_journey_database_projection'
    expected_journeys = query("""SELECT coalesce(jsonb_agg(jsonb_build_object(
      'id',j.id,'ownerAccountId',j.owner_account_id,'cityId',coalesce(j.city_id,''),
      'title',j.title,'summary',j.summary,'visibility',j.visibility,'status',j.status,
      'revision',j.revision,'createdAt',j.created_at,'updatedAt',j.updated_at,
      'journeyDetails',jsonb_build_object('schemaVersion',j.detail_schema,
        'startDate',coalesce(j.journey_start_date::text,''),
        'endDate',coalesce(j.journey_end_date::text,''),'dateLabel',j.journey_date_label,
        'days',j.journey_days,'itinerary',(SELECT coalesce(jsonb_agg(e.material||
          CASE WHEN o.origin IS NULL THEN '{}'::jsonb ELSE jsonb_build_object('origin',o.origin) END
          ORDER BY e.sequence_no),'[]'::jsonb) FROM journey_itinerary_entries e
          LEFT JOIN legacy_journey_entry_origins o ON o.journey_id=e.journey_id AND o.entry_id=e.entry_id
          WHERE e.journey_id=j.id),
        'sourceVersion',encode(sha256(convert_to(jsonb_build_array(j.xmin::text,
          (SELECT coalesce(jsonb_agg(jsonb_build_array(e.xmin::text,e.entry_id,e.material,o.xmin::text)
            ORDER BY e.sequence_no),'[]'::jsonb) FROM journey_itinerary_entries e
            LEFT JOIN legacy_journey_entry_origins o ON o.journey_id=e.journey_id AND o.entry_id=e.entry_id
            WHERE e.journey_id=j.id))::text,'UTF8')),'hex')))
      ORDER BY j.id),'[]'::jsonb) FROM journeys j JOIN legacy_journey_adaptations m
        ON m.target_id=j.id AND m.owner_account_id=j.owner_account_id
      WHERE m.run_id='%s' AND j.owner_account_id='%s'""" % (RUN, owner))
    require(bool(expected_journeys), 'EXISTING_OWNER_NATIVE_JOURNEYS_MISSING')
    stage = 'owned_journey_paging_and_details'
    require(call('GET', '/v1/me/personal-collections?kind=journey')[0] == 401,
            'ANONYMOUS_JOURNEY_LIST_NOT_REJECTED')
    journey_items, journey_pages = page_all('/v1/me/personal-collections?kind=journey', token)
    require(all(item.get('ownerAccountId') == owner for item in journey_items),
            'JOURNEY_LIST_CONTAINS_FOREIGN_OWNER')
    actual_journeys = {item['id']: item for item in journey_items}
    for expected in expected_journeys:
        require(expected['id'] in actual_journeys, 'OWNED_NATIVE_JOURNEY_OMITTED')
        check_journey(actual_journeys[expected['id']], expected)
        path = '/v1/me/personal-collections/journey/' + expected['id']
        check_journey(data('GET', path, token), expected)
        require(call('GET', path)[0] == 401, 'ANONYMOUS_JOURNEY_DETAIL_NOT_REJECTED')
    result.update(selectedOwnerJourneyRead='PASS' if expected_journeys else 'NOT_RUN',
                  selectedOwnerNativeJourneys=len(expected_journeys), journeyPages=journey_pages,
                  selectedOwnerItineraryEntries=sum(len(j['journeyDetails']['itinerary'])
                                                    for j in expected_journeys),
                  allSelectedJourneyFieldsAndOriginsMatched=bool(expected_journeys),
                  anonymousJourneyReadRejected=True)

    stage = 'owned_video_projection'
    expected_videos = query("""SELECT coalesce(jsonb_agg(jsonb_build_object(
      'schemaVersion','private-moment-video-v1','id',v.id,'momentId',v.moment_id,
      'ownerAccountId',v.owner_account_id,'origin','LEGACY_PRIVATE_IMPORT',
      'status',v.status,'revision',v.revision,'position',v.source_position,'mimeType','video/mp4',
      'inputByteSize',v.input_size,'byteSize',v.content_size,'sha256',v.content_sha256,
      'width',v.width,'height',v.height,'durationMs',v.duration_ms,'frameCount',v.frame_count,
      'codecAudio',v.audio_codec,'posterBytes',v.poster_size,'posterSha256',v.poster_sha256,
      'validationKind',v.validation_kind,'antivirusScan',v.antivirus_scan,
      'privacyRisk',v.privacy_risk,'retainUntil',v.retain_until)
      ORDER BY v.source_position,v.id),'[]'::jsonb) FROM private_moment_videos v
      WHERE v.run_id='%s' AND v.owner_account_id='%s'""" % (RUN, owner))
    checked_videos = 0
    binary_bytes = 0
    stage = 'owned_video_http'
    for moment_id in {item['momentId'] for item in expected_videos}:
        base = '/v1/me/moments/' + moment_id + '/videos'
        require(call('GET', base)[0] == 401, 'ANONYMOUS_VIDEO_LIST_NOT_REJECTED')
        listed = data('GET', base, token)
        require(isinstance(listed, dict) and isinstance(listed.get('items'), list),
                'VIDEO_LIST_CONTRACT_INVALID')
        actual_videos = {item['id']: item for item in listed['items']}
        for expected in [item for item in expected_videos if item['momentId'] == moment_id]:
            require(expected['id'] in actual_videos, 'OWNED_VIDEO_OMITTED')
            check_video_receipt(actual_videos[expected['id']], expected)
            path = base + '/' + expected['id']
            check_video_receipt(data('GET', path, token), expected)
            for suffix in ('', '/content', '/poster'):
                require(call('GET', path + suffix)[0] == 401,
                        'ANONYMOUS_VIDEO_READ_NOT_REJECTED')
            binary_bytes += binary_digest(path + '/content', token, expected['byteSize'],
                                          expected['sha256'], 'video/mp4', 64 * 1024 * 1024)
            binary_bytes += binary_digest(path + '/poster', token, expected['posterBytes'],
                                          expected['posterSha256'], 'image/jpeg', 5 * 1024 * 1024)
            checked_videos += 1
    if not expected_videos:
        require(call('GET', '/v1/me/moments/00000000-0000-0000-0000-000000000001/videos')[0] == 401,
                'ANONYMOUS_VIDEO_LIST_NOT_REJECTED')
    result.update(positiveOriginalVideoHTTP='PASS' if checked_videos else 'NOT_RUN',
                  selectedOwnerPrivateVideos=checked_videos,
                  selectedOwnerVideoHTTPBytes=binary_bytes,
                  allSelectedVideoReceiptAndPlaybackPosterSHAMatched=bool(checked_videos),
                  anonymousVideoReadRejected=True,
                  differentOwnerVideoHTTP='NOT_RUN_NO_OWNER_IMPERSONATION',
                  originalOwnerMatched=True, defaultTLSValidated=True)
    result['status'] = 'PASS'
except Exception as error:
    result.update(status='FAIL', failureStage=stage, failureType=type(error).__name__)
    if isinstance(error, RuntimeError) and re.fullmatch('[A-Z_]+', str(error)):
        result['failureCode'] = str(error)
finally:
    stage = 'cleanup'
    logged_out, me_rejected = [], []
    for created_token in tokens:
        try:
            logged_out.append(call('POST', '/v1/session/logout', created_token)[0] == 204)
            me_rejected.append(call('GET', '/v1/me', created_token)[0] == 401)
        except Exception:
            logged_out.append(False)
            me_rejected.append(False)
    result['allCreatedSessionsLoggedOut'] = bool(tokens) and all(logged_out) and all(me_rejected)
    result['allCreatedSessionsMeRejected'] = bool(tokens) and all(me_rejected)
    try:
        result['originalInputUnchanged'] = input_hashes is not None and input_hashes == (
            hashlib.sha256(SOURCE.read_bytes()).hexdigest(),
            hashlib.sha256(MANIFEST.read_bytes()).hexdigest())
    except Exception:
        result['originalInputUnchanged'] = False
    if owner is not None and before_state is not None:
        try:
            after_state = state(owner)
            result['ownerEnrollmentUnchanged'] = all(after_state[key] == before_state[key] for key in (
                'enrolledOwners', 'bindingOwners', 'importedActiveAccounts', 'importedActiveAgents',
                'firstFixedLoginAudits', 'selectedAccountActive', 'selectedProfilePrivate',
                'selectedAgents', 'selectedAgentsPaused', 'allMappingsUnclaimed', 'selectedEnrolled'))
            result['allImportedAgentsStillPaused'] = after_state['importedActiveAgents'] == 0
            result['selectedProfileStillPrivate'] = after_state['selectedProfilePrivate'] is True
        except Exception:
            result['ownerEnrollmentUnchanged'] = False
    if result.get('status') == 'PASS' and not all(result.get(key) is True for key in (
            'allCreatedSessionsLoggedOut', 'allCreatedSessionsMeRejected',
            'originalInputUnchanged', 'ownerEnrollmentUnchanged',
            'allImportedAgentsStillPaused', 'selectedProfileStillPrivate')):
        result.update(status='FAIL', failureStage='cleanup', failureCode='FINAL_BOUNDARY_CHECK_FAILED')
    print(json.dumps(result, sort_keys=True))
    raise SystemExit(0 if result.get('status') == 'PASS' else 1)
