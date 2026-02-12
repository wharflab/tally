# Test: truly undefined var is still caught after async resolution.
# PYTHON_VERSION exists in the python:3.12 image env, so using it is fine.
# But $NONEXISTENT does not exist in any image env — async resolution
# should still flag it.
FROM python:3.12
LABEL version=$PYTHON_VERSION
COPY $NONEXISTENT /app/
