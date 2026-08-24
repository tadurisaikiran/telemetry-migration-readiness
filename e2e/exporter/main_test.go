package main

import (
	"strings"
	"testing"
)

func TestRenderModes(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		mode     mode
		old      bool
		new      bool
		oldLabel bool
		newLabel bool
	}{
		{mode: modeOld, old: true, oldLabel: true},
		{mode: modeDual, old: true, new: true, oldLabel: true, newLabel: true},
		{mode: modeNew, new: true, newLabel: true},
	} {
		output := render(test.mode, 10)
		for needle, expected := range map[string]bool{
			"checkout_request_duration_seconds_count":        test.old,
			"checkout_server_request_duration_seconds_count": test.new,
			"http_method=\"GET\"":                            test.oldLabel,
			"http_request_method=\"GET\"":                    test.newLabel,
		} {
			if actual := strings.Contains(output, needle); actual != expected {
				t.Errorf("mode %s contains %q = %v, want %v", test.mode, needle, actual, expected)
			}
		}
	}
}

func TestParseModeRejectsUnknownValue(t *testing.T) {
	t.Parallel()
	if _, err := parseMode("unsafe"); err == nil {
		t.Fatal("parseMode accepted an unknown mode")
	}
}
