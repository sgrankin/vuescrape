package vueclient

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	cognitosrp "github.com/alexrudd/cognito-srp/v4"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"
	"golang.org/x/oauth2"
)

type Cognito struct {
	Region   string
	ClientID string
	UserPool string // region_guid
}

type Token struct {
	oauth2.Token

	// IDToken is a JWT that contains identity claims of the user.
	IDToken string `json:"id_token,omitempty"`
}

func (c *Cognito) Auth(ctx context.Context, username, password string) (*Token, error) {
	now := time.Now() // For calculating expiration once we authd.
	idp := c.idp(ctx)
	csrp, err := cognitosrp.NewCognitoSRP(username, password, c.UserPool, c.ClientID, nil)
	if err != nil {
		return nil, fmt.Errorf("srp: %w", err)
	}
	authResp, err := idp.InitiateAuth(ctx, &cognitoidentityprovider.InitiateAuthInput{
		AuthFlow:       types.AuthFlowTypeUserSrpAuth,
		ClientId:       aws.String(csrp.GetClientId()),
		AuthParameters: csrp.GetAuthParams(),
	})
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}
	if authResp.ChallengeName != types.ChallengeNameTypePasswordVerifier {
		return nil, fmt.Errorf("unsupported challenge type: %s", authResp.ChallengeName)
	}
	challengeResponses, _ := csrp.PasswordVerifierChallenge(authResp.ChallengeParameters, time.Now())

	challengeResp, err := idp.RespondToAuthChallenge(ctx, &cognitoidentityprovider.RespondToAuthChallengeInput{
		ChallengeName:      types.ChallengeNameTypePasswordVerifier,
		ChallengeResponses: challengeResponses,
		ClientId:           aws.String(csrp.GetClientId()),
	})
	if err != nil {
		return nil, fmt.Errorf("challenge response: %w", err)
	}
	return mkToken(challengeResp.AuthenticationResult, now, ""), nil
}

func (c *Cognito) Refresh(ctx context.Context, refreshToken string) (*Token, error) {
	now := time.Now() // For calculating expiration once we authd.
	idp := c.idp(ctx)
	authResp, err := idp.InitiateAuth(ctx, &cognitoidentityprovider.InitiateAuthInput{
		AuthFlow:       types.AuthFlowTypeRefreshToken,
		ClientId:       aws.String(c.ClientID),
		AuthParameters: map[string]string{"REFRESH_TOKEN": refreshToken},
	})
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}
	log.Printf("%+v", authResp)
	log.Printf("%+v", authResp.AuthenticationResult)
	log.Printf("%+v", authResp.AuthenticationResult)
	log.Printf("%+v", authResp.ChallengeName)
	log.Printf("%+v", authResp.ChallengeParameters)
	return mkToken(authResp.AuthenticationResult, now, refreshToken), nil
}

func (c *Cognito) idp(ctx context.Context) *cognitoidentityprovider.Client {
	cfg, _ := config.LoadDefaultConfig(
		ctx,
		config.WithRegion(c.Region),
		config.WithCredentialsProvider(aws.AnonymousCredentials{}),
	)
	return cognitoidentityprovider.NewFromConfig(cfg)
}

func mkToken(auth *types.AuthenticationResultType, now time.Time, refreshToken string) *Token {
	tok := &Token{
		Token: oauth2.Token{
			AccessToken:  *auth.AccessToken,
			TokenType:    *auth.TokenType,
			RefreshToken: refreshToken,
			Expiry:       now.Add(time.Duration(auth.ExpiresIn) * time.Second),
		},
		IDToken: *auth.IdToken,
	}
	if auth.RefreshToken != nil {
		// Auth results after a refresh don't carry the refresh token. ಠ_ಠ
		tok.RefreshToken = *auth.RefreshToken
	}
	return tok
}

type CognitoTokenSource struct {
	*Cognito

	// The current token.  May be set to create an initial token.
	Tok *Atom[*Token]

	// AuthFunc is used to get a username & password if initial auth is needed.
	AuthFunc func() (string, string, error)

	mu sync.Mutex
}

func (c *CognitoTokenSource) Token() (*Token, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	tok := c.Tok.Load()
	if tok.Valid() {
		return tok, nil
	}
	ctx := context.Background()
	if tok.RefreshToken != "" {
		tok, err := c.Cognito.Refresh(ctx, tok.RefreshToken)
		if err != nil {
			return nil, fmt.Errorf("refresh: %w", err)
		}
		c.Tok.Reset(tok)
		return tok, nil
	}

	if c.AuthFunc == nil {
		return nil, fmt.Errorf("token is expired and AuthFunc is not set")
	}
	username, password, err := c.AuthFunc()
	if err != nil {
		return nil, fmt.Errorf("get auth: %w", err)
	}
	tok, err = c.Cognito.Auth(ctx, username, password)
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}
	c.Tok.Reset(tok)
	return tok, nil
}

type cognitoAuthTransport struct {
	// Source supplies the token to add to outgoing requests' Authorization headers.
	Source *CognitoTokenSource

	// Base is the base RoundTripper used to make HTTP requests.
	// If nil, http.DefaultTransport is used.
	Base http.RoundTripper
}

// RoundTrip authorizes and authenticates the request with an access token from Transport's Source.
func (t *cognitoAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	reqBodyClosed := false
	if req.Body != nil {
		defer func() {
			if !reqBodyClosed {
				req.Body.Close()
			}
		}()
	}

	if t.Source == nil {
		return nil, errors.New("Transport's Source is nil")
	}
	token, err := t.Source.Token()
	if err != nil {
		return nil, err
	}

	req2 := req.Clone(req.Context()) // per RoundTripper contract
	req2.Header.Set("authtoken", token.IDToken)

	reqBodyClosed = true // req.Body is assumed to be closed by the base RoundTripper.
	return t.base().RoundTrip(req2)
}

func (t *cognitoAuthTransport) base() http.RoundTripper {
	if t.Base != nil {
		return t.Base
	}
	return http.DefaultTransport
}
