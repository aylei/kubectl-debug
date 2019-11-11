FROM ubuntu:xenial as build

RUN apt-get update && apt-get install libcgmanager-dev libnih-dbus-dev libnih-dev libfuse-dev automake libtool libpam-dev wget gcc automake -y && apt-get clean && rm -rf /var/lib/apt/lists/*

ENV LXCFS_VERSION 3.1.2
RUN wget https://linuxcontainers.org/downloads/lxcfs/lxcfs-$LXCFS_VERSION.tar.gz && \
	mkdir /lxcfs && tar xzvf lxcfs-$LXCFS_VERSION.tar.gz -C /lxcfs  --strip-components=1 && \
	cd /lxcfs && ./configure && make

FROM ubuntu:xenial

COPY --from=build /lxcfs/lxcfs /usr/local/bin/lxcfs
COPY --from=build /lxcfs/.libs/liblxcfs.so /usr/local/lib/lxcfs/liblxcfs.so
COPY --from=build /lxcfs/lxcfs /lxcfs/lxcfs
COPY --from=build /lxcfs/.libs/liblxcfs.so /lxcfs/liblxcfs.so
COPY ./scripts/start.sh /
COPY ./debug-agent /bin/debug-agent

EXPOSE 10027

CMD ["/start.sh"]
