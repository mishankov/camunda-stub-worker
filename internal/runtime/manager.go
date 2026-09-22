package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/mishankov/camunda-stub-worker/internal/domain"
	"github.com/mishankov/camunda-stub-worker/internal/gateway"
	"github.com/mishankov/camunda-stub-worker/internal/store"
)

type NotifyFunc func(string, any)

type Manager struct {
	store           *store.Store
	factory         gateway.Factory
	notify          NotifyFunc
	mu              sync.Mutex
	profile         domain.Profile
	gw              gateway.CamundaGateway
	connection      domain.ConnectionStatus
	types           map[string]*typeWorker
	active          map[string]*liveActivation
	reserved        int
	closed          bool
	startingProcess bool
}

type typeWorker struct {
	state      string
	desired    bool
	polling    bool
	cancelPoll context.CancelFunc
	active     int
	lastError  string
	wake       chan struct{}
}
type liveActivation struct {
	mu       sync.Mutex
	record   domain.Activation
	profile  domain.Profile
	deadline time.Time
	done     chan struct{}
	doneOnce sync.Once
}

func NewManager(s *store.Store, f gateway.Factory, notify NotifyFunc) (*Manager, error) {
	id, err := s.SelectedProfileID(context.Background())
	if err != nil {
		return nil, err
	}
	p, err := s.Profile(context.Background(), id)
	if err != nil {
		return nil, err
	}
	return &Manager{store: s, factory: f, notify: notify, profile: p, types: map[string]*typeWorker{}, active: map[string]*liveActivation{}, connection: domain.ConnectionStatus{State: "offline", Message: "Connection not checked", SelectedVersion: p.SelectedVersion}}, nil
}
func (m *Manager) emit(name string, data any) {
	if m.notify != nil {
		m.notify(name, data)
	}
}
func (m *Manager) SetNotify(fn NotifyFunc) { m.mu.Lock(); m.notify = fn; m.mu.Unlock() }

func (m *Manager) ConnectCheck(ctx context.Context) (domain.ConnectionStatus, error) {
	m.mu.Lock()
	p := m.profile
	m.mu.Unlock()
	gw, err := m.factory.Connect(p)
	if err != nil {
		return m.setConnectionFailure(p, err), err
	}
	defer gw.Close()
	checkCtx, cancel := context.WithTimeout(ctx, time.Duration(p.CommandTimeoutMS)*time.Millisecond)
	defer cancel()
	top, err := gw.Topology(checkCtx)
	if err != nil {
		return m.setConnectionFailure(p, err), err
	}
	st := domain.ConnectionStatus{State: "connected", Message: fmt.Sprintf("Available brokers: %d", top.Brokers), SelectedVersion: p.SelectedVersion, DetectedVersion: top.Version, CheckedAt: domain.UTCNow()}
	if top.Version != "" && top.Version != p.SelectedVersion && !sameMinor(top.Version, p.SelectedVersion) {
		st.State = "version_mismatch"
		st.Message = "Selected and detected versions do not match"
	}
	m.mu.Lock()
	m.connection = st
	m.mu.Unlock()
	m.emit("connection:changed", st)
	return st, nil
}
func (m *Manager) setConnectionFailure(p domain.Profile, err error) domain.ConnectionStatus {
	st := domain.ConnectionStatus{State: "unavailable", Message: err.Error(), SelectedVersion: p.SelectedVersion, CheckedAt: domain.UTCNow()}
	m.mu.Lock()
	m.connection = st
	m.mu.Unlock()
	m.emit("connection:changed", st)
	return st
}
func sameMinor(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	return len(pa) >= 2 && len(pb) >= 2 && pa[0] == pb[0] && pa[1] == pb[1]
}

func (m *Manager) SetProfile(ctx context.Context, p domain.Profile) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.hasWorkLocked() {
		return errors.New("the profile and network settings cannot be changed while workers are running or jobs are active")
	}
	if m.gw != nil {
		_ = m.gw.Close()
		m.gw = nil
	}
	m.profile = p
	m.connection = domain.ConnectionStatus{State: "offline", Message: "Connection not checked", SelectedVersion: p.SelectedVersion}
	return nil
}
func (m *Manager) HasWork() bool { m.mu.Lock(); defer m.mu.Unlock(); return m.hasWorkLocked() }
func (m *Manager) hasWorkLocked() bool {
	if m.startingProcess {
		return true
	}
	if len(m.active) > 0 || m.reserved > 0 {
		return true
	}
	for _, w := range m.types {
		if w.desired || w.polling {
			return true
		}
	}
	return false
}
func (m *Manager) ActiveCount() int { m.mu.Lock(); defer m.mu.Unlock(); return len(m.active) }
func (m *Manager) Connection() domain.ConnectionStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.connection
}
func (m *Manager) States() []domain.TypeRuntimeState {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]domain.TypeRuntimeState, 0, len(m.types))
	for id, w := range m.types {
		state := w.state
		if state == "" {
			state = "stopped"
		}
		out = append(out, domain.TypeRuntimeState{ConfigID: id, State: state, ActiveJobs: w.active, LastError: w.lastError})
	}
	return out
}

