FROM python:3.12
RUN uv pip install aws-otel otel polars==1.2.3 $CDK_DEPS $RUNTIME_DEPS
