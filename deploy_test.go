package main

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/camunda/zeebe/clients/go/v8/pkg/pb"
	"google.golang.org/grpc"

	"github.com/mishankov/camunda-stub-worker/internal/domain"
	"github.com/mishankov/camunda-stub-worker/internal/store"
)

const deployTestModel = `<definitions xmlns="http://www.omg.org/spec/BPMN/20100524/MODEL" xmlns:z="http://camunda.org/schema/zeebe/1.0">
<process id="order" name="Order process"><serviceTask id="charge" name="Charge card"><extensionElements><z:taskDefinition type="payment"/></extensionElements></serviceTask></process>
</definitions>`

type deployTestServer struct {
	pb.UnimplementedGatewayServer
	requests chan *pb.DeployResourceRequest
}

func (s *deployTestServer) DeployResource(_ context.Context, r *pb.DeployResourceRequest) (*pb.DeployResourceResponse, error) {
	s.requests <- r
	return &pb.DeployResourceResponse{
		Key: 9007199254740997,
		Deployments: []*pb.Deployment{
			{Metadata: &pb.Deployment_Process{Process: &pb.ProcessMetadata{BpmnProcessId: "order", Version: 1, ProcessDefinitionKey: 9007199254740993}}},
		},
	}, nil
}

func startDeployTestGateway(t *testing.T) (*deployTestServer, string, int) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	impl := &deployTestServer{requests: make(chan *pb.DeployResourceRequest, 1)}
	pb.RegisterGatewayServer(server, impl)
	go server.Serve(listener)
	t.Cleanup(server.Stop)
	host, port, _ := net.SplitHostPort(listener.Addr().String())
	n, _ := strconv.Atoi(port)
	return impl, host, n
}

func profileForTest(t *testing.T, host string, port int) domain.Profile {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	p := domain.Profile{Name: "test", Host: host, Port: port, SelectedVersion: "8.5", CommandTimeoutMS: 5000, ActivationTimeoutMS: 120000, RenewalIntervalMS: 30000, MaxActiveJobs: 10, HistoryRetentionDays: 30}
	if err = s.SaveProfile(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	return p
}

func writeModel(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDeployBPMNFile(t *testing.T) {
	impl, host, port := startDeployTestGateway(t)
	p := profileForTest(t, host, port)
	path := writeModel(t, "order.bpmn", deployTestModel)
	result, err := deployBPMNFile(context.Background(), p, path)
	if err != nil {
		t.Fatal(err)
	}
	request := <-impl.requests
	if len(request.Resources) != 1 || request.Resources[0].Name != "order.bpmn" || !strings.Contains(string(request.Resources[0].Content), "order") {
		t.Fatalf("unexpected request: %+v", request.Resources)
	}
	if result.FileName != "order.bpmn" || result.Deployment != "9007199254740997" {
		t.Fatalf("unexpected response: %+v", result)
	}
	if len(result.Processes) != 1 {
		t.Fatalf("expected one process, got %+v", result.Processes)
	}
	deployed := result.Processes[0]
	if deployed.BPMNProcessID != "order" || deployed.Name != "Order process" || deployed.Version != 1 || deployed.ProcessDefinitionKey != "9007199254740993" {
		t.Fatalf("unexpected deployed process: %+v", deployed)
	}
	if len(deployed.Tasks) != 1 || deployed.Tasks[0].ID != "charge" || deployed.Tasks[0].JobType != "payment" {
		t.Fatalf("unexpected tasks: %+v", deployed.Tasks)
	}
}

func TestDeployBPMNFileRejectsInvalidModels(t *testing.T) {
	_, host, port := startDeployTestGateway(t)
	p := profileForTest(t, host, port)
	for _, tc := range []struct{ name, content string }{
		{"not-bpmn.bpmn", "<html/>"},
		{"empty.bpmn", ""},
		{"no-processes.bpmn", `<definitions xmlns="http://www.omg.org/spec/BPMN/20100524/MODEL"></definitions>`},
	} {
		path := writeModel(t, tc.name, tc.content)
		if _, err := deployBPMNFile(context.Background(), p, path); err == nil {
			t.Fatalf("accepted invalid model %q", tc.name)
		}
	}
}

func TestDeployBPMNFileMissingFile(t *testing.T) {
	_, host, port := startDeployTestGateway(t)
	p := profileForTest(t, host, port)
	if _, err := deployBPMNFile(context.Background(), p, filepath.Join(t.TempDir(), "missing.bpmn")); err == nil {
		t.Fatal("expected an error for a missing file")
	}
}
