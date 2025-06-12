package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
	"strings"
)

const (
	gheAPIURL = "https://TelenorSwedenAB.ghe.com/api/v3"
)

type GHEClient struct {
	config     Config
	httpClient *http.Client
	baseURL    string
	token      string
}

// GitHub Enterprise types for self-hosted runners
type SelfHostedRunner struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	OS     string `json:"os"`
	Status string `json:"status"` // online, offline
	Busy   bool   `json:"busy"`
	Labels []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"labels"`
}

type SelfHostedRunnerList struct {
	TotalCount int                `json:"total_count"`
	Runners    []SelfHostedRunner `json:"runners"`
}

type RegistrationToken struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

type WorkflowRun struct {
	ID         int    `json:"id"`
	Status     string `json:"status"`     // queued, in_progress, completed
	Conclusion string `json:"conclusion"` // success, failure, cancelled
	RunnerName string `json:"runner_name,omitempty"`
	Repository *Repository `json:"repository,omitempty"`
}

type Repository struct {
	Name      string `json:"name"`
	FullName  string `json:"full_name"`
	Owner     *Owner `json:"owner"`
}

type Owner struct {
	Login string `json:"login"`
}

type WorkflowRunsList struct {
	TotalCount   int           `json:"total_count"`
	WorkflowRuns []WorkflowRun `json:"workflow_runs"`
}

// NewGHEClient creates a new GitHub Enterprise client
func NewGHEClient(config Config) *GHEClient {
	return &GHEClient{
		config:     config,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    gheAPIURL,
		token:      config.GitHubToken,
	}
}

// GetSelfHostedRunners gets all self-hosted runners for the organization
func (c *GHEClient) GetSelfHostedRunners(ctx context.Context) (*SelfHostedRunnerList, error) {
	url := fmt.Sprintf("%s/orgs/%s/actions/runners", c.baseURL, c.config.OrganizationName)
	
	resp, err := c.makeRequest(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get runners (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var runners SelfHostedRunnerList
	if err := json.NewDecoder(resp.Body).Decode(&runners); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &runners, nil
}

// GetRepositoriesInOrganization gets list of repositories in the organization
func (c *GHEClient) GetRepositoriesInOrganization(ctx context.Context) ([]Repository, error) {
	url := fmt.Sprintf("%s/orgs/%s/repos?per_page=100", c.baseURL, c.config.OrganizationName)
	
	var allRepos []Repository
	page := 1
	
	for {
		pageURL := fmt.Sprintf("%s&page=%d", url, page)
		resp, err := c.makeRequest(ctx, "GET", pageURL, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to make request: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("failed to get repositories (HTTP %d): %s", resp.StatusCode, string(body))
		}

		var repos []Repository
		if err := json.NewDecoder(resp.Body).Decode(&repos); err != nil {
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}

		if len(repos) == 0 {
			break
		}

		allRepos = append(allRepos, repos...)
		page++
		
		// Prevent infinite loops - GitHub has a max of 1000 repos per org
		if page > 10 {
			break
		}
	}

	return allRepos, nil
}

// GetQueuedWorkflowRuns gets workflow runs that are queued across repositories in the organization
func (c *GHEClient) GetQueuedWorkflowRuns(ctx context.Context) (*WorkflowRunsList, error) {
	return c.getWorkflowRunsAcrossRepos(ctx, "queued")
}

// GetRunningWorkflowRuns gets workflow runs that are in progress across repositories
func (c *GHEClient) GetRunningWorkflowRuns(ctx context.Context) (*WorkflowRunsList, error) {
	return c.getWorkflowRunsAcrossRepos(ctx, "in_progress")
}

// getWorkflowRunsAcrossRepos gets workflow runs with specified status across organization repositories
func (c *GHEClient) getWorkflowRunsAcrossRepos(ctx context.Context, status string) (*WorkflowRunsList, error) {
	var repos []Repository
	var err error

	// If specific repositories are configured, use them; otherwise get all org repos
	if len(c.config.RepositoryNames) > 0 {
		for _, repoName := range c.config.RepositoryNames {
			// Parse repo name (could be "owner/repo" or just "repo")
			var owner, name string
			if strings.Contains(repoName, "/") {
				parts := strings.Split(repoName, "/")
				if len(parts) == 2 {
					owner, name = parts[0], parts[1]
				} else {
					continue // Invalid format, skip
				}
			} else {
				owner, name = c.config.OrganizationName, repoName
			}

			repos = append(repos, Repository{
				Name:     name,
				FullName: fmt.Sprintf("%s/%s", owner, name),
				Owner:    &Owner{Login: owner},
			})
		}
	} else {
		// Get all repositories in the organization
		repos, err = c.GetRepositoriesInOrganization(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get repositories: %w", err)
		}
	}

	var allRuns []WorkflowRun
	totalCount := 0

	// Get workflow runs for each repository
	for _, repo := range repos {
		repoRuns, err := c.getRepositoryWorkflowRuns(ctx, repo.Owner.Login, repo.Name, status)
		if err != nil {
			// Log error but continue with other repositories
			fmt.Printf("Warning: failed to get workflow runs for %s: %v\n", repo.FullName, err)
			continue
		}

		// Add repository info to each run
		for _, run := range repoRuns.WorkflowRuns {
			run.Repository = &repo
			allRuns = append(allRuns, run)
		}
		totalCount += repoRuns.TotalCount
	}

	return &WorkflowRunsList{
		TotalCount:   totalCount,
		WorkflowRuns: allRuns,
	}, nil
}

// getRepositoryWorkflowRuns gets workflow runs for a specific repository
func (c *GHEClient) getRepositoryWorkflowRuns(ctx context.Context, owner, repo, status string) (*WorkflowRunsList, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/actions/runs?status=%s&per_page=100", c.baseURL, owner, repo, status)
	
	resp, err := c.makeRequest(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get workflow runs (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var runs WorkflowRunsList
	if err := json.NewDecoder(resp.Body).Decode(&runs); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &runs, nil
}

// GetRegistrationToken gets a new runner registration token
func (c *GHEClient) GetRegistrationToken(ctx context.Context) (*RegistrationToken, error) {
	url := fmt.Sprintf("%s/orgs/%s/actions/runners/registration-token", c.baseURL, c.config.OrganizationName)
	
	resp, err := c.makeRequest(ctx, "POST", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get registration token (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var token RegistrationToken
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &token, nil
}

// RemoveRunner removes a self-hosted runner
func (c *GHEClient) RemoveRunner(ctx context.Context, runnerID int) error {
	url := fmt.Sprintf("%s/orgs/%s/actions/runners/%d", c.baseURL, c.config.OrganizationName, runnerID)
	
	resp, err := c.makeRequest(ctx, "DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to remove runner (HTTP %d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// makeRequest makes an authenticated request to the GitHub Enterprise API
func (c *GHEClient) makeRequest(ctx context.Context, method, url string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "token "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return c.httpClient.Do(req)
}

// AnalyzeRunnerDemand analyzes current demand for runners
func (c *GHEClient) AnalyzeRunnerDemand(ctx context.Context) (*RunnerDemandAnalysis, error) {
	// Get current runners
	runners, err := c.GetSelfHostedRunners(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get runners: %w", err)
	}

	// Get queued workflow runs
	queuedRuns, err := c.GetQueuedWorkflowRuns(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get queued runs: %w", err)
	}

	// Analyze the data
	analysis := &RunnerDemandAnalysis{
		TotalRunners:       runners.TotalCount,
		OnlineRunners:      0,
		BusyRunners:        0,
		IdleRunners:        0,
		QueuedJobs:         queuedRuns.TotalCount,
		EstimatedNeed:      0,
	}

	for _, runner := range runners.Runners {
		if runner.Status == "online" {
			analysis.OnlineRunners++
			if runner.Busy {
				analysis.BusyRunners++
			} else {
				analysis.IdleRunners++
			}
		}
	}

	// Calculate estimated need
	// Need more runners if we have queued jobs but no idle runners
	if queuedRuns.TotalCount > 0 && analysis.IdleRunners == 0 {
		analysis.EstimatedNeed = queuedRuns.TotalCount
	}

	return analysis, nil
}

type RunnerDemandAnalysis struct {
	TotalRunners   int `json:"total_runners"`
	OnlineRunners  int `json:"online_runners"`
	BusyRunners    int `json:"busy_runners"`
	IdleRunners    int `json:"idle_runners"`
	QueuedJobs     int `json:"queued_jobs"`
	EstimatedNeed  int `json:"estimated_need"`
} 