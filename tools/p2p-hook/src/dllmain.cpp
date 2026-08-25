/*
 * FIFA 15 Local FUT — P2P hook (Phase 0: visibility)
 *
 * A dinput8.dll proxy that:
 *   1. forwards DirectInput8Create to the real system dinput8.dll (so the game runs),
 *   2. chain-loads the existing EA-MITM hook (renamed ea-mitm.dll) so its ProtoSSL
 *      redirect keeps working untouched,
 *   3. hooks the WinSock UDP path (sendto/recvfrom/WSASendTo/WSARecvFrom) and logs
 *      every datagram's peer address + first bytes.
 *
 * Phase 0 only observes — it does not modify or redirect any traffic yet. Its job is
 * to reveal, from inside the client, exactly what the game sends on its P2P socket
 * (direct CommUDP to the peer, or ProtoTunnel frames to the QoS/tunnel port), which
 * decides the Phase 1 tunnel design.
 *
 * License: GPL-2.0-or-later (matches EA-MITM, which this loads alongside).
 */

#include <winsock2.h>
#include <ws2tcpip.h>
#include <windows.h>

#include <MinHook.h>

#include <cstdarg>
#include <cstdint>
#include <cstdio>
#include <cwchar>
#include <mutex>

// ---------------------------------------------------------------------------
// Logging (thread-safe, appends next to this DLL as p2p-hook.log)
// ---------------------------------------------------------------------------
static HINSTANCE g_self = nullptr;
static FILE*     g_log  = nullptr;
static std::mutex g_log_mtx;

static void open_log() {
    wchar_t path[MAX_PATH] = {0};
    DWORD n = GetModuleFileNameW(g_self, path, MAX_PATH);
    // strip the file name, append p2p-hook.log
    for (DWORD i = n; i > 0; --i) {
        if (path[i - 1] == L'\\' || path[i - 1] == L'/') { path[i] = 0; break; }
    }
    wcsncat(path, L"p2p-hook.log", MAX_PATH - wcslen(path) - 1);
    g_log = _wfopen(path, L"a");
}

static void logf(const char* fmt, ...) {
    std::lock_guard<std::mutex> lock(g_log_mtx);
    if (!g_log) return;
    SYSTEMTIME t;
    GetLocalTime(&t);
    fprintf(g_log, "%02d:%02d:%02d.%03d ", t.wHour, t.wMinute, t.wSecond, t.wMilliseconds);
    va_list ap;
    va_start(ap, fmt);
    vfprintf(g_log, fmt, ap);
    va_end(ap);
    fputc('\n', g_log);
    fflush(g_log);
}

// Ports run by our own local services — tagged so the log is easy to read.
static const char* port_tag(unsigned p) {
    switch (p) {
        case 42230: return "redirector";
        case 10051: return "blaze";
        case 17502: return "qos/tunnel?";
        case 42232: return "easw";
        case 8199:  return "fut";
        case 8099:  return "fut-legacy";
        case 3216:  return "lsx";
        case 53:    return "dns";
        case 5353:  return "mdns";
        case 3659:  return "GAME-P2P";
        default:    return "";
    }
}

static void log_dgram(const char* tag, SOCKET s, const sockaddr* a, int len, const char* buf, int datalen) {
    if (!a || a->sa_family != AF_INET) return;
    const sockaddr_in* si = reinterpret_cast<const sockaddr_in*>(a);
    const unsigned char* ip = reinterpret_cast<const unsigned char*>(&si->sin_addr);
    unsigned port = ntohs(si->sin_port);
    if (ip[0] == 127) return;                 // skip loopback noise
    if (port == 53 || port == 5353) return;   // skip DNS/mDNS noise

    // Capture enough to see full demangler/QoS/broadcast payloads (up to 256 bytes).
    char hex[513];
    int nb = datalen < 256 ? datalen : 256;
    for (int i = 0; i < nb; ++i) {
        static const char* H = "0123456789abcdef";
        hex[i * 2]     = H[(static_cast<unsigned char>(buf[i]) >> 4) & 0xF];
        hex[i * 2 + 1] = H[static_cast<unsigned char>(buf[i]) & 0xF];
    }
    hex[nb * 2] = 0;

    const char* tg = port_tag(port);
    logf("%-8s sock=%llu %u.%u.%u.%u:%u%s%s len=%d hex=%s",
         tag, static_cast<unsigned long long>(s),
         ip[0], ip[1], ip[2], ip[3], port,
         *tg ? " " : "", tg, datalen, hex);
}

// ---------------------------------------------------------------------------
// Demangler redirect config (Phase 1): read p2p-hook.ini next to this DLL.
//   server=<our server ip>
//   demangler_port=<port>            (default 10000)
// The game contacts DirtySDK's demangler at EA's dead servers on UDP :10000; we
// redirect those to our server's demangler, which hands back the relay endpoint.
// ---------------------------------------------------------------------------
static bool           g_redirect        = false;
static in_addr        g_server_addr     = {};
static unsigned short g_demangler_port  = 10000;
static in_addr        g_last_demangler  = {};      // original demangler IP the game targeted
static const unsigned short DEMANGLER_PORT_STD = 10000;

