package configio

import (
	"strings"
	"testing"

	"github.com/mishankov/camunda-stub-worker/internal/domain"
)

func TestRoundTripPreservesLargeJSONNumber(t *testing.T) {
	p := domain.Profile{ID: "p", Name: "P", Host: "localhost", Port: 26500, SelectedVersion: "8.5", CommandTimeoutMS: 10000, ActivationTimeoutMS: 120000, RenewalIntervalMS: 30000, MaxActiveJobs: 10, HistoryRetentionDays: 30}
	typeID := "t"
	scenarioID := "s"
	types := []domain.JobTypeConfig{{ID: typeID, ProfileID: "p", JobType: "charge", Mode: domain.ModeAuto, ActiveScenarioID: &scenarioID, MaxActiveJobs: 5}}
	scenarios := []domain.Scenario{{ID: scenarioID, JobTypeConfigID: typeID, Name: "ok", Outcome: domain.OutcomeSuccess, VariablesJSON: `{"orderId":9007199254740993123456789}`}}
	raw, err := Encode(p, types, scenarios)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc.Scenarios[0].VariablesJSON, "9007199254740993123456789") {
		t.Fatalf("precision lost: %s", doc.Scenarios[0].VariablesJSON)
	}
}
