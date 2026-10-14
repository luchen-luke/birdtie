"""Preserve one verified isolated Civu restore; never import or authorize it.

The fixed schema was audited without row export. Actual row capture is an
explicit CLI action and goes only to a new private directory outside repositories.
No Civu business code, credentials, sessions, or media bytes are executed here.
"""
import argparse
import csv
import ctypes
from decimal import Decimal
import hashlib
import io
import json
import math
import os
from pathlib import Path
import re
import stat
import subprocess
import sys

CONTAINER = 'birdtie-civu-restore-20261014'
DATABASE = 'civu_restore'
INSTANCE_TAG = 'hangzhou-civu-20261014'
BACKUP_SHA256 = '5dbc323ae7f16df233dea5a412d30c6f563632150e1c639443c4a3997ec5838f'
SOURCE_VERSION = 'birdtie-civu-hangzhou-source-v1'
MANIFEST_VERSION = 'birdtie-civu-hangzhou-preservation-manifest-v1'
AUDIT_FILE_SHA256 = '77c8c987b64b1656c3d402f073861c015cfc2a669ab07b49b0e43519f92a9ec3'
SCHEMA_SHA256 = 'b4989ea80d771d454e61b93fb6b65b8692bde9e9a68c4a686cad57068801cd9c'
SCHEMA_MD5 = 'cb979e4fdf6411226917a765ba8b4068'
MAX_TABLE_ROWS = 100000
MAX_SOURCE_BYTES = 128 * 1024 * 1024
MAX_RELATION_BYTES = 128 * 1024 * 1024
HEX = re.compile(r'^[0-9a-f]{64}$')
UTC_TIME = re.compile(r'^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,6})?Z$')

