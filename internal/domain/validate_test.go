package domain

import "testing"

func TestValidateJSONObject(t *testing.T) {
	valid := []string{`{}`, `{"large":9007199254740993123456789}`, `{"nested":{"v":1}}`}
	for _, v := range valid {
		if err := ValidateJSONObject(v); err != nil {
			t.Fatalf("%s: %v", v, err)
		}
	}
	invalid := []string{"", `[]`, `null`, `42`, `{"broken":}`}
	for _, v := range invalid {
		if err := ValidateJSONObject(v); err == nil {
			t.Fatalf("expected %s to fail", v)
		}
	}
}

func TestScenarioRules(t *testing.T) {
	s := Scenario{Name: "error", Outcome: OutcomeBusinessError, VariablesJSON: `{}`}
	if err := ValidateScenario(s); err == nil {
		t.Fatal("business error without code must fail")
	}
	s.ErrorCode = "PAYMENT_DECLINED"
	if err := ValidateScenario(s); err != nil {
		t.Fatal(err)
	}
}