static void load_redirect_config() {
    wchar_t path[MAX_PATH] = {0};
    DWORD n = GetModuleFileNameW(g_self, path, MAX_PATH);
    for (DWORD i = n; i > 0; --i)
        if (path[i - 1] == L'\\' || path[i - 1] == L'/') { path[i] = 0; break; }
    wcsncat(path, L"p2p-hook.ini", MAX_PATH - wcslen(path) - 1);
    FILE* f = _wfopen(path, L"r");
    if (!f) { logf("redirect: no p2p-hook.ini (observe-only)"); return; }
    char line[256], server[128] = {0};
    unsigned port = DEMANGLER_PORT_STD;
    while (fgets(line, sizeof(line), f)) {
        if (!strncmp(line, "server=", 7))              sscanf(line + 7, "%127[^\r\n]", server);
        else if (!strncmp(line, "demangler_port=", 15)) sscanf(line + 15, "%u", &port);
    }
    fclose(f);
    if (server[0] && inet_pton(AF_INET, server, &g_server_addr) == 1) {
        g_demangler_port = (unsigned short)port;
        g_redirect = true;
        logf("redirect: demangler :%u -> %s:%u", DEMANGLER_PORT_STD, server, port);
    } else {
        logf("redirect: bad/absent server= in p2p-hook.ini (observe-only)");
    }
}

// If `to` is a demangler probe (UDP :10000), rewrite it to our server and return
// true (with the rewritten address in `out`). Records the original demangler IP.
static bool redirect_demangler_dest(const sockaddr* to, sockaddr_in* out) {
    if (!g_redirect || !to || to->sa_family != AF_INET) return false;
    const sockaddr_in* si = reinterpret_cast<const sockaddr_in*>(to);
    if (ntohs(si->sin_port) != DEMANGLER_PORT_STD) return false;
    g_last_demangler = si->sin_addr;
    *out = *si;
    out->sin_addr = g_server_addr;
    out->sin_port = htons(g_demangler_port);
    return true;
}

// If a datagram just arrived from our server's demangler, make it look like it
// came from the demangler address the game expects (so ConnApi accepts it).
static void unredirect_demangler_src(sockaddr* from, int fromlen) {
    if (!g_redirect || !from || from->sa_family != AF_INET) return;
    sockaddr_in* si = reinterpret_cast<sockaddr_in*>(from);
    if (si->sin_addr.s_addr == g_server_addr.s_addr && ntohs(si->sin_port) == g_demangler_port) {
        si->sin_addr = g_last_demangler.s_addr ? g_last_demangler : g_server_addr;
        si->sin_port = htons(DEMANGLER_PORT_STD);
    }
}

// ---------------------------------------------------------------------------
// WinSock UDP hooks (observe + demangler redirect)
// ---------------------------------------------------------------------------
typedef int (WSAAPI *sendto_t)(SOCKET, const char*, int, int, const sockaddr*, int);
typedef int (WSAAPI *recvfrom_t)(SOCKET, char*, int, int, sockaddr*, int*);
typedef int (WSAAPI *wsasendto_t)(SOCKET, LPWSABUF, DWORD, LPDWORD, DWORD,
                                  const sockaddr*, int, LPWSAOVERLAPPED,
                                  LPWSAOVERLAPPED_COMPLETION_ROUTINE);
typedef int (WSAAPI *wsarecvfrom_t)(SOCKET, LPWSABUF, DWORD, LPDWORD, LPDWORD,
                                    sockaddr*, LPINT, LPWSAOVERLAPPED,
                                    LPWSAOVERLAPPED_COMPLETION_ROUTINE);

static sendto_t      real_sendto      = nullptr;
static recvfrom_t    real_recvfrom    = nullptr;
static wsasendto_t   real_wsasendto   = nullptr;
static wsarecvfrom_t real_wsarecvfrom = nullptr;

static int WSAAPI hook_sendto(SOCKET s, const char* buf, int len, int flags,
                              const sockaddr* to, int tolen) {
    log_dgram("SENDTO", s, to, tolen, buf, len);
    sockaddr_in red;
    if (redirect_demangler_dest(to, &red)) {
        logf("REDIR   demangler probe -> server (was :%u)", DEMANGLER_PORT_STD);
        return real_sendto(s, buf, len, flags, reinterpret_cast<sockaddr*>(&red), (int)sizeof(red));
    }
    return real_sendto(s, buf, len, flags, to, tolen);
}

static int WSAAPI hook_recvfrom(SOCKET s, char* buf, int len, int flags,
                                sockaddr* from, int* fromlen) {
    int r = real_recvfrom(s, buf, len, flags, from, fromlen);
    if (r > 0) {
        unredirect_demangler_src(from, fromlen ? *fromlen : 0);
        log_dgram("RECVFROM", s, from, fromlen ? *fromlen : 0, buf, r);
    }
    return r;
}

