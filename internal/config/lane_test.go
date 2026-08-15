package config

import "testing"

func TestNormalizeLane(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"  ", ""},
		{"none", ""},
		{"unset", ""},
		{"-", ""},
		{"cn", DeviceLaneCN},
		{" CN ", DeviceLaneCN},
		{"domestic", DeviceLaneCN},
		{"china", DeviceLaneCN},
		{"intl", DeviceLaneIntl},
		{"INTL", DeviceLaneIntl},
		{"international", DeviceLaneIntl},
		{"global", DeviceLaneIntl},
		{"overseas", DeviceLaneIntl},
		{"eu", "eu"},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := NormalizeLane(tt.in); got != tt.want {
				t.Fatalf("NormalizeLane(%q)=%q want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestValidateLane(t *testing.T) {
	for _, ok := range []string{"", "cn", "intl", " CN ", "international"} {
		if err := ValidateLane(ok); err != nil {
			t.Fatalf("ValidateLane(%q) returned error: %v", ok, err)
		}
	}
	if err := ValidateLane("eu"); err == nil {
		t.Fatal("ValidateLane(eu) returned nil error")
	}
	if err := ValidateLane("us"); err == nil {
		t.Fatal("ValidateLane(us) returned nil error")
	}
}

func TestLaneLabel(t *testing.T) {
	if got := LaneLabel("cn"); got != "国内" {
		t.Fatalf("LaneLabel(cn)=%q", got)
	}
	if got := LaneLabel("intl"); got != "国外" {
		t.Fatalf("LaneLabel(intl)=%q", got)
	}
	if got := LaneLabel(""); got != "" {
		t.Fatalf("LaneLabel(empty)=%q", got)
	}
}
