# Multi-stage: build the React app with Node, serve static files with nginx.
FROM node:24-alpine AS build
WORKDIR /app
COPY package.json package-lock.json ./
# Exact versions from the lockfile; no install scripts from dependencies.
RUN npm ci --no-audit --no-fund --ignore-scripts
COPY . .
RUN npm run build

# Unprivileged nginx: runs as user nginx (uid 101) and listens on 8080, so
# the container needs no root and no Linux capabilities.
FROM nginxinc/nginx-unprivileged:1.29-alpine
COPY nginx.conf /etc/nginx/templates/default.conf.template
COPY nginx/ /etc/nginx/snippets/
COPY --from=build /app/dist /usr/share/nginx/html
# MSAPI_UPSTREAM / MAPSERVER_UPSTREAM / NGINX_REAL_IP_FROM are substituted
# into the template at start.
ENV MSAPI_UPSTREAM=http://msapi:8081 \
    MAPSERVER_UPSTREAM=http://mapserver:80 \
    NGINX_REAL_IP_FROM=127.0.0.1/32 \
    NGINX_ENVSUBST_FILTER=^(MSAPI_UPSTREAM|MAPSERVER_UPSTREAM|NGINX_REAL_IP_FROM)$
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1:8080/ || exit 1
