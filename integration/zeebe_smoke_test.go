//go:build integration

package integration

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/camunda/zeebe/clients/go/v8/pkg/zbc"

	"github.com/mishankov/camunda-stub-worker/internal/domain"
	"github.com/mishankov/camunda-stub-worker/internal/gateway"
)

func TestZeebe825Smoke(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	client, err := zbc.NewClient(&zbc.ClientConfig{GatewayAddress: "localhost:26500", UsePlaintextConnection: true})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	top, err := client.NewTopologyCommand().Send(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(top.Brokers) == 0 || !strings.HasPrefix(top.Brokers[0].Version, "8.5.25") {
		t.Fatalf("expected Zeebe 8.5.25, got %+v", top.Brokers)
	}
	deploy := client.NewDeployResourceCommand()
	for _, name := range []string{"success.bpmn", "business-error.bpmn", "technical-failure.bpmn"} {
		raw, e := os.ReadFile("bpmn/" + name)
		if e != nil {
			t.Fatal(e)
		}
		deploy.AddResource(raw, name)
	}
	if _, err = deploy.Send(ctx); err != nil {
		t.Fatal(err)
	}
	g, err := gateway.ZeebeFactory{}.Connect(domain.Profile{Host: "localhost", Port: 26500})
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	cases := []struct {
		process, jobType string
		respond          func(string) error
	}{
		{"stub_success_process", "stub-success", func(key string) error { return g.Complete(ctx, key, `{"answer":9007199254740993}`) }},
		{"stub_business_error_process", "stub-business-error", func(key string) error { return g.ThrowError(ctx, key, "BUSINESS_ERROR", "expected", `{"caught":true}`) }},
		{"stub_technical_failure_process", "stub-technical-failure", func(key string) error { return g.Fail(ctx, key, "expected failure", 0, 0, `{}`) }},
	}
	for _, tc := range cases {
		t.Run(tc.jobType, func(t *testing.T) {
			if _, e := client.NewCreateInstanceCommand().BPMNProcessId(tc.process).LatestVersion().Send(ctx); e != nil {
				t.Fatal(e)
			}
			jobs, e := g.Activate(ctx, tc.jobType, 1, 20*time.Second)
			if e != nil {
				t.Fatal(e)
			}
			if len(jobs) != 1 {
				t.Fatalf("activated %d jobs", len(jobs))
			}
			if e = g.Extend(ctx, jobs[0].Key, 20*time.Second); e != nil {
				t.Fatal(e)
			}
			if e = tc.respond(jobs[0].Key); e != nil {
				t.Fatal(e)
			}
		})
	}
}
