#!/usr/bin/env python3
"""Hosted-mode regression tests for FIFA 15 Local FUT.

Runs without the game and without extra dependencies:

    python3 tests/run_all.py            # all scenarios
    python3 tests/run_all.py multi tdf  # only the named scenarios

It starts real server processes on the standard FIFA ports (3216, 8099, 8199,
10051, 17502, 42230, 42232 on 127.0.0.1), so nothing else may be listening
there.  Everything is written to a temporary directory; the repository and
your real Local FUT save are never touched.
"""
from __future__ import annotations

import importlib.util
import json
import os
import shutil
import socket
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request
from pathlib import Path

REPO = Path(__file__).resolve().parents[1]
PAYLOAD = REPO / "payload"
PY = sys.executable

PORTS = {"lsx": 3216, "redirect": 42230, "blaze": 10051, "qos": 17502, "easw": 42232, "fut": 8199, "legacy": 8099}
ALL_PORTS = sorted(PORTS.values())


# ---- helpers -----------------------------------------------------------------
class Failure(AssertionError):
    pass


def check(cond: bool, msg: str) -> None:
    if not cond:
        raise Failure(msg)


def port_open(port: int, host: str = "127.0.0.1", timeout: float = 0.3) -> bool:
    try:
        with socket.create_connection((host, port), timeout=timeout):
            return True
    except OSError:
        return False


def wait_port(port: int, timeout: float = 20.0) -> bool:
    end = time.time() + timeout
    while time.time() < end:
        if port_open(port):
            return True
        time.sleep(0.2)
    return False


def wait_ports_free(timeout: float = 15.0) -> None:
    end = time.time() + timeout
    while time.time() < end:
        if not any(port_open(p, timeout=0.1) for p in ALL_PORTS):
            return
        time.sleep(0.3)
    busy = [p for p in ALL_PORTS if port_open(p, timeout=0.1)]
    raise Failure(f"ports still in use before scenario: {busy}")


def http(method: str, path: str, body=None, headers=None, port: int = PORTS["fut"], retries: int = 1):
    data = json.dumps(body).encode() if isinstance(body, (dict, list)) else body
    req = urllib.request.Request(f"http://127.0.0.1:{port}{path}", data=data, method=method, headers=headers or {})
    last: Exception | None = None
    for _ in range(retries):
        try:
            with urllib.request.urlopen(req, timeout=5) as r:
                return r.status, {k: v for k, v in r.headers.items()}, r.read()
        except urllib.error.HTTPError as e:
            return e.code, {k: v for k, v in e.headers.items()}, e.read()
        except Exception as exc:  # connection reset while the listener spins up
            last = exc
            time.sleep(0.3)
    raise Failure(f"{method} {path}: {last}")


class Game:
    """A throwaway copy of payload/ so generated cl.ini/EA-MITM.ini land in the temp dir."""

    def __init__(self, tmp: Path):
        self.root = tmp / "game"
        shutil.copytree(PAYLOAD / "localfut15", self.root / "localfut15", ignore=shutil.ignore_patterns("__pycache__"))
        for name in ("cl.ini", "EA-MITM.ini"):
            shutil.copy(PAYLOAD / name, self.root / name)
        self.server_py = self.root / "localfut15" / "server.py"


class Proc:
    def __init__(self, game: Game, runtime_root: Path, *args: str):
        self.env = dict(os.environ, FUT15_RUNTIME_ROOT=str(runtime_root), PYTHONUNBUFFERED="1")
        self.runtime_root = runtime_root
        self.proc = subprocess.Popen(
            [PY, str(game.server_py), *args], env=self.env,
            stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True,
        )
        self.output = ""

    def stop(self, timeout: float = 8.0) -> str:
        if self.proc.poll() is None:
            self.proc.terminate()
        try:
            self.output = self.proc.communicate(timeout=timeout)[0]
        except subprocess.TimeoutExpired:
            self.proc.kill()
            self.output = self.proc.communicate()[0]
        return self.output

    def wait_exit(self, timeout: float = 30.0) -> int:
        self.output = self.proc.communicate(timeout=timeout)[0]
        return self.proc.returncode

    def runtime_ports(self) -> dict:
        for name in ("runtime_ports.json", "runtime_ports-server.json"):
            path = self.runtime_root / name
            if path.exists():
                return json.loads(path.read_text(encoding="utf-8"))
        raise Failure(f"no runtime_ports file in {self.runtime_root}")