# Replaced once with explicit columns/types/nullability/default-presence and PKs
# from read-only01; never discovered dynamically to widen the extraction scope.
TABLE_SPECS = {
    'auth_identities': {
        'primaryKey': ('id',),
        'columns': (
            ('id', 'uuid', 'NO', 'true'),
            ('user_id', 'uuid', 'NO', 'false'),
            ('provider', 'text', 'NO', 'false'),
            ('subject', 'text', 'NO', 'false'),
            ('verified_at', 'timestamptz', 'YES', 'false'),
            ('created_at', 'timestamptz', 'NO', 'true'),
        ),
    },
    'cities': {
        'primaryKey': ('id',),
        'columns': (
            ('id', 'uuid', 'NO', 'true'),
            ('name', 'text', 'NO', 'false'),
            ('name_norm', 'text', 'NO', 'false'),
            ('province', 'text', 'NO', 'false'),
            ('level', 'text', 'NO', 'true'),
            ('adcode', 'text', 'YES', 'false'),
            ('sort_weight', 'int4', 'NO', 'true'),
            ('created_at', 'timestamptz', 'NO', 'true'),
        ),
    },
    'geo_areas': {
        'primaryKey': ('id',),
        'columns': (
            ('id', 'uuid', 'NO', 'true'),
            ('parent_id', 'uuid', 'YES', 'false'),
            ('kind', 'text', 'NO', 'false'),
            ('stable_key', 'text', 'NO', 'false'),
            ('name_zh', 'text', 'NO', 'true'),
            ('name_en', 'text', 'NO', 'true'),
            ('name_local', 'text', 'NO', 'true'),
            ('aliases', '_text', 'NO', 'true'),
            ('country_code', 'text', 'NO', 'true'),
            ('admin_code', 'text', 'NO', 'true'),
            ('center_lat', 'float8', 'NO', 'true'),
            ('center_lng', 'float8', 'NO', 'true'),
            ('bbox', 'jsonb', 'YES', 'false'),
            ('status', 'text', 'NO', 'true'),
            ('legacy_region_id', 'uuid', 'YES', 'false'),
            ('created_at', 'timestamptz', 'NO', 'true'),
            ('updated_at', 'timestamptz', 'NO', 'true'),
            ('canonical_id', 'uuid', 'YES', 'false'),
            ('travel_profile', 'jsonb', 'NO', 'true'),
            ('artwork_media_id', 'uuid', 'YES', 'false'),
            ('artwork_url', 'text', 'NO', 'true'),
            ('artwork_version', 'text', 'NO', 'true'),
            ('artwork_updated_at', 'timestamptz', 'YES', 'false'),
        ),
    },
    'journey_arrangements': {
        'primaryKey': ('id',),
        'columns': (
            ('id', 'uuid', 'NO', 'true'),
            ('journey_id', 'uuid', 'NO', 'false'),
            ('type', 'text', 'NO', 'true'),
            ('place_id', 'uuid', 'YES', 'false'),
            ('name', 'text', 'NO', 'true'),
            ('day_index', 'int4', 'YES', 'false'),
            ('position', 'int4', 'NO', 'true'),
            ('time_label', 'text', 'NO', 'true'),
            ('private_note', 'text', 'NO', 'true'),
            ('created_at', 'timestamptz', 'NO', 'true'),
            ('updated_at', 'timestamptz', 'NO', 'true'),
            ('start_date', 'date', 'YES', 'false'),
            ('start_time', 'text', 'NO', 'true'),
            ('end_date', 'date', 'YES', 'false'),
            ('end_time', 'text', 'NO', 'true'),
            ('transport_mode', 'text', 'NO', 'true'),
            ('service_number', 'text', 'NO', 'true'),
            ('departure_name', 'text', 'NO', 'true'),
            ('arrival_name', 'text', 'NO', 'true'),
            ('stop_id', 'uuid', 'YES', 'false'),
            ('from_stop_id', 'uuid', 'YES', 'false'),
            ('to_stop_id', 'uuid', 'YES', 'false'),
            ('custom_address', 'text', 'NO', 'true'),
            ('custom_lat', 'float8', 'YES', 'false'),
            ('custom_lng', 'float8', 'YES', 'false'),
            ('source_text', 'text', 'NO', 'true'),
        ),
    },
    'journey_media': {
        'primaryKey': ('id',),
        'columns': (
            ('id', 'uuid', 'NO', 'true'),
            ('journey_id', 'uuid', 'NO', 'false'),
            ('media_id', 'uuid', 'NO', 'false'),
            ('source', 'text', 'NO', 'true'),
            ('source_note_id', 'uuid', 'YES', 'false'),
            ('hidden', 'bool', 'NO', 'true'),
            ('publishable', 'bool', 'NO', 'true'),
            ('position', 'int4', 'NO', 'true'),
        ),
    },
    'journey_members': {
        'primaryKey': ('journey_id', 'user_id'),
        'columns': (
            ('journey_id', 'uuid', 'NO', 'false'),
            ('user_id', 'uuid', 'NO', 'false'),
            ('role', 'text', 'NO', 'false'),
            ('status', 'text', 'NO', 'true'),
            ('attribution_consent', 'bool', 'NO', 'true'),
            ('joined_at', 'timestamptz', 'NO', 'true'),
            ('ended_at', 'timestamptz', 'YES', 'false'),
        ),
    },
    'journey_notes': {
        'primaryKey': ('id',),
        'columns': (
            ('id', 'uuid', 'NO', 'true'),
            ('journey_id', 'uuid', 'NO', 'false'),
            ('body', 'text', 'NO', 'true'),
            ('position', 'int4', 'NO', 'true'),
            ('updated_at', 'timestamptz', 'NO', 'true'),
        ),
    },
    'journey_publication_places': {
        'primaryKey': ('publication_id', 'place_id'),
        'columns': (
            ('publication_id', 'uuid', 'NO', 'false'),
            ('journey_id', 'uuid', 'NO', 'false'),
            ('place_id', 'uuid', 'NO', 'false'),
            ('position', 'int4', 'NO', 'true'),
        ),
    },
    'journey_publications': {
        'primaryKey': ('id',),
        'columns': (
            ('id', 'uuid', 'NO', 'true'),
            ('journey_id', 'uuid', 'NO', 'false'),
            ('working_revision', 'int4', 'NO', 'true'),
            ('published_by', 'uuid', 'NO', 'false'),
            ('published_at', 'timestamptz', 'NO', 'true'),
            ('snapshot', 'jsonb', 'NO', 'true'),
            ('withdrawn_at', 'timestamptz', 'YES', 'false'),
            ('moderation_status', 'text', 'NO', 'true'),
            ('review_reason', 'text', 'NO', 'true'),
            ('decision_reason', 'text', 'YES', 'false'),
            ('moderated_by', 'uuid', 'YES', 'false'),
            ('moderated_at', 'timestamptz', 'YES', 'false'),
            ('moderation_updated_at', 'timestamptz', 'NO', 'true'),
            ('moderation_version', 'int4', 'NO', 'true'),
        ),
    },
    'journey_stops': {
        'primaryKey': ('id',),
        'columns': (
            ('id', 'uuid', 'NO', 'true'),
            ('journey_id', 'uuid', 'NO', 'false'),
            ('position', 'int4', 'NO', 'false'),
            ('name', 'text', 'NO', 'false'),
            ('kind', 'text', 'NO', 'true'),
            ('city_id', 'uuid', 'YES', 'false'),
            ('start_day', 'int4', 'YES', 'false'),
            ('end_day', 'int4', 'YES', 'false'),
            ('source', 'text', 'NO', 'true'),
            ('confidence', 'float8', 'NO', 'true'),
            ('confirmed', 'bool', 'NO', 'true'),
            ('created_at', 'timestamptz', 'NO', 'true'),
            ('destination_id', 'uuid', 'YES', 'false'),
        ),
    },
    'journeys': {
        'primaryKey': ('id',),
        'columns': (
            ('id', 'uuid', 'NO', 'true'),
            ('owner_id', 'uuid', 'NO', 'false'),
            ('title', 'text', 'NO', 'true'),
            ('city', 'text', 'NO', 'true'),
            ('date_label', 'text', 'NO', 'true'),
            ('start_date', 'date', 'YES', 'false'),
            ('days', 'int4', 'NO', 'true'),
            ('cover_url', 'text', 'NO', 'true'),
            ('visibility', 'text', 'NO', 'true'),
            ('working_revision', 'int4', 'NO', 'true'),
            ('published_revision_id', 'uuid', 'YES', 'false'),
            ('allow_copy', 'bool', 'NO', 'true'),
            ('source_public_journey_id', 'uuid', 'YES', 'false'),
            ('archived_at', 'timestamptz', 'YES', 'false'),
            ('deleted_at', 'timestamptz', 'YES', 'false'),
            ('created_at', 'timestamptz', 'NO', 'true'),
            ('updated_at', 'timestamptz', 'NO', 'true'),
            ('city_id', 'uuid', 'YES', 'false'),
            ('team_revision', 'int4', 'NO', 'true'),
            ('scope_label', 'text', 'NO', 'true'),
            ('deleted_by', 'uuid', 'YES', 'false'),
            ('purge_after', 'timestamptz', 'YES', 'false'),
            ('operator_created', 'bool', 'NO', 'true'),
            ('operator_created_by', 'uuid', 'YES', 'false'),
        ),
    },
    'media_assets': {
        'primaryKey': ('id',),
        'columns': (
            ('id', 'uuid', 'NO', 'true'),
            ('owner_id', 'uuid', 'YES', 'false'),
            ('kind', 'text', 'NO', 'true'),
            ('storage_key', 'text', 'NO', 'true'),
            ('public_url', 'text', 'NO', 'true'),
            ('width', 'int4', 'YES', 'false'),
            ('height', 'int4', 'YES', 'false'),
            ('status', 'text', 'NO', 'true'),
            ('created_at', 'timestamptz', 'NO', 'true'),
            ('content_type', 'text', 'NO', 'true'),
            ('size_bytes', 'int8', 'NO', 'true'),
            ('duration_ms', 'int4', 'NO', 'true'),
            ('motion_storage_key', 'text', 'NO', 'true'),
            ('motion_url', 'text', 'NO', 'true'),
            ('motion_content_type', 'text', 'NO', 'true'),
            ('motion_size_bytes', 'int8', 'NO', 'true'),
            ('updated_at', 'timestamptz', 'NO', 'true'),
        ),
    },
    'moment_thread_legs': {
        'primaryKey': ('thread_id', 'from_position'),
        'columns': (
            ('thread_id', 'uuid', 'NO', 'false'),
            ('from_note_id', 'uuid', 'NO', 'false'),
            ('to_note_id', 'uuid', 'NO', 'false'),
            ('from_position', 'int2', 'NO', 'false'),
            ('to_position', 'int2', 'NO', 'false'),
            ('mode', 'text', 'NO', 'false'),
            ('from_anchor_place_id', 'uuid', 'YES', 'false'),
            ('to_anchor_place_id', 'uuid', 'YES', 'false'),
            ('created_at', 'timestamptz', 'NO', 'true'),
            ('updated_at', 'timestamptz', 'NO', 'true'),
        ),
    },
    'moment_threads': {
        'primaryKey': ('id',),
        'columns': (
            ('id', 'uuid', 'NO', 'true'),
            ('author_id', 'uuid', 'NO', 'false'),
            ('entry_count', 'int2', 'NO', 'false'),
            ('created_at', 'timestamptz', 'NO', 'true'),
            ('route_revision', 'int8', 'NO', 'true'),
        ),
    },
    'note_media': {
        'primaryKey': ('note_id', 'position'),
        'columns': (
            ('note_id', 'uuid', 'NO', 'false'),
            ('media_id', 'uuid', 'NO', 'false'),
            ('position', 'int4', 'NO', 'false'),
        ),
    },
    'note_media_locations': {
        'primaryKey': ('note_id', 'media_id'),
        'columns': (
            ('note_id', 'uuid', 'NO', 'false'),
            ('media_id', 'uuid', 'NO', 'false'),
            ('latitude', 'float8', 'YES', 'false'),
            ('longitude', 'float8', 'YES', 'false'),
            ('map_enabled', 'bool', 'NO', 'true'),
            ('place_ids', 'jsonb', 'NO', 'true'),
            ('captured_date', 'date', 'YES', 'false'),
        ),
    },
    'note_places': {
        'primaryKey': ('note_id', 'position'),
        'columns': (
            ('note_id', 'uuid', 'NO', 'false'),
            ('place_id', 'uuid', 'YES', 'false'),
            ('raw_name', 'text', 'NO', 'true'),
            ('position', 'int4', 'NO', 'false'),
        ),
    },
    'notes': {
        'primaryKey': ('id',),
        'columns': (
            ('id', 'uuid', 'NO', 'true'),
            ('author_id', 'uuid', 'NO', 'false'),
            ('type', 'text', 'NO', 'true'),
            ('title', 'text', 'NO', 'true'),
            ('caption', 'text', 'NO', 'true'),
            ('tag', 'text', 'NO', 'true'),
            ('location_label', 'text', 'NO', 'true'),
            ('cover_url', 'text', 'NO', 'true'),
            ('language', 'text', 'NO', 'true'),
            ('visibility', 'text', 'NO', 'true'),
            ('like_count', 'int4', 'NO', 'true'),
            ('save_count', 'int4', 'NO', 'true'),
            ('created_at', 'timestamptz', 'NO', 'true'),
            ('updated_at', 'timestamptz', 'NO', 'true'),
            ('deleted_at', 'timestamptz', 'YES', 'false'),
            ('moderation_status', 'text', 'NO', 'true'),
            ('journey_id', 'uuid', 'YES', 'false'),
            ('journey_day_index', 'int4', 'YES', 'false'),
            ('version', 'int4', 'NO', 'true'),
            ('suspended_journey_id', 'uuid', 'YES', 'false'),
            ('suspended_journey_day_index', 'int4', 'YES', 'false'),
            ('suspended_journey_visibility', 'text', 'YES', 'false'),
            ('suspended_journey_version', 'int4', 'YES', 'false'),
            ('suspended_journey_at', 'timestamptz', 'YES', 'false'),
            ('review_reason', 'text', 'NO', 'true'),
            ('decision_reason', 'text', 'NO', 'true'),
            ('moderated_by', 'uuid', 'YES', 'false'),
            ('moderated_at', 'timestamptz', 'YES', 'false'),
            ('operator_created', 'bool', 'NO', 'true'),
            ('operator_created_by', 'uuid', 'YES', 'false'),
            ('destination_id', 'uuid', 'YES', 'false'),
            ('destination_source', 'text', 'NO', 'true'),
            ('recorded_start_date', 'date', 'YES', 'false'),
            ('recorded_end_date', 'date', 'YES', 'false'),
            ('comments_enabled', 'bool', 'NO', 'true'),
            ('like_count_hidden', 'bool', 'NO', 'true'),
            ('share_count_hidden', 'bool', 'NO', 'true'),
            ('share_count', 'int4', 'NO', 'true'),
            ('map_anchor_lat', 'float8', 'YES', 'false'),
            ('map_anchor_lng', 'float8', 'YES', 'false'),
            ('map_anchor_precision', 'text', 'NO', 'true'),
            ('map_anchor_source', 'text', 'NO', 'true'),
            ('media_locations_set', 'bool', 'NO', 'true'),
            ('thread_id', 'uuid', 'YES', 'false'),
            ('thread_position', 'int2', 'YES', 'false'),
            ('inbound_origin', 'text', 'NO', 'true'),
            ('inbound_mode', 'text', 'NO', 'true'),
            ('inbound_duration_minutes', 'int4', 'YES', 'false'),
            ('inbound_estimated_duration_minutes', 'int4', 'YES', 'false'),
        ),
    },
    'place_area_relations': {
        'primaryKey': ('place_id', 'area_id', 'relation_type'),
        'columns': (
            ('place_id', 'uuid', 'NO', 'false'),
            ('area_id', 'uuid', 'NO', 'false'),
            ('relation_type', 'text', 'NO', 'false'),
            ('source', 'text', 'NO', 'true'),
            ('confidence', 'numeric', 'NO', 'true'),
            ('review_status', 'text', 'NO', 'true'),
            ('created_at', 'timestamptz', 'NO', 'true'),
            ('updated_at', 'timestamptz', 'NO', 'true'),
        ),
    },
    'place_images': {
        'primaryKey': ('id',),
        'columns': (
            ('id', 'uuid', 'NO', 'true'),
            ('place_id', 'uuid', 'NO', 'false'),
            ('media_id', 'uuid', 'YES', 'false'),
            ('contributor_id', 'uuid', 'YES', 'false'),
            ('image_url', 'text', 'NO', 'true'),
            ('source_type', 'text', 'NO', 'true'),
            ('rights_basis', 'text', 'NO', 'true'),
            ('attribution', 'text', 'NO', 'true'),
            ('status', 'text', 'NO', 'true'),
            ('is_cover', 'bool', 'NO', 'true'),
            ('sort_order', 'int4', 'NO', 'true'),
            ('reviewed_by', 'uuid', 'YES', 'false'),
            ('reviewed_at', 'timestamptz', 'YES', 'false'),
            ('created_at', 'timestamptz', 'NO', 'true'),
            ('updated_at', 'timestamptz', 'NO', 'true'),
            ('source_note_id', 'uuid', 'YES', 'false'),
            ('suggest_cover', 'bool', 'NO', 'true'),
            ('cover_eligible', 'bool', 'NO', 'true'),
            ('review_reason', 'text', 'NO', 'true'),
            ('preview_media_id', 'uuid', 'YES', 'false'),
        ),
    },
    'place_provider_refs': {
        'primaryKey': ('provider', 'external_id'),
        'columns': (
            ('provider', 'text', 'NO', 'false'),
            ('external_id', 'text', 'NO', 'false'),
            ('place_id', 'uuid', 'NO', 'false'),
            ('created_at', 'timestamptz', 'NO', 'true'),
            ('raw_category', 'text', 'NO', 'true'),
            ('category_code', 'text', 'NO', 'true'),
            ('mapping_status', 'text', 'NO', 'true'),
            ('source_payload', 'jsonb', 'NO', 'true'),
            ('domain_code', 'text', 'NO', 'true'),
            ('subcategory_code', 'text', 'NO', 'true'),
            ('type_detail_codes', 'jsonb', 'NO', 'true'),
        ),
    },
    'place_save_sources': {
        'primaryKey': ('user_id', 'place_id', 'source_type', 'source_key'),
        'columns': (
            ('user_id', 'uuid', 'NO', 'false'),
            ('place_id', 'uuid', 'NO', 'false'),
            ('source_type', 'text', 'NO', 'false'),
            ('source_key', 'text', 'NO', 'false'),
            ('source_title_snapshot', 'text', 'NO', 'true'),
            ('source_owner_id', 'uuid', 'YES', 'false'),
            ('source_owner_name_snapshot', 'text', 'NO', 'true'),
            ('created_at', 'timestamptz', 'NO', 'true'),
        ),
    },
    'places': {
        'primaryKey': ('id',),
        'columns': (
            ('id', 'uuid', 'NO', 'true'),
            ('region_id', 'uuid', 'YES', 'false'),
            ('name', 'text', 'NO', 'false'),
            ('category', 'text', 'NO', 'true'),
            ('city', 'text', 'NO', 'true'),
            ('address', 'text', 'NO', 'true'),
            ('description', 'text', 'NO', 'true'),
            ('phone', 'text', 'NO', 'true'),
            ('lat', 'float8', 'NO', 'true'),
            ('lng', 'float8', 'NO', 'true'),
            ('cover_url', 'text', 'NO', 'true'),
            ('source', 'text', 'NO', 'true'),
            ('provider_links', 'jsonb', 'NO', 'true'),
            ('status', 'text', 'NO', 'true'),
            ('created_by', 'uuid', 'YES', 'false'),
            ('created_at', 'timestamptz', 'NO', 'true'),
            ('updated_at', 'timestamptz', 'NO', 'true'),
            ('category_code', 'text', 'YES', 'false'),
            ('domain_code', 'text', 'NO', 'true'),
            ('subcategory_code', 'text', 'NO', 'true'),
            ('experience_codes', 'jsonb', 'NO', 'true'),
            ('access_features', 'jsonb', 'NO', 'true'),
            ('opening_hours', 'text', 'NO', 'true'),
            ('source_url', 'text', 'NO', 'true'),
            ('admin_area_id', 'uuid', 'YES', 'false'),
            ('website_url', 'text', 'NO', 'true'),
            ('website_verified_at', 'timestamptz', 'YES', 'false'),
            ('website_verification_source', 'text', 'NO', 'true'),
            ('lifecycle_status', 'text', 'NO', 'true'),
            ('canonical_place_id', 'uuid', 'YES', 'false'),
            ('lifecycle_reason', 'text', 'NO', 'true'),
            ('admin_status', 'text', 'NO', 'true'),
            ('governance_version', 'int4', 'NO', 'true'),
            ('decision_reason', 'text', 'NO', 'true'),
            ('moderated_by', 'uuid', 'YES', 'false'),
            ('moderated_at', 'timestamptz', 'YES', 'false'),
            ('type_detail_codes', 'jsonb', 'NO', 'true'),
            ('name_zh', 'text', 'NO', 'true'),
            ('name_en', 'text', 'NO', 'true'),
            ('search_aliases', '_text', 'NO', 'true'),
            ('details', 'jsonb', 'NO', 'true'),
        ),
    },
    'regions': {
        'primaryKey': ('id',),
        'columns': (
            ('id', 'uuid', 'NO', 'true'),
            ('name', 'text', 'NO', 'false'),
            ('slug', 'text', 'NO', 'false'),
            ('country_code', 'text', 'NO', 'true'),
            ('center_lat', 'float8', 'NO', 'true'),
            ('center_lng', 'float8', 'NO', 'true'),
            ('bbox', 'jsonb', 'YES', 'false'),
            ('created_at', 'timestamptz', 'NO', 'true'),
        ),
    },
    'theme_map_entries': {
        'primaryKey': ('map_id', 'place_id'),
        'columns': (
            ('map_id', 'uuid', 'NO', 'false'),
            ('place_id', 'uuid', 'NO', 'false'),
            ('position', 'int4', 'NO', 'false'),
            ('section', 'text', 'NO', 'true'),
            ('creator_note', 'text', 'NO', 'true'),
            ('collaboration_disclosure', 'bool', 'NO', 'true'),
            ('created_at', 'timestamptz', 'NO', 'true'),
            ('updated_at', 'timestamptz', 'NO', 'true'),
            ('primary_moment_id', 'uuid', 'YES', 'false'),
        ),
    },
    'theme_map_entry_sources': {
        'primaryKey': ('target_map_id', 'place_id', 'source_map_key'),
        'columns': (
            ('target_map_id', 'uuid', 'NO', 'false'),
            ('place_id', 'uuid', 'NO', 'false'),
            ('source_map_key', 'text', 'NO', 'false'),
            ('source_revision', 'int4', 'NO', 'true'),
            ('source_title_snapshot', 'text', 'NO', 'true'),
            ('source_owner_id', 'uuid', 'YES', 'false'),
            ('source_owner_name_snapshot', 'text', 'NO', 'true'),
            ('created_at', 'timestamptz', 'NO', 'true'),
        ),
    },
    'theme_maps': {
        'primaryKey': ('id',),
        'columns': (
            ('id', 'uuid', 'NO', 'true'),
            ('owner_id', 'uuid', 'NO', 'false'),
            ('title', 'text', 'NO', 'false'),
            ('summary', 'text', 'NO', 'true'),
            ('cover_url', 'text', 'NO', 'true'),
            ('visibility', 'text', 'NO', 'true'),
            ('status', 'text', 'NO', 'true'),
            ('audit_status', 'text', 'NO', 'true'),
            ('revision', 'int4', 'NO', 'true'),
            ('published_at', 'timestamptz', 'YES', 'false'),
            ('created_at', 'timestamptz', 'NO', 'true'),
            ('updated_at', 'timestamptz', 'NO', 'true'),
            ('deleted_at', 'timestamptz', 'YES', 'false'),
            ('purge_after', 'timestamptz', 'YES', 'false'),
            ('moderation_status', 'text', 'NO', 'true'),
            ('commercial_disclosure', 'text', 'NO', 'true'),
            ('topic', 'text', 'NO', 'true'),
            ('review_reason', 'text', 'NO', 'true'),
            ('decision_reason', 'text', 'NO', 'true'),
            ('moderated_by', 'uuid', 'YES', 'false'),
            ('moderated_at', 'timestamptz', 'YES', 'false'),
            ('operator_created', 'bool', 'NO', 'true'),
            ('operator_created_by', 'uuid', 'YES', 'false'),
        ),
    },
    'user_saved_places': {
        'primaryKey': ('user_id', 'place_id'),
        'columns': (
            ('user_id', 'uuid', 'NO', 'false'),
            ('place_id', 'uuid', 'NO', 'false'),
            ('created_at', 'timestamptz', 'NO', 'true'),
        ),
    },
    'users': {
        'primaryKey': ('id',),
        'columns': (
            ('id', 'uuid', 'NO', 'true'),
            ('email', 'text', 'YES', 'false'),
            ('display_name', 'text', 'NO', 'false'),
            ('password_hash', 'text', 'YES', 'false'),
            ('created_at', 'timestamptz', 'NO', 'true'),
            ('handle', 'text', 'YES', 'false'),
            ('avatar_url', 'text', 'NO', 'true'),
            ('bio', 'text', 'NO', 'true'),
            ('city', 'text', 'NO', 'true'),
            ('profile_visibility', 'text', 'NO', 'true'),
            ('show_location', 'bool', 'NO', 'true'),
            ('show_saved_count', 'bool', 'NO', 'true'),
            ('allow_journey_copy', 'bool', 'NO', 'true'),
            ('status', 'text', 'NO', 'true'),
            ('version', 'int4', 'NO', 'true'),
            ('updated_at', 'timestamptz', 'NO', 'true'),
            ('data_origin', 'text', 'NO', 'true'),
            ('decision_reason', 'text', 'NO', 'true'),
            ('moderated_by', 'uuid', 'YES', 'false'),
            ('moderated_at', 'timestamptz', 'YES', 'false'),
            ('last_sessions_revoked_at', 'timestamptz', 'YES', 'false'),
            ('background_url', 'text', 'NO', 'true'),
            ('background_preset', 'text', 'NO', 'true'),
            ('background_history', 'jsonb', 'NO', 'true'),
        ),
    },
}

