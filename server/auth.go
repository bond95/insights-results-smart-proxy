// Auth implementation based on JWT

/*
Copyright © 2019, 2020 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/RedHatInsights/insights-operator-utils/collections"
	"github.com/RedHatInsights/insights-operator-utils/types"
	"github.com/dgrijalva/jwt-go"
	"github.com/rs/zerolog/log"
)

const (
	// #nosec G101
	malformedTokenMessage = "Malformed authentication token"
)

// Authentication middleware for checking auth rights
func (server *HTTPServer) Authentication(next http.Handler, noAuthURLs []string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		// for specific URLs it is ok to not use auth. mechanisms at all
		if collections.StringInSlice(r.RequestURI, noAuthURLs) || r.Method == "OPTIONS" {
			next.ServeHTTP(w, r)
			return
		}

		token, isTokenValid := server.getAuthTokenHeader(w, r)
		if !isTokenValid {
			// everything has been handled already
			return
		}

		decoded, err := jwt.DecodeSegment(token) // Decode token to JSON string
		if err != nil {                          // Malformed token, returns with http code 403 as usual
			log.Error().Err(err).Msg(malformedTokenMessage)
			handleServerError(w, &AuthenticationError{errString: malformedTokenMessage})
			return
		}

		tk := &types.Token{}

		// If we took JWT token, it has different structure then x-rh-identity
		if server.Config.AuthType == "jwt" {
			jwtPayload := &types.JWTPayload{}
			err = json.Unmarshal(decoded, jwtPayload)
			if err != nil { //Malformed token, returns with http code 403 as usual
				log.Error().Err(err).Msg(malformedTokenMessage)
				handleServerError(w, &AuthenticationError{errString: malformedTokenMessage})
				return
			}
			// Map JWT token to inner token
			tk.Identity = types.Identity{
				AccountNumber: jwtPayload.AccountNumber,
				Internal: types.Internal{
					OrgID: jwtPayload.OrgID,
				},
			}
		} else {
			err = json.Unmarshal(decoded, tk)

			if err != nil { //Malformed token, returns with http code 403 as usual
				log.Error().Err(err).Msg(malformedTokenMessage)
				handleServerError(w, &AuthenticationError{errString: malformedTokenMessage})
				return
			}
		}

		// Everything went well, proceed with the request and set the caller to the user retrieved from the parsed token
		ctx := context.WithValue(r.Context(), types.ContextKeyUser, tk.Identity)
		r = r.WithContext(ctx)

		next.ServeHTTP(w, r)
	})
}

// GetCurrentUserID retrieves current user's id from request
func (server *HTTPServer) GetCurrentUserID(request *http.Request) (types.UserID, error) {
	i := request.Context().Value(types.ContextKeyUser)

	if i == nil {
		return "", &AuthenticationError{errString: "user id is not provided"}
	}

	identity, ok := i.(types.Identity)
	if !ok {
		return "", fmt.Errorf("contextKeyUser has wrong type")
	}

	return identity.AccountNumber, nil
}

// GetAuthToken returns current authentication token
func (server *HTTPServer) GetAuthToken(request *http.Request) (*types.Identity, error) {
	i := request.Context().Value(types.ContextKeyUser)

	if i == nil {
		return nil, &AuthenticationError{errString: "token is not provided"}
	}

	identity, ok := i.(types.Identity)
	if !ok {
		return nil, fmt.Errorf("contextKeyUser has wrong type")
	}

	return &identity, nil
}

func (server *HTTPServer) getAuthTokenHeader(w http.ResponseWriter, r *http.Request) (string, bool) {
	var tokenHeader string
	// In case of testing on local machine we don't take x-rh-identity header, but instead Authorization with JWT token in it
	if server.Config.AuthType == "jwt" {
		tokenHeader = r.Header.Get("Authorization") //Grab the token from the header
		splitted := strings.Split(tokenHeader, " ") //The token normally comes in format `Bearer {token-body}`, we check if the retrieved token matched this requirement
		if len(splitted) != 2 {
			const message = "Invalid/Malformed auth token"
			log.Error().Msg(message)
			handleServerError(w, &AuthenticationError{errString: message})
			return "", false
		}

		// Here we take JWT token which include 3 parts, we need only second one
		splitted = strings.Split(splitted[1], ".")
		tokenHeader = splitted[1]
	} else {
		tokenHeader = r.Header.Get("x-rh-identity") //Grab the token from the header
	}
	// Token is missing, SHOULD RETURN with error code 403 Unauthorized - changes in utils necessary
	// TODO: Change SendUnauthorized in utils to accept string instead of map interface and change here
	if tokenHeader == "" {
		const message = "Missing auth token"
		log.Error().Msg(message)
		handleServerError(w, &AuthenticationError{errString: message})
		return "", false
	}

	return tokenHeader, true
}