def ip_host_order(ip: str) -> int:
    return int.from_bytes(socket.inet_aton(ip), "big")


def ip_little_endian(ip: str) -> int:
    return int.from_bytes(socket.inet_aton(ip), "little")


def load_server_module(runtime_root: Path):
    """Import server.py in-process (for pure-function tests) with state in runtime_root."""
    os.environ["FUT15_RUNTIME_ROOT"] = str(runtime_root)
    name = f"localfut15_server_{runtime_root.name.replace('-', '_')}"
    spec = importlib.util.spec_from_file_location(name, PAYLOAD / "localfut15" / "server.py")
    module = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    sys.modules[name] = module  # dataclasses look the module up by name during exec
    # The module wires its log StreamHandler to whatever sys.stderr is at import
    # time; point it at /dev/null so in-process scenarios stay quiet.
    real_stderr = sys.stderr
    with open(os.devnull, "w", encoding="utf-8") as devnull:
        sys.stderr = devnull
        try:
            spec.loader.exec_module(module)
        finally:
            sys.stderr = real_stderr
    return module


# ---- scenarios ---------------------------------------------------------------
def scenario_server(tmp: Path, game: Game) -> None:
    """Server mode advertises --public-host everywhere and writes no game files."""
    srv = Proc(game, tmp / "rt-server", "--mode", "server", "--host", "127.0.0.1", "--public-host", "10.1.2.3")
    try:
        check(wait_port(PORTS["blaze"]) and wait_port(PORTS["fut"]), "server ports did not open")
        _, _, redir = http("GET", "/redirector/getServerInstance", port=PORTS["redirect"], retries=10)
        text = redir.decode()
        check("<hostname>10.1.2.3</hostname>" in text, f"redirector hostname: {text}")
        check(f"<ip>{ip_host_order('10.1.2.3')}</ip>" in text, f"redirector ip int: {text}")
        _, _, qos = http("GET", "/qos/qos?qtyp=1", port=PORTS["qos"], retries=10)
        check(f"<qosip>{ip_little_endian('10.1.2.3')}</qosip>" in qos.decode(), f"qos ip: {qos!r}")
        _, _, fw = http("GET", "/qos/firewall", port=PORTS["qos"])
        check(f"<ips>{ip_little_endian('127.0.0.1')}</ips>" in fw.decode(), f"firewall should echo client ip: {fw!r}")
        _, _, shards = http("GET", "/ut/shards", retries=10)
        check(json.loads(shards)["shardInfo"][0]["clientFacingIpPort"] == "10.1.2.3:8199", f"shards: {shards!r}")
        _, _, status = http("GET", "/localfut/status")
        st = json.loads(status)
        check(st["mode"] == "server" and st["publicHost"] == "10.1.2.3" and st["users"] == 0, f"status: {st}")
        rp = srv.runtime_ports()
        check(rp["mode"] == "server" and rp["host"] == "10.1.2.3", f"runtime_ports: {rp}")
        check("mode=server" not in (game.root / "cl.ini").read_text(), "server mode must not write cl.ini")
        check((tmp / "rt-server" / "logs" / "localfut15-server.log").exists(), "server mode must log to its own file")
        check((tmp / "rt-server" / "runtime_ports-server.json").exists(), "server mode must use its own port-map file")
    finally:
        out = srv.stop()
    check("Traceback" not in out, f"server logged a traceback:\n{out[-2000:]}")


def scenario_local(tmp: Path, game: Game) -> None:
    """Local mode is the original single-player behaviour."""
    srv = Proc(game, tmp / "rt-local")
    try:
        check(wait_port(PORTS["blaze"]) and wait_port(PORTS["lsx"]), "local ports did not open")
        _, _, redir = http("GET", "/redirector/getServerInstance", port=PORTS["redirect"], retries=10)
        check("<hostname>127.0.0.1</hostname>" in redir.decode() and "<ip>2130706433</ip>" in redir.decode(), "local redirector")
        _, _, qos = http("GET", "/qos/qos?qtyp=1", port=PORTS["qos"], retries=10)
        check("<qosip>16777343</qosip>" in qos.decode(), "local qos ip")
        cl = (game.root / "cl.ini").read_text()
        check("mode=local" in cl and "FUT_TARGET_HOSTNAME = 127.0.0.1" in cl, f"cl.ini: {cl}")
        rp = srv.runtime_ports()
        check(rp["mode"] == "local" and rp["host"] == "127.0.0.1" and rp["lsx_mode"] == "local", f"runtime_ports: {rp}")
        _, h, _ = http("POST", "/ut/auth", {"x": 1}, retries=10)
        _, _, acct = http("GET", "/ut/game/fifa15/user/accountinfo", headers={"X-UT-SID": h["X-UT-SID"]})
        persona = json.loads(acct)["userAccountInfo"]["personas"][0]
        check(persona["personaId"] == 1000000002, f"local persona must stay the config one: {persona}")
    finally:
        out = srv.stop()
    check("Traceback" not in out, f"local server logged a traceback:\n{out[-2000:]}")


