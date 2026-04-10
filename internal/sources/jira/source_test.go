package jira

import (
	"testing"
	"time"

	jiraclient "github.com/ankitpokhrel/jira-cli/pkg/jira"
	"github.com/stretchr/testify/assert"

	"pkm-sync/pkg/models"
)

func TestBuildJQL_Empty(t *testing.T) {
	cfg := models.JiraSourceConfig{}
	jql := buildJQL(cfg, time.Time{}, "")
	assert.Equal(t, "ORDER BY updated DESC", jql)
}

func TestBuildJQL_ProjectKeys(t *testing.T) {
	cfg := models.JiraSourceConfig{
		ProjectKeys: []string{"PROJ", "TEAM"},
	}
	jql := buildJQL(cfg, time.Time{}, "")
	assert.Equal(t, "project IN (PROJ, TEAM) ORDER BY updated DESC", jql)
}

func TestBuildJQL_IssueTypes(t *testing.T) {
	cfg := models.JiraSourceConfig{
		IssueTypes: []string{"Bug", "Story"},
	}
	jql := buildJQL(cfg, time.Time{}, "")
	assert.Equal(t, `issuetype IN ("Bug", "Story") ORDER BY updated DESC`, jql)
}

func TestBuildJQL_Statuses(t *testing.T) {
	cfg := models.JiraSourceConfig{
		Statuses: []string{"In Progress", "Done"},
	}
	jql := buildJQL(cfg, time.Time{}, "")
	assert.Equal(t, `status IN ("In Progress", "Done") ORDER BY updated DESC`, jql)
}

func TestBuildJQL_AssigneeMe(t *testing.T) {
	cfg := models.JiraSourceConfig{
		AssigneeFilter: "me",
	}
	jql := buildJQL(cfg, time.Time{}, "rhn-support-user")
	assert.Equal(t, `assignee = "rhn-support-user" ORDER BY updated DESC`, jql)
}

func TestBuildJQL_AssigneeMe_NoUser(t *testing.T) {
	cfg := models.JiraSourceConfig{
		AssigneeFilter: "me",
	}
	// Without a resolved user, the assignee clause should be omitted.
	jql := buildJQL(cfg, time.Time{}, "")
	assert.Equal(t, "ORDER BY updated DESC", jql)
}

func TestBuildJQL_WithSince(t *testing.T) {
	cfg := models.JiraSourceConfig{
		ProjectKeys: []string{"PROJ"},
	}
	since := time.Date(2024, 1, 15, 9, 30, 0, 0, time.UTC)
	jql := buildJQL(cfg, since, "")
	assert.Equal(t, `project IN (PROJ) AND updated >= "2024-01-15 09:30" ORDER BY updated DESC`, jql)
}

func TestBuildJQL_CustomJQL(t *testing.T) {
	cfg := models.JiraSourceConfig{
		JQL: "project = MYPROJ AND type = Bug",
	}
	jql := buildJQL(cfg, time.Time{}, "")
	assert.Equal(t, "(project = MYPROJ AND type = Bug) ORDER BY updated DESC", jql)
}

func TestBuildJQL_CustomJQL_WithSince(t *testing.T) {
	cfg := models.JiraSourceConfig{
		JQL: "project = MYPROJ",
	}
	since := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	jql := buildJQL(cfg, since, "")
	assert.Equal(t, `(project = MYPROJ) AND updated >= "2024-06-01 00:00" ORDER BY updated DESC`, jql)
}

func TestBuildJQL_Combined(t *testing.T) {
	cfg := models.JiraSourceConfig{
		ProjectKeys:    []string{"PROJ"},
		IssueTypes:     []string{"Bug"},
		Statuses:       []string{"Open"},
		AssigneeFilter: "me",
	}
	since := time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC)
	jql := buildJQL(cfg, since, "alice")
	assert.Equal(t, `project IN (PROJ) AND issuetype IN ("Bug") AND status IN ("Open") AND assignee = "alice" AND updated >= "2024-03-01 12:00" ORDER BY updated DESC`, jql)
}

func makeTestIssue() *jiraclient.Issue {
	issue := &jiraclient.Issue{
		Key: "PROJ-123",
	}
	issue.Fields.Summary = "Fix the login bug"
	issue.Fields.Description = "Users cannot log in after upgrade"
	issue.Fields.Created = "2024-01-10T10:00:00+0000"
	issue.Fields.Updated = "2024-01-11T15:30:00+0000"
	issue.Fields.IssueType = jiraclient.IssueType{Name: "Bug"}
	issue.Fields.Status.Name = "In Progress"
	issue.Fields.Priority.Name = "High"
	issue.Fields.Assignee.Name = "alice"
	issue.Fields.Reporter.Name = "bob"
	issue.Fields.Labels = []string{"backend", "critical"}

	return issue
}

