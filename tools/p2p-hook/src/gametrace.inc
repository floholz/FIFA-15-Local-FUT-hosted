// ---------------------------------------------------------------------------
// Phase 2: game-code trace (trace_connapi=on in p2p-hook.ini)
//
// Hooks BlazeSDK ConnApiAdapter methods and DirtySDK ConnApi entry points
// inside fifa15.exe and logs each call (args, return value, caller address).
// Addresses were recovered from a runtime dump of this fifa15.exe build (the
// code is encrypted on disk and decrypted at startup), so every hook waits
// until its target's first 8 bytes match the expected decrypted prologue and
// is skipped on any other build.
// ---------------------------------------------------------------------------
struct TraceTarget {
    uintptr_t   addr;
    const char* name;
    const char* prologue_hex;   // first 8 bytes once decrypted
    int         kind;           // 0 generic, 1 ConnApiControl, 2 ConnApiOnline, 3 ListenerDispatch
    bool        hooked;
    long        calls;
};

static TraceTarget g_targets[] = {
    {0x14476d2f4, "ListenerDispatch",        "48895c240848896c", 3},
    {0x14478326a, "Adapter_vt10",            "48895c2408574883", 0},
    {0x14478329a, "Adapter_vtf8",            "48895c2408574883", 0},
    {0x14478a1b3, "Adapter_goOnline",        "564883ec404889ce", 0},
    {0x14478ca3e, "Adapter_vt68",            "4d85c0743e48895c", 0},
    {0x14478e82f, "Adapter_vtc8",            "48895c2418555641", 0},
    {0x144792a59, "Adapter_vt60_connectToEndpoint", "4d85c0743e48895c", 0},
    {0x144799a97, "Adapter_vt18",            "48897c2418415648", 0},
    {0x14479ad0f, "Adapter_vt80",            "48895c2410564883", 0},
    {0x14479afde, "Adapter_vt78",            "4d85c00f84d80000", 0},
    {0x14479d64e, "Adapter_vt90",            "4883ec28e84f2600", 0},
    {0x1447a502a, "Adapter_vt38",            "488b81d002000048", 0},
    {0x1447a5049, "Adapter_vt40",            "488b81d002000048", 0},
    {0x1447a6b27, "Adapter_vtc0",            "48895c2420555657", 0},
    {0x1447a9183, "Adapter_vt48",            "488d81d0030000c3", 0},
    {0x1447a9fc1, "Adapter_vt08_initialize", "534883ec2080b924", 0},
    {0x1447aa040, "Adapter_vtf0",            "488b49204885c90f", 0},
    {0x1447ac15d, "Adapter_vt58_initNetworkMesh", "48895c240848896c", 0},
    {0x1447ac24d, "Adapter_initMeshInternal","48895c2408574883", 0},
    {0x1447ad268, "Adapter_vt20_idle",       "48895c241048896c", 0},
    {0x1447b096f, "Adapter_vt28",            "4883ec28e82ef3fe", 0},
    {0x1447b0993, "Adapter_vt30",            "0fb681a9040000c3", 0},
    {0x1447b67fa, "Adapter_vtd8",            "48895c2408574883", 0},
    {0x1447b683a, "Adapter_vtd0",            "48895c2408488974", 0},
    {0x1447ce506, "Adapter_vta0",            "48895c2408574883", 0},
    {0x1447ce556, "Adapter_vt98",            "48895c2408488974", 0},
    {0x1447d06da, "Adapter_vta8",            "534883ec20e8c2f5", 0},
    {0x1447d3180, "Adapter_vtb8",            "48895c2420555641", 0},
    {0x1447d6be7, "Adapter_vtb0",            "534883ec20410fb6", 0},
    {0x1447d7dab, "Adapter_createConnApi",   "48895c2408488974", 0},
    {0x1447d93fb, "Adapter_setupTunnel",     "4989e3574881ec90", 0},
    {0x1447db03f, "Adapter_vt88",            "48895c2408574883", 0},
    {0x1447dd9c1, "Adapter_vte0",            "4883ec28e8dc22fc", 0},
    {0x1448dce89, "ConnApiOnline",           "48895c2408574883", 2},
    {0x1448dcee9, "ConnApiControl",          "48895c2408488974", 1},
    {0x1448dd5fa, "ConnApiCreate2",          "48895c240848896c", 0},
    {0x1448f49fb, "ProtoTunnelControl",      "48895c2420555657", 1},
    {0x1448f4d7a, "ProtoTunnelCreate",       "48895c2410555657", 0},
};
constexpr int N_TARGETS = (int)(sizeof(g_targets) / sizeof(g_targets[0]));
static_assert(N_TARGETS <= 64, "too many trace targets");

