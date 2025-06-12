package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
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

// GetQueuedWorkflowRuns gets workflow runs that are queued and waiting for runners
func (c *GHEClient) GetQueuedWorkflowRuns(ctx context.Context) (*WorkflowRunsList, error) {
	url := fmt.Sprintf("%s/orgs/%s/actions/runs?status=queued", c.baseURL, c.config.OrganizationName)
	
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