FROM debian:12-slim@sha256:67b30a61dc87758f0caf819646104f29ecbda97d920aaf5edc834128ac8493d3
RUN apt-get install -y php8.3-cli php8.3-fpm php8.3-opcache
CMD ["php-fpm8.3", "-F"]
