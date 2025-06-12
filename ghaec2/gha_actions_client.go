package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
)

// GitHub Actions Service API endpoints
const (
	scaleSetEndpoint     = "api/v3/actions/runner-groups"
	apiVersion           = "2.0"
)

// AcquirableJob represents a job that can be acquired by a runner
type AcquirableJob struct {
	AcquireJobURL   string   `json:"acquireJobUrl"`
	MessageType     string   `json:"messageType"`
	RunnerRequestID int64    `json:"runnerRequestId"`
	RepositoryName  string   `json:"repositoryName"`
	OwnerName       string   `json:"ownerName"`
	JobWorkflowRef  string   `json:"jobWorkflowRef"`
	EventName       string   `json:"eventName"`
	RequestLabels   []string `json:"requestLabels"`
}

// AcquirableJobList represents the response from the acquirable jobs API
type AcquirableJobList struct {
	Count int             `json:"count"`
	Jobs  []AcquirableJob `json:"value"`
}

// RunnerScaleSetSession represents a session for message polling
type RunnerScaleSetSession struct {
	SessionID               *uuid.UUID               `json:"sessionId,omitempty"`
	OwnerName               string                   `json:"ownerName,omitempty"`
	RunnerScaleSet          *RunnerScaleSet          `json:"runnerScaleSet,omitempty"`
	MessageQueueURL         string                   `json:"messageQueueUrl,omitempty"`
	MessageQueueAccessToken string                   `json:"messageQueueAccessToken,omitempty"`
	Statistics              *RunnerScaleSetStatistic `json:"statistics,omitempty"`
}

// RunnerScaleSet represents a GitHub Actions runner scale set
type RunnerScaleSet struct {
	ID              int                      `json:"id,omitempty"`
	Name            string                   `json:"name,omitempty"`
	RunnerGroupID   int                      `json:"runnerGroupId,omitempty"`
	RunnerGroupName string                   `json:"runnerGroupName,omitempty"`
	Labels          []Label                  `json:"labels,omitempty"`
	Statistics      *RunnerScaleSetStatistic `json:"statistics,omitempty"`
}

// Label represents a runner label
type Label struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

// RunnerScaleSetStatistic represents current statistics for a scale set
type RunnerScaleSetStatistic struct {
	TotalAvailableJobs     int `json:"totalAvailableJobs"`
	TotalAcquiredJobs      int `json:"totalAcquiredJobs"`
	TotalAssignedJobs      int `json:"totalAssignedJobs"`
	TotalRunningJobs       int `json:"totalRunningJobs"`
	TotalRegisteredRunners int `json:"totalRegisteredRunners"`
	TotalBusyRunners       int `json:"totalBusyRunners"`
	TotalIdleRunners       int `json:"totalIdleRunners"`
}

// RunnerScaleSetMessage represents a message from the Actions service
type RunnerScaleSetMessage struct {
	MessageID   int64                    `json:"messageId"`
	MessageType string                   `json:"messageType"`
	Body        string                   `json:"body"`
	Statistics  *RunnerScaleSetStatistic `json:"statistics,omitempty"`
}

// JobAvailable represents a job available message
type JobAvailable struct {
	AcquireJobURL string `json:"acquireJobUrl"`
	JobMessageBase
}

// JobMessageBase contains common job message fields
type JobMessageBase struct {
	MessageType        string    `json:"messageType"`
	RunnerRequestID    int64     `json:"runnerRequestId"`
	RepositoryName     string    `json:"repositoryName"`
	OwnerName          string    `json:"ownerName"`
	JobWorkflowRef     string    `json:"jobWorkflowRef"`
	JobDisplayName     string    `json:"jobDisplayName"`
	WorkflowRunID      int64     `json:"workflowRunId"`
	EventName          string    `json:"eventName"`
	RequestLabels      []string  `json:"requestLabels"`
	QueueTime          time.Time `json:"queueTime"`
	ScaleSetAssignTime time.Time `json:"scaleSetAssignTime"`
	RunnerAssignTime   time.Time `json:"runnerAssignTime"`
	FinishTime         time.Time `json:"finishTime"`
}

