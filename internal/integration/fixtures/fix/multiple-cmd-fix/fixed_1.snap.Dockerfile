FROM alpine:3.21
# [commented out by tally - Docker will ignore all but last CMD]: CMD echo "first"
RUN echo hello
CMD echo "second"
