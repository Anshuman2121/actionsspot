package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
	"encoding/json"
)

type PipelineMonitor struct {
	gheClient *GHEClient
	awsInfra  *AWSInfrastructure
	config    Config
}

type PipelineStatus struct {
	QueuedPipelines    []WorkflowRun `json:"queued_pipelines"`
	RunningPipelines   []WorkflowRun `json:"running_pipelines"`
	AvailableRunners   []SelfHostedRunner `json:"available_runners"`
	BusyRunners        []SelfHostedRunner `json:"busy_runners"`
	RunnersNeeded      int `json:"runners_needed"`
	CanCreateRunners   bool `json:"can_create_runners"`
}

func NewPipelineMonitor(gheClient *GHEClient, awsInfra *AWSInfrastructure, config Config) *PipelineMonitor {
	return &PipelineMonitor{
		gheClient: gheClient,
		awsInfra:  awsInfra,
		config:    config,
	}
}

// CheckPendingPipelines checks for pending workflows and determines if runners are needed
func (pm *PipelineMonitor) CheckPendingPipelines(ctx context.Context) (*PipelineStatus, error) {
	log.Printf("🔍 Checking for pending pipelines...")

	// Get queued workflows
	queuedRuns, err := pm.gheClient.GetQueuedWorkflowRuns(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get queued workflows: %w", err)
	}

	// Get running workflows
	runningRuns, err := pm.gheClient.GetRunningWorkflowRuns(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get running workflows: %w", err)
	}

	// Get current runners
	runners, err := pm.gheClient.GetSelfHostedRunners(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get runners: %w", err)
	}

	// Analyze the situation
	status := pm.analyzePipelineStatus(queuedRuns, runningRuns, runners)

	log.Printf("📊 Pipeline Status: Queued=%d, Running=%d, Available Runners=%d, Busy Runners=%d", 
		len(status.QueuedPipelines), len(status.RunningPipelines), 
		len(status.AvailableRunners), len(status.BusyRunners))

	return status, nil
}

// CreateRunnersForPendingPipelines creates runners for pending workflows
func (pm *PipelineMonitor) CreateRunnersForPendingPipelines(ctx context.Context, status *PipelineStatus) error {
	if status.RunnersNeeded <= 0 {
		log.Printf("✅ No additional runners needed")
		return nil
	}

	if !status.CanCreateRunners {
		log.Printf("⚠️  Cannot create more runners (already at max: %d)", pm.config.MaxRunners)
		return nil
	}

	log.Printf("🚀 Creating %d new runners for pending pipelines", status.RunnersNeeded)

	// Get registration token
	token, err := pm.gheClient.GetRegistrationToken(ctx)
	if err != nil {
		return fmt.Errorf("failed to get registration token: %w", err)
	}

	// Create runners
	successCount := 0
	for i := 0; i < status.RunnersNeeded; i++ {
		runnerName := fmt.Sprintf("lambda-runner-%d-%d", time.Now().Unix(), i)
		
		// Create spot instance with runner setup
		spotRequestID, err := pm.awsInfra.CreateSpotInstanceForPipeline(ctx, runnerName, token.Token, pm.config.RunnerLabels)
		if err != nil {
			log.Printf("❌ Failed to create runner %d: %v", i+1, err)
			continue
		}

		log.Printf("✅ Created runner %d/%d: %s (spot request: %s)", 
			i+1, status.RunnersNeeded, runnerName, *spotRequestID)
		successCount++
	}

	log.Printf("🎯 Successfully created %d/%d runners", successCount, status.RunnersNeeded)
	return nil
}

// MonitorAndScale performs the complete monitoring and scaling cycle
func (pm *PipelineMonitor) MonitorAndScale(ctx context.Context) error {
	log.Printf("🔄 Starting pipeline monitoring cycle at %s", time.Now().Format(time.RFC3339))

	// Check current pipeline status
	status, err := pm.CheckPendingPipelines(ctx)
	if err != nil {
		return fmt.Errorf("failed to check pending pipelines: %w", err)
	}

	// Log detailed status
	pm.logDetailedStatus(status)

	// Create runners if needed
	if status.RunnersNeeded > 0 {
		err = pm.CreateRunnersForPendingPipelines(ctx, status)
		if err != nil {
			return fmt.Errorf("failed to create runners: %w", err)
		}
	}

	// Clean up old/offline runners if configured
	if pm.config.CleanupOfflineRunners {
		err = pm.CleanupOfflineRunners(ctx, status)
		if err != nil {
			log.Printf("⚠️  Failed to cleanup offline runners: %v", err)
		}
	}

	log.Printf("✅ Pipeline monitoring cycle completed")
	return nil
}