FAMILIES = {
    'accounts': ('users', 'auth_identities'),
    'places': ('cities', 'regions', 'geo_areas', 'places',
               'place_area_relations', 'place_provider_refs'),
    'saved': ('user_saved_places', 'place_save_sources'),
    'collections': ('journeys', 'journey_stops', 'journey_arrangements',
                    'journey_notes', 'journey_members', 'journey_media',
                    'journey_publications', 'journey_publication_places',
                    'theme_maps', 'theme_map_entries', 'theme_map_entry_sources'),
    'notes': ('notes', 'note_places', 'note_media', 'note_media_locations',
              'moment_threads', 'moment_thread_legs'),
    'media': ('media_assets', 'place_images'),
}


class ExtractionError(ValueError):
    """Only fixed, non-sensitive reason codes may reach the CLI."""


def closed(value, fields, reason='SOURCE_PROTOCOL'):
    if not isinstance(value, dict) or set(value) != set(fields):
        raise ExtractionError(reason)
    return value


def canonical(value):
    """Lossless JSON numbers, deterministic keys, no text/unknown-field edits."""
    if value is None:
        return b'null'
    if type(value) is bool:
        return b'true' if value else b'false'
    if type(value) is int:
        return str(value).encode('ascii')
    if isinstance(value, Decimal):
        if not value.is_finite():
            raise ExtractionError('NONFINITE_JSON')
        return str(value).encode('ascii')
    if type(value) is float:
        if not math.isfinite(value):
            raise ExtractionError('NONFINITE_JSON')
        return json.dumps(value, allow_nan=False).encode('ascii')
    if isinstance(value, str):
        try:
            return json.dumps(value, ensure_ascii=False).encode('utf-8')
        except UnicodeError:
            raise ExtractionError('SOURCE_ENCODING') from None
    if isinstance(value, list) or isinstance(value, tuple):
        return b'[' + b','.join(canonical(x) for x in value) + b']'
    if isinstance(value, dict) and all(isinstance(k, str) for k in value):
        return b'{' + b','.join(canonical(k) + b':' + canonical(value[k])
                               for k in sorted(value)) + b'}'
    raise ExtractionError('SOURCE_ENCODING')


