package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/pdrhp/gitlab-elt/internal/domain"
	"github.com/pdrhp/gitlab-elt/internal/repository"
)

// Service discovers GitLab projects from configured groups and persists them.
type Service struct {
	gitlab   domain.GitlabClient
	queries  discoveryQueries
	groupIDs []int
	metrics  domain.MetricsRecorder
	tracer   trace.Tracer
}

type discoveryQueries interface {
	GetProject(ctx context.Context, id int32) (repository.Project, error)
	UpsertProject(ctx context.Context, arg repository.UpsertProjectParams) (repository.Project, error)
	UpsertRawProject(ctx context.Context, arg repository.UpsertRawProjectParams) (repository.RawProject, error)
}

// NewService creates a new Discovery Service.
func NewService(gitlab domain.GitlabClient, queries discoveryQueries, groupIDs []int, metrics domain.MetricsRecorder, tracer trace.Tracer) *Service {
	if tracer == nil {
		tracer = noop.NewTracerProvider().Tracer("github.com/pdrhp/gitlab-elt/internal/discovery")
	}
	return &Service{
		gitlab:   gitlab,
		queries:  queries,
		groupIDs: groupIDs,
		metrics:  metrics,
		tracer:   tracer,
	}
}

// Run executes a full discovery cycle: fetch projects from all groups, upsert into Bronze + Silver.
func (s *Service) Run(ctx context.Context) error {
	ctx, span := s.tracer.Start(ctx, "discovery.Run",
		trace.WithAttributes(
			attribute.Int("groups.count", len(s.groupIDs)),
		),
	)
	defer span.End()

	start := time.Now()
	status := "success"
	defer func() {
		if s.metrics == nil {
			return
		}
		s.metrics.RecordSyncRun("discovery", status, time.Since(start))
	}()

	slog.Info("discovery: starting project discovery", "groups", len(s.groupIDs))

	projects, err := s.fetchAllGroupProjects(ctx)
	if err != nil {
		status = "error"
		span.RecordError(err)
		return fmt.Errorf("discovery: fetch projects: %w", err)
	}

	if s.queries == nil {
		slog.Warn("discovery: no repository configured, skipping persistence")
		return nil
	}

	persisted := 0
	var hadPersistError bool
	for _, p := range projects {
		if err := s.persistProject(ctx, p); err != nil {
			hadPersistError = true
			slog.Error("discovery: failed to persist project",
				"project_id", p.ID,
				"project_name", p.Name,
				"error", err,
			)
			continue
		}
		persisted++
		if s.metrics != nil {
			s.metrics.RecordLastSuccessfulSync(strconv.Itoa(p.ID), "discovery", time.Now().UTC())
		}
	}
	if s.metrics != nil {
		s.metrics.SetProjectsMonitored(float64(persisted))
	}
	if hadPersistError {
		status = "error"
	}

	span.SetAttributes(
		attribute.Int("projects.discovered", len(projects)),
		attribute.Int("projects.persisted", persisted),
	)

	slog.Info("discovery: completed",
		"discovered", len(projects),
		"persisted", persisted,
	)
	return nil
}

// fetchAllGroupProjects fetches projects from all configured groups, deduplicating by ID.
func (s *Service) fetchAllGroupProjects(ctx context.Context) ([]domain.GitlabProject, error) {
	seen := make(map[int]bool)
	var result []domain.GitlabProject

	for _, gid := range s.groupIDs {
		projects, err := s.fetchGroupProjects(ctx, gid)
		if err != nil {
			return nil, fmt.Errorf("group %d: %w", gid, err)
		}
		for _, p := range projects {
			if !seen[p.ID] {
				seen[p.ID] = true
				result = append(result, p)
			}
		}
	}
	return result, nil
}

// fetchGroupProjects fetches all projects from a single group.
func (s *Service) fetchGroupProjects(ctx context.Context, groupID int) ([]domain.GitlabProject, error) {
	projects, err := s.gitlab.ListGroupProjects(ctx, groupID)
	if err != nil {
		return nil, err
	}
	slog.Info("discovery: fetched projects from group",
		"group_id", groupID,
		"count", len(projects),
	)
	return projects, nil
}

// persistProject upserts a project into both Bronze (raw_projects) and Silver (projects).
func (s *Service) persistProject(ctx context.Context, p domain.GitlabProject) error {
	// Marshal project as raw metadata for Bronze
	rawMeta, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshal raw metadata: %w", err)
	}

	epoch := pgtype.Timestamptz{
		Time:  time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC),
		Valid: true,
	}

	silverLastSyncedAt := epoch
	existingProject, err := s.queries.GetProject(ctx, int32(p.ID))
	if err == nil && existingProject.LastSyncedAt.Valid {
		silverLastSyncedAt = existingProject.LastSyncedAt
	} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("get existing silver project: %w", err)
	}

	// Bronze: upsert raw_projects
	_, err = s.queries.UpsertRawProject(ctx, repository.UpsertRawProjectParams{
		ID:           int32(p.ID),
		Name:         p.Name,
		Path:         p.PathWithNamespace,
		RawMetadata:  rawMeta,
		LastSyncedAt: epoch,
	})
	if err != nil {
		return fmt.Errorf("upsert raw_project: %w", err)
	}

	// Silver: upsert projects
	_, err = s.queries.UpsertProject(ctx, repository.UpsertProjectParams{
		ID:           int32(p.ID),
		Name:         p.Name,
		Path:         p.PathWithNamespace,
		LastSyncedAt: silverLastSyncedAt,
	})
	if err != nil {
		return fmt.Errorf("upsert project: %w", err)
	}

	return nil
}