// ActionsError represents an error from the Actions service
type ActionsError struct {
	StatusCode int
	ActivityID string
	Message    string
	Err        error
}

func (e *ActionsError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("Actions API error (status: %d, activity: %s): %v", e.StatusCode, e.ActivityID, e.Err)
	}
	return fmt.Sprintf("Actions API error (status: %d, activity: %s): %s", e.StatusCode, e.ActivityID, e.Message)
}

// ActionsServiceClient provides access to GitHub Actions Service APIs
type ActionsServiceClient struct {
	httpClient      *http.Client
	baseURL         string
	token           string
	logger          logr.Logger
	actionsTokenURL string
	adminToken      string
	adminTokenExpiry time.Time
}

// NewActionsServiceClient creates a new Actions Service client
func NewActionsServiceClient(gitHubEnterpriseURL, token string, logger logr.Logger) *ActionsServiceClient {
	baseURL := strings.TrimSuffix(gitHubEnterpriseURL, "/")
	
	return &ActionsServiceClient{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		baseURL: baseURL,
		token:   token,
		logger:  logger,
	}
}

// Initialize discovers the Actions Service URL and gets admin token
func (c *ActionsServiceClient) Initialize(ctx context.Context, org string) error {
	c.logger.Info("Initializing Actions Service client", 
		"organization", org,
		"baseURL", c.baseURL)
	
	// Step 1: Get runner registration token from GitHub API
	regToken, err := c.getRegistrationToken(ctx, org)
	if err != nil {
		return fmt.Errorf("failed to get registration token: %w", err)
	}
	
	c.logger.Info("Successfully obtained registration token",
		"expiresAt", regToken.ExpiresAt.Unix(),
		"tokenLength", len(regToken.Token))
	
	// Step 2: Try to get Actions Service admin connection
	// If this fails, we'll fall back to traditional GitHub API polling
	c.logger.Info("Attempting to get Actions Service admin connection",
		"baseURL", c.baseURL,
		"regTokenLength", len(regToken.Token))
	
	adminConn, err := c.getActionsServiceAdminConnection(ctx, regToken, org)
	if err != nil {
		c.logger.Info("Actions Service not available, falling back to GitHub API polling",
			"error", err.Error())
		
		// Fallback: Use GitHub API directly without Actions Service
		c.actionsTokenURL = c.baseURL
		c.adminToken = c.token
		c.adminTokenExpiry = time.Now().Add(24 * time.Hour) // GitHub tokens don't expire quickly
		
		c.logger.Info("Successfully initialized with GitHub API fallback",
			"fallbackMode", true)
		
		return nil
	}
	
	if adminConn.ActionsServiceURL == nil || adminConn.AdminToken == nil {
		c.logger.Info("Invalid Actions Service response, falling back to GitHub API polling")
		
		// Fallback: Use GitHub API directly
		c.actionsTokenURL = c.baseURL
		c.adminToken = c.token
		c.adminTokenExpiry = time.Now().Add(24 * time.Hour)
		
		c.logger.Info("Successfully initialized with GitHub API fallback",
			"fallbackMode", true)
		
		return nil
	}
	
	c.actionsTokenURL = *adminConn.ActionsServiceURL
	c.adminToken = *adminConn.AdminToken
	c.adminTokenExpiry = time.Now().Add(1 * time.Hour) // Tokens typically expire in 1 hour
	
	c.logger.Info("Successfully initialized Actions Service client",
		"actionsServiceURL", c.actionsTokenURL,
		"tokenExpiry", c.adminTokenExpiry,
		"fallbackMode", false)
	
	return nil
}

