FROM scratch as build
RUN echo "building"

FROM node@sha256:f6f9fae352b319e817eec5efc7721b3f06bc53104f2f800d0f778f3915835804
# DL3022: Should trigger - forward reference to later stage
COPY --from=runtime foo .

# This is fine - references a previously defined alias
COPY --from=build bar .

FROM alpine@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11 as runtime
RUN echo "runtime"
