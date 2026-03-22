-- NovelBuilder Database Schema - Part 14: Grant quarantine_zone permissions
-- Grants the application user full access to the quarantine_zone schema
-- so that routes_analysis.py can insert plot_elements without InsufficientPrivilege errors.

GRANT USAGE ON SCHEMA quarantine_zone TO novelbuilder;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA quarantine_zone TO novelbuilder;
ALTER DEFAULT PRIVILEGES IN SCHEMA quarantine_zone
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO novelbuilder;
