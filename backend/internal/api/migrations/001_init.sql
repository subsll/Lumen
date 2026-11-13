-- Lumen schema v1

CREATE FUNCTION lumen_join(text[]) RETURNS text
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
    AS $$ SELECT array_to_string($1, ' ') $$;

CREATE FUNCTION lumen_forbid_mutation() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'Lumen: % on % is not permitted (append-only evidence record)', TG_OP, TG_TABLE_NAME;
END $$;

CREATE TABLE settings (
    key   text PRIMARY KEY,
    value text NOT NULL
);

INSERT INTO settings (key, value) VALUES
    ('investigator_name', ''),
    ('organization', ''),
    ('case_prefix', 'LUM');

CREATE TABLE cases (
    id          uuid PRIMARY KEY,
    reference   text NOT NULL UNIQUE,
    title       text NOT NULL,
    description text NOT NULL DEFAULT '',
    status      text NOT NULL DEFAULT 'intake'
                CHECK (status IN ('intake', 'active', 'review', 'closed', 'archived')),
    priority    text NOT NULL DEFAULT 'medium'
                CHECK (priority IN ('low', 'medium', 'high', 'critical')),
    tags        text[] NOT NULL DEFAULT '{}',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    search      tsvector GENERATED ALWAYS AS (
                    to_tsvector('simple', reference || ' ' || title || ' ' || description || ' ' || lumen_join(tags))
                ) STORED
);
CREATE INDEX cases_search_idx ON cases USING gin (search);
CREATE TRIGGER cases_no_delete BEFORE DELETE ON cases
    FOR EACH ROW EXECUTE FUNCTION lumen_forbid_mutation();

CREATE TABLE evidence (
    id                 uuid PRIMARY KEY,
    case_id            uuid NOT NULL REFERENCES cases (id),
    number             integer NOT NULL,
    kind               text NOT NULL CHECK (kind IN ('file', 'web_capture')),
    title              text NOT NULL,
    description        text NOT NULL DEFAULT '',
    source             text NOT NULL DEFAULT '',
    acquisition_method text NOT NULL DEFAULT '',
    acquired_at        timestamptz NOT NULL,
    location           text NOT NULL DEFAULT '',
    tags               text[] NOT NULL DEFAULT '{}',
    integrity_status   text NOT NULL DEFAULT 'unverified'
                       CHECK (integrity_status IN ('unverified', 'verified', 'failed')),
    last_verified_at   timestamptz,
    created_at         timestamptz NOT NULL DEFAULT now(),
    search             tsvector GENERATED ALWAYS AS (
                           to_tsvector('simple', title || ' ' || description || ' ' || source || ' ' || lumen_join(tags))
                       ) STORED,
    UNIQUE (case_id, number)
);
CREATE INDEX evidence_case_idx ON evidence (case_id);
CREATE INDEX evidence_search_idx ON evidence USING gin (search);
CREATE TRIGGER evidence_no_delete BEFORE DELETE ON evidence
    FOR EACH ROW EXECUTE FUNCTION lumen_forbid_mutation();

-- Artifacts are the stored bytes of an evidence item. They never change.
CREATE TABLE artifacts (
    id            uuid PRIMARY KEY,
    evidence_id   uuid NOT NULL REFERENCES evidence (id),
    role          text NOT NULL,
    filename      text NOT NULL,
    mime_type     text NOT NULL,
    size_bytes    bigint NOT NULL,
    sha256        char(64) NOT NULL,
    sha1          char(40) NOT NULL,
    md5           char(32) NOT NULL,
    client_sha256 char(64),
    created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX artifacts_evidence_idx ON artifacts (evidence_id);
CREATE INDEX artifacts_sha256_idx ON artifacts (sha256);
CREATE INDEX artifacts_sha1_idx ON artifacts (sha1);
CREATE INDEX artifacts_md5_idx ON artifacts (md5);
CREATE TRIGGER artifacts_immutable BEFORE UPDATE OR DELETE ON artifacts
    FOR EACH ROW EXECUTE FUNCTION lumen_forbid_mutation();

CREATE TABLE case_notes (
    id         uuid PRIMARY KEY,
    case_id    uuid NOT NULL REFERENCES cases (id),
    body       text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX case_notes_case_idx ON case_notes (case_id);
CREATE TRIGGER case_notes_immutable BEFORE UPDATE OR DELETE ON case_notes
    FOR EACH ROW EXECUTE FUNCTION lumen_forbid_mutation();

-- The chain of custody: append-only and hash-chained (see internal/custody).
CREATE TABLE custody_events (
    seq         bigint PRIMARY KEY,
    id          uuid NOT NULL UNIQUE,
    case_id     uuid REFERENCES cases (id),
    evidence_id uuid REFERENCES evidence (id),
    action      text NOT NULL,
    actor       text NOT NULL,
    details     text NOT NULL,
    occurred_at timestamptz NOT NULL,
    prev_hash   char(64) NOT NULL,
    hash        char(64) NOT NULL UNIQUE
);
CREATE INDEX custody_case_idx ON custody_events (case_id, seq);
CREATE INDEX custody_evidence_idx ON custody_events (evidence_id, seq);
CREATE TRIGGER custody_immutable BEFORE UPDATE OR DELETE ON custody_events
    FOR EACH ROW EXECUTE FUNCTION lumen_forbid_mutation();
CREATE TRIGGER custody_no_truncate BEFORE TRUNCATE ON custody_events
    FOR EACH STATEMENT EXECUTE FUNCTION lumen_forbid_mutation();