def digest(value):
    return hashlib.sha256(canonical(value)).hexdigest()


def _pairs(items):
    result = {}
    for key, value in items:
        if key in result:
            raise ExtractionError('DUPLICATE_JSON_KEY')
        result[key] = value
    return result


def _constant(_):
    raise ExtractionError('NONFINITE_JSON')


def _load(raw):
    try:
        return json.loads(raw.decode('utf-8'), object_pairs_hook=_pairs,
                          parse_float=Decimal, parse_constant=_constant)
    except (UnicodeError, json.JSONDecodeError, RecursionError, ValueError) as error:
        if isinstance(error, ExtractionError):
            raise
        raise ExtractionError('SOURCE_ENCODING') from None


def _identifier(name):
    if not re.fullmatch(r'[a-z_]+', name):
        raise ExtractionError('INTERNAL_SCHEMA')
    return '"' + name + '"'


def _schema_cte():
    names = ','.join("'" + table + "'" for table in sorted(TABLE_SPECS))
    return """WITH metadata AS (
 SELECT 0 k,table_name t,ordinal_position n,''::text c,
 'COLUMN|'||table_name||'|'||ordinal_position||'|'||column_name||'|'||udt_name||'|'||is_nullable||'|'||(column_default IS NOT NULL)::text line
 FROM information_schema.columns WHERE table_schema='public' AND table_name IN (%s)
 UNION ALL
 SELECT 1,c.relname,0,p.conname::text,
 'CONSTRAINT|'||c.relname||'|'||p.conname||'|'||p.contype::text||'|'||pg_get_constraintdef(p.oid)
 FROM pg_constraint p JOIN pg_class c ON c.oid=p.conrelid JOIN pg_namespace ns ON ns.oid=c.relnamespace
 WHERE ns.nspname='public' AND c.relname IN (%s)
) SELECT md5(string_agg(line,E'\\n' ORDER BY k,t COLLATE "C",n,c COLLATE "C")) FROM metadata""" % (names, names)


