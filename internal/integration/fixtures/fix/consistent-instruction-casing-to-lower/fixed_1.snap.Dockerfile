from alpine:3.18
run echo hello
copy . /app
workdir /app