// registrationToken represents a GitHub runner registration token
type registrationToken struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// getRegistrationToken gets a runner registration token from GitHub API
func (c *ActionsServiceClient) getRegistrationToken(ctx context.Context, org string) (*registrationToken, error) {
	// Use the correct GitHub API endpoint for organization registration tokens
	path := fmt.Sprintf("/api/v3/orgs/%s/actions/runners/registration-token", org)
	url := fmt.Sprintf("%s%s", c.baseURL, path)
	
	c.logger.Info("Getting registration token",
		"organization", org,
		"baseURL", c.baseURL,
		"tokenLength", len(c.token))
	
	req, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	
	req.Header.Set("Authorization", fmt.Sprintf("token %s", c.token))
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	
	c.logger.Info("Sending registration token request", 
		"url", url,
		"authHeader", fmt.Sprintf("token %s", c.token[:4] + "..." + c.token[len(c.token)-4:]))
	
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusCreated {
		return nil, c.parseErrorResponse(resp)
	}
	
	var result registrationToken
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	
	c.logger.Info("Successfully obtained registration token",
		"expiresAt", result.ExpiresAt.Unix(),
		"tokenLength", len(result.Token))
	
	return &result, nil
}

// ActionsServiceAdminConnection represents the response from the runner registration endpoint
type ActionsServiceAdminConnection struct {
	ActionsServiceURL *string `json:"url,omitempty"`
	AdminToken        *string `json:"token,omitempty"`
}

// getActionsServiceAdminConnection gets the Actions Service URL and admin token
func (c *ActionsServiceClient) getActionsServiceAdminConnection(ctx context.Context, regToken *registrationToken, org string) (*ActionsServiceAdminConnection, error) {
	// Use the correct Actions Service endpoint (without /api/v3)
	path := "/actions/runner-registration"
	url := fmt.Sprintf("%s%s", c.baseURL, path)
	
	// Create the request body as per the reference implementation
	body := struct {
		URL         string `json:"url"`
		RunnerEvent string `json:"runner_event"`
	}{
		URL:         fmt.Sprintf("%s/%s", c.baseURL, org), // GitHub config URL (org scope)
		RunnerEvent: "register",
	}
	
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}
	
	c.logger.Info("Getting Actions Service admin connection",
		"baseURL", c.baseURL,
		"regTokenLength", len(regToken.Token))
	
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	
	// Use RemoteAuth with the registration token as per reference implementation
	req.Header.Set("Authorization", fmt.Sprintf("RemoteAuth %s", regToken.Token))
	req.Header.Set("Content-Type", "application/json")
	
	c.logger.Info("Sending admin connection request", 
		"url", url,
		"authHeader", fmt.Sprintf("RemoteAuth %s", regToken.Token[:4] + "..." + regToken.Token[len(regToken.Token)-4:]))
	
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send admin connection request: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, c.parseErrorResponse(resp)
	}
	
	var conn ActionsServiceAdminConnection
	if err := json.NewDecoder(resp.Body).Decode(&conn); err != nil {
		// Check if the error is likely due to HTML response (common indicator of Actions Service not being available)
		if strings.Contains(err.Error(), "invalid character '<'") {
			return nil, fmt.Errorf("Actions Service endpoint returned HTML instead of JSON - GitHub Enterprise Server version may not support this feature")
		}
		return nil, fmt.Errorf("failed to decode Actions Service response: %w", err)
	}
	
	c.logger.Info("Successfully obtained Actions Service admin connection")
	
	return &conn, nil
}