func (m *Manager) ensureGatewayLocked() error {
	if m.gw != nil {
		return nil
	}
	gw, err := m.factory.Connect(m.profile)
	if err != nil {
		return err
	}
	m.gw = gw
	return nil
}

func (m *Manager) StartType(ctx context.Context, id string) error {
	cfg, err := m.store.JobType(ctx, id)
	if err != nil {
		return err
	}
	m.mu.Lock()
	if cfg.ProfileID != m.profile.ID {
		m.mu.Unlock()
		return errors.New("the job type does not belong to the selected profile")
	}
	if cfg.Mode == domain.ModeAuto && cfg.ActiveScenarioID == nil {
		m.mu.Unlock()
		return errors.New("select an active scenario for automatic mode")
	}
	if err = m.ensureGatewayLocked(); err != nil {
		m.mu.Unlock()
		return err
	}
	w := m.types[id]
	if w == nil {
		w = &typeWorker{wake: make(chan struct{}, 1)}
		m.types[id] = w
	}
	if w.desired {
		m.mu.Unlock()
		return nil
	}
	w.desired = true
	w.state = "running"
	w.lastError = ""
	m.mu.Unlock()
	m.emitStates()
	go m.pollLoop(id, w)
	return nil
}
func (m *Manager) StartAll(ctx context.Context) error {
	types, err := m.store.JobTypes(ctx, m.profile.ID)
	if err != nil {
		return err
	}
	var errs []error
	started := 0
	for _, c := range types {
		if e := m.StartType(ctx, c.ID); e != nil {
			errs = append(errs, fmt.Errorf("%s: %w", c.JobType, e))
		} else {
			started++
		}
	}
	if started == 0 && len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}
func (m *Manager) StopType(id string) {
	m.mu.Lock()
	if w := m.types[id]; w != nil {
		w.desired = false
		if w.cancelPoll != nil {
			w.cancelPoll()
		}
		if w.polling || w.active > 0 {
			w.state = "stopping"
		} else {
			w.state = "stopped"
		}
		select {
		case w.wake <- struct{}{}:
		default:
		}
	}
	m.mu.Unlock()
	m.emitStates()
}
func (m *Manager) StopAll() {
	m.mu.Lock()
	for _, w := range m.types {
		w.desired = false
		if w.cancelPoll != nil {
			w.cancelPoll()
		}
		if w.polling || w.active > 0 {
			w.state = "stopping"
		} else {
			w.state = "stopped"
		}
		select {
		case w.wake <- struct{}{}:
		default:
		}
	}
	m.mu.Unlock()
	m.emitStates()
}

