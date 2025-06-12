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

// GitHub Actions Service API endpoints - using correct endpoints from actions-runner-controller
const (
	scaleSetEndpoint     = "_apis/runtime/runnerscalesets"
	apiVersion           = "6.0-preview"
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
	ID            int           `json:"id"`
	Name          string        `json:"name"`
	RunnerGroupID int           `json:"runnerGroupId"`
	Labels        []Label       `json:"labels"`
	RunnerSetting RunnerSetting `json:"runnerSetting"`
}

// Label represents a runner label
type Label struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// RunnerSetting represents runner configuration
type RunnerSetting struct {
	Ephemeral     bool `json:"ephemeral"`
	IsElastic     bool `json:"isElastic"`
	DisableUpdate bool `json:"disableUpdate"`
}

// RunnerScaleSetStatistic represents runtime statistics
type RunnerScaleSetStatistic struct {
	TotalAvailableJobs     int `json:"totalAvailableJobs"`
	TotalAcquiredJobs      int `json:"totalAcquiredJobs"`
	TotalAssignedJobs      int `json:"totalAssignedJobs"`
	TotalRunningJobs       int `json:"totalRunningJobs"`
	TotalRegisteredRunners int `json:"totalRegisteredRunners"`
	TotalBusyRunners       int `json:"totalBusyRunners"`
	TotalIdleRunners       int `json:"totalIdleRunners"`
}

// RunnerScaleSetMessage represents a message from the Actions Service
type RunnerScaleSetMessage struct {
	MessageID   int64                    `json:"messageId"`
	MessageType string                   `json:"messageType"`
	Body        string                   `json:"body"`
	Statistics  *RunnerScaleSetStatistic `json:"statistics,omitempty"`
}

// JobAvailable represents a job available message
type JobAvailable struct {
	MessageType     string   `json:"messageType"`
	RunnerRequestID int64    `json:"runnerRequestId"`
	RepositoryName  string   `json:"repositoryName"`
	OwnerName       string   `json:"ownerName"`
	JobWorkflowRef  string   `json:"jobWorkflowRef"`
	EventName       string   `json:"eventName"`
	RequestLabels   []string `json:"requestLabels"`
}

// JobMessageBase represents a base job message
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

// registrationToken represents the GitHub registration token response
type registrationToken struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// ActionsServiceAdminConnection represents the admin connection response
type ActionsServiceAdminConnection struct {
	AdminConnectionURL  string `json:"url"`
	AdminConnectionAuth string `json:"authorization"`
}

// ActionsServiceClient provides access to GitHub Actions Service APIs
type ActionsServiceClient struct {
	httpClient           *http.Client
	baseURL              string
	token                string
	logger               logr.Logger
	actionsServiceURL    string
	adminToken           string
	adminTokenExpiry     time.Time
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
	c.logger.Info("Initializing Actions Service client", "organization", org)
	
	// Step 1: Get registration token
	regToken, err := c.getRegistrationToken(ctx, org)
	if err != nil {
		return fmt.Errorf("failed to get registration token: %w", err)
	}
	
	c.logger.Info("Successfully obtained registration token")
	
	// Step 2: Get Actions Service admin connection
	adminConn, err := c.getActionsServiceAdminConnection(ctx, regToken, org)
	if err != nil {
		return fmt.Errorf("failed to get Actions Service admin connection: %w", err)
	}
	
	c.actionsServiceURL = adminConn.AdminConnectionURL
	c.adminToken = adminConn.AdminConnectionAuth
	c.adminTokenExpiry = time.Now().Add(55 * time.Minute) // Tokens typically expire in 1 hour
	
	c.logger.Info("Successfully initialized Actions Service client",
		"actionsServiceURL", c.actionsServiceURL,
		"tokenExpiry", c.adminTokenExpiry)
	
	return nil
}

