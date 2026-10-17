package lsx

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"time"
)

// Identity is what the stub hands FIFA: the player logged in on the hosted server.
type Identity struct {
	Name      string
	NucleusID int64
	PersonaID int64
	Token     string
}

// Server is a running LSX listener.
type Server struct {
	ln     net.Listener
	log    *log.Logger
	mu     sync.RWMutex
	id     Identity
	closed chan struct{}
}

// ListenerPresent reports whether something already accepts TCP on addr
// (EA App/Origin may own 3216 on some Windows installs).
func ListenerPresent(addr string, timeout time.Duration) bool {
	c, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// Listen starts the stub on addr (normally 127.0.0.1:3216).
func Listen(addr string, id Identity, logger *log.Logger) (*Server, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	s := &Server{ln: ln, log: logger, id: id, closed: make(chan struct{})}
	go s.serve()
	return s, nil
}

// SetIdentity updates the player answered from now on (after a re-login).
func (s *Server) SetIdentity(id Identity) {
	s.mu.Lock()
	s.id = id
	s.mu.Unlock()
}

func (s *Server) identity() Identity {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.id
}

func (s *Server) Addr() string { return s.ln.Addr().String() }

func (s *Server) Close() error {
	select {
	case <-s.closed:
	default:
		close(s.closed)
	}
	return s.ln.Close()
}

func (s *Server) serve() {
	for {
		c, err := s.ln.Accept()
		if err != nil {
			select {
			case <-s.closed:
				return
			default:
			}
			s.log.Printf("LSX accept: %v", err)
			time.Sleep(100 * time.Millisecond)
			continue
		}
		go s.handle(c)
	}
}

func readFrame(r *bufio.Reader) ([]byte, error) {
	b, err := r.ReadBytes(0)
	if err != nil {
		return nil, err
	}
	return b[:len(b)-1], nil
}

func (s *Server) handle(c net.Conn) {
	defer c.Close()
	// OriginSDK parses each recv() as one message: two frames arriving in one
	// read trip a rapidxml assertion and the game quits. Go disables Nagle by
	// default; Python (the reference) leaves it on, which keeps frames apart.
	if tc, ok := c.(*net.TCPConn); ok {
		tc.SetNoDelay(false)
	}
	peer := c.RemoteAddr().String()
	s.log.Printf("LSX client connected from %s", peer)
	r := bufio.NewReaderSize(c, 65536)

	// 1) The server speaks first.
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		s.log.Printf("LSX: no randomness: %v", err)
		return
	}
	challengeKey := strings.ToUpper(hex.EncodeToString(nonce[:]))
	challenge := `<LSX><Event sender="EALS"><Challenge key="` + challengeKey + `" version="3" build="10.6.1.8" /></Event></LSX>`
	c.SetDeadline(time.Now().Add(30 * time.Second))
	if _, err := c.Write(append([]byte(challenge), 0)); err != nil {
		return
	}

	// 2) ChallengeResponse, still plaintext.
	frame, err := readFrame(r)
	if err != nil {
		s.log.Printf("LSX client disconnected before ChallengeResponse")
		return
	}
	req := parseRequest(string(frame))
	if req.Name != "ChallengeResponse" || req.Attrs == nil {
		s.log.Printf("LSX expected ChallengeResponse, got %s", req.Name)
		return
	}
	primary := Key(0)
	response, clientKey := req.Attrs["response"], req.Attrs["key"]
	if expected := EncryptHex(primary, challengeKey); !strings.EqualFold(response, expected) {
		s.log.Printf("LSX challenge response did not validate; expected=%s got=%s", expected, response)
		return
	}
	if clientKey == "" {
		s.log.Printf("LSX ChallengeResponse is missing the client key")
		return
	}
	accept := EncryptHex(primary, clientKey)
	version := req.Attrs["version"]
	if version == "" {
		version = "3"
	}
	var key []byte
	switch version {
	case "2":
		key = primary
	case "3":
		key = Key(SessionSeed(accept))
	default:
		s.log.Printf("LSX unsupported protocol version %s", version)
		return
	}
	accepted := envelope(req.ID, `<ChallengeAccepted response="`+attr(accept)+`" />`, "EALS")
	if _, err := c.Write(append([]byte(accepted), 0)); err != nil {
		return
	}
	s.log.Printf("LSX challenge accepted for %s (protocol %s)", peer, version)
	// Let the game read ChallengeAccepted on its own before the next frame.
	time.Sleep(50 * time.Millisecond)

	send := func(xmlText string) error {
		_, err := c.Write(append([]byte(EncryptHex(key, xmlText)), 0))
		return err
	}
	// FIFA checks the login state before it attempts Blaze authentication.
	if err := send(`<LSX><Event sender="EbisuSDK"><Login IsLoggedIn="true" UserIndex="0" LoginReasonCode="ALREADY_ONLINE" /></Event></LSX>`); err != nil {
		return
	}

	// 3) Encrypted request/response loop. No idle timeout: FIFA sends nothing
	// during a match, and a timeout here quits the game mid-match.
	c.SetDeadline(time.Time{})
	for {
		frame, err := readFrame(r)
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
				s.log.Printf("LSX %s: %v", peer, err)
			}
			s.log.Printf("LSX client %s disconnected", peer)
			return
		}
		if len(frame) == 0 {
			continue
		}
		raw, err := hex.DecodeString(string(frame))
		if err != nil {
			s.log.Printf("LSX frame is not hex: %v", err)
			return
		}
		plain, err := Decrypt(key, raw)
		if err != nil {
			s.log.Printf("LSX frame could not be decrypted: %v", err)
			return
		}
		reply, name := Respond(string(plain), s.identity(), time.Now())
		s.log.Printf("LSX %s", name)
		if err := send(reply); err != nil {
			return
		}
	}
}

