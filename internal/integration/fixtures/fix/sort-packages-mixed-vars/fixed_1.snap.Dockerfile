FROM python:3.12@sha256:ed942629d18ad03521f9835ff95f3edbfbe99ccd38be6ba64a509ce3c1b149a8
RUN uv pip install aws-otel otel polars==1.2.3 $CDK_DEPS $RUNTIME_DEPS
