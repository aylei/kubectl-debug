FROM alpine:3.11.5

COPY ./scripts/start.sh /
RUN chmod 755 /start.sh
COPY ./debug-agent /bin/debug-agent
RUN apk add lxcfs

EXPOSE 10027

CMD ["/start.sh"]
