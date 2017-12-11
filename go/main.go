package main

/*

Copyright 2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.

Licensed under the Apache License, Version 2.0 (the "License"). You may not use this file except in compliance with the License. A copy of the License is located at

    http://aws.amazon.com/apache2.0/

or in the "license" file accompanying this file. This file is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific language governing permissions and limitations under the License.

*/

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/gob"
	"encoding/json"
	"html/template"
	"log"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/twitch"
)

func init() {
	// Sessions are serialised using the encoding/gob package, so it is easy
	// to register new datatypes for storage in sessions:
	// http://www.gorillatoolkit.org/pkg/sessions#overview
	gob.Register(&oauth2.Token{})
}

var (
	// Populate an OAuth2 config object with our app secrets
	oauthConfig = &oauth2.Config{
		RedirectURL:  "<YOUR REDIRECT URL HERE>",  // You can run locally with - http://localhost:3000/auth/twitch/callback
		ClientID:     "<YOUR CLIENT ID HERE>",     // The client ID assigned when you created your application
		ClientSecret: "<YOUR CLIENT SECRET HERE>", // The client secret assigned when you created your application
		Scopes:       []string{"user_read"},       // The scopes you would like to request
		Endpoint:     twitch.Endpoint,
	}
	sessionSecret = []byte("<SOME SECRET HERE>")

	oauthSession = "oauth-session"
	// http client session datastore
	store = sessions.NewCookieStore(sessionSecret)
)

func main() {
	routes := mux.NewRouter()
	routes.HandleFunc("/", index).Methods("GET")
	routes.HandleFunc("/auth/twitch", handleTwitchLogin).Methods("GET")
	routes.HandleFunc("/auth/twitch/callback", handleTwitchCallback).Methods("GET")
	log.Fatal(http.ListenAndServe(":3000", routes))
}

type viewData struct {
	Token   *oauth2.Token
	Profile map[string]interface{}
}

func index(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	session, err := store.Get(r, oauthSession)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	t, err := template.ParseFiles("templates/index.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	token, _ := session.Values["token"].(*oauth2.Token)
	var ctx *viewData
	if token != nil {
		user, err := userProfile(token)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		ctx = &viewData{
			Token:   token,
			Profile: user,
		}
	}

	t.Execute(w, ctx)
}

func handleTwitchLogin(w http.ResponseWriter, r *http.Request) {
	session, err := store.Get(r, oauthSession)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// randomBytes is an OAuth 2.0 opaque value, used to avoid CSRF attacks
	randomBytes := make([]byte, 32)
	_, err = rand.Read(randomBytes)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// encode randomBytes to ASCII
	state := base64.StdEncoding.EncodeToString(randomBytes)
	session.Values["state"] = state
	session.Save(r, w)

	// generate OAuth Authorization Code Flow url
	url := oauthConfig.AuthCodeURL(state)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func handleTwitchCallback(w http.ResponseWriter, r *http.Request) {
	// Check given state against previously stored one to mitigate CSRF attack
	session, err := store.Get(r, oauthSession)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	params := r.URL.Query()
	if getString(session.Values["state"]) == params.Get("state") {
		// use oauthConfig to convert an authorization code into a token.
		token, err := oauthConfig.Exchange(context.Background(), params.Get("code"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Twitch API v5 uses `OAuth` as auth type
		// New Twitch API uses `Bearer`
		// https://dev.twitch.tv/docs/authentication
		token.TokenType = "OAuth"
		session.Values["token"] = token
	} else {
		// user login and server state is out of sync or CSRF an attack has been tried.. retry
		delete(session.Values, "state")
	}

	session.Save(r, w)
	http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
}

// getString is a helper function to retrieve interface{} values from a map.
// Returns an empty string the object is nil or not a string, else return the string value
func getString(i interface{}) string {
	if i == nil {
		return ""
	}
	str, ok := i.(string)
	if !ok {
		return ""
	}
	return str
}

// userProfile gets the current user profile based on the given OAuth2 token
func userProfile(token *oauth2.Token) (map[string]interface{}, error) {
	client := oauthConfig.Client(context.Background(), token)
	req, err := http.NewRequest(http.MethodGet, "https://api.twitch.tv/kraken/user", nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Client-ID", oauthConfig.ClientID)
	token.SetAuthHeader(req)
	req.Header.Set("Accept", "application/vnd.twitchtv.v5+json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	var response map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&response)
	if err != nil {
		return nil, err
	}

	return response, nil
}
