FROM ubuntu:22.04

# Heredoc candidate with pipe: DL4006 should detect but suppress fix
# since prefer-run-heredoc would convert this to a heredoc
RUN apt-get update && \
    apt-get install -y curl && \
    curl -fsSL https://example.com/setup.sh | bash

# Simple pipe command: DL4006 should detect and offer SHELL fix
RUN wget -O - https://some.site | wc -l > /number
