package domain

import "time"

type Profile struct {
	OperateAuthMode       string `json:"operateAuthMode"`
	OperateUsername       string `json:"operateUsername,omitempty"`
	OperatePassword       string `json:"operatePassword,omitempty"`
	OperateToken          string `json:"operateToken,omitempty"`
	OperateURL            string `json:"operateUrl"`
	ID                    string `json:"id"`
	Name                  string `json:"name"`
	Host                  string `json:"host"`
	Port                  int    `json:"port"`
	SelectedVersion       string `json:"selectedVersion"`
	CompatibilityVerified bool   `json:"compatibilityVerified"`
	CommandTimeoutMS      int64  `json:"commandTimeoutMs"`
	ActivationTimeoutMS   int64  `json:"activationTimeoutMs"`
	RenewalIntervalMS     int64  `json:"renewalIntervalMs"`
	MaxActiveJobs         int    `json:"maxActiveJobs"`
	HistoryRetentionDays  int    `json:"historyRetentionDays"`
	CreatedAt             string `json:"createdAt"`
	UpdatedAt             string `json:"updatedAt"`
}

func (p Profile) WithoutOperateCredentials() Profile {
	p.OperateAuthMode = "none"
	p.OperateUsername, p.OperatePassword, p.OperateToken = "", "", ""
	return p
}

func (p Profile) Address() string { return p.Host + ":" + itoa(p.Port) }

type JobMode string

const (
	ModeManual JobMode = "manual"
	ModeAuto   JobMode = "auto"
)

type JobTypeConfig struct {
	ID               string  `json:"id"`
	ProfileID        string  `json:"profileId"`
	JobType          string  `json:"jobType"`
	Description      string  `json:"description"`
	Mode             JobMode `json:"mode"`
	ActiveScenarioID *string `json:"activeScenarioId"`
	MaxActiveJobs    int     `json:"maxActiveJobs"`
	CreatedAt        string  `json:"createdAt"`
	UpdatedAt        string  `json:"updatedAt"`
}

type Outcome string

const (
	OutcomeSuccess       Outcome = "success"
	OutcomeBusinessError Outcome = "business_error"
	OutcomeTechnicalFail Outcome = "technical_failure"
)

type Scenario struct {
	ID               string  `json:"id"`
	JobTypeConfigID  string  `json:"jobTypeConfigId"`
	Name             string  `json:"name"`
	Description      string  `json:"description"`
	Outcome          Outcome `json:"outcome"`
	VariablesJSON    string  `json:"variablesJson"`
	DelayMS          int64   `json:"delayMs"`
	ErrorCode        string  `json:"errorCode"`
	ErrorMessage     string  `json:"errorMessage"`
	RemainingRetries int32   `json:"remainingRetries"`
	RetryBackoffMS   int64   `json:"retryBackoffMs"`
	CreatedAt        string  `json:"createdAt"`
	UpdatedAt        string  `json:"updatedAt"`
}

type ConnectionStatus struct {
	State           string `json:"state"`
	Message         string `json:"message"`
	SelectedVersion string `json:"selectedVersion"`
	DetectedVersion string `json:"detectedVersion"`
	CheckedAt       string `json:"checkedAt"`
}

type TypeRuntimeState struct {
	ConfigID   string `json:"configId"`
	State      string `json:"state"`
	ActiveJobs int    `json:"activeJobs"`
	LastError  string `json:"lastError"`
}

type ActivationState string

const (
	ActivationActive      ActivationState = "active"
	ActivationFinished    ActivationState = "finished"
	ActivationExpired     ActivationState = "expired_or_unconfirmed"
	ActivationInterrupted ActivationState = "interrupted"
)

type SendStatus string

const (
	SendNotPrepared SendStatus = "not_prepared"
	SendPrepared    SendStatus = "prepared"
	SendSending     SendStatus = "sending"
	SendConfirmed   SendStatus = "confirmed"
	SendFailed      SendStatus = "failed"
	SendUnknown     SendStatus = "unknown"
)

type ActivatedJob struct {
	Key                  string `json:"key"`
	Type                 string `json:"type"`
	ProcessInstanceKey   string `json:"processInstanceKey"`
	ProcessDefinitionKey string `json:"processDefinitionKey"`
	BPMNProcessID        string `json:"bpmnProcessId"`
	ElementID            string `json:"elementId"`
	ElementInstanceKey   string `json:"elementInstanceKey"`
	Retries              int32  `json:"retries"`
	VariablesJSON        string `json:"variablesJson"`
	CustomHeadersJSON    string `json:"customHeadersJson"`
}

