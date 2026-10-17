package metadata

import (
	"context"
	"encoding/json"
	"fmt"
)

// pageInfo is GraphQL cursor paging
type pageInfo struct {
	HasNextPage bool   `json:"hasNextPage"`
	EndCursor   string `json:"endCursor"`
}

// connection is a GraphQL list page; PageInfo is dropped once every page is in
type connection[T any] struct {
	TotalCount int       `json:"totalCount"`
	PageInfo   *pageInfo `json:"pageInfo,omitempty"`
	Nodes      []T       `json:"nodes"`
}

// Discussion is a repo discussion with all its comments and replies
type Discussion struct {
	ID        string                        `json:"id"`
	Number    int                           `json:"number"`
	Title     string                        `json:"title"`
	Body      string                        `json:"body"`
	URL       string                        `json:"url"`
	CreatedAt string                        `json:"createdAt"`
	UpdatedAt string                        `json:"updatedAt"`
	Closed    bool                          `json:"closed"`
	Locked    bool                          `json:"locked"`
	Author    json.RawMessage               `json:"author"`
	Category  json.RawMessage               `json:"category"`
	Labels    json.RawMessage               `json:"labels"`
	Comments  connection[DiscussionComment] `json:"comments"`
}

// DiscussionComment is a top-level discussion comment with its replies
type DiscussionComment struct {
	ID        string                      `json:"id"`
	Body      string                      `json:"body"`
	URL       string                      `json:"url"`
	CreatedAt string                      `json:"createdAt"`
	UpdatedAt string                      `json:"updatedAt"`
	IsAnswer  bool                        `json:"isAnswer"`
	Author    json.RawMessage             `json:"author"`
	Replies   connection[json.RawMessage] `json:"replies"`
}

const replyFields = `id body url createdAt updatedAt author { login }`

const commentFields = `id body url createdAt updatedAt isAnswer author { login }
	replies(first: 50) { totalCount pageInfo { hasNextPage endCursor } nodes { ` + replyFields + ` } }`

const discussionsQuery = `query($owner: String!, $name: String!, $cursor: String) {
  repository(owner: $owner, name: $name) {
    discussions(first: 25, after: $cursor) {
      totalCount
      pageInfo { hasNextPage endCursor }
      nodes {
        id number title body url createdAt updatedAt closed locked
        author { login }
        category { name }
        labels(first: 100) { nodes { name } }
        comments(first: 50) { totalCount pageInfo { hasNextPage endCursor } nodes { ` + commentFields + ` } }
      }
    }
  }
}`

const moreCommentsQuery = `query($id: ID!, $cursor: String) {
  node(id: $id) { ... on Discussion { comments(first: 50, after: $cursor) {
    totalCount pageInfo { hasNextPage endCursor } nodes { ` + commentFields + ` } } } }
}`

const moreRepliesQuery = `query($id: ID!, $cursor: String) {
  node(id: $id) { ... on DiscussionComment { replies(first: 50, after: $cursor) {
    totalCount pageInfo { hasNextPage endCursor } nodes { ` + replyFields + ` } } } }
}`

// discussions returns every discussion of a repo with all comments and replies
func (e *Exporter) discussions(ctx context.Context, owner, name string) ([]Discussion, error) {
	out := make([]Discussion, 0)
	vars := map[string]any{"owner": owner, "name": name, "cursor": nil}
	for {
		var data struct {
			Repository *struct {
				Discussions connection[Discussion] `json:"discussions"`
			} `json:"repository"`
		}
		if err := e.graphql(ctx, discussionsQuery, vars, &data, func() bool { return data.Repository != nil }); err != nil {
			return nil, err
		}
		if data.Repository == nil {
			return nil, fmt.Errorf("repository %s/%s not returned", owner, name)
		}

		page := data.Repository.Discussions
		for i := range page.Nodes {
			if err := e.completeComments(ctx, &page.Nodes[i]); err != nil {
				return nil, err
			}
		}
		out = append(out, page.Nodes...)

		if page.PageInfo == nil || !page.PageInfo.HasNextPage {
			return out, nil
		}
		vars["cursor"] = page.PageInfo.EndCursor
	}
}

// completeComments fetches the comment and reply pages past the first one
func (e *Exporter) completeComments(ctx context.Context, d *Discussion) error {
	for d.Comments.PageInfo != nil && d.Comments.PageInfo.HasNextPage {
		var data struct {
			Node *struct {
				Comments connection[DiscussionComment] `json:"comments"`
			} `json:"node"`
		}
		vars := map[string]any{"id": d.ID, "cursor": d.Comments.PageInfo.EndCursor}
		if err := e.graphql(ctx, moreCommentsQuery, vars, &data, func() bool { return data.Node != nil }); err != nil {
			return err
		}
		if data.Node == nil {
			return fmt.Errorf("discussion %d not returned", d.Number)
		}
		d.Comments.Nodes = append(d.Comments.Nodes, data.Node.Comments.Nodes...)
		d.Comments.PageInfo = data.Node.Comments.PageInfo
	}
	d.Comments.PageInfo = nil

	for i := range d.Comments.Nodes {
		c := &d.Comments.Nodes[i]
		for c.Replies.PageInfo != nil && c.Replies.PageInfo.HasNextPage {
			var data struct {
				Node *struct {
					Replies connection[json.RawMessage] `json:"replies"`
				} `json:"node"`
			}
			vars := map[string]any{"id": c.ID, "cursor": c.Replies.PageInfo.EndCursor}
			if err := e.graphql(ctx, moreRepliesQuery, vars, &data, func() bool { return data.Node != nil }); err != nil {
				return err
			}
			if data.Node == nil {
				return fmt.Errorf("comment %s not returned", c.ID)
			}
			c.Replies.Nodes = append(c.Replies.Nodes, data.Node.Replies.Nodes...)
			c.Replies.PageInfo = data.Node.Replies.PageInfo
		}
		c.Replies.PageInfo = nil
	}
	return nil
}

