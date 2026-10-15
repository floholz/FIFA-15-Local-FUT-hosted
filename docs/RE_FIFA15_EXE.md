# Reverse engineering fifa15.exe (BlazeSDK 14.2.1.1 / DirtySDK)

Snapshot 2026-10-15. Applies to the fifa15.exe build in this install
(87,268,816 bytes, 2015-09-15). Addresses are virtual addresses at the fixed
image base 0x140000000.

## Why earlier static analysis failed, and how to get readable code

- `.arch` (0x140001000, 31 MB) has **no bytes on disk**. It holds the decrypted
  data at runtime: strings, vtables, TDF metadata and enum tables.
- `.xtext` (0x142e7e000, 68 MB) is the code. About half of its pages are
  **encrypted on disk** and decrypted at startup. `.pdata` points into it.
- After startup the code is ordinary MSVC x64. It is not virtualized, and DirtySDK
  functions match EA's public C source.

Recipe:

```sh
python3 tools/re/dump_unpacked.py /tmp/fifa-dump                # starts the game, dumps memory
python3 tools/re/rebuild_pe.py /tmp/fifa-dump/image.bin "<game>/fifa15.exe" /tmp/fifa-dump/fifa15_unpacked.exe
python3 tools/re/carve_pe.py /tmp/fifa-dump/fifa15_unpacked.exe 0x144700000 0x144a00000 /tmp/fifa-dump/net.exe
/opt/ghidra/support/analyzeHeadless /tmp/gh net -import /tmp/fifa-dump/net.exe \
    -scriptPath tools/re -postScript ExportDecomp.java /tmp/gh/net_decomp.c   # ~40 s, 7k functions
```

The full `.xtext` carve (0x142e7e000-0x147106000) analyses in about 5 minutes headless.
That is too slow for the rea MCP Ghidra provider's timeout, but the network slice is fine there.
No RTTI is compiled in. Reference source: DirtySDK 15.1.6 in `TornadoCookie/EAWebKit16-Linux`.
FIFA's ConnApi is an older revision with extra selectors such as `gsrv`, `peer` and `fbkp`.

## Server-controlled DirtySDK overrides (PreAuth CONF)

BlazeSDK walks the server config map (the PreAuth `CONF` map, which also carries
`connIdleTimeout` and `pingPeriod`) and applies every key with these prefixes:

| Key | Value | Applied as |
|---|---|---|
| `Override_ConnApi_<4cc>` | `"<int>,<int>,<str or NULL>"` | `ConnApiControl(ref, '4cc', v1, v2, p)` |
| `Override_ProtoTunnel_<4cc>` | same | `ProtoTunnelControl(...)` |
| `Override_QosApi_<4cc>` | same | `QosApiControl(...)` |
| `Override_ConnApiAdapter_<4cc>` | `"<int>"` or `"true"/"false"` | ConnApiAdapter config field |

The map is binary-searched, so the server must keep it sorted by key.
Set entries with `FUT15_CONF_OVERRIDES="key=value;key=value"`.

ConnApiAdapter selectors and the config offsets they write:
`mngl` +0x15 demangler, `etnd` +0x16 **tunnel for P2P games**, `evoi` +0x17 voip,
`dsdc` +0x14 dirtycast, `time` +0x20, `ctim` +0x24, `ulmt` +0x28, `mwid` +0x04,
`mnen` +0x08, `mntn` +0x0c max tunnels, `mvgp` +0x10/+0x12 game and voip port,
`rbuf`/`sbuf` +0xb4/+0xb8 tunnel buffers, `mvtm` +0xbc, `pngl` +0x1c, `vpsp` +0x18.

ConnApi selectors (handled by ConnApiControl, `0x1448dcee9`): `tunl` sets tunnel
on/off (v1) and port (v2). `mngl` toggles the demangler and `dsrv` sets the demangler host.
`gprt` and `vprt` set the game and voip ports, `type` the conn flags, `time`/`ctim` the timeouts.

## Mesh setup path (ConnApiAdapter, BlazeSDK)

| Address | Function |
|---|---|
| 0x1425dfa68 | ConnApiAdapter vtable (slot 0x58 = initNetworkMesh, 0x60 = connectToEndpoint) |
| 0x1447ac15d | `initNetworkMesh(adapter, mesh, cfg)`: creates the per-mesh network, error 3 on failure |
| 0x1447ac24d | stores mesh (+0x16978) and config (+0x16980), then the steps below |
| 0x1447d7dab | create ConnApi (`ConnApiCreate2` 0x1448dd5fa) + apply `Override_ConnApi_*` (0x1448724ff) |
| 0x1447d93fb | tunnel setup: `ProtoTunnelCreate` 0x1448f4d7a if enabled, then `tunl`, `gprt`/`fbkp`, `vprt` |
| 0x14478a1b3 | final step: **`ConnApiOnline`** (0x1448dce89), skipped if adapter byte +0x169a4 is set |
| 0x1447b0d39 | "dirtycast" check: cfg+0x14 set AND topology in {1 dedicated, 0x84, 0x85} |
| 0x1447b06af | tunnel decision: non-dirtycast returns cfg+0x16 (`etnd`); dirtycast needs mesh attr `tprt` |
| 0x14476d2f4 | listener dispatch (`err` argument: 0 ok, 3 init failure) |
| 0x144772f62 | `Game` constructor. The Mesh interface is at Game+0x418 (vtable 0x1425df790) |

The mesh vtable methods used by the adapter are +0x48 (tunnel key), +0x58 (topology),
+0x60 (voip topology), +0xd8 (local network address) and +0x100 (mesh attributes,
keys `tprt`/`vprt`).

Enum values confirmed from TDF metadata: topology `CLIENT_SERVER_PEER_HOSTED`=0,
`CLIENT_SERVER_DEDICATED`=1, `PEER_TO_PEER_FULL_MESH`=0x82 (130), `PARTIAL_MESH`=0x83,
`PEER_TO_PEER_DIRTYCAST_FAILOVER`=0x84, `PEER_HOSTED_DIRTYCAST_FAILOVER`=0x85,
`NETWORK_DISABLED`=0xff. VOIP: 0 disabled, 1 dedicated, 2 p2p. Presence: 0/1/2.
PlayerState `ACTIVE_CONNECTING`=2.

TDF member tables live in the section labelled `.reloc` (0x141ddb000), which actually holds writable data.
Each entry is a header qword (byte 0 = 0, bytes 1-3 = tag, byte 4 = type) followed by a name pointer.
FIFA's ReplicatedGamePlayer (table at 0x142a5c5c8):
BLOB CONG CSID EXID GID JFPS LOC NAME NASP PATT PID PNET PSET RCRE ROLE SID SLOT STAT TIDX TIME UGID UID UUID.
NetworkAddress union: 0 XboxClient, 1 XboxServer, 2 IpPair {EXIP, INIP, MACI}, 3 IpAddress {IP, MACI, PORT}, 4 HostName.
The adapter's client-list builder (0x1447ab085) reads IpPair internal ip/port at +0x20/+0x24,
external at +0x40/+0x44, and MACI at +0x50, which becomes the ConnApi client id.

## In-game tracing

`tools/p2p-hook` has a trace mode (`gametrace.inc`). It hooks the functions above
plus every ConnApiAdapter vtable method and logs args, return value and caller address
to `p2p-hook.log`. Each hook waits until its target's decrypted prologue matches,
so other exe builds are skipped safely. Verified 2026-10-15: all 38 hooks install
and the game keeps running. Run `tools/linux/trace_test.sh`, then search an online
match. See docs/P2P_STATUS.md.
