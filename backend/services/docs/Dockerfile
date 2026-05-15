FROM nginx:alpine

COPY services/docs/nginx.conf /etc/nginx/conf.d/default.conf
COPY services/docs/index.html /usr/share/nginx/html/index.html
COPY contracts/openapi/gateway.openapi.yaml /usr/share/nginx/html/openapi.yaml

