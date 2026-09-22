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