def snapshot_sql():
    """No user SQL, SELECT *, table discovery, session write, or pool fallback."""
    if len(TABLE_SPECS) != 29:
        raise ExtractionError('INTERNAL_SCHEMA')
    statements = [
        'BEGIN TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY;',
        "SET LOCAL timezone='UTC';",
        "SET LOCAL datestyle='ISO, YMD';",
        "SET LOCAL search_path=pg_catalog,public;",
        "SET LOCAL statement_timeout='90s';",
        "DO $birdtie_schema$ BEGIN IF (" + _schema_cte() + ") IS DISTINCT FROM '" + SCHEMA_MD5 + "' THEN RAISE EXCEPTION 'BIRDTIE_SOURCE_SCHEMA_CHANGED'; END IF; END; $birdtie_schema$;",
    ]
    for table in sorted(TABLE_SPECS):
        relation = 'public.' + _identifier(table)
        statements.append("DO $birdtie_budget$ BEGIN IF (SELECT count(*) FROM " + relation + ") > " + str(MAX_TABLE_ROWS) + " OR pg_relation_size('" + relation + "'::regclass) > " + str(MAX_RELATION_BYTES) + " THEN RAISE EXCEPTION 'BIRDTIE_SOURCE_BUDGET'; END IF; END; $birdtie_budget$;")
    statements.append("SELECT json_build_object('event','capture','observedAt',to_char(clock_timestamp(), 'YYYY-MM-DD\"T\"HH24:MI:SS.US\"Z\"'),'transactionSnapshot',txid_current_snapshot()::text,'schemaMd5','" + SCHEMA_MD5 + "');")
    for table, spec in sorted(TABLE_SPECS.items()):
        columns = ','.join(_identifier(x[0]) for x in spec['columns'])
        ordering = ','.join('r.' + _identifier(x) for x in spec['primaryKey'])
        relation = 'public.' + _identifier(table)
        statements.append("SELECT json_build_object('event','table','table','" + table + "','count',count(*),'rows',coalesce(json_agg(row_to_json(r) ORDER BY " + ordering + "),'[]'::json)) FROM (SELECT " + columns + ' FROM ' + relation + ') r;')
    statements.append('COMMIT;')
    return '\n'.join(statements) + '\n'


