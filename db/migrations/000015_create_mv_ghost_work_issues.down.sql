-- Migration: 000015_create_mv_ghost_work_issues (down)
-- Drop materialized view for ghost-work issues

DROP MATERIALIZED VIEW IF EXISTS mv_ghost_work_issues;
