package tfaps

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"io"
	"io/ioutil"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/thomseddon/go-flags"
)

var config *Config

// Config holds the runtime application config
type Config struct {
	LogLevel  string `long:"log-level" env:"LOG_LEVEL" default:"warn" choice:"trace" choice:"debug" choice:"info" choice:"warn" choice:"error" choice:"fatal" choice:"panic" description:"Log level"`
	LogFormat string `long:"log-format"  env:"LOG_FORMAT" default:"text" choice:"text" choice:"json" choice:"pretty" description:"Log format"`

	AuthHost               string               `long:"auth-host" env:"AUTH_HOST" description:"Single host to use when returning from 3rd party auth"`
	Config                 func(s string) error `long:"config" env:"CONFIG" description:"Path to config file" json:"-"`
	CookieDomains          []CookieDomain       `long:"cookie-domain" env:"COOKIE_DOMAIN" env-delim:"," description:"Domain to set auth cookie on, can be set multiple times"`
	InsecureCookie         bool                 `long:"insecure-cookie" env:"INSECURE_COOKIE" description:"Use insecure cookies"`
	CookieName             string               `long:"cookie-name" env:"COOKIE_NAME" default:"_forward_auth" description:"Cookie Name"`
	CSRFCookieName         string               `long:"csrf-cookie-name" env:"CSRF_COOKIE_NAME" default:"_forward_auth_csrf" description:"CSRF Cookie Name"`
	DefaultAction          string               `long:"default-action" env:"DEFAULT_ACTION" default:"auth" choice:"auth" choice:"allow" description:"Default action"`
	Domains                CommaSeparatedList   `long:"domain" env:"DOMAIN" env-delim:"," description:"Only allow given email domains, can be set multiple times"`
	LifetimeString         int                  `long:"lifetime" env:"LIFETIME" default:"43200" description:"Lifetime in seconds"`
	LogoutRedirect         string               `long:"logout-redirect" env:"LOGOUT_REDIRECT" description:"URL to redirect to following logout"`
	MatchWhitelistOrDomain bool                 `long:"match-whitelist-or-domain" env:"MATCH_WHITELIST_OR_DOMAIN" description:"Allow users that match *either* whitelist or domain (enabled by default in v3)"`
	Path                   string               `long:"url-path" env:"URL_PATH" default:"/_oauth" description:"Callback URL Path"`
	SecretString           string               `long:"secret" env:"SECRET" description:"Secret used for signing (required)" json:"-"`
	Whitelist              CommaSeparatedList   `long:"whitelist" env:"WHITELIST" env-delim:"," description:"Only allow given email addresses, can be set multiple times"`
	Port                   int                  `long:"port" env:"PORT" default:"4181" description:"Port to listen on"`
	Product                string               `long:"product" env:"PRODUCT" default:"traefik-forward-auth-plex-sso" description:"Identity of this service to send to Plex in X-Plex-Product header"`
	ClientIdentifierString string               `long:"client-identifier" env:"CLIENT_IDENTIFIER" description:"Client identifier of this service to send to Plex in X-Plex-Client-Identifier header" json:"-"`

	Rules map[string]*Rule `long:"rule.<name>.<param>" description:"Rule definitions, param can be: \"action\" or \"rule\""`

	// Filled during transformations
	Secret           []byte `json:"-"`
	Lifetime         time.Duration
	ClientIdentifier string `json:"-"`
}

// NewGlobalConfig creates a new global config, parsed from command arguments
func NewGlobalConfig() *Config {
	var err error
	config, err = NewConfig(os.Args[1:])
	if err != nil {
		fmt.Printf("%+v\n", err)
		os.Exit(1)
	}

	return config
}

// TODO: move config parsing into new func "NewParsedConfig"

// NewConfig parses and validates provided configuration into a config object
func NewConfig(args []string) (*Config, error) {
	c := &Config{
		Rules: map[string]*Rule{},
	}

	err := c.parseFlags(args)
	if err != nil {
		return c, err
	}

	// TODO: as log flags have now been parsed maybe we should return here so
	// any further errors can be logged via logrus instead of printed?

	// TODO: Rename "Validate" method to "Setup" and move all below logic

	// Setup

	// Transformations
	if len(c.Path) > 0 && c.Path[0] != '/' {
		c.Path = "/" + c.Path
	}
	c.Secret = []byte(c.SecretString)
	c.Lifetime = time.Second * time.Duration(c.LifetimeString)
	if len(c.ClientIdentifierString) == 0 {
		c.ClientIdentifier = uuid.New().String()
	} else {
		c.ClientIdentifier = c.ClientIdentifierString
	}

	return c, nil
}

