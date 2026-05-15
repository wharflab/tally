# Test: undefined var false positive elimination via async check.
# PYTHON_VERSION is set in the real python image env, so $PYTHON_VERSION should
# NOT be flagged as undefined when slow checks resolve the actual image env.
# (Without async, the fast path only knows about PATH, so it would flag this.)
FROM python:3.12@sha256:ed942629d18ad03521f9835ff95f3edbfbe99ccd38be6ba64a509ce3c1b149a8
LABEL version=$PYTHON_VERSION
