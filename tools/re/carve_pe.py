"""Carve a small PE from fifa15_unpacked.exe: all decrypted data (.arch, as data)
plus one code window, so Ghidra analyses it in under a minute (the full image is
too big for interactive use). Data directories are cleared.

    python3 tools/re/carve_pe.py fifa15_unpacked.exe 0x144700000 0x144a00000 net_carve.exe
The window 0x144700000-0x144a00000 holds BlazeSDK GameManager/ConnApiAdapter and
DirtySDK ConnApi/ProtoTunnel. Use 0x142e7e000 0x147106000 for all of .xtext.
"""
import struct, sys
d = open(sys.argv[1], "rb").read()
lo, hi = int(sys.argv[2], 16) - 0x140000000, int(sys.argv[3], 16) - 0x140000000
DLO, DHI = 0x1000, 0x1ddb000
pe = struct.unpack_from("<I", d, 0x3c)[0]; opt = pe + 24; optsz = struct.unpack_from("<H", d, pe + 20)[0]
hdr = bytearray(d[:0x1000])
struct.pack_into("<H", hdr, pe + 6, 2); struct.pack_into("<I", hdr, opt + 16, lo)
for i in range(16): struct.pack_into("<II", hdr, opt + 112 + 8 * i, 0, 0)
so = opt + optsz; hdr[so:so + 320] = b"\0" * 320
def sec(i, name, va, size, raw, ch):
    o = so + 40 * i; hdr[o:o + 8] = name.ljust(8, b"\0")
    struct.pack_into("<IIII", hdr, o + 8, size, va, size, raw); struct.pack_into("<I", hdr, o + 36, ch)
sec(0, b".data", DLO, DHI - DLO, 0x1000, 0x40000040)
sec(1, b".text", lo, hi - lo, 0x1000 + DHI - DLO, 0x60000020)
open(sys.argv[4], "wb").write(bytes(hdr) + d[DLO:DHI] + d[lo:hi])
