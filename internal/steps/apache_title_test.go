package steps

import "testing"

func TestApacheModulesTitleNoDisable(t *testing.T) {
	got := (&ApacheModules{Enable: []string{"ssl"}}).Info().Title.Vi
	if got != "Module Apache: bật ssl" {
		t.Errorf("Title = %q", got)
	}
	got = (&ApacheModules{Enable: []string{"mpm_event"}, Disable: []string{"mpm_prefork"}}).Info().Title.Vi
	if got != "Module Apache: bật mpm_event; tắt mpm_prefork" {
		t.Errorf("Title = %q", got)
	}
}