func TestIssueToItem_BasicFields(t *testing.T) {
	issue := makeTestIssue()
	item := issueToItem(issue, "https://issues.example.com", models.JiraSourceConfig{})

	assert.Equal(t, "jira_PROJ-123", item.GetID())
	assert.Equal(t, "PROJ-123", item.GetTitle()) // key used as title → PROJ-123.md filename
	assert.Equal(t, "Users cannot log in after upgrade", item.GetContent())
	assert.Equal(t, "jira", item.GetSourceType())
	assert.Equal(t, "issue", item.GetItemType())

	tags := item.GetTags()
	assert.Contains(t, tags, "backend")
	assert.Contains(t, tags, "critical")
	assert.Contains(t, tags, "type/bug")
	assert.Contains(t, tags, "status/in-progress")
	assert.Contains(t, tags, "priority/high")

	meta := item.GetMetadata()
	assert.Equal(t, "Fix the login bug", meta["summary"])
	assert.Equal(t, "PROJ-123", meta["issue_key"])
	assert.Equal(t, "Bug", meta["issue_type"])
	assert.Equal(t, "In Progress", meta["status"])
	assert.Equal(t, "High", meta["priority"])
	assert.Equal(t, "alice", meta["assignee"])
	assert.Equal(t, "bob", meta["reporter"])
	assert.Equal(t, "PROJ", meta["project"])

	links := item.GetLinks()
	assert.Len(t, links, 1)
	assert.Equal(t, "https://issues.example.com/browse/PROJ-123", links[0].URL)
}

func TestIssueToItem_Timestamps(t *testing.T) {
	issue := makeTestIssue()
	item := issueToItem(issue, "", models.JiraSourceConfig{})

	assert.Equal(t, 2024, item.GetCreatedAt().Year())
	assert.Equal(t, time.January, item.GetCreatedAt().Month())
	assert.Equal(t, 10, item.GetCreatedAt().Day())

	assert.Equal(t, 2024, item.GetUpdatedAt().Year())
	assert.Equal(t, 11, item.GetUpdatedAt().Day())
}

func TestIssueToItem_WithComments(t *testing.T) {
	issue := makeTestIssue()
	issue.Fields.Comment.Total = 1
	issue.Fields.Comment.Comments = []struct {
		ID      string          `json:"id"`
		Author  jiraclient.User `json:"author"`
		Body    interface{}     `json:"body"`
		Created string          `json:"created"`
	}{
		{
			ID:      "1",
			Author:  jiraclient.User{DisplayName: "Alice"},
			Body:    "This is a comment",
			Created: "2024-01-12T09:00:00+0000",
		},
	}

	item := issueToItem(issue, "", models.JiraSourceConfig{IncludeComments: true})
	content := item.GetContent()

	assert.Contains(t, content, "Users cannot log in after upgrade")
	assert.Contains(t, content, "## Comments")
	assert.Contains(t, content, "### Alice")
	assert.Contains(t, content, "This is a comment")
}

func TestIssueToItem_CommentsDisabled(t *testing.T) {
	issue := makeTestIssue()
	issue.Fields.Comment.Total = 1
	issue.Fields.Comment.Comments = []struct {
		ID      string          `json:"id"`
		Author  jiraclient.User `json:"author"`
		Body    interface{}     `json:"body"`
		Created string          `json:"created"`
	}{
		{
			ID:      "1",
			Author:  jiraclient.User{DisplayName: "Bob"},
			Body:    "Comment that should not appear",
			Created: "2024-01-12T09:00:00+0000",
		},
	}

	item := issueToItem(issue, "", models.JiraSourceConfig{})
	content := item.GetContent()

	assert.Equal(t, "Users cannot log in after upgrade", content)
	assert.NotContains(t, content, "## Comments")
}

func TestParseJiraTime_RFC3339(t *testing.T) {
	s := "2024-01-10T10:00:00+0000"
	ts := parseJiraTime(s)
	assert.False(t, ts.IsZero())
	assert.Equal(t, 2024, ts.Year())
	assert.Equal(t, time.January, ts.Month())
	assert.Equal(t, 10, ts.Day())
}

func TestParseJiraTime_RFC3339Milli(t *testing.T) {
	s := "2024-01-10T10:00:00.000+0000"
	ts := parseJiraTime(s)
	assert.False(t, ts.IsZero())
	assert.Equal(t, 2024, ts.Year())
}

func TestParseJiraTime_Empty(t *testing.T) {
	ts := parseJiraTime("")
	assert.True(t, ts.IsZero())
}

func TestParseJiraTime_Invalid(t *testing.T) {
	ts := parseJiraTime("not-a-date")
	assert.True(t, ts.IsZero())
}

func TestDescriptionToString_String(t *testing.T) {
	s := descriptionToMarkdown("hello world")
	assert.Equal(t, "hello world", s)
}

func TestDescriptionToString_Nil(t *testing.T) {
	s := descriptionToMarkdown(nil)
	assert.Equal(t, "", s)
}

func TestExtractProject(t *testing.T) {
	assert.Equal(t, "PROJ", extractProject("PROJ-123"))
	assert.Equal(t, "MY-PROJ", extractProject("MY-PROJ-42"))
	assert.Equal(t, "NONUM", extractProject("NONUM"))
}