// Project is a Projects (v2) board with its fields and every item
type Project struct {
	ID               string                      `json:"id"`
	Number           int                         `json:"number"`
	Title            string                      `json:"title"`
	ShortDescription string                      `json:"shortDescription"`
	Readme           string                      `json:"readme"`
	URL              string                      `json:"url"`
	Public           bool                        `json:"public"`
	Closed           bool                        `json:"closed"`
	CreatedAt        string                      `json:"createdAt"`
	UpdatedAt        string                      `json:"updatedAt"`
	Fields           connection[json.RawMessage] `json:"fields"`
	Items            []json.RawMessage           `json:"items"`
}

const projectsQuery = `query($login: String!, $cursor: String) {
  %s(login: $login) {
    projectsV2(first: 20, after: $cursor) {
      pageInfo { hasNextPage endCursor }
      nodes {
        id number title shortDescription readme url public closed createdAt updatedAt
        fields(first: 100) { totalCount nodes {
          ... on ProjectV2FieldCommon { id name dataType }
          ... on ProjectV2SingleSelectField { options { id name color description } }
          ... on ProjectV2IterationField { configuration { iterations { id title startDate duration } completedIterations { id title startDate duration } } }
        } }
      }
    }
  }
}`

const projectItemsQuery = `query($id: ID!, $cursor: String) {
  node(id: $id) { ... on ProjectV2 { items(first: 100, after: $cursor) {
    pageInfo { hasNextPage endCursor }
    nodes {
      id type isArchived createdAt updatedAt
      content {
        ... on Issue { number title url repository { nameWithOwner } }
        ... on PullRequest { number title url repository { nameWithOwner } }
        ... on DraftIssue { title body }
      }
      fieldValues(first: 100) { nodes {
        ... on ProjectV2ItemFieldTextValue { text field { ... on ProjectV2FieldCommon { name } } }
        ... on ProjectV2ItemFieldNumberValue { number field { ... on ProjectV2FieldCommon { name } } }
        ... on ProjectV2ItemFieldDateValue { date field { ... on ProjectV2FieldCommon { name } } }
        ... on ProjectV2ItemFieldSingleSelectValue { name optionId field { ... on ProjectV2FieldCommon { name } } }
        ... on ProjectV2ItemFieldIterationValue { title iterationId startDate duration field { ... on ProjectV2FieldCommon { name } } }
      } }
    }
  } } }
}`

// projects returns every Projects (v2) board of an org ("organization") or user ("user")
func (e *Exporter) projects(ctx context.Context, ownerKind, login string) ([]Project, error) {
	query := fmt.Sprintf(projectsQuery, ownerKind)
	out := make([]Project, 0)
	vars := map[string]any{"login": login, "cursor": nil}
	for {
		var data map[string]*struct {
			ProjectsV2 connection[Project] `json:"projectsV2"`
		}
		if err := e.graphql(ctx, query, vars, &data, func() bool { return data[ownerKind] != nil }); err != nil {
			return nil, err
		}
		owner := data[ownerKind]
		if owner == nil {
			return nil, fmt.Errorf("%s %s not returned", ownerKind, login)
		}

		page := owner.ProjectsV2
		for i := range page.Nodes {
			items, err := e.projectItems(ctx, page.Nodes[i].ID)
			if err != nil {
				return nil, fmt.Errorf("project %d: %w", page.Nodes[i].Number, err)
			}
			page.Nodes[i].Items = items
			page.Nodes[i].Fields.PageInfo = nil
		}
		out = append(out, page.Nodes...)

		if page.PageInfo == nil || !page.PageInfo.HasNextPage {
			return out, nil
		}
		vars["cursor"] = page.PageInfo.EndCursor
	}
}

// projectItems returns every item of one project, with its field values
func (e *Exporter) projectItems(ctx context.Context, id string) ([]json.RawMessage, error) {
	out := make([]json.RawMessage, 0)
	vars := map[string]any{"id": id, "cursor": nil}
	for {
		var data struct {
			Node *struct {
				Items connection[json.RawMessage] `json:"items"`
			} `json:"node"`
		}
		if err := e.graphql(ctx, projectItemsQuery, vars, &data, func() bool { return data.Node != nil }); err != nil {
			return nil, err
		}
		if data.Node == nil {
			return nil, fmt.Errorf("project %s not returned", id)
		}
		out = append(out, data.Node.Items.Nodes...)
		if data.Node.Items.PageInfo == nil || !data.Node.Items.PageInfo.HasNextPage {
			return out, nil
		}
		vars["cursor"] = data.Node.Items.PageInfo.EndCursor
	}
}
