import struct, sys, re
data = open("/home/floholz/Games/EA_OSS/FIFA_15/fifa15.exe", "rb").read()
e_lfanew = struct.unpack_from("<I", data, 0x3C)[0]
nsec = struct.unpack_from("<H", data, e_lfanew + 6)[0]
opt_off = e_lfanew + 24
magic = struct.unpack_from("<H", data, opt_off)[0]
image_base = struct.unpack_from("<Q", data, opt_off + 24)[0] if magic == 0x20B else struct.unpack_from("<I", data, opt_off + 28)[0]
opt_size = struct.unpack_from("<H", data, e_lfanew + 20)[0]
secs = []
for i in range(nsec):
    o = opt_off + opt_size + i * 40
    name = data[o:o+8].rstrip(b"\0").decode(errors="replace")
    vsize, va, rsize, raw = struct.unpack_from("<IIII", data, o + 8)
    secs.append((name, va, vsize, raw, rsize))
def off2va(off):
    for n, va, vs, raw, rs in secs:
        if raw <= off < raw + rs: return image_base + va + (off - raw)
def va2off(va):
    rva = va - image_base
    for n, va_, vs, raw, rs in secs:
        if va_ <= rva < va_ + rs: return raw + (rva - va_)
def dectag(v):
    t = v >> 8
    s = "".join(chr(((t >> (18 - 6*i)) & 0x3F) + 0x20) for i in range(4))
    return s
def cstr(off):
    e = data.index(b"\0", off); return data[off:e].decode(errors="replace")
def find_str(s):
    b = s.encode() + b"\0"
    out = []; i = -1
    while True:
        i = data.find(b"\0" + b, i + 1)
        if i < 0: break
        out.append(i + 1)
    return out

def entries_for(name):
    res = []
    for off in find_str(name):
        va = off2va(off)
        if va is None: continue
        pat = struct.pack("<Q", va)
        j = -1
        while True:
            j = data.find(pat, j + 1)
            if j < 0: break
            res.append(j)
    return res

# Inspect the context around each pointer to the member name to learn the entry layout.
for nm in sys.argv[1:]:
    for j in entries_for(nm):
        ctx = data[j-32:j+40]
        words = [struct.unpack_from("<I", ctx, k)[0] for k in range(0, len(ctx)-3, 4)]
        tags = [(k*4-32, dectag(w)) for k, w in enumerate(words) if (w & 0xFF) == 0 and 0x80000000 <= w <= 0xFFFFFF00 and re.fullmatch(r"[A-Z0-9 ]{4}", dectag(w))]
        print(f"{nm}: ptr@0x{j:x}  tags-nearby={tags}")

def entry_ok(j):
    if j - 8 < 0 or j + 8 > len(data): return None
    tag = struct.unpack_from("<I", data, j - 8)[0]
    if (tag & 0xFF) != 0 or not re.fullmatch(r"[A-Z0-9 ]{4}", dectag(tag)): return None
    ptr = struct.unpack_from("<Q", data, j)[0]
    off = va2off(ptr)
    if off is None: return None
    try: s = cstr(off)
    except ValueError: return None
    if not re.fullmatch(r"m[A-Za-z0-9_]{1,60}", s): return None
    return dectag(tag), s

def dump_table(j):
    start = j
    while entry_ok(start - 32): start -= 32
    rows = []; k = start
    while True:
        e = entry_ok(k)
        if not e: break
        rows.append(e); k += 32
    return start, rows

if __name__ == "__main__" and "--tables" in sys.argv:
    for p in [int(x, 16) for x in sys.argv[sys.argv.index("--tables")+1:]]:
        start, rows = dump_table(p)
        print(f"== table @0x{start:x} ({len(rows)} members)")
        for t, n in rows: print(f"   {t}  {n}")
