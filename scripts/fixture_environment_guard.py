"""Explicit isolated-fixture boundaries; no network, login, or writes on import.

Database facts must come from the actual target connection. This module does not
turn environment-variable assertions into proof of an API/database binding.
"""
from __future__ import annotations

import argparse
import ipaddress
import re
from urllib.parse import urlsplit

DATABASE_PATTERN = r'(?:birdtie_fixture_[a-z0-9_]{1,40}|birdtie_owned_migration_[0-9a-f]{24})'
MARKER_PREFIX = 'BIRDTIE_ISOLATED_FIXTURE_V1:'
OPT_IN = 'isolated-fixtures-v1'
RETIRED_PRODUCTION_SCRIPTS = frozenset({
    'remote_friend_chat_flow.py', 'remote_friend_chat_flow02.py',
    'remote_media_thread_flow.py', 'remote_multilocation166.py',
    'remote_crosscity166.py', 'verify_remote_social.py', 'verify_remote_social02.py',
})


class FixtureEnvironmentError(RuntimeError):
    pass


def require_fixture_database(database, marker, opt_in=None):
    if not isinstance(database, str) or re.fullmatch(DATABASE_PATTERN, database) is None:
        raise FixtureEnvironmentError('DEDICATED_FIXTURE_DATABASE_REQUIRED')
    if marker != MARKER_PREFIX + database:
        raise FixtureEnvironmentError('EXACT_FIXTURE_DATABASE_MARKER_REQUIRED')
    if opt_in != OPT_IN:
        raise FixtureEnvironmentError('EXPLICIT_FIXTURE_OPT_IN_REQUIRED')


def require_fixture_api_origin(origin):
    try:
        parsed = urlsplit(origin)
        address = ipaddress.ip_address(parsed.hostname or '')
        port = parsed.port
    except (TypeError, ValueError):
        raise FixtureEnvironmentError('LITERAL_LOOPBACK_FIXTURE_ORIGIN_REQUIRED') from None
    if (parsed.scheme != 'http' or not address.is_loopback or parsed.username or parsed.password
            or parsed.path not in ('', '/') or parsed.query or parsed.fragment
            or port is None or not 20000 <= port <= 65535 or port == 55432):
        raise FixtureEnvironmentError('LITERAL_LOOPBACK_FIXTURE_ORIGIN_REQUIRED')


def fixture_session_sql(database, marker, opt_in=None):
    require_fixture_database(database, marker, opt_in)
    # database passed the identifier grammar; no connection strings are accepted.
    return ("SET birdtie.fixture_opt_in = '" + OPT_IN + "'; "
            "SET birdtie.fixture_database = '" + database + "';")


def refuse_historical_production_fixture(script_name):
    """These source snapshots target production and have no safe local adapter.

    Deliberately has no environment-variable bypass. Port a flow into the guarded
    isolated runner before reusing it. This check runs before the old module body.
    """
    if script_name not in RETIRED_PRODUCTION_SCRIPTS:
        raise FixtureEnvironmentError('UNREVIEWED_HISTORICAL_FIXTURE_SCRIPT')
    raise FixtureEnvironmentError('HISTORICAL_PRODUCTION_FIXTURE_RETIRED_USE_ISOLATED_RUNNER')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--database', required=True)
    parser.add_argument('--marker', required=True)
    parser.add_argument('--allow-isolated-fixtures', action='store_true')
    parser.add_argument('--api-origin')
    args = parser.parse_args()
    if args.api_origin:
        require_fixture_api_origin(args.api_origin)
    print(fixture_session_sql(args.database, args.marker,
        OPT_IN if args.allow_isolated_fixtures else None))


if __name__ == '__main__':
    main()
