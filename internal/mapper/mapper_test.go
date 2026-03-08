package mapper

import (
	"testing"
)

func TestMapLabel_StateMappings(t *testing.T) {
	m := newTestMapper()

	tests := []struct {
		label     string
		wantState string
		wantType  LabelType
	}{
		{"Em dev", "IN_PROGRESS", LabelTypeState},
		{"Backlog", "BACKLOG", LabelTypeState},
		{"Concluido", "DONE", LabelTypeState},
		{"Bloqueado", "BLOCKED", LabelTypeState},
		{"Teste HOM", "QA_REVIEW", LabelTypeState},
		{"Cancelado", "CANCELED", LabelTypeState},
	}

	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			state, labelType := m.MapLabel(tt.label)
			if state != tt.wantState {
				t.Errorf("MapLabel(%q) state = %q, want %q", tt.label, state, tt.wantState)
			}
			if labelType != tt.wantType {
				t.Errorf("MapLabel(%q) type = %v, want %v", tt.label, labelType, tt.wantType)
			}
		})
	}
}

func TestMapLabel_MetadataMappings(t *testing.T) {
	m := newTestMapper()

	tests := []struct {
		label    string
		wantKey  string
		wantType LabelType
	}{
		{"Bug", "tipo", LabelTypeMetadata},
		{"PRIORIDADE: ALTA", "prioridade", LabelTypeMetadata},
		{"Backend", "area", LabelTypeMetadata},
	}

	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			_, labelType := m.MapLabel(tt.label)
			if labelType != tt.wantType {
				t.Errorf("MapLabel(%q) type = %v, want %v", tt.label, labelType, tt.wantType)
			}
			key := m.MetadataKey(tt.label)
			if key != tt.wantKey {
				t.Errorf("MetadataKey(%q) = %q, want %q", tt.label, key, tt.wantKey)
			}
		})
	}
}

func TestMapLabel_Unknown(t *testing.T) {
	m := newTestMapper()

	state, labelType := m.MapLabel("some-random-label")
	if state != "UNKNOWN" {
		t.Errorf("got state=%q, want UNKNOWN", state)
	}
	if labelType != LabelTypeUnknown {
		t.Errorf("got type=%v, want LabelTypeUnknown", labelType)
	}
}

func TestMapLabel_Normalization(t *testing.T) {
	m := newTestMapper()

	// Test case insensitivity
	tests := []struct {
		label     string
		wantState string
	}{
		{"Em dev", "IN_PROGRESS"},     // original
		{"EM DEV", "IN_PROGRESS"},     // uppercase
		{"em dev", "IN_PROGRESS"},     // lowercase
		{"Concluido", "DONE"},         // without accent
		{"Concluído", "DONE"},         // with accent
		{"CONCLUÍDO", "DONE"},         // uppercase with accent
		{"  Em dev  ", "IN_PROGRESS"}, // with whitespace
	}

	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			state, labelType := m.MapLabel(tt.label)
			if state != tt.wantState {
				t.Errorf("MapLabel(%q) state = %q, want %q", tt.label, state, tt.wantState)
			}
			if labelType != LabelTypeState {
				t.Errorf("MapLabel(%q) type = %v, want LabelTypeState", tt.label, labelType)
			}
		})
	}
}

func TestMapLabel_IsStatefulTransition(t *testing.T) {
	m := newTestMapper()

	if !m.IsStateLabel("Em dev") {
		t.Error("expected 'Em dev' to be a state label")
	}
	if m.IsStateLabel("Bug") {
		t.Error("expected 'Bug' NOT to be a state label")
	}
	if m.IsStateLabel("random") {
		t.Error("expected 'random' NOT to be a state label")
	}
}

// newTestMapper creates a Mapper with known test data (mirrors seed data).
func newTestMapper() *Mapper {
	m := New()
	// State mappings (subset of seeds)
	m.AddStateMapping("Backlog", "BACKLOG")
	m.AddStateMapping("Em dev", "IN_PROGRESS")
	m.AddStateMapping("Em Andamento", "IN_PROGRESS")
	m.AddStateMapping("Teste HOM", "QA_REVIEW")
	m.AddStateMapping("Bloqueado", "BLOCKED")
	m.AddStateMapping("Concluido", "DONE")
	m.AddStateMapping("Cancelado", "CANCELED")

	// Metadata mappings (subset of seeds)
	m.AddMetadataMapping("Bug", "tipo")
	m.AddMetadataMapping("PRIORIDADE: ALTA", "prioridade")
	m.AddMetadataMapping("Backend", "area")

	return m
}
