package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"github.com/mishankov/camunda-stub-worker/internal/domain"
)

type Store struct {
	db   *sql.DB
	path string
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, path: path}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, pragma := range []string{"PRAGMA foreign_keys=ON", "PRAGMA journal_mode=WAL", "PRAGMA busy_timeout=5000", "PRAGMA synchronous=NORMAL"} {
		if _, err = db.ExecContext(ctx, pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("sqlite %s: %w", pragma, err)
		}
	}
	if err = s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if err = s.recoverInterrupted(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if err = s.ensureDefaultProfile(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }
func (s *Store) Path() string { return s.path }

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations(version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return err
	}
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version=1`).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		statements := []string{
			`CREATE TABLE connection_profiles(
 id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE, host TEXT NOT NULL, port INTEGER NOT NULL,
 selected_version TEXT NOT NULL, compatibility_verified INTEGER NOT NULL DEFAULT 0,
 command_timeout_ms INTEGER NOT NULL, activation_timeout_ms INTEGER NOT NULL,
 renewal_interval_ms INTEGER NOT NULL, max_active_jobs INTEGER NOT NULL,
 history_retention_days INTEGER NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
)`,
			`CREATE TABLE job_type_configs(
 id TEXT PRIMARY KEY, profile_id TEXT NOT NULL REFERENCES connection_profiles(id) ON DELETE CASCADE,
 job_type TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', mode TEXT NOT NULL,
 active_scenario_id TEXT, max_active_jobs INTEGER NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
 UNIQUE(profile_id, job_type)
)`,
			`CREATE TABLE scenarios(
 id TEXT PRIMARY KEY, job_type_config_id TEXT NOT NULL REFERENCES job_type_configs(id) ON DELETE CASCADE,
 name TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', outcome TEXT NOT NULL,
 variables_json TEXT NOT NULL, delay_ms INTEGER NOT NULL DEFAULT 0, error_code TEXT NOT NULL DEFAULT '',
 error_message TEXT NOT NULL DEFAULT '', remaining_retries INTEGER NOT NULL DEFAULT 0,
 retry_backoff_ms INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
 UNIQUE(job_type_config_id, name)
)`,
			`CREATE TABLE activations(
 id TEXT PRIMARY KEY, profile_id TEXT NOT NULL, job_type_config_id TEXT NOT NULL,
 job_key TEXT NOT NULL, job_type TEXT NOT NULL, process_instance_key TEXT NOT NULL DEFAULT '',
 process_definition_key TEXT NOT NULL DEFAULT '', bpmn_process_id TEXT NOT NULL DEFAULT '',
 element_id TEXT NOT NULL DEFAULT '', element_instance_key TEXT NOT NULL DEFAULT '', retries INTEGER NOT NULL,
 custom_headers_json TEXT NOT NULL, input_json TEXT NOT NULL, mode TEXT NOT NULL,
 scenario_snapshot_json TEXT NOT NULL DEFAULT '', draft_json TEXT NOT NULL DEFAULT '',
 activation_state TEXT NOT NULL, activation_state_reason TEXT NOT NULL DEFAULT '', send_status TEXT NOT NULL,
 received_at TEXT NOT NULL, prepared_at TEXT NOT NULL DEFAULT '', confirmed_deadline_at TEXT NOT NULL,
 finished_at TEXT NOT NULL DEFAULT '', profile_snapshot_json TEXT NOT NULL
)`,
			`CREATE INDEX idx_activations_profile_received ON activations(profile_id, received_at DESC)`,
			`CREATE INDEX idx_activations_pending ON activations(activation_state, mode, received_at)`,
			`CREATE TABLE response_attempts(
 id TEXT PRIMARY KEY, activation_id TEXT NOT NULL REFERENCES activations(id) ON DELETE CASCADE,
 sequence INTEGER NOT NULL, command TEXT NOT NULL, payload_json TEXT NOT NULL, status TEXT NOT NULL,
 started_at TEXT NOT NULL, finished_at TEXT NOT NULL DEFAULT '', duration_ms INTEGER NOT NULL DEFAULT 0,
 diagnostic_code TEXT NOT NULL DEFAULT '', diagnostic_text TEXT NOT NULL DEFAULT '',
 UNIQUE(activation_id, sequence)
)`,
			`CREATE TABLE activation_events(
 id TEXT PRIMARY KEY, activation_id TEXT NOT NULL REFERENCES activations(id) ON DELETE CASCADE,
 kind TEXT NOT NULL, success INTEGER NOT NULL, details TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL
)`,
			`CREATE INDEX idx_activation_events_activation ON activation_events(activation_id, created_at)`,
			`CREATE TABLE app_settings(key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
			`INSERT INTO schema_migrations(version, applied_at) VALUES(1, datetime('now'))`,
		}
		for _, stmt := range statements {
			if _, err = tx.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("migration 1: %w", err)
			}
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}

	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version=2`).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if _, err = tx.ExecContext(ctx, `UPDATE connection_profiles SET name='Local Camunda', updated_at=?
 WHERE name='Локальная Camunda'
 AND NOT EXISTS (SELECT 1 FROM connection_profiles WHERE name='Local Camunda')`, domain.UTCNow()); err != nil {
			return fmt.Errorf("migration 2: %w", err)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations(version, applied_at) VALUES(2, datetime('now'))`); err != nil {
			return fmt.Errorf("migration 2: %w", err)
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version=3`).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if _, err = tx.ExecContext(ctx, `ALTER TABLE connection_profiles ADD COLUMN operate_url TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("migration 3: %w", err)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations(version, applied_at) VALUES(3, datetime('now'))`); err != nil {
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}

	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version=4`).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		for _, stmt := range []string{
			`ALTER TABLE connection_profiles ADD COLUMN operate_auth_mode TEXT NOT NULL DEFAULT 'none'`,
			`ALTER TABLE connection_profiles ADD COLUMN operate_username TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE connection_profiles ADD COLUMN operate_password TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE connection_profiles ADD COLUMN operate_token TEXT NOT NULL DEFAULT ''`,
			`UPDATE connection_profiles SET operate_url='http://localhost:8081', operate_auth_mode='password', operate_username='demo', operate_password='demo' WHERE name='Local Camunda' AND host='localhost' AND port=26500 AND operate_url=''`,
			`INSERT INTO schema_migrations(version,applied_at) VALUES(4,datetime('now'))`,
		} {
			if _, err = tx.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("migration 4: %w", err)
			}
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}

	return nil
}

