FROM alpine:3.21
# [commented out by tally - Docker will ignore all but last ENTRYPOINT]: ENTRYPOINT ["/bin/bash"]
ENTRYPOINT ["/bin/sh"]
