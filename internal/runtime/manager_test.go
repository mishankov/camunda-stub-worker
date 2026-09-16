package runtime

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/mishankov/camunda-stub-worker/internal/domain"
	"github.com/mishankov/camunda-stub-worker/internal/gateway"
	"github.com/mishankov/camunda-stub-worker/internal/store"
)

type fakeFactory struct{ g *fakeGateway }

func (f fakeFactory) Connect(domain.Profile) (gateway.CamundaGateway, error) { return f.g, nil }

type fakeGateway struct {
	mu                sync.Mutex
	activateOnce      sync.Once
	activateStarted   chan struct{}
	completeCalls     int
	completeVariables string
	completeErr       error
}

func (f *fakeGateway) Topology(context.Context) (gateway.Topology, error) {
	return gateway.Topology{Version: "8.5.25", Brokers: 1}, nil
}
func (f *fakeGateway) Activate(ctx context.Context, _ string, _ int32, _ time.Duration) ([]domain.ActivatedJob, error) {
	if f.activateStarted != nil {
		f.activateOnce.Do(func() { close(f.activateStarted) })
	}
	<-ctx.Done()
	return nil, ctx.Err()
}
func (f *fakeGateway) Complete(_ context.Context, _ string, v string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.completeCalls++
	f.completeVariables = v
	return f.completeErr
}
func (f *fakeGateway) ThrowError(context.Context, string, string, string, string) error {
	return errors.New("unused")
}
func (f *fakeGateway) Fail(context.Context, string, string, int32, time.Duration, string) error {
	return errors.New("unused")
}
func (f *fakeGateway) Extend(context.Context, string, time.Duration) error { return nil }
func (f *fakeGateway) Close() error                                        { return nil }

func setupLive(t *testing.T, sendErr error) (*Manager, *store.Store, *fakeGateway, domain.Activation) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	profiles, _ := s.Profiles(ctx)
	p := profiles[0]
	p.ActivationTimeoutMS = 500
	p.RenewalIntervalMS = 100
	p.CommandTimeoutMS = 100
	if err = s.SaveProfile(ctx, p); err != nil {
		t.Fatal(err)
	}
	cfg, err := s.SaveJobType(ctx, domain.JobTypeConfig{ProfileID: p.ID, JobType: "test", Mode: domain.ModeManual, MaxActiveJobs: 1})
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeGateway{completeErr: sendErr}
	m, err := NewManager(s, fakeFactory{fake}, nil)
	if err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	m.gw = fake
	m.types[cfg.ID] = &typeWorker{state: "running", desired: true, wake: make(chan struct{}, 1)}
	m.reserved = 1
	m.mu.Unlock()
	if err = m.accept(cfg, p, domain.ActivatedJob{Key: "9007199254740993", Type: "test", VariablesJSON: `{"large":9007199254740993123456789}`, CustomHeadersJSON: `{}`}); err != nil {
		t.Fatal(err)
	}
	pending, err := s.Pending(ctx, p.ID)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending=%v err=%v", pending, err)
	}
	return m, s, fake, pending[0]
}

func TestSubmitPreservesJSONAndBlocksSecondCall(t *testing.T) {
	m, s, fake, a := setupLive(t, nil)
	defer s.Close()
	defer m.Close()
	d := domain.ResponseDraft{Outcome: domain.OutcomeSuccess, VariablesJSON: `{"large":9007199254740993123456789}`}
	if _, err := m.Submit(context.Background(), a.ID, d); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Submit(context.Background(), a.ID, d); err == nil {
		t.Fatal("second submit must be rejected")
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.completeCalls != 1 || fake.completeVariables != d.VariablesJSON {
		t.Fatalf("calls=%d variables=%s", fake.completeCalls, fake.completeVariables)
	}
}

func TestUnavailableAfterSendBecomesUnknown(t *testing.T) {
	m, s, fake, a := setupLive(t, status.Error(codes.Unavailable, "connection lost"))
	defer s.Close()
	defer m.Close()
	d := domain.ResponseDraft{Outcome: domain.OutcomeSuccess, VariablesJSON: `{}`}
	if _, err := m.Submit(context.Background(), a.ID, d); err == nil {
		t.Fatal("expected unknown-result error")
	}
	got, err := s.Activation(context.Background(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SendStatus != domain.SendUnknown || got.ActivationState != domain.ActivationExpired {
		t.Fatalf("status=%s state=%s", got.SendStatus, got.ActivationState)
	}
	if _, err = m.Submit(context.Background(), a.ID, d); err == nil {
		t.Fatal("unknown result must never be retried")
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.completeCalls != 1 {
		t.Fatalf("calls=%d", fake.completeCalls)
	}
}

func TestStopCancelsActivePoll(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	profiles, err := s.Profiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p := profiles[0]
	p.CommandTimeoutMS = 5000
	if err = s.SaveProfile(ctx, p); err != nil {
		t.Fatal(err)
	}
	cfg, err := s.SaveJobType(ctx, domain.JobTypeConfig{ProfileID: p.ID, JobType: "cancel-poll", Mode: domain.ModeManual, MaxActiveJobs: 1})
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeGateway{activateStarted: make(chan struct{})}
	m, err := NewManager(s, fakeFactory{fake}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err = m.StartType(ctx, cfg.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-fake.activateStarted:
	case <-time.After(time.Second):
		t.Fatal("worker did not begin polling")
	}

	m.StopType(cfg.ID)
	deadline := time.After(time.Second)
	for {
		states := m.States()
		if len(states) == 1 && states[0].State == "stopped" {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("worker did not stop promptly: %+v", states)
		case <-time.After(10 * time.Millisecond):
		}
	}
}