func (s *Store) recoverInterrupted(ctx context.Context) error {
	now := domain.UTCNow()
	_, err := s.db.ExecContext(ctx, `UPDATE activations SET
	 activation_state='interrupted', activation_state_reason='The application was restarted', finished_at=?,
 send_status=CASE WHEN send_status='sending' THEN 'unknown' ELSE send_status END
 WHERE activation_state='active'`, now)
	return err
}

func (s *Store) ensureDefaultProfile(ctx context.Context) error {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM connection_profiles`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	now := domain.UTCNow()
	p := domain.Profile{ID: uuid.NewString(), Name: "Local Camunda", OperateURL: "http://localhost:8081", OperateAuthMode: "password", OperateUsername: "demo", OperatePassword: "demo", Host: "localhost", Port: 26500, SelectedVersion: "8.5", CompatibilityVerified: true, CommandTimeoutMS: 10000, ActivationTimeoutMS: 120000, RenewalIntervalMS: 30000, MaxActiveJobs: 10, HistoryRetentionDays: 30, CreatedAt: now, UpdatedAt: now}
	if err := s.SaveProfile(ctx, p); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO app_settings(key,value) VALUES('selected_profile_id',?)`, p.ID)
	return err
}

func (s *Store) Profiles(ctx context.Context) ([]domain.Profile, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,host,port,selected_version,compatibility_verified,command_timeout_ms,activation_timeout_ms,renewal_interval_ms,max_active_jobs,history_retention_days,created_at,updated_at,operate_url,operate_auth_mode,operate_username,operate_password,operate_token FROM connection_profiles ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Profile
	for rows.Next() {
		var p domain.Profile
		if err = rows.Scan(&p.ID, &p.Name, &p.Host, &p.Port, &p.SelectedVersion, &p.CompatibilityVerified, &p.CommandTimeoutMS, &p.ActivationTimeoutMS, &p.RenewalIntervalMS, &p.MaxActiveJobs, &p.HistoryRetentionDays, &p.CreatedAt, &p.UpdatedAt, &p.OperateURL, &p.OperateAuthMode, &p.OperateUsername, &p.OperatePassword, &p.OperateToken); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) Profile(ctx context.Context, id string) (domain.Profile, error) {
	var p domain.Profile
	err := s.db.QueryRowContext(ctx, `SELECT id,name,host,port,selected_version,compatibility_verified,command_timeout_ms,activation_timeout_ms,renewal_interval_ms,max_active_jobs,history_retention_days,created_at,updated_at,operate_url,operate_auth_mode,operate_username,operate_password,operate_token FROM connection_profiles WHERE id=?`, id).Scan(&p.ID, &p.Name, &p.Host, &p.Port, &p.SelectedVersion, &p.CompatibilityVerified, &p.CommandTimeoutMS, &p.ActivationTimeoutMS, &p.RenewalIntervalMS, &p.MaxActiveJobs, &p.HistoryRetentionDays, &p.CreatedAt, &p.UpdatedAt, &p.OperateURL, &p.OperateAuthMode, &p.OperateUsername, &p.OperatePassword, &p.OperateToken)
	return p, err
}

func (s *Store) SelectedProfileID(ctx context.Context) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM app_settings WHERE key='selected_profile_id'`).Scan(&v)
	return v, err
}
func (s *Store) SetSelectedProfileID(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO app_settings(key,value) VALUES('selected_profile_id',?)`, id)
	return err
}

