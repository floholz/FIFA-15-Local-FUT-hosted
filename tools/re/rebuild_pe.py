"""Turn a memory image (dump_unpacked.py image.bin) back into a loadable PE whose
file layout equals its memory layout (FileAlignment = SectionAlignment).

    python3 tools/re/rebuild_pe.py image.bin original/fifa15.exe fifa15_unpacked.exe
"""
import struct, sys
img = bytearray(open(sys.argv[1], "rb").read()); orig = open(sys.argv[2], "rb").read()
pe = struct.unpack_from("<I", orig, 0x3c)[0]
hdr = bytearray(orig[:0x1000])
nsec = struct.unpack_from("<H", hdr, pe + 6)[0]; opt = struct.unpack_from("<H", hdr, pe + 20)[0]
struct.pack_into("<I", hdr, pe + 24 + 36, 0x1000)
for i in range(nsec):
    o = pe + 24 + opt + 40 * i
    vs, va = struct.unpack_from("<II", hdr, o + 8)
    size = min((vs + 0xfff) & ~0xfff, len(img) - va)
    struct.pack_into("<II", hdr, o + 16, size, va)
    if hdr[o:o + 5] == b".arch":
        struct.pack_into("<I", hdr, o + 36, 0xe0000020)
img[:0x1000] = hdr
open(sys.argv[3], "wb").write(img)
