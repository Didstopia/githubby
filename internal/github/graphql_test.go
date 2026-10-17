package github

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/jarcoal/httpmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGraphQL(t *testing.T) {
	httpmock.Activate()
	defer httpmock.DeactivateAndReset()

	client := NewRESTClient("test-token")
	ctx := context.Background()

	t.Run("decodes data and sends auth", func(t *testing.T) {
		httpmock.RegisterResponder("POST", "https://api.github.com/graphql", func(req *http.Request) (*http.Response, error) {
			body, _ := io.ReadAll(req.Body)
			if req.Header.Get("Authorization") != "Bearer test-token" || !strings.Contains(string(body), `"login":"x"`) {
				return httpmock.NewStringResponse(400, "{}"), nil
			}
			return httpmock.NewStringResponse(200, `{"data":{"viewer":{"login":"x"}}}`), nil
		})
		var out struct{ Viewer struct{ Login string } }
		require.NoError(t, GraphQL(ctx, client, "query { viewer { login } }", map[string]any{"login": "x"}, &out))
		assert.Equal(t, "x", out.Viewer.Login)
	})

	t.Run("errors come back typed with the data", func(t *testing.T) {
		httpmock.RegisterResponder("POST", "https://api.github.com/graphql",
			httpmock.NewStringResponder(200, `{"data":{"viewer":{"login":"x"}},"errors":[{"type":"FORBIDDEN","message":"nope"},{"message":"plain"}]}`))
		var out struct{ Viewer struct{ Login string } }
		err := GraphQL(ctx, client, "q", nil, &out)
		var gqlErr *GraphQLError
		require.ErrorAs(t, err, &gqlErr)
		assert.True(t, gqlErr.HasType("FORBIDDEN"))
		assert.False(t, gqlErr.HasType("RATE_LIMITED"))
		assert.Equal(t, "graphql: FORBIDDEN: nope; plain", gqlErr.Error())
		assert.Equal(t, "x", out.Viewer.Login)
	})

	t.Run("bad data and HTTP errors", func(t *testing.T) {
		httpmock.RegisterResponder("POST", "https://api.github.com/graphql", httpmock.NewStringResponder(200, `{"data":{"viewer":5}}`))
		var out struct{ Viewer struct{ Login string } }
		assert.ErrorContains(t, GraphQL(ctx, client, "q", nil, &out), "decode reply")

		httpmock.RegisterResponder("POST", "https://api.github.com/graphql", httpmock.NewStringResponder(502, `{"message":"bad gateway"}`))
		assert.Error(t, GraphQL(ctx, client, "q", nil, &out))
	})
}
