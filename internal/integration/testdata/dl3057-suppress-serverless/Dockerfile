# Test case for DL3057 smart suppression: serverless base image
# AWS Lambda base image — HEALTHCHECK is not beneficial, rule should be suppressed
FROM public.ecr.aws/lambda/python:3.12

COPY requirements.txt .
RUN pip install -r requirements.txt
COPY app.py /var/task/
CMD ["app.handler"]
