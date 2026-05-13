FROM python:3.13
RUN pip install --no-cache-dir -r requirements.txt && pip cache purge
RUN uv sync --no-cache --frozen && uv cache clean
RUN bun install --no-cache && bun pm cache rm
