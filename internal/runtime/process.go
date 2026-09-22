package runtime

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/mishankov/camunda-stub-worker/internal/domain"
	"github.com/mishankov/camunda-stub-worker/internal/gateway"
)

func (m *Manager) StartProcess(ctx context.Context, request domain.StartProcessRequest) (domain.ProcessInstance, error) {
	if err := domain.ValidateStartProcess(request); err != nil {
		return domain.ProcessInstance{}, err
	}
	m.mu.Lock()
	if m.closed || m.startingProcess || request.ProfileID != m.profile.ID {
		m.mu.Unlock()
		return domain.ProcessInstance{}, errors.New("cannot start process: check the selected profile and wait for any pending start to finish")
	}
	p := m.profile
	m.startingProcess = true
	m.mu.Unlock()
	defer func() { m.mu.Lock(); m.startingProcess = false; m.mu.Unlock() }()

	gw, err := m.factory.Connect(p)
	if err != nil {
		return domain.ProcessInstance{}, err
	}
	defer gw.Close()
	commandCtx, cancel := context.WithTimeout(ctx, time.Duration(p.CommandTimeoutMS)*time.Millisecond)
	defer cancel()
	result, err := gw.StartProcess(commandCtx, request)
	if err != nil && (gateway.ClassifyCommandError(err) == gateway.ErrorUnknown || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)) {
		return domain.ProcessInstance{}, fmt.Errorf("process start outcome unknown; an instance may have been created. Check Camunda before trying again to avoid duplicates: %w", err)
	}
	return result, err
}
