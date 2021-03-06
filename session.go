package revel

import (
	"WEB/WebCore/Module/ssdb"
	"code.google.com/p/go-uuid/uuid"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// A signed cookie (and thus limited to 4kb in size).
// Restriction: Keys may not have a colon in them.
type Session map[string]string

const (
	SESSION_ID_KEY = "_ID"
	TIMESTAMP_KEY  = "_TS"
)

// expireAfterDuration is the time to live, in seconds, of a session cookie.
// It may be specified in config as "session.expires". Values greater than 0
// set a persistent cookie with a time to live as specified, and the value 0
// sets a session cookie.
var expireAfterDuration time.Duration

func init() {
	// Set expireAfterDuration, default to 30 days if no value in config
	OnAppStart(func() {
		var err error
		if expiresString, ok := Config.String("session.expires"); !ok {
			expireAfterDuration = 30 * 24 * time.Hour
		} else if expiresString == "session" {
			expireAfterDuration = 0
		} else if expireAfterDuration, err = time.ParseDuration(expiresString); err != nil {
			panic(fmt.Errorf("session.expires invalid: %s", err))
		}
	})
}

// Id retrieves from the cookie or creates a time-based UUID identifying this
// session.
func (s Session) Id() string {
	if sessionIdStr, ok := s[SESSION_ID_KEY]; ok {
		return sessionIdStr
	}
	/*
		buffer := make([]byte, 32)
		if _, err := rand.Read(buffer); err != nil {
			panic(err)
		}
		s[SESSION_ID_KEY] = hex.EncodeToString(buffer)
	*/
	uid := uuid.NewUUID()
	str := base64.StdEncoding.EncodeToString([]byte(uid))
	s[SESSION_ID_KEY] = str[:len(str)-2]

	return s[SESSION_ID_KEY]
}

// getExpiration return a time.Time with the session's expiration date.
// If previous session has set to "session", remain it
func (s Session) getExpiration() time.Time {
	if expireAfterDuration == 0 || s[TIMESTAMP_KEY] == "session" {
		// Expire after closing browser
		return time.Time{}
	}
	return time.Now().Add(expireAfterDuration)
}

// Cookie returns an http.Cookie containing the signed session.
func (s Session) Cookie() *http.Cookie {
	var sessionValue string
	ts := s.getExpiration()
	s[TIMESTAMP_KEY] = getSessionExpirationCookie(ts)
	for key, value := range s {
		if strings.ContainsAny(key, ":\x00") {
			panic("Session keys may not have colons or null bytes")
		}
		if strings.Contains(value, "\x00") {
			panic("Session values may not have null bytes")
		}
		sessionValue += "\x00" + key + ":" + value + "\x00"
	}

	sessionData := url.QueryEscape(sessionValue)
	return &http.Cookie{
		Name:     CookiePrefix + "_SESSION",
		Value:    Sign(sessionData) + "-" + sessionData,
		Domain:   CookieDomain,
		Path:     "/",
		HttpOnly: CookieHttpOnly,
		Secure:   CookieSecure,
		Expires:  ts.UTC(),
	}
}

// sessionTimeoutExpiredOrMissing returns a boolean of whether the session
// cookie is either not present or present but beyond its time to live; i.e.,
// whether there is not a valid session.
func sessionTimeoutExpiredOrMissing(session Session) bool {
	if exp, present := session[TIMESTAMP_KEY]; !present {
		return true
	} else if exp == "session" {
		return false
	} else if expInt, _ := strconv.Atoi(exp); int64(expInt) < time.Now().Unix() {
		return true
	}
	return false
}

// GetSessionFromCookie returns a Session struct pulled from the signed
// session cookie.
func GetSessionFromCookie(cookie *http.Cookie) Session {
	session := make(Session)

	// Separate the data from the signature.
	hyphen := strings.Index(cookie.Value, "-")
	if hyphen == -1 || hyphen >= len(cookie.Value)-1 {
		return session
	}
	sig, data := cookie.Value[:hyphen], cookie.Value[hyphen+1:]

	// Verify the signature.
	if !Verify(data, sig) {
		INFO.Println("Session cookie signature failed")
		return session
	}

	ParseKeyValueCookie(data, func(key, val string) {
		session[key] = val
	})

	if sessionTimeoutExpiredOrMissing(session) {
		session = make(Session)
	}

	return session
}

// SessionFilter is a Revel Filter that retrieves and sets the session cookie.
// Within Revel, it is available as a Session attribute on Controller instances.
// The name of the Session cookie is set as CookiePrefix + "_SESSION".
func SessionFilter(c *Controller, fc []Filter) {
	c.Session = restoreSession(c.Request.Request)
	sessionWasEmpty := len(c.Session) == 0

	// Make session vars available in templates as {{.session.xyz}}
	c.RenderArgs["session"] = c.Session

	fc[0](c, fc[1:])

	// Store the signed session if it could have changed.
	if len(c.Session) > 0 || !sessionWasEmpty {
		c.SetCookie(c.Session.Cookie())
	}
}

// restoreSession returns either the current session, retrieved from the
// session cookie, or a new session.
func restoreSession(req *http.Request) Session {
	cookie, err := req.Cookie(CookiePrefix + "_SESSION")
	if err != nil {
		return make(Session)
	} else {
		return GetSessionFromCookie(cookie)
	}
}

// getSessionExpirationCookie retrieves the cookie's time to live as a
// string of either the number of seconds, for a persistent cookie, or
// "session".
func getSessionExpirationCookie(t time.Time) string {
	if t.IsZero() {
		return "session"
	}
	return strconv.FormatInt(t.Unix(), 10)
}

// SetNoExpiration sets session to expire when browser session ends
func (s Session) SetNoExpiration() {
	s[TIMESTAMP_KEY] = "session"
}

// SetDefaultExpiration sets session to expire after default duration
func (s Session) SetDefaultExpiration() {
	delete(s, TIMESTAMP_KEY)
}

/*##################################小白#########################################*/

func (s Session) Start() {
	if s["ENV_START"] == "1" {
		return
	}
	_, session_ok := s[SESSION_ID_KEY]
	if session_ok == true {
		seesionValue := SessionGet(s[SESSION_ID_KEY])
		ParseKeyValueCookie(seesionValue, func(key, val string) {
			s[key] = val
		})
		s["ENV_START"] = "1"
	}
}

func (s Session) Save() {
	delete(s, "ENV_START")
	seesionValue := ""
	for key, value := range s {
		if key == SESSION_ID_KEY {
			continue
		}
		if strings.ContainsAny(key, ":\x00") {
			panic("Session keys may not have colons or null bytes")
		}
		if strings.Contains(value, "\x00") {
			panic("Session values may not have null bytes")
		}
		seesionValue += "\x00" + key + ":" + value + "\x00"
	}

	SessionSet(s[SESSION_ID_KEY], seesionValue)
}

func SessionFilterNew(c *Controller, fc []Filter) {
	c.Session = restoreSessionNew(c.Request.Request)
	sessionWasEmpty := len(c.Session) == 0

	// Make session vars available in templates as {{.session.xyz}}
	c.RenderArgs["session"] = c.Session

	fc[0](c, fc[1:])

	if len(c.Session) == 0 {
		c.Session.Id()
	}

	// Store the signed session if it could have changed.
	if len(c.Session) > 0 || !sessionWasEmpty {
		//c.SetCookie(c.Session.cookie())
		var cookiesValue string
		ts := time.Now().Add(24 * time.Hour)
		//时间加上session_id生成id
		cookiesValue += "\x00" + TIMESTAMP_KEY + ":" + getSessionExpirationCookie(ts) + "\x00"
		cookiesValue += "\x00" + SESSION_ID_KEY + ":" + c.Session.Id() + "\x00"

		var host = c.Request.Host
		if strings.Count(host, ".") > 1 {
			host = host[strings.Index(host, ".")+1:]
		}

		cookiesData := url.QueryEscape(cookiesValue)
		c.SetCookie(&http.Cookie{
			Name:     "_S",
			Domain:   host,
			Value:    cookiesData,
			Path:     "/",
			HttpOnly: false,
			Secure:   false,
			Expires:  ts.UTC(),
		})
	}
}

func SessionDel(Id string) {
	con, err := ssdb.Connect(session_ip, 6379)
	if err != nil {
		panic(err)
	}
	_, err = con.Do("del", Id)
	if err != nil {
		panic(err)
	}
	//return num
	con.Close()
}

var session_ip string
var ssdbkey string

func InitSession() {
	var err bool
	session_ip, err = Config.String("session_ip")
	if err != true {
		panic("无法初始化session_ip")
	} else {
		fmt.Println("初始化session_ip成功")
	}

	ssdbkey, err = Config.String("ssdbkey")
	if err != true {
		panic("无法初始化ssdbkey")
	} else {
		fmt.Println("初始化ssdbkey成功")
	}
}

//根据hash key和字段获取数据
func SessionGet(key string) string {
	con, err := ssdb.Connect(session_ip, 6379)
	defer con.Close()
	if err != nil {
		panic(err)
	}
	val, err := con.Do("get", ssdbkey+"_"+key)
	if err != nil {
		panic(err)
	}

	if len(val) == 2 && val[0] == "ok" {
		return val[1]
	}
	return ""
}

//写入session进入ssdb
func SessionSet(key, val string) bool {
	con, err := ssdb.Connect(session_ip, 6379)
	defer con.Close()
	if err != nil {
		panic(err)
	}
	resp, err := con.Do("set", ssdbkey+"_"+key, val)
	if err != nil {
		panic(err)
	}

	if len(resp) == 1 && resp[0] == "ok" {
		return true
	} else {
		return false
	}
}

func restoreSessionNew(req *http.Request) Session {
	cookie, err := req.Cookie("_S")
	if err != nil {
		return make(Session)
	} else {
		return getSessionFromCookieNew(cookie)
	}
}

func getSessionFromCookieNew(cookie *http.Cookie) Session {
	session := make(Session)

	data := cookie.Value

	ParseKeyValueCookie(data, func(key, val string) {
		session[key] = val
	})

	if sessionTimeoutExpiredOrMissing(session) {
		session = make(Session)
	}

	return session
}

/*##################################小白#########################################*/
