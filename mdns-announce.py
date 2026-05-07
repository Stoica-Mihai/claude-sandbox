import os
import signal
import socket
import sys
import threading

from zeroconf import ServiceInfo, Zeroconf

hostname = os.environ.get("MDNS_HOSTNAME", "claude")
try:
    port = int(os.environ.get("DASHBOARD_PORT", 8080))
except ValueError:
    print("DASHBOARD_PORT must be a number", flush=True)
    sys.exit(1)

_s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
try:
    _s.settimeout(5)
    _s.connect(("8.8.8.8", 80))
    ip = _s.getsockname()[0]
finally:
    _s.close()

info = ServiceInfo(
    "_http._tcp.local.",
    "Claude Sandbox._http._tcp.local.",
    addresses=[socket.inet_aton(ip)],
    port=port,
    server=f"{hostname}.local.",
)

zc = Zeroconf()
zc.register_service(info)
print(f"mDNS: http://{hostname}.local:{port} -> {ip}", flush=True)

_stop = threading.Event()


def _shutdown(sig, frame):
    _stop.set()


signal.signal(signal.SIGTERM, _shutdown)
signal.signal(signal.SIGINT, _shutdown)

_stop.wait()
zc.unregister_service(info)
zc.close()