def parse_capture(raw):
    if not isinstance(raw, bytes) or len(raw) > MAX_SOURCE_BYTES:
        raise ExtractionError('SOURCE_BUDGET')
    capture = None
    tables = {}
    for line in raw.splitlines():
        if not line.strip():
            continue
        event = _load(line)
        if not isinstance(event, dict):
            raise ExtractionError('SOURCE_PROTOCOL')
        if event.get('event') == 'capture':
            closed(event, ('event', 'observedAt', 'transactionSnapshot', 'schemaMd5'))
            if capture is not None or tables or event['schemaMd5'] != SCHEMA_MD5:
                raise ExtractionError('SOURCE_SCHEMA_CHANGED')
            if not isinstance(event['observedAt'], str) or not UTC_TIME.fullmatch(event['observedAt']):
                raise ExtractionError('SOURCE_PROTOCOL')
            if not isinstance(event['transactionSnapshot'], str) or not re.fullmatch(r'\d+:\d+:(?:\d+(?:,\d+)*)?', event['transactionSnapshot']):
                raise ExtractionError('SOURCE_PROTOCOL')
            capture = {'observedAt': event['observedAt'],
                       'transactionSnapshot': event['transactionSnapshot']}
            continue
        closed(event, ('event', 'table', 'count', 'rows'))
        table = event['table']
        if capture is None or event['event'] != 'table' or not isinstance(table, str) or table not in TABLE_SPECS or table in tables:
            raise ExtractionError('SOURCE_PROTOCOL')
        rows = event['rows']
        if type(event['count']) is not int or not isinstance(rows, list) or event['count'] != len(rows) or not 0 <= len(rows) <= MAX_TABLE_ROWS:
            raise ExtractionError('SOURCE_BUDGET')
        spec = TABLE_SPECS[table]
        columns = {x[0] for x in spec['columns']}
        seen = set()
        for row in rows:
            closed(row, columns, 'SOURCE_COLUMNS_CHANGED')
            values = [row[key] for key in spec['primaryKey']]
            if any(x is None or isinstance(x, (dict, list, bool)) for x in values):
                raise ExtractionError('SOURCE_PRIMARY_KEY')
            key = canonical(values)
            if key in seen:
                raise ExtractionError('SOURCE_PRIMARY_KEY')
            seen.add(key)
        tables[table] = rows
    if capture is None or set(tables) != set(TABLE_SPECS):
        raise ExtractionError('SOURCE_INCOMPLETE')
    return capture, tables


