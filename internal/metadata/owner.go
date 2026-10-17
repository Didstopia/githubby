package metadata

import (
	"context"

	gh "github.com/google/go-github/v86/github"
)

// OrgFiles and UserFiles are the files an org or user export writes; verify checks them
var (
	OrgFiles  = []string{"org.json", "actions-variables.json", "actions-secrets.json", "members.json", "teams.json", "projects-v2.json", RulesetsFile}
	UserFiles = []string{"projects-v2.json"}
)

// ExportOrg writes an org's settings, Actions variables, secret names, members,
// teams, projects and rulesets into <target>/<org>.meta
func (e *Exporter) ExportOrg(ctx context.Context, org string, rep *Report) {
	name := "org " + org
	e.writeParts(ctx, name, OwnerDir(e.target, org), e.orgParts(org), rep)
	if rep.Failed[name] == nil {
		rep.Exported = append(rep.Exported, name)
	}
}

func (e *Exporter) orgParts(org string) []part {
	return []part{
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
		{RulesetsFile, "admin:org", func(ctx context.Context) (any, error) {
			return e.rulesets(ctx, func(o gh.ListOptions) ([]*gh.RepositoryRuleset, *gh.Response, error) {
				return e.gh.Organizations.GetAllRepositoryRulesets(ctx, org, &o)
			}, func(id int64) (*gh.RepositoryRuleset, *gh.Response, error) {
				return e.gh.Organizations.GetRepositoryRuleset(ctx, org, id)
			})
		}},
	}
}

// ExportUser writes a user's projects into <target>/<login>.meta
func (e *Exporter) ExportUser(ctx context.Context, login string, rep *Report) {
	name := "user " + login
	e.writeParts(ctx, name, OwnerDir(e.target, login), e.userParts(login), rep)
	if rep.Failed[name] == nil {
		rep.Exported = append(rep.Exported, name)
	}
}

func (e *Exporter) userParts(login string) []part {
	return []part{
		{"projects-v2.json", "read:project", func(ctx context.Context) (any, error) {
			return e.projects(ctx, "user", login)
		}},
	}
}
