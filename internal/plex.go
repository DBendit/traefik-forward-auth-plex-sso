package tfaps

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"github.com/sirupsen/logrus"
	"net/http"
	"net/url"
)

const PinURL = "https://plex.tv/api/v2/pins"
const LoginURL = "https://app.plex.tv/auth/#!"
const UserURL = "https://plex.tv/users/account"
const ServerURL = "https://plex.tv/api/resources"

type Pin struct {
	Id   json.Number `json:"id"`
	Code string      `json:"code"`
}

type Token struct {
	Token string `json:"authToken"`
}

type User struct {
	XMLName xml.Name `xml:"user"`
	Email   string   `xml:"email,attr"`
}

func addHeaders(req *http.Request) {
	req.Header.Add("accept", "application/json")
	req.Header.Add("X-Plex-Product", config.Product)
	req.Header.Add("X-Plex-Client-Identifier", config.ClientIdentifier)
}

func doReq(logger *logrus.Entry, req *http.Request, output interface{}) error {
	client := http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		logger.WithField("error", err).Error("Error requesting pin")
		return errors.New("failure while sending request")
	}
	defer resp.Body.Close()
	err = json.NewDecoder(resp.Body).Decode(output)
	if err != nil {
		logger.WithField("error", err).Error("Error unmarshalling pin response")
		return errors.New("failure unmarshalling response")
	}

	return nil
}

func doReqXml(logger *logrus.Entry, req *http.Request, output interface{}) error {
	client := http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		logger.WithField("error", err).Error("Error requesting pin")
		return errors.New("failure while sending request")
	}
	defer resp.Body.Close()
	err = xml.NewDecoder(resp.Body).Decode(output)
	if err != nil {
		logger.WithField("error", err).Error("Error unmarshalling pin response")
		return errors.New("failure unmarshalling response")
	}

	return nil
}

func GetPin(logger *logrus.Entry) (Pin, error) {
	pinUrl, _ := url.Parse(PinURL)

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

func GetLoginURL(redirectURI, code string) string {
	// Can't use url.Parse here, since Plex API wants a leading fragment for some reason
	q := url.Values{}
	q.Set("clientID", config.ClientIdentifier)
	q.Set("code", code)
	q.Set("forwardUrl", redirectURI)
	return fmt.Sprintf("%s?%s", LoginURL, q.Encode())
}

func GetToken(logger *logrus.Entry, pinId string) (string, error) {
	pinUrl, _ := url.Parse(PinURL)
	pinUrl.Path += fmt.Sprintf("/%s", pinId)
	req, err := http.NewRequest("GET", pinUrl.String(), nil)
	if err != nil {
		return "", errors.New("unable to construct pin request")
	}
	addHeaders(req)
	var token Token
	err = doReq(logger, req, &token)
	if err != nil {
		return "", err
	}

	return token.Token, nil
}

func GetUser(logger *logrus.Entry, token string) (User, error) {
	userUrl, _ := url.Parse(UserURL)
	req, err := http.NewRequest("GET", userUrl.String(), nil)
	if err != nil {
		return User{}, errors.New("unable to construct user request")
	}
	addHeaders(req)
	req.Header.Add("X-Plex-Token", token)
	var user User
	err = doReqXml(logger, req, &user)
	if err != nil {
		return User{}, err
	}

	return user, nil
}
