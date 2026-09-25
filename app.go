package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gofrs/flock"
	"github.com/google/uuid"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/mishankov/camunda-stub-worker/internal/configio"
	"github.com/mishankov/camunda-stub-worker/internal/domain"
	"github.com/mishankov/camunda-stub-worker/internal/operate"
	workerruntime "github.com/mishankov/camunda-stub-worker/internal/runtime"
	"github.com/mishankov/camunda-stub-worker/internal/store"
	"github.com/mishankov/camunda-stub-worker/internal/updatechecker"
)

var version = "1.0.0"

const releasesURL = "https://github.com/mishankov/camunda-stub-worker/releases/latest"

type App struct {
	ctx           context.Context
	store         *store.Store
	manager       *workerruntime.Manager
	lock          *flock.Flock
	dataDir       string
	cleanupCancel context.CancelFunc
}

func newApp(s *store.Store, m *workerruntime.Manager, l *flock.Flock, dataDir string) *App {
	return &App{store: s, manager: m, lock: l, dataDir: dataDir}
}
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.manager.SetNotify(func(name string, data any) { wailsruntime.EventsEmit(ctx, name, data) })
	cleanupCtx, cancel := context.WithCancel(context.Background())
	a.cleanupCancel = cancel
	go func() {
		_ = a.store.CleanupExpired(cleanupCtx)
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-cleanupCtx.Done():
				return
			case <-ticker.C:
				_ = a.store.CleanupExpired(cleanupCtx)
			}
		}
	}()
}
func (a *App) shutdown(context.Context) {
	if a.cleanupCancel != nil {
		a.cleanupCancel()
	}
	a.manager.Close()
	_ = a.store.Close()
	if a.lock != nil {
		_ = a.lock.Unlock()
	}
}
func (a *App) beforeClose(ctx context.Context) bool {
	n := a.manager.ActiveCount()
	if n == 0 {
		return false
	}
	result, err := wailsruntime.MessageDialog(ctx, wailsruntime.MessageDialogOptions{Type: wailsruntime.QuestionDialog, Title: "Active jobs", Message: fmt.Sprintf("Active jobs: %d. Lease renewal will stop and the jobs will expire on the server. Close the application?", n), Buttons: []string{"Cancel", "Close application"}, DefaultButton: "Cancel", CancelButton: "Cancel"})
	return err != nil || result != "Close application"
}

func (a *App) Bootstrap() (domain.Bootstrap, error) {
	ctx := context.Background()
	profiles, err := a.store.Profiles(ctx)
	if err != nil {
		return domain.Bootstrap{}, err
	}
	id, err := a.store.SelectedProfileID(ctx)
	if err != nil {
		return domain.Bootstrap{}, err
	}
	if err := a.store.EnsureDefaultResponses(ctx, id); err != nil {
		return domain.Bootstrap{}, err
	}
	types, err := a.store.JobTypes(ctx, id)
	if err != nil {
		return domain.Bootstrap{}, err
	}
	scenarios, err := a.store.AllScenarios(ctx, id)
	if err != nil {
		return domain.Bootstrap{}, err
	}
	pending, err := a.store.Pending(ctx, id)
	if err != nil {
		return domain.Bootstrap{}, err
	}
	return domain.Bootstrap{AppVersion: version, Profiles: profiles, SelectedProfileID: id, JobTypes: types, Scenarios: scenarios, Runtime: a.manager.States(), Connection: a.manager.Connection(), Pending: pending, DataPath: a.store.Path()}, nil
}
func (a *App) CheckConnection() (domain.ConnectionStatus, error) {
	return a.manager.ConnectCheck(context.Background())
}
func (a *App) ListProcesses(profileID string) ([]operate.Process, error) {
	p, err := a.store.Profile(context.Background(), profileID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(p.CommandTimeoutMS)*time.Millisecond)
	defer cancel()
	return operate.ListProcesses(ctx, p.OperateURL, operate.Auth{Mode: p.OperateAuthMode, Username: p.OperateUsername, Password: p.OperatePassword, Token: p.OperateToken})
}

