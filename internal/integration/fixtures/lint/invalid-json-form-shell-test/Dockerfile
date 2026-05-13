FROM public.ecr.aws/lambda/python:3.12

ARG DEPENDENCIES
RUN --mount=type=cache,target=/root/.cache/pip [ -z "$DEPENDENCIES" ] || { pip install --upgrade pip && pip install $(base64 -d <<< $DEPENDENCIES); }

COPY ./ ./
