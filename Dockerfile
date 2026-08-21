# FIFA 15 Local FUT - hosted server image.
# Runs only the Python backend (mode=server). Game files, DLLs and the Origin
# LSX stub stay on each player's PC; players connect with CONNECT_TO_SERVER.cmd.
FROM python:3.12-slim

WORKDIR /app
COPY payload/localfut15/ /app/localfut15/

# Persistent state (SQLite save, logs, hosted.json) lives in /data.
ENV FUT15_RUNTIME_ROOT=/data
VOLUME ["/data"]

# Redirector, Blaze, QoS, EASW, FUT, FUT-compat.
EXPOSE 42230 10051 17502 42232 8199 8099

# PUBLIC_HOST must be the address players can reach (VPN IP, public IP or DNS name).
ENV PUBLIC_HOST=""
CMD ["sh", "-c", "exec python /app/localfut15/server.py --mode server --host 0.0.0.0 ${PUBLIC_HOST:+--public-host \"$PUBLIC_HOST\"}"]
