package tfaps

import (
	"github.com/containous/traefik/v2/pkg/rules"
	"github.com/sirupsen/logrus"
	"net/http"
	"net/url"
)

// Server contains router and handler methods
type Server struct {
	router *rules.Router
}

// NewServer creates a new server object and builds router
func NewServer() *Server {
	s := &Server{}
	s.buildRoutes()
	return s
}

func (s *Server) buildRoutes() {
	var err error
	s.router, err = rules.NewRouter()
	if err != nil {
		log.Fatal(err)
	}

	// Let's build a router
	for name, rule := range config.Rules {
		matchRule := rule.formattedRule()
		if rule.Action == "allow" {
			s.router.AddRoute(matchRule, 1, s.AllowHandler(name))
		} else {
			s.router.AddRoute(matchRule, 1, s.AuthHandler(name))
		}
	}

	// Add callback handler
	s.router.Handle(config.Path, s.AuthCallbackHandler())

	// Add logout handler
	s.router.Handle(config.Path+"/logout", s.LogoutHandler())

	// Add a default handler
	if config.DefaultAction == "allow" {
		s.router.NewRoute().Handler(s.AllowHandler("default"))
	} else {
		s.router.NewRoute().Handler(s.AuthHandler("default"))
	}
}

// RootHandler Overwrites the request method, host and URL with those from the
// forwarded request so it's correctly routed by mux
func (s *Server) RootHandler(w http.ResponseWriter, r *http.Request) {
	// Modify request
	r.Method = r.Header.Get("X-Forwarded-Method")
	r.Host = r.Header.Get("X-Forwarded-Host")

	// Read URI from header if we're acting as forward auth middleware
	if _, ok := r.Header["X-Forwarded-Uri"]; ok {
		r.URL, _ = url.Parse(r.Header.Get("X-Forwarded-Uri"))
	}

	// Pass to mux
	s.router.ServeHTTP(w, r)
}

// AllowHandler Allows requests
func (s *Server) AllowHandler(rule string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.logger(r, "Allow", rule, "Allowing request")
		w.WriteHeader(200)
	}
}

// AuthHandler Authenticates requests
func (s *Server) AuthHandler(rule string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Logging setup
		logger := s.logger(r, "Auth", rule, "Authenticating request")

		// Get auth cookie
		c, err := r.Cookie(config.CookieName)
		if err != nil {
			s.authRedirect(logger, w, r)
			return
		}

		// Validate cookie
		email, err := ValidateCookie(r, c)
		if err != nil {
			if err.Error() == "Cookie has expired" {
				logger.Info("Cookie has expired")
				s.authRedirect(logger, w, r)
			} else {
				logger.WithField("error", err).Warn("Invalid cookie")
				http.Error(w, "Not authorized", 401)
			}
			return
		}

		// Validate user
		valid := ValidateEmail(email, rule)
		if !valid {
			logger.WithField("email", Sanitize(email)).Warn("Invalid email")
			http.Error(w, "Not authorized", 401)
			return
		}

		// Valid request
		logger.Debug("Allowing valid request")
		w.Header().Set("X-Forwarded-User", email)
		w.WriteHeader(200)
	}
}

