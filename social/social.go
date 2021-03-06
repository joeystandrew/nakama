// Copyright 2018 The Nakama Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package social

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"go.uber.org/zap"
	"io/ioutil"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dgrijalva/jwt-go"
)

// Client is responsible for making calls to different providers
type Client struct {
	logger *zap.Logger

	client               *http.Client
	googleMutex          sync.RWMutex
	googleCerts          []*rsa.PublicKey
	googleCertsRefreshAt int64
	gamecenterCaCert     *x509.Certificate
	appleMutex           sync.RWMutex
	appleCerts           map[string]*AppleCert
	appleCertsRefreshAt  int64
}

type AppleCerts struct {
	Keys []*AppleCert `json:"keys"`
}

// JWK certificate data for an Apple Sign In verification key.
type AppleCert struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`

	key *rsa.PublicKey
}

// AppleProfile is an abbreviated version of a user authenticated through Apple Sign In.
type AppleProfile struct {
	ID            string
	Email         string
	EmailVerified bool
}

// FacebookProfile is an abbreviated version of a Facebook profile.
type FacebookProfile struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type facebookPagingCursors struct {
	After  string `json:"after"`
	Before string `json:"before"`
}

type facebookPaging struct {
	Cursors  facebookPagingCursors `json:"cursors"`
	Previous string                `json:"previous"`
	Next     string                `json:"next"`
}

type facebookFriends struct {
	Data   []FacebookProfile `json:"data"`
	Paging facebookPaging    `json:"paging"`
}

// GoogleProfile is an abbreviated version of a Google profile extracted from in a verified ID token.
type GoogleProfile struct {
	// Fields available in all tokens.
	Iss string `json:"iss"`
	Sub string `json:"sub"`
	Azp string `json:"azp"`
	Aud string `json:"aud"`
	Iat int64  `json:"iat"`
	Exp int64  `json:"exp"`
	// Fields available only if the user granted the "profile" and "email" OAuth scopes.
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
	GivenName     string `json:"given_name"`
	FamilyName    string `json:"family_name"`
	Locale        string `json:"locale"`
}

// SteamProfile is an abbreviated version of a Steam profile.
type SteamProfile struct {
	SteamID uint64 `json:"steamid,string"`
}

// SteamError contains a possible error response from the Steam Web API.
type SteamError struct {
	ErrorCode int    `json:"errorcode"`
	ErrorDesc string `json:"errordesc"`
}

// Unwrapping the SteamProfile
type SteamProfileWrapper struct {
	Response struct {
		Params *SteamProfile `json:"params"`
		Error  *SteamError   `json:"error"`
	} `json:"response"`
}

// NewClient creates a new Social Client
func NewClient(logger *zap.Logger, timeout time.Duration) *Client {
	// From https://knowledge.symantec.com/support/code-signing-support/index?page=content&actp=CROSSLINK&id=AR2170
	// Issued to: Symantec Class 3 SHA256 Code Signing CA
	// Issued by: VeriSign Class 3 Public Primary Certification Authority - G5
	// Valid from: 12/9/2013 to 12/9/2023
	// Serial Number: 3d 78 d7 f9 76 49 60 b2 61 7d f4 f0 1e ca 86 2a
	caData := []byte(`-----BEGIN CERTIFICATE-----
