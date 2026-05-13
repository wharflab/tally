FROM alpine:3.19 AS base

# DL3043: FROM is forbidden inside ONBUILD
ONBUILD FROM debian:bookworm

# tally/invalid-onbuild-trigger: COPPY is not a valid instruction
ONBUILD COPPY . /app
