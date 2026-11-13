-- Embedded metadata (EXIF, XMP, PDF info, Office properties, text encoding...)
-- extracted from original files by the sandboxed metadata service.
-- One row per artifact, written once. `fields` is stored as text, not jsonb,
-- so the bytes whose SHA-256 is recorded in the custody log stay exact.
CREATE TABLE artifact_metadata (
    artifact_id   uuid PRIMARY KEY REFERENCES artifacts (id),
    status        text NOT NULL CHECK (status IN ('extracted', 'failed', 'skipped')),
    tool          text NOT NULL,
    tool_version  text NOT NULL DEFAULT '',
    fields        text,
    fields_sha256 char(64),
    field_count   integer NOT NULL DEFAULT 0,
    error         text NOT NULL DEFAULT '',
    extracted_at  timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER artifact_metadata_immutable BEFORE UPDATE OR DELETE ON artifact_metadata
    FOR EACH ROW EXECUTE FUNCTION lumen_forbid_mutation();
