FROM alpine:3.4
RUN apk add --update --no-cache ca-certificates && rm /var/cache/apk/*

COPY ./debug-agent /bin/debug-agent
EXPOSE 10027

ENTRYPOINT ["/bin/debug-agent"]