typedef uint64_t (*fn8_t)(uint64_t, uint64_t, uint64_t, uint64_t,
                          uint64_t, uint64_t, uint64_t, uint64_t);
static fn8_t g_orig[64];

static bool safe_read(const void* p, void* out, size_t n) {
    SIZE_T got = 0;
    return ReadProcessMemory(GetCurrentProcess(), p, out, n, &got) && got == n;
}

static void fourcc(uint64_t v, char out[5]) {
    for (int i = 0; i < 4; ++i) {
        char c = (char)((v >> (24 - 8 * i)) & 0xFF);
        out[i] = (c >= 32 && c < 127) ? c : '.';
    }
    out[4] = 0;
}

static void safe_str(uint64_t p, char* out, size_t n) {
    out[0] = 0;
    if (!p) { snprintf(out, n, "(null)"); return; }
    char tmp[64] = {0};
    if (!safe_read((const void*)p, tmp, sizeof(tmp) - 1)) { snprintf(out, n, "(unreadable)"); return; }
    size_t i = 0;
    for (; i < sizeof(tmp) - 1 && tmp[i]; ++i) if (tmp[i] < 32 || tmp[i] > 126) tmp[i] = '?';
    tmp[i] = 0;
    snprintf(out, n, "\"%s\"", tmp);
}


// Hex-dump `n` bytes at `p` into the log (safe against unreadable memory).
static void log_hex(const char* label, uint64_t p, size_t n) {
    static unsigned char buf[0x400];
    if (n > sizeof(buf)) n = sizeof(buf);
    if (!safe_read((const void*)p, buf, n)) { logf("%s @%llx unreadable", label, (unsigned long long)p); return; }
    static char hex[0x801];
    for (size_t i = 0; i < n; ++i) { static const char* H = "0123456789abcdef"; hex[2*i] = H[buf[i] >> 4]; hex[2*i+1] = H[buf[i] & 15]; }
    hex[2*n] = 0;
    logf("%s @%llx len=%u hex=%s", label, (unsigned long long)p, (unsigned)n, hex);
}

// After ConnApiOnline: watch ConnApi's internal client entries (ref+0x718,
// stride 0x3d8 in this build) to see the peer address/status evolve.
static DWORD WINAPI connapi_watch(LPVOID ref) {
    static const int when_ms[] = {500, 2000, 5000, 10000, 20000};
    int prev = 0;
    for (int w : when_ms) {
        Sleep(w - prev); prev = w;
        char lbl[64];
        for (int i = 0; i < 2; ++i) {
            snprintf(lbl, sizeof(lbl), "TRACE connapi+%dms client[%d]", w, i);
            log_hex(lbl, (uint64_t)ref + 0x718 + (uint64_t)i * 0x3d8, 0x200);
        }
        snprintf(lbl, sizeof(lbl), "TRACE connapi+%dms ref", w);
        log_hex(lbl, (uint64_t)ref, 0x340);
    }
    return 0;
}

static bool trace_should_log(TraceTarget& t) {
    long c = InterlockedIncrement(&t.calls);
    return c <= 40 || (c % 2000) == 0;
}