def scenario_client(tmp: Path, game: Game) -> None:
    """Client mode registers on the server, runs only LSX and routes FIFA to the server."""
    srv = Proc(game, tmp / "rt-client-server", "--mode", "server", "--host", "127.0.0.1", "--public-host", "127.0.0.1")
    cli = None
    try:
        check(wait_port(PORTS["fut"]) and wait_port(PORTS["blaze"]), "helper server did not start")
        http("GET", "/localfut/status", retries=10)
        crt = tmp / "rt-client"
        crt.mkdir()
        (crt / "hosted.json").write_text(json.dumps({"mode": "client", "server_host": "127.0.0.1", "player_name": "carol"}))
        cli = Proc(game, crt)
        check(wait_port(PORTS["lsx"]), "client LSX did not open")
        time.sleep(1.0)  # let the banner and routing files land
        cl = (game.root / "cl.ini").read_text()
        mitm = (game.root / "EA-MITM.ini").read_text()
        check("mode=client" in cl and "FUT_TARGET_HOSTNAME = 127.0.0.1" in cl, f"client cl.ini: {cl}")
        check("Redirect.0.Address=127.0.0.1" in mitm, "client EA-MITM.ini")
        rp = cli.runtime_ports()
        check(rp["mode"] == "client" and rp["lsx_mode"] == "local", f"client runtime_ports: {rp}")
        hosted = json.loads((crt / "hosted.json").read_text())
        check(len(hosted.get("player_secret", "")) == 32, f"secret must be generated: {hosted}")
        out = cli.stop()
        check("Player     : carol (persona 1200000001)" in out, f"client banner:\n{out[-1500:]}")
        cli = None
        # same name, different secret -> refused
        crt2 = tmp / "rt-client2"
        crt2.mkdir()
        hosted["player_secret"] = "another-secret-0000"
        (crt2 / "hosted.json").write_text(json.dumps(hosted))
        dup = Proc(game, crt2)
        rc = dup.wait_exit()
        check(rc == 2 and "already taken" in dup.output, f"duplicate name must be refused: rc={rc}\n{dup.output[-800:]}")
    finally:
        if cli is not None:
            cli.stop()
        srv.stop()


def scenario_client_unreachable(tmp: Path, game: Game) -> None:
    """Client mode refuses to start when the server is down."""
    cli = Proc(game, tmp / "rt-unreach", "--mode", "client", "--server-host", "127.0.0.1")
    rc = cli.wait_exit()
    check(rc == 2, f"expected exit 2, got {rc}")
    check("not reachable" in cli.output and "FUT/POW (8199)" in cli.output, cli.output[-800:])
    check(not (tmp / "rt-unreach" / "runtime_ports.json").exists(), "must not write runtime_ports.json when unreachable")