func (s *Store) SaveProfile(ctx context.Context, p domain.Profile) error {
	if p.OperateAuthMode == "" || p.OperateAuthMode == "none" {
		p = p.WithoutOperateCredentials()
	}
	if p.OperateAuthMode == "password" {
		p.OperateToken = ""
	}
	if p.OperateAuthMode == "token" {
		p.OperateUsername = ""
		p.OperatePassword = ""
	}

	if err := domain.ValidateProfile(p); err != nil {
		return err
	}
	now := domain.UTCNow()
	if p.ID == "" {
		p.ID = uuid.NewString()
	}
	if p.CreatedAt == "" {
		p.CreatedAt = now
	}
	p.UpdatedAt = now
	p.CompatibilityVerified = p.SelectedVersion == "8.5"
	_, err := s.db.ExecContext(ctx, `INSERT INTO connection_profiles(id,name,host,port,selected_version,compatibility_verified,command_timeout_ms,activation_timeout_ms,renewal_interval_ms,max_active_jobs,history_retention_days,created_at,updated_at,operate_url,operate_auth_mode,operate_username,operate_password,operate_token) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,host=excluded.host,port=excluded.port,selected_version=excluded.selected_version,compatibility_verified=excluded.compatibility_verified,command_timeout_ms=excluded.command_timeout_ms,activation_timeout_ms=excluded.activation_timeout_ms,renewal_interval_ms=excluded.renewal_interval_ms,max_active_jobs=excluded.max_active_jobs,history_retention_days=excluded.history_retention_days,updated_at=excluded.updated_at,operate_url=excluded.operate_url,operate_auth_mode=excluded.operate_auth_mode,operate_username=excluded.operate_username,operate_password=excluded.operate_password,operate_token=excluded.operate_token`, p.ID, p.Name, p.Host, p.Port, p.SelectedVersion, p.CompatibilityVerified, p.CommandTimeoutMS, p.ActivationTimeoutMS, p.RenewalIntervalMS, p.MaxActiveJobs, p.HistoryRetentionDays, p.CreatedAt, p.UpdatedAt, p.OperateURL, p.OperateAuthMode, p.OperateUsername, p.OperatePassword, p.OperateToken)
	return friendlyConstraint(err)
}

