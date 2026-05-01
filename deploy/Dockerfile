FROM alpine:3.19
RUN apk add --no-cache ca-certificates curl

RUN curl -fsSL https://raw.githubusercontent.com/jhgundersen/magazine-builder/master/install.sh | \
    PREFIX=/usr/local sh

WORKDIR /app
VOLUME ["/app/magazine-work"]
EXPOSE 8080

ENTRYPOINT ["magazine-builder", "-workdir", "/app/magazine-work"]
