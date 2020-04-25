FROM ubuntu:xenial

COPY ./scripts/start.sh /
RUN chmod 755 /start.sh
COPY ./debug-agent /bin/debug-agent
RUN apt-get update && apt-get install lxcfs -y && apt-get clean && rm -rf /var/lib/apt/lists/*

EXPOSE 10027

CMD ["/start.sh"]
