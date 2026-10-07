param([ValidateSet(39, 40, 41, 42, 43, 44, 45, 46, 47, 48, 49, 50, 51, 52)][int]$Through = 39)
$ErrorActionPreference = 'Stop'
$api = Join-Path $PSScriptRoot '..\apps\api'
$database = 'birdtie_place_verify_' + [Guid]::NewGuid().ToString('N').Substring(0, 12)
function Sql([string]$db, [string[]]$arguments) {
    & docker compose exec -T db psql -v ON_ERROR_STOP=1 -U birdtie -d $db @arguments
    if ($LASTEXITCODE -ne 0) { throw "psql failed in $db ($LASTEXITCODE)" }
}
Push-Location $api
try {
    Sql 'postgres' @('-c', "CREATE DATABASE $database") | Out-Null
    $migrations = @(Get-ChildItem migrations -Filter '*.sql' |
        Where-Object { $_.Name -notlike '*.down.sql' } | Sort-Object Name)
    foreach ($migration in $migrations | Where-Object { [int]$_.Name.Substring(0, 3) -le 29 }) {
        Sql $database @('-f', "/migrations/$($migration.Name)") | Out-Null
    }
    Sql $database @('-f', '/dev-seeds/001_badminton.sql') | Out-Null
    Sql $database @('-f', '/dev-seeds/002_functional_mvp.sql') | Out-Null
    foreach ($migration in $migrations | Where-Object {
        $number = [int]$_.Name.Substring(0, 3); $number -ge 30 -and $number -le $Through
    }) {
        if ($migration.Name -eq '036_social_intents.sql') {
            Sql $database @('-c', "INSERT INTO intents
                (owner_account_id,city_id,topic,details,available_from,available_until,
                 time_zone,coarse_area_label,audience,state,expires_at)
                SELECT id,'aberdeen-gb','Synthetic legacy intent','migration guard',
                    now(),now()+interval '1 day','Europe/London','Aberdeen',
                    'private','draft',now()+interval '1 day'
                FROM accounts WHERE account_type='person' AND status='active' ORDER BY id LIMIT 1") | Out-Null
        }
        if ($migration.Name -eq '041_business_principals.sql') {
            $legacyBefore41 = (Sql $database @('-qAtc', "SELECT
                (SELECT coalesce(string_agg(id::text,',' ORDER BY id),'') FROM accounts) || '|' ||
                (SELECT coalesce(string_agg(id::text,',' ORDER BY id),'') FROM agents) || '|' ||
                (SELECT coalesce(string_agg(id::text,',' ORDER BY id),'') FROM organizations) || '|' ||
                (SELECT coalesce(string_agg(id::text,',' ORDER BY id),'') FROM places) || '|' ||
                (SELECT coalesce(string_agg(id::text,',' ORDER BY id),'') FROM activities) || '|' ||
                (SELECT coalesce(string_agg(id::text,',' ORDER BY id),'') FROM intents) || '|' ||
                (SELECT coalesce(string_agg(activity_id::text || ':' ||
                    coalesce(person_account_id::text,community_id::text,organization_id::text),
                    ',' ORDER BY activity_id),'') FROM activity_organizers)")).Trim()
        }
        if ($migration.Name -eq '043_moment_context_links.sql') {
            $legacyMomentID = (Sql $database @('-qAtc', "INSERT INTO moments
                (author_account_id,city_id,title,body,time_precision,location_precision)
                SELECT id,'aberdeen-gb','Legacy private Moment','preserve ID','unknown','city'
                FROM accounts WHERE account_type='person' ORDER BY id LIMIT 1 RETURNING id")).Trim()
            if (!$legacyMomentID) { throw '043 legacy Moment setup failed' }
        }
        Sql $database @('-f', "/migrations/$($migration.Name)") | Out-Null
        if ($migration.Name -eq '043_moment_context_links.sql') {
            $preservedMoment = (Sql $database @('-qAtc', "SELECT id FROM moments WHERE id='$legacyMomentID'")).Trim()
            if ($preservedMoment -ne $legacyMomentID) { throw '043 changed legacy Moment ID' }
            Write-Output '[PASS] 043 preserved existing private Moment ID'
        }
        if ($migration.Name -eq '041_business_principals.sql') {
            $legacyAfter41 = (Sql $database @('-qAtc', "SELECT
                (SELECT coalesce(string_agg(id::text,',' ORDER BY id),'') FROM accounts) || '|' ||
                (SELECT coalesce(string_agg(id::text,',' ORDER BY id),'') FROM agents) || '|' ||
                (SELECT coalesce(string_agg(id::text,',' ORDER BY id),'') FROM organizations) || '|' ||
                (SELECT coalesce(string_agg(id::text,',' ORDER BY id),'') FROM places) || '|' ||
                (SELECT coalesce(string_agg(id::text,',' ORDER BY id),'') FROM activities) || '|' ||
                (SELECT coalesce(string_agg(id::text,',' ORDER BY id),'') FROM intents) || '|' ||
                (SELECT coalesce(string_agg(activity_id::text || ':' ||
                    coalesce(person_account_id::text,community_id::text,organization_id::text),
                    ',' ORDER BY activity_id),'') FROM activity_organizers)")).Trim()
            if ($legacyBefore41 -ne $legacyAfter41) { throw '041 changed existing account/Agent/Organization/Place/Activity/Intent or organizer IDs' }
            Write-Output '[PASS] 041 forward preserved existing IDs and organizer ownership'
        }
        if ($migration.Name -eq '033_context_graph.sql') {
            Sql $database @('-f', '/dev-seeds/003_community_social.sql') | Out-Null
        }
    }
    $before = (Sql $database @('-Atc', "SELECT
        (SELECT coalesce(string_agg(id::text,',' ORDER BY id),'') FROM places) || '|' ||
        (SELECT coalesce(string_agg(id::text,',' ORDER BY id),'') FROM activities) || '|' ||
        (SELECT coalesce(string_agg(id::text,',' ORDER BY id),'') FROM intents) || '|' ||
        (SELECT coalesce(string_agg(id::text,',' ORDER BY id),'') FROM moments)")).Trim()
    $env:BIRDTIE_DATABASE_URL = "postgres://birdtie:birdtie_local_only@127.0.0.1:55432/$database`?sslmode=disable"
    $env:BIRDTIE_DISPOSABLE_DB = '1'
    try {
        # Tests own their mutable fixtures; exercise normal package concurrency.
        & go test ./... -count=1
        if ($LASTEXITCODE -ne 0) { throw "$Through full Go test failed" }
    } finally {
        Remove-Item Env:BIRDTIE_DATABASE_URL -ErrorAction SilentlyContinue
        Remove-Item Env:BIRDTIE_DISPOSABLE_DB -ErrorAction SilentlyContinue
    }
    $after = (Sql $database @('-Atc', "SELECT
        (SELECT coalesce(string_agg(id::text,',' ORDER BY id),'') FROM places) || '|' ||
        (SELECT coalesce(string_agg(id::text,',' ORDER BY id),'') FROM activities) || '|' ||
        (SELECT coalesce(string_agg(id::text,',' ORDER BY id),'') FROM intents) || '|' ||
        (SELECT coalesce(string_agg(id::text,',' ORDER BY id),'') FROM moments)")).Trim()
    if ($before -ne $after) { throw 'existing Place, Activity, Intent or Moment IDs changed' }
    Write-Output "[PASS] $Through full Go API/DB tests and existing Place/Activity/Intent/Moment IDs preserved"
    if ($Through -ge 52) {
        $newPeoplePerson = (Sql $database @('-Atc', "INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),'person') RETURNING id")).Trim().Split("`n")[0]
        Sql $database @('-c', "INSERT INTO person_new_people_consent(account_id,enabled) VALUES('$newPeoplePerson',false)") | Out-Null
        $newPeopleGuard = (& docker compose exec -T db psql -v ON_ERROR_STOP=1 -U birdtie -d $database -f /migrations/052_new_people_consent.down.sql 2>&1 | Out-String)
        if ($LASTEXITCODE -eq 0 -or $newPeopleGuard -notmatch 'cannot remove new people consent while choices exist') { throw '052 down did not protect explicit choices' }
        Sql $database @('-c', "DELETE FROM person_new_people_consent WHERE account_id='$newPeoplePerson'; DELETE FROM accounts WHERE id='$newPeoplePerson'") | Out-Null
        Sql $database @('-f', '/migrations/052_new_people_consent.down.sql') | Out-Null
        Sql $database @('-f', '/migrations/052_new_people_consent.sql') | Out-Null
        Write-Output '[PASS] 052 explicit choice guard and empty down/reapply'
        Sql $database @('-f', '/migrations/052_new_people_consent.down.sql') | Out-Null
    }
    if ($Through -ge 51) {
        $consentPerson = (Sql $database @('-Atc', "INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),'person') RETURNING id")).Trim().Split("`n")[0]
        Sql $database @('-c', "INSERT INTO person_agent_relationship_consent(account_id,enabled) VALUES('$consentPerson',false)") | Out-Null
        $consentGuard = (& docker compose exec -T db psql -v ON_ERROR_STOP=1 -U birdtie -d $database -f /migrations/051_agent_relationship_consent.down.sql 2>&1 | Out-String)
        if ($LASTEXITCODE -eq 0 -or $consentGuard -notmatch 'cannot remove Agent relationship consent while choices exist') { throw '051 down did not protect explicit choices' }
        Sql $database @('-c', "DELETE FROM person_agent_relationship_consent WHERE account_id='$consentPerson'; DELETE FROM accounts WHERE id='$consentPerson'") | Out-Null
        Sql $database @('-f', '/migrations/051_agent_relationship_consent.down.sql') | Out-Null
        Sql $database @('-f', '/migrations/051_agent_relationship_consent.sql') | Out-Null
        Write-Output '[PASS] 051 explicit choice guard and empty down/reapply'
        Sql $database @('-f', '/migrations/051_agent_relationship_consent.down.sql') | Out-Null
    }
    if ($Through -ge 50) {
        $communityChatID = (Sql $database @('-qAtc', "INSERT INTO community_conversations(community_id)
            SELECT id FROM communities ORDER BY id LIMIT 1 RETURNING id")).Trim()
        if (!$communityChatID) { throw '050 community conversation guard setup failed' }
        $communityChatGuard = (& docker compose exec -T db psql -v ON_ERROR_STOP=1 -U birdtie -d $database -f /migrations/050_community_conversations.down.sql 2>&1 | Out-String)
        if ($LASTEXITCODE -eq 0 -or $communityChatGuard -notmatch 'cannot remove community conversation data or reports') {
            throw '050 down did not protect community conversation data'
        }
        Sql $database @('-c', "DELETE FROM community_conversations WHERE id='$communityChatID'") | Out-Null
        $communityChatReportID = (Sql $database @('-qAtc', "INSERT INTO incident_reports(reporter_account_id,target_type,target_id,reason,details)
            SELECT id,'community_message',gen_random_uuid(),'technical','Synthetic migration guard'
            FROM accounts WHERE account_type='person' ORDER BY id LIMIT 1 RETURNING id")).Trim()
        $communityReportGuard = (& docker compose exec -T db psql -v ON_ERROR_STOP=1 -U birdtie -d $database -f /migrations/050_community_conversations.down.sql 2>&1 | Out-String)
        if ($LASTEXITCODE -eq 0 -or $communityReportGuard -notmatch 'cannot remove community conversation data or reports') {
            throw '050 down did not protect community message reports'
        }
        Sql $database @('-c', "DELETE FROM incident_reports WHERE id='$communityChatReportID'") | Out-Null
        Sql $database @('-f', '/migrations/050_community_conversations.down.sql') | Out-Null
        Sql $database @('-f', '/migrations/050_community_conversations.sql') | Out-Null
        Write-Output '[PASS] 050 conversation/report guards and empty down/reapply'
        Sql $database @('-f', '/migrations/050_community_conversations.down.sql') | Out-Null
    }
    if ($Through -ge 49) {
        $chatID = (Sql $database @('-qAtc', "INSERT INTO activity_conversations(activity_id)
            SELECT id FROM activities ORDER BY id LIMIT 1 RETURNING id")).Trim()
        if (!$chatID) { throw '049 activity conversation guard setup failed' }
        $chatGuard = (& docker compose exec -T db psql -v ON_ERROR_STOP=1 -U birdtie -d $database -f /migrations/049_activity_conversations.down.sql 2>&1 | Out-String)
        if ($LASTEXITCODE -eq 0 -or $chatGuard -notmatch 'cannot remove activity conversation data or reports') {
            throw '049 down did not protect activity conversation data'
        }
        Sql $database @('-c', "DELETE FROM activity_conversations WHERE id='$chatID'") | Out-Null
        $chatReportID = (Sql $database @('-qAtc', "INSERT INTO incident_reports(reporter_account_id,target_type,target_id,reason,details)
            SELECT id,'activity_message',gen_random_uuid(),'technical','Synthetic migration guard'
            FROM accounts WHERE account_type='person' ORDER BY id LIMIT 1 RETURNING id")).Trim()
        $reportGuard49 = (& docker compose exec -T db psql -v ON_ERROR_STOP=1 -U birdtie -d $database -f /migrations/049_activity_conversations.down.sql 2>&1 | Out-String)
        if ($LASTEXITCODE -eq 0 -or $reportGuard49 -notmatch 'cannot remove activity conversation data or reports') {
            throw '049 down did not protect activity message reports'
        }
        Sql $database @('-c', "DELETE FROM incident_reports WHERE id='$chatReportID'") | Out-Null
        Sql $database @('-f', '/migrations/049_activity_conversations.down.sql') | Out-Null
        Sql $database @('-f', '/migrations/049_activity_conversations.sql') | Out-Null
        Write-Output '[PASS] 049 conversation/report guards and empty down/reapply'
        Sql $database @('-f', '/migrations/049_activity_conversations.down.sql') | Out-Null
    }
    if ($Through -ge 48) {
        $disclosurePerson = (Sql $database @('-qAtc', "INSERT INTO person_social_disclosure(account_id)
            SELECT id FROM accounts WHERE account_type='person' AND status='active' ORDER BY id LIMIT 1
            RETURNING account_id")).Trim()
        if (!$disclosurePerson) { throw '048 social disclosure guard setup failed' }
        $disclosureGuard = (& docker compose exec -T db psql -v ON_ERROR_STOP=1 -U birdtie -d $database -f /migrations/048_social_disclosure.down.sql 2>&1 | Out-String)
        if ($LASTEXITCODE -eq 0 -or $disclosureGuard -notmatch 'cannot remove social disclosure preferences') {
            throw '048 down did not protect disclosure choices'
        }
        Sql $database @('-c', "DELETE FROM person_social_disclosure WHERE account_id='$disclosurePerson'") | Out-Null
        Sql $database @('-f', '/migrations/048_social_disclosure.down.sql') | Out-Null
        Sql $database @('-f', '/migrations/048_social_disclosure.sql') | Out-Null
        Write-Output '[PASS] 048 disclosure data guard and empty down/reapply'
        Sql $database @('-f', '/migrations/048_social_disclosure.down.sql') | Out-Null
    }
    if ($Through -ge 47) {
        $followID = (Sql $database @('-qAtc', "INSERT INTO follows(follower_account_id,organization_id)
            SELECT person.id,org.id FROM
                (SELECT id FROM accounts WHERE account_type='person' AND status='active' ORDER BY id LIMIT 1) person
            CROSS JOIN (SELECT id FROM organizations WHERE status='active' ORDER BY id LIMIT 1) org
            RETURNING id")).Trim()
        if (!$followID) { throw '047 Follow guard setup failed' }
        $followGuard = (& docker compose exec -T db psql -v ON_ERROR_STOP=1 -U birdtie -d $database -f /migrations/047_asymmetric_follows.down.sql 2>&1 | Out-String)
        if ($LASTEXITCODE -eq 0 -or $followGuard -notmatch 'cannot remove Follow relationships') {
            throw '047 down did not protect Follow data'
        }
        Sql $database @('-c', "DELETE FROM follows WHERE id='$followID'") | Out-Null
        Sql $database @('-f', '/migrations/047_asymmetric_follows.down.sql') | Out-Null
        Sql $database @('-f', '/migrations/047_asymmetric_follows.sql') | Out-Null
        Write-Output '[PASS] 047 Follow data guard and empty down/reapply'
        Sql $database @('-f', '/migrations/047_asymmetric_follows.down.sql') | Out-Null
    }
    if ($Through -ge 46) {
        $reportID = (Sql $database @('-qAtc', "INSERT INTO incident_reports
            (reporter_account_id,target_type,target_id,reason,details)
            SELECT id,'message',gen_random_uuid(),'harassment','Synthetic migration guard'
            FROM accounts WHERE account_type='person' ORDER BY id LIMIT 1 RETURNING id")).Trim()
        if (!$reportID) { throw '046 social report guard setup failed' }
        $reportGuard = (& docker compose exec -T db psql -v ON_ERROR_STOP=1 -U birdtie -d $database -f /migrations/046_social_report_targets.down.sql 2>&1 | Out-String)
        if ($LASTEXITCODE -eq 0 -or $reportGuard -notmatch 'cannot remove social report target types') {
            throw '046 down did not protect social reports'
        }
        Sql $database @('-c', "DELETE FROM incident_reports WHERE id='$reportID'") | Out-Null
        Sql $database @('-f', '/migrations/046_social_report_targets.down.sql') | Out-Null
        Sql $database @('-f', '/migrations/046_social_report_targets.sql') | Out-Null
        Write-Output '[PASS] 046 social report target guard and empty down/reapply'
    }
    if ($Through -ge 45) {
        Sql $database @('-f', '/migrations/045_business_activity_cancel_after_venue_revocation.down.sql') | Out-Null
        Sql $database @('-f', '/migrations/045_business_activity_cancel_after_venue_revocation.sql') | Out-Null
        Write-Output '[PASS] 045 Business cancellation policy down and reapply'
    }
    if ($Through -ge 44) {
        $cardConversation = (Sql $database @('-qAtc', "WITH request AS (
            INSERT INTO connection_requests(sender_account_id,recipient_account_id,city_id,note,scope,state,expires_at)
            VALUES('b1700000-0000-4000-8000-000000000010','b1700000-0000-4000-8000-000000000011',
              'aberdeen-gb','Synthetic migration guard','conversation','accepted',now()+interval '1 day')
            RETURNING id)
            INSERT INTO conversations(request_id,member_a_account_id,member_b_account_id)
            SELECT id,'b1700000-0000-4000-8000-000000000010',
              'b1700000-0000-4000-8000-000000000011' FROM request RETURNING id")).Trim()
        Sql $database @('-c', "INSERT INTO conversation_messages(conversation_id,sender_account_id,body,entity_type,entity_id)
            VALUES('$cardConversation','b1700000-0000-4000-8000-000000000010',
              'Synthetic card','place','b1700000-0000-4000-8000-000000000004')") | Out-Null
        $cardGuard = (& docker compose exec -T db psql -v ON_ERROR_STOP=1 -U birdtie -d $database -f /migrations/044_chat_entity_cards.down.sql 2>&1 | Out-String)
        if ($LASTEXITCODE -eq 0 -or $cardGuard -notmatch 'cannot remove Chat entity cards') {
            throw '044 down did not protect shared cards'
        }
        Sql $database @('-c', "DELETE FROM conversation_messages WHERE conversation_id='$cardConversation'") | Out-Null
        Sql $database @('-c', "DELETE FROM conversations WHERE id='$cardConversation'") | Out-Null
        Sql $database @('-c', "DELETE FROM connection_requests WHERE note='Synthetic migration guard'") | Out-Null
        Sql $database @('-f', '/migrations/044_chat_entity_cards.down.sql') | Out-Null
        Sql $database @('-f', '/migrations/044_chat_entity_cards.sql') | Out-Null
        Write-Output '[PASS] 044 shared-card down guard and empty down/reapply'
    }
    if ($Through -ge 43) {
        $momentID = (Sql $database @('-qAtc', "INSERT INTO moments
            (author_account_id,city_id,title,body,time_precision,location_precision)
            SELECT id,'aberdeen-gb','Synthetic context rollback guard','private fixture','unknown','city'
            FROM accounts WHERE account_type='person' ORDER BY id LIMIT 1 RETURNING id")).Trim()
        Sql $database @('-c', "INSERT INTO moment_community_links(moment_id,community_id,author_confirmed_at)
            VALUES('$momentID','b1700000-0000-4000-8000-000000000030',now())") | Out-Null
        $momentGuard = (& docker compose exec -T db psql -v ON_ERROR_STOP=1 -U birdtie -d $database -f /migrations/043_moment_context_links.down.sql 2>&1 | Out-String)
        if ($LASTEXITCODE -eq 0 -or $momentGuard -notmatch 'cannot remove Moment context links') {
            throw '043 down did not protect Moment context data'
        }
        Sql $database @('-c', "DELETE FROM moments WHERE id='$momentID'") | Out-Null
        Sql $database @('-f', '/migrations/043_moment_context_links.down.sql') | Out-Null
        Sql $database @('-f', '/migrations/043_moment_context_links.sql') | Out-Null
        Write-Output '[PASS] 043 Moment context links protected down and empty down/reapply'
    }
    if ($Through -ge 42) {
        $locationBefore = (Sql $database @('-qAtc', "SELECT count(*) FROM activities
            WHERE place_id IS NOT NULL AND modality='in_person' AND physical_place_status='confirmed'")).Trim()
        if ([int]$locationBefore -eq 0) { throw '042 did not backfill legacy Activity place relations' }
        $activityID = (Sql $database @('-qAtc', "SELECT id FROM activities WHERE place_id IS NOT NULL ORDER BY id LIMIT 1")).Trim()
        $oldPlaceID = (Sql $database @('-qAtc', "SELECT place_id FROM activities WHERE id='$activityID'")).Trim()
        Sql $database @('-c', "BEGIN; UPDATE activities SET modality='in_person',physical_place_status='tbd',
            place_id=NULL,venue_place_id=NULL WHERE id='$activityID'; ROLLBACK") | Out-Null
        Sql $database @('-c', "BEGIN; UPDATE activities SET modality='online',physical_place_status='not_applicable',
            place_id=NULL,venue_place_id=NULL WHERE id='$activityID'; ROLLBACK") | Out-Null
        $venueCandidate = (Sql $database @('-qAtc', "INSERT INTO venue_candidates
            (place_id,city_id,submitted_by,capacity,reservation_support,source_url,rights_note,
             expires_at,status,reviewed_by,reviewed_at)
            SELECT p.id,p.city_id,author.id,20,'contact','https://example.org/synthetic-activity-venue',
                   'Synthetic Activity Venue relation check',now()+interval '2 days','approved',reviewer.id,now()
            FROM places p CROSS JOIN LATERAL
                (SELECT id FROM accounts WHERE account_type='person' ORDER BY id LIMIT 1) author
            CROSS JOIN LATERAL
                (SELECT id FROM accounts WHERE account_type='person' ORDER BY id OFFSET 1 LIMIT 1) reviewer
            WHERE p.id='$oldPlaceID' RETURNING id")).Trim()
        if (!$venueCandidate) { throw '042 Venue relation setup failed' }
        Sql $database @('-c', "INSERT INTO venues
            (place_id,city_id,capacity,reservation_support,source_candidate_id,source_url,
             reviewed_by,reviewed_at,expires_at)
            SELECT place_id,city_id,capacity,reservation_support,id,source_url,reviewed_by,reviewed_at,expires_at
            FROM venue_candidates WHERE id='$venueCandidate'") | Out-Null
        Sql $database @('-c', "UPDATE activities SET venue_place_id='$oldPlaceID' WHERE id='$activityID'") | Out-Null
        $venueLinked = (Sql $database @('-qAtc', "SELECT venue_place_id FROM activities WHERE id='$activityID'")).Trim()
        if ($venueLinked -ne $oldPlaceID) { throw '042 did not preserve explicit Venue relation' }
        Sql $database @('-c', "UPDATE activities SET venue_place_id=NULL WHERE id='$activityID'") | Out-Null
        Sql $database @('-c', "DELETE FROM venues WHERE source_candidate_id='$venueCandidate'") | Out-Null
        Sql $database @('-c', "DELETE FROM venue_candidates WHERE id='$venueCandidate'") | Out-Null
        Sql $database @('-c', "UPDATE places SET publication_status='hidden' WHERE id='$oldPlaceID'") | Out-Null
        $hiddenPublish = (& docker compose exec -T db psql -v ON_ERROR_STOP=1 -U birdtie -d $database -c "UPDATE activities SET publication_status='published' WHERE id='$activityID'" 2>&1 | Out-String)
        if ($LASTEXITCODE -eq 0 -or $hiddenPublish -notmatch 'activity place must be current and public') {
            throw '042 allowed publication with a hidden Place'
        }
        Sql $database @('-c', "UPDATE places SET publication_status='published' WHERE id='$oldPlaceID'") | Out-Null
        $invalid = (& docker compose exec -T db psql -v ON_ERROR_STOP=1 -U birdtie -d $database -c "UPDATE activities SET modality='online',physical_place_status='not_applicable' WHERE id='$activityID'" 2>&1 | Out-String)
        if ($LASTEXITCODE -eq 0 -or $invalid -notmatch 'activity_physical_place_consistent') {
            throw '042 accepted an online Activity with a physical Place'
        }
        Write-Output '[PASS] 042 legacy relation, reviewed Venue link, TBD and online transitions, hidden Place and mixed-state rejection'
        Sql $database @('-c', "UPDATE activities SET modality='in_person',physical_place_status='tbd',
            place_id=NULL,venue_place_id=NULL WHERE id='$activityID'") | Out-Null
        $locationGuard = (& docker compose exec -T db psql -v ON_ERROR_STOP=1 -U birdtie -d $database -f /migrations/042_activity_place_venue.down.sql 2>&1 | Out-String)
        if ($LASTEXITCODE -eq 0 -or $locationGuard -notmatch 'cannot remove explicit Activity location semantics') {
            throw '042 down did not protect explicit Activity location data'
        }
        Write-Output '[PASS] 042 down refuses to erase explicit Activity location'
        Sql $database @('-c', "UPDATE activities SET modality='in_person',physical_place_status='confirmed',
            place_id='$oldPlaceID' WHERE id='$activityID'") | Out-Null
        Sql $database @('-f', '/migrations/042_activity_place_venue.down.sql') | Out-Null
        Sql $database @('-f', '/migrations/042_activity_place_venue.sql') | Out-Null
        Write-Output '[PASS] 042 down and reapply on disposable seeded database'
        Sql $database @('-f', '/migrations/042_activity_place_venue.down.sql') | Out-Null
    }
    if ($Through -ge 41) {
        $businessAccount = (Sql $database @('-qAtc', "INSERT INTO accounts(id,account_type)
            VALUES(gen_random_uuid(),'business') RETURNING id")).Trim()
        if (!$businessAccount) { throw 'Business principal guard setup failed' }
        Sql $database @('-c', "INSERT INTO businesses(account_id,name) VALUES('$businessAccount','Synthetic rollback guard')") | Out-Null
        $businessGuard = (& docker compose exec -T db psql -v ON_ERROR_STOP=1 -U birdtie -d $database -f /migrations/041_business_principals.down.sql 2>&1 | Out-String)
        if ($LASTEXITCODE -eq 0 -or $businessGuard -notmatch 'cannot remove Business principals') {
            throw '041 down did not protect Business principal data'
        }
        Write-Output '[PASS] 041 down refuses to erase Business principal data'
        Sql $database @('-c', "DELETE FROM businesses WHERE account_id='$businessAccount'") | Out-Null
        Sql $database @('-c', "DELETE FROM accounts WHERE id='$businessAccount'") | Out-Null
        Sql $database @('-f', '/migrations/041_business_principals.down.sql') | Out-Null
        Sql $database @('-f', '/migrations/041_business_principals.sql') | Out-Null
        Write-Output '[PASS] 041 down and reapply on disposable seeded database'
        Sql $database @('-f', '/migrations/041_business_principals.down.sql') | Out-Null
    }
    if ($Through -ge 40) {
        $candidateID = (Sql $database @('-qAtc', "INSERT INTO venue_candidates
            (place_id,city_id,submitted_by,capacity,reservation_support,source_url,rights_note,expires_at)
            SELECT p.id,p.city_id,a.id,24,'contact','https://example.org/synthetic-venue',
                   'Synthetic rollback guard',now()+interval '2 days'
            FROM places p CROSS JOIN LATERAL
                (SELECT id FROM accounts WHERE account_type='person' ORDER BY id LIMIT 1) a
            WHERE p.publication_status='published' ORDER BY p.id LIMIT 1 RETURNING id")).Trim()
        if (!$candidateID) { throw 'no Place for Venue down guard' }
        $venueGuard = (& docker compose exec -T db psql -v ON_ERROR_STOP=1 -U birdtie -d $database -f /migrations/040_venue_capabilities.down.sql 2>&1 | Out-String)
        if ($LASTEXITCODE -eq 0 -or $venueGuard -notmatch 'cannot remove sourced Venue capabilities') {
            throw '040 down did not protect sourced Venue candidate'
        }
        Write-Output '[PASS] 040 down refuses to erase sourced Venue candidate'
        Sql $database @('-c', "DELETE FROM venue_candidates WHERE id='$candidateID'") | Out-Null
        Sql $database @('-f', '/migrations/040_venue_capabilities.down.sql') | Out-Null
        Sql $database @('-f', '/migrations/040_venue_capabilities.sql') | Out-Null
        Write-Output '[PASS] 040 down and reapply on disposable seeded database'
        if ($Through -ge 41) {
            Sql $database @('-f', '/migrations/041_business_principals.sql') | Out-Null
        }
    }
    $placeID = (Sql $database @('-qAtc', "SELECT id FROM places WHERE location_precision='point' ORDER BY id LIMIT 1")).Trim()
    if (!$placeID) { throw 'no point Place for address guard' }
    Sql $database @('-c', "UPDATE places SET address_label='Synthetic reviewed address' WHERE id='$placeID'") | Out-Null
    $guard = (& docker compose exec -T db psql -v ON_ERROR_STOP=1 -U birdtie -d $database -f /migrations/039_place_context_address.down.sql 2>&1 | Out-String)
    if ($LASTEXITCODE -eq 0 -or $guard -notmatch 'cannot remove reviewed Place addresses') {
        throw '039 down did not protect sourced addresses'
    }
    Write-Output '[PASS] 039 down refuses to erase reviewed address'
    Sql $database @('-c', "UPDATE places SET address_label=NULL WHERE id='$placeID'") | Out-Null
    Sql $database @('-f', '/migrations/039_place_context_address.down.sql') | Out-Null
    Sql $database @('-f', '/migrations/039_place_context_address.sql') | Out-Null
    Write-Output '[PASS] 039 down and reapply on disposable seeded database'
} finally {
    try { Sql 'postgres' @('-c', "DROP DATABASE IF EXISTS $database WITH (FORCE)") | Out-Null }
    finally { Pop-Location }
}
