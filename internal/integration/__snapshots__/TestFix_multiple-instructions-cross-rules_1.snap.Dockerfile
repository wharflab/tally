FROM alpine:3.21
# [commented out by tally - Docker will ignore all but last CMD]: cmd echo first
CMD ["echo","second"]
ENTRYPOINT ["/bin/sh"]
