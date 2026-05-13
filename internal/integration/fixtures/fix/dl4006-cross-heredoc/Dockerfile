FROM ubuntu:22.04
RUN apt-get update && apt-get install -y curl && curl -fsSL https://example.com/setup.sh | bash
RUN wget -O - https://some.site | wc -l > /number
