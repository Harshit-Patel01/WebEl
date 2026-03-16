-- Migration 003: Add domain tracking, deployment target, and ensure all fullstack columns exist
-- Note: SQLite will error if column already exists, but migration system should handle this gracefully

-- Try to add backend columns (these might already exist from migration 002)
-- If they exist, these will fail but we continue with the new columns
ALTER TABLE projects ADD COLUMN backend_working_directory TEXT DEFAULT '';
ALTER TABLE projects ADD COLUMN backend_install_command TEXT DEFAULT '';
ALTER TABLE projects ADD COLUMN backend_build_command TEXT DEFAULT '';

-- Add new columns for this migration
ALTER TABLE projects ADD COLUMN domain TEXT DEFAULT '';
ALTER TABLE projects ADD COLUMN deployment_target TEXT DEFAULT 'local';

-- Update schema version
UPDATE schema_version SET version = 3;