// AuthCallbackHandler Handles auth callback request
func (s *Server) AuthCallbackHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Logging setup
		logger := s.logger(r, "AuthCallback", "default", "Handling callback")

		// Check for CSRF cookie
		c, err := FindCSRFCookie(r)
		if err != nil {
			logger.Info("Missing csrf cookie")
			http.Error(w, "Not authorized", 401)
			return
		}

		// Validate CSRF cookie against state
		valid, pinId, redirect, err := ValidateCSRFCookie(c)
		if !valid {
			logger.WithFields(logrus.Fields{
				"error":       err,
				"csrf_cookie": Sanitize(c.String()),
			}).Warn("Error validating csrf cookie")
			http.Error(w, "Not authorized", 401)
			return
		}

		// Clear CSRF cookie
		http.SetCookie(w, ClearCSRFCookie(r, c))

		// Exchange code for token
		token, err := GetToken(logger, pinId)
		if err != nil {
			logger.WithField("error", err).Error("Code exchange failed with provider")
			http.Error(w, "Service unavailable", 503)
			return
		}

		// Get user
		user, err := GetUser(logger, token)
		if err != nil {
			logger.WithField("error", err).Error("Error getting user")
			http.Error(w, "Service unavailable", 503)
			return
		}

		// Verify that the user is a member of the configured server
		if len(config.ServerIdentifier) > 0 {
			accessTier, err := GetAccessTier(logger, token)
			if err != nil {
				logger.WithField("error", err).WithField("user", user.Email).Error("Error getting access tier")
				http.Error(w, "Service unavailable", 503)
				return
			}
			if accessTier == NoAccess {
				logger.WithField("user", user.Email).Info("User unauthorized")
				http.Error(w, "Forbidden", 403)
				return
			} else {
				logger.WithField("user", user.Email).WithField("access_tier", accessTier).Info("User authorized")
			}
		}

		// Generate cookie
		http.SetCookie(w, MakeCookie(r, user.Email))
		logger.WithFields(logrus.Fields{
			"redirect": Sanitize(redirect),
			"user":     user.Email,
		}).Info("Successfully generated auth cookie, redirecting user.")

		// Redirect
		http.Redirect(w, r, redirect, http.StatusTemporaryRedirect)
	}
}

// LogoutHandler logs a user out
func (s *Server) LogoutHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Clear cookie
		http.SetCookie(w, ClearCookie(r))

		logger := s.logger(r, "Logout", "default", "Handling logout")
		logger.Info("Logged out user")

		if config.LogoutRedirect != "" {
			http.Redirect(w, r, config.LogoutRedirect, http.StatusTemporaryRedirect)
		} else {
			http.Error(w, "You have been logged out", 401)
		}
	}
}

func (s *Server) authRedirect(logger *logrus.Entry, w http.ResponseWriter, r *http.Request) {
	// Error indicates no cookie, request pin
	pin, err := GetPin(logger)
	if err != nil {
		logger.WithField("error", err).Error("Error retrieving pin")
		http.Error(w, "Service unavailable", 503)
		return
	}

	// Set the CSRF cookie
	csrf := MakeCSRFCookie(r, string(pin.Id), redirectBase(r))
	http.SetCookie(w, csrf)

	if !config.InsecureCookie && r.Header.Get("X-Forwarded-Proto") != "https" {
		logger.Warn("You are using \"secure\" cookies for a request that was not " +
			"received via https. You should either redirect to https or pass the " +
			"\"insecure-cookie\" config option to permit cookies via http.")
	}

	// Forward them on
	loginURL := GetLoginURL(redirectUri(r), pin.Code)
	http.Redirect(w, r, loginURL, http.StatusTemporaryRedirect)

	logger.WithFields(logrus.Fields{
		"csrf_cookie": csrf,
		"login_url":   loginURL,
	}).Debug("Set CSRF cookie and redirected to provider login url")
}

func (s *Server) logger(r *http.Request, handler, rule, msg string) *logrus.Entry {
	// Create logger
	logger := log.WithFields(logrus.Fields{
		"handler":   handler,
		"rule":      rule,
		"method":    Sanitize(r.Header.Get("X-Forwarded-Method")),
		"proto":     Sanitize(r.Header.Get("X-Forwarded-Proto")),
		"host":      Sanitize(r.Header.Get("X-Forwarded-Host")),
		"uri":       Sanitize(r.Header.Get("X-Forwarded-Uri")),
		"source_ip": Sanitize(r.Header.Get("X-Forwarded-For")),
	})

	// Log request
	logger.WithFields(logrus.Fields{
		"cookies": r.Cookies(),
	}).Debug(msg)

	return logger
}
