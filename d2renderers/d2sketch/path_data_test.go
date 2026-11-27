package d2sketch

import "testing"

func TestCompactPathData(t *testing.T) {
	for input, want := range map[string]string{
		"M-0.0010001 -12.0000008 L1.2300009 10.0000004":             "M-.001-12L1.23 10",
		"M1.23456799 2 C-1.000002 4.0, -0.123456789 6.00009, 7 8.6": "M1.234567 2C-1.000002 4-.123456 6.00009 7 8.6",
		"M1.0040001e-8 -2.500000e-7":                                "M1.004e-8-2.5e-7",
	} {
		if got := compactPathData(input); got != want {
			t.Errorf("compactPathData(%q) = %q, want %q", input, got, want)
		}
	}
}
