package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/go-logr/logr"
	"github.com/google/uuid"
)

// MessageQueueScaler implements the same pattern as actions-runner-controller AutoscalingListener
// It polls GitHub's Actions Service message queue for job events and scales EC2 instances accordingly
type MessageQueueScaler struct {
	config        *Config
	ec2Client     *ec2.Client
	actionsClient *ActionsServiceClient
	logger        logr.Logger

	// Scale set and session management (like AutoscalingListener)
	scaleSet      *RunnerScaleSet
	session       *RunnerScaleSetSession
	lastMessageID int64

	// Runner tracking
	runnerTracker *EC2RunnerTracker
	mu            sync.RWMutex
}

// EC2RunnerTracker tracks EC2 instances acting as GitHub Actions runners
type EC2RunnerTracker struct {
	mu        sync.RWMutex
	instances map[string]*EC2RunnerInstance // instanceID -> instance info
	logger    logr.Logger
}

// EC2RunnerInstance represents an EC2 instance running as a GitHub Actions runner
type EC2RunnerInstance struct {
	InstanceID   string    `json:"instanceId"`
	LaunchTime   time.Time `json:"launchTime"`
	State        string    `json:"state"` // "pending", "running", "terminating"
	JobID        int64     `json:"jobId,omitempty"`
	RunnerID     int64     `json:"runnerId,omitempty"`
	Labels       []string  `json:"labels"`
	LastActivity time.Time `json:"lastActivity"`
}

// NewMessageQueueScaler creates a new message queue-based scaler
func NewMessageQueueScaler(config *Config, ec2Client *ec2.Client, logger logr.Logger) *MessageQueueScaler {
	actionsClient := NewActionsServiceClient(config.GitHubEnterpriseURL, config.GitHubToken, logger.WithName("actions-client"))

	tracker := &EC2RunnerTracker{
		instances: make(map[string]*EC2RunnerInstance),
		logger:    logger.WithName("runner-tracker"),
	}

	return &MessageQueueScaler{
		config:        config,
		ec2Client:     ec2Client,
		actionsClient: actionsClient,
		logger:        logger.WithName("message-queue-scaler"),
		runnerTracker: tracker,
	}
}

// Run starts the message queue scaler (following AutoscalingListener.Listen pattern)
func (s *MessageQueueScaler) Run(ctx context.Context) error {
	s.logger.Info("Starting Message Queue Scaler")

	// Initialize Actions Service connection (like actions-runner-controller)
	if err := s.initializeActionsService(ctx); err != nil {
		return fmt.Errorf("failed to initialize Actions Service: %w", err)
	}

	// Initialize or get existing runner scale set
	if err := s.initializeScaleSet(ctx); err != nil {
		return fmt.Errorf("failed to initialize scale set: %w", err)
	}

	// Create message session (like AutoscalingListener.createSession)
	if err := s.createMessageSession(ctx); err != nil {
		return fmt.Errorf("failed to create message session: %w", err)
	}
	defer s.cleanupSession(ctx)

	// Handle initial statistics and start message polling loop (like Listener.Listen)
	return s.startMessagePolling(ctx)
}

// initializeActionsService initializes the Actions Service connection
func (s *MessageQueueScaler) initializeActionsService(ctx context.Context) error {
	s.logger.Info("Initializing Actions Service connection")

	if err := s.actionsClient.Initialize(ctx, s.config.OrganizationName); err != nil {
		return fmt.Errorf("failed to initialize Actions Service client: %w", err)
	}

	s.logger.Info("Actions Service connection established",
		"actionsServiceURL", s.actionsClient.actionsServiceURL)

	return nil
}

