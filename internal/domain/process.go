package domain

import (
	"errors"
	"strings"
)

type StartProcessRequest struct {
	ProfileID     string `json:"profileId"`
	BPMNProcessID string `json:"bpmnProcessId"`
	Version       int32  `json:"version"` // Zero selects the latest deployed version.
	VariablesJSON string `json:"variablesJson"`
}

type ProcessInstance struct {
	BPMNProcessID        string `json:"bpmnProcessId"`
	Version              int32  `json:"version"`
	ProcessDefinitionKey string `json:"processDefinitionKey"`
	ProcessInstanceKey   string `json:"processInstanceKey"`
}

func ValidateStartProcess(r StartProcessRequest) error {
	if strings.TrimSpace(r.BPMNProcessID) == "" || r.BPMNProcessID != strings.TrimSpace(r.BPMNProcessID) {
		return errors.New("enter a BPMN process ID without leading or trailing whitespace")
	}
	if r.Version < 0 {
		return errors.New("process version must be positive, or zero for latest")
	}
	return ValidateJSONObject(r.VariablesJSON)
}
