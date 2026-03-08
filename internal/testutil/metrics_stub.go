package testutil

import (
	"sync"
	"time"

	"github.com/pdrhp/gitlab-elt/internal/domain"
)

// MetricsStub is a thread-safe recorder for MetricsRecorder invocations in tests.
type MetricsStub struct {
	mu sync.Mutex

	syncRuns          []SyncRunCall
	gitlabRequests    []GitLabRequestCall
	rawEvents         []RawEventCall
	transformResults  []TransformResultCall
	lastSuccessful    []LastSuccessfulSyncCall
	projectsMonitored []ProjectsMonitoredCall
}

type SyncRunCall struct {
	Stage    string
	Status   string
	Duration time.Duration
}

type GitLabRequestCall struct {
	Endpoint string
	Status   string
	Duration time.Duration
}

type RawEventCall struct {
	ProjectID string
	EventType string
	Count     int
}

type TransformResultCall struct {
	ProjectID string
	EventType string
	Status    string
}

type LastSuccessfulSyncCall struct {
	ProjectID string
	Stage     string
	Timestamp time.Time
}

type ProjectsMonitoredCall struct {
	Value float64
}

// NewMetricsStub returns an empty metrics stub.
func NewMetricsStub() *MetricsStub {
	return &MetricsStub{}
}

func (m *MetricsStub) RecordSyncRun(stage, status string, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.syncRuns = append(m.syncRuns, SyncRunCall{Stage: stage, Status: status, Duration: duration})
}

func (m *MetricsStub) RecordGitLabRequest(endpoint, status string, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gitlabRequests = append(m.gitlabRequests, GitLabRequestCall{Endpoint: endpoint, Status: status, Duration: duration})
}

func (m *MetricsStub) RecordRawEventIngested(projectID, eventType string, count int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rawEvents = append(m.rawEvents, RawEventCall{ProjectID: projectID, EventType: eventType, Count: count})
}

func (m *MetricsStub) RecordTransformResult(projectID, eventType, status string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.transformResults = append(m.transformResults, TransformResultCall{ProjectID: projectID, EventType: eventType, Status: status})
}

func (m *MetricsStub) RecordLastSuccessfulSync(projectID, stage string, ts time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastSuccessful = append(m.lastSuccessful, LastSuccessfulSyncCall{ProjectID: projectID, Stage: stage, Timestamp: ts})
}

func (m *MetricsStub) SetProjectsMonitored(n float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.projectsMonitored = append(m.projectsMonitored, ProjectsMonitoredCall{Value: n})
}

// Accessors for tests.
func (m *MetricsStub) SyncRuns() []SyncRunCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]SyncRunCall(nil), m.syncRuns...)
}

func (m *MetricsStub) GitLabRequests() []GitLabRequestCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]GitLabRequestCall(nil), m.gitlabRequests...)
}

func (m *MetricsStub) RawEvents() []RawEventCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]RawEventCall(nil), m.rawEvents...)
}

func (m *MetricsStub) TransformResults() []TransformResultCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]TransformResultCall(nil), m.transformResults...)
}

func (m *MetricsStub) LastSuccessfulSyncs() []LastSuccessfulSyncCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]LastSuccessfulSyncCall(nil), m.lastSuccessful...)
}

func (m *MetricsStub) ProjectsMonitored() []ProjectsMonitoredCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]ProjectsMonitoredCall(nil), m.projectsMonitored...)
}

var _ domain.MetricsRecorder = (*MetricsStub)(nil)
