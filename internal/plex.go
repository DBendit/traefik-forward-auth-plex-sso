package tfaps

import (
	"encoding/xml"
	"errors"
	"fmt"
	"github.com/sirupsen/logrus"
	"net/http"
	"net/url"
)

const pinURL = "https://plex.tv/api/v2/pins"
const loginURL = "https://app.plex.tv/auth/#!"
const userURL = "https://plex.tv/users/account"
const resourcesURL = "https://plex.tv/api/resources"

type AccessTier int64

const (
	NoAccess AccessTier = iota
	NormalUser
	HomeUser
	Owner
)

func (a AccessTier) String() string {
	switch a {
	case NoAccess:
		return "NoAccess"
	case NormalUser:
		return "NormalUser"
	case HomeUser:
		return "HomeUser"
	case Owner:
		return "Owner"
	}
	return "Unknown"
}

// Pin A pin response from Plex's auth system
type Pin struct {
	XMLName xml.Name `xml:"pin"`
	Id      string   `xml:"id,attr"`
	Code    string   `xml:"code,attr"`
	Token   string   `xml:"authToken,attr"`
}

// User A user record from Plex, deserialized from XML
type User struct {
	XMLName xml.Name `xml:"user"`
	Email   string   `xml:"email,attr"`
}

// Resources A collection of device resources associated with a User
type Resources struct {
	XMLName xml.Name `xml:"MediaContainer"`
	Devices []struct {
		ClientIdentifier string `xml:"clientIdentifier,attr"`
		Owned            string `xml:"owned,attr"`
		Home             string `xml:"home,attr"`
	} `xml:"Device"`
}

func addHeaders(req *http.Request) {
	req.Header.Add("X-Plex-Product", config.Product)
	req.Header.Add("X-Plex-Client-Identifier", config.ClientIdentifier)
}

func addTokenHeader(req *http.Request, token string) {
	req.Header.Add("X-Plex-Token", token)
}

func doReq(logger *logrus.Entry, req *http.Request, output interface{}) error {
	client := http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		logger.WithField("error", err).Error("Error sending request")
		return errors.New("failure while sending request")
	}
	defer resp.Body.Close()
	err = xml.NewDecoder(resp.Body).Decode(output)
	if err != nil {
		logger.WithField("error", err).Error("Error unmarshalling response")
		return errors.New("failure unmarshalling response")
	}

	return nil
}

// GetPin Retrieve a Pin (with Id and Code) from Plex
func GetPin(logger *logrus.Entry) (Pin, error) {
	pinUrl, _ := url.Parse(pinURL)

	q := url.Values{}
	q.Set("strong", "true")
	pinUrl.RawQuery = q.Encode()

	req, err := http.NewRequest("POST", pinUrl.String(), nil)
	if err != nil {
		return Pin{}, errors.New("unable to construct pin request")
	}
	addHeaders(req)
	var pinResp Pin
	err = doReq(logger, req, &pinResp)
	if err != nil {
		return Pin{}, err
	}

	return pinResp, nil
}

// GetLoginURL Construct a login URL for authenticating with Plex
func GetLoginURL(redirectURI, code string) string {
	// Can't use url.Parse here, since Plex API wants a leading fragment for some reason
	q := url.Values{}
	q.Set("clientID", config.ClientIdentifier)
	q.Set("code", code)
	q.Set("forwardUrl", redirectURI)
	return fmt.Sprintf("%s?%s", loginURL, q.Encode())
}

// GetToken Retrieve an authentication Token using a Pin
func GetToken(logger *logrus.Entry, pinId string) (string, error) {
	pinUrl, _ := url.Parse(pinURL)
	pinUrl.Path += fmt.Sprintf("/%s", pinId)
	req, err := http.NewRequest("GET", pinUrl.String(), nil)
	if err != nil {
		return "", errors.New("unable to construct pin request")
	}
	addHeaders(req)
	var pin Pin
	err = doReq(logger, req, &pin)
	if err != nil {
		return "", err
	}

	return pin.Token, nil
}

// GetUser Retrieve an authenticated User
func GetUser(logger *logrus.Entry, token string) (User, error) {
	userUrl, _ := url.Parse(userURL)
	req, err := http.NewRequest("GET", userUrl.String(), nil)
	if err != nil {
		return User{}, errors.New("unable to construct user request")
	}
	addHeaders(req)
	addTokenHeader(req, token)
	var user User
	err = doReq(logger, req, &user)
	if err != nil {
		return User{}, err
	}

	return user, nil
}

// GetAccessTier Retrieve the access tier of this user on a configured server
func GetAccessTier(logger *logrus.Entry, token string) (AccessTier, error) {
	resourcesUrl, _ := url.Parse(resourcesURL)
	req, err := http.NewRequest("GET", resourcesUrl.String(), nil)
	if err != nil {
		return NoAccess, errors.New("unable to construct resources request")
	}
	addHeaders(req)
	addTokenHeader(req, token)
	var resources Resources
	err = doReq(logger, req, &resources)
	if err != nil {
		return NoAccess, err
	}

	for _, device := range resources.Devices {
		if device.ClientIdentifier == config.ServerIdentifier {
			if device.Owned == "1" {
				return Owner, nil
			}

			if device.Home == "1" {
				return HomeUser, nil
			}

			return NormalUser, nil
		}
	}

	return NoAccess, nil
}