def scenario_multi(tmp: Path, game: Game) -> None:
    """Two players on one server: separate identities, sessions and coins."""
    rt = tmp / "rt-multi"
    srv = Proc(game, rt, "--mode", "server", "--host", "127.0.0.1", "--public-host", "127.0.0.1")
    try:
        check(wait_port(PORTS["fut"]), "server did not start")
        http("GET", "/localfut/status", retries=10)
        c, _, b = http("POST", "/localfut/register", {"name": "alice", "secret": "alice-secret-1"})
        alice = json.loads(b)
        check(c == 200 and alice["personaId"] == 1200000001 and alice["nucleusId"] == 1100000001, f"alice: {c} {alice}")
        c, _, b = http("POST", "/localfut/register", {"name": "bob", "secret": "bob-secret-123"})
        bob = json.loads(b)
        check(c == 200 and bob["personaId"] == 1200000002, f"bob: {c} {bob}")
        c, _, b = http("POST", "/localfut/register", {"name": "Alice", "secret": "wrong-secret-xx"})
        check(c == 403, f"wrong secret must be 403: {c} {b!r}")
        c, _, b = http("POST", "/localfut/register", {"name": "a", "secret": "short"})
        check(c == 400, f"bad input must be 400: {c} {b!r}")
        c, _, b = http("POST", "/localfut/register", {"name": "ALICE", "secret": "alice-secret-1"})
        check(json.loads(b)["userId"] == 1, "re-login (case-insensitive) must keep the account")

        _, h, _ = http("POST", "/ut/auth", {"identification": {"authCode": alice["token"]}})
        sid_a = h["X-UT-SID"]
        _, h, _ = http("POST", "/ut/auth", {"identification": {"authCode": bob["token"]}})
        sid_b = h["X-UT-SID"]
        check(sid_a != sid_b, "sessions must differ")

        def persona(sid: str | None):
            hdr = {"X-UT-SID": sid} if sid else {}
            _, _, acct = http("GET", "/ut/game/fifa15/user/accountinfo", headers=hdr)
            p = json.loads(acct)["userAccountInfo"]["personas"][0]
            return p["personaId"], p["personaName"]

        check(persona(sid_a) == (1200000001, "alice"), f"alice session: {persona(sid_a)}")
        check(persona(sid_b) == (1200000002, "bob"), f"bob session: {persona(sid_b)}")
        _, h, _ = http("POST", "/ut/auth", {"nucleusPersonaId": 1200000002})
        check(h["X-UT-SID"] == sid_b, "persona id in body must resolve the user")
        http("POST", "/localfut/register", {"name": "bob", "secret": "bob-secret-123", "mac": "d8:43:ae:40:20:1e"})
        # FIFA's real /ut/auth body: persona of the Blaze session + the machine MAC; MAC wins
        _, h, _ = http("POST", "/ut/auth", {"nucleusPersonaId": 1200000001, "macAddress": "d8:43:ae:40:20:1e", "method": "cas"})
        check(h["X-UT-SID"] == sid_b, "macAddress in body must resolve the user before persona")
        # alice logs in last from this ip, so the ip fallback must resolve to alice
        http("POST", "/localfut/register", {"name": "alice", "secret": "alice-secret-1"})
        check(persona(None)[1] == "alice", "ip fallback -> last player logged in from this ip")

        coins = subprocess.run([PY, str(game.root / "localfut15" / "add_coins.py"), "5000", "--user", "alice"],
                               env=srv.env, capture_output=True, text=True)
        check(coins.returncode == 0, coins.stdout + coins.stderr)
        _, _, ca = http("GET", "/ut/game/fifa15/user/credits", headers={"X-UT-SID": sid_a})
        _, _, cb = http("GET", "/ut/game/fifa15/user/credits", headers={"X-UT-SID": sid_b})
        check(json.loads(ca)["credits"] == 5000 and json.loads(cb)["credits"] == 0, f"coins leaked: {ca!r} {cb!r}")
        for rel in ("users.sqlite3", "users/user-1.sqlite3", "users/user-2.sqlite3"):
            check((rt / rel).exists(), f"missing {rel}")
        _, _, status = http("GET", "/localfut/status")
        check(json.loads(status)["users"] == 2, status)
    finally:
        out = srv.stop()
    check("Traceback" not in out, f"server logged a traceback:\n{out[-2000:]}")


