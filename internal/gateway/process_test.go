package gateway

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/camunda/zeebe/clients/go/v8/pkg/pb"
	"github.com/mishankov/camunda-stub-worker/internal/domain"
	"google.golang.org/grpc"
)

type processServer struct {
	pb.UnimplementedGatewayServer
	requests       chan *pb.CreateProcessInstanceRequest
	deployRequests chan *pb.DeployResourceRequest
}

func (s *processServer) CreateProcessInstance(_ context.Context, r *pb.CreateProcessInstanceRequest) (*pb.CreateProcessInstanceResponse, error) {
	s.requests <- r
	return &pb.CreateProcessInstanceResponse{BpmnProcessId: r.BpmnProcessId, Version: 3, ProcessDefinitionKey: 9007199254740993, ProcessInstanceKey: 9007199254740995}, nil
}

func (s *processServer) DeployResource(_ context.Context, r *pb.DeployResourceRequest) (*pb.DeployResourceResponse, error) {
	s.deployRequests <- r
	return &pb.DeployResourceResponse{
		Key: 9007199254740997,
		Deployments: []*pb.Deployment{
			{Metadata: &pb.Deployment_Process{Process: &pb.ProcessMetadata{BpmnProcessId: "order", Version: 2, ProcessDefinitionKey: 9007199254740993}}},
		},
	}, nil
}
func TestStartProcessWireFormat(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	impl := &processServer{requests: make(chan *pb.CreateProcessInstanceRequest, 2)}
	pb.RegisterGatewayServer(server, impl)
	go server.Serve(listener)
	t.Cleanup(server.Stop)
	host, port, _ := net.SplitHostPort(listener.Addr().String())
	n, _ := strconv.Atoi(port)
	g, err := (ZeebeFactory{}).Connect(domain.Profile{Host: host, Port: n})
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, version := range []int32{0, 2} {
		raw := `{"large":9007199254740993123456789}`
		result, err := g.StartProcess(ctx, domain.StartProcessRequest{BPMNProcessID: "order", Version: version, VariablesJSON: raw})
		if err != nil {
			t.Fatal(err)
		}
		request := <-impl.requests
		expected := version
		if expected == 0 {
			expected = -1
		}
		if request.BpmnProcessId != "order" || request.Version != expected || request.Variables != raw {
			t.Fatalf("unexpected request: %+v", request)
		}
		if result.ProcessInstanceKey != "9007199254740995" || result.ProcessDefinitionKey != "9007199254740993" || result.Version != 3 {
			t.Fatalf("unexpected result: %+v", result)
		}
	}
}

func TestDeployWireFormat(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	impl := &processServer{deployRequests: make(chan *pb.DeployResourceRequest, 1)}
	pb.RegisterGatewayServer(server, impl)
	go server.Serve(listener)
	t.Cleanup(server.Stop)
	host, port, _ := net.SplitHostPort(listener.Addr().String())
	n, _ := strconv.Atoi(port)
	g, err := (ZeebeFactory{}).Connect(domain.Profile{Host: host, Port: n})
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	content := []byte("<definitions/>")
	result, err := g.Deploy(ctx, "order.bpmn", content)
	if err != nil {
		t.Fatal(err)
	}
	request := <-impl.deployRequests
	if len(request.Resources) != 1 || request.Resources[0].Name != "order.bpmn" || string(request.Resources[0].Content) != string(content) {
		t.Fatalf("unexpected request: %+v", request.Resources)
	}
	if result.DeploymentKey != "9007199254740997" || len(result.Processes) != 1 ||
		result.Processes[0].BPMNProcessID != "order" || result.Processes[0].Version != 2 || result.Processes[0].ProcessDefinitionKey != "9007199254740993" {
		t.Fatalf("unexpected result: %+v", result)
	}
}