template <int N>
static uint64_t trace_detour(uint64_t a, uint64_t b, uint64_t c, uint64_t d,
                             uint64_t e, uint64_t f, uint64_t g, uint64_t h) {
    TraceTarget& t = g_targets[N];
    void* ra = __builtin_return_address(0);
    bool lg = trace_should_log(t);
    if (lg) {
        if (t.kind == 1) {
            char cc[5]; fourcc(b, cc);
            char sv[96]; safe_str(e, sv, sizeof(sv));
            logf("TRACE %s(ref=%llx, '%s', %lld, %lld, p=%llx %s) from %p #%ld",
                 t.name, (unsigned long long)a, cc, (long long)(int32_t)c, (long long)(int32_t)d,
                 (unsigned long long)e, e ? sv : "", ra, t.calls);
        } else if (t.kind == 2) {
            // FIFA's ConnApiOnline(ref, pClientList, iNumClients, iGameHost, iVoipHost, uGameId, iTopology?)
            logf("TRACE %s(ref=%llx, list=%llx, nclients=%lld, host=%lld, voiphost=%lld, a6=%llx, a7=%lld) from %p",
                 t.name, (unsigned long long)a, (unsigned long long)b, (long long)(int32_t)c, (long long)(int32_t)d,
                 (long long)(int32_t)e, (unsigned long long)(uint32_t)f, (long long)(int32_t)g, ra);
            int nc = (int32_t)c; if (nc < 0) nc = 0; if (nc > 4) nc = 4;
            for (int i = 0; i < nc; ++i) {
                char lbl[48]; snprintf(lbl, sizeof(lbl), "TRACE   clientlist[%d]", i);
                log_hex(lbl, b + (uint64_t)i * 0x38c, 0x38c);
            }
        } else if (t.kind == 3) {
            logf("TRACE %s(list=%llx, fn=%llx, a3=%llx, err=%lld) from %p #%ld",
                 t.name, (unsigned long long)a, (unsigned long long)b, (unsigned long long)c,
                 (long long)(int32_t)d, ra, t.calls);
        } else {
            logf("TRACE %s(%llx, %llx, %llx, %llx) from %p #%ld", t.name,
                 (unsigned long long)a, (unsigned long long)b, (unsigned long long)c,
                 (unsigned long long)d, ra, t.calls);
        }
    }
    uint64_t r = g_orig[N](a, b, c, d, e, f, g, h);
    if (t.kind == 2) CreateThread(nullptr, 0, connapi_watch, (LPVOID)a, 0, nullptr);
    if (lg && t.kind != 3) logf("TRACE   %s -> %llx", t.name, (unsigned long long)r);
    return r;
}

template <int N> struct DetourTable {
    static void fill(void** out) { out[N] = (void*)&trace_detour<N>; DetourTable<N - 1>::fill(out); }
};
template <> struct DetourTable<-1> { static void fill(void**) {} };


static int hexval(char c) { return c <= '9' ? c - '0' : (c | 0x20) - 'a' + 10; }

static DWORD WINAPI trace_installer(LPVOID) {
    void* detours[64] = {0};
    DetourTable<N_TARGETS - 1>::fill(detours);
    int remaining = N_TARGETS;
    logf("trace: waiting for %d game-code targets to decrypt", remaining);
    for (int iter = 0; iter < 6000 && remaining > 0; ++iter) {   // ~10 min max
        for (int i = 0; i < N_TARGETS; ++i) {
            TraceTarget& t = g_targets[i];
            if (t.hooked) continue;
            unsigned char cur[8], want[8];
            for (int k = 0; k < 8; ++k) want[k] = (unsigned char)(hexval(t.prologue_hex[2*k]) * 16 + hexval(t.prologue_hex[2*k+1]));
            if (!safe_read((const void*)t.addr, cur, 8) || memcmp(cur, want, 8) != 0) continue;
            MH_STATUS s1 = MH_CreateHook((void*)t.addr, detours[i], (void**)&g_orig[i]);
            MH_STATUS s2 = s1 == MH_OK ? MH_EnableHook((void*)t.addr) : s1;
            t.hooked = true; --remaining;
            logf("trace: hook %s @%llx: %s", t.name, (unsigned long long)t.addr,
                 s2 == MH_OK ? "ok" : MH_StatusToString(s2));
        }
        Sleep(100);
    }
    if (remaining) {
        for (int i = 0; i < N_TARGETS; ++i)
            if (!g_targets[i].hooked) logf("trace: %s @%llx never matched (other exe build?)",
                                           g_targets[i].name, (unsigned long long)g_targets[i].addr);
    }
    logf("trace: installer done, %d targets unhooked", remaining);
    return 0;
}
