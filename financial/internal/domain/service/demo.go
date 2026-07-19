package service

import (
	"os"
	"strings"
)

func demoDataEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("SEED_DEMO_DATA")), "true")
}