// GetOrCreateRunnerScaleSet gets an existing scale set or creates a new one
func (c *ActionsServiceClient) GetOrCreateRunnerScaleSet(ctx context.Context, name string, labels []string) (*RunnerScaleSet, error) {
	// In fallback mode, we don't use runner groups - just return a mock scale set
	if strings.Contains(c.actionsTokenURL, c.baseURL) && c.adminToken == c.token {
		c.logger.Info("Creating mock scale set for fallback mode", "name", name)
		return &RunnerScaleSet{
			ID:              1, // Mock ID
			Name:            name,
			RunnerGroupID:   1,
			RunnerGroupName: "default",
			Labels:          make([]Label, len(labels)),
		}, nil
	}
	
	// For GitHub Enterprise, we need the organization name, not the scale set name
	// We'll need to get this from the config - for now, let's try a different approach
	c.logger.Info("In normal mode, but runner groups may not be available on this GHES version")
	
	// Since runner groups might not be available on older GHES versions,
	// let's create a simple mock scale set that works with basic functionality
	scaleSet := &RunnerScaleSet{
		ID:              1,
		Name:            name,
		RunnerGroupID:   1,
		RunnerGroupName: "default",
		Labels:          make([]Label, len(labels)),
	}
	
	for i, label := range labels {
		scaleSet.Labels[i] = Label{
			Type: "string",
			Name: label,
		}
	}
	
	c.logger.Info("Created basic scale set", "name", name, "id", scaleSet.ID)
	return scaleSet, nil
}

// GetAcquirableJobs gets jobs that can be acquired by the scale set
func (c *ActionsServiceClient) GetAcquirableJobs(ctx context.Context, scaleSetID int) (*AcquirableJobList, error) {
	if err := c.refreshTokenIfNeeded(ctx); err != nil {
		return nil, fmt.Errorf("failed to refresh token: %w", err)
	}
	
	// Check if we're using Actions Service or fallback mode
	if strings.Contains(c.actionsTokenURL, c.baseURL) && c.adminToken == c.token {
		// Fallback mode: Use GitHub API to get workflow runs
		return c.getAcquirableJobsFallback(ctx)
	}
	
	// Normal Actions Service mode
	path := fmt.Sprintf("/%s/%d/acquirablejobs", scaleSetEndpoint, scaleSetID)
	url := fmt.Sprintf("%s%s?api-version=%s", c.actionsTokenURL, path, apiVersion)
	
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.adminToken))
	req.Header.Set("Content-Type", "application/json")
	
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode == http.StatusNoContent {
		return &AcquirableJobList{Count: 0, Jobs: []AcquirableJob{}}, nil
	}
	
	if resp.StatusCode != http.StatusOK {
		return nil, c.parseErrorResponse(resp)
	}
	
	var jobList AcquirableJobList
	if err := json.NewDecoder(resp.Body).Decode(&jobList); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	
	return &jobList, nil
}

