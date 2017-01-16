package googlephotos

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/mholt/photobak"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

func init() {
	// Get OAuth2 credentials from https://console.developers.google.com
	oauth2Config.ClientID = os.Getenv("GOOGLEPHOTOS_CLIENT_ID")
	oauth2Config.ClientSecret = os.Getenv("GOOGLEPHOTOS_CLIENT_SECRET")
}

// getToken gets an OAuth2 token from the user.
func getToken(username string) ([]byte, error) {
	if oauth2Config.ClientID == "" || oauth2Config.ClientSecret == "" {
		return nil, fmt.Errorf("missing client ID and/or secret env variables; create OAuth 2.0 client ID at console.developers.google.com")
	}

	fmt.Println("Photobak needs authorization to access the photos and")
	fmt.Printf("videos for %s. To obtain this, a browser\n", username)
	fmt.Println("tab will be opened where you can grant access.")
	fmt.Println("Press [ENTER] to continue.")
	fmt.Scanln()

	token, err := getNewToken(oauth2Config)
	if err != nil {
		return nil, err
	}

	// no particular reason we use JSON except that
	// we used to write it to a file and JSON just
	// seemed more sensible if a human needed to
	// inspect it; also the type is external and
	// struct tags may afford more compatibility than
	// a gob encoding, should the type def change.
	tokenJSON, err := json.Marshal(token)
	if err != nil {
		return nil, err
	}

	return tokenJSON, nil
}

// newClient returns an authenticated Client given the
// token data.
func newClient(tokenData []byte) (photobak.Client, error) {
	oauthClient, err := newOAuth2Client(tokenData)
	if err != nil {
		return nil, err
	}
	return &Client{HTTPClient: oauthClient}, nil
}

// newOAuth2Client gives a new authenticated http.Client
// given the token data.
func newOAuth2Client(tokenData []byte) (*http.Client, error) {
	var token *oauth2.Token
	err := json.Unmarshal(tokenData, &token)
	if err != nil {
		return nil, fmt.Errorf("parsing token data: %v", err)
	}
	return oauth2Config.Client(oauth2.NoContext, token), nil
}

// getNewToken will get a new OAuth2 token from the user
// by opening the browser for them.
func getNewToken(conf *oauth2.Config) (*oauth2.Token, error) {
	log.Println("Getting new OAuth2 token")

	cbURL, err := url.Parse(conf.RedirectURL)
	if err != nil {
		return nil, fmt.Errorf("bad redirect URL: %v", err)
	}

	stateVal := randString(14)

	ln, err := net.Listen("tcp", "localhost:5013")
	if err != nil {
		return nil, err
	}
	defer ln.Close()

	ch := make(chan *oauth2.Token)
	errCh := make(chan error)

	go func() {
		http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			state := r.FormValue("state")
			code := r.FormValue("code")

			if r.Method != "GET" || r.URL.Path != cbURL.Path || state == "" || code == "" {
				http.Error(w, "This endpoint is for OAuth2 callbacks only", http.StatusNotFound)
				return
			}

			if state != stateVal {
				errCh <- fmt.Errorf("invalid OAuth2 state; expected '%s' but got '%s'", stateVal, state)
				http.Error(w, "invalid state", http.StatusUnauthorized)
				return
			}

			token, err := conf.Exchange(oauth2.NoContext, code)
			if err != nil {
				errCh <- fmt.Errorf("code exchange failed: %v", err)
				http.Error(w, "code exchange failed", http.StatusUnauthorized)
				return
			}

			ch <- token

			w.WriteHeader(http.StatusOK)
			fmt.Fprintln(w, successBody)
		}))
	}()

	url := conf.AuthCodeURL(stateVal, oauth2.AccessTypeOffline)

	err = openBrowser(url)
	if err != nil {
		return nil, err
	}

	select {
	case token := <-ch:
		fmt.Println("[ OK ] Successfully authenticated. Performing backup (could take hours)...")
		return token, nil
	case err := <-errCh:
		return nil, err
	}
}

// openBrowser opens the browser to url.
func openBrowser(url string) error {
	osCommand := map[string][]string{
		"darwin":  []string{"open"},
		"freebsd": []string{"xdg-open"},
		"linux":   []string{"xdg-open"},
		"netbsd":  []string{"xdg-open"},
		"openbsd": []string{"xdg-open"},
		"windows": []string{"cmd", "/c", "start"},
	}

	if runtime.GOOS == "windows" {
		// escape characters not allowed by cmd
		url = strings.Replace(url, "&", `^&`, -1)
	}

	all := osCommand[runtime.GOOS]
	exe := all[0]
	args := all[1:]

	cmd := exec.Command(exe, append(args, url)...)
	return cmd.Run()
}

func randString(n int) string {
	const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letterBytes[rand.Intn(len(letterBytes))]
	}
	return string(b)
}

var oauth2Config = &oauth2.Config{
	RedirectURL: "http://localhost:5013/photobak-oauth-" + randString(5),
	Scopes:      []string{"https://picasaweb.google.com/data/"},
	Endpoint:    google.Endpoint,
}

const successBody = `<!DOCTYPE html>
<html>
	<head>
		<title>Authorization Successful</title>
		<meta charset="utf-8">
		<style>
			body { text-align: center; padding: 5%; font-family: sans-serif; }
			h1 { font-size: 20px; }
			p { font-size: 16px; color: #444; }
		</style>
	</head>
	<body>
		<h1>Authorization successful, thank you!</h1>
		<p>
			You may now close this window and return to the program.
		</p>
	</body>
</html>
`
