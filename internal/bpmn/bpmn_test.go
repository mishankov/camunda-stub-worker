package bpmn

import (
	"testing"
)

const modelXML = `<definitions xmlns="http://www.omg.org/spec/BPMN/20100524/MODEL" xmlns:z="http://camunda.org/schema/zeebe/1.0">
<process id="order" name="Order process"><serviceTask id="charge" name="Charge card"><extensionElements><z:taskDefinition type="payment"/></extensionElements></serviceTask>
<subProcess id="sub"><sendTask id="notify"><extensionElements><z:taskDefinition type="payment"/></extensionElements></sendTask></subProcess>
<serviceTask id="dynamic"><extensionElements><z:taskDefinition type="= workerType"/></extensionElements></serviceTask>
<userTask id="human"/><callActivity id="called"/>
</process><process id="other"><serviceTask id="excluded"><extensionElements><z:taskDefinition type="other"/></extensionElements></serviceTask></process></definitions>`

func TestParseProcessesAndTasks(t *testing.T) {
	processes, err := Parse([]byte(modelXML))
	if err != nil {
		t.Fatal(err)
	}
	if len(processes) != 2 || processes[0].BPMNProcessID != "order" || processes[0].Name != "Order process" || processes[1].BPMNProcessID != "other" {
		t.Fatalf("unexpected processes: %+v", processes)
	}
	if len(processes[0].Tasks) != 3 || processes[0].Tasks[0].Name != "Charge card" || processes[0].Tasks[0].JobType != "payment" || processes[0].Tasks[1].ID != "notify" || !processes[0].Tasks[2].Dynamic {
		t.Fatalf("unexpected tasks: %+v", processes[0].Tasks)
	}
	if len(processes[1].Tasks) != 1 || processes[1].Tasks[0].JobType != "other" {
		t.Fatalf("unexpected tasks in other process: %+v", processes[1].Tasks)
	}
}

func TestParseRejectsInvalidDefinitions(t *testing.T) {
	for _, raw := range []string{`<html/>`, `<definitions`, `<definitions xmlns="http://example.org"><process id="order"/></definitions>`} {
		if _, err := Parse([]byte(raw)); err == nil {
			t.Fatalf("accepted invalid definition %q", raw)
		}
	}
}

func TestTasksSelectsProcess(t *testing.T) {
	tasks, err := Tasks([]byte(modelXML), "order")
	if err != nil || len(tasks) != 3 {
		t.Fatalf("tasks=%+v error=%v", tasks, err)
	}
	if _, err := Tasks([]byte(modelXML), "missing"); err == nil {
		t.Fatal("accepted unknown process")
	}
	if _, err := Tasks([]byte(modelXML), "other"); err != nil {
		t.Fatalf("process without tasks failed: %v", err)
	}
}