def scenario_tdf(tmp: Path, game: Game) -> None:
    """The TDF decoder round-trips the server's own encoder output."""
    S = load_server_module(tmp / "rt-tdf")
    tree = S._tdf_decode_tree(S._blaze_fifa35_auth_bootstrap_payload())
    check(tree["SESS"]["PDTL"]["DSNM"] == "LocalPlayer" and tree["SESS"]["UID"] == 1000000002, f"fifa35: {tree}")
    tree = S._tdf_decode_tree(S._blaze_preauth_payload())
    check(tree["QOSS"]["BWPS"]["PSA"] == "127.0.0.1" and tree["CIDS"] == [1, 9, 35, 30722], f"preauth: {tree}")
    mixed = (
        S._tdf_field_object_id("OBID", 4, 1, 12345)
        + S._tdf_field_blob("BLOB", b"\x01\x02")
        + S._tdf_field_list_int("LIST", [1, 70, 5000])
        + S._tdf_field_map_str_group("MAPG", [("k", S._tdf_field_int("V", 9))])
        + S._tdf_field_map_str_str("MAPS", [("a", "b")])
        + S._tdf_field_list_groups("LGRP", [S._tdf_field_str("ID", "x")])
        + S._tdf_field_int("NEG", -5)
    )
    tree = S._tdf_decode_tree(mixed)
    check(tree["OBID"] == {"component": 4, "type": 1, "id": 12345}, tree)
    check(tree["BLOB"] == {"blob": "0102"} and tree["LIST"] == [1, 70, 5000], tree)
    check(tree["MAPG"] == {"k": {"V": 9}} and tree["MAPS"] == {"a": "b"} and tree["LGRP"] == [{"ID": "x"}], tree)
    check(tree["NEG"] == (1 << 64) - 5, tree)
    check(S._tdf_debug_summary(b"\xff\xfe\x00garbage").startswith("hex="), "garbage must fall back to hex summary")
    check(S._tdf_debug_summary(mixed).startswith("tree="), "valid payload must log as tree")


def scenario_lsx(tmp: Path, game: Game) -> None:
    """The LSX stub hands FIFA the hosted player's identity and session token."""
    S = load_server_module(tmp / "rt-lsx")
    req = lambda op: f'<LSX><Request id="7"><{op}/></Request></LSX>'
    local_profile = S._lsx_xml_response(req("GetProfile"))
    check('UserId="1000000001" PersonaId="1000000002" Persona="LocalPlayer"' in local_profile, local_profile)
    check('value="LOCAL-FUT15-AUTH-CODE"' in S._lsx_xml_response(req("GetAuthCode")), "local auth code")
    S.CLIENT_IDENTITY = {"id": 3, "name": "carol", "nucleus_id": 1100000003, "persona_id": 1200000003,
                         "db_path": S.DB_PATH, "token": "LFUT1.3." + "ab" * 16}
    profile = S._lsx_xml_response(req("GetProfile"))
    check('UserId="1100000003" PersonaId="1200000003" Persona="carol"' in profile, profile)
    check(f'value="LFUT1.3.{"ab" * 16}"' in S._lsx_xml_response(req("GetAuthCode")), "client auth code must be the token")
    check(f'value="LFUT1.3.{"ab" * 16}"' in S._lsx_xml_response(req("GetAuthToken")), "client auth token must be the token")
    # and the server side recognises that token inside an arbitrary payload
    S.CLIENT_IDENTITY = None
    user = S._users().authenticate("carol", "carol-secret-12", "127.0.0.1")
    token = S._issue_token(user)
    found = S._user_from_token_text(b"\x00junk" + token.encode() + b"\x00more")
    check(found is not None and found["id"] == user["id"], "token scan")
    check(S._user_from_token_text(b"LFUT1.9." + b"0" * 32) is None, "unknown token must not resolve")


def scenario_blaze_mac(tmp: Path, game: Game) -> None:
    """A registered player's MAC in Blaze PostAuth binds the Blaze connection to that player."""
    S = load_server_module(tmp / "rt-blaze-mod")  # encoder helpers only
    rt = tmp / "rt-blaze"
    srv = Proc(game, rt, "--mode", "server", "--host", "127.0.0.1", "--public-host", "127.0.0.1")
    try:
        check(wait_port(PORTS["fut"]) and wait_port(PORTS["blaze"]), "server did not start")
        http("GET", "/localfut/status", retries=10)
        c, _, b = http("POST", "/localfut/register", {"name": "dave", "secret": "dave-secret-123", "mac": "d8:43:ae:40:20:1e"})
        check(c == 200, b)
        c, _, b = http("POST", "/localfut/register", {"name": "erin", "secret": "erin-secret-123", "mac": "$aabbccddeeff"})
        check(c == 200, b)
        # erin was the last to register from 127.0.0.1, so ip-binding would say erin;
        # a PostAuth carrying dave's MAC must rebind the connection to dave.
        with socket.create_connection(("127.0.0.1", PORTS["blaze"]), timeout=5) as sock:
            sock.sendall(S._fire2_build(9, 7, 0, 0, S._tdf_field_str("CFID", "x")))
            sock.recv(65536)
            sock.sendall(S._fire2_build(9, 8, 1, 0, S._tdf_field_str("MAC", "d8:43:ae:40:20:1e") + S._tdf_field_str("UDID", "")))
            sock.recv(65536)
            # login after the rebind must answer with dave's persona
            sock.sendall(S._fire2_build(35, 10, 2, 0, S._tdf_field_str("AUTH", "")))
            reply = sock.recv(65536)
            pkt = S._fire2_try_parse(bytearray(reply))
            tree = S._tdf_decode_tree(pkt.payload)
            check(tree["SESS"]["PDTL"]["DSNM"] == "dave" and tree["SESS"]["UID"] == 1200000001, f"login reply: {tree}")
    finally:
        out = srv.stop()
    # (UserSessions network-info capture is validated end-to-end against the real
    # game, not here: a hand-built ADDR union is too brittle to assert on.)
    check("BLAZE MAC rebind 127.0.0.1: erin" in out and "-> dave" in out, f"expected MAC rebind in log:\n{out[-2500:]}")
    check("BLAZE LOGIN user=dave(id=1) via=mac" in out, f"login must use the MAC-bound player:\n{out[-2500:]}")
    check("Traceback" not in out, out[-2000:])


