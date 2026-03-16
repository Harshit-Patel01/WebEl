-- Migration 003: Add domain tracking and deployment target
ALTER TABLE projects ADD COLUMN domain TEXT DEFAULT '';
ALTER TABLE projects ADD COLUMN deployment_target TEXT DEFAULT 'local';

-- Update schema version
UPDATE schema_version SET version = 3;