func (a *App) GetProcessTasks(profileID, key, processID string) ([]operate.Task, error) {
	p, err := a.store.Profile(context.Background(), profileID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(p.CommandTimeoutMS)*time.Millisecond)
	defer cancel()
	return operate.GetProcessTasks(ctx, p.OperateURL, operate.Auth{Mode: p.OperateAuthMode, Username: p.OperateUsername, Password: p.OperatePassword, Token: p.OperateToken}, key, processID)
}

func (a *App) StartProcess(request domain.StartProcessRequest) (domain.ProcessInstance, error) {
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return a.manager.StartProcess(ctx, request)
}
func (a *App) CheckForUpdates() (updatechecker.Info, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	return (updatechecker.Client{HTTPClient: &http.Client{Timeout: 8 * time.Second}}).Check(ctx, version)
}
func (a *App) OpenReleasesPage() {
	wailsruntime.BrowserOpenURL(a.ctx, releasesURL)
}
func (a *App) StartType(id string) error { return a.manager.StartType(context.Background(), id) }
func (a *App) StopType(id string)        { a.manager.StopType(id) }
func (a *App) StartAll() error           { return a.manager.StartAll(context.Background()) }
func (a *App) StopAll()                  { a.manager.StopAll() }

func (a *App) SelectProfile(id string) error {
	if a.manager.HasWork() {
		return errors.New("stop all workers and wait for active jobs to finish")
	}
	p, err := a.store.Profile(context.Background(), id)
	if err != nil {
		return err
	}
	if err = a.manager.SetProfile(context.Background(), p); err != nil {
		return err
	}
	return a.store.SetSelectedProfileID(context.Background(), id)
}
func (a *App) SaveProfile(p domain.Profile) (domain.Profile, error) {
	ctx := context.Background()
	if p.ID == "" {
		p.ID = uuid.NewString()
	}
	if old, err := a.store.Profile(ctx, p.ID); err == nil && a.manager.HasWork() {
		if old.Host != p.Host || old.Port != p.Port || old.SelectedVersion != p.SelectedVersion || old.CommandTimeoutMS != p.CommandTimeoutMS || old.ActivationTimeoutMS != p.ActivationTimeoutMS || old.RenewalIntervalMS != p.RenewalIntervalMS || old.MaxActiveJobs != p.MaxActiveJobs {
			return p, errors.New("network settings cannot be changed while workers are running or jobs are active")
		}
	}
	if err := a.store.SaveProfile(ctx, p); err != nil {
		return p, err
	}
	selected, _ := a.store.SelectedProfileID(ctx)
	if selected == p.ID && !a.manager.HasWork() {
		saved, _ := a.store.Profile(ctx, p.ID)
		_ = a.manager.SetProfile(ctx, saved)
	}
	return a.store.Profile(ctx, p.ID)
}
func (a *App) DeleteProfile(id string) error {
	if a.manager.HasWork() {
		return errors.New("a profile cannot be deleted while jobs are being processed")
	}
	profiles, err := a.store.Profiles(context.Background())
	if err != nil {
		return err
	}
	if len(profiles) <= 1 {
		return errors.New("the only profile cannot be deleted")
	}
	selected, _ := a.store.SelectedProfileID(context.Background())
	if selected == id {
		return errors.New("select another profile first")
	}
	return a.store.DeleteProfile(context.Background(), id)
}

