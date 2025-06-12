package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
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

// ActionsServiceAdminConnection represents the response from admin connection endpoint
type ActionsServiceAdminConnection struct {
	ActionsServiceURL *string `json:"url,omitempty"`
	AdminToken        *string `json:"token,omitempty"`
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
	
	// First, verify the token is valid and has proper permissions
	if err := c.verifyToken(ctx, org); err != nil {
		return fmt.Errorf("token verification failed: %w", err)
	}
	
	// Check GHES version compatibility
	if err := c.checkGHESCompatibility(ctx); err != nil {
		return err
	}
	
	// First, try to get a registration token to discover the Actions Service URL
	regToken, err := c.getRegistrationToken(ctx, org)
	if err != nil {
		return fmt.Errorf("failed to get registration token: %w", err)
	}
	
	c.logger.Info("Successfully obtained registration token")
	
	// Get Actions Service admin connection
	adminConn, err := c.getActionsServiceAdminConnection(ctx, regToken, org)
	if err != nil {
		return fmt.Errorf("failed to get Actions Service admin connection: %w", err)
	}
	
	if adminConn.ActionsServiceURL == nil || adminConn.AdminToken == nil {
		return fmt.Errorf("invalid Actions Service connection response - missing URL or token")
	}
	
	c.actionsServiceURL = *adminConn.ActionsServiceURL
	c.adminToken = *adminConn.AdminToken
	c.adminTokenExpiry = time.Now().Add(1 * time.Hour) // Tokens typically expire in 1 hour
	
	c.logger.Info("Successfully initialized Actions Service client",
		"actionsServiceURL", c.actionsServiceURL,
		"tokenExpiry", c.adminTokenExpiry,
	)
	
	return nil
}

// getRegistrationToken gets a registration token from GitHub
func (c *ActionsServiceClient) getRegistrationToken(ctx context.Context, org string) (*registrationToken, error) {
	path := fmt.Sprintf("/api/v3/orgs/%s/actions/runners/registration-token", org)
	
	req, err := c.NewGitHubAPIRequest(ctx, "POST", path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	
	// Set authentication headers after creating request
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

// NewGitHubAPIRequest creates a new GitHub API request (matching official controller pattern)
func (c *ActionsServiceClient) NewGitHubAPIRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse base URL: %w", err)
	}
	
	u.Path = path
	
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	
	// Only set User-Agent header like the official controller
	req.Header.Set("User-Agent", "ghaec2-scaler/1.0")
	
	return req, nil
}

// getActionsServiceAdminConnection gets the Actions Service URL and admin token
func (c *ActionsServiceClient) getActionsServiceAdminConnection(ctx context.Context, regToken *registrationToken, org string) (*ActionsServiceAdminConnection, error) {
	path := "/actions/runner-registration"
	
	// Create request body exactly like the official controller
	body := struct {
		Url         string `json:"url"`
		RunnerEvent string `json:"runner_event"`
	}{
		Url:         fmt.Sprintf("%s/%s", c.baseURL, org), // GitHub config URL (org scope)
		RunnerEvent: "register",
	}
	
	buf := &bytes.Buffer{}
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	
	if err := enc.Encode(body); err != nil {
		return nil, fmt.Errorf("failed to encode body: %w", err)
	}
	
	// Use NewGitHubAPIRequest to match official controller pattern
	req, err := c.NewGitHubAPIRequest(ctx, http.MethodPost, path, buf)
	if err != nil {
		return nil, fmt.Errorf("failed to create new GitHub API request: %w", err)
	}
	
	// Override Authorization header for RemoteAuth
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("RemoteAuth %s", regToken.Token))
	
	c.logger.Info("Getting Actions Service admin connection (trying Actions Service API)",
		"registrationURL", req.URL.String(),
		"regTokenLength", len(regToken.Token))
	
	// Implement retry logic exactly like official controller
	var resp *http.Response
	retry := 0
	for {
		var err error
		resp, err = c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to issue the request: %w", err)
		}
		defer resp.Body.Close()
		
		// Success case
		if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
			break
		}
		
		// Read response body for error analysis
		body, readErr := io.ReadAll(resp.Body)
		var innerErr error
		if readErr != nil {
			innerErr = readErr
		} else {
			innerErr = fmt.Errorf("%s", string(body))
		}
		
		// Check if this is an HTML response (indication GHES doesn't support Actions Service)
		if resp.StatusCode == 404 || strings.Contains(string(body), "<html") || strings.Contains(string(body), "<!DOCTYPE") {
			return nil, fmt.Errorf("Actions Service API not supported on this GitHub Enterprise Server version. " +
				"The endpoint '/actions/runner-registration' returned HTML instead of JSON. " +
				"Please upgrade to a GHES version that supports Actions Service API (3.5+) or use traditional runners")
		}
		
		// Handle auth errors with retry (like official controller)
		if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
			return nil, fmt.Errorf("Actions Service registration failed (status: %d): %w", resp.StatusCode, innerErr)
		}
		
		retry++
		if retry > 5 {
			return nil, fmt.Errorf("unable to register with Actions Service after 5 retries: %w", innerErr)
		}
		
		// Add exponential backoff + jitter like official controller
		baseDelay := 500 * time.Millisecond
		jitter := time.Duration(rand.Intn(1000))
		maxDelay := 20 * time.Second
		delay := baseDelay*(1<<retry) + jitter*time.Millisecond
		
		if delay > maxDelay {
			delay = maxDelay
		}
		
		c.logger.Info("Retrying Actions Service registration", "retry", retry, "delay", delay)
		time.Sleep(delay)
	}
	
	var actionsServiceAdminConnection *ActionsServiceAdminConnection
	if err := json.NewDecoder(resp.Body).Decode(&actionsServiceAdminConnection); err != nil {
		return nil, fmt.Errorf("failed to decode Actions Service response: %w", err)
	}
	
	if actionsServiceAdminConnection.ActionsServiceURL == nil || actionsServiceAdminConnection.AdminToken == nil {
		return nil, fmt.Errorf("invalid Actions Service connection response - missing URL or token")
	}
	
	c.logger.Info("Successfully obtained Actions Service admin connection",
		"actionsServiceURL", *actionsServiceAdminConnection.ActionsServiceURL)
	
	return actionsServiceAdminConnection, nil
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

