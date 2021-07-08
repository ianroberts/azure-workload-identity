package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Azure/go-autorest/autorest"
	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/confidential"
	"github.com/pkg/errors"
)

// authResult contains the subset of results from token acquisition operation in ConfidentialClientApplication
// For details see https://aka.ms/msal-net-authenticationresult
type authResult struct {
	accessToken    string
	expiresOn      time.Time
	grantedScopes  []string
	declinedScopes []string
}

func clientAssertionBearerAuthorizerCallback(tenantID, resource string) (*autorest.BearerAuthorizer, error) {
	// AAD Pod Identity webhook will inject the following env vars
	// 	AZURE_CLIENT_ID with the clientID set in the service account annotation
	// 	AZURE_TENANT_ID with the tenantID set in the service account annotation. If not defined, then
	// 	the tenantID provided via aad-pi-webhook-config for the webhook will be used.
	// 	TOKEN_FILE_PATH is the service account token path
	clientID := os.Getenv("AZURE_CLIENT_ID")
	tokenFilePath := os.Getenv("TOKEN_FILE_PATH")

	// generate a token using the msal confidential client
	// this will always generate a new token request to AAD
	// TODO (aramase) consider using acquire token silent (https://github.com/Azure/aad-pod-managed-identity/issues/76)

	// read the service account token from the filesystem
	signedAssertion, err := readJWTFromFS(tokenFilePath)
	if err != nil {
		return nil, errors.Errorf("failed to read service account token: %v", err)
	}
	cred, err := confidential.NewCredFromAssertion(signedAssertion)
	if err != nil {
		return nil, errors.Errorf("failed to create confidential creds: %v", err)
	}
	// create the confidential client to request an AAD token
	confidentialClientApp, err := confidential.New(
		clientID,
		cred,
		confidential.WithAuthority(fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/token", tenantID)))
	if err != nil {
		return nil, errors.Errorf("failed to create confidential client app: %v", err)
	}

	// trim the suffix / if exists
	resource = strings.TrimSuffix(resource, "/")
	// .default needs to be added to the scope
	if !strings.HasSuffix(resource, ".default") {
		resource += "/.default"
	}

	result, err := confidentialClientApp.AcquireTokenByCredential(context.Background(), []string{resource})
	if err != nil {
		return nil, errors.Errorf("failed to get token: %v", err)
	}

	return autorest.NewBearerAuthorizer(authResult{
		accessToken:    result.AccessToken,
		expiresOn:      result.ExpiresOn,
		grantedScopes:  result.GrantedScopes,
		declinedScopes: result.DeclinedScopes,
	}), nil
}

// OAuthToken implements the OAuthTokenProvider interface.  It returns the current access token.
func (ar authResult) OAuthToken() string {
	return ar.accessToken
}

// readJWTFromFS reads the jwt from file system
func readJWTFromFS(tokenFilePath string) (string, error) {
	token, err := os.ReadFile(tokenFilePath)
	if err != nil {
		return "", err
	}
	return string(token), nil
}