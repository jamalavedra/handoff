// Package config loads Handoff's runtime configuration from the environment.
package config

import (
	"crypto/rand"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
)

type Config struct {
	Addr        string
	DatabaseURL string
	// BaseURL is the public URL of the apex, e.g. https://handoff.app.
	// Orgs live at <slug>.<host>; its scheme and port are reused for org URLs.
	BaseURL *url.URL
	// SingleOrg, when set, serves exactly one org (self-hosted mode) on any host.
	SingleOrg     string
	SingleOrgName string
	AllowSignup   bool
	// Dev returns magic login links in API responses instead of only mailing them.
	Dev bool

	SMTPURL  string
	MailFrom string

	GoogleClientID     string
	GoogleClientSecret string

	Secret []byte
}

var slugRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,30}[a-z0-9])?$`)

// ValidSlug reports whether s can be used as an org subdomain.
func ValidSlug(s string) bool { return slugRe.MatchString(s) }

func Load() (*Config, error) {
	c := &Config{
		Addr:               env("HANDOFF_ADDR", ":8080"),
		DatabaseURL:        os.Getenv("DATABASE_URL"),
		SingleOrg:          strings.ToLower(os.Getenv("HANDOFF_SINGLE_ORG")),
		SingleOrgName:      os.Getenv("HANDOFF_SINGLE_ORG_NAME"),
		Dev:                truthy(os.Getenv("HANDOFF_DEV")),
		SMTPURL:            os.Getenv("HANDOFF_SMTP_URL"),
		MailFrom:           env("HANDOFF_MAIL_FROM", "Handoff <no-reply@localhost>"),
		GoogleClientID:     os.Getenv("HANDOFF_GOOGLE_CLIENT_ID"),
		GoogleClientSecret: os.Getenv("HANDOFF_GOOGLE_CLIENT_SECRET"),
	}
	if c.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	base, err := url.Parse(env("HANDOFF_BASE_URL", "http://handoff.localhost:8080"))
	if err != nil || base.Host == "" || (base.Scheme != "http" && base.Scheme != "https") {
		return nil, fmt.Errorf("HANDOFF_BASE_URL must be an absolute http(s) URL")
	}
	base.Path, base.RawQuery, base.Fragment = "", "", ""
	c.BaseURL = base
	if c.SingleOrg != "" && !ValidSlug(c.SingleOrg) {
		return nil, fmt.Errorf("HANDOFF_SINGLE_ORG must be a lowercase slug")
	}
	if c.SingleOrgName == "" {
		c.SingleOrgName = c.SingleOrg
	}
	c.AllowSignup = c.SingleOrg == ""
	if v, ok := os.LookupEnv("HANDOFF_ALLOW_SIGNUP"); ok {
		c.AllowSignup = truthy(v) && c.SingleOrg == ""
	}
	if s := os.Getenv("HANDOFF_SECRET"); s != "" {
		c.Secret = []byte(s)
	} else {
		c.Secret = make([]byte, 32)
		_, _ = rand.Read(c.Secret)
	}
	return c, nil
}

// BaseHost is the apex hostname without port.
func (c *Config) BaseHost() string { return c.BaseURL.Hostname() }

// OrgURL is the canonical URL of an org's workspace.
func (c *Config) OrgURL(slug string) string {
	if c.SingleOrg != "" {
		return c.BaseURL.String()
	}
	host := slug + "." + c.BaseURL.Hostname()
	if p := c.BaseURL.Port(); p != "" {
		host += ":" + p
	}
	return c.BaseURL.Scheme + "://" + host
}

// URLForHost builds a URL on a host the request arrived on, keeping the public scheme.
func (c *Config) URLForHost(host string) string { return c.BaseURL.Scheme + "://" + host }

func (c *Config) Secure() bool { return c.BaseURL.Scheme == "https" }

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func truthy(v string) bool {
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
