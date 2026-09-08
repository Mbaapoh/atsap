Repository: /home/mbaapoh/atsap. Work ONLY in that repo.

TASKS 7.1 and 7.2 from openspec/changes/pbx-extensions-projection/tasks.md.
Task 7.3 is NOT yours — it is Go configuration work handled separately.

## What you are doing

Making Asterisk read its PJSIP configuration from the atsapbx database
instead of only from files, so an extension created through the API is
registrable with no reload (DECISIONS D-47).

## Files to create (only these three)

core/conf/res_pgsql.conf
core/conf/extconfig.conf
core/conf/sorcery.conf

## res_pgsql.conf

Connects Asterisk to the APPLICATION database as the restricted engine
role. Note this is the `atsapbx` database, NOT the `asterisk` CDR/CEL
database the existing cdr_pgsql.conf points at — they are different
databases on the same server.

    [general]
    dbhost=postgres
    dbport=5432
    dbname=atsapbx
    dbuser=asterisk_engine
    dbpass=devpassword123
    requirements=warn

Follow the comment style of the existing core/conf/cdr_pgsql.conf: state
that these credentials are local-development only and that production
ships a separate conf.prod/ variant.

## extconfig.conf

Maps four tables. All four, including ps_contacts — omitting it makes
registration silently fail to bind:

    [settings]
    ps_endpoints => pgsql,atsapbx,ps_endpoints
    ps_auths => pgsql,atsapbx,ps_auths
    ps_aors => pgsql,atsapbx,ps_aors
    ps_contacts => pgsql,atsapbx,ps_contacts

## sorcery.conf — READ THIS CAREFULLY

    [res_pjsip]
    endpoint=config,pjsip.conf,criteria=type=endpoint
    endpoint=realtime,ps_endpoints
    auth=config,pjsip.conf,criteria=type=auth
    auth=realtime,ps_auths
    aor=config,pjsip.conf,criteria=type=aor
    aor=realtime,ps_aors
    contact=realtime,ps_contacts

The three `config,pjsip.conf` lines are NOT optional and must not be
removed as redundant. Adding a realtime wizard REPLACES the default
config-file wizard rather than supplementing it. Without those lines the
existing dev fixtures 1000 and 1001 disappear from Asterisk, and the
telephony-core end-to-end test — which dials them — breaks. This was
observed during the D-47 spike; it is the single most likely way to get
this task wrong.

## Verification — do all of it, report raw output

Use this Go path if you need it (mise is permitted but this is simpler):
/home/mbaapoh/.local/share/mise/installs/go/1.25.14/bin/go

1. Rebuild and restart Asterisk:
       cd /home/mbaapoh/atsap
       docker compose -f deploy/docker-compose.yml build asterisk
       docker compose -f deploy/docker-compose.yml up -d asterisk
       sleep 30

2. Database connection is live — must report a connection, not an error:
       docker exec deploy-asterisk-1 asterisk -rx "realtime show pgsql status"

3. The dev fixtures SURVIVED — both 1000 and 1001 must be listed:
       docker exec deploy-asterisk-1 asterisk -rx "pjsip show endpoints"

4. No realtime errors in the log — this must print nothing:
       docker logs deploy-asterisk-1 2>&1 | grep -iE "pgsql.*(error|failed)" | tail -20

5. The sipua sidecar re-registers after the restart:
       docker restart deploy-sipua-1 && sleep 10
       docker exec deploy-asterisk-1 asterisk -rx "pjsip show endpoints" | grep Contact

## Hard constraints — a violation fails the task

- Do NOT edit core/conf/pjsip.conf. The 1000/1001 fixtures stay exactly
  as they are.
- Do NOT edit anything under api/. No Go code is part of this task.
- Do NOT edit the migrations.
- Do NOT change deploy/docker-compose.yml.
- If a verification step fails, REPORT the failure with its raw output.
  Do not work around it, and do not remove a config line to make an error
  go away.

## Report

Paste the raw output of all five verification steps. Do not summarise
them. If step 3 does not list both 1000 and 1001, say so plainly — that
is the failure this task most needs to catch.