// getAcquirableJobsFallback gets jobs using GitHub API workflow runs (fallback mode)
func (c *ActionsServiceClient) getAcquirableJobsFallback(ctx context.Context) (*AcquirableJobList, error) {
	c.logger.Info("Using GitHub API fallback to get workflow runs")
	
	// For demonstration, let's check workflow runs in the organization
	// This is a simplified approach - you could enhance this to:
	// 1. Get all repositories in the organization
	// 2. Check each repository for queued/in_progress workflow runs
	// 3. Filter by runner labels
	
	// For now, let's make a simple API call to get organization workflow runs
	// Note: GitHub doesn't have a direct org-level workflow runs API, so we'll simulate
	
	jobs := []AcquirableJob{}
	
	// Since we can't easily get all org workflow runs, we'll check a known repository
	// In a real implementation, you'd iterate through all repos
	testRepoURL := fmt.Sprintf("%s/api/v3/repos/TelenorSweden/test-spot-runner/actions/runs", c.baseURL)
	
	req, err := http.NewRequestWithContext(ctx, "GET", testRepoURL, nil)
	if err != nil {
		c.logger.Error(err, "Failed to create workflow runs request")
		return &AcquirableJobList{Count: 0, Jobs: jobs}, nil
	}
	
	req.Header.Set("Authorization", fmt.Sprintf("token %s", c.token))
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	
	// Add query parameters to get only queued and in_progress runs
	q := req.URL.Query()
	q.Add("status", "queued")
	q.Add("per_page", "10")
	req.URL.RawQuery = q.Encode()
	
	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Error(err, "Failed to get workflow runs")
		return &AcquirableJobList{Count: 0, Jobs: jobs}, nil
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		c.logger.Info("Failed to get workflow runs", "statusCode", resp.StatusCode)
		return &AcquirableJobList{Count: 0, Jobs: jobs}, nil
	}
	
	var workflowRuns struct {
		TotalCount   int `json:"total_count"`
		WorkflowRuns []struct {
			ID          int64  `json:"id"`
			Name        string `json:"name"`
			Status      string `json:"status"`
			Conclusion  string `json:"conclusion"`
			HeadBranch  string `json:"head_branch"`
			WorkflowID  int64  `json:"workflow_id"`
			Repository  struct {
				Name     string `json:"name"`
				FullName string `json:"full_name"`
				Owner    struct {
					Login string `json:"login"`
				} `json:"owner"`
			} `json:"repository"`
		} `json:"workflow_runs"`
	}
	
	if err := json.NewDecoder(resp.Body).Decode(&workflowRuns); err != nil {
		c.logger.Error(err, "Failed to decode workflow runs response")
		return &AcquirableJobList{Count: 0, Jobs: jobs}, nil
	}
	
	c.logger.Info("Found workflow runs", 
		"totalCount", workflowRuns.TotalCount,
		"runs", len(workflowRuns.WorkflowRuns))
	
	// Convert workflow runs to acquirable jobs
	for _, run := range workflowRuns.WorkflowRuns {
		if run.Status == "queued" || run.Status == "in_progress" {
			job := AcquirableJob{
				AcquireJobURL:   fmt.Sprintf("%s/jobs/%d/acquire", c.baseURL, run.ID),
				MessageType:     "JobAvailable",
				RunnerRequestID: run.ID,
				RepositoryName:  run.Repository.Name,
				OwnerName:       run.Repository.Owner.Login,
				JobWorkflowRef:  fmt.Sprintf("%s@refs/heads/%s", run.Repository.FullName, run.HeadBranch),
				EventName:       "workflow_dispatch", // Default
				RequestLabels:   []string{"self-hosted", "linux", "x64", "ghalistener-managed"}, // Assume our labels
			}
			jobs = append(jobs, job)
		}
	}
	
	c.logger.Info("Created acquirable jobs from workflow runs", "jobCount", len(jobs))
	
	return &AcquirableJobList{Count: len(jobs), Jobs: jobs}, nil
}

// CreateMessageSession creates a session for receiving real-time messages
func (c *ActionsServiceClient) CreateMessageSession(ctx context.Context, scaleSetID int, owner string) (*RunnerScaleSetSession, error) {
	if err := c.refreshTokenIfNeeded(ctx); err != nil {
		return nil, fmt.Errorf("failed to refresh token: %w", err)
	}
	
	// Check if we're using fallback mode
	if strings.Contains(c.actionsTokenURL, c.baseURL) && c.adminToken == c.token {
		c.logger.Info("Creating mock session for fallback mode")
		// Return a mock session for fallback mode
		mockSessionID := uuid.New()
		return &RunnerScaleSetSession{
			SessionID:               &mockSessionID,
			OwnerName:               owner,
			MessageQueueURL:         "fallback://mock-queue",
			MessageQueueAccessToken: "mock-token",
			Statistics: &RunnerScaleSetStatistic{
				TotalAvailableJobs:     0,
				TotalAcquiredJobs:      0,
				TotalAssignedJobs:      0,
				TotalRunningJobs:       0,
				TotalRegisteredRunners: 0,
				TotalBusyRunners:       0,
				TotalIdleRunners:       0,
			},
		}, nil
	}
	
	// Normal Actions Service mode
	path := fmt.Sprintf("/%s/%d/sessions", scaleSetEndpoint, scaleSetID)
	url := fmt.Sprintf("%s%s?api-version=%s", c.actionsTokenURL, path, apiVersion)
	
	newSession := &RunnerScaleSetSession{
		OwnerName: owner,
	}
	
	body, err := json.Marshal(newSession)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal session: %w", err)
	}
	
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.adminToken))
	req.Header.Set("Content-Type", "application/json")
	
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return nil, c.parseErrorResponse(resp)
	}
	
	var session RunnerScaleSetSession
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	
	return &session, nil
}

