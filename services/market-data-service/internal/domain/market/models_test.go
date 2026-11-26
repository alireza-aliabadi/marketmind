package market

import "testing"

func TestParseCIK(t *testing.T) {
	for _, input := range []string{"1045810", "0001045810", " 1045810 "} {
		got, err := ParseCIK(input)
		if err != nil || got != "0001045810" {
			t.Fatalf("%q: %q, %v", input, got, err)
		}
	}
	for _, input := range []string{"", "0", "0000000000", "12345678901", "-1", "12.3", "１２３", "1 2"} {
		if _, err := ParseCIK(input); err == nil {
			t.Errorf("accepted invalid CIK %q", input)
		}
	}
}
