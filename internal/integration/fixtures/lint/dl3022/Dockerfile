FROM scratch as build
RUN echo "building"

FROM node
# DL3022: Should trigger - forward reference to later stage
COPY --from=runtime foo .

# This is fine - references a previously defined alias
COPY --from=build bar .

FROM alpine as runtime
RUN echo "runtime"
