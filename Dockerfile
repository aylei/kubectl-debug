FROM gcr.io/distroless/static

COPY ./debug-agent /bin/debug-agent
EXPOSE 10027

ENTRYPOINT ["/bin/debug-agent"]
