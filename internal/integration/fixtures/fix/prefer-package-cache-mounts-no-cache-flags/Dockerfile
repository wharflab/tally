FROM python:3.13@sha256:4f2d437a6b02de3c9f9aab4b90ba4bc50bd8ad825c5640c28a558c5639f6ded1
RUN pip install --no-cache-dir -r requirements.txt && pip cache purge
RUN uv sync --no-cache --frozen && uv cache clean
RUN bun install --no-cache && bun pm cache rm
