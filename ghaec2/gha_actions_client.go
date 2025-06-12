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
	
	// For GitHub Enterprise, we use the organization's runner groups endpoint
	url := fmt.Sprintf("%s/api/v3/orgs/%s/actions/runners/registration-token", c.baseURL, org)
	
	req, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	
	req.Header.Set("Authorization", fmt.Sprintf("token %s", c.token))
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	
	c.logger.Info("Sending registration request", 
		"url", url,
		"authHeader", fmt.Sprintf("token %s", c.token[:4] + "..." + c.token[len(c.token)-4:]))
	
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusCreated {
		return c.parseErrorResponse(resp)
	}
	
	var result struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}
	
	c.adminToken = result.Token
	c.adminTokenExpiry = result.ExpiresAt
	c.actionsTokenURL = c.baseURL
	
	c.logger.Info("Successfully initialized",
		"tokenExpiry", c.adminTokenExpiry,
		"tokenLength", len(c.adminToken))
	
	return nil
}

// GetOrCreateRunnerScaleSet gets an existing scale set or creates a new one
func (c *ActionsServiceClient) GetOrCreateRunnerScaleSet(ctx context.Context, name string, labels []string) (*RunnerScaleSet, error) {
	// For GitHub Enterprise, we use the organization's runner groups
	url := fmt.Sprintf("%s/api/v3/orgs/%s/actions/runner-groups", c.baseURL, name)
	
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	
	req.Header.Set("Authorization", fmt.Sprintf("token %s", c.token))
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return nil, c.parseErrorResponse(resp)
	}
	
	var result struct {
		TotalCount int `json:"total_count"`
		RunnerGroups []struct {
			ID              int      `json:"id"`
			Name            string   `json:"name"`
			Visibility      string   `json:"visibility"`
			Default         bool     `json:"default"`
			Selected        bool     `json:"selected"`
			Runners        []string `json:"runners"`
		} `json:"runner_groups"`
	}
	
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	
	// Find or create the runner group
	var groupID int
	for _, group := range result.RunnerGroups {
		if group.Name == name {
			groupID = group.ID
			break
		}
	}
	
	if groupID == 0 {
		// Create new runner group
		createURL := fmt.Sprintf("%s/api/v3/orgs/%s/actions/runner-groups", c.baseURL, name)
		payload := struct {
			Name       string   `json:"name"`
			Visibility string   `json:"visibility"`
			Labels     []string `json:"labels"`
		}{
			Name:       name,
			Visibility: "org",
			Labels:     labels,
		}
		
		body, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		
		req, err := http.NewRequestWithContext(ctx, "POST", createURL, bytes.NewBuffer(body))
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
		
		req.Header.Set("Authorization", fmt.Sprintf("token %s", c.token))
		req.Header.Set("Accept", "application/vnd.github.v3+json")
		req.Header.Set("Content-Type", "application/json")
		
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to send request: %w", err)
		}
		defer resp.Body.Close()
		
		if resp.StatusCode != http.StatusCreated {
			return nil, c.parseErrorResponse(resp)
		}
		
		var newGroup struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		}
		
		if err := json.NewDecoder(resp.Body).Decode(&newGroup); err != nil {
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}
		
		groupID = newGroup.ID
	}
	
	// Convert to RunnerScaleSet format
	scaleSet := &RunnerScaleSet{
		ID:              groupID,
		Name:            name,
		RunnerGroupID:   groupID,
		RunnerGroupName: name,
		Labels:          make([]Label, len(labels)),
	}
	
	for i, label := range labels {
		scaleSet.Labels[i] = Label{
			Type: "string",
			Name: label,
		}
	}
	
	return scaleSet, nil
}

// GetAcquirableJobs gets jobs that can be acquired by the scale set
func (c *ActionsServiceClient) GetAcquirableJobs(ctx context.Context, scaleSetID int) (*AcquirableJobList, error) {
	if err := c.refreshTokenIfNeeded(ctx); err != nil {
		return nil, fmt.Errorf("failed to refresh token: %w", err)
	}
	
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

// CreateMessageSession creates a session for receiving real-time messages
func (c *ActionsServiceClient) CreateMessageSession(ctx context.Context, scaleSetID int, owner string) (*RunnerScaleSetSession, error) {
	if err := c.refreshTokenIfNeeded(ctx); err != nil {
		return nil, fmt.Errorf("failed to refresh token: %w", err)
	}
	
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