func (c *Config) parseFlags(args []string) error {
	p := flags.NewParser(c, flags.Default|flags.IniUnknownOptionHandler)
	p.UnknownOptionHandler = c.parseUnknownFlag

	i := flags.NewIniParser(p)
	c.Config = func(s string) error {
		// Try parsing at as an ini
		err := i.ParseFile(s)
		return err
	}

	_, err := p.ParseArgs(args)
	if err != nil {
		return handleFlagError(err)
	}

	return nil
}

func (c *Config) parseUnknownFlag(option string, arg flags.SplitArgument, args []string) ([]string, error) {
	// Parse rules in the format "rule.<name>.<param>"
	parts := strings.Split(option, ".")
	if len(parts) == 3 && parts[0] == "rule" {
		// Ensure there is a name
		name := parts[1]
		if len(name) == 0 {
			return args, errors.New("route name is required")
		}

		// Get value, or pop the next arg
		val, ok := arg.Value()
		if !ok && len(args) > 1 {
			val = args[0]
			args = args[1:]
		}

		// Check value
		if len(val) == 0 {
			return args, errors.New("route param value is required")
		}

		// Unquote if required
		if val[0] == '"' {
			var err error
			val, err = strconv.Unquote(val)
			if err != nil {
				return args, err
			}
		}

		// Get or create rule
		rule, ok := c.Rules[name]
		if !ok {
			rule = NewRule()
			c.Rules[name] = rule
		}

		// Add param value to rule
		switch parts[2] {
		case "action":
			rule.Action = val
		case "rule":
			rule.Rule = val
		case "whitelist":
			list := CommaSeparatedList{}
			list.UnmarshalFlag(val)
			rule.Whitelist = list
		case "domains":
			list := CommaSeparatedList{}
			list.UnmarshalFlag(val)
			rule.Domains = list
		default:
			return args, fmt.Errorf("invalid route param: %v", option)
		}
	} else {
		return args, fmt.Errorf("unknown flag: %v", option)
	}

	return args, nil
}

func handleFlagError(err error) error {
	flagsErr, ok := err.(*flags.Error)
	if ok && flagsErr.Type == flags.ErrHelp {
		// Library has just printed cli help
		os.Exit(0)
	}

	return err
}

var legacyFileFormat = regexp.MustCompile(`(?m)^([a-z-]+) (.*)$`)

func convertLegacyToIni(name string) (io.Reader, error) {
	b, err := ioutil.ReadFile(name)
	if err != nil {
		return nil, err
	}

	return bytes.NewReader(legacyFileFormat.ReplaceAll(b, []byte("$1=$2"))), nil
}

// Validate validates a config object
func (c *Config) Validate() {
	// Check for show stopper errors
	if len(c.Secret) == 0 {
		log.Fatal("\"secret\" option must be set")
	}

	// Check rules (validates the rule and the rule provider)
	for _, rule := range c.Rules {
		err := rule.Validate()
		if err != nil {
			log.Fatal(err)
		}
	}
}

func (c Config) String() string {
	jsonConf, _ := json.Marshal(c)
	return string(jsonConf)
}

// Rule holds defined rules
type Rule struct {
	Action    string
	Rule      string
	Whitelist CommaSeparatedList
	Domains   CommaSeparatedList
}

// NewRule creates a new rule object
func NewRule() *Rule {
	return &Rule{
		Action: "auth",
	}
}

func (r *Rule) formattedRule() string {
	// Traefik implements their own "Host" matcher and then offers "HostRegexp"
	// to invoke the mux "Host" matcher. This ensures the mux version is used
	return strings.ReplaceAll(r.Rule, "Host(", "HostRegexp(")
}

// Validate validates a rule
func (r *Rule) Validate() error {
	if r.Action != "auth" && r.Action != "allow" {
		return errors.New("invalid rule action, must be \"auth\" or \"allow\"")
	}

	return nil
}

// Legacy support for comma separated lists

// CommaSeparatedList provides legacy support for config values provided as csv
type CommaSeparatedList []string

// UnmarshalFlag converts a comma separated list to an array
func (c *CommaSeparatedList) UnmarshalFlag(value string) error {
	*c = append(*c, strings.Split(value, ",")...)
	return nil
}

// MarshalFlag converts an array back to a comma separated list
func (c *CommaSeparatedList) MarshalFlag() (string, error) {
	return strings.Join(*c, ","), nil
}
