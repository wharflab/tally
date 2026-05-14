# Test: undefined var false positive elimination via async check.
# PYTHON_VERSION is set in the real python image env, so $PYTHON_VERSION should
# NOT be flagged as undefined when slow checks resolve the actual image env.
# (Without async, the fast path only knows about PATH, so it would flag this.)
FROM python:3.12
LABEL version=$PYTHON_VERSION