MIIFWTCCBEGgAwIBAgIQPXjX+XZJYLJhffTwHsqGKjANBgkqhkiG9w0BAQsFADCB
yjELMAkGA1UEBhMCVVMxFzAVBgNVBAoTDlZlcmlTaWduLCBJbmMuMR8wHQYDVQQL
ExZWZXJpU2lnbiBUcnVzdCBOZXR3b3JrMTowOAYDVQQLEzEoYykgMjAwNiBWZXJp
U2lnbiwgSW5jLiAtIEZvciBhdXRob3JpemVkIHVzZSBvbmx5MUUwQwYDVQQDEzxW
ZXJpU2lnbiBDbGFzcyAzIFB1YmxpYyBQcmltYXJ5IENlcnRpZmljYXRpb24gQXV0
aG9yaXR5IC0gRzUwHhcNMTMxMjEwMDAwMDAwWhcNMjMxMjA5MjM1OTU5WjB/MQsw
CQYDVQQGEwJVUzEdMBsGA1UEChMUU3ltYW50ZWMgQ29ycG9yYXRpb24xHzAdBgNV
BAsTFlN5bWFudGVjIFRydXN0IE5ldHdvcmsxMDAuBgNVBAMTJ1N5bWFudGVjIENs
YXNzIDMgU0hBMjU2IENvZGUgU2lnbmluZyBDQTCCASIwDQYJKoZIhvcNAQEBBQAD
ggEPADCCAQoCggEBAJeDHgAWryyx0gjE12iTUWAecfbiR7TbWE0jYmq0v1obUfej
DRh3aLvYNqsvIVDanvPnXydOC8KXyAlwk6naXA1OpA2RoLTsFM6RclQuzqPbROlS
Gz9BPMpK5KrA6DmrU8wh0MzPf5vmwsxYaoIV7j02zxzFlwckjvF7vjEtPW7ctZlC
n0thlV8ccO4XfduL5WGJeMdoG68ReBqYrsRVR1PZszLWoQ5GQMWXkorRU6eZW4U1
V9Pqk2JhIArHMHckEU1ig7a6e2iCMe5lyt/51Y2yNdyMK29qclxghJzyDJRewFZS
AEjM0/ilfd4v1xPkOKiE1Ua4E4bCG53qWjjdm9sCAwEAAaOCAYMwggF/MC8GCCsG
AQUFBwEBBCMwITAfBggrBgEFBQcwAYYTaHR0cDovL3MyLnN5bWNiLmNvbTASBgNV
HRMBAf8ECDAGAQH/AgEAMGwGA1UdIARlMGMwYQYLYIZIAYb4RQEHFwMwUjAmBggr
BgEFBQcCARYaaHR0cDovL3d3dy5zeW1hdXRoLmNvbS9jcHMwKAYIKwYBBQUHAgIw
HBoaaHR0cDovL3d3dy5zeW1hdXRoLmNvbS9ycGEwMAYDVR0fBCkwJzAloCOgIYYf
aHR0cDovL3MxLnN5bWNiLmNvbS9wY2EzLWc1LmNybDAdBgNVHSUEFjAUBggrBgEF
BQcDAgYIKwYBBQUHAwMwDgYDVR0PAQH/BAQDAgEGMCkGA1UdEQQiMCCkHjAcMRow
GAYDVQQDExFTeW1hbnRlY1BLSS0xLTU2NzAdBgNVHQ4EFgQUljtT8Hkzl699g+8u
K8zKt4YecmYwHwYDVR0jBBgwFoAUf9Nlp8Ld7LvwMAnzQzn6Aq8zMTMwDQYJKoZI
hvcNAQELBQADggEBABOFGh5pqTf3oL2kr34dYVP+nYxeDKZ1HngXI9397BoDVTn7
cZXHZVqnjjDSRFph23Bv2iEFwi5zuknx0ZP+XcnNXgPgiZ4/dB7X9ziLqdbPuzUv
M1ioklbRyE07guZ5hBb8KLCxR/Mdoj7uh9mmf6RWpT+thC4p3ny8qKqjPQQB6rqT
og5QIikXTIfkOhFf1qQliZsFay+0yQFMJ3sLrBkFIqBgFT/ayftNTI/7cmd3/SeU
x7o1DohJ/o39KK9KEr0Ns5cF3kQMFfo2KwPcwVAB8aERXRTl4r0nS1S+K4ReD6bD
dAUK75fDiSKxH3fzvc1D1PFMqT+1i4SvZPLQFCE=
-----END CERTIFICATE-----`)
	caBlock, _ := pem.Decode(caData)
	caCert, _ := x509.ParseCertificate(caBlock.Bytes)
	return &Client{
		logger: logger,

		client: &http.Client{
			Timeout: timeout,
		},
		gamecenterCaCert: caCert,
	}
}

// GetFacebookProfile retrieves the user's Facebook Profile given the accessToken
func (c *Client) GetFacebookProfile(ctx context.Context, accessToken string) (*FacebookProfile, error) {
	c.logger.Debug("Getting Facebook profile", zap.String("token", accessToken))

	path := "https://graph.facebook.com/v9.0/me?access_token=" + url.QueryEscape(accessToken) +
		"&fields=" + url.QueryEscape("name,email")
	var profile FacebookProfile
	err := c.request(ctx, "facebook profile", path, nil, &profile)
	if err != nil {
		return nil, err
	}
	return &profile, nil
}

// GetFacebookFriends queries the Facebook Graph.
// Token is expected to also have the "user_friends" permission.
func (c *Client) GetFacebookFriends(ctx context.Context, accessToken string) ([]FacebookProfile, error) {
	c.logger.Debug("Getting Facebook friends", zap.String("token", accessToken))

	friends := make([]FacebookProfile, 0)
	after := ""
	for {
		// In FB Graph API 2.0+ this only returns friends that also use the same app.
		path := "https://graph.facebook.com/v9.0/me/friends?access_token=" + url.QueryEscape(accessToken)
		if after != "" {
			path += "&after=" + after
		}
		var currentFriends facebookFriends
		err := c.request(ctx, "facebook friends", path, nil, &currentFriends)
		if err != nil {
			return friends, err
		}
		friends = append(friends, currentFriends.Data...)
		// When there are no more items, this will be "" and end the loop.
		if currentFriends.Paging.Next == "" {
			return friends, nil
		}
		after = currentFriends.Paging.Cursors.After
	}
}

// Extract player ID and validate the Facebook Instant Game token.
func (c *Client) ExtractFacebookInstantGameID(signedPlayerInfo string, appSecret string) (facebookInstantGameID string, err error) {
	c.logger.Debug("Extracting Facebook Instant Game ID", zap.String("signedPlayerInfo", signedPlayerInfo))

	parts := strings.Split(signedPlayerInfo, ".")
	if len(parts) != 2 {
		return "", errors.New("malformed signedPlayerInfo")
	}

	signatureBase64 := parts[0]
	payloadBase64 := parts[1]
	payloadRaw, err := jwt.DecodeSegment(payloadBase64)
	if err != nil {
		return "", err
	}

	var payload struct {
		Algorithm      string `json:"algorithm"`
		IssuedAt       int    `json:"issued_at"`
		PlayerID       string `json:"player_id"`
		RequestPayload string `json:"request_payload"` // discarded
	}
	err = json.Unmarshal(payloadRaw, &payload)
	if err != nil {
		return "", err
	}

	signingMethod := jwt.GetSigningMethod(payload.Algorithm)
	if signingMethod == nil {
		if payload.Algorithm == "HMAC-SHA256" {
			signingMethod = jwt.GetSigningMethod("HS256")
		} else {
			return "", errors.New("invalid signing method")
		}
	}

	err = signingMethod.Verify(payloadBase64, signatureBase64, []byte(appSecret))
	if err != nil {
		return "", err
	}

	return payload.PlayerID, nil
}

// CheckGoogleToken extracts the user's Google Profile from a given ID token.
func (c *Client) CheckGoogleToken(ctx context.Context, idToken string) (*GoogleProfile, error) {
	c.logger.Debug("Checking Google ID", zap.String("idToken", idToken))

	c.googleMutex.RLock()
	if c.googleCertsRefreshAt < time.Now().UTC().Unix() {
		// Release the read lock and perform a certificate refresh.
		c.googleMutex.RUnlock()
		c.googleMutex.Lock()
		if c.googleCertsRefreshAt < time.Now().UTC().Unix() {
			certs := make(map[string]string, 3)
			err := c.request(ctx, "google cert", "https://www.googleapis.com/oauth2/v1/certs", nil, &certs)
			if err != nil {
				c.googleMutex.Unlock()
				return nil, err
			}
			newCerts := make([]*rsa.PublicKey, 0, len(certs))
			var newRefreshAt int64
			for _, data := range certs {
				currentBlock, _ := pem.Decode([]byte(data))
				if currentBlock == nil {
					// Block was invalid, ignore it and try the next.
					continue
				}
				currentCert, err := x509.ParseCertificate(currentBlock.Bytes)
				if err != nil {
					// Certificate was invalid, ignore it and try the next.
					continue
				}
				t := time.Now()
				if currentCert.NotBefore.After(t) || currentCert.NotAfter.Before(t) {
					// Certificate not yet valid or has already expired, skip it.
					continue
				}
				pub, ok := currentCert.PublicKey.(*rsa.PublicKey)
				if !ok {
					// Certificate was not an RSA public key.
					continue
				}
				newCerts = append(newCerts, pub)
				if newRefreshAt == 0 || newRefreshAt > currentCert.NotAfter.UTC().Unix() {
					// Refresh all certs 1 hour before the soonest expiry is due.
					newRefreshAt = currentCert.NotAfter.UTC().Unix() - 3600
				}
			}
			if len(newCerts) == 0 {
				c.googleMutex.Unlock()
				return nil, errors.New("error finding valid google cert")
			}
			c.googleCerts = newCerts
			c.googleCertsRefreshAt = newRefreshAt
		}
		c.googleMutex.Unlock()
		c.googleMutex.RLock()
	}
	googleCerts := c.googleCerts
	c.googleMutex.RUnlock()

	var err error
	var token *jwt.Token
	for _, cert := range googleCerts {
		// Try to parse and verify the token with each of the currently available certificates.
		token, err = jwt.Parse(idToken, func(token *jwt.Token) (interface{}, error) {
			if s, ok := token.Method.(*jwt.SigningMethodRSA); !ok || s.Hash != crypto.SHA256 {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			claims := token.Claims.(jwt.MapClaims)
			if !claims.VerifyIssuer("accounts.google.com", true) && !claims.VerifyIssuer("https://accounts.google.com", true) {
				return nil, fmt.Errorf("unexpected issuer: %v", claims["iss"])
			}
			return cert, nil
		})
		if err == nil {
			// If any certificate worked, the token is valid.
			break
		}
	}

	// All verification attempts failed.
	if token == nil {
		return nil, errors.New("google id token invalid")
	}

	claims := token.Claims.(jwt.MapClaims)
	profile := &GoogleProfile{}
	if v, ok := claims["iss"]; ok {
		if profile.Iss, ok = v.(string); !ok {
			return nil, errors.New("google id token iss field invalid")
		}
	} else {
		return nil, errors.New("google id token iss field missing")
	}
	if v, ok := claims["sub"]; ok {
		if profile.Sub, ok = v.(string); !ok {
			return nil, errors.New("google id token sub field invalid")
		}
	} else {
		return nil, errors.New("google id token sub field missing")
	}
	if v, ok := claims["azp"]; ok {
		if profile.Azp, ok = v.(string); !ok {
			return nil, errors.New("google id token azp field invalid")
		}
	} else {
		return nil, errors.New("google id token azp field missing")
	}
	if v, ok := claims["aud"]; ok {
		if profile.Aud, ok = v.(string); !ok {
			return nil, errors.New("google id token aud field invalid")
		}
	} else {
		return nil, errors.New("google id token aud field missing")
	}
	if v, ok := claims["iat"]; ok {
		switch v.(type) {
		case string:
			vi, err := strconv.Atoi(v.(string))
			if err != nil {
				return nil, errors.New("google id token iat field invalid")
			}
			profile.Iat = int64(vi)
		case float64:
			profile.Iat = int64(v.(float64))
		case int64:
			profile.Iat = v.(int64)
		default:
			return nil, errors.New("google id token iat field unknown")
		}
	}
	if v, ok := claims["exp"]; ok {
		switch v.(type) {
		case string:
			vi, err := strconv.Atoi(v.(string))
			if err != nil {
				return nil, errors.New("google id token exp field invalid")
			}
			profile.Exp = int64(vi)
		case float64:
			profile.Exp = int64(v.(float64))
		case int64:
			profile.Exp = v.(int64)
		default:
			return nil, errors.New("google id token exp field unknown")
		}
	}
	if v, ok := claims["email"]; ok {
		if profile.Email, ok = v.(string); !ok {
			return nil, errors.New("google id token email field invalid")
		}
	}
	if v, ok := claims["email_verified"]; ok {
		switch v.(type) {
		case bool:
			profile.EmailVerified = v.(bool)
		case string:
			vb, err := strconv.ParseBool(v.(string))
			if err != nil {
				return nil, errors.New("google id token email_verified field invalid")
			}
			profile.EmailVerified = vb
		default:
			return nil, errors.New("google id token email_verified field unknown")
		}
	}
	if v, ok := claims["name"]; ok {
		if profile.Name, ok = v.(string); !ok {
			return nil, errors.New("google id token name field invalid")
		}
	}
	if v, ok := claims["picture"]; ok {
		if profile.Picture, ok = v.(string); !ok {
			return nil, errors.New("google id token picture field invalid")
		}
	}
	if v, ok := claims["given_name"]; ok {
		if profile.GivenName, ok = v.(string); !ok {
			return nil, errors.New("google id token given name field invalid")
		}
	}
	if v, ok := claims["family_name"]; ok {
		if profile.FamilyName, ok = v.(string); !ok {
			return nil, errors.New("google id token family name field invalid")
		}
	}
	if v, ok := claims["locale"]; ok {
		if profile.Locale, ok = v.(string); !ok {
			return nil, errors.New("google id token locale field invalid")
		}
	}

	// Check token has not expired.
	if profile.Exp != 0 && profile.Exp < time.Now().UTC().Unix() {
		return nil, errors.New("google id token expired")
	}

	return profile, nil
}

// CheckGameCenterID checks to see validity of the GameCenter playerID
func (c *Client) CheckGameCenterID(ctx context.Context, playerID string, bundleID string, timestamp int64, salt string, signature string, publicKeyURL string) (bool, error) {
	c.logger.Debug("Checking Game Center ID", zap.String("playerID", playerID), zap.String("bundleID", bundleID), zap.Int64("timestamp", timestamp), zap.String("salt", salt), zap.String("signature", signature), zap.String("publicKeyURL", publicKeyURL))

	pub, err := url.Parse(publicKeyURL)
	if err != nil {
		return false, fmt.Errorf("gamecenter check error: invalid public key url: %v", err.Error())
	} else if pub.Scheme != "https" {
		return false, errors.New("gamecenter check error: invalid public key url scheme")
	} else if pub.Path == "" || pub.Path == "/" {
		return false, errors.New("gamecenter check error: invalid public key url path")
	} else if !strings.HasSuffix(pub.Host, ".apple.com") {
		return false, errors.New("gamecenter check error: invalid public key url domain")
	}
	slt, err := base64.StdEncoding.DecodeString(salt)
	if err != nil {
		return false, errors.New("gamecenter check error: error decoding salt")
	}
	sig, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return false, errors.New("gamecenter check error: error decoding signature")
	}

	body, err := c.requestRaw(ctx, "apple public key url", publicKeyURL, nil)
	if err != nil {
		return false, err
	}

	// Parse the public key, check issuer, check signature.
	pubBlock, rest := pem.Decode(body)
	if pubBlock == nil {
		pubBlock, _ = pem.Decode([]byte("\n-----BEGIN CERTIFICATE-----\n" + base64.StdEncoding.EncodeToString(rest) + "\n-----END CERTIFICATE-----"))
		if pubBlock == nil {
			return false, errors.New("gamecenter check error: error decoding public key")
		}
	}
	pubCert, err := x509.ParseCertificate(pubBlock.Bytes)
	if err != nil {
		return false, fmt.Errorf("gamecenter check error: error parsing public block: %v", err.Error())
	}
	err = pubCert.CheckSignatureFrom(c.gamecenterCaCert)
	if err != nil {
		return false, fmt.Errorf("gamecenter check error: bad public key signature: %v", err.Error())
	}
	ts := make([]byte, 8)
	binary.BigEndian.PutUint64(ts, uint64(timestamp))
	payload := [][]byte{[]byte(playerID), []byte(bundleID), ts, slt}
	err = pubCert.CheckSignature(x509.SHA256WithRSA, bytes.Join(payload, []byte{}), sig)
	if err != nil {
		return false, fmt.Errorf("gamecenter check error: signature mismatch: %v", err.Error())
	}
	return true, nil
}

// GetSteamProfile retrieves the user's Steam Profile.
// Key and App ID should be configured at the application level.
// See: https://partner.steamgames.com/documentation/auth#client_to_backend_webapi
func (c *Client) GetSteamProfile(ctx context.Context, publisherKey string, appID int, ticket string) (*SteamProfile, error) {
	c.logger.Debug("Getting Steam profile", zap.String("publisherKey", publisherKey), zap.Int("appID", appID), zap.String("ticket", ticket))

	path := "https://api.steampowered.com/ISteamUserAuth/AuthenticateUserTicket/v1/?format=json" +
		"&key=" + url.QueryEscape(publisherKey) + "&appid=" + strconv.Itoa(appID) + "&ticket=" + url.QueryEscape(ticket)
	var profileWrapper SteamProfileWrapper
	err := c.request(ctx, "steam profile", path, nil, &profileWrapper)
	if err != nil {
		return nil, err
	}
	if profileWrapper.Response.Error != nil {
		return nil, fmt.Errorf("%v, %v", profileWrapper.Response.Error.ErrorDesc, profileWrapper.Response.Error.ErrorCode)
	}
	if profileWrapper.Response.Params == nil {
		return nil, errors.New("no steam profile")
	}
	return profileWrapper.Response.Params, nil
}

func (c *Client) CheckAppleToken(ctx context.Context, bundleId string, idToken string) (*AppleProfile, error) {
	c.logger.Debug("Checking Apple Sign In", zap.String("bundleId", bundleId), zap.String("idToken", idToken))

	if bundleId == "" {
		return nil, errors.New("apple sign in not enabled")
	}

	c.appleMutex.RLock()
	if c.appleCertsRefreshAt < time.Now().UTC().Unix() {
		// Release the read lock and perform a certificate refresh.
		c.appleMutex.RUnlock()
		c.appleMutex.Lock()
		if c.appleCertsRefreshAt < time.Now().UTC().Unix() {
			var certs AppleCerts
			err := c.request(ctx, "apple cert", "https://appleid.apple.com/auth/keys", nil, &certs)
			if err != nil {
				c.appleMutex.Unlock()
				return nil, err
			}
			newCerts := make(map[string]*AppleCert, len(certs.Keys))
			for _, cert := range certs.Keys {
				// Check if certificate has all required fields.
				if cert.Kty == "" || cert.Kid == "" || cert.Use == "" || cert.Alg == "" || cert.N == "" || cert.E == "" {
					// Invalid certificate, skip it.
					continue
				}

				// Parse certificate's RSA Public Key encoded components.
				nBytes, err := base64.RawURLEncoding.DecodeString(cert.N)
				if err != nil {
					// Invalid modulus, skip certificate.
					continue
				}
				eBytes, err := base64.RawURLEncoding.DecodeString(cert.E)
				if err != nil {
					// Invalid exponent, skip certificate.
					continue
				}
				if len(eBytes) < 8 {
					// Pad the front of the exponent bytes with zeroes to ensure it's 8 bytes long.
					eBytes = append(make([]byte, 8-len(eBytes), 8), eBytes...)
				}
				var e uint64
				err = binary.Read(bytes.NewReader(eBytes), binary.BigEndian, &e)
				if err != nil {
					// Invalid exponent contents, skip certificate.
					continue
				}

				cert.key = &rsa.PublicKey{
					N: &big.Int{},
					E: int(e),
				}
				cert.key.N.SetBytes(nBytes)

				newCerts[cert.Kid] = cert
			}
			if len(newCerts) == 0 {
				c.appleMutex.Unlock()
				return nil, errors.New("error finding valid apple cert")
			}
			c.appleCerts = newCerts
			c.appleCertsRefreshAt = time.Now().UTC().Add(60 * time.Minute).Unix()
		}
		c.appleMutex.Unlock()
		c.appleMutex.RLock()
	}
	appleCerts := c.appleCerts
	c.appleMutex.RUnlock()

	// Try to parse and validate the JWT token.
	token, _ := jwt.Parse(idToken, func(token *jwt.Token) (interface{}, error) {
		// Grab the token's "kid" (key id) claim and see if we have a JWK certificate that matches it.
		kid, ok := token.Header["kid"]
		if !ok {
			return nil, fmt.Errorf("missing kid claim: %v", kid)
		}
		kidString, ok := kid.(string)
		if !ok {
			return nil, fmt.Errorf("invalid kid claim: %v", kid)
		}
		cert, ok := appleCerts[kidString]
		if !ok {
			return nil, fmt.Errorf("invalid kid claim: %v", kid)
		}

		// Check the token signing algorithm and the certificate signing algorithm match.
		if token.Method.Alg() != cert.Alg {
			return nil, fmt.Errorf("invalid alg: %v, expected %v", token.Method.Alg(), cert.Alg)
		}

		claims := token.Claims.(jwt.MapClaims)

		// Verify the issuer.
		if !claims.VerifyIssuer("https://appleid.apple.com", true) {
			return nil, fmt.Errorf("unexpected issuer: %v", claims["iss"])
		}

		// Verify the audience matches the configured client ID.
		if !claims.VerifyAudience(bundleId, true) {
			return nil, fmt.Errorf("unexpected audience: %v", claims["aud"])
		}

		return cert, nil
	})

	// Check if verification attempt has failed.
	if token == nil {
		return nil, errors.New("apple id token invalid")
	}

	// Extract the claims we need now that we know the token is valid.
	claims := token.Claims.(jwt.MapClaims)
	profile := &AppleProfile{}
	if v, ok := claims["sub"]; ok {
		if profile.ID, ok = v.(string); !ok {
			return nil, errors.New("apple id token sub field invalid")
		}
	} else {
		return nil, errors.New("apple id token sub field missing")
	}
	if v, ok := claims["email"]; ok {
		if profile.Email, ok = v.(string); !ok {
			return nil, errors.New("apple id token email field invalid")
		}
	}
	if v, ok := claims["email_verified"]; ok {
		switch v.(type) {
		case bool:
			profile.EmailVerified = v.(bool)
		case string:
			vb, err := strconv.ParseBool(v.(string))
			if err != nil {
				return nil, errors.New("apple id token email_verified field invalid")
			}
			profile.EmailVerified = vb
		default:
			return nil, errors.New("apple id token email_verified field unknown")
		}
	}

	return profile, nil
}

func (c *Client) request(ctx context.Context, provider, path string, headers map[string]string, to interface{}) error {
	body, err := c.requestRaw(ctx, provider, path, headers)
	if err != nil {
		return err
	}
	err = json.Unmarshal(body, to)
	if err != nil {
		c.logger.Warn("error decoding social response", zap.String("provider", provider), zap.Error(err))
		return err
	}
	return nil
}

func (c *Client) requestRaw(ctx context.Context, provider, path string, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequest("GET", path, nil)
	if err != nil {
		c.logger.Warn("error constructing social request", zap.String("provider", provider), zap.Error(err))
		return nil, err
	}
	req = req.WithContext(ctx)
	for k, v := range headers {
		req.Header.Add(k, v)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		c.logger.Warn("error executing social request", zap.String("provider", provider), zap.Error(err))
		return nil, err
	}
	body, err := ioutil.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		c.logger.Warn("error reading social response", zap.String("provider", provider), zap.Error(err))
		return nil, err
	}
	switch resp.StatusCode {
	case 200:
		return body, nil
	case 401:
		return nil, fmt.Errorf("%v error url %v, status code %v, body %s", provider, path, resp.StatusCode, body)
	default:
		c.logger.Warn("error response code from social request", zap.String("provider", provider), zap.Int("code", resp.StatusCode))
		return nil, fmt.Errorf("%v error url %v, status code %v, body %s", provider, path, resp.StatusCode, body)
	}
}