func (m *Manager) pollLoop(id string, w *typeWorker) {
	backoff := time.Second
	for {
		m.mu.Lock()
		if !w.desired || m.closed {
			w.polling = false
			if w.active == 0 {
				w.state = "stopped"
			}
			m.mu.Unlock()
			m.emitStates()
			return
		}
		m.mu.Unlock()
		cfg, err := m.store.JobType(context.Background(), id)
		if err != nil {
			m.workerError(id, err)
			return
		}
		slots := m.reserve(cfg)
		if slots == 0 {
			select {
			case <-w.wake:
				break
			case <-time.After(200 * time.Millisecond):
				break
			}
			continue
		}
		m.mu.Lock()
		gw := m.gw
		p := m.profile
		pollCtx, cancel := context.WithTimeout(context.Background(), time.Duration(p.CommandTimeoutMS)*time.Millisecond)
		w.polling = true
		w.cancelPoll = cancel
		m.mu.Unlock()
		jobs, activateErr := gw.Activate(pollCtx, cfg.JobType, int32(slots), time.Duration(p.ActivationTimeoutMS)*time.Millisecond)
		cancel()
		m.mu.Lock()
		w.polling = false
		w.cancelPoll = nil
		m.reserved -= slots - len(jobs)
		m.mu.Unlock()
		if len(jobs) > 0 {
			// Mode and active-scenario changes apply at receipt time, not when the
			// long-poll request was opened.
			latest, loadErr := m.store.JobType(context.Background(), id)
			if loadErr != nil {
				m.releaseReserved(len(jobs))
				m.pauseForStorageError(loadErr)
				return
			}
			cfg = latest
		}
		// A streaming ActivateJobs call may return already received jobs with a terminal error.
		// Persist those before handling the connection failure.
		for i, j := range jobs {
			if e := m.accept(cfg, p, j); e != nil {
				m.releaseReserved(len(jobs) - i)
				m.pauseForStorageError(e)
				return
			}
		}
		if activateErr != nil {
			m.mu.Lock()
			desired := w.desired
			m.mu.Unlock()
			if !desired {
				continue
			}
			m.workerTransientError(id, activateErr)
			delay := jitter(backoff)
			if backoff < 30*time.Second {
				backoff *= 2
				if backoff > 30*time.Second {
					backoff = 30 * time.Second
				}
			}
			select {
			case <-w.wake:
				break
			case <-time.After(delay):
				break
			}
			continue
		}
		backoff = time.Second
		m.connectionHealthy()
	}
}

func (m *Manager) reserve(cfg domain.JobTypeConfig) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	w := m.types[cfg.ID]
	availableGlobal := m.profile.MaxActiveJobs - len(m.active) - m.reserved
	availableType := cfg.MaxActiveJobs - w.active
	if availableGlobal <= 0 || availableType <= 0 {
		return 0
	}
	n := availableGlobal
	if availableType < n {
		n = availableType
	}
	m.reserved += n
	return n
}
func (m *Manager) releaseReserved(n int) {
	m.mu.Lock()
	m.reserved -= n
	if m.reserved < 0 {
		m.reserved = 0
	}
	m.mu.Unlock()
}
func (m *Manager) accept(cfg domain.JobTypeConfig, p domain.Profile, j domain.ActivatedJob) error {
	received := domain.UTCNow()
	a := domain.Activation{ID: uuid.NewString(), ProfileID: p.ID, JobTypeConfigID: cfg.ID, JobKey: j.Key, JobType: j.Type, ProcessInstanceKey: j.ProcessInstanceKey, ProcessDefinitionKey: j.ProcessDefinitionKey, BPMNProcessID: j.BPMNProcessID, ElementID: j.ElementID, ElementInstanceKey: j.ElementInstanceKey, Retries: j.Retries, CustomHeadersJSON: normalizeObject(j.CustomHeadersJSON), InputJSON: normalizeObject(j.VariablesJSON), Mode: cfg.Mode, ActivationState: domain.ActivationActive, SendStatus: domain.SendNotPrepared, ReceivedAt: received, ConfirmedDeadlineAt: time.Now().UTC().Add(time.Duration(p.ActivationTimeoutMS) * time.Millisecond).Format(time.RFC3339Nano)}
	profileRaw, _ := json.Marshal(p.WithoutOperateCredentials())
	a.ProfileSnapshotJSON = string(profileRaw)
	var draft domain.ResponseDraft
	if cfg.ActiveScenarioID != nil {
		s, err := m.store.Scenario(context.Background(), *cfg.ActiveScenarioID)
		if err != nil {
			return fmt.Errorf("active scenario: %w", err)
		}
		raw, _ := json.Marshal(s)
		a.ScenarioSnapshotJSON = string(raw)
		draft = draftFromScenario(s)
		draftRaw, _ := json.Marshal(draft)
		a.DraftJSON = string(draftRaw)
		a.SendStatus = domain.SendPrepared
		a.PreparedAt = received
	} else if cfg.Mode == domain.ModeAuto {
		return errors.New("the automatic job type has no active scenario")
	}
	if err := m.store.InsertActivation(context.Background(), a); err != nil {
		return err
	}
	live := &liveActivation{record: a, profile: p, deadline: time.Now().Add(time.Duration(p.ActivationTimeoutMS) * time.Millisecond), done: make(chan struct{})}
	m.mu.Lock()
	m.reserved--
	if m.reserved < 0 {
		m.reserved = 0
	}
	m.active[a.ID] = live
	if w := m.types[cfg.ID]; w != nil {
		w.active++
	}
	m.mu.Unlock()
	m.emit("activation:created", a)
	m.emitStates()
	go m.renewLoop(live)
	if cfg.Mode == domain.ModeAuto {
		var s domain.Scenario
		_ = json.Unmarshal([]byte(a.ScenarioSnapshotJSON), &s)
		go m.autoRespond(live, draft, time.Duration(s.DelayMS)*time.Millisecond)
	}
	return nil
}
func normalizeObject(v string) string {
	if v == "" {
		return "{}"
	}
	if domain.ValidateJSONObject(v) != nil {
		return "{}"
	}
	return v
}
func draftFromScenario(s domain.Scenario) domain.ResponseDraft {
	return domain.ResponseDraft{Outcome: s.Outcome, VariablesJSON: s.VariablesJSON, ErrorCode: s.ErrorCode, ErrorMessage: s.ErrorMessage, RemainingRetries: s.RemainingRetries, RetryBackoffMS: s.RetryBackoffMS}
}

