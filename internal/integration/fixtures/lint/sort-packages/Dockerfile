FROM ubuntu:22.04@sha256:962f6cadeae0ea6284001009daa4cc9a8c37e75d1f5191cf0eb83fe565b63dd7

# Unsorted apt-get packages (single-line)
RUN apt-get update && apt-get install -y wget curl git

# Unsorted multi-line apt-get packages
RUN apt-get install -y \
    zip \
    curl \
    git

# Already sorted - should not trigger
RUN apk add --no-cache curl git wget

# Unsorted npm packages with scoped package
RUN npm install express axios @eslint/js
