package market

import "testing"

func TestExactDecimalAndDelayValidation(t *testing.T) {
	for _, raw := range []string{"0", "-1.123456789012345678", "1e-8", "1.25"} {
		value, err := ParseDecimal(raw)
		if err != nil || string(value) != raw {
			t.Fatal(raw, value, err)
		}
	}
	for _, raw := range []string{"", "NaN", "Inf", "1/2", "1e1000000000", "1..2"} {
		if _, err := ParseDecimal(raw); err == nil {
			t.Fatal("invalid decimal", raw)
		}
	}
	if Decimal("-1").Nonnegative() {
		t.Fatal("negative admitted")
	}
	for _, delay := range []Delay{{}, {Mode: "delayed"}, {Mode: "realtime"}} {
		if delay.Validate() == nil {
			t.Fatal("unknown delay silently treated as realtime", delay)
		}
	}
}