type request struct {
	ID    string
	Name  string
	Attrs map[string]string // attributes of the operation element; nil when absent
}

// parseRequest finds <Request id=..> and its first child element.
func parseRequest(text string) request {
	req := request{ID: "0", Name: "Unknown"}
	d := xml.NewDecoder(strings.NewReader(text))
	inRequest := false
	for {
		tok, err := d.Token()
		if err != nil {
			return req
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if !inRequest {
			if start.Name.Local == "Request" {
				inRequest = true
				for _, a := range start.Attr {
					if a.Name.Local == "id" {
						req.ID = a.Value
					}
				}
			}
			continue
		}
		req.Name = start.Name.Local
		req.Attrs = map[string]string{}
		for _, a := range start.Attr {
			req.Attrs[a.Name.Local] = a.Value
		}
		return req
	}
}

// attr escapes like Python's html.escape(quote=True).
var attrEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#x27;")

func attr(v string) string { return attrEscaper.Replace(v) }

func envelope(id, inner, sender string) string {
	return `<LSX><Response id="` + attr(id) + `" sender="` + attr(sender) + `">` + inner + `</Response></LSX>`
}

var configServices = [][2]string{
	{"EbisuSDK", "SDK"}, {"EbisuSDK", "PROFILE"}, {"XMPP", "PRESENCE"}, {"XMPP", "FRIENDS"},
	{"Commerce", "COMMERCE"}, {"EbisuSDK", "RECENTPLAYER"}, {"EbisuSDK", "IGO"}, {"EbisuSDK", "MISC"},
	{"EALS", "LOGIN"}, {"EbisuSDK", "UTILITY"}, {"XMPP", "XMPP"}, {"XMPP", "CHAT"},
	{"EbisuSDK", "IGO_EVENT"}, {"EALS", "EALS_EVENTS"}, {"EbisuSDK", "LOGIN_EVENT"}, {"XMPP", "INVITE_EVENT"},
	{"EbisuSDK", "PROFILE_EVENT"}, {"XMPP", "PRESENCE_EVENT"}, {"XMPP", "FRIENDS_EVENT"}, {"Commerce", "COMMERCE_EVENT"},
	{"XMPP", "CHAT_EVENT"}, {"EbisuSDK", "DOWNLOAD_EVENT"}, {"EbisuSDK", "PERMISSION"}, {"EbisuSDK", "RESOURCES"},
	{"EbisuSDK", "BLOCKED_USERS"}, {"EbisuSDK", "BLOCKED_USER_EVENT"}, {"EbisuSDK", "GET_USERID"}, {"EbisuSDK", "ONLINE_STATUS_EVENT"},
	{"EbisuSDK", "ACHIEVEMENT"}, {"EbisuSDK", "ACHIEVEMENT_EVENT"}, {"EbisuSDK", "BROADCAST_EVENT"}, {"PI", "PROGRESSIVE_INSTALLATION"},
	{"PI", "PROGRESSIVE_INSTALLATION_EVENT"}, {"EbisuSDK", "CONTENT"},
}

var settings = map[string]string{
	"LANGUAGE": "en_US", "ENVIRONMENT": "production", "IS_IGO_AVAILABLE": "false",
	"IS_IGO_ENABLED": "false", "IS_TELEMETRY_ENABLED": "false", "IS_MANUAL_OFFLINE": "false",
}

// Without UPTODATE=true FIFA shows the mandatory-update dialog.
var gameInfo = map[string]string{
	"UPTODATE": "true", "LANGUAGES": "en_US,pt_PT,fr_FR,de_DE,es_ES,it_IT", "FREETRIAL": "false",
	"EXPIRATION": "0000-00-00T00:00:00", "EXPIRATION_DURATION": "0", "INSTALLED_VERSION": "1.0.0.0",
	"INSTALLED_LANGUAGE": "en_US", "AVAILABLE_VERSION": "1.0.0.0", "DISPLAY_NAME": "EA SPORTS FIFA 15",
	"FULLGAME_PURCHASED": "true", "FULLGAME_IS_RELEASED": "true", "FULLGAME_RELEASE_DATE": "2014-09-25T00:00:00",
	"MAX_GROUP_SIZE": "22", "ENTITLEMENT_SOURCE": "LOCAL",
}

// Respond answers one decrypted LSX request; it returns the reply XML and the request name.
func Respond(requestXML string, id Identity, now time.Time) (string, string) {
	req := parseRequest(requestXML)
	token := id.Token
	var inner string
	switch req.Name {
	case "GetConfig":
		var b strings.Builder
		b.WriteString("<GetConfigResponse>")
		for _, s := range configServices {
			fmt.Fprintf(&b, `<Service Name="%s" Facility="%s" />`, s[0], s[1])
		}
		b.WriteString("</GetConfigResponse>")
		inner = b.String()
	case "GetSettings":
		inner = `<GetSettingsResponse Language="en_US" Environment="production" IsIGOAvailable="false" IsIGOEnabled="false" IsTelemetryEnabled="false" IsManualOffline="false" />`
	case "GetSetting":
		inner = `<GetSettingResponse Setting="` + attr(settings[req.Attrs["SettingId"]]) + `" />`
	case "GetInternetConnectedState":
		inner = `<InternetConnectedState connected="1" />`
	case "GetUtcTime":
		inner = `<GetUtcTimeResponse Time="` + now.UTC().Format("2006-01-02T15:04:05Z") + `" />`
	case "GetAuthToken":
		inner = `<AuthToken value="` + attr(orDefault(token, "LOCAL-FUT15-ACCESS")) + `" />`
	case "GetAuthCode":
		inner = `<AuthCode value="` + attr(orDefault(token, "LOCAL-FUT15-AUTH-CODE")) + `" />`
	case "GetProfile":
		inner = fmt.Sprintf(`<GetProfileResponse UserIndex="0" UserId="%d" PersonaId="%d" Persona="%s" AvatarId="" `+
			`Country="PT" IsUnderAge="false" IsSubscriber="false" IsTrialSubscriber="false" SubscriberLevel="0" GeoCountry="PT" `+
			`CommerceCountry="PT" CommerceCurrency="EUR" IsSteamSubscriber="false" />`, id.NucleusID, id.PersonaID, attr(id.Name))
	case "GetBlockList":
		inner = `<GetBlockListResponse />`
	case "QueryFriends":
		inner = `<QueryFriendsResponse></QueryFriendsResponse>`
	case "QueryAreFriends":
		inner = `<QueryAreFriendsResponse />`
	case "QueryContent":
		inner = `<QueryContentResponse />`
	case "QueryEntitlements":
		inner = `<QueryEntitlementsResponse />`
	case "GetPresence":
		inner = `<GetPresenceResponse></GetPresenceResponse>`
	case "QueryPresence":
		inner = `<QueryPresenceResponse></QueryPresenceResponse>`
	case "GetWalletBalance":
		inner = `<GetWalletBalanceResponse Balance="0" Currency="EUR" />`
	case "GetGameInfo":
		inner = `<GetGameInfoResponse GameInfo="` + attr(gameInfo[strings.ToUpper(req.Attrs["GameInfoId"])]) + `" />`
	case "GetAllGameInfo":
		inner = `<GetAllGameInfoResponse UpToDate="true" Languages="en_US,pt_PT,fr_FR,de_DE,es_ES,it_IT" FreeTrial="false" ` +
			`FullGamePurchased="true" FullGameReleased="true" FullGameReleaseDate="2014-09-25T00:00:00" Expiration="" ` +
			`SystemTime="` + now.UTC().Format("2006-01-02T15:04:05") + `" HasExpiration="false" InstalledVersion="1.0.0.0" ` +
			`InstalledLanguage="en_US" AvailableVersion="1.0.0.0" DisplayName="EA SPORTS FIFA 15" MaxGroupSize="22" EntitlementSource="LOCAL" />`
	case "RequestLicense":
		inner = `<RequestLicenseResponse />`
	default:
		inner = `<ErrorSuccess Code="0" Description="Local FUT success" />`
	}
	return envelope(req.ID, inner, "EbisuSDK"), req.Name
}

func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return v
}
