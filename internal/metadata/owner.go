package metadata

import (
	"context"

	gh "github.com/google/go-github/v86/github"
)

// ExportOrg writes an org's settings, Actions variables, secret names, members,
// teams and projects into <target>/<org>.meta
func (e *Exporter) ExportOrg(ctx context.Context, org string, rep *Report) {
	name := "org " + org
	parts := []part{
		{"org.json", "read:org", func(ctx context.Context) (any, error) {
			var o *gh.Organization
			err := e.call(ctx, func() error {
				var err error
				o, _, err = e.gh.Organizations.Get(ctx, org)
				return err
			})
			return o, err
		}},
		{"actions-variables.json", "admin:org", func(ctx context.Context) (any, error) {
			return listAll(ctx, e, func(o gh.ListOptions) ([]*gh.ActionsVariable, *gh.Response, error) {
				v, resp, err := e.gh.Actions.ListOrgVariables(ctx, org, &o)
				if v == nil {
					return nil, resp, err
				}
				return v.Variables, resp, err
			})
		}},
		{"actions-secrets.json", "admin:org", func(ctx context.Context) (any, error) {
			// The API only ever returns secret names, never values
			return listAll(ctx, e, func(o gh.ListOptions) ([]*gh.Secret, *gh.Response, error) {
				s, resp, err := e.gh.Actions.ListOrgSecrets(ctx, org, &o)
				if s == nil {
					return nil, resp, err
				}
				return s.Secrets, resp, err
			})
		}},
		{"members.json", "read:org", func(ctx context.Context) (any, error) {
			return listAll(ctx, e, func(o gh.ListOptions) ([]*gh.User, *gh.Response, error) {
				return e.gh.Organizations.ListMembers(ctx, org, &gh.ListMembersOptions{ListOptions: o})
			})
		}},
		{"teams.json", "read:org", func(ctx context.Context) (any, error) {
			return listAll(ctx, e, func(o gh.ListOptions) ([]*gh.Team, *gh.Response, error) {
				return e.gh.Teams.ListTeams(ctx, org, &o)
			})
		}},
		{"projects-v2.json", "read:project", func(ctx context.Context) (any, error) {
			return e.projects(ctx, "organization", org)
		}},
	}

	e.writeParts(ctx, name, OwnerDir(e.target, org), parts, rep)
	if rep.Failed[name] == nil {
		rep.Exported = append(rep.Exported, name)
	}
}

// ExportUser writes a user's projects into <target>/<login>.meta
func (e *Exporter) ExportUser(ctx context.Context, login string, rep *Report) {
	name := "user " + login
	parts := []part{
		{"projects-v2.json", "read:project", func(ctx context.Context) (any, error) {
			return e.projects(ctx, "user", login)
		}},
	}

	e.writeParts(ctx, name, OwnerDir(e.target, login), parts, rep)
	if rep.Failed[name] == nil {
		rep.Exported = append(rep.Exported, name)
	}
}
