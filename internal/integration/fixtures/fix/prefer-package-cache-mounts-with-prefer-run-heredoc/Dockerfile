FROM node:20
RUN npm install && npm cache clean --force
RUN npm ci && npm cache clean --force
RUN npm install left-pad && npm cache clean --force
