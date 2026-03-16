# Test: duplicate HEALTHCHECK where last is NONE
# DL3057 should be suppressed (NONE is explicit opt-out)
# MultipleInstructionsDisallowed should fire (duplicate HEALTHCHECK)
FROM scratch
HEALTHCHECK CMD /bin/check
HEALTHCHECK NONE
