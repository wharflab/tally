FROM alpine:3.19@sha256:6baf43584bcb78f2e5847d1de515f23499913ac9f12bdf834811a3145eb11ca1 AS base

# DL3043: FROM is forbidden inside ONBUILD
ONBUILD FROM debian:bookworm

# tally/invalid-onbuild-trigger: COPPY is not a valid instruction
ONBUILD COPPY . /app
