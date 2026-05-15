FROM alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d AS Scratch
RUN echo "triggers both StageNameCasing and ReservedStageName"

FROM alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d AS Context
RUN echo "same cross-rule interaction"

FROM alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d AS Builder
RUN echo "triggers only StageNameCasing (not reserved)"
