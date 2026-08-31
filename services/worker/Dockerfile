FROM python:3.12-slim
RUN useradd --create-home --uid 10001 app
WORKDIR /app
COPY worker /app/worker
USER app
ENV PYTHONUNBUFFERED=1 \
    PYTHONPATH=/app \
    WORKER_ADDR=0.0.0.0 \
    WORKER_PORT=8090
EXPOSE 8090
CMD ["python", "-m", "worker.app"]

