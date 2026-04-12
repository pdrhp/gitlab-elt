-- Migration: 000017_assignee_cycle_time
-- Rollback: Drop assignee cycle time views

DROP VIEW IF EXISTS vw_individual_performance_metrics;
DROP VIEW IF EXISTS vw_assignee_cycle_time;