// initializeScaleSet creates or gets the runner scale set (like autoscalingrunnerset_controller.go)
func (s *MessageQueueScaler) initializeScaleSet(ctx context.Context) error {
	s.logger.Info("Initializing runner scale set", "name", s.config.RunnerScaleSetName)

	scaleSet, err := s.actionsClient.GetOrCreateRunnerScaleSet(ctx, s.config.RunnerScaleSetName, s.config.RunnerLabels, s.config.RunnerGroupID)
	if err != nil {
		return fmt.Errorf("failed to get or create scale set: %w", err)
	}

	s.scaleSet = scaleSet
	s.config.RunnerScaleSetID = scaleSet.ID

	s.logger.Info("Scale set initialized",
		"id", scaleSet.ID,
		"name", scaleSet.Name,
		"runnerGroupId", scaleSet.RunnerGroupID,
		"labels", s.extractLabelNames(scaleSet.Labels),
	)

	return nil
}

// createMessageSession creates a message session (like Listener.createSession)
func (s *MessageQueueScaler) createMessageSession(ctx context.Context) error {
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "ghaec2-scaler"
	}

	// Add a unique suffix to avoid conflicts with other instances
	randomBytes := make([]byte, 4)
	rand.Read(randomBytes)
	uniqueOwner := fmt.Sprintf("%s-%s", hostname, hex.EncodeToString(randomBytes))

	s.logger.Info("Creating message session", "owner", uniqueOwner)

	session, err := s.actionsClient.CreateMessageSession(ctx, s.config.RunnerScaleSetID, uniqueOwner)
	if err != nil {
		// Check if it's a session conflict error
		if strings.Contains(err.Error(), "already has an active session") {
			s.logger.Info("Session conflict detected, attempting to resolve", "owner", uniqueOwner)
			
			// Try with a different owner name
			randomBytes = make([]byte, 8)
			rand.Read(randomBytes)
			uniqueOwner = fmt.Sprintf("ghaec2-%s", hex.EncodeToString(randomBytes))
			
			s.logger.Info("Retrying with different owner", "owner", uniqueOwner)
			session, err = s.actionsClient.CreateMessageSession(ctx, s.config.RunnerScaleSetID, uniqueOwner)
			if err != nil {
				return fmt.Errorf("failed to create message session after retry: %w", err)
			}
		} else {
			return fmt.Errorf("failed to create message session: %w", err)
		}
	}

	s.session = session
	s.lastMessageID = 0

	s.logger.Info("Message session created",
		"sessionId", session.SessionID,
		"messageQueueUrl", session.MessageQueueURL,
		"owner", uniqueOwner)

	return nil
}

// startMessagePolling starts the message polling loop (exactly like Listener.Listen)
func (s *MessageQueueScaler) startMessagePolling(ctx context.Context) error {
	// Handle initial message with statistics (exactly like Listener.Listen does)
	initialMessage := &RunnerScaleSetMessage{
		MessageID:   0,
		MessageType: "RunnerScaleSetJobMessages",
		Statistics:  s.session.Statistics,
		Body:        "",
	}

	if s.session.Statistics == nil {
		return fmt.Errorf("session statistics is nil")
	}

	s.logger.Info("Initial runner scale set statistics",
		"availableJobs", s.session.Statistics.TotalAvailableJobs,
		"assignedJobs", s.session.Statistics.TotalAssignedJobs,
		"runningJobs", s.session.Statistics.TotalRunningJobs,
		"registeredRunners", s.session.Statistics.TotalRegisteredRunners,
		"busyRunners", s.session.Statistics.TotalBusyRunners,
		"idleRunners", s.session.Statistics.TotalIdleRunners,
	)

	// Handle initial desired runner count (like Listener.Listen)
	desiredRunners, err := s.handleDesiredRunnerCount(ctx, initialMessage.Statistics.TotalAssignedJobs, 0)
	if err != nil {
		return fmt.Errorf("handling initial message failed: %w", err)
	}
	s.logger.Info("Initial desired runners calculated", "desiredRunners", desiredRunners)

	// Start the message polling loop (exactly like Listener.Listen)
	s.logger.Info("Starting message polling loop")

	// Add a ticker for more frequent polling when no messages are received
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Add a diagnostic ticker to periodically check for issues
	diagnosticTicker := time.NewTicker(2 * time.Minute)
	defer diagnosticTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			// Periodic polling attempt
			s.logger.V(1).Info("Periodic message poll check")
		case <-diagnosticTicker.C:
			// Run diagnostics periodically
			if err := s.runDiagnostics(ctx); err != nil {
				s.logger.Error(err, "Diagnostics failed")
			}
		default:
		}

		// Get next message (like Listener.getMessage)
		msg, err := s.getMessage(ctx)
		if err != nil {
			s.logger.Error(err, "Failed to get message, will retry in 5 seconds")
			time.Sleep(5 * time.Second)
			continue
		}

		if msg == nil {
			// No new messages - handle as null message (like Listener.Listen)
			s.logger.V(1).Info("No new messages received, handling as null message")
			_, err := s.handleDesiredRunnerCount(ctx, 0, 0)
			if err != nil {
				s.logger.Error(err, "Failed to handle null message")
				continue
			}
			time.Sleep(5 * time.Second) // Wait before next poll
			continue
		}

		s.logger.Info("Received message", 
			"messageId", msg.MessageID, 
			"messageType", msg.MessageType,
			"bodyLength", len(msg.Body),
			"hasStatistics", msg.Statistics != nil)

		// Handle the message (like Listener.handleMessage)
		// Use context.WithoutCancel to avoid cancelling message handling
		if err := s.handleMessage(context.WithoutCancel(ctx), msg); err != nil {
			s.logger.Error(err, "Failed to handle message, will continue polling")
			continue
		}
	}
}