// checkGHESCompatibility checks if the GHES version supports Actions Service API
func (c *ActionsServiceClient) checkGHESCompatibility(ctx context.Context) error {
	// Try to get GHES version info
	path := "/api/v3/meta"
	req, err := c.NewGitHubAPIRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		c.logger.Info("Could not create version check request", "error", err)
		return nil // Don't fail on version check
	}
	
	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Info("Could not check GHES version", "error", err)
		return nil // Don't fail on version check
	}
	defer resp.Body.Close()
	
	if resp.StatusCode == 200 {
		var meta struct {
			GitHubServicesGheVersion string `json:"github_services_ghe_version"`
			InstalledVersion         string `json:"installed_version"`
		}
		
		if err := json.NewDecoder(resp.Body).Decode(&meta); err == nil {
			version := meta.GitHubServicesGheVersion
			if version == "" {
				version = meta.InstalledVersion
			}
			
			c.logger.Info("Detected GitHub Enterprise Server version", "version", version)
			
			// Actions Service API was introduced in GHES 3.5+
			if strings.HasPrefix(version, "3.0") || strings.HasPrefix(version, "3.1") || 
			   strings.HasPrefix(version, "3.2") || strings.HasPrefix(version, "3.3") || 
			   strings.HasPrefix(version, "3.4") {
				return fmt.Errorf("GitHub Enterprise Server version %s detected. Actions Service API requires GHES 3.5 or later. "+
					"Please upgrade your GHES instance or use traditional runners", version)
			}
		}
	}
	
	return nil
}

// verifyToken checks if the GitHub token is valid and has required permissions
func (c *ActionsServiceClient) verifyToken(ctx context.Context, org string) error {
	c.logger.Info("Verifying GitHub token permissions", "organization", org)
	
	// Test 1: Check if token can access the API at all
	path := "/api/v3/user"
	req, err := c.NewGitHubAPIRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return fmt.Errorf("failed to create user request: %w", err)
	}
	
	// Add authentication headers
	req.Header.Set("Authorization", fmt.Sprintf("token %s", c.token))
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute user request: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode == 401 {
		return fmt.Errorf("GitHub token is invalid or expired. Please check your GITHUB_TOKEN environment variable")
	}
	
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("token verification failed (status: %d): %s", resp.StatusCode, string(body))
	}
	
	var user struct {
		Login string `json:"login"`
		Type  string `json:"type"`
	}
	
	if err := json.NewDecoder(resp.Body).Decode(&user); err == nil {
		c.logger.Info("Token validated successfully", "user", user.Login, "type", user.Type)
	}
	
	// Test 2: Check if token can access the organization
	path = fmt.Sprintf("/api/v3/orgs/%s", org)
	req, err = c.NewGitHubAPIRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return fmt.Errorf("failed to create org request: %w", err)
	}
	
	// Add authentication headers
	req.Header.Set("Authorization", fmt.Sprintf("token %s", c.token))
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	
	resp, err = c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute org request: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode == 404 {
		return fmt.Errorf("organization '%s' not found or token doesn't have access to it", org)
	}
	
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("organization access check failed (status: %d): %s", resp.StatusCode, string(body))
	}
	
	c.logger.Info("Token has access to organization", "organization", org)
	
	// Test 3: Check if token has Actions permissions
	path = fmt.Sprintf("/api/v3/orgs/%s/actions/permissions", org)
	req, err = c.NewGitHubAPIRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return fmt.Errorf("failed to create actions permissions request: %w", err)
	}
	
	// Add authentication headers
	req.Header.Set("Authorization", fmt.Sprintf("token %s", c.token))
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	
	resp, err = c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute actions permissions request: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode == 403 {
		return fmt.Errorf("token doesn't have Actions permissions. Please ensure the token has 'actions:read' or 'admin:org' scope")
	}
	
	if resp.StatusCode == 200 {
		c.logger.Info("Token has Actions permissions")
	} else {
		c.logger.Info("Actions permissions check returned status", "status", resp.StatusCode)
	}
	
	return nil
}

