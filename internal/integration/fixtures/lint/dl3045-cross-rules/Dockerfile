# Cross-rule interaction: WorkdirRelativePath + DL3003 + DL3045
#
# All three rules fire because the Dockerfile avoids setting an absolute
# WORKDIR, using cd and relative paths instead.

FROM debian:bookworm-20260505@sha256:85019db29298555fd1a5f4bb57673ae989414a9884117c75d7a3e1a6cce21688

# DL3045: COPY to relative destination without WORKDIR set
COPY app.bin app.bin

# WorkdirRelativePath: relative WORKDIR without a preceding absolute one
WORKDIR src

# DL3003: cd in RUN instead of WORKDIR
RUN cd /opt && echo "building"