// getMessage gets the next message from the queue (like Listener.getMessage)
func (s *MessageQueueScaler) getMessage(ctx context.Context) (*RunnerScaleSetMessage, error) {
	s.logger.V(1).Info("Getting next message", "lastMessageID", s.lastMessageID)

	msg, err := s.actionsClient.GetMessage(ctx,
		s.session.MessageQueueURL,
		s.session.MessageQueueAccessToken,
		s.lastMessageID,
		s.config.MaxRunners)

	if err == nil {
		return msg, nil
	}

	// Handle token expiration (like Listener.getMessage does)
	if isMessageQueueTokenExpiredError(err) {
		if err := s.refreshSession(ctx); err != nil {
			return nil, err
		}

		// Retry after session refresh
		msg, err = s.actionsClient.GetMessage(ctx,
			s.session.MessageQueueURL,
			s.session.MessageQueueAccessToken,
			s.lastMessageID,
			s.config.MaxRunners)
		if err != nil {
			return nil, fmt.Errorf("failed to get next message after session refresh: %w", err)
		}
	} else {
		return nil, fmt.Errorf("failed to get next message: %w", err)
	}

	return msg, nil
}

// handleMessage processes a message (like Listener.handleMessage)
func (s *MessageQueueScaler) handleMessage(ctx context.Context, msg *RunnerScaleSetMessage) error {
	parsedMsg, err := s.parseMessage(ctx, msg)
	if err != nil {
		return fmt.Errorf("failed to parse message: %w", err)
	}

	// Handle available jobs (like Listener.handleMessage)
	if len(parsedMsg.jobsAvailable) > 0 {
		acquiredJobIDs, err := s.acquireAvailableJobs(ctx, parsedMsg.jobsAvailable)
		if err != nil {
			return fmt.Errorf("failed to acquire jobs: %w", err)
		}
		s.logger.Info("Jobs acquired", "count", len(acquiredJobIDs), "requestIds", acquiredJobIDs)
	}

	// Update last message ID
	s.lastMessageID = msg.MessageID

	// Delete the processed message
	if err := s.deleteLastMessage(ctx); err != nil {
		return fmt.Errorf("failed to delete message: %w", err)
	}

	// Handle job started events
	for _, jobStarted := range parsedMsg.jobsStarted {
		if err := s.handleJobStarted(ctx, jobStarted); err != nil {
			return fmt.Errorf("failed to handle job started: %w", err)
		}
	}

	// Handle desired runner count based on statistics
	desiredRunners, err := s.handleDesiredRunnerCount(ctx, parsedMsg.statistics.TotalAssignedJobs, len(parsedMsg.jobsCompleted))
	if err != nil {
		return fmt.Errorf("failed to handle desired runner count: %w", err)
	}

	s.logger.Info("Desired runners calculated", "desiredRunners", desiredRunners)
	return nil
}

