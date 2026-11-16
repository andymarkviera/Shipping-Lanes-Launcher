package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const accountSite = "https://shipping-lanes.andyviera578.chatgpt.site"
const publicKey = "MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEXLRrOFBp63y6jPb2Q/YoqB+6+h4HEGLJyur4jmTn9mAwsrLrGxfwExAft/fkh9Vl5kYPS0LBMdlo2rVdLTM55g=="

var usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9_]{3,20}$`)

type accountClaim struct {
	Audience string `json:"aud"`
	Subject  string `json:"sub"`
	Username string `json:"username"`
	State    string `json:"state"`
	Expires  int64  `json:"expires"`
}

func verifyAccount(proof, state string, now time.Time) (accountClaim, error) {
	var claim accountClaim
	invalid := errors.New("invalid or expired account sign-in")
	message, signature, found := strings.Cut(proof, ".")
	if !found || len(proof) > 2048 || state == "" {
		return claim, invalid
	}
	decoder := base64.RawURLEncoding
	payload, payloadErr := decoder.DecodeString(message)
	sig, sigErr := decoder.DecodeString(signature)
	if payloadErr != nil || sigErr != nil || len(sig) != 64 {
		return claim, invalid
	}
	encodedKey, _ := base64.StdEncoding.DecodeString(publicKey)
	parsed, err := x509.ParsePKIXPublicKey(encodedKey)
	key, ok := parsed.(*ecdsa.PublicKey)
	if err != nil || !ok {
		return claim, invalid
	}
	digest := sha256.Sum256(payload)
	if !ecdsa.Verify(key, digest[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		return claim, invalid
	}
	if json.Unmarshal(payload, &claim) != nil || claim.Audience != "shipping-lanes-launcher" ||
		claim.State != state || claim.Expires < now.Unix() || claim.Expires > now.Add(2*time.Minute).Unix() ||
		claim.Subject == "" || !usernamePattern.MatchString(claim.Username) {
		return accountClaim{}, invalid
	}
	return claim, nil
}

func (l *launcher) beginSignIn() error {
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return err
	}
	secret := hex.EncodeToString(bytes[:])
	deadline := time.Now().Add(10 * time.Minute)
	l.accountState = secret
	challenge := fmt.Sprintf("%x", sha256.Sum256([]byte(secret)))
	target := accountSite + "/account?" + url.Values{"device": {challenge}}.Encode()
	go func() {
		if err := openURL(target); err != nil {
			l.finishSignIn(secret, accountClaim{}, "Could not open sign-in in your browser.")
			return
		}
		l.pollSignIn(secret, challenge, deadline)
	}()
	return nil
}

func (l *launcher) pollSignIn(secret, challenge string, deadline time.Time) {
	ctx, cancel := context.WithDeadline(l.ctx, deadline)
	defer cancel()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		l.mu.Lock()
		current := l.accountState == secret
		l.mu.Unlock()
		if !current {
			return
		}
		proof := l.fetchProof(ctx, secret)
		if proof != "" {
			claim, err := verifyAccount(proof, challenge, time.Now())
			if err == nil {
				l.finishSignIn(secret, claim, "")
				return
			}
			l.finishSignIn(secret, accountClaim{}, "Sign-in expired. Try again from the launcher.")
			return
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			l.finishSignIn(secret, accountClaim{}, "Sign-in expired. Try again from the launcher.")
			return
		}
	}
}

func (l *launcher) fetchProof(ctx context.Context, secret string) string {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, accountSite+"/account/device", strings.NewReader(secret))
	if err != nil {
		return ""
	}
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("User-Agent", "Shipping-Lanes-Launcher/1.2.1")
	// The redeem request contains a private challenge secret. Never redirect it.
	client := http.Client{Transport: l.client.Transport, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	result, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer result.Body.Close()
	if result.StatusCode != http.StatusOK {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(result.Body, 2049))
	if err != nil || len(body) > 2048 {
		return ""
	}
	return string(body)
}

func (l *launcher) finishSignIn(secret string, claim accountClaim, message string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.accountState != secret {
		return
	}
	l.accountState = ""
	l.accountName = claim.Username
	l.accountSubject = claim.Subject
	l.accountMessage = message
}
