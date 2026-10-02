package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/mishankov/camunda-stub-worker/internal/domain"
)

func TestDefaultsAndRestartRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	profiles, err := s.Profiles(ctx)
	if err != nil || len(profiles) != 1 {
		t.Fatalf("profiles=%v err=%v", profiles, err)
	}
	p := profiles[0]
	if p.Name != "Local Camunda" {
		t.Fatalf("default profile name=%q", p.Name)
	}
	cfg, err := s.SaveJobType(ctx, domain.JobTypeConfig{ProfileID: p.ID, JobType: "payment", Mode: domain.ModeManual, MaxActiveJobs: 1})
	if err != nil {
		t.Fatal(err)
	}
	a := domain.Activation{ID: "a", ProfileID: p.ID, JobTypeConfigID: cfg.ID, JobKey: "9223372036854775807", JobType: cfg.JobType, CustomHeadersJSON: `{}`, InputJSON: `{"large":9007199254740993}`, Mode: domain.ModeManual, ActivationState: domain.ActivationActive, SendStatus: domain.SendSending, ReceivedAt: domain.UTCNow(), ConfirmedDeadlineAt: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano), ProfileSnapshotJSON: `{}`}
	if err = s.InsertActivation(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.Activation(ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	if got.ActivationState != domain.ActivationInterrupted {
		t.Fatalf("state=%s", got.ActivationState)
	}
	if got.SendStatus != domain.SendUnknown {
		t.Fatalf("send status=%s", got.SendStatus)
	}
	if got.ActivationStateReason != "The application was restarted" {
		t.Fatalf("restart reason=%q", got.ActivationStateReason)
	}
	if got.JobKey != "9223372036854775807" || got.InputJSON != a.InputJSON {
		t.Fatal("precision-sensitive strings changed")
	}
}

func TestMigrationRenamesBuiltInRussianProfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	oldName := "\u041b\u043e\u043a\u0430\u043b\u044c\u043d\u0430\u044f Camunda"
	if _, err = s.db.ExecContext(ctx, `UPDATE connection_profiles SET name=?`, oldName); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.ExecContext(ctx, `DELETE FROM schema_migrations WHERE version=2`); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}

	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	profiles, err := s.Profiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 1 || profiles[0].Name != "Local Camunda" {
		t.Fatalf("profiles=%v", profiles)
	}
}

func TestImportIsIndependent(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	profiles, _ := s.Profiles(ctx)
	source := profiles[0]
	active := "old-s"
	types := []domain.JobTypeConfig{{ID: "old-t", ProfileID: source.ID, JobType: "ship", Mode: domain.ModeAuto, ActiveScenarioID: &active, MaxActiveJobs: 5}}
	scenarios := []domain.Scenario{{ID: active, JobTypeConfigID: "old-t", Name: "ok", Outcome: domain.OutcomeSuccess, VariablesJSON: `{"id":99999999999999999999}`}}
	imported, err := s.ImportProfile(ctx, source, types, scenarios)
	if err != nil {
		t.Fatal(err)
	}
	if imported.ID == source.ID || imported.Name == source.Name {
		t.Fatal("import did not create an independent profile/name")
	}
	gotTypes, _ := s.JobTypes(ctx, imported.ID)
	gotScenarios, _ := s.AllScenarios(ctx, imported.ID)
	if len(gotTypes) != 1 || len(gotScenarios) != 1 || gotScenarios[0].VariablesJSON != scenarios[0].VariablesJSON {
		t.Fatal("import content mismatch")
	}
	if gotTypes[0].ActiveScenarioID == nil || *gotTypes[0].ActiveScenarioID != gotScenarios[0].ID {
		t.Fatal("active scenario was not remapped")
	}
}

