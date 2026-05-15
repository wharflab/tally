# Test case for DL3046: useradd without -l and high UID
FROM debian:bookworm-20260505@sha256:85019db29298555fd1a5f4bb57673ae989414a9884117c75d7a3e1a6cce21688

# This should trigger DL3046 - high UID without -l flag
RUN useradd -u 123456 appuser

# This is OK - has -l flag
RUN useradd -l -u 123456 otheruser

# This is OK - short UID (5 digits)
RUN useradd -u 99999 shortuid