func (s *Store) DeleteProfile(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM connection_profiles WHERE id=?`, id)
	return err
}

func (s *Store) JobTypes(ctx context.Context, profileID string) ([]domain.JobTypeConfig, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,profile_id,job_type,description,mode,active_scenario_id,max_active_jobs,created_at,updated_at FROM job_type_configs WHERE profile_id=? ORDER BY job_type`, profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.JobTypeConfig
	for rows.Next() {
		var c domain.JobTypeConfig
		var active sql.NullString
		if err = rows.Scan(&c.ID, &c.ProfileID, &c.JobType, &c.Description, &c.Mode, &active, &c.MaxActiveJobs, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		if active.Valid {
			c.ActiveScenarioID = &active.String
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) JobType(ctx context.Context, id string) (domain.JobTypeConfig, error) {
	var c domain.JobTypeConfig
	var active sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT id,profile_id,job_type,description,mode,active_scenario_id,max_active_jobs,created_at,updated_at FROM job_type_configs WHERE id=?`, id).Scan(&c.ID, &c.ProfileID, &c.JobType, &c.Description, &c.Mode, &active, &c.MaxActiveJobs, &c.CreatedAt, &c.UpdatedAt)
	if active.Valid {
		c.ActiveScenarioID = &active.String
	}
	return c, err
}

func (s *Store) SaveJobType(ctx context.Context, c domain.JobTypeConfig) (domain.JobTypeConfig, error) {
	if err := domain.ValidateJobType(c); err != nil {
		return c, err
	}
	now := domain.UTCNow()
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	if c.CreatedAt == "" {
		c.CreatedAt = now
	}
	c.UpdatedAt = now
	_, err := s.db.ExecContext(ctx, `INSERT INTO job_type_configs(id,profile_id,job_type,description,mode,active_scenario_id,max_active_jobs,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET job_type=excluded.job_type,description=excluded.description,mode=excluded.mode,active_scenario_id=excluded.active_scenario_id,max_active_jobs=excluded.max_active_jobs,updated_at=excluded.updated_at`, c.ID, c.ProfileID, c.JobType, c.Description, c.Mode, c.ActiveScenarioID, c.MaxActiveJobs, c.CreatedAt, c.UpdatedAt)
	return c, friendlyConstraint(err)
}

func (s *Store) DeleteJobType(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM job_type_configs WHERE id=?`, id)
	return err
}

func (s *Store) Scenarios(ctx context.Context, jobTypeID string) ([]domain.Scenario, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,job_type_config_id,name,description,outcome,variables_json,delay_ms,error_code,error_message,remaining_retries,retry_backoff_ms,created_at,updated_at FROM scenarios WHERE job_type_config_id=? ORDER BY name`, jobTypeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Scenario
	for rows.Next() {
		var v domain.Scenario
		if err = rows.Scan(&v.ID, &v.JobTypeConfigID, &v.Name, &v.Description, &v.Outcome, &v.VariablesJSON, &v.DelayMS, &v.ErrorCode, &v.ErrorMessage, &v.RemainingRetries, &v.RetryBackoffMS, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) AllScenarios(ctx context.Context, profileID string) ([]domain.Scenario, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT s.id,s.job_type_config_id,s.name,s.description,s.outcome,s.variables_json,s.delay_ms,s.error_code,s.error_message,s.remaining_retries,s.retry_backoff_ms,s.created_at,s.updated_at FROM scenarios s JOIN job_type_configs j ON j.id=s.job_type_config_id WHERE j.profile_id=? ORDER BY j.job_type,s.name`, profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Scenario
	for rows.Next() {
		var v domain.Scenario
		if err = rows.Scan(&v.ID, &v.JobTypeConfigID, &v.Name, &v.Description, &v.Outcome, &v.VariablesJSON, &v.DelayMS, &v.ErrorCode, &v.ErrorMessage, &v.RemainingRetries, &v.RetryBackoffMS, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) Scenario(ctx context.Context, id string) (domain.Scenario, error) {
	var v domain.Scenario
	err := s.db.QueryRowContext(ctx, `SELECT id,job_type_config_id,name,description,outcome,variables_json,delay_ms,error_code,error_message,remaining_retries,retry_backoff_ms,created_at,updated_at FROM scenarios WHERE id=?`, id).Scan(&v.ID, &v.JobTypeConfigID, &v.Name, &v.Description, &v.Outcome, &v.VariablesJSON, &v.DelayMS, &v.ErrorCode, &v.ErrorMessage, &v.RemainingRetries, &v.RetryBackoffMS, &v.CreatedAt, &v.UpdatedAt)
	return v, err
}

func (s *Store) SaveScenario(ctx context.Context, v domain.Scenario) (domain.Scenario, error) {
	if err := domain.ValidateScenario(v); err != nil {
		return v, err
	}
	now := domain.UTCNow()
	if v.ID == "" {
		v.ID = uuid.NewString()
	}
	if v.CreatedAt == "" {
		v.CreatedAt = now
	}
	v.UpdatedAt = now
	_, err := s.db.ExecContext(ctx, `INSERT INTO scenarios(id,job_type_config_id,name,description,outcome,variables_json,delay_ms,error_code,error_message,remaining_retries,retry_backoff_ms,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,description=excluded.description,outcome=excluded.outcome,variables_json=excluded.variables_json,delay_ms=excluded.delay_ms,error_code=excluded.error_code,error_message=excluded.error_message,remaining_retries=excluded.remaining_retries,retry_backoff_ms=excluded.retry_backoff_ms,updated_at=excluded.updated_at`, v.ID, v.JobTypeConfigID, v.Name, v.Description, v.Outcome, v.VariablesJSON, v.DelayMS, v.ErrorCode, v.ErrorMessage, v.RemainingRetries, v.RetryBackoffMS, v.CreatedAt, v.UpdatedAt)
	return v, friendlyConstraint(err)
}

func (s *Store) DeleteScenario(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM scenarios WHERE id=?`, id)
	return err
}

func (s *Store) InsertActivation(ctx context.Context, a domain.Activation) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO activations(id,profile_id,job_type_config_id,job_key,job_type,process_instance_key,process_definition_key,bpmn_process_id,element_id,element_instance_key,retries,custom_headers_json,input_json,mode,scenario_snapshot_json,draft_json,activation_state,activation_state_reason,send_status,received_at,prepared_at,confirmed_deadline_at,finished_at,profile_snapshot_json) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, a.ID, a.ProfileID, a.JobTypeConfigID, a.JobKey, a.JobType, a.ProcessInstanceKey, a.ProcessDefinitionKey, a.BPMNProcessID, a.ElementID, a.ElementInstanceKey, a.Retries, a.CustomHeadersJSON, a.InputJSON, a.Mode, a.ScenarioSnapshotJSON, a.DraftJSON, a.ActivationState, a.ActivationStateReason, a.SendStatus, a.ReceivedAt, a.PreparedAt, a.ConfirmedDeadlineAt, a.FinishedAt, a.ProfileSnapshotJSON)
	return err
}

func scanActivation(scanner interface{ Scan(...any) error }) (domain.Activation, error) {
	var a domain.Activation
	err := scanner.Scan(&a.ID, &a.ProfileID, &a.JobTypeConfigID, &a.JobKey, &a.JobType, &a.ProcessInstanceKey, &a.ProcessDefinitionKey, &a.BPMNProcessID, &a.ElementID, &a.ElementInstanceKey, &a.Retries, &a.CustomHeadersJSON, &a.InputJSON, &a.Mode, &a.ScenarioSnapshotJSON, &a.DraftJSON, &a.ActivationState, &a.ActivationStateReason, &a.SendStatus, &a.ReceivedAt, &a.PreparedAt, &a.ConfirmedDeadlineAt, &a.FinishedAt, &a.ProfileSnapshotJSON)
	return a, err
}

const activationColumns = `id,profile_id,job_type_config_id,job_key,job_type,process_instance_key,process_definition_key,bpmn_process_id,element_id,element_instance_key,retries,custom_headers_json,input_json,mode,scenario_snapshot_json,draft_json,activation_state,activation_state_reason,send_status,received_at,prepared_at,confirmed_deadline_at,finished_at,profile_snapshot_json`

func (s *Store) Activation(ctx context.Context, id string) (domain.Activation, error) {
	return scanActivation(s.db.QueryRowContext(ctx, `SELECT `+activationColumns+` FROM activations WHERE id=?`, id))
}
func (s *Store) Pending(ctx context.Context, profileID string) ([]domain.Activation, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+activationColumns+` FROM activations WHERE profile_id=? AND activation_state='active' AND mode='manual' ORDER BY received_at`, profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Activation
	for rows.Next() {
		a, e := scanActivation(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
func (s *Store) UpdateDraft(ctx context.Context, id, raw string, status domain.SendStatus) error {
	_, err := s.db.ExecContext(ctx, `UPDATE activations SET draft_json=?,send_status=?,prepared_at=? WHERE id=? AND activation_state='active' AND send_status NOT IN ('sending','confirmed','unknown')`, raw, status, domain.UTCNow(), id)
	return err
}

func (s *Store) UpdateDraftSnapshot(ctx context.Context, id, draftRaw, snapshotRaw string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE activations SET draft_json=?,scenario_snapshot_json=?,send_status='prepared',prepared_at=? WHERE id=? AND activation_state='active' AND send_status NOT IN ('sending','confirmed','unknown')`, draftRaw, snapshotRaw, domain.UTCNow(), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return errors.New("the activation no longer allows scenario changes")
	}
	return nil
}

func (s *Store) BeginAttempt(ctx context.Context, activationID, command, payload string) (domain.ResponseAttempt, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ResponseAttempt{}, err
	}
	defer tx.Rollback()
	var state string
	var status string
	if err = tx.QueryRowContext(ctx, `SELECT activation_state,send_status FROM activations WHERE id=?`, activationID).Scan(&state, &status); err != nil {
		return domain.ResponseAttempt{}, err
	}
	if state != "active" {
		return domain.ResponseAttempt{}, errors.New("the activation is no longer valid")
	}
	if status == "sending" || status == "confirmed" || status == "unknown" {
		return domain.ResponseAttempt{}, errors.New("the response is already being sent or retry is not allowed")
	}
	var seq int
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence),0)+1 FROM response_attempts WHERE activation_id=?`, activationID).Scan(&seq); err != nil {
		return domain.ResponseAttempt{}, err
	}
	a := domain.ResponseAttempt{ID: uuid.NewString(), ActivationID: activationID, Sequence: seq, Command: command, PayloadJSON: payload, Status: domain.SendSending, StartedAt: domain.UTCNow()}
	if _, err = tx.ExecContext(ctx, `INSERT INTO response_attempts(id,activation_id,sequence,command,payload_json,status,started_at) VALUES(?,?,?,?,?,?,?)`, a.ID, a.ActivationID, a.Sequence, a.Command, a.PayloadJSON, a.Status, a.StartedAt); err != nil {
		return a, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE activations SET send_status='sending' WHERE id=? AND activation_state='active' AND send_status NOT IN ('sending','confirmed','unknown')`, activationID)
	if err != nil {
		return a, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return a, errors.New("the activation state has changed")
	}
	return a, tx.Commit()
}

