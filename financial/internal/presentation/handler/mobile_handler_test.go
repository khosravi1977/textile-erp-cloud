package handler

import "testing"

func TestMobileAccountingDateConvertsJalali(t *testing.T) {
	tests := map[string]string{
		"1405/01/01": "2026-03-21",
		"۱۴۰۵/۰۵/۰۱": "2026-07-23",
		"2026-07-22": "2026-07-22",
	}
	for input, expected := range tests {
		if actual := mobileAccountingDate(input); actual != expected {
			t.Errorf("mobileAccountingDate(%q) = %q, want %q", input, actual, expected)
		}
	}
}
