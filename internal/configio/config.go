package configio

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/mishankov/camunda-stub-worker/internal/domain"
)

const SchemaVersion = 1

type Document struct {
	SchemaVersion int                    `json:"schemaVersion"`
	Profile       domain.Profile         `json:"profile"`
	JobTypes      []domain.JobTypeConfig `json:"jobTypes"`
	Scenarios     []domain.Scenario      `json:"scenarios"`
}

func Encode(p domain.Profile, types []domain.JobTypeConfig, scenarios []domain.Scenario) ([]byte, error) {
	d := Document{SchemaVersion: SchemaVersion, Profile: p, JobTypes: types, Scenarios: scenarios}
	return json.MarshalIndent(d, "", "  ")
}
func Decode(raw []byte) (Document, error) {
	var d Document
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&d); err != nil {
		return d, fmt.Errorf("invalid import file: %w", err)
	}
	if d.SchemaVersion != SchemaVersion {
		return d, fmt.Errorf("unsupported schemaVersion: %d", d.SchemaVersion)
	}
	if err := domain.ValidateProfile(d.Profile); err != nil {
		return d, fmt.Errorf("profile: %w", err)
	}
	typeIDs := map[string]bool{}
	for _, t := range d.JobTypes {
		if err := domain.ValidateJobType(t); err != nil {
			return d, fmt.Errorf("job type %q: %w", t.JobType, err)
		}
		if t.ProfileID != d.Profile.ID {
			return d, errors.New("job type references a different profile")
		}
		if typeIDs[t.ID] {
			return d, errors.New("duplicate job-type UUID")
		}
		typeIDs[t.ID] = true
	}
	scenarioIDs := map[string]bool{}
	for _, s := range d.Scenarios {
		if err := domain.ValidateScenario(s); err != nil {
			return d, fmt.Errorf("scenario %q: %w", s.Name, err)
		}
		if !typeIDs[s.JobTypeConfigID] {
			return d, fmt.Errorf("scenario %q references an unknown job type", s.Name)
		}
		if scenarioIDs[s.ID] {
			return d, errors.New("duplicate scenario UUID")
		}
		scenarioIDs[s.ID] = true
	}
	for _, t := range d.JobTypes {
		if t.ActiveScenarioID != nil && !scenarioIDs[*t.ActiveScenarioID] {
			return d, fmt.Errorf("active scenario for job type %q is missing", t.JobType)
		}
	}
	return d, nil
}