// parsedMessage holds parsed message components (like Listener.parsedMessage)
type parsedMessage struct {
	statistics    *RunnerScaleSetStatistic
	jobsStarted   []*JobStarted
	jobsAvailable []*JobAvailable
	jobsCompleted []*JobCompleted
}

// Job message types (following actions-runner-controller patterns)

// JobStarted represents a job started message
type JobStarted struct {
	RunnerID   int    `json:"runnerId"`
	RunnerName string `json:"runnerName"`
	JobMessageBase
}

// JobCompleted represents a job completed message
type JobCompleted struct {
	Result     string `json:"result"`
	RunnerID   int    `json:"runnerId"`
	RunnerName string `json:"runnerName"`
	JobMessageBase
}

// parseMessage parses a message (like Listener.parseMessage)
func (s *MessageQueueScaler) parseMessage(ctx context.Context, msg *RunnerScaleSetMessage) (*parsedMessage, error) {
	if msg.MessageType != "RunnerScaleSetJobMessages" {
		s.logger.Info("Skipping message", "messageType", msg.MessageType)
		return nil, fmt.Errorf("invalid message type: %s", msg.MessageType)
	}

	s.logger.Info("Processing message", "messageId", msg.MessageID, "messageType", msg.MessageType)

	if msg.Statistics == nil {
		return nil, fmt.Errorf("invalid message: statistics is nil")
	}

	s.logger.Info("Runner scale set statistics",
		"availableJobs", msg.Statistics.TotalAvailableJobs,
		"assignedJobs", msg.Statistics.TotalAssignedJobs,
		"runningJobs", msg.Statistics.TotalRunningJobs,
		"registeredRunners", msg.Statistics.TotalRegisteredRunners,
		"busyRunners", msg.Statistics.TotalBusyRunners,
		"idleRunners", msg.Statistics.TotalIdleRunners,
	)

	// Parse batched messages in the body
	var batchedMessages []json.RawMessage
	if len(msg.Body) > 0 {
		s.logger.Info("Message has body content", "bodyLength", len(msg.Body), "body", msg.Body)
		if err := json.Unmarshal([]byte(msg.Body), &batchedMessages); err != nil {
			s.logger.Error(err, "Failed to unmarshal batched messages", "body", msg.Body)
			return nil, fmt.Errorf("failed to unmarshal batched messages: %w", err)
		}
		s.logger.Info("Parsed batched messages", "count", len(batchedMessages))
	} else {
		s.logger.Info("Message has empty body")
	}

	parsedMsg := &parsedMessage{
		statistics: msg.Statistics,
	}

	// Parse individual messages (like Listener.parseMessage)
	for i, rawMsg := range batchedMessages {
		var msgType struct {
			MessageType string `json:"messageType"`
		}
		if err := json.Unmarshal(rawMsg, &msgType); err != nil {
			s.logger.Error(err, "Failed to unmarshal message type", "index", i, "rawMessage", string(rawMsg))
			continue
		}

		s.logger.Info("Processing batched message", "index", i, "messageType", msgType.MessageType)

		switch msgType.MessageType {
		case "JobAvailable":
			var jobAvailable JobAvailable
			if err := json.Unmarshal(rawMsg, &jobAvailable); err == nil {
				s.logger.Info("Found JobAvailable message", 
					"runnerRequestId", jobAvailable.RunnerRequestID,
					"repositoryName", jobAvailable.RepositoryName,
					"ownerName", jobAvailable.OwnerName,
					"requestLabels", jobAvailable.RequestLabels)
				parsedMsg.jobsAvailable = append(parsedMsg.jobsAvailable, &jobAvailable)
			} else {
				s.logger.Error(err, "Failed to unmarshal JobAvailable message", "rawMessage", string(rawMsg))
			}
		case "JobStarted":
			var jobStarted JobStarted
			if err := json.Unmarshal(rawMsg, &jobStarted); err == nil {
				s.logger.Info("Found JobStarted message", 
					"runnerRequestId", jobStarted.RunnerRequestID,
					"runnerId", jobStarted.RunnerID,
					"runnerName", jobStarted.RunnerName)
				parsedMsg.jobsStarted = append(parsedMsg.jobsStarted, &jobStarted)
			} else {
				s.logger.Error(err, "Failed to unmarshal JobStarted message", "rawMessage", string(rawMsg))
			}
		case "JobCompleted":
			var jobCompleted JobCompleted
			if err := json.Unmarshal(rawMsg, &jobCompleted); err == nil {
				s.logger.Info("Found JobCompleted message", 
					"runnerRequestId", jobCompleted.RunnerRequestID,
					"runnerId", jobCompleted.RunnerID,
					"result", jobCompleted.Result)
				parsedMsg.jobsCompleted = append(parsedMsg.jobsCompleted, &jobCompleted)
			} else {
				s.logger.Error(err, "Failed to unmarshal JobCompleted message", "rawMessage", string(rawMsg))
			}
		default:
			s.logger.Info("Unknown message type in batch", "messageType", msgType.MessageType, "rawMessage", string(rawMsg))
		}
	}

	s.logger.Info("Parsed message",
		"jobsAvailable", len(parsedMsg.jobsAvailable),
		"jobsStarted", len(parsedMsg.jobsStarted),
		"jobsCompleted", len(parsedMsg.jobsCompleted))

	return parsedMsg, nil
}

