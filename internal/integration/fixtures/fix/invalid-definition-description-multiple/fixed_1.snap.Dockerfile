# check=experimental=InvalidDefinitionDescription
# bar this is the bar

ARG foo=bar
# BasE this is the BasE image

FROM scratch AS base
# definitely a bad comment

ARG version=latest
# definitely a bad comment

ARG baz=quux
