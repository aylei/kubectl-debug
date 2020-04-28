FROM alpine:3.11.5 as build

RUN apk add lxcfs containerd 

FROM alpine:3.11.5

COPY --from=build /usr/bin/lxcfs /usr/bin/lxcfs
COPY --from=build /usr/lib/*fuse* /usr/lib/
COPY --from=build /usr/bin/ctr /usr/bin/ctr

COPY ./scripts/start.sh /
RUN chmod 755 /start.sh
COPY ./debug-agent /bin/debug-agent

EXPOSE 10027

CMD ["/start.sh"]