static int WSAAPI hook_wsasendto(SOCKET s, LPWSABUF bufs, DWORD count, LPDWORD sent,
                                 DWORD flags, const sockaddr* to, int tolen,
                                 LPWSAOVERLAPPED ov, LPWSAOVERLAPPED_COMPLETION_ROUTINE cr) {
    if (count > 0 && bufs) log_dgram("WSASENDTO", s, to, tolen, bufs[0].buf, (int)bufs[0].len);
    sockaddr_in red;
    if (redirect_demangler_dest(to, &red)) {
        logf("REDIR   demangler probe (WSA) -> server (was :%u)", DEMANGLER_PORT_STD);
        return real_wsasendto(s, bufs, count, sent, flags, reinterpret_cast<sockaddr*>(&red),
                              (int)sizeof(red), ov, cr);
    }
    return real_wsasendto(s, bufs, count, sent, flags, to, tolen, ov, cr);
}

static int WSAAPI hook_wsarecvfrom(SOCKET s, LPWSABUF bufs, DWORD count, LPDWORD recvd,
                                   LPDWORD flags, sockaddr* from, LPINT fromlen,
                                   LPWSAOVERLAPPED ov, LPWSAOVERLAPPED_COMPLETION_ROUTINE cr) {
    int r = real_wsarecvfrom(s, bufs, count, recvd, flags, from, fromlen, ov, cr);
    // synchronous completion only; overlapped completes elsewhere
    if (r == 0 && recvd && *recvd > 0 && count > 0 && bufs) {
        unredirect_demangler_src(from, fromlen ? *fromlen : 0);
        log_dgram("WSARECVFROM", s, from, fromlen ? *fromlen : 0, bufs[0].buf, (int)*recvd);
    }
    return r;
}

static void hook_one(const wchar_t* mod, const char* name, void* detour, void** orig) {
    MH_STATUS st = MH_CreateHookApi(mod, name, detour, orig);
    logf("HOOK create %s: %s", name, st == MH_OK ? "ok" : "FAILED");
}

// ---------------------------------------------------------------------------
// Worker: chain-load EA-MITM, then install the WinSock hooks.
// ---------------------------------------------------------------------------
static DWORD WINAPI worker(LPVOID) {
    open_log();
    logf("p2p-hook loaded (Phase 1: observe + demangler redirect). build " __DATE__ " " __TIME__);
    load_redirect_config();

    // Keep EA-MITM's ProtoSSL redirect alive (it must be renamed ea-mitm.dll).
    HMODULE mitm = LoadLibraryW(L"ea-mitm.dll");
    logf("ea-mitm.dll load: %s", mitm ? "ok" : "NOT FOUND (ProtoSSL redirect will be missing)");

    // Ensure ws2_32 is present, then hook its UDP exports.
    LoadLibraryW(L"ws2_32.dll");
    if (MH_Initialize() != MH_OK) {
        logf("MH_Initialize FAILED");
        return 0;
    }
    hook_one(L"ws2_32.dll", "sendto",      reinterpret_cast<void*>(hook_sendto),      reinterpret_cast<void**>(&real_sendto));
    hook_one(L"ws2_32.dll", "recvfrom",    reinterpret_cast<void*>(hook_recvfrom),    reinterpret_cast<void**>(&real_recvfrom));
    hook_one(L"ws2_32.dll", "WSASendTo",   reinterpret_cast<void*>(hook_wsasendto),   reinterpret_cast<void**>(&real_wsasendto));
    hook_one(L"ws2_32.dll", "WSARecvFrom", reinterpret_cast<void*>(hook_wsarecvfrom), reinterpret_cast<void**>(&real_wsarecvfrom));
    logf("hooks enable all: %s", MH_EnableHook(MH_ALL_HOOKS) == MH_OK ? "ok" : "FAILED");
    return 0;
}

// ---------------------------------------------------------------------------
// dinput8.dll proxy export
// ---------------------------------------------------------------------------
extern "C" __declspec(dllexport)
HRESULT WINAPI DirectInput8Create(HINSTANCE inst, DWORD version, REFIID riid,
                                  LPVOID* out, LPUNKNOWN outer) {
    typedef HRESULT (WINAPI *fn_t)(HINSTANCE, DWORD, REFIID, LPVOID*, LPUNKNOWN);
    static fn_t real = nullptr;
    if (!real) {
        wchar_t sys[MAX_PATH] = {0};
        GetSystemDirectoryW(sys, MAX_PATH);
        wcsncat(sys, L"\\dinput8.dll", MAX_PATH - wcslen(sys) - 1);
        HMODULE m = LoadLibraryW(sys);
        if (m) real = reinterpret_cast<fn_t>(GetProcAddress(m, "DirectInput8Create"));
    }
    if (!real) return E_FAIL;
    return real(inst, version, riid, out, outer);
}

BOOL WINAPI DllMain(HINSTANCE inst, DWORD reason, LPVOID) {
    if (reason == DLL_PROCESS_ATTACH) {
        g_self = inst;
        DisableThreadLibraryCalls(inst);
        CreateThread(nullptr, 0, worker, nullptr, 0, nullptr);
    }
    return TRUE;
}
