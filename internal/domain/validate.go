package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

func ValidateJSONObject(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return errors.New("JSON cannot be empty")
	}
	dec := json.NewDecoder(bytes.NewBufferString(raw))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	if dec.More() {
		return errors.New("unexpected data after the JSON object")
	}
	var extra any
	if err := dec.Decode(&extra); err == nil {
		return errors.New("unexpected data after the JSON object")
	}
	if _, ok := value.(map[string]any); !ok {
		return errors.New("the JSON root value must be an object")
	}
	return nil
}

func ValidateProfile(p Profile) error {
	if strings.TrimSpace(p.Name) == "" {
		return errors.New("profile name is required")
	}
	if p.Name != strings.TrimSpace(p.Name) {
		return errors.New("profile name must not start or end with whitespace")
	}
	if strings.TrimSpace(p.Host) == "" || p.Host != strings.TrimSpace(p.Host) {
		return errors.New("enter a valid host without leading or trailing whitespace")
	}
	if p.Port < 1 || p.Port > 65535 {
		return errors.New("port must be between 1 and 65535")
	}
	parts := strings.Split(p.SelectedVersion, ".")
	if (len(parts) != 2 && len(parts) != 3) || parts[0] != "8" {
		return errors.New("version must use the 8.x or 8.x.y format")
	}
	for _, part := range parts[1:] {
		if part == "" {
			return errors.New("version must use the 8.x or 8.x.y format")
		}
		if _, err := strconv.ParseUint(part, 10, 16); err != nil {
			return errors.New("minor and patch versions must be non-negative integers")
		}
	}
	if p.CommandTimeoutMS <= 0 || p.ActivationTimeoutMS <= 0 || p.RenewalIntervalMS <= 0 {
		return errors.New("timeouts must be positive")
	}
	if p.RenewalIntervalMS >= p.ActivationTimeoutMS {
		return errors.New("renewal interval must be shorter than the activation timeout")
	}
	if p.MaxActiveJobs <= 0 {
		return errors.New("global active-job limit must be positive")
	}
	if p.HistoryRetentionDays <= 0 {
		return errors.New("history retention must be a positive number of days")
	}
	return nil
}

func ValidateJobType(c JobTypeConfig) error {
	if strings.TrimSpace(c.JobType) == "" {
		return errors.New("job type is required")
	}
	if c.JobType != strings.TrimSpace(c.JobType) {
		return errors.New("job type must not start or end with whitespace")
	}
	if c.Mode != ModeManual && c.Mode != ModeAuto {
		return errors.New("unknown job mode")
	}
	if c.MaxActiveJobs <= 0 {
		return errors.New("job-type limit must be positive")
	}
	return nil
}

func ValidateScenario(s Scenario) error {
	if strings.TrimSpace(s.Name) == "" {
		return errors.New("scenario name is required")
	}
	if s.Name != strings.TrimSpace(s.Name) {
		return errors.New("scenario name must not start or end with whitespace")
	}
	if err := ValidateJSONObject(s.VariablesJSON); err != nil {
		return err
	}
	if s.DelayMS < 0 || s.RetryBackoffMS < 0 || s.RemainingRetries < 0 {
		return errors.New("delays and remainingRetries cannot be negative")
	}
	switch s.Outcome {
	case OutcomeSuccess:
	case OutcomeBusinessError:
		if strings.TrimSpace(s.ErrorCode) == "" {
			return errors.New("errorCode is required for a business error")
		}
	case OutcomeTechnicalFail:
	default:
		return errors.New("unknown scenario outcome")
	}
	return nil
}

func ValidateDraft(d ResponseDraft) error {
	return ValidateScenario(Scenario{Name: "draft", Outcome: d.Outcome, VariablesJSON: d.VariablesJSON, ErrorCode: d.ErrorCode, ErrorMessage: d.ErrorMessage, RemainingRetries: d.RemainingRetries, RetryBackoffMS: d.RetryBackoffMS})
}
