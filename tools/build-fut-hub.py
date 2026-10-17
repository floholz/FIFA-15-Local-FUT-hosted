#!/usr/bin/env python3
"""Build the OUFT Ultimate Team hub layout into a copy of data_front_end.big/.bh.

    tools/build-fut-hub.py <original data_front_end.big> <original .bh> <out.big> <out.bh>

Takes EA's data/ui/layout/fut/futfluxhubcfg.xml from the original archive, replaces
its PLAY tab (<panel_1>) with mods/fut-hub/play_tab.xml and stores the result back
(tools/bigreplace.py). Only our block lives in the repo; the launcher ships a patch.
"""
from __future__ import annotations

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import bigreplace  # noqa: E402

ENTRY = "data/ui/layout/fut/futfluxhubcfg.xml"
REPO = Path(__file__).resolve().parents[1]


def build(xml: str, play_tab: str) -> str:
    start = xml.index("\t<panel_1 DESCRIPTION")
    end = xml.index("\t</panel_1>") + len("\t</panel_1>")
    return xml[:start] + play_tab.rstrip("\n").replace("\n", "\r\n") + xml[end:]


def main(argv: list[str]) -> int:
    if len(argv) != 5:
        print(__doc__)
        return 2
    big, bh = open(argv[1], "rb").read(), open(argv[2], "rb").read()
    entry = next(e for e in bigreplace.read_table(big) if e[1] == ENTRY)
    original = bigreplace.unchunkzip(big[entry[2]:entry[2] + entry[3]]).decode("ascii")
    if "OUFT:" in original:
        raise SystemExit("the input archive already has the OUFT layout; pass the originals")
    play_tab = (REPO / "mods/fut-hub/play_tab.xml").read_text(encoding="ascii")
    new = build(original, play_tab)
    import xml.dom.minidom
    xml.dom.minidom.parseString(new.encode("ascii"))  # must stay well-formed
    out, outbh = bigreplace.replace(big, bh, ENTRY, new.encode("ascii"))
    open(argv[3], "wb").write(out)
    open(argv[4], "wb").write(outbh)
    print(f"  fut hub layout: {len(original)} -> {len(new)} bytes")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
