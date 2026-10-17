#!/usr/bin/env python3
"""Replace one file inside an EA BIG4 archive (+ its .bh index), keeping its name.

    tools/bigreplace.py <in.big> <in.bh> <entry name> <new file> <out.big> <out.bh>

The new content is stored as chunkzip (EA's deflate container, same layout as the
originals) and appended at the end of the archive (64-byte aligned). The archive table
and the .bh index entry are pointed at it. The .bh index keys files by a hash of the
NAME, so replacing (not adding) needs no knowledge of that hash.

BIG4 layout: "BIG4", u32 LE archive size, u32 BE file count, u32 BE header size, then
per file: u32 BE offset, u32 BE size, NUL-terminated name.
.bh ("ViV4") layout: 16-byte header, then per file 20 bytes: u32 BE offset, u32 BE size,
u32 0, two u32 name hashes.
"""
from __future__ import annotations

import struct
import sys
import zlib

ALIGN = 64


def chunkzip(data: bytes) -> bytes:
    comp = zlib.compressobj(9, zlib.DEFLATED, -15)
    body = comp.compress(data) + comp.flush()
    if len(data) > 0x2D000:
        raise ValueError("only single-chunk files are supported (<= 0x2d000 bytes)")
    head = b"chunkzip" + struct.pack(">IIII", 2, len(data), 0x2D000, 1)
    head += struct.pack(">I", 0x10) + b"\0" * 12 + struct.pack(">II", len(body), 1)
    return head + body


def unchunkzip(blob: bytes) -> bytes:
    if blob[:8] != b"chunkzip":
        return blob
    ver, usize, _, nchunks = struct.unpack(">IIII", blob[8:24])
    if ver != 2 or nchunks != 1:
        raise ValueError("unsupported chunkzip")
    clen = struct.unpack(">I", blob[40:44])[0]
    out = zlib.decompress(blob[48:48 + clen], -15)
    if len(out) != usize:
        raise ValueError("chunkzip size mismatch")
    return out


def read_table(big: bytes):
    n, hdr = struct.unpack(">II", big[8:16])
    ents, off = [], 16
    for _ in range(n):
        o, s = struct.unpack(">II", big[off:off + 8])
        end = big.index(b"\0", off + 8)
        ents.append((off, big[off + 8:end].decode("latin-1"), o, s))  # off = table position
        off = end + 1
    return ents


def replace(big: bytes, bh: bytes, name: str, content: bytes) -> tuple[bytes, bytes]:
    ents = read_table(big)
    match = [e for e in ents if e[1] == name]
    if len(match) != 1:
        raise SystemExit(f"{name}: {len(match)} matches in the archive")
    tpos, _, old_off, old_size = match[0]
    blob = chunkzip(content)
    pad = (-len(big)) % ALIGN
    new_off = len(big) + pad
    out = bytearray(big) + b"\0" * pad + blob
    struct.pack_into(">II", out, tpos, new_off, len(blob))
    struct.pack_into("<I", out, 4, len(out))  # archive size
    # .bh: find the row that points at the old data
    n = struct.unpack(">I", bh[8:12])[0]
    newbh = bytearray(bh)
    rows = [i for i in range(n) if struct.unpack(">II", bh[16 + i * 20:24 + i * 20]) == (old_off, old_size)]
    if len(rows) != 1:
        raise SystemExit(f"{name}: {len(rows)} matching .bh rows")
    struct.pack_into(">II", newbh, 16 + rows[0] * 20, new_off, len(blob))
    return bytes(out), bytes(newbh)


def main(argv: list[str]) -> int:
    if len(argv) != 7:
        print(__doc__)
        return 2
    big, bh = open(argv[1], "rb").read(), open(argv[2], "rb").read()
    content = open(argv[4], "rb").read()
    out, outbh = replace(big, bh, argv[3], content)
    # self-check: the new entry decodes back to the content
    e = next(e for e in read_table(out) if e[1] == argv[3])
    if unchunkzip(out[e[2]:e[2] + e[3]]) != content:
        raise SystemExit("self-check failed")
    open(argv[5], "wb").write(out)
    open(argv[6], "wb").write(outbh)
    print(f"{argv[3]}: {len(content)} bytes -> chunkzip {e[3]} at {e[2]}; archive {len(big)} -> {len(out)}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
