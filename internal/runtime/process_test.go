package runtime

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mishankov/camunda-stub-worker/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestStartProcessValidationAndUncertainResult(t *testing.T) {
	calls := 0
	fake := &fakeGateway{startProcess: func(ctx context.Context, r domain.StartProcessRequest) (domain.ProcessInstance, error) {
		calls++
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > time.Second {
			t.Fatal("missing command timeout")
		}
		return domain.ProcessInstance{}, status.Error(codes.DeadlineExceeded, "timeout")
	}}
	m := &Manager{factory: fakeFactory{fake}, profile: domain.Profile{ID: "profile", CommandTimeoutMS: 1000}}
	request := domain.StartProcessRequest{ProfileID: "profile", BPMNProcessID: "order", VariablesJSON: `{}`}
	invalid := []domain.StartProcessRequest{
		{ProfileID: "other", BPMNProcessID: "order", VariablesJSON: `{}`},
		{ProfileID: "profile", BPMNProcessID: " ", VariablesJSON: `{}`},
		{ProfileID: "profile", BPMNProcessID: "order", Version: -1, VariablesJSON: `{}`},
		{ProfileID: "profile", BPMNProcessID: "order", VariablesJSON: `[]`},
		{ProfileID: "profile", BPMNProcessID: "order", VariablesJSON: `{} trailing`},
	}
	for _, r := range invalid {
		if _, err := m.StartProcess(context.Background(), r); err == nil {
			t.Fatalf("accepted %+v", r)
		}
	}
	if calls != 0 {
		t.Fatal("invalid input sent to gateway")
	}
	_, err := m.StartProcess(context.Background(), request)
	if err == nil || !strings.Contains(err.Error(), "outcome unknown") || calls != 1 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
	if m.HasWork() {
		t.Fatal("start not released")
	}
}

func TestPendingStartBlocksProfileSwitchAndDuplicate(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	fake := &fakeGateway{startProcess: func(context.Context, domain.StartProcessRequest) (domain.ProcessInstance, error) {
		close(started)
		<-release
		return domain.ProcessInstance{ProcessInstanceKey: "9007199254740993"}, nil
	}}
	p := domain.Profile{ID: "profile", CommandTimeoutMS: 1000}
	m := &Manager{factory: fakeFactory{fake}, profile: p}
	request := domain.StartProcessRequest{ProfileID: p.ID, BPMNProcessID: "order", VariablesJSON: `{}`}
	done := make(chan error, 1)
	go func() { _, err := m.StartProcess(context.Background(), request); done <- err }()
	<-started
	if !m.HasWork() {
		t.Error("pending start not counted")
	}
	if err := m.SetProfile(context.Background(), p); err == nil {
		t.Error("profile switch allowed")
	}
	if _, err := m.StartProcess(context.Background(), request); err == nil {
		t.Error("duplicate allowed")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := m.SetProfile(context.Background(), p); err != nil {
		t.Fatal(err)
	}
}