// AcquireJobs acquires available jobs
func (c *ActionsServiceClient) AcquireJobs(ctx context.Context, runnerScaleSetID int, messageQueueAccessToken string, requestIDs []int64) ([]int64, error) {
	payload := map[string]interface{}{
		"requestIds": requestIDs,
	}
	
	url := fmt.Sprintf("%s/%s/%d/jobs", c.actionsServiceURL, scaleSetEndpoint, runnerScaleSetID)
	
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}
	
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	
	req.Header.Set("Authorization", "Bearer "+messageQueueAccessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "ghaec2-scaler/1.0")
	
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to acquire jobs (HTTP %d): %s", resp.StatusCode, string(body))
	}
	
	var result struct {
		Value []int64 `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode acquire jobs response: %w", err)
	}
	
	return result.Value, nil
}

// RefreshMessageSession refreshes an existing message session
func (c *ActionsServiceClient) RefreshMessageSession(ctx context.Context, runnerScaleSetID int, sessionID *uuid.UUID) (*RunnerScaleSetSession, error) {
	if sessionID == nil {
		return nil, fmt.Errorf("session ID is nil")
	}
	
	url := fmt.Sprintf("%s/%s/%d/sessions/%s", c.actionsServiceURL, scaleSetEndpoint, runnerScaleSetID, sessionID.String())
	resp, err := c.makeActionsServiceRequest(ctx, "POST", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to refresh message session: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to refresh message session (HTTP %d): %s", resp.StatusCode, string(body))
	}
	
	var session RunnerScaleSetSession
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		return nil, fmt.Errorf("failed to decode session response: %w", err)
	}
	
	return &session, nil
}

// DeleteMessage deletes a processed message
func (c *ActionsServiceClient) DeleteMessage(ctx context.Context, messageQueueURL, messageQueueAccessToken string, messageID int64) error {
	if messageQueueURL == "" || messageID == 0 {
		return nil // Nothing to delete
	}
	
	params := url.Values{}
	params.Set("api-version", apiVersion)
	params.Set("messageId", fmt.Sprintf("%d", messageID))
	
	url := fmt.Sprintf("%s?%s", messageQueueURL, params.Encode())
	
	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	
	req.Header.Set("Authorization", "Bearer "+messageQueueAccessToken)
	req.Header.Set("User-Agent", "ghaec2-scaler/1.0")
	
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to delete message (HTTP %d): %s", resp.StatusCode, string(body))
	}
	
	return nil
}

// DeleteMessageSession deletes a message session
func (c *ActionsServiceClient) DeleteMessageSession(ctx context.Context, runnerScaleSetID int, sessionID *uuid.UUID) error {
	if sessionID == nil {
		return nil // Nothing to delete
	}
	
	url := fmt.Sprintf("%s/%s/%d/sessions/%s", c.actionsServiceURL, scaleSetEndpoint, runnerScaleSetID, sessionID.String())
	resp, err := c.makeActionsServiceRequest(ctx, "DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("failed to delete message session: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to delete message session (HTTP %d): %s", resp.StatusCode, string(body))
	}
	
	return nil
}

// makeActionsServiceRequest makes a request to the Actions Service
func (c *ActionsServiceClient) makeActionsServiceRequest(ctx context.Context, method, url string, payload interface{}) (*http.Response, error) {
	var body io.Reader
	if payload != nil {
		jsonData, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal payload: %w", err)
		}
		body = bytes.NewBuffer(jsonData)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Use admin token for Actions Service requests
	if c.adminToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.adminToken)
	} else {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "ghaec2-scaler/1.0")
	
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return c.httpClient.Do(req)
}