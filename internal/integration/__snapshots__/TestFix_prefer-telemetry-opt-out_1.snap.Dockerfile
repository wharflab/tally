FROM node:22
# [tally] settings to opt out from telemetry
ENV DO_NOT_TRACK=1 NEXT_TELEMETRY_DISABLED=1
RUN bun install && next build
