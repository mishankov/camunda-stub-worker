package domain

import "testing"

func TestValidateJSONObject(t *testing.T) {
	valid := []string{`{}`, `{"large":9007199254740993123456789}`, `{"nested":{"v":1}}`}
	for _, v := range valid {
		if err := ValidateJSONObject(v); err != nil {
			t.Fatalf("%s: %v", v, err)
		}
	}
	invalid := []string{"", `[]`, `null`, `42`, `{"broken":}`, `{} trailing`, `{} {}`, `{}]`}
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

func TestValidateOperateURL(t *testing.T) {
	for _, raw := range []string{"", "http://localhost:8081", "https://host/operate/v1/"} {
		if err := ValidateOperateURL(raw); err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
	}
	for _, raw := range []string{"localhost:8081", "ftp://host", "https://user:secret@host", "https://host?token=secret", "https://host#fragment", " http://host"} {
		if err := ValidateOperateURL(raw); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}