def preserve(capture, tables):
    """All raw values/relationships remain private; no native mapping is inferred."""
    header = {'instanceTag': INSTANCE_TAG, 'container': CONTAINER,
              'database': DATABASE, 'backupCipherSha256': BACKUP_SHA256,
              'schemaSha256': SCHEMA_SHA256}
    header['snapshotSha256'] = digest({'source': header, 'tables': tables})
    document = {'schemaVersion': SOURCE_VERSION, 'source': header,
                'capture': capture, 'tables': tables}
    raw = canonical(document) + b'\n'
    if len(raw) > MAX_SOURCE_BYTES:
        raise ExtractionError('SOURCE_BUDGET')
    manifest = {
        'schemaVersion': MANIFEST_VERSION, 'source': header,
        'sourceFileSha256': hashlib.sha256(raw).hexdigest(),
        'auditedSchemaOutputSha256': AUDIT_FILE_SHA256,
        'counts': {table: len(rows) for table, rows in sorted(tables.items())},
        'families': FAMILIES,
        'tableSha256': {table: digest(rows) for table, rows in sorted(tables.items())},
        'execution': {'preservation': 'EXTRACTED_ONLY', 'targetImport': 'NOT_RUN',
                      'identityVerification': 'NOT_RUN', 'sourceRights': 'NOT_RUN',
                      'mediaBytesMatched': 'NOT_RUN', 'mediaAuthorization': 'NOT_RUN',
                      'sessionMigration': 'NOT_RUN', 'relationshipEffects': 0,
                      'sourceWrites': 0, 'runtimeStarted': False},
    }
    return raw, canonical(manifest) + b'\n', manifest


def _run(args, *, input=None, timeout=120):
    try:
        return subprocess.run(args, input=input, stdout=subprocess.PIPE,
                              stderr=subprocess.PIPE, shell=False,
                              timeout=timeout, check=False)
    except (OSError, subprocess.SubprocessError):
        raise ExtractionError('SOURCE_PROCESS_UNAVAILABLE') from None


def source_read(runner=_run):
    # No full inspect output, environment/credential dump, or remote DB option.
    inspect = runner(['docker', 'inspect', '--format',
                      '{{json .HostConfig.NetworkMode}}|{{json .NetworkSettings.Ports}}|{{.State.Running}}',
                      CONTAINER], timeout=15)
    if inspect.returncode != 0:
        raise ExtractionError('SOURCE_PROCESS_UNAVAILABLE')
    try:
        network, ports, running = inspect.stdout.decode('utf-8').strip().split('|')
        isolated = json.loads(network) == 'none' and json.loads(ports) in ({}, None) and running == 'true'
    except (ValueError, UnicodeError):
        isolated = False
    if not isolated:
        raise ExtractionError('SOURCE_NOT_ISOLATED')
    result = runner(['docker', 'exec', '-i', CONTAINER, 'psql', '-X', '-U',
                     'postgres', '-d', DATABASE, '-q', '-A', '-t',
                     '-v', 'ON_ERROR_STOP=1', '--pset=pager=off'],
                    input=snapshot_sql().encode('utf-8'), timeout=120)
    if result.returncode != 0:
        # PostgreSQL diagnostics can contain values; never relay/persist stderr.
        raise ExtractionError('SOURCE_READ_REJECTED')
    return parse_capture(result.stdout)


