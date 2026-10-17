package lsx

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

// testdata/python_vectors.json is generated from payload/localfut15/server.py
// (OriginCrypto and _lsx_xml_response), the reference implementation.
type vectors struct {
	Keys []struct {
		Seed uint32 `json:"seed"`
		Key  string `json:"key"`
	} `json:"keys"`
	Enc []struct {
		Seed uint32 `json:"seed"`
		Text string `json:"text"`
		Hex  string `json:"hex"`
	} `json:"enc"`
	Replies []struct {
		Req   string `json:"req"`
		Reply string `json:"reply"`
	} `json:"replies"`
}

var testID = Identity{Name: `Fl<o>&'"`, NucleusID: 1000000123, PersonaID: 1000000456, Token: "tok-abc"}

func loadVectors(t *testing.T) vectors {
	raw, err := os.ReadFile("testdata/python_vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var v vectors
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestKeysMatchPython(t *testing.T) {
	for _, k := range loadVectors(t).Keys {
		if got := hex.EncodeToString(Key(k.Seed)); got != k.Key {
			t.Errorf("seed %d: key %s, python %s", k.Seed, got, k.Key)
		}
	}
}

func TestEncryptMatchesPython(t *testing.T) {
	for _, e := range loadVectors(t).Enc {
		got := EncryptHex(Key(e.Seed), e.Text)
		if got != e.Hex {
			t.Errorf("seed %d %q: %s, python %s", e.Seed, e.Text, got, e.Hex)
		}
		raw, _ := hex.DecodeString(got)
		plain, err := Decrypt(Key(e.Seed), raw)
		if err != nil || string(plain) != e.Text {
			t.Errorf("round trip %q: %q %v", e.Text, plain, err)
		}
	}
}

func TestRepliesMatchPython(t *testing.T) {
	for _, r := range loadVectors(t).Replies {
		got, _ := Respond(r.Req, testID, time.Now())
		if got != r.Reply {
			t.Errorf("request %s\n go:     %s\n python: %s", r.Req, got, r.Reply)
		}
	}
}

// TestHandshake plays the OriginSDK client side against the stub.
func TestHandshake(t *testing.T) {
	srv, err := Listen("127.0.0.1:0", testID, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	c, err := net.Dial("tcp", srv.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	r := bufio.NewReader(c)

	frame, err := readFrame(r)
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`Challenge key="([0-9A-F]{32})"`).FindStringSubmatch(string(frame))
	if m == nil {
		t.Fatalf("no challenge: %s", frame)
	}
	primary := Key(0)
	clientKey := "0123456789abcdef0123456789abcdef"
	resp := `<LSX><Request recipient="EALS" id="1"><ChallengeResponse response="` + EncryptHex(primary, m[1]) +
		`" key="` + clientKey + `" version="3"><ContentId>1</ContentId></ChallengeResponse></Request></LSX>`
	c.Write(append([]byte(resp), 0))

	frame, err = readFrame(r)
	if err != nil {
		t.Fatal(err)
	}
	want := EncryptHex(primary, clientKey)
	if !strings.Contains(string(frame), `<ChallengeAccepted response="`+want+`" />`) {
		t.Fatalf("bad accept: %s", frame)
	}
	key := Key(SessionSeed(want))

	recv := func() string {
		f, err := readFrame(r)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := hex.DecodeString(string(f))
		plain, err := Decrypt(key, raw)
		if err != nil {
			t.Fatal(err)
		}
		return string(plain)
	}
	if login := recv(); !strings.Contains(login, `IsLoggedIn="true"`) {
		t.Fatalf("no login event: %s", login)
	}
	c.Write(append([]byte(EncryptHex(key, `<LSX><Request id="5"><GetProfile/></Request></LSX>`)), 0))
	if got := recv(); !strings.Contains(got, `PersonaId="1000000456"`) || !strings.Contains(got, `id="5"`) {
		t.Fatalf("bad profile reply: %s", got)
	}
}
