package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCoreDeployContinuesWhenOptionalTelegramRelayIsBlocked(t *testing.T) {
	t.Parallel()

	workflow, err := os.ReadFile(filepath.Join("..", ".github", "workflows", "deploy-vps.yml"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(workflow)
	for _, expected := range []string{
		"id: telegram_relay_vps",
		"continue-on-error: true",
		"if: steps.telegram_relay_vps.outcome == 'failure'",
		"TEXTILE_TELEGRAM_ENABLED=false",
		"umask 077",
		"mktemp \"${remote_file}.disabled.XXXXXX\"",
		"sudo /usr/local/sbin/viora-deploy textile-erp",
	} {
		if !strings.Contains(source, expected) {
			t.Fatalf("deployment workflow is missing %q", expected)
		}
	}
}
