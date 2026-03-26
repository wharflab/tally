FROM ubuntu:22.04
COPY <<EOF /usr/include/stub.h
#ifndef H
#define H
int f(void);
#endif
EOF
