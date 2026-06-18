CREATE TABLE platform_settings (
    id                       TEXT PRIMARY KEY DEFAULT 'platform-global',
    usage_collection_enabled BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);
