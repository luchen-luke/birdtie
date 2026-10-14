"""Validate an explicitly selected, normalized export. Local preparation only.

No database client, HTTP client, apply mode, credential loading, SQL generation,
legacy repository scan, or migration execution belongs to this module.
"""
import argparse
import hashlib
import json
from pathlib import Path
import re
import uuid

SCHEMA = 'birdtie-civu-selected-collections-v1'
HEX = re.compile(r'^[0-9a-f]{64}$')
NAMESPACE = uuid.UUID('97c39a15-1a74-455c-b60e-7d76d5c32c3c')

class InvalidSelection(ValueError):
    pass

def closed(value, keys):
    if not isinstance(value, dict) or set(value) != set(keys):
        raise InvalidSelection('CLOSED_FIELDS')
    return value

def identifier(value):
    try:
        if not isinstance(value, str) or str(uuid.UUID(value)) != value:
            raise ValueError()
    except ValueError:
        raise InvalidSelection('INVALID_ID') from None
    return value

def text(value, limit, empty=True):
    if not isinstance(value, str):
        raise InvalidSelection('INVALID_TEXT')
    try:
        size = len(value.encode('utf-8'))
    except UnicodeError:
        raise InvalidSelection('INVALID_TEXT') from None
    if '\0' in value or size > limit or (not empty and not value.strip()):
        raise InvalidSelection('INVALID_TEXT')
    return value

def canonical(value):
    return json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(',', ':')).encode('utf-8')

def digest(value):
    return hashlib.sha256(canonical(value)).hexdigest()

def load_selected(path):
    raw = Path(path).read_bytes()
    if len(raw) > 2 * 1024 * 1024:
        raise InvalidSelection('INPUT_SIZE')
    def pairs(items):
        result = {}
        for key, value in items:
            if key in result:
                raise InvalidSelection('DUPLICATE_KEY')
            result[key] = value
        return result
    try:
        value = json.loads(raw.decode('utf-8'), object_pairs_hook=pairs,
                           parse_constant=lambda _: (_ for _ in ()).throw(InvalidSelection('NON_FINITE_JSON')))
    except (UnicodeError, json.JSONDecodeError):
        raise InvalidSelection('INVALID_JSON') from None
    return value, hashlib.sha256(raw).hexdigest()

