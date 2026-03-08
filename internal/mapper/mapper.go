package mapper

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"

	"github.com/pdrhp/gitlab-elt/internal/repository"
)

// LabelType categorizes what a GitLab label represents.
type LabelType int

const (
	LabelTypeState    LabelType = iota // Workflow state (Backlog, Em dev, etc.)
	LabelTypeMetadata                  // Categorical metadata (Bug, Priority, etc.)
	LabelTypeUnknown                   // Not mapped
)

// Mapper translates GitLab label names to canonical states or metadata keys.
// Thread-safe via RWMutex for concurrent reads.
type Mapper struct {
	mu               sync.RWMutex
	stateMappings    map[string]string // label_name -> canonical_state
	metadataMappings map[string]string // label_name -> metadata_key
}

// New creates a new empty Mapper.
func New() *Mapper {
	return &Mapper{
		stateMappings:    make(map[string]string),
		metadataMappings: make(map[string]string),
	}
}

// normalizeLabel normalizes a label by:
// - Converting to lowercase
// - Removing accents/diacritics
// - Trimming whitespace
func normalizeLabel(label string) string {
	// Convert to lowercase
	normalized := strings.ToLower(label)

	// Remove accents/diacritics using NFKD decomposition
	t := transform.Chain(norm.NFKD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	normalized, _, _ = transform.String(t, normalized)

	// Trim whitespace
	normalized = strings.TrimSpace(normalized)

	return normalized
}

// LoadFromDB loads all mappings from the database into the in-memory cache.
func (m *Mapper) LoadFromDB(ctx context.Context, queries *repository.Queries) error {
	stateMappings, err := queries.ListStateMappings(ctx)
	if err != nil {
		return err
	}

	metadataMappings, err := queries.ListMetadataMappings(ctx)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.stateMappings = make(map[string]string, len(stateMappings))
	for _, sm := range stateMappings {
		normalizedLabel := normalizeLabel(sm.GitlabLabelName)
		m.stateMappings[normalizedLabel] = sm.CanonicalState
	}

	m.metadataMappings = make(map[string]string, len(metadataMappings))
	for _, mm := range metadataMappings {
		normalizedLabel := normalizeLabel(mm.GitlabLabelName)
		m.metadataMappings[normalizedLabel] = mm.MetadataKey
	}

	slog.Info("mapper: loaded mappings",
		"state_mappings", len(m.stateMappings),
		"metadata_mappings", len(m.metadataMappings),
	)
	return nil
}

// AddStateMapping adds a label → canonical state mapping (used in tests and manual setup).
func (m *Mapper) AddStateMapping(label, canonicalState string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	normalizedLabel := normalizeLabel(label)
	m.stateMappings[normalizedLabel] = canonicalState
}

// AddMetadataMapping adds a label → metadata key mapping.
func (m *Mapper) AddMetadataMapping(label, metadataKey string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	normalizedLabel := normalizeLabel(label)
	m.metadataMappings[normalizedLabel] = metadataKey
}

// MapLabel returns the canonical state and the label type.
// For state labels: returns (canonical_state, LabelTypeState)
// For metadata labels: returns ("", LabelTypeMetadata)
// For unknown labels: returns ("UNKNOWN", LabelTypeUnknown)
func (m *Mapper) MapLabel(label string) (string, LabelType) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	normalizedLabel := normalizeLabel(label)

	if state, ok := m.stateMappings[normalizedLabel]; ok {
		return state, LabelTypeState
	}
	if _, ok := m.metadataMappings[normalizedLabel]; ok {
		return "", LabelTypeMetadata
	}
	return "UNKNOWN", LabelTypeUnknown
}

// MetadataKey returns the metadata key for a label, or empty string if not a metadata label.
func (m *Mapper) MetadataKey(label string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	normalizedLabel := normalizeLabel(label)
	return m.metadataMappings[normalizedLabel]
}

// IsStateLabel returns true if the label is a workflow state label.
func (m *Mapper) IsStateLabel(label string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	normalizedLabel := normalizeLabel(label)
	_, ok := m.stateMappings[normalizedLabel]
	return ok
}

// StateFor returns the canonical state for a label, or empty string if not a state label.
func (m *Mapper) StateFor(label string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	normalizedLabel := normalizeLabel(label)
	return m.stateMappings[normalizedLabel]
}
