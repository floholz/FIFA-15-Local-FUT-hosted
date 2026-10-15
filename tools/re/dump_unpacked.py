"""Dump fifa15.exe's decrypted image from a running game (Linux/Proton).

fifa15.exe ships with its data section (.arch, strings/vtables) absent on disk and
about half of its code section (.xtext) encrypted; both are decrypted at startup.
This launches the game through tools/linux/play.sh as a CHILD process (so
kernel.yama.ptrace_scope=1 still lets us read /proc/<pid>/mem), waits, and writes
the 0x140000000.. image to <out>/image.bin plus <out>/maps.txt.

    python3 tools/re/dump_unpacked.py OUT_DIR [WAIT_SECONDS] [-- play.sh args]

The game keeps running afterwards; quit it normally.
"""
import os, subprocess, sys, time

REPO = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
out = sys.argv[1]; wait = int(sys.argv[2]) if len(sys.argv) > 2 and sys.argv[2] != "--" else 75
extra = sys.argv[sys.argv.index("--") + 1:] if "--" in sys.argv else ["--no-server"]
os.makedirs(out, exist_ok=True)
subprocess.Popen([os.path.join(REPO, "tools/linux/play.sh"), *extra],
                 stdout=open(os.path.join(out, "play.log"), "w"), stderr=subprocess.STDOUT)

def find_game():
    for pid in filter(str.isdigit, os.listdir("/proc")):
        try:
            cmd = open(f"/proc/{pid}/cmdline", "rb").read().lower()
            if b"fifa15.exe" in cmd and b"umu" not in cmd and "fifa15.exe" in open(f"/proc/{pid}/maps").read().lower():
                return int(pid)
        except OSError:
            pass

pid, t0 = None, time.time()
while not pid and time.time() - t0 < 180:
    time.sleep(2); pid = find_game()
if not pid:
    sys.exit("game process not found")
print("game pid", pid, "- waiting", wait, "s for decryption", flush=True)
time.sleep(wait)
maps = open(f"/proc/{pid}/maps").read()
open(os.path.join(out, "maps.txt"), "w").write(maps)
mem = open(f"/proc/{pid}/mem", "rb", 0)
with open(os.path.join(out, "image.bin"), "wb") as f:
    for line in maps.splitlines():
        a, b = (int(x, 16) for x in line.split()[0].split("-"))
        if 0x140000000 <= a < 0x147200000:
            mem.seek(a)
            try: data = mem.read(b - a)
            except OSError: data = b"\0" * (b - a)
            f.seek(a - 0x140000000); f.write(data)
print("wrote", os.path.join(out, "image.bin"))