func (a *App) SaveJobType(c domain.JobTypeConfig) (domain.JobTypeConfig, error) {
	return a.store.SaveJobType(context.Background(), c)
}
func (a *App) DeleteJobType(id string) error {
	for _, s := range a.manager.States() {
		if s.ConfigID == id && (s.State != "stopped" || s.ActiveJobs > 0) {
			return errors.New("stop the job type and wait for its active jobs to finish")
		}
	}
	return a.store.DeleteJobType(context.Background(), id)
}
func (a *App) SaveScenario(s domain.Scenario) (domain.Scenario, error) {
	return a.store.SaveScenario(context.Background(), s)
}
func (a *App) DuplicateScenario(id string) (domain.Scenario, error) {
	s, err := a.store.Scenario(context.Background(), id)
	if err != nil {
		return s, err
	}
	s.ID = ""
	s.Name += " — copy"
	return a.store.SaveScenario(context.Background(), s)
}
func (a *App) DeleteScenario(id string) error {
	s, err := a.store.Scenario(context.Background(), id)
	if err != nil {
		return err
	}
	cfg, err := a.store.JobType(context.Background(), s.JobTypeConfigID)
	if err != nil {
		return err
	}
	if cfg.ActiveScenarioID != nil && *cfg.ActiveScenarioID == id {
		running := false
		for _, state := range a.manager.States() {
			if state.ConfigID == cfg.ID && state.State != "stopped" {
				running = true
			}
		}
		if running {
			return errors.New("select another active scenario or stop the job type first")
		}
		cfg.ActiveScenarioID = nil
		if _, err = a.store.SaveJobType(context.Background(), cfg); err != nil {
			return err
		}
	}
	return a.store.DeleteScenario(context.Background(), id)
}

func (a *App) SaveDraft(id string, d domain.ResponseDraft) error {
	return a.manager.SaveDraft(context.Background(), id, d)
}
func (a *App) ApplyScenario(id, scenarioID string) (domain.ResponseDraft, error) {
	return a.manager.ApplyScenario(context.Background(), id, scenarioID)
}
func (a *App) SubmitResponse(id string, d domain.ResponseDraft) (domain.ResponseAttempt, error) {
	return a.manager.Submit(context.Background(), id, d)
}
func (a *App) History(q domain.HistoryQuery) (domain.HistoryPage, error) {
	return a.store.History(context.Background(), q)
}
func (a *App) Attempts(activationID string) ([]domain.ResponseAttempt, error) {
	return a.store.Attempts(context.Background(), activationID)
}
func (a *App) ClearHistory(profileID string, confirmed bool) (int64, error) {
	if !confirmed {
		return 0, errors.New("confirmation is required to clear history")
	}
	return a.store.ClearHistory(context.Background(), profileID, domain.UTCNow())
}

func (a *App) ExportProfileJSON(id string) (string, error) {
	p, err := a.store.Profile(context.Background(), id)
	if err != nil {
		return "", err
	}
	types, err := a.store.JobTypes(context.Background(), id)
	if err != nil {
		return "", err
	}
	scenarios, err := a.store.AllScenarios(context.Background(), id)
	if err != nil {
		return "", err
	}
	raw, err := configio.Encode(p, types, scenarios)
	return string(raw), err
}
func (a *App) ImportProfileJSON(raw string) (domain.Profile, error) {
	d, err := configio.Decode([]byte(raw))
	if err != nil {
		return domain.Profile{}, err
	}
	return a.store.ImportProfile(context.Background(), d.Profile, d.JobTypes, d.Scenarios)
}
func (a *App) ExportProfileFile(id string) error {
	raw, err := a.ExportProfileJSON(id)
	if err != nil {
		return err
	}
	path, err := wailsruntime.SaveFileDialog(a.ctx, wailsruntime.SaveDialogOptions{Title: "Export profile", DefaultFilename: "camunda-stub-profile.json", Filters: []wailsruntime.FileFilter{{DisplayName: "JSON", Pattern: "*.json"}}})
	if err != nil || path == "" {
		return err
	}
	return os.WriteFile(path, []byte(raw), 0600)
}
func (a *App) ImportProfileFile() (domain.Profile, error) {
	path, err := wailsruntime.OpenFileDialog(a.ctx, wailsruntime.OpenDialogOptions{Title: "Import profile", Filters: []wailsruntime.FileFilter{{DisplayName: "JSON", Pattern: "*.json"}}})
	if err != nil || path == "" {
		return domain.Profile{}, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return domain.Profile{}, err
	}
	return a.ImportProfileJSON(string(raw))
}
func (a *App) OpenDataDirectory() error {
	return openDirectory(a.dataDir)
}
func (a *App) FormatJSON(raw string) (string, error) {
	if err := domain.ValidateJSONObject(raw); err != nil {
		return "", err
	}
	var value any
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&value); err != nil {
		return "", err
	}
	formatted, err := json.MarshalIndent(value, "", "  ")
	return string(formatted), err
}