// acquireAvailableJobs acquires available jobs (like Listener.acquireAvailableJobs)
func (s *MessageQueueScaler) acquireAvailableJobs(ctx context.Context, jobsAvailable []*JobAvailable) ([]int64, error) {
	ids := make([]int64, 0, len(jobsAvailable))
	for _, job := range jobsAvailable {
		ids = append(ids, job.RunnerRequestID)
	}

	s.logger.Info("Acquiring jobs", "count", len(ids), "requestIds", ids)

	idsAcquired, err := s.actionsClient.AcquireJobs(ctx, s.config.RunnerScaleSetID, s.actionsClient.adminToken, ids)
	if err == nil {
		return idsAcquired, nil
	}

	// Handle token expiration
	if isMessageQueueTokenExpiredError(err) {
		if err := s.refreshSession(ctx); err != nil {
			return nil, err
		}

		idsAcquired, err = s.actionsClient.AcquireJobs(ctx, s.config.RunnerScaleSetID, s.session.MessageQueueAccessToken, ids)
		if err != nil {
			return nil, fmt.Errorf("failed to acquire jobs after session refresh: %w", err)
		}
	} else {
		return nil, fmt.Errorf("failed to acquire jobs: %w", err)
	}

	return idsAcquired, nil
}

// handleJobStarted handles a job started event
func (s *MessageQueueScaler) handleJobStarted(ctx context.Context, jobInfo *JobStarted) error {
	s.logger.Info("Job started",
		"runnerId", jobInfo.RunnerID,
		"runnerName", jobInfo.RunnerName,
		"repository", jobInfo.RepositoryName,
		"workflowRef", jobInfo.JobWorkflowRef)

	// Update our tracking
	s.runnerTracker.mu.Lock()
	for _, instance := range s.runnerTracker.instances {
		if instance.RunnerID == int64(jobInfo.RunnerID) {
			instance.JobID = jobInfo.RunnerRequestID
			instance.LastActivity = time.Now()
			break
		}
	}
	s.runnerTracker.mu.Unlock()

	return nil
}