type ResponseDraft struct {
	Outcome          Outcome `json:"outcome"`
	VariablesJSON    string  `json:"variablesJson"`
	ErrorCode        string  `json:"errorCode"`
	ErrorMessage     string  `json:"errorMessage"`
	RemainingRetries int32   `json:"remainingRetries"`
	RetryBackoffMS   int64   `json:"retryBackoffMs"`
}

type Activation struct {
	ID                    string          `json:"id"`
	ProfileID             string          `json:"profileId"`
	JobTypeConfigID       string          `json:"jobTypeConfigId"`
	JobKey                string          `json:"jobKey"`
	JobType               string          `json:"jobType"`
	ProcessInstanceKey    string          `json:"processInstanceKey"`
	ProcessDefinitionKey  string          `json:"processDefinitionKey"`
	BPMNProcessID         string          `json:"bpmnProcessId"`
	ElementID             string          `json:"elementId"`
	ElementInstanceKey    string          `json:"elementInstanceKey"`
	Retries               int32           `json:"retries"`
	CustomHeadersJSON     string          `json:"customHeadersJson"`
	InputJSON             string          `json:"inputJson"`
	Mode                  JobMode         `json:"mode"`
	ScenarioSnapshotJSON  string          `json:"scenarioSnapshotJson"`
	DraftJSON             string          `json:"draftJson"`
	ActivationState       ActivationState `json:"activationState"`
	ActivationStateReason string          `json:"activationStateReason"`
	SendStatus            SendStatus      `json:"sendStatus"`
	ReceivedAt            string          `json:"receivedAt"`
	PreparedAt            string          `json:"preparedAt"`
	ConfirmedDeadlineAt   string          `json:"confirmedDeadlineAt"`
	FinishedAt            string          `json:"finishedAt"`
	ProfileSnapshotJSON   string          `json:"profileSnapshotJson"`
}

type ResponseAttempt struct {
	ID             string     `json:"id"`
	ActivationID   string     `json:"activationId"`
	Sequence       int        `json:"sequence"`
	Command        string     `json:"command"`
	PayloadJSON    string     `json:"payloadJson"`
	Status         SendStatus `json:"status"`
	StartedAt      string     `json:"startedAt"`
	FinishedAt     string     `json:"finishedAt"`
	DurationMS     int64      `json:"durationMs"`
	DiagnosticCode string     `json:"diagnosticCode"`
	DiagnosticText string     `json:"diagnosticText"`
}

type ActivationEvent struct {
	ID           string `json:"id"`
	ActivationID string `json:"activationId"`
	Kind         string `json:"kind"`
	Success      bool   `json:"success"`
	Details      string `json:"details"`
	CreatedAt    string `json:"createdAt"`
}

type HistoryQuery struct {
	ProfileID  string `json:"profileId"`
	JobType    string `json:"jobType"`
	Outcome    string `json:"outcome"`
	SendStatus string `json:"sendStatus"`
	Search     string `json:"search"`
	FromUTC    string `json:"fromUtc"`
	ToUTC      string `json:"toUtc"`
	Page       int    `json:"page"`
}

type HistoryPage struct {
	Items    []Activation `json:"items"`
	Page     int          `json:"page"`
	PageSize int          `json:"pageSize"`
	Total    int          `json:"total"`
}

type Bootstrap struct {
	AppVersion        string             `json:"appVersion"`
	Profiles          []Profile          `json:"profiles"`
	SelectedProfileID string             `json:"selectedProfileId"`
	JobTypes          []JobTypeConfig    `json:"jobTypes"`
	Scenarios         []Scenario         `json:"scenarios"`
	Runtime           []TypeRuntimeState `json:"runtime"`
	Connection        ConnectionStatus   `json:"connection"`
	Pending           []Activation       `json:"pending"`
	DataPath          string             `json:"dataPath"`
}

func UTCNow() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	b := make([]byte, 0, 10)
	for v > 0 {
		b = append(b, byte('0'+v%10))
		v /= 10
	}
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
	return string(b)
}