// getRegistrationToken gets a registration token from GitHub
func (c *ActionsServiceClient) getRegistrationToken(ctx context.Context, org string) (*registrationToken, error) {
	url := fmt.Sprintf("%s/api/v3/orgs/%s/actions/runners/registration-token", c.baseURL, org)
	
	req, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	
	req.Header.Set("Authorization", fmt.Sprintf("token %s", c.token))
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, c.parseErrorResponse(resp)
	}
	
	var token registrationToken
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return nil, fmt.Errorf("failed to decode registration token: %w", err)
	}
	
	return &token, nil
}

// getActionsServiceAdminConnection gets the Actions Service URL and admin token
func (c *ActionsServiceClient) getActionsServiceAdminConnection(ctx context.Context, regToken *registrationToken, org string) (*ActionsServiceAdminConnection, error) {
	// Use the correct Actions Service endpoint
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
		return nil, fmt.Errorf("Actions Service not available on this GitHub Enterprise Server. Please ensure you have a compatible version that supports the Actions Service API. Status: %d", resp.StatusCode)
	}
	
	var conn ActionsServiceAdminConnection
	if err := json.NewDecoder(resp.Body).Decode(&conn); err != nil {
		return nil, fmt.Errorf("Actions Service endpoint returned invalid response. This feature may not be enabled on your GitHub Enterprise Server: %w", err)
	}
	
	c.logger.Info("Successfully obtained Actions Service admin connection")
	
	return &conn, nil
}

// refreshTokenIfNeeded refreshes the admin token if it's close to expiry
func (c *ActionsServiceClient) refreshTokenIfNeeded(ctx context.Context) error {
	if time.Now().Before(c.adminTokenExpiry.Add(-5*time.Minute)) {
		return nil // Token is still valid
	}
	
	c.logger.Info("Refreshing admin token")
	
	// For Actions Service, we need to re-authenticate
	return fmt.Errorf("token refresh not implemented - please reinitialize the client")
}

// GetOrCreateRunnerScaleSet gets or creates a runner scale set
func (c *ActionsServiceClient) GetOrCreateRunnerScaleSet(ctx context.Context, name string, labels []string) (*RunnerScaleSet, error) {
	if err := c.refreshTokenIfNeeded(ctx); err != nil {
		return nil, fmt.Errorf("failed to refresh token: %w", err)
	}
	
	c.logger.Info("Getting or creating runner scale set", "name", name)
	
	// For now, return a mock scale set - in a real implementation,
	// this would call the actual scale set creation APIs
	scaleSet := &RunnerScaleSet{
		ID:            1,
		Name:          name,
		RunnerGroupID: 1,
		Labels:        make([]Label, len(labels)),
		RunnerSetting: RunnerSetting{
			Ephemeral: true,
			IsElastic: true,
		},
	}
	
	// Convert string labels to Label objects
	for i, label := range labels {
		scaleSet.Labels[i] = Label{
			ID:   i + 1,
			Name: label,
			Type: "custom",
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
	url := fmt.Sprintf("%s%s?api-version=%s", c.actionsServiceURL, path, apiVersion)
	
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
	url := fmt.Sprintf("%s%s?api-version=%s", c.actionsServiceURL, path, apiVersion)
	
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

// parseErrorResponse parses error responses from the API
func (c *ActionsServiceClient) parseErrorResponse(resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &ActionsError{
			StatusCode: resp.StatusCode,
			ActivityID: resp.Header.Get("X-GitHub-Request-Id"),
			Message:    "Failed to read error response",
		}
	}
	
	c.logger.Info("API error response",
		"statusCode", resp.StatusCode,
		"requestId", resp.Header.Get("X-GitHub-Request-Id"),
		"body", string(body))
	
	// Try to parse as GitHub API error
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
	
	return &ActionsError{
		StatusCode: resp.StatusCode,
		ActivityID: resp.Header.Get("X-GitHub-Request-Id"),
		Message:    strings.Join(messages, "; "),
		Err:        fmt.Errorf("documentation: %s", ghErr.DocumentationURL),
	}
}