func (s *Store) FinishAttempt(ctx context.Context, attemptID, activationID string, status domain.SendStatus, code, text string, duration int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := domain.UTCNow()
	_, err = tx.ExecContext(ctx, `UPDATE response_attempts SET status=?,finished_at=?,duration_ms=?,diagnostic_code=?,diagnostic_text=? WHERE id=?`, status, now, duration, code, text, attemptID)
	if err != nil {
		return err
	}
	activationState := "active"
	finished := ""
	reason := ""
	if status == domain.SendConfirmed {
		activationState = "finished"
		finished = now
	} else if status == domain.SendUnknown {
		activationState = "expired_or_unconfirmed"
		finished = now
		reason = "Command outcome is unknown; retry is not allowed"
	}
	_, err = tx.ExecContext(ctx, `UPDATE activations SET send_status=?,activation_state=?,activation_state_reason=?,finished_at=? WHERE id=?`, status, activationState, reason, finished, activationID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) UpdateDeadline(ctx context.Context, id, deadline string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE activations SET confirmed_deadline_at=? WHERE id=? AND activation_state='active'`, deadline, id)
	return err
}
func (s *Store) ExpireActivation(ctx context.Context, id, reason string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE activations SET activation_state='expired_or_unconfirmed',activation_state_reason=?,finished_at=? WHERE id=? AND activation_state='active'`, reason, domain.UTCNow(), id)
	return err
}
func (s *Store) AddEvent(ctx context.Context, e domain.ActivationEvent) error {
	if e.ID == "" {
		e.ID = uuid.NewString()
	}
	if e.CreatedAt == "" {
		e.CreatedAt = domain.UTCNow()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO activation_events(id,activation_id,kind,success,details,created_at) VALUES(?,?,?,?,?,?)`, e.ID, e.ActivationID, e.Kind, e.Success, e.Details, e.CreatedAt)
	return err
}

func (s *Store) History(ctx context.Context, q domain.HistoryQuery) (domain.HistoryPage, error) {
	if q.Page < 1 {
		q.Page = 1
	}
	args := []any{}
	where := []string{"1=1"}
	if q.ProfileID != "" {
		where = append(where, "profile_id=?")
		args = append(args, q.ProfileID)
	}
	if q.JobType != "" {
		where = append(where, "job_type=?")
		args = append(args, q.JobType)
	}
	if q.SendStatus != "" {
		where = append(where, "send_status=?")
		args = append(args, q.SendStatus)
	}
	if q.Outcome != "" {
		where = append(where, "json_extract(draft_json,'$.outcome')=?")
		args = append(args, q.Outcome)
	}
	if q.Search != "" {
		where = append(where, "(job_key LIKE ? OR process_instance_key LIKE ?)")
		needle := "%" + q.Search + "%"
		args = append(args, needle, needle)
	}
	if q.FromUTC != "" {
		where = append(where, "received_at>=?")
		args = append(args, q.FromUTC)
	}
	if q.ToUTC != "" {
		where = append(where, "received_at<=?")
		args = append(args, q.ToUTC)
	}
	clause := strings.Join(where, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM activations WHERE `+clause, args...).Scan(&total); err != nil {
		return domain.HistoryPage{}, err
	}
	queryArgs := append(append([]any{}, args...), 100, (q.Page-1)*100)
	rows, err := s.db.QueryContext(ctx, `SELECT `+activationColumns+` FROM activations WHERE `+clause+` ORDER BY received_at DESC LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return domain.HistoryPage{}, err
	}
	defer rows.Close()
	items := []domain.Activation{}
	for rows.Next() {
		a, e := scanActivation(rows)
		if e != nil {
			return domain.HistoryPage{}, e
		}
		items = append(items, a)
	}
	return domain.HistoryPage{Items: items, Page: q.Page, PageSize: 100, Total: total}, rows.Err()
}

func (s *Store) Attempts(ctx context.Context, activationID string) ([]domain.ResponseAttempt, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,activation_id,sequence,command,payload_json,status,started_at,finished_at,duration_ms,diagnostic_code,diagnostic_text FROM response_attempts WHERE activation_id=? ORDER BY sequence`, activationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ResponseAttempt
	for rows.Next() {
		var a domain.ResponseAttempt
		if err = rows.Scan(&a.ID, &a.ActivationID, &a.Sequence, &a.Command, &a.PayloadJSON, &a.Status, &a.StartedAt, &a.FinishedAt, &a.DurationMS, &a.DiagnosticCode, &a.DiagnosticText); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) ClearHistory(ctx context.Context, profileID string, before string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM activations WHERE profile_id=? AND activation_state!='active' AND received_at<?`, profileID, before)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
func (s *Store) CleanupExpired(ctx context.Context) error {
	profiles, err := s.Profiles(ctx)
	if err != nil {
		return err
	}
	for _, p := range profiles {
		before := time.Now().UTC().Add(-time.Duration(p.HistoryRetentionDays) * 24 * time.Hour).Format(time.RFC3339Nano)
		if _, err = s.ClearHistory(ctx, p.ID, before); err != nil {
			return err
		}
	}
	return nil
}

// ImportProfile inserts a fully validated document in one transaction. All IDs are regenerated.
func (s *Store) ImportProfile(ctx context.Context, p domain.Profile, types []domain.JobTypeConfig, scenarios []domain.Scenario) (domain.Profile, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return p, err
	}
	defer tx.Rollback()
	base := p.Name
	name := base
	for suffix := 2; ; suffix++ {
		var n int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM connection_profiles WHERE name=?`, name).Scan(&n); err != nil {
			return p, err
		}
		if n == 0 {
			break
		}
		name = fmt.Sprintf("%s (%d)", base, suffix)
	}
	now := domain.UTCNow()
	oldProfile := p.ID
	p.ID = uuid.NewString()
	p.Name = name
	p.CreatedAt = now
	p.UpdatedAt = now
	p.CompatibilityVerified = p.SelectedVersion == "8.5"
	_, err = tx.ExecContext(ctx, `INSERT INTO connection_profiles(id,name,host,port,selected_version,compatibility_verified,command_timeout_ms,activation_timeout_ms,renewal_interval_ms,max_active_jobs,history_retention_days,created_at,updated_at,operate_url,operate_auth_mode,operate_username,operate_password,operate_token) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, p.ID, p.Name, p.Host, p.Port, p.SelectedVersion, p.CompatibilityVerified, p.CommandTimeoutMS, p.ActivationTimeoutMS, p.RenewalIntervalMS, p.MaxActiveJobs, p.HistoryRetentionDays, p.CreatedAt, p.UpdatedAt, p.OperateURL, p.OperateAuthMode, p.OperateUsername, p.OperatePassword, p.OperateToken)
	if err != nil {
		return p, err
	}
	typeMap := map[string]string{}
	scenarioMap := map[string]string{}
	for _, v := range scenarios {
		scenarioMap[v.ID] = uuid.NewString()
	}
	for _, c := range types {
		typeMap[c.ID] = uuid.NewString()
	}
	for _, c := range types {
		old := c.ID
		c.ID = typeMap[old]
		c.ProfileID = p.ID
		c.CreatedAt = now
		c.UpdatedAt = now
		var active *string
		if c.ActiveScenarioID != nil {
			mapped := scenarioMap[*c.ActiveScenarioID]
			active = &mapped
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO job_type_configs(id,profile_id,job_type,description,mode,active_scenario_id,max_active_jobs,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, c.ID, c.ProfileID, c.JobType, c.Description, c.Mode, active, c.MaxActiveJobs, c.CreatedAt, c.UpdatedAt)
		if err != nil {
			return p, err
		}
	}
	for _, v := range scenarios {
		v.ID = scenarioMap[v.ID]
		v.JobTypeConfigID = typeMap[v.JobTypeConfigID]
		v.CreatedAt = now
		v.UpdatedAt = now
		_, err = tx.ExecContext(ctx, `INSERT INTO scenarios(id,job_type_config_id,name,description,outcome,variables_json,delay_ms,error_code,error_message,remaining_retries,retry_backoff_ms,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, v.ID, v.JobTypeConfigID, v.Name, v.Description, v.Outcome, v.VariablesJSON, v.DelayMS, v.ErrorCode, v.ErrorMessage, v.RemainingRetries, v.RetryBackoffMS, v.CreatedAt, v.UpdatedAt)
		if err != nil {
			return p, err
		}
	}
	_ = oldProfile
	return p, tx.Commit()
}

func friendlyConstraint(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "UNIQUE constraint failed: connection_profiles.name") {
		return errors.New("a profile with this name already exists")
	}
	if strings.Contains(err.Error(), "UNIQUE constraint failed: job_type_configs.profile_id") {
		return errors.New("the job type already exists in this profile")
	}
	if strings.Contains(err.Error(), "UNIQUE constraint failed: scenarios.job_type_config_id") {
		return errors.New("a scenario with this name already exists")
	}
	return err
}