// analyzePipelineStatus analyzes the current state and determines actions needed
func (pm *PipelineMonitor) analyzePipelineStatus(queued, running *WorkflowRunsList, runners *SelfHostedRunnerList) *PipelineStatus {
	status := &PipelineStatus{
		QueuedPipelines:  queued.WorkflowRuns,
		RunningPipelines: running.WorkflowRuns,
	}

	// Categorize runners
	totalRunners := 0
	for _, runner := range runners.Runners {
		if runner.Status == "online" {
			totalRunners++
			if runner.Busy {
				status.BusyRunners = append(status.BusyRunners, runner)
			} else {
				status.AvailableRunners = append(status.AvailableRunners, runner)
			}
		}
	}

	// Calculate runners needed
	queuedCount := len(status.QueuedPipelines)
	availableCount := len(status.AvailableRunners)
	
	// Basic strategy: need one runner per queued pipeline if no runners available
	if queuedCount > 0 && availableCount == 0 {
		status.RunnersNeeded = queuedCount
	} else if queuedCount > availableCount {
		status.RunnersNeeded = queuedCount - availableCount
	}

	// Respect max runners limit
	currentTotal := totalRunners + pm.getCurrentPendingRunners()
	if currentTotal + status.RunnersNeeded > pm.config.MaxRunners {
		status.RunnersNeeded = pm.config.MaxRunners - currentTotal
		if status.RunnersNeeded < 0 {
			status.RunnersNeeded = 0
		}
	}

	status.CanCreateRunners = status.RunnersNeeded > 0 && currentTotal < pm.config.MaxRunners

	return status
}

// logDetailedStatus logs detailed information about the current status
func (pm *PipelineMonitor) logDetailedStatus(status *PipelineStatus) {
	log.Printf("📋 Detailed Pipeline Status:")
	
	if len(status.QueuedPipelines) > 0 {
		log.Printf("   ⏳ Queued Pipelines (%d):", len(status.QueuedPipelines))
		for i, pipeline := range status.QueuedPipelines {
			if i >= 3 { // Limit output
				log.Printf("      ... and %d more", len(status.QueuedPipelines)-3)
				break
			}
			log.Printf("      - ID: %d, Status: %s", pipeline.ID, pipeline.Status)
		}
	}

	if len(status.RunningPipelines) > 0 {
		log.Printf("   🏃 Running Pipelines (%d):", len(status.RunningPipelines))
		for i, pipeline := range status.RunningPipelines {
			if i >= 3 {
				log.Printf("      ... and %d more", len(status.RunningPipelines)-3)
				break
			}
			log.Printf("      - ID: %d, Runner: %s", pipeline.ID, pipeline.RunnerName)
		}
	}

	log.Printf("   🤖 Runners - Available: %d, Busy: %d", 
		len(status.AvailableRunners), len(status.BusyRunners))

	if status.RunnersNeeded > 0 {
		log.Printf("   🎯 Action: Need to create %d runners", status.RunnersNeeded)
	} else {
		log.Printf("   ✅ Action: No runners needed")
	}
}

// CleanupOfflineRunners removes offline runners from GitHub and terminates EC2 instances
func (pm *PipelineMonitor) CleanupOfflineRunners(ctx context.Context, status *PipelineStatus) error {
	runners, err := pm.gheClient.GetSelfHostedRunners(ctx)
	if err != nil {
		return err
	}

	cleanedCount := 0
	for _, runner := range runners.Runners {
		if runner.Status == "offline" {
			// Remove from GitHub
			err := pm.gheClient.RemoveRunner(ctx, runner.ID)
			if err != nil {
				log.Printf("Failed to remove offline runner %s: %v", runner.Name, err)
				continue
			}

			// Find and terminate corresponding EC2 instance
			err = pm.awsInfra.TerminateRunnerInstance(ctx, runner.Name)
			if err != nil {
				log.Printf("Failed to terminate instance for runner %s: %v", runner.Name, err)
			}

			log.Printf("🧹 Cleaned up offline runner: %s", runner.Name)
			cleanedCount++
		}
	}

	if cleanedCount > 0 {
		log.Printf("🧹 Cleaned up %d offline runners", cleanedCount)
	}

	return nil
}

// getCurrentPendingRunners gets count of runners currently being created
func (pm *PipelineMonitor) getCurrentPendingRunners() int {
	// This would query DynamoDB for pending runner creation requests
	// For now, return 0 as a simple implementation
	return 0
}

// Utility function to get running workflows (add to GHE client)
func (c *GHEClient) GetRunningWorkflowRuns(ctx context.Context) (*WorkflowRunsList, error) {
	url := fmt.Sprintf("%s/orgs/%s/actions/runs?status=in_progress", c.baseURL, c.config.OrganizationName)
	
	resp, err := c.makeRequest(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get running workflow runs (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var runs WorkflowRunsList
	if err := json.NewDecoder(resp.Body).Decode(&runs); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &runs, nil
}

// RemoveRunner removes a self-hosted runner from GitHub
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