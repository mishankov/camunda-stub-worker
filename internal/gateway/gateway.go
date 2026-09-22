package gateway

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/camunda/zeebe/clients/go/v8/pkg/zbc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/mishankov/camunda-stub-worker/internal/domain"
)

type Topology struct {
	Version    string `json:"version"`
	Brokers    int    `json:"brokers"`
	Partitions int    `json:"partitions"`
}

type CamundaGateway interface {
	StartProcess(context.Context, domain.StartProcessRequest) (domain.ProcessInstance, error)
	Topology(context.Context) (Topology, error)
	Activate(context.Context, string, int32, time.Duration) ([]domain.ActivatedJob, error)
	Complete(context.Context, string, string) error
	ThrowError(context.Context, string, string, string, string) error
	Fail(context.Context, string, string, int32, time.Duration, string) error
	Extend(context.Context, string, time.Duration) error
	Close() error
}

type Factory interface {
	Connect(domain.Profile) (CamundaGateway, error)
}
type ZeebeFactory struct{}

func (ZeebeFactory) Connect(p domain.Profile) (CamundaGateway, error) {
	client, err := zbc.NewClient(&zbc.ClientConfig{GatewayAddress: p.Address(), UsePlaintextConnection: true, UserAgent: "camunda-stub-worker/1.0"})
	if err != nil {
		return nil, err
	}
	return &ZeebeGateway{client: client}, nil
}

type ZeebeGateway struct{ client zbc.Client }

func (g *ZeebeGateway) StartProcess(ctx context.Context, request domain.StartProcessRequest) (domain.ProcessInstance, error) {
	if err := domain.ValidateStartProcess(request); err != nil {
		return domain.ProcessInstance{}, err
	}
	version := request.Version
	if version == 0 {
		version = -1
	}
	cmd, err := g.client.NewCreateInstanceCommand().BPMNProcessId(request.BPMNProcessID).Version(version).VariablesFromString(request.VariablesJSON)
	if err != nil {
		return domain.ProcessInstance{}, err
	}
	r, err := cmd.Send(ctx)
	if err != nil {
		return domain.ProcessInstance{}, err
	}
	return domain.ProcessInstance{
		BPMNProcessID: r.BpmnProcessId, Version: r.Version,
		ProcessDefinitionKey: strconv.FormatInt(r.ProcessDefinitionKey, 10),
		ProcessInstanceKey:   strconv.FormatInt(r.ProcessInstanceKey, 10),
	}, nil
}

func (g *ZeebeGateway) Close() error { return g.client.Close() }
func (g *ZeebeGateway) Topology(ctx context.Context) (Topology, error) {
	r, err := g.client.NewTopologyCommand().Send(ctx)
	if err != nil {
		return Topology{}, err
	}
	t := Topology{Brokers: len(r.Brokers), Partitions: int(r.ClusterSize)}
	if len(r.Brokers) > 0 {
		t.Version = r.Brokers[0].Version
	}
	return t, nil
}
func (g *ZeebeGateway) Activate(ctx context.Context, jobType string, max int32, timeout time.Duration) ([]domain.ActivatedJob, error) {
	jobs, err := g.client.NewActivateJobsCommand().JobType(jobType).MaxJobsToActivate(max).Timeout(timeout).WorkerName("camunda-stub-worker").Send(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.ActivatedJob, 0, len(jobs))
	for _, j := range jobs {
		v := j.ActivatedJob
		out = append(out, domain.ActivatedJob{Key: strconv.FormatInt(v.Key, 10), Type: v.Type, ProcessInstanceKey: strconv.FormatInt(v.ProcessInstanceKey, 10), ProcessDefinitionKey: strconv.FormatInt(v.ProcessDefinitionKey, 10), BPMNProcessID: v.BpmnProcessId, ElementID: v.ElementId, ElementInstanceKey: strconv.FormatInt(v.ElementInstanceKey, 10), Retries: v.Retries, VariablesJSON: v.Variables, CustomHeadersJSON: v.CustomHeaders})
	}
	return out, nil
}
func parseKey(v string) (int64, error) {
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid Zeebe key %q", v)
	}
	return n, nil
}
func (g *ZeebeGateway) Complete(ctx context.Context, key, variables string) error {
	k, e := parseKey(key)
	if e != nil {
		return e
	}
	cmd, e := g.client.NewCompleteJobCommand().JobKey(k).VariablesFromString(variables)
	if e != nil {
		return e
	}
	_, e = cmd.Send(ctx)
	return e
}
func (g *ZeebeGateway) ThrowError(ctx context.Context, key, code, message, variables string) error {
	k, e := parseKey(key)
	if e != nil {
		return e
	}
	cmd, e := g.client.NewThrowErrorCommand().JobKey(k).ErrorCode(code).ErrorMessage(message).VariablesFromString(variables)
	if e != nil {
		return e
	}
	_, e = cmd.Send(ctx)
	return e
}
func (g *ZeebeGateway) Fail(ctx context.Context, key, message string, retries int32, backoff time.Duration, variables string) error {
	k, e := parseKey(key)
	if e != nil {
		return e
	}
	cmd, e := g.client.NewFailJobCommand().JobKey(k).Retries(retries).ErrorMessage(message).RetryBackoff(backoff).VariablesFromString(variables)
	if e != nil {
		return e
	}
	_, e = cmd.Send(ctx)
	return e
}
func (g *ZeebeGateway) Extend(ctx context.Context, key string, timeout time.Duration) error {
	k, e := parseKey(key)
	if e != nil {
		return e
	}
	_, e = g.client.NewUpdateJobTimeoutCommand().JobKey(k).Timeout(timeout.Milliseconds()).Send(ctx)
	return e
}

type ErrorDisposition string

const (
	ErrorDefinite       ErrorDisposition = "definite"
	ErrorUnknown        ErrorDisposition = "unknown"
	ErrorActivationLost ErrorDisposition = "activation_lost"
	ErrorUnsupported    ErrorDisposition = "unsupported"
)

func ClassifyCommandError(err error) ErrorDisposition {
	if err == nil {
		return ""
	}
	c := status.Code(err)
	switch c {
	case codes.DeadlineExceeded, codes.Unavailable, codes.Canceled, codes.Unknown, codes.Internal:
		return ErrorUnknown
	case codes.NotFound, codes.FailedPrecondition:
		return ErrorActivationLost
	case codes.Unimplemented:
		return ErrorUnsupported
	default:
		return ErrorDefinite
	}
}
func Diagnostic(err error) (string, string) {
	if err == nil {
		return "", ""
	}
	if s, ok := status.FromError(err); ok {
		return s.Code().String(), s.Message()
	}
	return "LOCAL", err.Error()
}
func IsContextCancellation(err error) bool { return errors.Is(err, context.Canceled) }
