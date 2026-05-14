FROM alpine:3.21 AS Scratch
RUN echo "triggers both StageNameCasing and ReservedStageName"

FROM alpine:3.21 AS Context
RUN echo "same cross-rule interaction"

FROM alpine:3.21 AS Builder
RUN echo "triggers only StageNameCasing (not reserved)"
