-- Network context of a web capture (HTTP, TLS, DNS, redirects), kept for display
-- and search. The authoritative, hashed copy is the capture's metadata artifact.
ALTER TABLE evidence ADD COLUMN capture jsonb;
