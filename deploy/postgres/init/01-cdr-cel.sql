-- Schema for Asterisk's cdr_pgsql and cel_pgsql backends.
-- Column names/types follow what those modules expect by default
-- (see core/conf/cdr_pgsql.conf and cel_pgsql.conf).

CREATE TABLE IF NOT EXISTS cdr (
    id           bigserial PRIMARY KEY,
    calldate     timestamp NOT NULL DEFAULT now(),
    clid         varchar(80)  NOT NULL DEFAULT '',
    src          varchar(80)  NOT NULL DEFAULT '',
    dst          varchar(80)  NOT NULL DEFAULT '',
    dcontext     varchar(80)  NOT NULL DEFAULT '',
    channel      varchar(80)  NOT NULL DEFAULT '',
    dstchannel   varchar(80)  NOT NULL DEFAULT '',
    lastapp      varchar(80)  NOT NULL DEFAULT '',
    lastdata     varchar(80)  NOT NULL DEFAULT '',
    duration     integer      NOT NULL DEFAULT 0,
    billsec      integer      NOT NULL DEFAULT 0,
    disposition  varchar(45)  NOT NULL DEFAULT '',
    amaflags     integer      NOT NULL DEFAULT 0,
    accountcode  varchar(20)  NOT NULL DEFAULT '',
    -- Wide enough for our own self-supplied channel IDs (D-19
    -- correlation strategy, docs/hld/01-architecture.md §2.1), e.g.
    -- "atsa-part-<uuid>" (46 chars) — Asterisk's default short numeric
    -- uniqueid fits easily too. A too-narrow column here doesn't just
    -- drop the CDR/CEL row: the insert failure loop it causes was
    -- observed hanging up the live channel almost immediately after
    -- answer (task 10.2 walking-skeleton test).
    uniqueid     varchar(64)  NOT NULL DEFAULT '',
    userfield    varchar(255) NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS cdr_uniqueid_idx ON cdr (uniqueid);
CREATE INDEX IF NOT EXISTS cdr_calldate_idx ON cdr (calldate);

CREATE TABLE IF NOT EXISTS cel (
    id           bigserial PRIMARY KEY,
    eventtype    varchar(30)  NOT NULL DEFAULT '',
    eventtime    timestamp    NOT NULL DEFAULT now(),
    cid_name     varchar(80)  NOT NULL DEFAULT '',
    cid_num      varchar(80)  NOT NULL DEFAULT '',
    cid_ani      varchar(80)  NOT NULL DEFAULT '',
    cid_rdnis    varchar(80)  NOT NULL DEFAULT '',
    cid_dnid     varchar(80)  NOT NULL DEFAULT '',
    exten        varchar(80)  NOT NULL DEFAULT '',
    context      varchar(80)  NOT NULL DEFAULT '',
    channame     varchar(80)  NOT NULL DEFAULT '',
    appname      varchar(80)  NOT NULL DEFAULT '',
    appdata      varchar(80)  NOT NULL DEFAULT '',
    amaflags     integer      NOT NULL DEFAULT 0,
    accountcode  varchar(20)  NOT NULL DEFAULT '',
    -- Same widening as cdr.uniqueid above, plus linkedid (also set from
    -- a channel's uniqueid for a linked call).
    uniqueid     varchar(64)  NOT NULL DEFAULT '',
    linkedid     varchar(64)  NOT NULL DEFAULT '',
    userdeftype  varchar(255) NOT NULL DEFAULT '',
    peer         varchar(80)  NOT NULL DEFAULT '',
    peeraccount  varchar(20)  NOT NULL DEFAULT '',
    extra        varchar(235) NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS cel_uniqueid_idx ON cel (uniqueid);
CREATE INDEX IF NOT EXISTS cel_eventtime_idx ON cel (eventtime);
