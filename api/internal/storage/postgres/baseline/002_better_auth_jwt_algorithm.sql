-- BetterAuth 1.6 JWT key metadata. Both fields are optional strings; NULL
-- preserves the plugin's legacy-key fallback to its configured algorithm.
ALTER TABLE jwks ADD COLUMN alg text;
ALTER TABLE jwks ADD COLUMN crv text;
