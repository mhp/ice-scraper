package main

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/pkg/errors"
)

// GAuthenticator encapsulates service account authentication in
// an http.RoundTripper wrapper.  See:
// https://developers.google.com/identity/protocols/OAuth2ServiceAccount
type GAuthenticator struct {
	// Configuration to obtain tokens
	privateKey  *rsa.PrivateKey
	clientEmail string
	tokenUri    string
	scope       string

	// Once obtained, use this token whilst it is valid
	currentToken  string
	tokenValidity time.Time
	tokenFile     string

	// Underlying RoundTripper for forwarding request
	NextLayer http.RoundTripper
}

func NewAuthenticator(credFile, tokenFile string) (*GAuthenticator, error) {
	f, err := os.Open(credFile)
	if err != nil {
		return nil, errors.Wrap(err, "opening credentials")
	}
	defer f.Close()

	credsJson, err := ioutil.ReadAll(f)
	if err != nil {
		return nil, errors.Wrap(err, "reading credentials")
	}

	var myCreds struct {
		// A service credentials file looks like this.  See:
		// https://console.developers.google.com/apis/credentials
		Type                    string
		ProjectId               string `json:"project_id"`
		PrivateKeyId            string `json:"private_key_id"`
		PrivateKey              string `json:"private_key"`
		ClientEmail             string `json:"client_email"`
		ClientId                string `json:"client_id"`
		AuthUri                 string `json:"auth_uri"`
		TokenUri                string `json:"token_uri"`
		AuthProviderX509CertUrl string `json:"auth_provider_x509_cert_url"`
		ClientX509CertUrl       string `json:"client_x509_cert_url"`
	}
	if err := json.Unmarshal(credsJson, &myCreds); err != nil {
		return nil, errors.Wrap(err, "parsing credentials")
	}

	rsaKey, err := parsePrivateKey(myCreds.PrivateKey)
	if err != nil {
		return nil, errors.Wrap(err, "parsing private key")
	}

	gauth := &GAuthenticator{
		privateKey:  rsaKey,
		clientEmail: myCreds.ClientEmail,
		tokenUri:    myCreds.TokenUri,
		scope:       `https://www.googleapis.com/auth/calendar`,
		tokenFile:   tokenFile,
		NextLayer:   http.DefaultTransport,
	}

	if tokenFile != "" {
		// preload stored token if we have one
		gauth.currentToken, gauth.tokenValidity = getStoredToken(tokenFile)
	}

	return gauth, nil
}

func (ga *GAuthenticator) RoundTrip(req *http.Request) (*http.Response, error) {
	if !ga.validToken() {
		if err := ga.getToken(); err != nil {
			return nil, err
		}
	}
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", ga.currentToken))
	return ga.NextLayer.RoundTrip(req)
}

func (ga *GAuthenticator) validToken() bool {
	if ga.currentToken == "" || ga.tokenValidity.Before(time.Now()) {
		return false
	}
	return true
}

const TokenGrantType = "urn:ietf:params:oauth:grant-type:jwt-bearer"

func (ga *GAuthenticator) getToken() error {
	now := time.Now()
	cs, err := jwtClaimset(ga.clientEmail, ga.tokenUri, now)
	if err != nil {
		return err
	}

	sig, err := signJwt(ga.privateKey, jwtHeader, cs)
	if err != nil {
		return err
	}

	args := url.Values{}
	args.Set("grant_type", TokenGrantType)
	args.Set("assertion", fmt.Sprintf("%s.%s.%s", jwtHeader, cs, sig))

	resp, err := http.Post(ga.tokenUri, "application/x-www-form-urlencoded", strings.NewReader(args.Encode()))
	if err != nil {
		return errors.Wrap(err, "requesting access token")
	}
	defer resp.Body.Close()

	responseJson, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return errors.Wrap(err, "reading token response")
	}

	var response struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
		TokenType   string `json:"token_type"`

		// If the request doesn't work, we'll see these
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if err := json.Unmarshal(responseJson, &response); err != nil {
		return errors.Wrap(err, "parsing token response")
	}

	// If we got an error, return it
	if response.Error != "" {
		return errors.Errorf("no-access-token: %v (%v)", response.Error, response.ErrorDescription)
	}

	// We only understand the semantics of Bearer tokens - reject anything else
	if response.TokenType != "Bearer" {
		return errors.Errorf("unknown-token-type: %v", response.TokenType)
	}

	ga.currentToken = response.AccessToken
	ga.tokenValidity = now.Add(time.Duration(response.ExpiresIn * int64(time.Second)))

	if ga.tokenFile != "" {
		if err := storeToken(ga.tokenFile, ga.currentToken, ga.tokenValidity); err != nil {
			return errors.Wrap(err, "can't store access token")
		}
	}

	return nil
}

// tokenStore represents the small json file used to persist tokens
// between program runs, thus reducing the number of token requests
// we need to make
type tokenStore struct {
	Token    string
	Validity time.Time
}

func getStoredToken(file string) (string, time.Time) {
	f, err := os.Open(file)
	if err != nil {
		log.Print("stored token not retrieved: ", err)
		return "", time.Time{}
	}
	defer f.Close()

	storeJson, err := ioutil.ReadAll(f)
	if err != nil {
		log.Print("stored token not readable: ", err)
		return "", time.Time{}
	}

	myTokenStore := tokenStore{}
	if err := json.Unmarshal(storeJson, &myTokenStore); err != nil {
		log.Print("stored token file malformed: ", err)
		return "", time.Time{}
	}

	return myTokenStore.Token, myTokenStore.Validity
}

func storeToken(file string, token string, validity time.Time) error {
	f, err := os.Create(file)
	if err != nil {
		return errors.Wrap(err, "creating token store file")
	}
	defer f.Close()

	myTokenStore := tokenStore{token, validity}
	storeJson, err := json.Marshal(myTokenStore)
	if err != nil {
		return errors.Wrap(err, "marshalling token store")
	}

	if _, err := f.Write(storeJson); err != nil {
		return errors.Wrap(err, "writing token store")
	}

	return nil
}

func parsePrivateKey(pemKey string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemKey))
	if block == nil {
		return nil, errors.New("can't decode private key")
	}

	if block.Type != "PRIVATE KEY" {
		return nil, errors.Errorf("unexpected key type %v", block.Type)
	}

	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.Wrap(err, "parsing private key")
	}

	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.Errorf("Wrong private key type: %T", key)
	}

	return rsaKey, nil
}

var jwtHeader = base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))

func jwtClaimset(svcEmail string, audience string, issuedAt time.Time) (string, error) {
	j, err := json.Marshal(struct {
		Aud   string `json:"aud"`
		Exp   int64  `json:"exp"`
		Iat   int64  `json:"iat"`
		Iss   string `json:"iss"`
		Scope string `json:"scope"`
	}{
		Scope: `https://www.googleapis.com/auth/calendar`,
		Aud:   audience,
		Iss:   svcEmail,
		Exp:   issuedAt.Add(time.Hour).Unix(),
		Iat:   issuedAt.Unix(),
	})

	if err != nil {
		log.Print("Can't marshall jwt claimset", err)
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(j), nil
}

func signJwt(privateKey *rsa.PrivateKey, header, claimset string) (string, error) {
	h := sha256.New()
	h.Write([]byte(header))
	h.Write([]byte("."))
	h.Write([]byte(claimset))
	d := h.Sum(nil)

	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, d)
	if err != nil {
		log.Print("Can't sign digest", err)
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(signature), nil
}
