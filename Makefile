# FW-01 / MASTER-RESCUE-10: one entry point for the guard suite.
# Windows devs: run guards directly (`bash scripts/fw/check-x.sh`); make
# verify is the CI-facing contract (Ubuntu runner).

GUARDS := $(wildcard scripts/fw/check-*.sh)

.PHONY: verify selftest
verify: selftest
	@rc=0; for g in $(GUARDS); do \
		bash $$g || rc=1; \
	done; exit $$rc

selftest:
	@rc=0; for g in $(GUARDS); do \
		bash $$g --self-test || rc=1; \
	done; exit $$rc
