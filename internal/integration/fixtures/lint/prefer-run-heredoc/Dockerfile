# Test cases for prefer-run-heredoc rule
FROM ubuntu:22.04

# Case 1: Should trigger (3 consecutive regular RUNs)
RUN apt-get update
RUN apt-get upgrade -y
RUN apt-get install -y vim

WORKDIR /app

# Case 2: Should trigger (3 chained commands)
RUN echo step1 && echo step2 && echo step3

WORKDIR /test

# Case 3: Should NOT trigger (only 2 consecutive RUNs)
RUN echo hello
RUN echo world

WORKDIR /tmp

# Case 4: Should NOT trigger (only 2 commands in chain)
RUN apt-get update && apt-get install -y nginx

WORKDIR /srv

# Case 5: Should NOT trigger (single heredoc with 2 commands)
RUN <<EOF
echo hello
echo world
EOF

WORKDIR /home

# Case 6: Exec form breaks sequence - no violation for 2 RUNs before/after
RUN npm install
RUN ["./build.sh"]
RUN npm run test
