package operate

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const taskXML = `<definitions xmlns="http://www.omg.org/spec/BPMN/20100524/MODEL" xmlns:z="http://camunda.org/schema/zeebe/1.0">
<process id="order"><serviceTask id="charge" name="Charge card"><extensionElements><z:taskDefinition type="payment"/></extensionElements></serviceTask>
<subProcess id="sub"><sendTask id="notify"><extensionElements><z:taskDefinition type="payment"/></extensionElements></sendTask></subProcess>
<serviceTask id="dynamic"><extensionElements><z:taskDefinition type="= workerType"/></extensionElements></serviceTask>
<userTask id="human"/><callActivity id="called"/>
</process><process id="other"><serviceTask id="excluded"><extensionElements><z:taskDefinition type="other"/></extensionElements></serviceTask></process></definitions>`

func TestParseTasks(t *testing.T) {
	tasks, err := parseTasks([]byte(taskXML), "order")
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 3 || tasks[0].Name != "Charge card" || tasks[0].JobType != "payment" || tasks[1].ID != "notify" || !tasks[2].Dynamic {
		t.Fatalf("unexpected tasks: %+v", tasks)
	}
	for _, tc := range []struct{ raw, id string }{{taskXML, "missing"}, {"<html/>", "order"}, {"<definitions", "order"}} {
		if _, err := parseTasks([]byte(tc.raw), tc.id); err == nil {
			t.Fatalf("accepted invalid definition %q", tc)
		}
	}
}

func TestGetProcessTasks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/operate/v1/process-definitions/9007199254740993/xml" || r.Method != "GET" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		fmt.Fprint(w, taskXML)
	}))
	defer server.Close()
	tasks, err := GetProcessTasks(context.Background(), server.URL+"/operate/v1/", Auth{Mode: "token", Token: "secret"}, "9007199254740993", "order")
	if err != nil || len(tasks) != 3 {
		t.Fatalf("tasks=%+v error=%v", tasks, err)
	}
	if _, err := GetProcessTasks(context.Background(), server.URL, Auth{}, "../bad", "order"); err == nil {
		t.Fatal("accepted invalid key")
	}
}

func TestProcessVersionsPreserveKeys(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"items":[{"key":9007199254740993,"bpmnProcessId":"order","version":1},{"key":9007199254740995,"bpmnProcessId":"order","version":2}],"total":2}`)
	}))
	defer server.Close()
	result, err := ListProcesses(context.Background(), server.URL, Auth{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || len(result[0].Versions) != 2 || result[0].Versions[0].Key != "9007199254740995" || result[0].Versions[1].Key != "9007199254740993" {
		t.Fatalf("unexpected versions: %+v", result)
	}
}

func TestGetProcessTasksPasswordAndErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/login" {
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "valid", Path: "/"})
			return
		}
		if _, err := r.Cookie("session"); err != nil {
			t.Error("missing login session")
		}
		w.WriteHeader(403)
		fmt.Fprint(w, "sensitive error body")
	}))
	defer server.Close()
	_, err := GetProcessTasks(context.Background(), server.URL, Auth{Mode: "password", Username: "demo", Password: "demo"}, "1", "order")
	if err == nil || !strings.Contains(err.Error(), "403") || strings.Contains(err.Error(), "sensitive") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Operate deployments can expose the raw XML as text/xml or text/plain.
// Model content negotiation so a restrictive Accept header reproduces HTTP 406.
func TestGetProcessTasksAcceptsXMLMediaTypes(t *testing.T) {
	for _, contentType := range []string{"text/xml", "application/xml", "text/plain"} {
		t.Run(contentType, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				accept := r.Header.Get("Accept")
				if accept != "" && accept != "*/*" && accept != contentType {
					w.WriteHeader(http.StatusNotAcceptable)
					return
				}
				w.Header().Set("Content-Type", contentType)
				fmt.Fprint(w, taskXML)
			}))
			defer server.Close()
			tasks, err := GetProcessTasks(context.Background(), server.URL, Auth{}, "1", "order")
			if err != nil || len(tasks) != 3 {
				t.Fatalf("tasks=%+v error=%v", tasks, err)
			}
		})
	}
}