// handleDesiredRunnerCount handles desired runner count calculation (like Handler.HandleDesiredRunnerCount)
func (s *MessageQueueScaler) handleDesiredRunnerCount(ctx context.Context, assignedJobs, completedJobs int) (int, error) {
	currentRunners, err := s.getCurrentRunnerCount(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get current runner count: %w", err)
	}

	// Calculate desired runners based on assigned jobs (following actions-runner-controller logic)
	desiredRunners := assignedJobs

	// Ensure we stay within min/max bounds
	if desiredRunners < s.config.MinRunners {
		desiredRunners = s.config.MinRunners
	}
	if desiredRunners > s.config.MaxRunners {
		desiredRunners = s.config.MaxRunners
	}

	s.logger.Info("Scaling decision",
		"currentRunners", currentRunners,
		"assignedJobs", assignedJobs,
		"completedJobs", completedJobs,
		"desiredRunners", desiredRunners)

	// Scale up if needed
	if desiredRunners > currentRunners {
		runnersToCreate := desiredRunners - currentRunners
		s.logger.Info("Scaling up", "runnersToCreate", runnersToCreate)

		for i := 0; i < runnersToCreate; i++ {
			if err := s.createRunner(ctx); err != nil {
				s.logger.Error(err, "Failed to create runner", "attempt", i+1)
			}
		}
	}

	// Scale down if needed
	if desiredRunners < currentRunners {
		runnersToTerminate := currentRunners - desiredRunners
		s.logger.Info("Scaling down", "runnersToTerminate", runnersToTerminate)

		if err := s.terminateIdleRunners(ctx, runnersToTerminate); err != nil {
			s.logger.Error(err, "Failed to terminate idle runners")
		}
	}

	return desiredRunners, nil
}

// getCurrentRunnerCount gets the current number of EC2 runners
func (s *MessageQueueScaler) getCurrentRunnerCount(ctx context.Context) (int, error) {
	// Implementation to count current EC2 instances with our tags
	s.runnerTracker.mu.RLock()
	count := len(s.runnerTracker.instances)
	s.runnerTracker.mu.RUnlock()

	// TODO: Also query EC2 to get actual current count and sync with tracker
	return count, nil
}

// createRunner creates a new EC2 runner instance
func (s *MessageQueueScaler) createRunner(ctx context.Context) error {
	s.logger.Info("Creating new EC2 runner instance")

	// TODO: Implement actual EC2 instance creation
	// This should:
	// 1. Launch EC2 spot instance with runner configuration
	// 2. Install GitHub Actions runner
	// 3. Register runner with GitHub
	// 4. Add to runnerTracker

	// Placeholder implementation
	instanceID := fmt.Sprintf("i-%s", uuid.New().String()[:8])
	instance := &EC2RunnerInstance{
		InstanceID:   instanceID,
		LaunchTime:   time.Now(),
		State:        "pending",
		Labels:       s.config.RunnerLabels,
		LastActivity: time.Now(),
	}

	s.runnerTracker.mu.Lock()
	s.runnerTracker.instances[instanceID] = instance
	s.runnerTracker.mu.Unlock()

	s.logger.Info("EC2 runner instance created", "instanceId", instanceID)
	return nil
}

// terminateIdleRunners terminates idle runner instances
func (s *MessageQueueScaler) terminateIdleRunners(ctx context.Context, count int) error {
	s.logger.Info("Terminating idle runners", "count", count)

	s.runnerTracker.mu.Lock()
	defer s.runnerTracker.mu.Unlock()

	// Find idle runners to terminate
	var idleRunners []*EC2RunnerInstance
	for _, instance := range s.runnerTracker.instances {
		if instance.State == "running" && instance.JobID == 0 {
			idleRunners = append(idleRunners, instance)
		}
	}

	// Terminate the requested number of idle runners
	terminated := 0
	for _, instance := range idleRunners {
		if terminated >= count {
			break
		}

		s.logger.Info("Terminating idle runner", "instanceId", instance.InstanceID)

		// TODO: Implement actual EC2 termination
		// This should:
		// 1. Unregister runner from GitHub
		// 2. Terminate EC2 instance
		// 3. Remove from runnerTracker

		// Placeholder implementation
		delete(s.runnerTracker.instances, instance.InstanceID)
		terminated++
	}

	s.logger.Info("Terminated idle runners", "terminated", terminated)
	return nil
}

