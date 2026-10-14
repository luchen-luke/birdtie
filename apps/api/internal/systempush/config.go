package systempush

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
)

type FCMConfig struct {
	Enabled         bool
	ProjectID       string
	CredentialsFile string
	ApplicationID   string
}
type APNSConfig struct {
	Enabled     bool
	KeyFile     string
	KeyID       string
	TeamID      string
	Topic       string
	Environment string
}
type Config struct {
	Enabled     bool
	KeyringFile string
	FCM         FCMConfig
	APNS        APNSConfig
}

var ErrConfig = errors.New("invalid system push configuration")
var appPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,199}$`)
var projectPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{4,62}[a-z0-9]$`)
var appleIDPattern = regexp.MustCompile(`^[A-Z0-9]{10}$`)

func (c Config) Validate() error {
	if !c.Enabled {
		return nil
	}
	if strings.TrimSpace(c.KeyringFile) == "" || (!c.FCM.Enabled && !c.APNS.Enabled) {
		return ErrConfig
	}
	if c.FCM.Enabled && (!projectPattern.MatchString(c.FCM.ProjectID) || c.FCM.CredentialsFile == "" || !appPattern.MatchString(c.FCM.ApplicationID)) {
		return ErrConfig
	}
	if c.APNS.Enabled && (c.APNS.KeyFile == "" || !appleIDPattern.MatchString(c.APNS.KeyID) || !appleIDPattern.MatchString(c.APNS.TeamID) || !appPattern.MatchString(c.APNS.Topic) || (c.APNS.Environment != "production" && c.APNS.Environment != "sandbox")) {
		return ErrConfig
	}
	return nil
}
func LoadConfig(lookup func(string) (string, bool)) (Config, error) {
	var c Config
	if lookup == nil {
		return c, ErrConfig
	}
	read := func(k string) string { v, _ := lookup("BIRDTIE_SYSTEM_PUSH_" + k); return strings.TrimSpace(v) }
	boolean := func(k string) (bool, error) {
		v := read(k)
		if v == "" {
			return false, nil
		}
		b, e := strconv.ParseBool(v)
		if e != nil {
			return false, ErrConfig
		}
		return b, nil
	}
	var e error
	if c.Enabled, e = boolean("ENABLED"); e != nil {
		return c, e
	}
	// OFF neither reads credential files nor implicitly discovers providers.
	if !c.Enabled {
		return c, nil
	}
	c.KeyringFile = read("KEYRING_FILE")
	if c.FCM.Enabled, e = boolean("FCM_ENABLED"); e != nil {
		return c, e
	}
	if c.APNS.Enabled, e = boolean("APNS_ENABLED"); e != nil {
		return c, e
	}
	c.FCM.ProjectID = read("FCM_PROJECT_ID")
	c.FCM.CredentialsFile = read("FCM_CREDENTIALS_FILE")
	c.FCM.ApplicationID = read("FCM_APPLICATION_ID")
	c.APNS.KeyFile = read("APNS_KEY_FILE")
	c.APNS.KeyID = read("APNS_KEY_ID")
	c.APNS.TeamID = read("APNS_TEAM_ID")
	c.APNS.Topic = read("APNS_TOPIC")
	c.APNS.Environment = read("APNS_ENVIRONMENT")
	return c, c.Validate()
}
func (c Config) Kinds() []Kind {
	k := []Kind{}
	if c.Enabled && c.FCM.Enabled {
		k = append(k, FCMToken)
	}
	if c.Enabled && c.APNS.Enabled {
		k = append(k, APNSToken)
	}
	return k
}
func (c Config) Namespace(k Kind) (string, string, bool) {
	if c.Enabled && k == FCMToken && c.FCM.Enabled {
		return c.FCM.ApplicationID, "FCM", true
	}
	if c.Enabled && k == APNSToken && c.APNS.Enabled {
		return c.APNS.Topic, c.APNS.Environment, true
	}
	return "", "", false
}
