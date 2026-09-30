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

// ProcessTask is a service task discovered in a BPMN model.
type ProcessTask struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	JobType string `json:"jobType"`
	Dynamic bool   `json:"dynamic"`
}

// Deployment is the result of a Zeebe resource deployment.
type Deployment struct {
	DeploymentKey string            `json:"deploymentKey"`
	Processes     []DeployedProcess `json:"processes"`
}

type DeployedProcess struct {
	BPMNProcessID        string `json:"bpmnProcessId"`
	Version              int32  `json:"version"`
	ProcessDefinitionKey string `json:"processDefinitionKey"`
}

// DeployResponse describes a BPMN model deployed from a local file: the
// deployed process versions plus the worker tasks parsed from the file.
type DeployResponse struct {
	FileName   string                `json:"fileName"`
	Deployment string                `json:"deploymentKey"`
	Processes  []DeployedProcessInfo `json:"processes"`
}

type DeployedProcessInfo struct {
	BPMNProcessID        string        `json:"bpmnProcessId"`
	Name                 string        `json:"name"`
	Version              int32         `json:"version"`
	ProcessDefinitionKey string        `json:"processDefinitionKey"`
	Tasks                []ProcessTask `json:"tasks"`
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
