package github

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	gh "github.com/google/go-github/v86/github"
	"golang.org/x/oauth2"
)

// NewRESTClient returns a plain go-github client that sends the token
func NewRESTClient(token string) *gh.Client {
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	return gh.NewClient(oauth2.NewClient(context.Background(), ts))
}

// GraphQLErrorItem is one entry of a GraphQL "errors" list
type GraphQLErrorItem struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// GraphQLError is returned when a GraphQL reply carries errors.
// The data that did come back is still decoded.
type GraphQLError struct {
	Errors []GraphQLErrorItem
	Rate   gh.Rate
}

func (e *GraphQLError) Error() string {
	msgs := make([]string, 0, len(e.Errors))
	for _, item := range e.Errors {
		if item.Type != "" {
			msgs = append(msgs, item.Type+": "+item.Message)
		} else {
			msgs = append(msgs, item.Message)
		}
	}
	return "graphql: " + strings.Join(msgs, "; ")
}

// HasType reports whether any error has one of the given types
func (e *GraphQLError) HasType(types ...string) bool {
	for _, item := range e.Errors {
		for _, t := range types {
			if item.Type == t {
				return true
			}
		}
	}
	return false
}

// GraphQL posts a query to the API's /graphql endpoint with the client's auth,
// and decodes the "data" part into out
func GraphQL(ctx context.Context, client *gh.Client, query string, vars map[string]any, out any) error {
	req, err := client.NewRequest(ctx, "POST", "graphql", map[string]any{"query": query, "variables": vars})
	if err != nil {
		return err
	}

	var body struct {
		Data   json.RawMessage    `json:"data"`
		Errors []GraphQLErrorItem `json:"errors"`
	}
	resp, err := client.Do(req, &body)
	if err != nil {
		return err
	}

	if len(body.Data) > 0 && string(body.Data) != "null" {
		if err := json.Unmarshal(body.Data, out); err != nil {
			return fmt.Errorf("graphql: decode reply: %w", err)
		}
	}

	if len(body.Errors) > 0 {
		gqlErr := &GraphQLError{Errors: body.Errors}
		if resp != nil {
			gqlErr.Rate = resp.Rate
		}
		return gqlErr
	}
	return nil
}