def scenario_gating(tmp: Path, game: Game) -> None:
    """A gated server refuses registrations without the access code / off the allowlist."""
    rt = tmp / "rt-gate"
    (rt).mkdir(parents=True)
    # write hosted.json-independent config via a config.json override in the game copy
    cfg = game.root / "localfut15" / "config.json"
    import json as _json
    data = _json.loads(cfg.read_text())
    data["server_access_code"] = "let-me-in-42"
    data["allowed_players"] = ["alice", "bob"]
    cfg.write_text(_json.dumps(data))
    srv = Proc(game, rt, "--mode", "server", "--host", "127.0.0.1", "--public-host", "127.0.0.1")
    try:
        check(wait_port(PORTS["fut"]), "server did not start")
        http("GET", "/localfut/status", retries=10)
        st = json.loads(http("GET", "/localfut/status")[2])
        check(st.get("gated") is True, f"status must report gated: {st}")
        c, _, b = http("POST", "/localfut/register", {"name": "alice", "secret": "alice-secret-1"})
        check(c == 403 and b"access code" in b, f"no code must be refused: {c} {b!r}")
        c, _, b = http("POST", "/localfut/register", {"name": "alice", "secret": "alice-secret-1", "access_code": "nope"})
        check(c == 403 and b"access code" in b, f"wrong code must be refused: {c} {b!r}")
        c, _, b = http("POST", "/localfut/register", {"name": "carol", "secret": "carol-secret-1", "access_code": "let-me-in-42"})
        check(c == 403 and b"allowlist" in b, f"off-allowlist must be refused even with code: {c} {b!r}")
        c, _, b = http("POST", "/localfut/register", {"name": "alice", "secret": "alice-secret-1", "access_code": "let-me-in-42"})
        check(c == 200, f"allowed player with code must succeed: {c} {b!r}")
    finally:
        out = srv.stop()
    check("Traceback" not in out, out[-2000:])


SCENARIOS = {
    "server": scenario_server,
    "local": scenario_local,
    "client": scenario_client,
    "client-unreachable": scenario_client_unreachable,
    "multi": scenario_multi,
    "tdf": scenario_tdf,
    "lsx": scenario_lsx,
    "blaze-mac": scenario_blaze_mac,
    "gating": scenario_gating,
}


def main(argv: list[str]) -> int:
    names = argv or list(SCENARIOS)
    unknown = [n for n in names if n not in SCENARIOS]
    if unknown:
        print(f"unknown scenario(s): {unknown}; available: {list(SCENARIOS)}")
        return 2
    failures: list[str] = []
    with tempfile.TemporaryDirectory(prefix="fut15-tests-") as tmpdir:
        tmp = Path(tmpdir)
        for name in names:
            game = Game(tmp / name)
            started = time.time()
            try:
                wait_ports_free()
                SCENARIOS[name](tmp / name, game)
                print(f"PASS  {name:20s} {time.time() - started:5.1f}s")
            except Exception as exc:  # noqa: BLE001 - report everything
                failures.append(name)
                print(f"FAIL  {name:20s} {time.time() - started:5.1f}s\n      {type(exc).__name__}: {exc}")
    print("ALL PASS" if not failures else f"FAILED: {failures}")
    return 0 if not failures else 1


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
