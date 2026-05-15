FROM public.ecr.aws/lambda/python:3.12@sha256:d21bf26c01559a643a77adb1947775f4eb19e3b08662634c4a3401a12d288cd4

ARG DEPENDENCIES
RUN --mount=type=cache,target=/root/.cache/pip [ -z "$DEPENDENCIES" ] || { pip install --upgrade pip && pip install $(base64 -d <<< $DEPENDENCIES); }

COPY ./ ./
