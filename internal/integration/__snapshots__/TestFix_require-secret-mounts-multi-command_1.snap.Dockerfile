FROM node:20
RUN --mount=type=secret,id=npmrc,target=/root/.npmrc --mount=type=secret,id=pipconf,target=/root/.config/pip/pip.conf --mount=type=cache,target=/root/.npm,id=npm --mount=type=cache,target=/root/.cache/pip,id=pip npm install && pip install pandas
