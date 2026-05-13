# Test: BuildKit heredoc with #!/bin/bash shebang should be linted as bash, not sh.
# Docker BuildKit respects the shebang inside a heredoc body and runs it with
# the specified interpreter. Tally must detect this and pass the correct dialect
# to ShellCheck; otherwise valid bash syntax gets flagged as POSIX violations.
#
# Validated: this Dockerfile builds successfully with `docker build --no-cache`
# and the heredoc step prints "one\ntwo\nthree", confirming Docker uses bash.
FROM alpine:3.20

RUN apk add --no-cache bash

# This heredoc uses #!/bin/bash — Docker will run it with bash.
# The array syntax below is valid bash but invalid POSIX sh.
# ShellCheck must NOT emit SC3030 ("arrays are not supported in sh").
RUN <<EOF
#!/bin/bash
set -e
arr=(one two three)
for item in "${arr[@]}"; do
  echo "$item"
done
EOF
