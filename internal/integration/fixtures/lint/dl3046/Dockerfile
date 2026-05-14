# Test case for DL3046: useradd without -l and high UID
FROM debian:bookworm

# This should trigger DL3046 - high UID without -l flag
RUN useradd -u 123456 appuser

# This is OK - has -l flag
RUN useradd -l -u 123456 otheruser

# This is OK - short UID (5 digits)
RUN useradd -u 99999 shortuid