def prepare(selected):
    closed(selected, ('schemaVersion', 'sourceInstanceTag', 'sourceBackupSha256',
                      'sourceSchemaSha256', 'accountMappings', 'placeMappings', 'collections'))
    if selected['schemaVersion'] != SCHEMA or not isinstance(selected['sourceInstanceTag'], str) or not re.fullmatch(r'[A-Za-z0-9_-]{1,80}', selected['sourceInstanceTag']):
        raise InvalidSelection('SOURCE_IDENTITY')
    for key in ('sourceBackupSha256', 'sourceSchemaSha256'):
        if not isinstance(selected[key], str) or not HEX.fullmatch(selected[key]):
            raise InvalidSelection('SOURCE_IDENTITY')
    accounts, places = selected['accountMappings'], selected['placeMappings']
    if not isinstance(accounts, dict) or not isinstance(places, dict) or len(accounts) > 1000 or len(places) > 5000:
        raise InvalidSelection('MAPPING_SIZE')
    for old, new in accounts.items():
        identifier(old); identifier(new)
    if len(set(accounts.values())) != len(accounts):
        raise InvalidSelection('ACCOUNT_MERGE_REQUIRES_REVIEW')
    for old, target in places.items():
        identifier(old); closed(target, ('id', 'cityId')); identifier(target['id']); text(target['cityId'], 100, False)
    rows = selected['collections']
    if not isinstance(rows, list) or len(rows) > 1000:
        raise InvalidSelection('SELECTION_SIZE')
    seen, plans, deferred = set(), [], []
    for row in rows:
        closed(row, ('kind', 'id', 'ownerId', 'revision', 'status', 'visibility',
                     'cityId', 'title', 'summary', 'entries'))
        if row['kind'] not in ('journey', 'theme_map'):
            raise InvalidSelection('UNSUPPORTED_ENTITY')
        identifier(row['id']); identifier(row['ownerId']); text(row['cityId'], 100, False)
        text(row['title'], 160, False); text(row['summary'], 5000)
        if type(row['revision']) is not int or not 1 <= row['revision'] <= 8999999999999999:
            raise InvalidSelection('SOURCE_REVISION')
        if row['status'] not in ('active', 'removed', 'archived') or row['visibility'] not in ('private', 'draft', 'public', 'team'):
            raise InvalidSelection('SOURCE_STATE')
        source_key = (row['kind'], row['id'])
        if source_key in seen:
            raise InvalidSelection('DUPLICATE_SOURCE')
        seen.add(source_key)
        entries = row['entries']
        if not isinstance(entries, list) or not 1 <= len(entries) <= 50:
            raise InvalidSelection('ENTRY_LIMIT')
        order, source_places, target_places, normalized = set(), set(), set(), []
        reasons = set()
        if row['status'] != 'active': reasons.add('SOURCE_NOT_ACTIVE')
        if row['ownerId'] not in accounts: reasons.add('OWNER_MAPPING_MISSING')
        for entry in entries:
            closed(entry, ('placeId', 'position'))
            identifier(entry['placeId'])
            if type(entry['position']) is not int or not 0 <= entry['position'] <= 1000000:
                raise InvalidSelection('ENTRY_ORDER')
            if entry['position'] in order or entry['placeId'] in source_places:
                raise InvalidSelection('DUPLICATE_ENTRY')
            order.add(entry['position']); source_places.add(entry['placeId'])
            target = places.get(entry['placeId'])
            if target is None: reasons.add('PLACE_MAPPING_MISSING'); continue
            if target['cityId'] != row['cityId']: reasons.add('CITY_MAPPING_MISMATCH')
            if target['id'] in target_places: reasons.add('PLACE_MERGE_REQUIRES_REVIEW')
            target_places.add(target['id'])
            normalized.append((entry['position'], {'placeId': target['id'], 'note': '', 'locationVisibility': 'hidden'}))
        selector = {'kind': row['kind'], 'sourceId': row['id'], 'sourceRevision': row['revision']}
        source_hash = digest(row)
        if reasons:
            deferred.append(dict(selector, sourceSha256=source_hash, reasons=sorted(reasons)))
            continue
        # Only a stable original-operation proposal. The existing domain writer
        # will assign the actual new collection ID after future authorization.
        operation = str(uuid.uuid5(NAMESPACE, json.dumps([selected['sourceInstanceTag'], *source_key], separators=(',', ':'))))
        candidate = {'operationId': operation, 'kind': row['kind'], 'cityId': row['cityId'],
                     'title': row['title'], 'summary': row['summary'],
                     'entries': [value for _, value in sorted(normalized)]}
        plans.append(dict(selector, sourceSha256=source_hash, proposedOwnerAccountId=accounts[row['ownerId']],
                          state='NEEDS_OWNER_AND_CURRENT_SOURCE_CONFIRMATION',
                          publicationState='private/draft', candidateInput=candidate,
                          candidateSha256=digest(candidate), targetCollectionId=None))
    plans.sort(key=lambda p: (p['kind'], p['sourceId']))
    deferred.sort(key=lambda p: (p['kind'], p['sourceId']))
    counts = {}
    for item in deferred:
        for reason in item['reasons']: counts[reason] = counts.get(reason, 0) + 1
    return {'schemaVersion': 'birdtie-civu-collection-preparation-v1',
            'sourceInstanceTag': selected['sourceInstanceTag'],
            'sourceBackupSha256': selected['sourceBackupSha256'], 'sourceSchemaSha256': selected['sourceSchemaSha256'],
            'selectionSha256': digest(selected), 'plans': plans, 'deferred': deferred,
            'summary': {'selected': len(rows), 'candidates': len(plans), 'deferred': len(deferred), 'reasons': counts},
            'execution': 'NOT_RUN', 'ownerRights': 'NOT_RUN', 'currentTargetReaders': 'NOT_RUN',
            'sourcePreservation': 'NOT_RUN', 'databaseChanges': 0, 'networkRequests': 0,
            'mediaMoved': 0, 'sessionsMigrated': 0, 'relationshipEffects': 0}

def main():
    parser = argparse.ArgumentParser(description='Prepare explicitly selected Civu collection candidates locally; no apply mode.')
    parser.add_argument('--selected', required=True)
    parser.add_argument('--output', required=True)
    args = parser.parse_args()
    out = Path(args.output)
    if out.exists(): parser.error('output already exists; preserve the previous preparation')
    try:
        selected, raw_hash = load_selected(args.selected)
        result = prepare(selected)
        result['inputFileSha256'] = raw_hash
    except InvalidSelection as error:
        print(json.dumps({'status': 'REJECTED', 'reason': str(error)}, sort_keys=True))
        return 2
    out.parent.mkdir(parents=True, exist_ok=True)
    with out.open('xb') as target: target.write(canonical(result) + b'\n')
    print(json.dumps({'status': 'PREPARED_ONLY', **result['summary'], 'outputSha256': hashlib.sha256(out.read_bytes()).hexdigest()}, sort_keys=True))
    return 0

if __name__ == '__main__':
    raise SystemExit(main())
