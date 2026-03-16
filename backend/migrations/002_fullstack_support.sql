-- Migration 002: Add Full Stack deployment support
ALTER TABLE projects ADD COLUMN backend_working_directory TEXT DEFAULT '';
ALTER TABLE projects ADD COLUMN backend_install_command TEXT DEFAULT '';
ALTER TABLE projects ADD COLUMN backend_build_command TEXT DEFAULT '';

-- Update schema version
UPDATE schema_version SET version = 2;