def _reparse(path):
    info = path.lstat()
    return stat.S_ISLNK(info.st_mode) or bool(getattr(info, 'st_file_attributes', 0) & 0x400)


def output_location(path):
    candidate = Path(path)
    if not candidate.is_absolute() or candidate.exists() or candidate.is_symlink():
        raise ExtractionError('OUTPUT_NEW_PRIVATE_DIRECTORY_REQUIRED')
    if not candidate.parent.is_dir():
        raise ExtractionError('OUTPUT_PARENT_REQUIRED')
    for ancestor in (candidate.parent, *candidate.parent.parents):
        if _reparse(ancestor):
            raise ExtractionError('OUTPUT_REPARSE_DENIED')
    resolved = candidate.resolve()
    blocked = (Path(__file__).resolve().parents[1], Path(r'D:\BirdTie'), Path(r'D:\Program\Civu'))
    for root in blocked:
        if resolved == root or root in resolved.parents:
            raise ExtractionError('OUTPUT_REPOSITORY_DENIED')
    return resolved


def _windows_private_directory(path):
    # Current SID, not a guessed account name; the DACL has one inherited owner ACE.
    current = _run(['whoami.exe', '/user', '/fo', 'csv', '/nh'], timeout=10)
    try:
        row = next(csv.reader(io.StringIO(current.stdout.decode('utf-8', errors='replace'))))
        sid = row[1]
        if current.returncode != 0 or not re.fullmatch(r'S-1-\d+(?:-\d+)+', sid):
            raise ValueError()
    except (ValueError, UnicodeError, IndexError, StopIteration):
        raise ExtractionError('OUTPUT_PROTECTION_FAILED') from None
    advapi = ctypes.WinDLL('advapi32', use_last_error=True)
    kernel = ctypes.WinDLL('kernel32', use_last_error=True)
    descriptor = ctypes.c_void_p()
    convert = advapi.ConvertStringSecurityDescriptorToSecurityDescriptorW
    convert.argtypes = (ctypes.c_wchar_p, ctypes.c_uint32,
                        ctypes.POINTER(ctypes.c_void_p), ctypes.c_void_p)
    convert.restype = ctypes.c_int
    apply = advapi.SetFileSecurityW
    apply.argtypes = (ctypes.c_wchar_p, ctypes.c_uint32, ctypes.c_void_p)
    apply.restype = ctypes.c_int
    kernel.LocalFree.argtypes = (ctypes.c_void_p,)
    kernel.LocalFree.restype = ctypes.c_void_p
    if not convert('D:P(A;OICI;FA;;;' + sid + ')', 1, ctypes.byref(descriptor), None):
        raise ExtractionError('OUTPUT_PROTECTION_FAILED')
    try:
        if not apply(str(path), 0x00000004 | 0x80000000, descriptor):
            raise ExtractionError('OUTPUT_PROTECTION_FAILED')
    finally:
        kernel.LocalFree(descriptor)


def private_directory(path):
    path = output_location(path)
    try:
        path.mkdir(mode=0o700)
        if os.name == 'nt':
            _windows_private_directory(path)
        else:
            path.chmod(0o700)
            if stat.S_IMODE(path.stat().st_mode) != 0o700:
                raise ExtractionError('OUTPUT_PROTECTION_FAILED')
    except OSError:
        raise ExtractionError('OUTPUT_PROTECTION_FAILED') from None
    return path


def _write_new(path, raw):
    try:
        descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(descriptor, 'wb') as target:
            target.write(raw)
            target.flush()
            os.fsync(target.fileno())
    except OSError:
        raise ExtractionError('OUTPUT_WRITE_FAILED') from None


class _Parser(argparse.ArgumentParser):
    def error(self, _):
        raise ExtractionError('CLI_ARGUMENTS')


def main(argv=None, *, runner=_run):
    parser = _Parser(description='Preserve the verified isolated Hangzhou restore; no import, authorization, media access or service start.')
    parser.add_argument('--container', default=CONTAINER)
    parser.add_argument('--database', default=DATABASE)
    parser.add_argument('--source-backup-sha256', required=True)
    parser.add_argument('--output-directory', required=True)
    try:
        args = parser.parse_args(argv)
        if args.container != CONTAINER or args.database != DATABASE or args.source_backup_sha256 != BACKUP_SHA256:
            raise ExtractionError('SOURCE_IDENTITY_DENIED')
        directory = private_directory(args.output_directory)
        capture, tables = source_read(runner)
        source, manifest, details = preserve(capture, tables)
        _write_new(directory / 'source.json', source)
        _write_new(directory / 'manifest.json', manifest)
        # No hash or output-path echo: these private objects contain credentials.
        print(json.dumps({'status': 'PRESERVED_ONLY', 'tables': len(TABLE_SPECS),
                          'counts': details['counts'], 'sourceWrites': 0,
                          'targetImport': 'NOT_RUN', 'mediaBytesMatched': 'NOT_RUN'}, sort_keys=True))
        return 0
    except ExtractionError as error:
        print(json.dumps({'status': 'REJECTED', 'reason': str(error)}, sort_keys=True))
        return 2
    except (OSError, ValueError, TypeError, RecursionError):
        print(json.dumps({'status': 'REJECTED', 'reason': 'PRESERVATION_FAILED'}, sort_keys=True))
        return 2


if __name__ == '__main__':
    raise SystemExit(main())
