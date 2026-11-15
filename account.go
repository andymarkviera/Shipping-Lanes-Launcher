package main

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net"
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

func (l *launcher) beginSignIn(host string) error {
	_, port, err := net.SplitHostPort(host)
	if err != nil {
		return err
	}
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return err
	}
	l.accountState = hex.EncodeToString(bytes[:])
	l.accountDeadline = time.Now().Add(10 * time.Minute)
	target := accountSite + "/account?" + url.Values{"state": {l.accountState}, "port": {port}}.Encode()
	state := l.accountState
	go func() {
		if err := openURL(target); err != nil {
			l.mu.Lock()
			if l.accountState == state {
				l.accountState = ""
				l.accountMessage = "Could not open sign-in in your browser."
			}
			l.mu.Unlock()
		}
	}()
	return nil
}

func (l *launcher) callback(w http.ResponseWriter, r *http.Request, origin string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'")
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, callbackPage)
	case http.MethodPost:
		if r.Header.Get("Origin") != origin {
			http.Error(w, "Invalid sign-in origin", 403)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 2048))
		if err != nil {
			http.Error(w, "Invalid sign-in", 400)
			return
		}
		l.mu.Lock()
		defer l.mu.Unlock()
		now := time.Now()
		claim, err := verifyAccount(string(body), l.accountState, now)
		if err != nil || now.After(l.accountDeadline) {
			http.Error(w, "Sign-in expired. Start again in the launcher.", 403)
			return
		}
		l.accountState = ""
		l.accountName = claim.Username
		l.accountSubject = claim.Subject
		l.accountMessage = ""
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

const callbackPage = `<!doctype html><html lang="en"><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Shipping Lanes sign-in</title><main><h1>Shipping Lanes</h1><p id="status" role="status">Connecting your account…</p></main>
<script>
const statusText = document.querySelector('#status');
const proof = location.hash.slice(1);
history.replaceState(null, '', location.pathname);
if (!proof) {
  statusText.textContent = 'Start sign-in from the launcher.';
} else {
  fetch('/account-callback',{method:'POST',body:proof}).then(async response => {
    if (!response.ok) throw new Error();
    statusText.textContent = 'Signed in. You can close this tab and return to the desktop launcher.';
  }).catch(() => {
    statusText.textContent = 'Sign-in expired. Return to the launcher and try again.';
  });
}
</script></html>`