func (m *Manager) ApplyScenario(ctx context.Context, id, scenarioID string) (domain.ResponseDraft, error) {
	s, err := m.store.Scenario(ctx, scenarioID)
	if err != nil {
		return domain.ResponseDraft{}, err
	}
	if err = domain.ValidateScenario(s); err != nil {
		return domain.ResponseDraft{}, err
	}
	m.mu.Lock()
	l := m.active[id]
	m.mu.Unlock()
	if l == nil {
		return domain.ResponseDraft{}, errors.New("the activation was not found or is no longer valid")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if s.JobTypeConfigID != l.record.JobTypeConfigID {
		return domain.ResponseDraft{}, errors.New("the scenario belongs to another job type")
	}
	if time.Now().After(l.deadline) {
		return domain.ResponseDraft{}, errors.New("the confirmed activation deadline has expired")
	}
	d := draftFromScenario(s)
	draftRaw, _ := json.Marshal(d)
	snapshotRaw, _ := json.Marshal(s)
	if err = m.store.UpdateDraftSnapshot(ctx, id, string(draftRaw), string(snapshotRaw)); err != nil {
		return domain.ResponseDraft{}, err
	}
	l.record.DraftJSON = string(draftRaw)
	l.record.ScenarioSnapshotJSON = string(snapshotRaw)
	l.record.SendStatus = domain.SendPrepared
	m.emit("activation:changed", l.record)
	return d, nil
}

func (m *Manager) renewLoop(l *liveActivation) {
	interval := time.Duration(l.profile.RenewalIntervalMS) * time.Millisecond
	timer := time.NewTimer(interval)
	defer timer.Stop()
	failures := 0
	for {
		select {
		case <-l.done:
			return
		case <-timer.C:
			l.mu.Lock()
			if time.Now().After(l.deadline) {
				l.mu.Unlock()
				m.expire(l, "The activation deadline was not confirmed locally")
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(l.profile.CommandTimeoutMS)*time.Millisecond)
			m.mu.Lock()
			gw := m.gw
			m.mu.Unlock()
			err := gw.Extend(ctx, l.record.JobKey, time.Duration(l.profile.ActivationTimeoutMS)*time.Millisecond)
			cancel()
			if err == nil {
				l.deadline = time.Now().Add(time.Duration(l.profile.ActivationTimeoutMS) * time.Millisecond)
				l.record.ConfirmedDeadlineAt = l.deadline.UTC().Format(time.RFC3339Nano)
				dbErr := m.store.UpdateDeadline(context.Background(), l.record.ID, l.record.ConfirmedDeadlineAt)
				l.mu.Unlock()
				if dbErr != nil {
					m.pauseForStorageError(dbErr)
					m.expire(l, "Failed to persist the activation extension")
					return
				}
				_ = m.store.AddEvent(context.Background(), domain.ActivationEvent{ActivationID: l.record.ID, Kind: "lease_extended", Success: true, Details: "timeout updated"})
				failures = 0
				timer.Reset(interval)
				continue
			}
			disp := gateway.ClassifyCommandError(err)
			code, text := gateway.Diagnostic(err)
			_ = m.store.AddEvent(context.Background(), domain.ActivationEvent{ActivationID: l.record.ID, Kind: "lease_extension_failed", Success: false, Details: code + ": " + text})
			l.mu.Unlock()
			if disp == gateway.ErrorActivationLost {
				m.expire(l, "Camunda reported that the job is no longer active")
				return
			}
			if disp == gateway.ErrorUnsupported {
				m.StopAll()
				m.expire(l, "The server does not support UpdateJobTimeout")
				m.workerError(l.record.JobTypeConfigID, errors.New("the server does not support the required UpdateJobTimeout operation"))
				return
			}
			failures++
			retry := time.Duration(1<<min(failures-1, 4)) * time.Second
			if retry > 30*time.Second {
				retry = 30 * time.Second
			}
			l.mu.Lock()
			remaining := time.Until(l.deadline)
			l.mu.Unlock()
			if remaining <= 0 {
				m.expire(l, "The activation deadline was not confirmed after an extension error")
				return
			}
			if retry >= remaining {
				retry = remaining
			}
			timer.Reset(retry)
		}
	}
}

func (m *Manager) autoRespond(l *liveActivation, d domain.ResponseDraft, delay time.Duration) {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-l.done:
		return
	case <-timer.C:
		_, _ = m.submit(context.Background(), l.record.ID, d)
	}
}
func (m *Manager) SaveDraft(ctx context.Context, id string, d domain.ResponseDraft) error {
	if err := domain.ValidateDraft(d); err != nil {
		return err
	}
	m.mu.Lock()
	l := m.active[id]
	m.mu.Unlock()
	if l == nil {
		return errors.New("the activation was not found or is no longer valid")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if time.Now().After(l.deadline) {
		return errors.New("the confirmed activation deadline has expired")
	}
	raw, _ := json.Marshal(d)
	if err := m.store.UpdateDraft(ctx, id, string(raw), domain.SendPrepared); err != nil {
		return err
	}
	l.record.DraftJSON = string(raw)
	l.record.SendStatus = domain.SendPrepared
	m.emit("activation:changed", l.record)
	return nil
}
func (m *Manager) Submit(ctx context.Context, id string, d domain.ResponseDraft) (domain.ResponseAttempt, error) {
	if err := domain.ValidateDraft(d); err != nil {
		return domain.ResponseAttempt{}, err
	}
	return m.submit(ctx, id, d)
}
func (m *Manager) submit(ctx context.Context, id string, d domain.ResponseDraft) (domain.ResponseAttempt, error) {
	m.mu.Lock()
	l := m.active[id]
	gw := m.gw
	m.mu.Unlock()
	if l == nil {
		return domain.ResponseAttempt{}, errors.New("the activation was not found or retry is not allowed")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if time.Now().After(l.deadline) {
		go m.expire(l, "The activation deadline was not confirmed before sending")
		return domain.ResponseAttempt{}, errors.New("the confirmed activation deadline has expired; sending is blocked")
	}
	payload, _ := json.Marshal(d)
	command := string(d.Outcome)
	attempt, err := m.store.BeginAttempt(context.Background(), id, command, string(payload))
	if err != nil {
		return attempt, err
	}
	l.record.SendStatus = domain.SendSending
	m.emit("activation:changed", l.record)
	started := time.Now()
	cmdCtx, cancel := context.WithTimeout(ctx, time.Duration(l.profile.CommandTimeoutMS)*time.Millisecond)
	switch d.Outcome {
	case domain.OutcomeSuccess:
		err = gw.Complete(cmdCtx, l.record.JobKey, d.VariablesJSON)
	case domain.OutcomeBusinessError:
		err = gw.ThrowError(cmdCtx, l.record.JobKey, d.ErrorCode, d.ErrorMessage, d.VariablesJSON)
	case domain.OutcomeTechnicalFail:
		err = gw.Fail(cmdCtx, l.record.JobKey, d.ErrorMessage, d.RemainingRetries, time.Duration(d.RetryBackoffMS)*time.Millisecond, d.VariablesJSON)
	default:
		err = errors.New("unknown outcome")
	}
	cancel()
	duration := time.Since(started).Milliseconds()
	if err == nil {
		attempt.Status = domain.SendConfirmed
		attempt.DurationMS = duration
		if dbErr := m.store.FinishAttempt(context.Background(), attempt.ID, id, domain.SendConfirmed, "", "", duration); dbErr != nil {
			m.pauseForStorageError(dbErr)
			return attempt, dbErr
		}
		l.record.SendStatus = domain.SendConfirmed
		l.record.ActivationState = domain.ActivationFinished
		l.record.FinishedAt = domain.UTCNow()
		l.doneOnce.Do(func() { close(l.done) })
		go m.release(l)
		m.emit("activation:changed", l.record)
		return attempt, nil
	}
	disp := gateway.ClassifyCommandError(err)
	code, text := gateway.Diagnostic(err)
	attempt.DiagnosticCode = code
	attempt.DiagnosticText = text
	attempt.DurationMS = duration
	if disp == gateway.ErrorUnknown {
		attempt.Status = domain.SendUnknown
		_ = m.store.FinishAttempt(context.Background(), attempt.ID, id, domain.SendUnknown, code, text, duration)
		l.record.SendStatus = domain.SendUnknown
		l.record.ActivationState = domain.ActivationExpired
		l.record.ActivationStateReason = "Command outcome is unknown; retry is not allowed"
		l.doneOnce.Do(func() { close(l.done) })
		remaining := time.Until(l.deadline)
		if remaining < 0 {
			remaining = 0
		}
		go func() { time.Sleep(remaining); m.release(l) }()
		m.emit("activation:changed", l.record)
		return attempt, fmt.Errorf("send outcome is unknown: %w", err)
	}
	attempt.Status = domain.SendFailed
	_ = m.store.FinishAttempt(context.Background(), attempt.ID, id, domain.SendFailed, code, text, duration)
	l.record.SendStatus = domain.SendFailed
	if disp == gateway.ErrorActivationLost {
		l.record.ActivationState = domain.ActivationExpired
		l.record.ActivationStateReason = "Camunda rejected the command: the activation is no longer valid"
		_ = m.store.ExpireActivation(context.Background(), id, l.record.ActivationStateReason)
		l.doneOnce.Do(func() { close(l.done) })
		go m.release(l)
	}
	m.emit("activation:changed", l.record)
	return attempt, err
}

func (m *Manager) expire(l *liveActivation, reason string) {
	l.doneOnce.Do(func() { close(l.done) })
	_ = m.store.ExpireActivation(context.Background(), l.record.ID, reason)
	l.mu.Lock()
	l.record.ActivationState = domain.ActivationExpired
	l.record.ActivationStateReason = reason
	l.mu.Unlock()
	m.emit("activation:changed", l.record)
	m.release(l)
}
func (m *Manager) release(l *liveActivation) {
	m.mu.Lock()
	if _, ok := m.active[l.record.ID]; !ok {
		m.mu.Unlock()
		return
	}
	delete(m.active, l.record.ID)
	if w := m.types[l.record.JobTypeConfigID]; w != nil {
		w.active--
		if w.active < 0 {
			w.active = 0
		}
		if !w.desired && !w.polling && w.active == 0 {
			w.state = "stopped"
		}
		select {
		case w.wake <- struct{}{}:
		default:
		}
	}
	for _, w := range m.types {
		select {
		case w.wake <- struct{}{}:
		default:
		}
	}
	m.mu.Unlock()
	m.emitStates()
}
func (m *Manager) workerTransientError(id string, err error) {
	m.mu.Lock()
	if w := m.types[id]; w != nil {
		w.lastError = err.Error()
	}
	p := m.profile
	m.mu.Unlock()
	m.setConnectionFailure(p, err)
	m.emitStates()
}
func (m *Manager) workerError(id string, err error) {
	m.mu.Lock()
	if w := m.types[id]; w != nil {
		w.lastError = err.Error()
		w.desired = false
		if w.active > 0 {
			w.state = "stopping"
		} else {
			w.state = "error"
		}
	}
	m.mu.Unlock()
	m.emitStates()
}
func (m *Manager) pauseForStorageError(err error) {
	m.mu.Lock()
	for _, w := range m.types {
		w.desired = false
		w.lastError = "SQLite error: " + err.Error()
		if w.active > 0 || w.polling {
			w.state = "stopping"
		} else {
			w.state = "error"
		}
	}
	m.mu.Unlock()
	m.emit("storage:error", err.Error())
	m.emitStates()
}
func (m *Manager) connectionHealthy() {
	m.mu.Lock()
	if m.connection.State == "unavailable" {
		m.connection.State = "connected"
		m.connection.Message = "Connection restored"
		m.connection.CheckedAt = domain.UTCNow()
	}
	st := m.connection
	m.mu.Unlock()
	m.emit("connection:changed", st)
}
func (m *Manager) emitStates() { m.emit("runtime:changed", m.States()) }
func jitter(d time.Duration) time.Duration {
	return time.Duration(float64(d) * (0.8 + rand.Float64()*0.4))
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	for _, w := range m.types {
		w.desired = false
		if w.cancelPoll != nil {
			w.cancelPoll()
		}
	}
	gw := m.gw
	m.gw = nil
	for _, l := range m.active {
		l.doneOnce.Do(func() { close(l.done) })
	}
	m.mu.Unlock()
	if gw != nil {
		_ = gw.Close()
	}
}
