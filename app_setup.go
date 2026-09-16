//go:build !bindings

package main

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/gofrs/flock"

	"github.com/mishankov/camunda-stub-worker/internal/gateway"
	workerruntime "github.com/mishankov/camunda-stub-worker/internal/runtime"
	"github.com/mishankov/camunda-stub-worker/internal/store"
)

func buildApp() (*App, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	dataDir := filepath.Join(base, "Camunda Stub Worker")
	if err = os.MkdirAll(dataDir, 0700); err != nil {
		return nil, err
	}
	instanceLock := flock.New(filepath.Join(dataDir, "instance.lock"))
	locked, err := instanceLock.TryLock()
	if err != nil {
		return nil, err
	}
	if !locked {
		return nil, errors.New("Camunda Stub Worker is already running for this user")
	}
	s, err := store.Open(filepath.Join(dataDir, "camunda-stub-worker.db"))
	if err != nil {
		_ = instanceLock.Unlock()
		return nil, err
	}
	m, err := workerruntime.NewManager(s, gateway.ZeebeFactory{}, nil)
	if err != nil {
		_ = s.Close()
		_ = instanceLock.Unlock()
		return nil, err
	}
	return newApp(s, m, instanceLock, dataDir), nil
}