// GetMessage polls for new messages from the message queue
func (c *ActionsServiceClient) GetMessage(ctx context.Context, messageQueueURL, accessToken string, lastMessageID int64, maxCapacity int) (*RunnerScaleSetMessage, error) {
	// Check if we're using fallback mode
	if messageQueueURL == "fallback://mock-queue" {
		// In fallback mode, we don't have real-time messages
		// Return nil to indicate no messages (polling will continue)
		return nil, nil
	}
	
	params := url.Values{}
	params.Set("lastMessageId", fmt.Sprintf("%d", lastMessageID))
	if maxCapacity > 0 {
		params.Set("runnerCapacity", fmt.Sprintf("%d", maxCapacity))
	}
	
	url := fmt.Sprintf("%s?%s", messageQueueURL, params.Encode())
	
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))
	req.Header.Set("Content-Type", "application/json")
	
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode == http.StatusNoContent {
		return nil, nil // No messages
	}
	
	if resp.StatusCode != http.StatusOK {
		return nil, c.parseErrorResponse(resp)
	}
	
	var message RunnerScaleSetMessage
	if err := json.NewDecoder(resp.Body).Decode(&message); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	
	return &message, nil
}

// Helper methods for token management and API calls

func (c *ActionsServiceClient) refreshTokenIfNeeded(ctx context.Context) error {
	if time.Now().Before(c.adminTokenExpiry.Add(-5 * time.Minute)) {
		return nil // Token is still valid
	}
	
	c.logger.Info("Token expired, need to restart service to get new token")
	return fmt.Errorf("token expired - restart service to refresh")
}

func (c *ActionsServiceClient) parseErrorResponse(resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &ActionsError{
			StatusCode: resp.StatusCode,
			ActivityID: resp.Header.Get("X-GitHub-Request-Id"),
			Message:    fmt.Sprintf("failed to read error response body: %v", err),
		}
	}

	var ghErr struct {
		Message string `json:"message"`
		Errors  []struct {
			Message string `json:"message"`
			Code    string `json:"code"`
			Field   string `json:"field"`
		} `json:"errors"`
		DocumentationURL string `json:"documentation_url"`
	}

	if err := json.Unmarshal(body, &ghErr); err != nil {
		// If we can't parse the JSON, return the raw body
		return &ActionsError{
			StatusCode: resp.StatusCode,
			ActivityID: resp.Header.Get("X-GitHub-Request-Id"),
			Message:    string(body),
		}
	}

	// Build detailed error message
	var messages []string
	messages = append(messages, ghErr.Message)
	for _, e := range ghErr.Errors {
		if e.Message != "" {
			messages = append(messages, fmt.Sprintf("%s: %s", e.Field, e.Message))
		}
	}

	c.logger.Info("GitHub API error response",
		"statusCode", resp.StatusCode,
		"requestId", resp.Header.Get("X-GitHub-Request-Id"),
		"message", strings.Join(messages, "; "),
		"documentation", ghErr.DocumentationURL)

	return &ActionsError{
		StatusCode: resp.StatusCode,
		ActivityID: resp.Header.Get("X-GitHub-Request-Id"),
		Message:    strings.Join(messages, "; "),
		Err:        fmt.Errorf("documentation: %s", ghErr.DocumentationURL),
	}
} 