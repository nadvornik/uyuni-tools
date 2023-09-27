package api

import (
	"github.com/rs/zerolog/log"

	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const ROOT_PATH_APIv1 = "/rhn/manager/api"

type HTTPClient struct {
	BaseURL    string
	Client     *http.Client
	AuthCookie *http.Cookie
}

type ConnectionDetails struct {
	Host     string
	User     string
	Password string
}

func prettyPrint(v interface{}) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return ""
	}
	return fmt.Sprintln(string(b))
}

func (c *HTTPClient) sendRequest(req *http.Request) (*http.Response, error) {
	log.Debug().Msgf("Sending %s request %s", req.Method, req.URL)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Accept", "application/json; charset=utf-8")
	if c.AuthCookie != nil {
		req.AddCookie(c.AuthCookie)
	}

	log.Trace().Msg(prettyPrint(req.Header))

	res, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}

	log.Trace().Msg(prettyPrint(res.Header))
	log.Trace().Msg(prettyPrint(res.Body))

	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusBadRequest {
		var errResponse map[string]string
		if err = json.NewDecoder(res.Body).Decode(&errResponse); err == nil {
			return nil, fmt.Errorf(errResponse["message"])
		}
		return nil, fmt.Errorf("Unknown error: %d", res.StatusCode)
	}
	log.Debug().Msgf("Received response with code %d", res.StatusCode)

	return res, nil
}

func Init(fqdn string) *HTTPClient {
	client := &HTTPClient{
		BaseURL: fmt.Sprintf("https://%s%s", fqdn, ROOT_PATH_APIv1),
		Client: &http.Client{
			Timeout: time.Minute,
		},
	}
	return client
}

func (c *HTTPClient) Login(username string, password string) error {
	url := fmt.Sprintf("%s/%s", c.BaseURL, "auth/login")
	data := map[string]string{
		"login":    username,
		"password": password,
	}
	jsonData, err := json.Marshal(data)
	if err != nil {
		log.Fatal().Err(err).Msg("Unable to create login data")
	}
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}

	res, err := c.sendRequest(req)
	if err != nil {
		return err
	}

	var response map[string]interface{}
	if err = json.NewDecoder(res.Body).Decode(&response); err != nil {
		return err
	}
	if !response["success"].(bool) {
		log.Error().Msgf("%s", response["messages"])
	}

	cookies := res.Cookies()
	for _, cookie := range cookies {
		if cookie.Name == "pxt-session-cookie" && cookie.MaxAge > 0 {
			c.AuthCookie = cookie
			break
		}
	}

	if c.AuthCookie == nil {
		log.Fatal().Msg("Auth cookie not found in login response")
	}

	return nil
}

func (c *HTTPClient) Post(path string, data map[string]interface{}) (map[string]interface{}, error) {
	url := fmt.Sprintf("%s/%s", c.BaseURL, path)
	jsonData, err := json.Marshal(data)
	if err != nil {
		log.Error().Err(err).Msg("Unable to JSONify data")
		return nil, err
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}

	res, err := c.sendRequest(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	var response map[string]interface{}
	if err = json.NewDecoder(res.Body).Decode(&response); err != nil {
		return nil, err
	}

	return response, nil
}

func (c *HTTPClient) Get(path string) (map[string]interface{}, error) {
	url := fmt.Sprintf("%s/%s", c.BaseURL, path)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	res, err := c.sendRequest(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	var response map[string]interface{}
	if err = json.NewDecoder(res.Body).Decode(&response); err != nil {
		return nil, err
	}

	return response, nil
}