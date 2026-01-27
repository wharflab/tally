FROM alpine:3.18 AS builder
RUN echo "first builder"

FROM ubuntu:22.04 AS builder
RUN echo "second builder - should trigger DL3024"
