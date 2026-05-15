# Test case for DL3057 smart suppression: serverless base image
# AWS Lambda base image — HEALTHCHECK is not beneficial, rule should be suppressed
FROM public.ecr.aws/lambda/python:3.12@sha256:d21bf26c01559a643a77adb1947775f4eb19e3b08662634c4a3401a12d288cd4

COPY requirements.txt .
RUN pip install -r requirements.txt
COPY app.py /var/task/
CMD ["app.handler"]
