# Cross-rule interaction: WorkdirRelativePath + DL3003 + DL3045
#
# All three rules fire because the Dockerfile avoids setting an absolute
# WORKDIR, using cd and relative paths instead.

FROM debian:bookworm

# DL3045: COPY to relative destination without WORKDIR set
COPY app.bin app.bin

# WorkdirRelativePath: relative WORKDIR without a preceding absolute one
WORKDIR src

# DL3003: cd in RUN instead of WORKDIR
RUN cd /opt && echo "building"
