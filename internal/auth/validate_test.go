package auth

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateToken_Success(t *testing.T) {
	var gotURL, gotAuth string
	fakeNetwork(t, func(r *http.Request) (*http.Response, error) {
		gotURL = r.URL.String()
		gotAuth = r.Header.Get("Authorization")
		return jsonResp(200, `{"login":"octo","name":"Octo Cat","email":"o@example.com","avatar_url":"https://a/x.png"}`), nil
	})
	u, err := ValidateToken(context.Background(), "tok", "")
	require.NoError(t, err)
	assert.Equal(t, &AuthenticatedUser{Login: "octo", Name: "Octo Cat", Email: "o@example.com", AvatarURL: "https://a/x.png"}, u)
	assert.Equal(t, "https://api.github.com/user", gotURL)
	assert.Equal(t, "Bearer tok", gotAuth)
}

func TestValidateToken_Enterprise(t *testing.T) {
	var gotURL string
	fakeNetwork(t, func(r *http.Request) (*http.Response, error) {
		gotURL = r.URL.String()
		return jsonResp(200, `{"login":"ent"}`), nil
	})
	u, err := ValidateToken(context.Background(), "tok", "ghe.example.com")
	require.NoError(t, err)
	assert.Equal(t, "ent", u.Login)
	assert.Equal(t, "https://ghe.example.com/api/v3/user", gotURL)
}

func TestValidateToken_BadHostname(t *testing.T) {
	_, err := ValidateToken(context.Background(), "tok", "bad host\x7f")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "enterprise client")
}

func TestValidateToken_Unauthorized(t *testing.T) {
	fakeNetwork(t, func(r *http.Request) (*http.Response, error) {
		return jsonResp(401, `{"message":"Bad credentials"}`), nil
	})
	_, err := ValidateToken(context.Background(), "tok", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to validate token")
}