// Helper functions

func (s *MessageQueueScaler) extractLabelNames(labels []Label) []string {
	names := make([]string, len(labels))
	for i, label := range labels {
		names[i] = label.Name
	}
	return names
}

func (s *MessageQueueScaler) refreshSession(ctx context.Context) error {
	s.logger.Info("Message queue token expired, refreshing session...")

	session, err := s.actionsClient.RefreshMessageSession(ctx, s.session.RunnerScaleSet.ID, s.session.SessionID)
	if err != nil {
		return fmt.Errorf("refresh message session failed: %w", err)
	}

	s.session = session
	return nil
}

func (s *MessageQueueScaler) deleteLastMessage(ctx context.Context) error {
	s.logger.V(1).Info("Deleting last message", "lastMessageID", s.lastMessageID)

	err := s.actionsClient.DeleteMessage(ctx, s.session.MessageQueueURL, s.session.MessageQueueAccessToken, s.lastMessageID)
	if err == nil {
		return nil
	}

	// Handle token expiration
	if isMessageQueueTokenExpiredError(err) {
		if err := s.refreshSession(ctx); err != nil {
			return err
		}

		err = s.actionsClient.DeleteMessage(ctx, s.session.MessageQueueURL, s.session.MessageQueueAccessToken, s.lastMessageID)
		if err != nil {
			return fmt.Errorf("failed to delete last message after session refresh: %w", err)
		}
	} else {
		return fmt.Errorf("failed to delete last message: %w", err)
	}

	return nil
}

func (s *MessageQueueScaler) cleanupSession(ctx context.Context) {
	if s.session != nil && s.session.SessionID != nil {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		s.logger.Info("Deleting message session")

		if err := s.actionsClient.DeleteMessageSession(ctx, s.session.RunnerScaleSet.ID, s.session.SessionID); err != nil {
			s.logger.Error(err, "Failed to delete message session")
		}
	}
}

func isMessageQueueTokenExpiredError(err error) bool {
	// TODO: Implement proper error type checking
	return err != nil && (err.Error() == "message queue token expired" ||
		err.Error() == "unauthorized")
}

// runDiagnostics runs diagnostic checks to help troubleshoot issues
func (s *MessageQueueScaler) runDiagnostics(ctx context.Context) error {
	s.logger.Info("Running diagnostics to troubleshoot message queue issues")

	// Check acquirable jobs directly
	acquirableJobs, err := s.actionsClient.GetAcquirableJobs(ctx, s.config.RunnerScaleSetID)
	if err != nil {
		s.logger.Error(err, "Failed to get acquirable jobs")
	} else {
		s.logger.Info("Acquirable jobs check", 
			"count", acquirableJobs.Count,
			"jobs", len(acquirableJobs.Jobs))
		
		for i, job := range acquirableJobs.Jobs {
			s.logger.Info("Available job", 
				"index", i,
				"runnerRequestId", job.RunnerRequestID,
				"repositoryName", job.RepositoryName,
				"ownerName", job.OwnerName,
				"requestLabels", job.RequestLabels,
				"jobWorkflowRef", job.JobWorkflowRef)
		}
	}

	// Log current scale set configuration
	s.logger.Info("Current scale set configuration",
		"scaleSetId", s.config.RunnerScaleSetID,
		"scaleSetName", s.config.RunnerScaleSetName,
		"runnerLabels", s.config.RunnerLabels,
		"minRunners", s.config.MinRunners,
		"maxRunners", s.config.MaxRunners)

	// Log session information
	if s.session != nil {
		s.logger.Info("Current message session",
			"sessionId", s.session.SessionID,
			"messageQueueUrl", s.session.MessageQueueURL,
			"lastMessageId", s.lastMessageID)
	}

	return nil
}