func TestCallHistoryForJobType(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	profiles, _ := s.Profiles(ctx)
	p := profiles[0]
	cfgA, err := s.SaveJobType(ctx, domain.JobTypeConfig{ProfileID: p.ID, JobType: "a", Mode: domain.ModeManual, MaxActiveJobs: 1})
	if err != nil {
		t.Fatal(err)
	}
	cfgB, err := s.SaveJobType(ctx, domain.JobTypeConfig{ProfileID: p.ID, JobType: "b", Mode: domain.ModeAuto, MaxActiveJobs: 1})
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{"act-0", "act-1", "act-2"}
	cfgs := []domain.JobTypeConfig{cfgA, cfgA, cfgB}
	for i, cfg := range cfgs {
		a := domain.Activation{ID: ids[i], ProfileID: p.ID, JobTypeConfigID: cfg.ID, JobKey: "k", JobType: cfg.JobType, Mode: cfg.Mode, InputJSON: `{"in":1}`, ActivationState: domain.ActivationActive, SendStatus: domain.SendNotPrepared, ReceivedAt: domain.UTCNow(), ConfirmedDeadlineAt: domain.UTCNow(), ProfileSnapshotJSON: `{}`}
		if err = s.InsertActivation(ctx, a); err != nil {
			t.Fatal(err)
		}
		attempt, err := s.BeginAttempt(ctx, ids[i], string(domain.OutcomeSuccess), `{"outcome":"success","variablesJson":"{\"out\":1}"}`)
		if err != nil {
			t.Fatal(err)
		}
		if err = s.FinishAttempt(ctx, attempt.ID, ids[i], domain.SendConfirmed, "", "", 5); err != nil {
			t.Fatal(err)
		}
	}
	// Fix the attempt timestamps so the newest-first order is deterministic.
	for _, row := range []struct{ id, at string }{{"act-0", "2026-01-01T00:00:00Z"}, {"act-1", "2026-01-01T00:00:02Z"}, {"act-2", "2026-01-01T00:00:01Z"}} {
		if _, err = s.db.ExecContext(ctx, `UPDATE response_attempts SET started_at=? WHERE activation_id=?`, row.at, row.id); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := s.CallHistoryForJobType(ctx, cfgA.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries=%v", entries)
	}
	for _, e := range entries {
		if e.JobType != "a" || e.InputContext != `{"in":1}` || e.OutputContext != `{"out":1}` || e.ResponseType != "complete" || e.CallType != domain.ModeManual || e.SendStatus != domain.SendConfirmed || e.ActivationState != domain.ActivationFinished {
			t.Fatalf("entry=%+v", e)
		}
		activation, err := s.Activation(ctx, e.ActivationID)
		if err != nil || activation.JobTypeConfigID != cfgA.ID {
			t.Fatalf("call history activation=%+v err=%v", activation, err)
		}
	}
	if entries[0].ActivationID != "act-1" || entries[1].ActivationID != "act-0" || entries[0].Time != "2026-01-01T00:00:02Z" {
		t.Fatalf("newest first expected act-1, got %v", entries[0])
	}
	limited, err := s.CallHistoryForJobType(ctx, cfgA.ID, 1)
	if err != nil || len(limited) != 1 || limited[0].Time != "2026-01-01T00:00:02Z" {
		t.Fatalf("limited=%v err=%v", limited, err)
	}
}

func TestCallHistoryIncludesUnansweredCalls(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	profiles, err := s.Profiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := s.SaveJobType(ctx, domain.JobTypeConfig{ProfileID: profiles[0].ID, JobType: "test", Mode: domain.ModeManual, MaxActiveJobs: 1})
	if err != nil {
		t.Fatal(err)
	}
	states := []domain.ActivationState{domain.ActivationActive, domain.ActivationExpired, domain.ActivationInterrupted}
	for _, state := range states {
		a := domain.Activation{ID: string(state), ProfileID: cfg.ProfileID, JobTypeConfigID: cfg.ID, JobKey: "job", JobType: cfg.JobType, Mode: cfg.Mode, InputJSON: `{"in":1}`, ActivationState: state, SendStatus: domain.SendNotPrepared, ReceivedAt: "2026-01-01T00:00:00Z", ConfirmedDeadlineAt: "2026-01-01T00:01:00Z", ProfileSnapshotJSON: `{}`}
		if err = s.InsertActivation(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := s.CallHistoryForJobType(ctx, cfg.ID, 50)
	if err != nil || len(entries) != len(states) {
		t.Fatalf("entries=%v err=%v", entries, err)
	}
	for _, entry := range entries {
		if entry.ActivationID != string(entry.ActivationState) || entry.Time != "2026-01-01T00:00:00Z" || entry.InputContext != `{"in":1}` || entry.ResponseType != "" || entry.OutputContext != "" || entry.SendStatus != domain.SendNotPrepared {
			t.Fatalf("unanswered entry=%+v", entry)
		}
	}
	// Beginning a response replaces the unanswered entry with its attempt.
	if _, err = s.BeginAttempt(ctx, string(domain.ActivationActive), string(domain.OutcomeBusinessError), `{"variablesJson":"{}"}`); err != nil {
		t.Fatal(err)
	}
	entries, err = s.CallHistoryForJobType(ctx, cfg.ID, 50)
	if err != nil || len(entries) != len(states) || entries[0].ActivationID != string(domain.ActivationActive) || entries[0].SendStatus != domain.SendSending || entries[0].ResponseType != "throwError" {
		t.Fatalf("after sending: entries=%v err=%v", entries, err)
	}
	// Clearing completed history removes unanswered expired/interrupted calls,
	// while the active call and its attempt remain inspectable.
	deleted, err := s.ClearHistory(ctx, cfg.ProfileID, "2027-01-01T00:00:00Z")
	if err != nil || deleted != 2 {
		t.Fatalf("deleted=%d err=%v", deleted, err)
	}
	entries, err = s.CallHistoryForJobType(ctx, cfg.ID, 50)
	if err != nil || len(entries) != 1 || entries[0].ActivationID != string(domain.ActivationActive) {
		t.Fatalf("after clearing: entries=%v err=%v", entries, err)
	}
}

func TestCallHistoryPreservesEachAttemptStatus(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	a := domain.Activation{ID: "activation", ProfileID: "profile", JobTypeConfigID: "config", JobKey: "job", JobType: "test", Mode: domain.ModeAuto, InputJSON: `{}`, ActivationState: domain.ActivationActive, SendStatus: domain.SendPrepared, ReceivedAt: domain.UTCNow(), ConfirmedDeadlineAt: domain.UTCNow(), ProfileSnapshotJSON: `{}`}
	if err = s.InsertActivation(ctx, a); err != nil {
		t.Fatal(err)
	}
	for _, status := range []domain.SendStatus{domain.SendFailed, domain.SendConfirmed} {
		attempt, err := s.BeginAttempt(ctx, a.ID, string(domain.OutcomeTechnicalFail), `{"variablesJson":"{\"out\":1}"}`)
		if err != nil {
			t.Fatal(err)
		}
		if err = s.FinishAttempt(ctx, attempt.ID, a.ID, status, "", "", 5); err != nil {
			t.Fatal(err)
		}
	}
	// Identical timestamps exercise the sequence tie breaker for retries.
	if _, err = s.db.ExecContext(ctx, `UPDATE response_attempts SET started_at='2026-01-01T00:00:00Z'`); err != nil {
		t.Fatal(err)
	}
	entries, err := s.CallHistoryForJobType(ctx, a.JobTypeConfigID, 50)
	if err != nil || len(entries) != 2 {
		t.Fatalf("entries=%v err=%v", entries, err)
	}
	if entries[0].SendStatus != domain.SendConfirmed || entries[1].SendStatus != domain.SendFailed {
		t.Fatalf("retry must not overwrite earlier status: entries=%+v", entries)
	}
	for _, entry := range entries {
		if entry.ResponseType != "fail" || entry.CallType != domain.ModeAuto || entry.OutputContext != `{"out":1}` {
			t.Fatalf("entry=%+v", entry)
		}
	}
}

func TestOperateURLMigrationAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	// Recreate the previous schema to exercise an actual upgrade.
	if _, err = s.db.ExecContext(ctx, `ALTER TABLE connection_profiles DROP COLUMN operate_url`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.ExecContext(ctx, `DELETE FROM schema_migrations WHERE version=3`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	profiles, err := s.Profiles(ctx)
	if err != nil || profiles[0].OperateURL != "" {
		t.Fatalf("profiles=%v err=%v", profiles, err)
	}
	p := profiles[0]
	p.OperateURL = "https://operate.example.test/operate"
	if err = s.SaveProfile(ctx, p); err != nil {
		t.Fatal(err)
	}
	imported, err := s.ImportProfile(ctx, p, nil, nil)
	if err != nil || imported.OperateURL != p.OperateURL {
		t.Fatalf("import=%v err=%v", imported, err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	saved, err := s.Profile(ctx, p.ID)
	if err != nil || saved.OperateURL != p.OperateURL {
		t.Fatalf("saved=%v err=%v", saved, err)
	}
}

func TestOperateCredentialsDefaultsMigrationAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	profiles, err := s.Profiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p := profiles[0]
	if p.OperateURL != "http://localhost:8081" || p.OperateAuthMode != "password" || p.OperateUsername != "demo" || p.OperatePassword != "demo" {
		t.Fatal("missing local Operate defaults")
	}
	custom := p
	custom.ID = "custom"
	custom.Name = "Custom connection"
	custom.OperateURL = "https://operate.example.test"
	if err = s.SaveProfile(ctx, custom); err != nil {
		t.Fatal(err)
	}
	// Upgrade a v3 database with an unconfigured local profile and a custom URL.
	for _, query := range []string{
		`UPDATE connection_profiles SET operate_url='' WHERE name='Local Camunda'`,
		`ALTER TABLE connection_profiles DROP COLUMN operate_auth_mode`,
		`ALTER TABLE connection_profiles DROP COLUMN operate_username`,
		`ALTER TABLE connection_profiles DROP COLUMN operate_password`,
		`ALTER TABLE connection_profiles DROP COLUMN operate_token`,
		`DELETE FROM schema_migrations WHERE version=4`,
	} {
		if _, err = s.db.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	p, err = s.Profile(ctx, p.ID)
	if err != nil || p.OperatePassword != "demo" || p.OperateURL != "http://localhost:8081" {
		t.Fatal("default migration failed")
	}
	custom, err = s.Profile(ctx, custom.ID)
	if err != nil || custom.OperateURL != "https://operate.example.test" || custom.OperateAuthMode != "none" {
		t.Fatal("customized connection changed")
	}
	p.OperateUsername = "alice"
	p.OperatePassword = "test-password"
	if err = s.SaveProfile(ctx, p); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, err = s.Profile(ctx, p.ID)
	if err != nil || p.OperateUsername != "alice" || p.OperatePassword != "test-password" {
		t.Fatal("credentials did not survive restart")
	}
	p.OperateAuthMode = "token"
	p.OperateToken = "test-token"
	if err = s.SaveProfile(ctx, p); err != nil {
		t.Fatal(err)
	}
	p, err = s.Profile(ctx, p.ID)
	if err != nil || p.OperateToken != "test-token" || p.OperateUsername != "" || p.OperatePassword != "" {
		t.Fatal("token mode did not clear password")
	}
	p.OperateAuthMode = "none"
	if err = s.SaveProfile(ctx, p); err != nil {
		t.Fatal(err)
	}
	p, err = s.Profile(ctx, p.ID)
	if err != nil || p.OperateToken != "" {
		t.Fatal("none mode did not clear credentials")
	}
}

func TestDefaultResponsesFollowDisplayOrderAndPreserveSelection(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "defaults.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	profiles, err := s.Profiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	profileID := profiles[0].ID
	worker, err := s.SaveJobType(ctx, domain.JobTypeConfig{ProfileID: profileID, JobType: "default-response", Mode: domain.ModeManual, MaxActiveJobs: 1})
	if err != nil {
		t.Fatal(err)
	}
	ensure := func() {
		t.Helper()
		if err := s.EnsureDefaultResponses(ctx, profileID); err != nil {
			t.Fatal(err)
		}
	}
	assertSelection := func(want string) {
		t.Helper()
		got, err := s.JobType(ctx, worker.ID)
		if err != nil {
			t.Fatal(err)
		}
		if want == "" {
			if got.ActiveScenarioID != nil {
				t.Fatalf("unexpected selection: %v", *got.ActiveScenarioID)
			}
		} else if got.ActiveScenarioID == nil || *got.ActiveScenarioID != want {
			t.Fatalf("selection=%v want=%s", got.ActiveScenarioID, want)
		}
	}
	save := func(name string) domain.Scenario {
		t.Helper()
		v, err := s.SaveScenario(ctx, domain.Scenario{JobTypeConfigID: worker.ID, Name: name, Outcome: domain.OutcomeSuccess, VariablesJSON: "{}"})
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	ensure()
	assertSelection("")
	z := save("Z response")
	assertSelection(z.ID) // The first save must select it without Bootstrap or refresh.
	a := save("A response")
	assertSelection(z.ID) // Adding a response must not replace the user's selection.
	if _, err := s.SaveJobType(ctx, worker); err != nil {
		t.Fatal(err)
	} // Simulate an older unselected worker.
	if err := s.EnsureDefaultResponses(ctx, "another-profile"); err != nil {
		t.Fatal(err)
	}
	assertSelection("")
	ensure()
	assertSelection(a.ID)
	worker.ActiveScenarioID = &z.ID
	if _, err := s.SaveJobType(ctx, worker); err != nil {
		t.Fatal(err)
	}
	ensure()
	assertSelection(z.ID)
	if err := s.DeleteScenario(ctx, z.ID); err != nil {
		t.Fatal(err)
	}
	ensure()
	assertSelection(a.ID)
}
