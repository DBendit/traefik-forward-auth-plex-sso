package tfaps

import (
	// "fmt"
	"os"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

/**
 * Tests
 */

func TestConfigDefaults(t *testing.T) {
	assert := assert.New(t)
	c, err := NewConfig([]string{})
	assert.Nil(err)

	assert.Equal("warn", c.LogLevel)
	assert.Equal("text", c.LogFormat)

	assert.Equal("", c.AuthHost)
	assert.Len(c.CookieDomains, 0)
	assert.False(c.InsecureCookie)
	assert.Equal("_forward_auth", c.CookieName)
	assert.Equal("_forward_auth_csrf", c.CSRFCookieName)
	assert.Equal("auth", c.DefaultAction)
	assert.Len(c.Domains, 0)
	assert.Equal(time.Second*time.Duration(43200), c.Lifetime)
	assert.Equal("", c.LogoutRedirect)
	assert.False(c.MatchWhitelistOrDomain)
	assert.Equal("/_oauth", c.Path)
	assert.Len(c.Whitelist, 0)
	assert.Equal(c.Port, 4181)
}

func TestConfigParseArgs(t *testing.T) {
	assert := assert.New(t)
	c, err := NewConfig([]string{
		"--cookie-name=cookiename",
		"--csrf-cookie-name", "\"csrfcookiename\"",
		"--rule.1.action=allow",
		"--rule.1.rule=PathPrefix(`/one`)",
		"--rule.two.action=auth",
		"--rule.two.rule=\"Host(`two.com`) && Path(`/two`)\"",
		"--port=8000",
	})
	require.Nil(t, err)

	// Check normal flags
	assert.Equal("cookiename", c.CookieName)
	assert.Equal("csrfcookiename", c.CSRFCookieName)
	assert.Equal(8000, c.Port)

	// Check rules
	assert.Equal(map[string]*Rule{
		"1": {
			Action: "allow",
			Rule:   "PathPrefix(`/one`)",
		},
		"two": {
			Action: "auth",
			Rule:   "Host(`two.com`) && Path(`/two`)",
		},
	}, c.Rules)
}

func TestConfigParseUnknownFlags(t *testing.T) {
	_, err := NewConfig([]string{
		"--unknown=_oauthpath2",
	})
	if assert.Error(t, err) {
		assert.Equal(t, "unknown flag: unknown", err.Error())
	}
}

func TestConfigParseRuleError(t *testing.T) {
	assert := assert.New(t)

	// Rule without name
	_, err := NewConfig([]string{
		"--rule..action=auth",
	})
	if assert.Error(err) {
		assert.Equal("route name is required", err.Error())
	}

	// Rule without value
	c, err := NewConfig([]string{
		"--rule.one.action=",
	})
	if assert.Error(err) {
		assert.Equal("route param value is required", err.Error())
	}
	// Check rules
	assert.Equal(map[string]*Rule{}, c.Rules)
}

func TestConfigParseIni(t *testing.T) {
	assert := assert.New(t)
	c, err := NewConfig([]string{
		"--config=../test/config0",
		"--config=../test/config1",
		"--csrf-cookie-name=csrfcookiename",
	})
	require.Nil(t, err)

	assert.Equal("inicookiename", c.CookieName, "should be read from ini file")
	assert.Equal("csrfcookiename", c.CSRFCookieName, "should be read from ini file")
	assert.Equal("/two", c.Path, "variable in second ini file should override first ini file")
	assert.Equal(map[string]*Rule{
		"1": {
			Action: "allow",
			Rule:   "PathPrefix(`/one`)",
		},
		"two": {
			Action: "auth",
			Rule:   "Host(`two.com`) && Path(`/two`)",
		},
	}, c.Rules)
}

func TestConfigParseEnvironment(t *testing.T) {
	assert := assert.New(t)
	os.Setenv("COOKIE_NAME", "env_cookie_name")
	os.Setenv("COOKIE_DOMAIN", "test1.com,example.org")
	os.Setenv("DOMAIN", "test2.com,example.org")
	os.Setenv("WHITELIST", "test3.com,example.org")

	c, err := NewConfig([]string{})
	assert.Nil(err)

	assert.Equal("env_cookie_name", c.CookieName, "variable should be read from environment")
	assert.Equal([]CookieDomain{
		*NewCookieDomain("test1.com"),
		*NewCookieDomain("example.org"),
	}, c.CookieDomains, "array variable should be read from environment COOKIE_DOMAIN")
	assert.Equal(CommaSeparatedList{"test2.com", "example.org"}, c.Domains, "array variable should be read from environment DOMAIN")
	assert.Equal(CommaSeparatedList{"test3.com", "example.org"}, c.Whitelist, "array variable should be read from environment WHITELIST")

	os.Unsetenv("COOKIE_NAME")
	os.Unsetenv("COOKIE_DOMAIN")
	os.Unsetenv("DOMAIN")
	os.Unsetenv("WHITELIST")
}

func TestConfigTransformation(t *testing.T) {
	assert := assert.New(t)
	c, err := NewConfig([]string{
		"--url-path=_oauthpath",
		"--secret=verysecret",
		"--lifetime=200",
	})
	require.Nil(t, err)

	assert.Equal("/_oauthpath", c.Path, "path should add slash to front")

	assert.Equal("verysecret", c.SecretString)
	assert.Equal([]byte("verysecret"), c.Secret, "secret should be converted to byte array")

	assert.Equal(200, c.LifetimeString)
	assert.Equal(time.Second*time.Duration(200), c.Lifetime, "lifetime should be read and converted to duration")
}

func TestConfigValidate(t *testing.T) {
	assert := assert.New(t)

	// Install new logger + hook
	var hook *test.Hook
	log, hook = test.NewNullLogger()
	log.ExitFunc = func(code int) {}

	// Validate default config + rule error
	c, _ := NewConfig([]string{
		"--rule.1.action=bad",
	})
	c.Validate()

	logs := hook.AllEntries()
	assert.Len(logs, 2)

	// Should have fatal error requiring secret
	assert.Equal("\"secret\" option must be set", logs[0].Message)
	assert.Equal(logrus.FatalLevel, logs[0].Level)

	// Should validate rule
	assert.Equal("invalid rule action, must be \"auth\" or \"allow\"", logs[1].Message)
	assert.Equal(logrus.FatalLevel, logs[1].Level)

	hook.Reset()

	// Validate with invalid providers
	c, _ = NewConfig([]string{
		"--secret=veryverysecret",
		"--rule.1.action=auth",
		"--rule.1.provider=bad2",
	})
	c.Validate()

	logs = hook.AllEntries()
	assert.Len(logs, 1)
}

func TestConfigCommaSeparatedList(t *testing.T) {
	assert := assert.New(t)
	list := CommaSeparatedList{}

	err := list.UnmarshalFlag("one,two")
	assert.Nil(err)
	assert.Equal(CommaSeparatedList{"one", "two"}, list, "should parse comma sepearated list")

	marshal, err := list.MarshalFlag()
	assert.Nil(err)
	assert.Equal("one,two", marshal, "should marshal back to comma sepearated list")
}
